package graph

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/jairo-litman/ast-analyzer/extractor"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// BuildProject walks rootDir, parses every `.ts` and `.tsx` file with
// the matching grammar, runs the full extractor query set, and
// assembles a Project. tsconfigPath drives path-alias and module
// resolution.
//
// Per-file parsing and extraction run on a worker pool of
// runtime.NumCPU() goroutines; the merge into the shared Project is
// serialised. Output is sorted by (File, StartByte) to keep results
// deterministic regardless of worker completion order.
func BuildProject(rootDir, tsconfigPath string) (*Project, error) {
	pair, err := newExtractorPair(tsconfigPath)
	if err != nil {
		return nil, fmt.Errorf("build project: %w", err)
	}

	rootAbs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("absolute root: %w", err)
	}

	project := &Project{
		Root:  rootAbs,
		Files: map[string]*FileResult{},
	}

	work, err := enumerateSourceFiles(rootAbs)
	if err != nil {
		return nil, err
	}
	if len(work) == 0 {
		return project, nil
	}

	if err := buildProjectParallel(project, pair, work); err != nil {
		project.Close()
		return nil, err
	}

	sortProjectByFileAndPosition(project)
	return project, nil
}

// fileWork is one (relPath, absPath) job in the build pipeline.
type fileWork struct {
	relPath string
	absPath string
}

// enumerateSourceFiles walks rootAbs collecting fileWork entries for
// every file IsIncludedFile accepts.
func enumerateSourceFiles(rootAbs string) ([]fileWork, error) {
	var work []fileWork
	err := filepath.WalkDir(rootAbs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != rootAbs && IsSkippedDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !IsIncludedFile(path) {
			return nil
		}
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil {
			return fmt.Errorf("relative path for %s: %w", path, relErr)
		}
		work = append(work, fileWork{relPath: filepath.ToSlash(rel), absPath: path})
		return nil
	})
	return work, err
}

// buildProjectParallel runs the parse phase for each work item across
// runtime.NumCPU() workers and merges their results into project on a
// single goroutine. The first parse error (if any) is returned;
// already-collected trees are closed by the caller via project.Close.
func buildProjectParallel(project *Project, pair *extractorPair, work []fileWork) error {
	workers := runtime.NumCPU()
	if workers > len(work) {
		workers = len(work)
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan fileWork, len(work))
	results := make(chan *parseResult, len(work))
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				results <- parseFile(pair.For(job.relPath), project.Root, job.relPath, job.absPath)
			}
		}()
	}

	for _, job := range work {
		jobs <- job
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			if res.tree != nil {
				res.tree.Close()
			}
			continue
		}
		if firstErr != nil {
			// Drain remaining trees so they don't leak.
			if res.tree != nil {
				res.tree.Close()
			}
			continue
		}
		mergeParseResult(project, res)
	}
	return firstErr
}

// sortProjectByFileAndPosition gives deterministic global ordering to
// the slices BuildProject populates so worker completion order doesn't
// leak into the output.
func sortProjectByFileAndPosition(p *Project) {
	sort.SliceStable(p.Symbols, func(i, j int) bool {
		if p.Symbols[i].File != p.Symbols[j].File {
			return p.Symbols[i].File < p.Symbols[j].File
		}
		return p.Symbols[i].StartByte < p.Symbols[j].StartByte
	})
	sort.SliceStable(p.Calls, func(i, j int) bool {
		if p.Calls[i].File != p.Calls[j].File {
			return p.Calls[i].File < p.Calls[j].File
		}
		return p.Calls[i].StartByte < p.Calls[j].StartByte
	})
	sort.SliceStable(p.Imports, func(i, j int) bool {
		if p.Imports[i].File != p.Imports[j].File {
			return p.Imports[i].File < p.Imports[j].File
		}
		return p.Imports[i].StartByte < p.Imports[j].StartByte
	})
	sort.SliceStable(p.ReExports, func(i, j int) bool {
		if p.ReExports[i].File != p.ReExports[j].File {
			return p.ReExports[i].File < p.ReExports[j].File
		}
		return p.ReExports[i].StartByte < p.ReExports[j].StartByte
	})
	sort.Strings(p.Warnings)
}

