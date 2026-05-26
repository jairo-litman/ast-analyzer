package pruner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jairo-litman/ast-analyzer/extractor"
	"github.com/jairo-litman/ast-analyzer/graph"
)

// Extract calls ExtractWithOptions with DefaultExtractOptions.
func Extract(p *graph.Project, symbolID string) (*Context, error) {
	return ExtractWithOptions(p, symbolID, DefaultExtractOptions())
}

// ExtractWithOptions assembles a Context for symbolID. Module-kind
// targets are rejected. For class / interface / enum / type_alias
// targets the callees slice stays empty; callers come from any call
// whose ResolvedTo includes the target.
func ExtractWithOptions(p *graph.Project, symbolID string, opts ExtractOptions) (*Context, error) {
	target, ok := lookupSymbol(p, symbolID)
	if !ok {
		return nil, fmt.Errorf("symbol %q not found in project", symbolID)
	}
	if target.Kind == graph.SymbolModule {
		return nil, fmt.Errorf("Extract does not accept module-kind targets (synthetic per-file callers); got %q", symbolID)
	}

	cache := newSourceCache(p)

	targetSrc, err := cache.symbolSource(target)
	if err != nil {
		return nil, err
	}

	ctx := &Context{
		Target: TargetSource{Symbol: target, Source: targetSrc},
	}

	// EnclosingType only applies to function-kind targets (the
	// class-method case). Other kinds are themselves the top of
	// their declaration.
	if target.Kind == graph.SymbolFunction {
		if encl, ok := findEnclosingClass(p, target); ok {
			enclSrc, err := renderEnclosingClass(cache, encl)
			if err != nil {
				return nil, err
			}
			ctx.EnclosingType = &EnclosingType{Symbol: encl, Source: enclSrc}
		}
	}

	imports, err := collectImports(p, cache, target.File)
	if err != nil {
		return nil, err
	}
	ctx.Imports = filterRelevantImports(ctx, imports)

	// Callees only apply to functions; class symbols are never the
	// CallerID of a call.
	if target.Kind == graph.SymbolFunction {
		callees, err := collectCalleesBFS(p, cache, target, opts)
		if err != nil {
			return nil, err
		}
		ctx.Callees = callees
	}
	callers, err := collectCallersBFS(p, cache, target, opts)
	if err != nil {
		return nil, err
	}
	ctx.Callers = callers

	types, err := collectTypesBFS(p, cache, target, opts)
	if err != nil {
		return nil, err
	}
	ctx.Types = types

	ctx.ImportChains = collectImportChains(p, cache, ctx)

	return ctx, nil
}

// collectImportChains walks the set of files that contribute
// rendered content (callers, callees, type entries) and returns one
// chain per file that has a traceable path to target. The target's
// own file is excluded; duplicate entries (same importing file +
// target) are deduplicated.
func collectImportChains(p *graph.Project, cache *sourceCache, ctx *Context) []ImportChain {
	files := map[string]bool{}
	for _, c := range ctx.Callers {
		if c.Symbol.File != "" && c.Symbol.File != ctx.Target.Symbol.File {
			files[c.Symbol.File] = true
		}
	}
	for _, c := range ctx.Callees {
		if c.Symbol.File != "" && c.Symbol.File != ctx.Target.Symbol.File {
			files[c.Symbol.File] = true
		}
	}
	for _, te := range ctx.Types {
		if te.Symbol.File != "" && te.Symbol.File != ctx.Target.Symbol.File {
			files[te.Symbol.File] = true
		}
	}

	out := make([]ImportChain, 0, len(files))
	for f := range files {
		if chain, ok := buildImportChainWithSource(p, cache, f, ctx.Target.Symbol); ok {
			out = append(out, chain)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ImportingFile < out[j].ImportingFile
	})
	return out
}

// filterRelevantImports keeps only entries whose bindings are
// referenced in the target's body or its enclosing class header.
// Side-effect imports are always kept; their effect is observable
// beyond identifier references.
func filterRelevantImports(ctx *Context, imports []ImportEntry) []ImportEntry {
	if len(imports) == 0 {
		return imports
	}
	text := ctx.Target.Source
	if ctx.EnclosingType != nil && ctx.EnclosingType.Symbol.File == ctx.Target.Symbol.File {
		text = text + "\n" + ctx.EnclosingType.Source
	}
	out := imports[:0]
	for _, imp := range imports {
		if importIsReferenced(imp, text) {
			out = append(out, imp)
		}
	}
	return out
}

// importIsReferenced reports whether any of the import's bound names
// appears as a whole-word identifier in source.
func importIsReferenced(imp ImportEntry, source string) bool {
	if imp.Edge.Kind == extractor.KindSideEffect {
		return true
	}
	if imp.Edge.Namespace != "" && containsIdentifier(source, imp.Edge.Namespace) {
		return true
	}
	for _, ident := range imp.Edge.Identifiers {
		name := ident.LocalName
		if name == "" {
			name = ident.RemoteName
		}
		if containsIdentifier(source, name) {
			return true
		}
	}
	return false
}

