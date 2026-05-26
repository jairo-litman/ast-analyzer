package pruner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jairo-litman/ast-analyzer/graph"
)

const cutMarker = "<- cut content ->"

// moduleCallSiteContextLines is the number of source lines to keep
// on each side of a module-scope call site so the enclosing
// describe/it block or top-level statement surfaces in the recorte.
const moduleCallSiteContextLines = 3

// RenderRedacted emits a multi-file, source-faithful view of ctx
// with `<- cut content ->` markers between kept ranges and a
// per-file metadata comment summarising the callees, callers, and
// type entries that contributed content.
func RenderRedacted(ctx *Context, p *graph.Project) (string, error) {
	cache := newSourceCache(p)
	perFile, files, err := computeFileSections(ctx, p, cache)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	first := true
	for _, f := range files {
		ranges, ok := perFile[f]
		if !ok || len(ranges) == 0 {
			continue
		}
		if !first {
			sb.WriteByte('\n')
		}
		first = false

		sb.WriteString("# ")
		sb.WriteString(f)
		sb.WriteByte('\n')
		sb.WriteString(entryMetadata(ctx, f))

		source, err := cache.source(f)
		if err != nil {
			return "", fmt.Errorf("source for %s: %w", f, err)
		}
		sb.WriteString(renderRangesAsLines(source, ranges))
	}
	return sb.String(), nil
}

// RenderMarkdown wraps RenderRedacted's structure in `## file:`
// headings and fenced TS code blocks. The metadata comment travels
// inside the fence so it survives copy-paste into a chat.
func RenderMarkdown(ctx *Context, p *graph.Project) (string, error) {
	cache := newSourceCache(p)
	perFile, files, err := computeFileSections(ctx, p, cache)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Extracted context: `%s`\n", ctx.Target.Symbol.ID)

	for _, f := range files {
		ranges, ok := perFile[f]
		if !ok || len(ranges) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n## file: %s\n\n", f)
		sb.WriteString("```ts\n")
		sb.WriteString(entryMetadata(ctx, f))
		source, err := cache.source(f)
		if err != nil {
			return "", fmt.Errorf("source for %s: %w", f, err)
		}
		sb.WriteString(renderRangesAsLines(source, ranges))
		sb.WriteString("```\n")
	}
	return sb.String(), nil
}