// extractorPair holds one extractor per grammar variant so per-file
// work can be routed by extension.
type extractorPair struct {
	ts  *extractor.Extractor
	tsx *extractor.Extractor
}

func newExtractorPair(tsconfigPath string) (*extractorPair, error) {
	tsExt, err := extractor.NewExtractorForLanguage(tsconfigPath, extractor.LangTypeScript)
	if err != nil {
		return nil, err
	}
	tsxExt, err := extractor.NewExtractorForLanguage(tsconfigPath, extractor.LangTSX)
	if err != nil {
		return nil, err
	}
	return &extractorPair{ts: tsExt, tsx: tsxExt}, nil
}

// For picks the extractor matching relPath's extension. Anything not
// ending in `.tsx` uses the TS grammar.
func (p *extractorPair) For(relPath string) *extractor.Extractor {
	if strings.HasSuffix(relPath, ".tsx") {
		return p.tsx
	}
	return p.ts
}

// IsSkippedDir reports whether the walker should refuse to descend
// into a directory of the given name (`node_modules`, `.git`, common
// build outputs).
func IsSkippedDir(name string) bool {
	switch name {
	case "node_modules", ".git", "dist", "build", "out", ".next", "coverage":
		return true
	}
	return false
}

// IsIncludedFile selects TypeScript and TSX implementation files.
// Declaration files (`.d.ts`) are skipped — they carry no bodies.
func IsIncludedFile(path string) bool {
	switch {
	case strings.HasSuffix(path, ".d.ts"):
		return false
	case strings.HasSuffix(path, ".ts"), strings.HasSuffix(path, ".tsx"):
		return true
	}
	return false
}

// parseResult is what a parse-phase worker produces for one file:
// the parsed tree, raw extractor outputs, and already-resolved import
// and re-export edges. Merging these into a Project is the serial
// step performed by mergeParseResult.
type parseResult struct {
	relPath        string
	source         []byte
	tree           *sitter.Tree
	fr             *FileResult
	callContexts   []extractor.FunctionCallContext
	defaultNames   []string
	resolvedImps   []ImportEdge
	resolvedReExps []ReExportEdge
	err            error
}

// parseFile is the parallel-safe per-file work: read, parse, query,
// and resolve imports / re-exports. It does not touch the Project; the
// caller hands the result to mergeParseResult.
func parseFile(e *extractor.Extractor, projectRoot, relPath, absPath string) *parseResult {
	res := &parseResult{relPath: relPath}

	source, err := os.ReadFile(absPath)
	if err != nil {
		res.err = fmt.Errorf("read %s: %w", relPath, err)
		return res
	}
	res.source = source

	tree, err := e.Parse(source)
	if err != nil {
		res.err = fmt.Errorf("parse %s: %w", relPath, err)
		return res
	}
	res.tree = tree

	root := tree.RootNode()
	fr := &FileResult{Path: relPath}
	res.fr = fr

	if v, err := e.QueryImports(root, source); err != nil {
		res.err = fmt.Errorf("imports in %s: %w", relPath, err)
		return res
	} else {
		fr.Imports = v
	}
	if v, err := e.QueryReExports(root, source); err != nil {
		res.err = fmt.Errorf("re-exports in %s: %w", relPath, err)
		return res
	} else {
		fr.ReExports = v
	}
	if v, err := e.QueryFunctions(root, source); err != nil {
		res.err = fmt.Errorf("functions in %s: %w", relPath, err)
		return res
	} else {
		fr.Functions = v
	}
	if v, err := e.QueryClasses(root, source); err != nil {
		res.err = fmt.Errorf("classes in %s: %w", relPath, err)
		return res
	} else {
		fr.Classes = v
	}
	if v, err := e.QueryInterfaces(root, source); err != nil {
		res.err = fmt.Errorf("interfaces in %s: %w", relPath, err)
		return res
	} else {
		fr.Interfaces = v
	}
	if v, err := e.QueryEnums(root, source); err != nil {
		res.err = fmt.Errorf("enums in %s: %w", relPath, err)
		return res
	} else {
		fr.Enums = v
	}
	if v, err := e.QueryTypeAliases(root, source); err != nil {
		res.err = fmt.Errorf("type aliases in %s: %w", relPath, err)
		return res
	} else {
		fr.TypeAliases = v
	}

	if v, err := e.QueryFunctionCalls(root, source); err != nil {
		res.err = fmt.Errorf("calls in %s: %w", relPath, err)
		return res
	} else {
		res.callContexts = v
	}

	if v, err := e.QueryDefaultExports(root, source); err != nil {
		res.err = fmt.Errorf("default exports in %s: %w", relPath, err)
		return res
	} else {
		res.defaultNames = v
	}

	res.resolvedImps = resolveImportEdges(e, fr.Imports, projectRoot, relPath, absPath)
	res.resolvedReExps = resolveReExportEdges(e, fr.ReExports, projectRoot, relPath, absPath)
	return res
}