// containsIdentifier reports whether name appears as a whole-word
// identifier in source. Word boundaries are non-identifier bytes.
func containsIdentifier(source, name string) bool {
	if name == "" {
		return false
	}
	n, m := len(source), len(name)
	for i := 0; i+m <= n; i++ {
		if source[i:i+m] != name {
			continue
		}
		if i > 0 && isIdentifierByte(source[i-1]) {
			continue
		}
		if i+m < n && isIdentifierByte(source[i+m]) {
			continue
		}
		return true
	}
	return false
}

func isIdentifierByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

func lookupSymbol(p *graph.Project, id string) (graph.Symbol, bool) {
	for _, s := range p.Symbols {
		if s.ID == id {
			return s, true
		}
	}
	return graph.Symbol{}, false
}

// findEnclosingClass returns the smallest class symbol whose byte
// range strictly contains target's. Smallest = innermost.
func findEnclosingClass(p *graph.Project, target graph.Symbol) (graph.Symbol, bool) {
	var best graph.Symbol
	bestSize := ^uint(0)
	found := false
	for _, s := range p.Symbols {
		if s.File != target.File || s.Kind != graph.SymbolClass {
			continue
		}
		if s.StartByte <= target.StartByte && target.EndByte <= s.EndByte {
			size := s.EndByte - s.StartByte
			if size < bestSize {
				bestSize = size
				best = s
				found = true
			}
		}
	}
	return best, found
}

// renderEnclosingClass returns a stripped class header from
// ClassDetails, falling back to the full source slice for symbols
// without ClassDetails.
func renderEnclosingClass(cache *sourceCache, encl graph.Symbol) (string, error) {
	if encl.ClassDetails != nil {
		return renderClassHeader(encl.Name, encl.ClassDetails), nil
	}
	return cache.symbolSource(encl)
}

// renderClassHeader emits a stripped TS-shaped class header from
// ClassDetails alone. Properties render as `name: type;` and methods
// as `name(params): returnType;`. Empty type annotations are omitted.
func renderClassHeader(name string, d *graph.ClassDetails) string {
	var sb strings.Builder
	if d.Abstract {
		sb.WriteString("abstract ")
	}
	sb.WriteString("class ")
	sb.WriteString(name)
	if d.Extends != "" {
		sb.WriteString(" extends ")
		sb.WriteString(d.Extends)
	}
	if len(d.Implements) > 0 {
		sb.WriteString(" implements ")
		sb.WriteString(strings.Join(d.Implements, ", "))
	}
	sb.WriteString(" {\n")
	for _, p := range d.Properties {
		sb.WriteString("    ")
		sb.WriteString(p.Name)
		if p.Type != "" {
			sb.WriteString(": ")
			sb.WriteString(p.Type)
		}
		sb.WriteString(";\n")
	}
	for _, m := range d.Methods {
		sb.WriteString("    ")
		sb.WriteString(m.Name)
		sb.WriteString("(")
		for i, param := range m.Parameters {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(param.Name)
			if param.Type != "" {
				sb.WriteString(": ")
				sb.WriteString(param.Type)
			}
		}
		sb.WriteString(")")
		if m.ReturnType != "" {
			sb.WriteString(": ")
			sb.WriteString(m.ReturnType)
		}
		sb.WriteString(";\n")
	}
	sb.WriteString("}")
	return sb.String()
}

// collectImports gathers every import statement from file along with
// its raw source text, in source order.
func collectImports(p *graph.Project, cache *sourceCache, file string) ([]ImportEntry, error) {
	var out []ImportEntry
	var src []byte

	for _, edge := range p.Imports {
		if edge.File != file {
			continue
		}
		if edge.EndByte == 0 {
			// Edge has no byte range; surface metadata without Source.
			out = append(out, ImportEntry{Edge: edge})
			continue
		}
		if src == nil {
			loaded, err := cache.source(file)
			if err != nil {
				return nil, err
			}
			src = loaded
		}
		if edge.EndByte > uint(len(src)) || edge.StartByte > edge.EndByte {
			return nil, fmt.Errorf("import in %s byte range [%d,%d) is invalid", file, edge.StartByte, edge.EndByte)
		}
		out = append(out, ImportEntry{
			Edge:   edge,
			Source: string(src[edge.StartByte:edge.EndByte]),
		})
	}
	return out, nil
}