// entryMetadata returns the per-file metadata block: one or two
// comment lines naming the callees/callers in file with their depth
// and inclusion mode (body / signature). Empty string when neither
// list contributes content to the file.
func entryMetadata(ctx *Context, file string) string {
	formatEntry := func(name string, depth int, hasBody bool) string {
		mode := "signature"
		if hasBody {
			mode = "body"
		}
		return fmt.Sprintf("%s (depth=%d, %s)", name, depth, mode)
	}

	var callees, callers, types, chains []string
	for _, c := range ctx.Callees {
		if c.Symbol.File != file {
			continue
		}
		callees = append(callees, formatEntry(c.Symbol.Name, c.Depth, c.Body != ""))
	}
	for _, c := range ctx.Callers {
		if c.Symbol.File != file {
			continue
		}
		callers = append(callers, formatEntry(c.Symbol.Name, c.Depth, c.Body != ""))
	}
	for _, te := range ctx.Types {
		if te.Symbol.File != file {
			continue
		}
		types = append(types, formatTypeEntry(te))
	}
	for _, chain := range ctx.ImportChains {
		if chain.ImportingFile != file {
			continue
		}
		chains = append(chains, formatImportChain(chain))
	}

	if len(callees) == 0 && len(callers) == 0 && len(types) == 0 && len(chains) == 0 {
		return ""
	}
	var sb strings.Builder
	if len(callees) > 0 {
		sb.WriteString("// callees: ")
		sb.WriteString(strings.Join(callees, "; "))
		sb.WriteByte('\n')
	}
	if len(callers) > 0 {
		sb.WriteString("// callers: ")
		sb.WriteString(strings.Join(callers, "; "))
		sb.WriteByte('\n')
	}
	if len(types) > 0 {
		sb.WriteString("// types: ")
		sb.WriteString(strings.Join(types, "; "))
		sb.WriteByte('\n')
	}
	if len(chains) > 0 {
		sb.WriteString("// chains: ")
		sb.WriteString(strings.Join(chains, "; "))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// formatImportChain renders one chain as `local -> canonical (via
// file1, file2, ...)`. The "via" list omits the importing file (the
// section already declares it) and the target file (implicit).
func formatImportChain(c ImportChain) string {
	if c.LocalName == c.TargetName {
		if len(c.Trail) <= 1 {
			return fmt.Sprintf("%s (direct)", c.LocalName)
		}
		return fmt.Sprintf("%s (via %s)", c.LocalName, hopFiles(c.Trail))
	}
	if len(c.Trail) <= 1 {
		return fmt.Sprintf("%s -> %s (direct)", c.LocalName, c.TargetName)
	}
	return fmt.Sprintf("%s -> %s (via %s)", c.LocalName, c.TargetName, hopFiles(c.Trail))
}

func hopFiles(trail []ChainHop) string {
	if len(trail) <= 1 {
		return ""
	}
	files := make([]string, 0, len(trail)-1)
	for _, h := range trail[1:] {
		files = append(files, h.File)
	}
	return strings.Join(files, ", ")
}

// formatTypeEntry renders one TypeEntry as `Name (depth=N, kind,
// origins=a+b)`.
func formatTypeEntry(te TypeEntry) string {
	originStrs := make([]string, 0, len(te.Origins))
	for _, o := range te.Origins {
		originStrs = append(originStrs, string(o))
	}
	origins := strings.Join(originStrs, "+")
	if origins == "" {
		origins = "unknown"
	}
	return fmt.Sprintf("%s (depth=%d, %s, origins=%s)", te.Symbol.Name, te.Depth, te.Symbol.Kind, origins)
}

// expandForLeadingComment returns the start byte rewound past any
// immediately-preceding `/** */`, `/* */`, or `//` doc comment.
// Whitespace and declaration-modifier keywords (export, default,
// abstract, ...) between the comment and decl are walked through,
// because tree-sitter often anchors decl at the inner keyword and
// leaves modifiers in an outer node.
func expandForLeadingComment(source []byte, decl uint) uint {
	if decl == 0 || int(decl) > len(source) {
		return decl
	}
	i := int(decl) - 1
	for {
		for i >= 0 && isSpaceByte(source[i]) {
			i--
		}
		if i < 1 {
			return decl
		}
		// `*/` closing a block comment.
		if source[i] == '/' && source[i-1] == '*' {
			for j := i - 2; j >= 1; j-- {
				if source[j] == '*' && source[j-1] == '/' {
					return uint(j - 1)
				}
			}
			return decl
		}
		// `//` line comment(s): the position after stripping whitespace
		// lands on the last character of the comment text, not on
		// `//` itself. So we check whether the line containing i is
		// a comment line (trimmed-leading-whitespace starts with //),
		// then walk up through any consecutive comment lines above.
		lineStart := i
		for lineStart > 0 && source[lineStart-1] != '\n' {
			lineStart--
		}
		ws := lineStart
		for ws < i && (source[ws] == ' ' || source[ws] == '\t') {
			ws++
		}
		if ws+1 <= i && source[ws] == '/' && source[ws+1] == '/' {
			for lineStart > 0 {
				prevLineEnd := lineStart - 1
				prevLineStart := prevLineEnd
				for prevLineStart > 0 && source[prevLineStart-1] != '\n' {
					prevLineStart--
				}
				ws2 := prevLineStart
				for ws2 < prevLineEnd && (source[ws2] == ' ' || source[ws2] == '\t') {
					ws2++
				}
				if ws2+1 < prevLineEnd && source[ws2] == '/' && source[ws2+1] == '/' {
					lineStart = prevLineStart
					continue
				}
				break
			}
			return uint(lineStart)
		}
		// Identifier — accept only known declaration modifiers and
		// keep scanning back for a comment beyond them.
		if isIdentChar(source[i]) {
			end := i + 1
			for i >= 0 && isIdentChar(source[i]) {
				i--
			}
			start := i + 1
			if !isModifierKeyword(string(source[start:end])) {
				return decl
			}
			continue
		}
		return decl
	}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

func isIdentChar(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// expandModuleCallSite widens site to include linesContext source
// lines before its starting line and after its ending line. Used
// for module-scope call sites where the surrounding statement
// carries the meaning (an `it(...)` block, a top-level
// `app.use(...)` wiring, ...).
func expandModuleCallSite(source []byte, site byteRange, linesContext int) byteRange {
	if linesContext <= 0 || len(source) == 0 {
		return site
	}
	lineStarts := computeLineStarts(source)
	fileLen := uint(len(source))

	startLineIdx := lineOf(lineStarts, site.Start) - 1
	if startLineIdx-linesContext < 0 {
		startLineIdx = 0
	} else {
		startLineIdx -= linesContext
	}
	newStart := lineStarts[startLineIdx]

	endByte := site.End
	if endByte == 0 {
		endByte = 1
	}
	endLineIdx := lineOf(lineStarts, endByte-1) - 1
	targetEndLine := endLineIdx + linesContext + 1
	newEnd := fileLen
	if targetEndLine < len(lineStarts) {
		newEnd = lineStarts[targetEndLine]
	}
	return byteRange{Start: newStart, End: newEnd}
}

func isModifierKeyword(s string) bool {
	switch s {
	case "export", "default", "abstract", "async",
		"declare", "public", "private", "protected",
		"static", "readonly",
		// Declaration heads for arrow-function consts and similar
		// bindings where Symbol.StartByte may land at or past these
		// tokens.
		"const", "let", "var":
		return true
	}
	return false
}

func isAllWhitespace(b []byte) bool {
	for _, c := range b {
		if !isSpaceByte(c) {
			return false
		}
	}
	return true
}

// keepKind tags how a function-kind symbol is retained inside a
// class. keepFully keeps the whole body; keepSignatureOnly subtracts
// the body bytes but keeps the declaration line.
type keepKind int

const (
	keepFully keepKind = iota
	keepSignatureOnly
)

// computeFileSections plans the per-file byte ranges shared by
// RenderRedacted and RenderMarkdown. Returns the ranges keyed by
// file plus a stable file order (target file first, then sorted).
func computeFileSections(ctx *Context, p *graph.Project, cache *sourceCache) (map[string][]byteRange, []string, error) {
	// keptInsideClass marks function-kind symbol IDs that survive
	// class redaction. The target is always kept fully.
	keptInsideClass := map[string]keepKind{ctx.Target.Symbol.ID: keepFully}
	for _, c := range ctx.Callees {
		if c.Body != "" {
			keptInsideClass[c.Symbol.ID] = keepFully
		} else {
			keptInsideClass[c.Symbol.ID] = keepSignatureOnly
		}
	}
	for _, c := range ctx.Callers {
		if c.Body != "" {
			keptInsideClass[c.Symbol.ID] = keepFully
		} else {
			keptInsideClass[c.Symbol.ID] = keepSignatureOnly
		}
	}

	perFile := map[string][]byteRange{}
	targetFile := ctx.Target.Symbol.File

	// classesRendered dedupes class rendering across paths that may
	// land on the same class (enclosing + callee constructor + ...).
	classesRendered := map[string]bool{}

	renderClass := func(class graph.Symbol) error {
		if classesRendered[class.ID] {
			return nil
		}
		classesRendered[class.ID] = true

		source, err := cache.source(class.File)
		if err != nil {
			return fmt.Errorf("source for %s: %w", class.File, err)
		}
		lineStarts := computeLineStarts(source)
		fileLen := uint(len(source))

		// The class range expands backwards to pull in any JSDoc /
		// line-comment block immediately above the class declaration.
		// Subtract ranges must round to whole lines BEFORE subtraction;
		// rounding afterward leaves the method's first and last lines
		// leaking through the cut.
		classStart := expandForLeadingComment(source, class.StartByte)
		classRange := roundToLines(byteRange{Start: classStart, End: class.EndByte}, lineStarts, fileLen)

		var subtract []byteRange
		for _, s := range p.Symbols {
			if s.File != class.File || s.Kind != graph.SymbolFunction {
				continue
			}
			if !insideRange(s.StartByte, s.EndByte, class.StartByte, class.EndByte) {
				continue
			}
			if s.Name == "constructor" {
				continue
			}
			if mode, ok := keptInsideClass[s.ID]; ok {
				if mode == keepFully {
					continue
				}
				// signature-only: subtract only the body bytes, leaving
				// the doc comment and declaration line visible inside
				// the class.
				if s.BodyStartByte > s.StartByte && s.BodyStartByte < s.EndByte {
					subtract = append(subtract, roundToLines(byteRange{Start: s.BodyStartByte, End: s.EndByte}, lineStarts, fileLen))
					continue
				}
			}
			// Fully elided: subtract from any preceding doc comment
			// through the method body so the doc doesn't orphan above
			// the cut marker.
			docStart := expandForLeadingComment(source, s.StartByte)
			subtract = append(subtract, roundToLines(byteRange{Start: docStart, End: s.EndByte}, lineStarts, fileLen))
		}
		perFile[class.File] = append(perFile[class.File],
			subtractRanges([]byteRange{classRange}, subtract)...)
		return nil
	}

	// findEnclosingClass returns the smallest class symbol whose
	// byte range contains sym's, or (Symbol{}, false) if none.
	findEnclosingClass := func(sym graph.Symbol) (graph.Symbol, bool) {
		var best graph.Symbol
		bestSize := ^uint(0)
		found := false
		for _, c := range p.Symbols {
			if c.File != sym.File || c.Kind != graph.SymbolClass {
				continue
			}
			if !insideRange(sym.StartByte, sym.EndByte, c.StartByte, c.EndByte) {
				continue
			}
			size := c.EndByte - c.StartByte
			if size < bestSize {
				bestSize = size
				best = c
				found = true
			}
		}
		return best, found
	}

	// Imports section.
	for _, imp := range ctx.Imports {
		if imp.Edge.EndByte > imp.Edge.StartByte {
			perFile[targetFile] = append(perFile[targetFile], byteRange{
				Start: imp.Edge.StartByte,
				End:   imp.Edge.EndByte,
			})
		}
	}

	// Target's enclosing scope: enclosing class (with subtraction)
	// or the target's own range, pulled back to include any
	// preceding doc comment.
	if ctx.EnclosingType != nil {
		if err := renderClass(ctx.EnclosingType.Symbol); err != nil {
			return nil, nil, err
		}
	} else {
		src, err := cache.source(targetFile)
		if err != nil {
			return nil, nil, err
		}
		start := expandForLeadingComment(src, ctx.Target.Symbol.StartByte)
		perFile[targetFile] = append(perFile[targetFile], byteRange{
			Start: start,
			End:   ctx.Target.Symbol.EndByte,
		})
	}

	// Caller / callee ranges:
	//   - Module-kind callers surface only their call sites (rendering
	//     the whole file would drown out the redaction).
	//   - Class symbols (e.g. constructor callees) render as redacted
	//     class headers.
	//   - Function symbols inside a class render that class with
	//     redaction; standalone functions emit their own range.
	addCallSites := func(sites []graph.CallSite) {
		for _, site := range sites {
			r := byteRange{Start: site.StartByte, End: site.EndByte}
			if src, err := cache.source(site.File); err == nil {
				r = expandModuleCallSite(src, r, moduleCallSiteContextLines)
			}
			perFile[site.File] = append(perFile[site.File], r)
		}
	}
	addSymbol := func(sym graph.Symbol, sites []graph.CallSite, wantBody bool) error {
		switch sym.Kind {
		case graph.SymbolModule:
			addCallSites(sites)
			return nil
		case graph.SymbolClass:
			return renderClass(sym)
		case graph.SymbolFunction:
			if class, ok := findEnclosingClass(sym); ok {
				return renderClass(class)
			}
			src, err := cache.source(sym.File)
			if err != nil {
				return err
			}
			start := expandForLeadingComment(src, sym.StartByte)
			end := sym.EndByte
			if !wantBody && sym.BodyStartByte > sym.StartByte && sym.BodyStartByte < sym.EndByte {
				end = sym.BodyStartByte
			}
			perFile[sym.File] = append(perFile[sym.File], byteRange{
				Start: start,
				End:   end,
			})
		}
		return nil
	}

	for _, c := range ctx.Callees {
		if err := addSymbol(c.Symbol, c.CallSites, c.Body != ""); err != nil {
			return nil, nil, err
		}
	}
	for _, c := range ctx.Callers {
		if err := addSymbol(c.Symbol, c.CallSites, c.Body != ""); err != nil {
			return nil, nil, err
		}
	}

	// Type entries: classes render with method-body redaction;
	// interface / enum / type_alias entries emit the full declaration.
	for _, te := range ctx.Types {
		switch te.Symbol.Kind {
		case graph.SymbolClass:
			if err := renderClass(te.Symbol); err != nil {
				return nil, nil, err
			}
		case graph.SymbolInterface, graph.SymbolEnum, graph.SymbolTypeAlias:
			src, err := cache.source(te.Symbol.File)
			if err != nil {
				return nil, nil, err
			}
			start := expandForLeadingComment(src, te.Symbol.StartByte)
			perFile[te.Symbol.File] = append(perFile[te.Symbol.File], byteRange{
				Start: start,
				End:   te.Symbol.EndByte,
			})
		}
	}

	// Import chains: every hop adds a byte range to its own file
	// section. The first hop is an import in the consuming file; the
	// rest are re-exports in intermediate barrel files. Each
	// re-export hop is expanded to pull in any preceding doc comment.
	for _, chain := range ctx.ImportChains {
		for i, hop := range chain.Trail {
			if hop.EndByte <= hop.StartByte {
				continue
			}
			start := hop.StartByte
			if i > 0 {
				if src, err := cache.source(hop.File); err == nil {
					start = expandForLeadingComment(src, hop.StartByte)
				}
			}
			perFile[hop.File] = append(perFile[hop.File], byteRange{
				Start: start,
				End:   hop.EndByte,
			})
		}
	}

	// Render the target file first, then others lex-sorted for
	// stable output.
	files := []string{targetFile}
	var others []string
	for f := range perFile {
		if f != targetFile {
			others = append(others, f)
		}
	}
	sort.Strings(others)
	files = append(files, others...)
	return perFile, files, nil
}

// byteRange is a half-open [Start, End) byte interval.
type byteRange struct {
	Start, End uint
}

// insideRange reports whether [innerStart, innerEnd) is contained in
// [outerStart, outerEnd).
func insideRange(innerStart, innerEnd, outerStart, outerEnd uint) bool {
	return outerStart <= innerStart && innerEnd <= outerEnd
}

// mergeRanges sorts ranges and collapses overlapping or adjacent
// entries. Half-open adjacency collapses cleanly via Start <= End.
func mergeRanges(ranges []byteRange) []byteRange {
	if len(ranges) == 0 {
		return ranges
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].Start != ranges[j].Start {
			return ranges[i].Start < ranges[j].Start
		}
		return ranges[i].End < ranges[j].End
	})
	out := []byteRange{ranges[0]}
	for i := 1; i < len(ranges); i++ {
		last := &out[len(out)-1]
		if ranges[i].Start <= last.End {
			if ranges[i].End > last.End {
				last.End = ranges[i].End
			}
			continue
		}
		out = append(out, ranges[i])
	}
	return out
}

// subtractRanges returns `ranges` with every overlap of `subtract`
// removed. For each input range, walks the subtractions, emitting
// left and/or right remainders as overlap dictates.
func subtractRanges(ranges []byteRange, subtract []byteRange) []byteRange {
	var out []byteRange
	for _, r := range ranges {
		pieces := []byteRange{r}
		for _, s := range subtract {
			var next []byteRange
			for _, p := range pieces {
				if s.End <= p.Start || s.Start >= p.End {
					next = append(next, p)
					continue
				}
				if s.Start > p.Start {
					next = append(next, byteRange{Start: p.Start, End: s.Start})
				}
				if s.End < p.End {
					next = append(next, byteRange{Start: s.End, End: p.End})
				}
			}
			pieces = next
			if len(pieces) == 0 {
				break
			}
		}
		out = append(out, pieces...)
	}
	return out
}

// renderRangesAsLines emits the source covered by `ranges` as
// line-numbered text, with cut markers between non-adjacent ranges
// and at file boundaries. Each range rounds to whole lines.
func renderRangesAsLines(source []byte, ranges []byteRange) string {
	if len(ranges) == 0 {
		return ""
	}
	lineStarts := computeLineStarts(source)
	fileLen := uint(len(source))

	rounded := make([]byteRange, 0, len(ranges))
	for _, r := range ranges {
		rounded = append(rounded, roundToLines(r, lineStarts, fileLen))
	}
	rounded = mergeRanges(rounded)

	// Drop ranges whose content is all whitespace: these arise as
	// by-products of class-method subtraction (blank lines between
	// elided methods) and otherwise show up as `<- cut content ->\n
	// NN: \n<- cut content ->`. Folding them into a single cut keeps
	// the elision visually quiet.
	filtered := rounded[:0]
	for _, r := range rounded {
		if isAllWhitespace(source[r.Start:r.End]) {
			continue
		}
		filtered = append(filtered, r)
	}
	rounded = filtered
	if len(rounded) == 0 {
		return ""
	}

	var sb strings.Builder
	if rounded[0].Start > 0 {
		sb.WriteString(cutMarker)
		sb.WriteByte('\n')
	}

	for i, r := range rounded {
		if i > 0 {
			sb.WriteString(cutMarker)
			sb.WriteByte('\n')
		}
		startLine := lineOf(lineStarts, r.Start)
		slice := source[r.Start:r.End]
		// Drop the trailing newline so each printed line owns its own
		// \n; otherwise a blank line would precede the cut marker.
		if len(slice) > 0 && slice[len(slice)-1] == '\n' {
			slice = slice[:len(slice)-1]
		}
		for j, line := range strings.Split(string(slice), "\n") {
			fmt.Fprintf(&sb, "%d: %s\n", startLine+j, line)
		}
	}

	if rounded[len(rounded)-1].End < fileLen {
		sb.WriteString(cutMarker)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// computeLineStarts returns the byte offset of every line start in
// source. Index i is the start of line i+1 (1-based). Always
// contains at least one entry (0).
func computeLineStarts(source []byte) []uint {
	out := []uint{0}
	for i, b := range source {
		if b == '\n' {
			out = append(out, uint(i+1))
		}
	}
	return out
}

// lineOf returns the 1-based line number containing offset.
func lineOf(lineStarts []uint, offset uint) int {
	return sort.Search(len(lineStarts), func(i int) bool {
		return lineStarts[i] > offset
	})
}

// roundToLines expands r to cover whole lines. Start moves to the
// containing line's first byte; End moves to the next line's start
// (or fileLen at end-of-file).
func roundToLines(r byteRange, lineStarts []uint, fileLen uint) byteRange {
	if r.End <= r.Start {
		return r
	}
	startLine := lineOf(lineStarts, r.Start)
	r.Start = lineStarts[startLine-1]

	endLine := lineOf(lineStarts, r.End-1)
	if endLine < len(lineStarts) {
		r.End = lineStarts[endLine]
	} else {
		r.End = fileLen
	}
	return r
}