// resolveImportEdges turns ImportContexts into already-resolved
// ImportEdges. Resolver is thread-safe (CachingFS uses a mutex) so
// this runs inside a worker.
func resolveImportEdges(e *extractor.Extractor, imports []extractor.ImportContext, projectRoot, relPath, absPath string) []ImportEdge {
	out := make([]ImportEdge, 0, len(imports))
	for _, imp := range imports {
		edge := ImportEdge{
			File:        relPath,
			Path:        imp.Path,
			Kind:        imp.Kind,
			Namespace:   imp.Namespace,
			Identifiers: imp.Identifiers,
		}
		if imp.Node != nil {
			edge.StartByte = imp.Node.StartByte()
			edge.EndByte = imp.Node.EndByte()
		}
		if e.Resolver != nil {
			if resolved, err := e.Resolver.Resolve(imp.Path, absPath); err == nil {
				if rel, err := filepath.Rel(projectRoot, resolved); err == nil {
					edge.Resolved = filepath.ToSlash(rel)
				}
			}
		}
		out = append(out, edge)
	}
	return out
}

// resolveReExportEdges does the same for re-exports.
func resolveReExportEdges(e *extractor.Extractor, reExports []extractor.ReExportContext, projectRoot, relPath, absPath string) []ReExportEdge {
	out := make([]ReExportEdge, 0, len(reExports))
	for _, re := range reExports {
		edge := ReExportEdge{
			File:      relPath,
			Path:      re.Path,
			Kind:      re.Kind,
			Namespace: re.Namespace,
		}
		for _, b := range re.Bindings {
			edge.Bindings = append(edge.Bindings, ReExportBinding{
				LocalName:  b.LocalName,
				RemoteName: b.RemoteName,
				IsTypeOnly: b.IsTypeOnly,
			})
		}
		if re.Node != nil {
			edge.StartByte = re.Node.StartByte()
			edge.EndByte = re.Node.EndByte()
		}
		if e.Resolver != nil {
			if resolved, err := e.Resolver.Resolve(re.Path, absPath); err == nil {
				if rel, err := filepath.Rel(projectRoot, resolved); err == nil {
					edge.Resolved = filepath.ToSlash(rel)
				}
			}
		}
		out = append(out, edge)
	}
	return out
}