// collectCalleesBFS walks the call graph downward from target up to
// opts.CalleeDepth hops. Entries within opts.CalleeBodyDepth carry
// the full source body; the rest carry Signature only. Cycles are
// broken by a visited-set seeded with the target.
func collectCalleesBFS(p *graph.Project, cache *sourceCache, target graph.Symbol, opts ExtractOptions) ([]Callee, error) {
	if opts.CalleeDepth <= 0 {
		return nil, nil
	}

	visited := map[string]bool{target.ID: true}
	var result []Callee
	frontier := []string{target.ID}

	for depth := 1; depth <= opts.CalleeDepth && len(frontier) > 0; depth++ {
		buckets := map[string][]graph.CallSite{}
		for _, currentID := range frontier {
			for _, c := range p.Calls {
				if c.CallerID != currentID {
					continue
				}
				for _, resolved := range c.ResolvedTo {
					if visited[resolved] {
						continue
					}
					buckets[resolved] = append(buckets[resolved], c)
				}
			}
		}
		entries := truncateByID(buckets, opts.MaxPerLevel)

		var next []string
		for _, symID := range entries {
			sym, ok := lookupSymbol(p, symID)
			if !ok {
				continue
			}
			visited[sym.ID] = true
			callee := Callee{
				Symbol:    sym,
				Signature: cache.signature(sym),
				File:      sym.File,
				Depth:     depth,
				CallSites: buckets[symID],
			}
			if depth <= opts.CalleeBodyDepth {
				body, err := cache.symbolSource(sym)
				if err != nil {
					return nil, err
				}
				callee.Body = body
			}
			result = append(result, callee)
			// Only function-kind symbols can themselves call further.
			if sym.Kind == graph.SymbolFunction {
				next = append(next, sym.ID)
			}
		}
		frontier = next
	}
	return result, nil
}

// collectCallersBFS walks the call graph upward from target up to
// opts.CallerDepth hops. Module-kind callers terminate the frontier.
func collectCallersBFS(p *graph.Project, cache *sourceCache, target graph.Symbol, opts ExtractOptions) ([]Caller, error) {
	if opts.CallerDepth <= 0 {
		return nil, nil
	}

	visited := map[string]bool{target.ID: true}
	var result []Caller
	frontier := []string{target.ID}

	for depth := 1; depth <= opts.CallerDepth && len(frontier) > 0; depth++ {
		buckets := map[string][]graph.CallSite{}
		for _, currentID := range frontier {
			for _, c := range p.Calls {
				hit := false
				for _, resolved := range c.ResolvedTo {
					if resolved == currentID {
						hit = true
						break
					}
				}
				if !hit {
					continue
				}
				if visited[c.CallerID] {
					continue
				}
				buckets[c.CallerID] = append(buckets[c.CallerID], c)
			}
		}
		entries := truncateByID(buckets, opts.MaxPerLevel)

		var next []string
		for _, symID := range entries {
			sym, ok := lookupSymbol(p, symID)
			if !ok {
				continue
			}
			visited[sym.ID] = true
			caller := Caller{
				Symbol:    sym,
				Signature: cache.signature(sym),
				File:      sym.File,
				Depth:     depth,
				CallSites: buckets[symID],
			}
			if depth <= opts.CallerBodyDepth {
				body, err := cache.symbolSource(sym)
				if err != nil {
					return nil, err
				}
				caller.Body = body
			}
			result = append(result, caller)
			if sym.Kind == graph.SymbolFunction {
				next = append(next, sym.ID)
			}
		}
		frontier = next
	}
	return result, nil
}

// truncateByID returns the keys of buckets sorted ascending. When
// maxPerLevel > 0 and the bucket count exceeds it, the tail is
// dropped — deterministic by symbol-ID order.
func truncateByID(buckets map[string][]graph.CallSite, maxPerLevel int) []string {
	out := make([]string, 0, len(buckets))
	for k := range buckets {
		out = append(out, k)
	}
	sort.Strings(out)
	if maxPerLevel > 0 && len(out) > maxPerLevel {
		out = out[:maxPerLevel]
	}
	return out
}

// sourceCache memoizes file reads across one Extract call so a
// referenced file is read at most once.
type sourceCache struct {
	p     *graph.Project
	files map[string][]byte
}

func newSourceCache(p *graph.Project) *sourceCache {
	return &sourceCache{p: p, files: map[string][]byte{}}
}

func (c *sourceCache) source(relPath string) ([]byte, error) {
	if src, ok := c.files[relPath]; ok {
		return src, nil
	}
	src, err := os.ReadFile(filepath.Join(c.p.Root, relPath))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", relPath, err)
	}
	c.files[relPath] = src
	return src, nil
}

func (c *sourceCache) symbolSource(s graph.Symbol) (string, error) {
	src, err := c.source(s.File)
	if err != nil {
		return "", err
	}
	if s.EndByte > uint(len(src)) || s.StartByte > s.EndByte {
		return "", fmt.Errorf("symbol %q byte range [%d,%d) is invalid for %s", s.ID, s.StartByte, s.EndByte, s.File)
	}
	return string(src[s.StartByte:s.EndByte]), nil
}

// signature returns the source text from sym.StartByte up to the
// body opening — for a function, the declaration line (name, params,
// return type). Falls back to the symbol's name for non-function
// kinds or invalid slices.
func (c *sourceCache) signature(sym graph.Symbol) string {
	if sym.Kind != graph.SymbolFunction {
		return sym.Name
	}
	if sym.BodyStartByte == 0 || sym.BodyStartByte <= sym.StartByte {
		return sym.Name
	}
	src, err := c.source(sym.File)
	if err != nil {
		return sym.Name
	}
	if sym.BodyStartByte > uint(len(src)) {
		return sym.Name
	}
	return strings.TrimSpace(string(src[sym.StartByte:sym.BodyStartByte]))
}