// mergeParseResult folds one parse phase output into the shared
// project. Must be called serially across files because it mutates
// project.Symbols / Calls / Imports / ReExports / trees / Files.
func mergeParseResult(project *Project, res *parseResult) {
	if project.trees == nil {
		project.trees = map[string]*sitter.Tree{}
	}
	if old, ok := project.trees[res.relPath]; ok && old != nil {
		old.Close()
	}
	project.trees[res.relPath] = res.tree
	project.Files[res.relPath] = res.fr

	fnRanges := collectFunctionSymbols(project, res.fr, res.relPath, res.source)
	moduleID := res.relPath + "#module"
	needsModule := false
	for _, c := range res.callContexts {
		if c.Node == nil {
			continue
		}
		callerID := findEnclosingFunction(fnRanges, c.Node.StartByte(), c.Node.EndByte())
		if callerID == "" {
			callerID = moduleID
			needsModule = true
		}
		project.Calls = append(project.Calls, CallSite{
			CallerID:      callerID,
			Callee:        c.Name,
			Receiver:      c.Receiver,
			Expression:    c.Expression,
			IsConstructor: c.IsConstructor,
			File:          res.relPath,
			StartByte:     c.Node.StartByte(),
			EndByte:       c.Node.EndByte(),
		})
	}
	if needsModule {
		project.Symbols = append(project.Symbols, Symbol{
			ID:        moduleID,
			Kind:      SymbolModule,
			Name:      res.relPath,
			File:      res.relPath,
			StartByte: 0,
			EndByte:   res.tree.RootNode().EndByte(),
		})
	}

	collectKindSymbols(project, res.fr, res.relPath)
	project.Imports = append(project.Imports, res.resolvedImps...)
	project.ReExports = append(project.ReExports, res.resolvedReExps...)

	markDefaultsByName(project, res.relPath, res.defaultNames)
}

// markDefaultsByName flips IsDefaultExport on every same-file Symbol
// whose name is in names; appends a Project warning when more than
// one symbol ends up flagged.
func markDefaultsByName(project *Project, relPath string, names []string) {
	if len(names) == 0 {
		return
	}
	wanted := make(map[string]bool, len(names))
	for _, n := range names {
		wanted[n] = true
	}
	flagged := 0
	for i := range project.Symbols {
		s := &project.Symbols[i]
		if s.File != relPath {
			continue
		}
		if wanted[s.Name] {
			s.IsDefaultExport = true
			flagged++
		}
	}
	if flagged > 1 {
		project.Warnings = append(project.Warnings,
			fmt.Sprintf("%s: %d symbols flagged as default export; the resolver will pick the first encountered", relPath, flagged))
	}
}

// processFile composes parseFile + mergeParseResult for the
// incremental UpdateFiles path, which works one file at a time.
func processFile(e *extractor.Extractor, project *Project, relPath, absPath string) error {
	res := parseFile(e, project.Root, relPath, absPath)
	if res.err != nil {
		if res.tree != nil {
			res.tree.Close()
		}
		return res.err
	}
	mergeParseResult(project, res)
	return nil
}

// collectFunctionSymbols turns each FunctionContext into a Symbol and
// returns the function byte ranges so call-site attribution can
// later pair each call with its enclosing function.
//
// Method symbols are extended backward through any preceding
// decorator siblings so `@memoize compute()` is one contiguous range.
//
// Each symbol's LocalTypes is populated from `const x = new T(...)`
// and `const x: T = ...` declarations inside the body so the
// resolver can later type-check method calls on those variables.
func collectFunctionSymbols(project *Project, fr *FileResult, relPath string, source []byte) []functionRange {
	var ranges []functionRange
	for _, fn := range fr.Functions {
		if fn.Node == nil {
			continue
		}
		startByte := fn.Node.StartByte()
		if fn.Node.Kind() == "method_definition" {
			startByte = expandToPrecedingDecorators(fn.Node)
		}
		sym := Symbol{
			ID:         symbolID(relPath, startByte),
			Kind:       SymbolFunction,
			Name:       fn.Name,
			File:       relPath,
			StartByte:  startByte,
			EndByte:    fn.Node.EndByte(),
			ReturnType: fn.ReturnType,
		}
		if fn.BodyNode != nil {
			sym.BodyStartByte = fn.BodyNode.StartByte()
			sym.LocalTypes, sym.LocalCallBindings, sym.LocalMethodBindings, sym.LocalDestructureBindings, sym.LocalTypeOrigins = extractLocalTypes(fn.BodyNode, source)
			if sym.ReturnType == "" {
				if inferred, props := inferReturnShape(fn.BodyNode, source); inferred != "" || len(props) > 0 {
					sym.ReturnType = inferred
					sym.InlineReturnProperties = props
				}
			}
		}
		project.Symbols = append(project.Symbols, sym)
		ranges = append(ranges, functionRange{
			start:    sym.StartByte,
			end:      sym.EndByte,
			symbolID: sym.ID,
		})
	}
	return ranges
}

// extractLocalTypes walks body for variable declarations that bind
// a local name to a type the resolver can use later. Returns five
// maps:
//
//   - types: name → class name, from `const x: T = ...` and
//     `const x = new T(...)`.
//   - callBindings: name → callee identifier, for
//     `const x = factory(...)` and the awaited form. Resolved later
//     by ResolveCalls once each function's ReturnType is known.
//   - methodBindings: name → (receiver, method), for
//     `const x = recv.method(...)` and the awaited form. Iterated in
//     ResolveCalls so receivers resolved mid-pass unlock further
//     bindings.
//   - destructureBindings: name → (source call, property), for
//     `const { a, b } = factory()` and friends. Resolved later via
//     ClassDetails/InterfaceDetails property lookup.
//   - origins: name → TypeOrigin for entries that types was populated
//     with directly (annotation or new).
//
// Skips nested function bodies. Any map may be returned nil.
func extractLocalTypes(body *sitter.Node, source []byte) (
	types map[string]string,
	callBindings map[string]string,
	methodBindings map[string]LocalMethodTarget,
	destructureBindings map[string]LocalDestructureSource,
	origins map[string]TypeOrigin,
) {
	if body == nil {
		return nil, nil, nil, nil, nil
	}
	types = map[string]string{}
	callBindings = map[string]string{}
	methodBindings = map[string]LocalMethodTarget{}
	destructureBindings = map[string]LocalDestructureSource{}
	origins = map[string]TypeOrigin{}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "function_declaration", "method_definition", "arrow_function", "function_expression", "generator_function", "generator_function_declaration":
			// Don't descend into nested function scopes.
			return
		case "variable_declarator":
			if name, typ, origin := localTypeFromDeclarator(n, source); name != "" && typ != "" {
				types[name] = typ
				origins[name] = origin
				return
			}
			if name, callee := localCallBindingFromDeclarator(n, source); name != "" && callee != "" {
				callBindings[name] = callee
				return
			}
			if name, target := localMethodBindingFromDeclarator(n, source); name != "" && target.Method != "" {
				methodBindings[name] = target
				return
			}
			extractDestructureBindings(n, source, destructureBindings)
		}
		for i, count := uint(0), n.NamedChildCount(); i < count; i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(body)
	if len(types) == 0 {
		types = nil
	}
	if len(callBindings) == 0 {
		callBindings = nil
	}
	if len(methodBindings) == 0 {
		methodBindings = nil
	}
	if len(destructureBindings) == 0 {
		destructureBindings = nil
	}
	if len(origins) == 0 {
		origins = nil
	}
	return types, callBindings, methodBindings, destructureBindings, origins
}

// localTypeFromDeclarator extracts (localName, className, origin)
// from a variable_declarator bound via `const x: T = ...` or
// `const x = new T(...)`. Returns empty strings for any other shape.
func localTypeFromDeclarator(decl *sitter.Node, source []byte) (string, string, TypeOrigin) {
	nameNode := decl.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "identifier" {
		return "", "", ""
	}
	localName := nameNode.Utf8Text(source)

	if t := decl.ChildByFieldName("type"); t != nil && t.Kind() == "type_annotation" && t.NamedChildCount() > 0 {
		inner := t.NamedChild(0)
		if inner != nil && inner.Kind() == "type_identifier" {
			return localName, inner.Utf8Text(source), OriginAnnotation
		}
	}

	if v := decl.ChildByFieldName("value"); v != nil && v.Kind() == "new_expression" {
		if c := v.ChildByFieldName("constructor"); c != nil && c.Kind() == "identifier" {
			return localName, c.Utf8Text(source), OriginInferredNew
		}
	}
	return "", "", ""
}

// localCallBindingFromDeclarator extracts (localName, calleeName)
// from a variable_declarator initialized by a bare-identifier
// function call (`const x = fn(...)`) or an awaited one
// (`const x = await fn(...)`). Method-on-receiver calls (`obj.m()`)
// fall through to localMethodBindingFromDeclarator. Returns empty
// strings for any other shape.
func localCallBindingFromDeclarator(decl *sitter.Node, source []byte) (string, string) {
	nameNode := decl.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "identifier" {
		return "", ""
	}
	value := unwrapAwait(decl.ChildByFieldName("value"))
	if value == nil || value.Kind() != "call_expression" {
		return "", ""
	}
	fn := value.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "identifier" {
		return "", ""
	}
	return nameNode.Utf8Text(source), fn.Utf8Text(source)
}

// localMethodBindingFromDeclarator extracts (localName, target)
// from a variable_declarator initialized by a method-on-identifier
// call (`const x = recv.method(...)`) or the awaited form. Only
// single-identifier receivers are recognized — deeper expressions
// (`const x = a.b.c()`) stay out of scope.
func localMethodBindingFromDeclarator(decl *sitter.Node, source []byte) (string, LocalMethodTarget) {
	nameNode := decl.ChildByFieldName("name")
	if nameNode == nil || nameNode.Kind() != "identifier" {
		return "", LocalMethodTarget{}
	}
	value := unwrapAwait(decl.ChildByFieldName("value"))
	if value == nil || value.Kind() != "call_expression" {
		return "", LocalMethodTarget{}
	}
	fn := value.ChildByFieldName("function")
	if fn == nil || fn.Kind() != "member_expression" {
		return "", LocalMethodTarget{}
	}
	recv := fn.ChildByFieldName("object")
	method := fn.ChildByFieldName("property")
	if recv == nil || recv.Kind() != "identifier" || method == nil {
		return "", LocalMethodTarget{}
	}
	return nameNode.Utf8Text(source), LocalMethodTarget{
		Receiver: recv.Utf8Text(source),
		Method:   method.Utf8Text(source),
	}
}

// inferReturnShape walks body for top-level `return` statements
// (skipping nested function scopes) and infers either a single
// class name (when every return is `return new T(...)` with the
// same T) or a per-property source map (when there's exactly one
// `return { ... }` literal). Returns ("", nil) when nothing
// matches or returns are inconsistent.
func inferReturnShape(body *sitter.Node, source []byte) (string, map[string]InlineReturnSource) {
	if body == nil {
		return "", nil
	}
	var newTypes []string
	var objs []map[string]InlineReturnSource
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		switch n.Kind() {
		case "function_declaration", "method_definition", "arrow_function", "function_expression", "generator_function", "generator_function_declaration":
			return
		case "return_statement":
			if n.NamedChildCount() == 0 {
				return
			}
			value := unwrapAwait(n.NamedChild(0))
			if value == nil {
				return
			}
			switch value.Kind() {
			case "new_expression":
				if c := value.ChildByFieldName("constructor"); c != nil && c.Kind() == "identifier" {
					newTypes = append(newTypes, c.Utf8Text(source))
				}
			case "object":
				if m := extractObjectLiteralSources(value, source); len(m) > 0 {
					objs = append(objs, m)
				}
			}
			return
		}
		for i, count := uint(0), n.NamedChildCount(); i < count; i++ {
			walk(n.NamedChild(i))
		}
	}
	walk(body)

	if len(newTypes) > 0 && len(objs) == 0 {
		first := newTypes[0]
		consistent := true
		for _, t := range newTypes[1:] {
			if t != first {
				consistent = false
				break
			}
		}
		if consistent {
			return first, nil
		}
	}
	if len(objs) == 1 && len(newTypes) == 0 {
		return "", objs[0]
	}
	return "", nil
}

// extractObjectLiteralSources walks an `object` node (used as a
// return value) and captures per-property source info for shapes
// the resolver can later type-check: `{ x }` shorthand, `{ x: new
// T() }`, and `{ x: localVar }`. Other rhs shapes are silently
// skipped (the property simply won't gain inferred type).
func extractObjectLiteralSources(obj *sitter.Node, source []byte) map[string]InlineReturnSource {
	out := map[string]InlineReturnSource{}
	for i, n := uint(0), obj.NamedChildCount(); i < n; i++ {
		child := obj.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "shorthand_property_identifier":
			name := child.Utf8Text(source)
			out[name] = InlineReturnSource{LocalVar: name}
		case "pair":
			key := child.ChildByFieldName("key")
			val := child.ChildByFieldName("value")
			if key == nil || val == nil {
				continue
			}
			var keyText string
			switch key.Kind() {
			case "property_identifier", "identifier":
				keyText = key.Utf8Text(source)
			default:
				continue
			}
			switch val.Kind() {
			case "new_expression":
				if c := val.ChildByFieldName("constructor"); c != nil && c.Kind() == "identifier" {
					out[keyText] = InlineReturnSource{NewType: c.Utf8Text(source)}
				}
			case "identifier":
				out[keyText] = InlineReturnSource{LocalVar: val.Utf8Text(source)}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// unwrapAwait peels one `await ...` layer off a value node, leaving
// `null` initializers and non-await nodes untouched.
func unwrapAwait(value *sitter.Node) *sitter.Node {
	if value == nil {
		return nil
	}
	if value.Kind() == "await_expression" && value.NamedChildCount() > 0 {
		return value.NamedChild(0)
	}
	return value
}

// extractDestructureBindings handles `const { a, b: alias } =
// factory(...)` / `const { ctx } = await factory(...)` /
// `const { x } = receiver.method(...)`. For each destructured
// shorthand or pair pattern it records the source-call shape
// (Receiver, Callee) and the property name being destructured.
// Array patterns and rest elements are out of scope. Each entry
// shares the same source-call info but lists its own Property.
func extractDestructureBindings(decl *sitter.Node, source []byte, out map[string]LocalDestructureSource) {
	pattern := decl.ChildByFieldName("name")
	if pattern == nil || pattern.Kind() != "object_pattern" {
		return
	}
	value := unwrapAwait(decl.ChildByFieldName("value"))
	if value == nil || value.Kind() != "call_expression" {
		return
	}
	fn := value.ChildByFieldName("function")
	if fn == nil {
		return
	}
	var receiver, callee string
	switch fn.Kind() {
	case "identifier":
		callee = fn.Utf8Text(source)
	case "member_expression":
		recv := fn.ChildByFieldName("object")
		method := fn.ChildByFieldName("property")
		if recv == nil || recv.Kind() != "identifier" || method == nil {
			return
		}
		receiver = recv.Utf8Text(source)
		callee = method.Utf8Text(source)
	default:
		return
	}
	for i, n := uint(0), pattern.NamedChildCount(); i < n; i++ {
		entry := pattern.NamedChild(i)
		if entry == nil {
			continue
		}
		switch entry.Kind() {
		case "shorthand_property_identifier_pattern":
			name := entry.Utf8Text(source)
			out[name] = LocalDestructureSource{
				Receiver: receiver,
				Callee:   callee,
				Property: name,
			}
		case "pair_pattern":
			key := entry.ChildByFieldName("key")
			val := entry.ChildByFieldName("value")
			if key == nil || val == nil || val.Kind() != "identifier" {
				continue
			}
			keyText := ""
			switch key.Kind() {
			case "property_identifier", "identifier":
				keyText = key.Utf8Text(source)
			default:
				continue
			}
			out[val.Utf8Text(source)] = LocalDestructureSource{
				Receiver: receiver,
				Callee:   callee,
				Property: keyText,
			}
		}
	}
}

// expandToPrecedingDecorators returns the StartByte of the leftmost
// contiguous decorator preceding node among its parent's named
// children, or node.StartByte() when there are none.
func expandToPrecedingDecorators(node *sitter.Node) uint {
	if node == nil {
		return 0
	}
	parent := node.Parent()
	if parent == nil {
		return node.StartByte()
	}
	count := parent.NamedChildCount()
	idx := -1
	for i := uint(0); i < count; i++ {
		c := parent.NamedChild(i)
		if c == nil {
			continue
		}
		if c.StartByte() == node.StartByte() && c.EndByte() == node.EndByte() {
			idx = int(i)
			break
		}
	}
	if idx <= 0 {
		return node.StartByte()
	}
	start := node.StartByte()
	for i := idx - 1; i >= 0; i-- {
		sib := parent.NamedChild(uint(i))
		if sib == nil || sib.Kind() != "decorator" {
			break
		}
		start = sib.StartByte()
	}
	return start
}

// functionRange is one entry in the per-file index used to attribute
// call sites by enclosing function.
type functionRange struct {
	start, end uint
	symbolID   string
}

// findEnclosingFunction returns the symbol ID of the smallest
// function range containing [callStart, callEnd], or "" if none does.
// Smallest = innermost for nested declarations.
func findEnclosingFunction(ranges []functionRange, callStart, callEnd uint) string {
	bestID := ""
	bestSize := ^uint(0)
	for _, r := range ranges {
		if r.start <= callStart && callEnd <= r.end {
			size := r.end - r.start
			if size < bestSize {
				bestSize = size
				bestID = r.symbolID
			}
		}
	}
	return bestID
}

// collectKindSymbols emits Symbol entries for every non-function
// declaration kind. Class and interface symbols carry structured
// details for AST-free rendering; other kinds are name + byte range.
func collectKindSymbols(project *Project, fr *FileResult, relPath string) {
	for _, c := range fr.Classes {
		if c.Node == nil {
			continue
		}
		project.Symbols = append(project.Symbols, Symbol{
			ID:        symbolID(relPath, c.Node.StartByte()),
			Kind:      SymbolClass,
			Name:      c.Name,
			File:      relPath,
			StartByte: c.Node.StartByte(),
			EndByte:   c.Node.EndByte(),
			ClassDetails: &ClassDetails{
				Abstract:   c.Abstract,
				Extends:    c.Extends,
				Implements: c.Implements,
				Properties: c.Properties,
				Methods:    c.Methods,
			},
		})
	}
	for _, i := range fr.Interfaces {
		if i.Node == nil {
			continue
		}
		project.Symbols = append(project.Symbols, Symbol{
			ID:        symbolID(relPath, i.Node.StartByte()),
			Kind:      SymbolInterface,
			Name:      i.Name,
			File:      relPath,
			StartByte: i.Node.StartByte(),
			EndByte:   i.Node.EndByte(),
			InterfaceDetails: &InterfaceDetails{
				Extends:    i.Extends,
				Properties: i.Properties,
				Methods:    i.Methods,
			},
		})
	}
	for _, en := range fr.Enums {
		if en.Node == nil {
			continue
		}
		project.Symbols = append(project.Symbols, Symbol{
			ID:        symbolID(relPath, en.Node.StartByte()),
			Kind:      SymbolEnum,
			Name:      en.Name,
			File:      relPath,
			StartByte: en.Node.StartByte(),
			EndByte:   en.Node.EndByte(),
		})
	}
	for _, ta := range fr.TypeAliases {
		if ta.Node == nil {
			continue
		}
		project.Symbols = append(project.Symbols, Symbol{
			ID:        symbolID(relPath, ta.Node.StartByte()),
			Kind:      SymbolTypeAlias,
			Name:      ta.Name,
			File:      relPath,
			StartByte: ta.Node.StartByte(),
			EndByte:   ta.Node.EndByte(),
		})
	}
}

// symbolID is the canonical file-and-position-keyed Symbol identifier.
// Byte-keyed (rather than line/column) so whitespace edits don't shift
// IDs.
func symbolID(relPath string, startByte uint) string {
	return fmt.Sprintf("%s#%d", relPath, startByte)
}
