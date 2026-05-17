package graph

import (
	"strings"

	"github.com/jairo-litman/ast-analyzer/extractor"
)

// ResolveCalls populates each CallSite.ResolvedTo with the IDs of
// the Symbol(s) it statically targets. Unresolvable calls keep an
// empty ResolvedTo. Also runs populateTypeRefs at the end.
func ResolveCalls(p *Project) {
	ctx := newResolveContext(p)
	ctx.enrichLocalTypesFromBindings()
	// symbolsByID holds copies of p.Symbols; the enrichment mutated
	// the slice in place, so the lookup map needs a refresh before
	// call resolution reads LocalTypes through it.
	ctx.symbolsByID = buildSymbolByIDIndex(p)
	for i := range p.Calls {
		p.Calls[i].ResolvedTo = ctx.resolveCall(&p.Calls[i])
	}
	populateTypeRefs(ctx)
}

// enrichLocalTypesFromBindings resolves LocalCallBindings,
// LocalMethodBindings, and LocalDestructureBindings into LocalTypes
// entries, iterating to a fixed point so chained bindings converge.
//
// symbolsByID is rebuilt at the start of each iteration because
// recordLocalType may have assigned a fresh map to sym.LocalTypes;
// the index's value copies need to be refreshed to see it.
func (ctx *resolveContext) enrichLocalTypesFromBindings() {
	for changed := true; changed; {
		ctx.symbolsByID = buildSymbolByIDIndex(ctx.p)
		changed = false
		for i := range ctx.p.Symbols {
			sym := &ctx.p.Symbols[i]

			for local, callee := range sym.LocalCallBindings {
				if _, already := sym.LocalTypes[local]; already {
					continue
				}
				retType := ctx.lookupFunctionReturnType(sym.File, callee)
				if retType == "" {
					continue
				}
				if ctx.recordLocalType(sym, local, retType, OriginInferredCallReturn) {
					changed = true
				}
			}

			for local, target := range sym.LocalMethodBindings {
				if _, already := sym.LocalTypes[local]; already {
					continue
				}
				retType := ctx.lookupMethodReturnTypeOnLocal(*sym, target.Receiver, target.Method)
				if retType == "" {
					continue
				}
				if ctx.recordLocalType(sym, local, retType, OriginInferredMethodReturn) {
					changed = true
				}
			}

			for local, src := range sym.LocalDestructureBindings {
				if _, already := sym.LocalTypes[local]; already {
					continue
				}
				propType := ctx.lookupDestructuredPropertyType(*sym, src)
				if propType == "" {
					continue
				}
				if ctx.recordLocalType(sym, local, propType, OriginInferredDestructure) {
					changed = true
				}
			}
		}
	}
}

// lookupDestructuredPropertyType returns the type of src.Property,
// reading it off the source function's declared return-type class
// or interface, or its InlineReturnProperties when un-annotated.
func (ctx *resolveContext) lookupDestructuredPropertyType(caller Symbol, src LocalDestructureSource) string {
	var sourceFn *Symbol
	if src.Receiver == "" {
		sourceFn = ctx.lookupFunctionInScope(caller.File, src.Callee)
	} else {
		sourceFn = ctx.lookupMethodOnLocal(caller, src.Receiver, src.Callee)
	}
	if sourceFn == nil {
		return ""
	}
	if sourceFn.ReturnType != "" {
		baseType := stripGenericArgs(sourceFn.ReturnType)
		if baseType != "" {
			if classSym, ok := ctx.findClassByName(sourceFn.File, baseType); ok {
				for _, cls := range ctx.collectClassChain(classSym) {
					if cls.ClassDetails == nil {
						continue
					}
					for _, p := range cls.ClassDetails.Properties {
						if p.Name == src.Property {
							return p.Type
						}
					}
				}
			}
			if ifaceSym, ok := ctx.findInterfaceByName(sourceFn.File, baseType); ok {
				if t := propertyTypeFromInterfaceChain(ctx, ifaceSym, src.Property); t != "" {
					return t
				}
			}
		}
	}
	if inline, ok := sourceFn.InlineReturnProperties[src.Property]; ok {
		switch {
		case inline.NewType != "":
			return inline.NewType
		case inline.LocalVar != "":
			if t, ok := sourceFn.LocalTypes[inline.LocalVar]; ok {
				return t
			}
		}
	}
	return ""
}

// lookupFunctionInScope resolves a bare callee identifier in file's
// scope (same-file declaration, then named/aliased imports) to its
// function symbol. Returns nil when the name doesn't bind to a
// function reachable from file.
func (ctx *resolveContext) lookupFunctionInScope(file, callee string) *Symbol {
	pick := func(ids []string) *Symbol {
		for _, id := range ids {
			s, ok := ctx.symbolsByID[id]
			if !ok || s.Kind != SymbolFunction {
				continue
			}
			sym := s
			return &sym
		}
		return nil
	}
	if s := pick(ctx.symbolsByFile[file][callee]); s != nil {
		return s
	}
	if sc := ctx.scopes[file]; sc != nil {
		if b, ok := sc.aliases[callee]; ok {
			return pick(ctx.symbolsByFile[b.file][b.remote])
		}
	}
	return nil
}

// lookupMethodOnLocal returns the method symbol named methodName on
// the class bound to recvName in caller's LocalTypes.
func (ctx *resolveContext) lookupMethodOnLocal(caller Symbol, recvName, methodName string) *Symbol {
	className, ok := caller.LocalTypes[recvName]
	if !ok {
		return nil
	}
	classSym, ok := ctx.findClassByName(caller.File, className)
	if !ok {
		return nil
	}
	for _, cls := range ctx.collectClassChain(classSym) {
		for _, m := range ctx.findMethodInClass(cls, methodName) {
			s, ok := ctx.symbolsByID[m]
			if !ok {
				continue
			}
			sym := s
			return &sym
		}
	}
	return nil
}

// propertyTypeFromInterfaceChain walks an interface's extends chain
// looking for a property named prop, returning its declared type.
func propertyTypeFromInterfaceChain(ctx *resolveContext, iface Symbol, prop string) string {
	visited := map[string]bool{iface.ID: true}
	queue := []Symbol{iface}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur.InterfaceDetails == nil {
			continue
		}
		for _, p := range cur.InterfaceDetails.Properties {
			if p.Name == prop {
				return p.Type
			}
		}
		for _, parentName := range cur.InterfaceDetails.Extends {
			parent, ok := ctx.findInterfaceByName(cur.File, parentName)
			if !ok || visited[parent.ID] {
				continue
			}
			visited[parent.ID] = true
			queue = append(queue, parent)
		}
	}
	return ""
}

// recordLocalType stores retType (generics stripped) under local in
// sym.LocalTypes and tags the origin. Returns false when stripping
// yields nothing usable.
func (ctx *resolveContext) recordLocalType(sym *Symbol, local, retType string, origin TypeOrigin) bool {
	baseType := stripGenericArgs(retType)
	if baseType == "" {
		return false
	}
	if sym.LocalTypes == nil {
		sym.LocalTypes = map[string]string{}
	}
	sym.LocalTypes[local] = baseType
	if sym.LocalTypeOrigins == nil {
		sym.LocalTypeOrigins = map[string]TypeOrigin{}
	}
	sym.LocalTypeOrigins[local] = origin
	return true
}

// lookupMethodReturnTypeOnLocal returns the ReturnType of the method
// named methodName on the class bound to recvName in caller's
// LocalTypes.
func (ctx *resolveContext) lookupMethodReturnTypeOnLocal(caller Symbol, recvName, methodName string) string {
	className, ok := caller.LocalTypes[recvName]
	if !ok {
		return ""
	}
	classSym, ok := ctx.findClassByName(caller.File, className)
	if !ok {
		return ""
	}
	for _, cls := range ctx.collectClassChain(classSym) {
		for _, m := range ctx.findMethodInClass(cls, methodName) {
			s, ok := ctx.symbolsByID[m]
			if ok && s.ReturnType != "" {
				return s.ReturnType
			}
		}
	}
	return ""
}

// lookupFunctionReturnType resolves a bare callee identifier in
// file's scope (same-file declaration, then named/aliased imports)
// to a function symbol and returns its ReturnType. Returns "" when
// the callee isn't a function or has no annotation.
func (ctx *resolveContext) lookupFunctionReturnType(file, callee string) string {
	pick := func(ids []string) string {
		for _, id := range ids {
			s, ok := ctx.symbolsByID[id]
			if !ok || s.Kind != SymbolFunction {
				continue
			}
			if s.ReturnType != "" {
				return s.ReturnType
			}
		}
		return ""
	}
	if rt := pick(ctx.symbolsByFile[file][callee]); rt != "" {
		return rt
	}
	if sc := ctx.scopes[file]; sc != nil {
		if b, ok := sc.aliases[callee]; ok {
			return pick(ctx.symbolsByFile[b.file][b.remote])
		}
	}
	return ""
}

// resolveContext holds the lookup tables shared by per-call helpers
// during a single ResolveCalls run.
type resolveContext struct {
	p             *Project
	symbolsByFile symbolIndex
	symbolsByID   map[string]Symbol
	scopes        map[string]*scope
}

func newResolveContext(p *Project) *resolveContext {
	idx := buildSymbolIndex(p)
	return &resolveContext{
		p:             p,
		symbolsByFile: idx,
		symbolsByID:   buildSymbolByIDIndex(p),
		scopes:        buildScopes(p, idx),
	}
}

// symbolIndex is file → name → symbol IDs. Names legitimately collide
// (e.g. a function and class sharing an identifier), so the value is
// a slice.
type symbolIndex map[string]map[string][]string

// scope is the per-file resolution table built from imports. Local
// declarations are looked up directly off symbolIndex.
type scope struct {
	// namespaces maps an `import * as X` alias to the imported file.
	namespaces map[string]string

	// aliases maps an imported binding's local name to its remote
	// target — `import { add as plus }` records `plus → {add, ./helper}`.
	aliases map[string]importBinding
}

type importBinding struct {
	file   string
	remote string
}

func buildSymbolIndex(p *Project) symbolIndex {
	out := symbolIndex{}
	for _, s := range p.Symbols {
		if out[s.File] == nil {
			out[s.File] = map[string][]string{}
		}
		out[s.File][s.Name] = append(out[s.File][s.Name], s.ID)
	}
	return out
}

func buildSymbolByIDIndex(p *Project) map[string]Symbol {
	out := make(map[string]Symbol, len(p.Symbols))
	for _, s := range p.Symbols {
		out[s.ID] = s
	}
	return out
}

func buildScopes(p *Project, idx symbolIndex) map[string]*scope {
	defaults := buildDefaultExportIndex(p)
	reExports := buildReExportIndex(p)
	nsReExports := buildNamespaceReExportIndex(p)

	out := map[string]*scope{}
	ensure := func(file string) *scope {
		if out[file] == nil {
			out[file] = &scope{
				namespaces: map[string]string{},
				aliases:    map[string]importBinding{},
			}
		}
		return out[file]
	}
	for _, e := range p.Imports {
		// Type-only and unresolved imports never produce call edges.
		if e.Resolved == "" || e.Kind == extractor.KindType {
			continue
		}
		sc := ensure(e.File)
		if e.Namespace != "" {
			sc.namespaces[e.Namespace] = e.Resolved
			continue
		}
		for _, id := range e.Identifiers {
			if id.IsTypeOnly {
				continue
			}
			if id.RemoteName == "default" {
				if name, ok := defaults[e.Resolved]; ok {
					sc.aliases[id.LocalName] = importBinding{
						file:   e.Resolved,
						remote: name,
					}
				}
				continue
			}
			// Namespace re-exports (`export * as Foo from "./other"`)
			// turn the imported name into a namespace alias whose
			// member access resolves into the source module.
			if nsTargets, ok := nsReExports[e.Resolved]; ok {
				if target, ok := nsTargets[id.RemoteName]; ok {
					sc.namespaces[id.LocalName] = target
					continue
				}
			}
			// Try the direct target; otherwise walk its re-export chain.
			finalFile, finalName := e.Resolved, id.RemoteName
			if len(idx[finalFile][finalName]) == 0 {
				if rf, rn, ok := followReExports(idx, reExports, defaults, finalFile, finalName); ok {
					finalFile, finalName = rf, rn
				}
			}
			sc.aliases[id.LocalName] = importBinding{
				file:   finalFile,
				remote: finalName,
			}
		}
	}
	return out
}

// buildNamespaceReExportIndex maps each file's
// `export * as Name from "./other"` declarations as
// localName → resolved source-module path.
func buildNamespaceReExportIndex(p *Project) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, re := range p.ReExports {
		if re.Resolved == "" || re.Kind == extractor.KindType {
			continue
		}
		if re.Namespace == "" {
			continue
		}
		if out[re.File] == nil {
			out[re.File] = map[string]string{}
		}
		out[re.File][re.Namespace] = re.Resolved
	}
	return out
}

// buildReExportIndex groups every value-kind re-export by file for
// cheap chain-following lookups. Type-only re-exports are dropped.
func buildReExportIndex(p *Project) map[string][]ReExportEdge {
	out := map[string][]ReExportEdge{}
	for _, re := range p.ReExports {
		if re.Resolved == "" || re.Kind == extractor.KindType {
			continue
		}
		out[re.File] = append(out[re.File], re)
	}
	return out
}

// followReExports chases a re-export chain from (file, name) until it
// lands on a file that defines a same-named symbol. Cycle-safe via a
// (file, name) visited set. defaults maps each file to its default-
// exported symbol's name so `export { default as X } from ...` can
// follow through to the actual binding.
func followReExports(idx symbolIndex, reExports map[string][]ReExportEdge, defaults map[string]string, file, name string) (string, string, bool) {
	visited := map[string]bool{}
	return followReExportsHelper(idx, reExports, defaults, file, name, visited)
}

func followReExportsHelper(idx symbolIndex, reExports map[string][]ReExportEdge, defaults map[string]string, file, name string, visited map[string]bool) (string, string, bool) {
	key := file + ":" + name
	if visited[key] {
		return "", "", false
	}
	visited[key] = true

	if len(idx[file][name]) > 0 {
		return file, name, true
	}

	for _, re := range reExports[file] {
		// Named binding match: the re-export advertises name as either
		// its local or alias side.
		matched := false
		var nextRemote string
		for _, b := range re.Bindings {
			if b.IsTypeOnly {
				continue
			}
			if b.LocalName == name {
				matched = true
				nextRemote = b.RemoteName
				break
			}
		}
		if matched {
			// `export { default as X } from "./other"`: hop to the
			// source module's default-exported symbol name.
			if nextRemote == "default" {
				if defName, ok := defaults[re.Resolved]; ok {
					if rf, rn, ok := followReExportsHelper(idx, reExports, defaults, re.Resolved, defName, visited); ok {
						return rf, rn, true
					}
				}
				continue
			}
			if rf, rn, ok := followReExportsHelper(idx, reExports, defaults, re.Resolved, nextRemote, visited); ok {
				return rf, rn, true
			}
			continue
		}
		// Star re-export (empty bindings + empty namespace): try the
		// same name in the source module.
		if len(re.Bindings) == 0 && re.Namespace == "" {
			if rf, rn, ok := followReExportsHelper(idx, reExports, defaults, re.Resolved, name, visited); ok {
				return rf, rn, true
			}
		}
	}
	return "", "", false
}

// buildDefaultExportIndex maps a file path to its IsDefaultExport
// symbol's name. Each file legally has at most one; for malformed
// inputs with multiple, the first encountered wins.
func buildDefaultExportIndex(p *Project) map[string]string {
	out := map[string]string{}
	for _, s := range p.Symbols {
		if !s.IsDefaultExport {
			continue
		}
		if _, exists := out[s.File]; exists {
			continue
		}
		out[s.File] = s.Name
	}
	return out
}

func (ctx *resolveContext) resolveCall(call *CallSite) []string {
	// Bare `super(...)` (callee="", receiver="super") is the parent
	// class's constructor invocation — handle before the empty-callee
	// rejection below.
	if call.Callee == "" && call.Receiver == "super" {
		return ctx.resolveSuperConstructorCall(call)
	}
	if call.Callee == "" {
		return nil
	}

	switch call.Receiver {
	case "":
		// Direct identifier: prefer same-file declarations, then
		// imported bindings.
		if ids := ctx.symbolsByFile[call.File][call.Callee]; len(ids) > 0 {
			return copyIDs(ids)
		}
		if sc := ctx.scopes[call.File]; sc != nil {
			if b, ok := sc.aliases[call.Callee]; ok {
				if ids := ctx.symbolsByFile[b.file][b.remote]; len(ids) > 0 {
					return copyIDs(ids)
				}
			}
		}
		return nil

	case "this":
		return ctx.resolveThisCall(call)

	case "super":
		return ctx.resolveSuperCall(call)
	}

	// `this.<seg1>.<seg2>...<segN>.<method>()` — walk a chain of
	// typed class fields starting from the enclosing class. Each
	// hop is a property lookup against the current class's chain;
	// the property's declared type names the next class.
	if strings.HasPrefix(call.Receiver, "this.") {
		path := strings.Split(call.Receiver[len("this."):], ".")
		if ids := ctx.resolveThisFieldCall(call, path); len(ids) > 0 {
			return ids
		}
	}

	// Member call: try (1) namespace alias from imports, then (2) a
	// local variable whose type was tracked in the caller's body,
	// then (3) a static-style call where the receiver names a class
	// in scope (same-file or imported).
	if sc := ctx.scopes[call.File]; sc != nil {
		if target, ok := sc.namespaces[call.Receiver]; ok {
			if ids := ctx.symbolsByFile[target][call.Callee]; len(ids) > 0 {
				return copyIDs(ids)
			}
		}
	}
	if ids := ctx.resolveLocalInstanceCall(call); len(ids) > 0 {
		return ids
	}
	if ids := ctx.resolveStaticMethodCall(call); len(ids) > 0 {
		return ids
	}
	return nil
}

// resolveStaticMethodCall handles `ClassName.method()` when
// ClassName resolves to a class in call.File's scope. Lookup is by
// method name; the static/instance distinction isn't captured by
// the extractor.
func (ctx *resolveContext) resolveStaticMethodCall(call *CallSite) []string {
	classSym, ok := ctx.findClassByName(call.File, call.Receiver)
	if !ok {
		return nil
	}
	for _, cls := range ctx.collectClassChain(classSym) {
		if matches := ctx.findMethodInClass(cls, call.Callee); len(matches) > 0 {
			return matches
		}
	}
	return nil
}

// resolveLocalInstanceCall handles `localVar.method()` when localVar
// was declared in the caller's body via `new T(...)` or an explicit
// `: T` annotation. The class chain (and its interface fallback) is
// walked the same way `this.method()` resolves.
func (ctx *resolveContext) resolveLocalInstanceCall(call *CallSite) []string {
	caller, ok := ctx.symbolsByID[call.CallerID]
	if !ok || len(caller.LocalTypes) == 0 {
		return nil
	}
	className, ok := caller.LocalTypes[call.Receiver]
	if !ok {
		return nil
	}
	classSym, ok := ctx.findClassByName(caller.File, className)
	if !ok {
		return nil
	}
	chain := ctx.collectClassChain(classSym)
	for _, cls := range chain {
		if matches := ctx.findMethodInClass(cls, call.Callee); len(matches) > 0 {
			return matches
		}
	}
	return ctx.findMethodInInterfaceChain(chain, call.Callee)
}

// resolveThisCall walks the enclosing class's extends chain looking
// for a method named call.Callee. First match wins (TS override
// semantics). Falls back to the interface chain when no class in the
// chain implements it.
func (ctx *resolveContext) resolveThisCall(call *CallSite) []string {
	classSym, ok := ctx.findEnclosingClassOfCall(call)
	if !ok {
		return nil
	}
	chain := ctx.collectClassChain(classSym)
	for _, cls := range chain {
		if matches := ctx.findMethodInClass(cls, call.Callee); len(matches) > 0 {
			return matches
		}
	}
	return ctx.findMethodInInterfaceChain(chain, call.Callee)
}

// resolveThisFieldCall handles `this.<a>.<b>.<method>()`. Walks the
// path of typed properties through the enclosing class's chain
// (including constructor-parameter shorthand), then resolves the
// method against the final class's chain with interface fallback.
func (ctx *resolveContext) resolveThisFieldCall(call *CallSite, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	classSym, ok := ctx.findEnclosingClassOfCall(call)
	if !ok {
		return nil
	}
	currentClass := classSym
	for _, seg := range path {
		propType, propFile := ctx.lookupPropertyTypeInClassChain(currentClass, seg)
		if propType == "" {
			return nil
		}
		baseType := stripGenericArgs(propType)
		if baseType == "" {
			return nil
		}
		// The type identifier is scoped to the file that DECLARES
		// the property (which knows its own imports), not the file
		// that CONTAINS the call site.
		next, ok := ctx.findClassByName(propFile, baseType)
		if !ok {
			return nil
		}
		currentClass = next
	}
	chain := ctx.collectClassChain(currentClass)
	for _, cls := range chain {
		if matches := ctx.findMethodInClass(cls, call.Callee); len(matches) > 0 {
			return matches
		}
	}
	return ctx.findMethodInInterfaceChain(chain, call.Callee)
}

// lookupPropertyTypeInClassChain returns the declared type and the
// file of the first matching property found by walking classSym's
// class chain (own → extends → extends → …). Returns "", "" when
// no class in the chain declares the property.
func (ctx *resolveContext) lookupPropertyTypeInClassChain(classSym Symbol, propName string) (propType, propFile string) {
	for _, cls := range ctx.collectClassChain(classSym) {
		if cls.ClassDetails == nil {
			continue
		}
		for _, p := range cls.ClassDetails.Properties {
			if p.Name == propName {
				return p.Type, cls.File
			}
		}
	}
	return "", ""
}

// stripGenericArgs reduces a TypeScript type annotation to a single
// class-or-interface identifier. Peels `Promise<T>` wrappers
// recursively so awaited async returns resolve to the inner type,
// then drops any remaining generic arguments.
func stripGenericArgs(typ string) string {
	typ = strings.TrimSpace(typ)
	for strings.HasPrefix(typ, "Promise<") && strings.HasSuffix(typ, ">") {
		typ = strings.TrimSpace(typ[len("Promise<") : len(typ)-1])
	}
	if i := strings.IndexByte(typ, '<'); i >= 0 {
		return strings.TrimSpace(typ[:i])
	}
	return typ
}

// resolveSuperConstructorCall handles the bare `super(...)` form:
// resolves to the constructor of the enclosing class's parent, or
// the nearest ancestor that declares one (JS allows skipping levels
// when intermediate classes have no explicit constructor).
func (ctx *resolveContext) resolveSuperConstructorCall(call *CallSite) []string {
	classSym, ok := ctx.findEnclosingClassOfCall(call)
	if !ok {
		return nil
	}
	if classSym.ClassDetails == nil || classSym.ClassDetails.Extends == "" {
		return nil
	}
	parent, ok := ctx.findClassByName(classSym.File, classSym.ClassDetails.Extends)
	if !ok {
		return nil
	}
	for _, cls := range ctx.collectClassChain(parent) {
		if matches := ctx.findMethodInClass(cls, "constructor"); len(matches) > 0 {
			return matches
		}
	}
	return nil
}

// resolveSuperCall walks from the enclosing class's parent. The
// interface fallback consults the parent's (and ancestors') implements
// — never the enclosing class's own.
func (ctx *resolveContext) resolveSuperCall(call *CallSite) []string {
	classSym, ok := ctx.findEnclosingClassOfCall(call)
	if !ok {
		return nil
	}
	if classSym.ClassDetails == nil || classSym.ClassDetails.Extends == "" {
		return nil
	}
	parent, ok := ctx.findClassByName(classSym.File, classSym.ClassDetails.Extends)
	if !ok {
		return nil
	}
	chain := ctx.collectClassChain(parent)
	for _, cls := range chain {
		if matches := ctx.findMethodInClass(cls, call.Callee); len(matches) > 0 {
			return matches
		}
	}
	return ctx.findMethodInInterfaceChain(chain, call.Callee)
}

// findEnclosingClassOfCall returns the class symbol containing the
// call's caller method. Returns (Symbol{}, false) when the caller
// isn't a known symbol or has no enclosing class.
func (ctx *resolveContext) findEnclosingClassOfCall(call *CallSite) (Symbol, bool) {
	caller, ok := ctx.symbolsByID[call.CallerID]
	if !ok {
		return Symbol{}, false
	}
	return ctx.findEnclosingClass(caller)
}

// findEnclosingClass returns the smallest class symbol whose byte
// range contains sym's range, or (Symbol{}, false) if none.
func (ctx *resolveContext) findEnclosingClass(sym Symbol) (Symbol, bool) {
	var best Symbol
	bestSize := ^uint(0)
	found := false
	for _, c := range ctx.p.Symbols {
		if c.File != sym.File || c.Kind != SymbolClass {
			continue
		}
		if c.StartByte <= sym.StartByte && sym.EndByte <= c.EndByte {
			size := c.EndByte - c.StartByte
			if size < bestSize {
				bestSize = size
				best = c
				found = true
			}
		}
	}
	return best, found
}

// collectClassChain walks the extends chain from start, innermost
// first. Cycle-safe.
func (ctx *resolveContext) collectClassChain(start Symbol) []Symbol {
	var out []Symbol
	visited := map[string]bool{}
	cur := start
	for {
		if visited[cur.ID] {
			break
		}
		visited[cur.ID] = true
		out = append(out, cur)
		if cur.ClassDetails == nil || cur.ClassDetails.Extends == "" {
			break
		}
		parent, ok := ctx.findClassByName(cur.File, cur.ClassDetails.Extends)
		if !ok {
			break
		}
		cur = parent
	}
	return out
}

// findMethodInClass returns function-kind symbol IDs of methods
// named methodName declared on class.
func (ctx *resolveContext) findMethodInClass(class Symbol, methodName string) []string {
	var out []string
	for _, id := range ctx.symbolsByFile[class.File][methodName] {
		s, ok := ctx.symbolsByID[id]
		if !ok || s.Kind != SymbolFunction {
			continue
		}
		if class.StartByte <= s.StartByte && s.EndByte <= class.EndByte {
			out = append(out, id)
		}
	}
	return out
}

// findMethodInInterfaceChain walks all interfaces reachable from chain
// (via Implements + transitive Extends) and returns the IDs of those
// declaring methodName.
func (ctx *resolveContext) findMethodInInterfaceChain(chain []Symbol, methodName string) []string {
	visited := map[string]bool{}
	var queue []namedRef
	for _, c := range chain {
		if c.ClassDetails == nil {
			continue
		}
		for _, name := range c.ClassDetails.Implements {
			queue = append(queue, namedRef{file: c.File, name: name})
		}
	}

	var out []string
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]

		iface, ok := ctx.findInterfaceByName(ref.file, ref.name)
		if !ok || visited[iface.ID] {
			continue
		}
		visited[iface.ID] = true

		if iface.InterfaceDetails != nil {
			for _, m := range iface.InterfaceDetails.Methods {
				if m.Name == methodName {
					out = append(out, iface.ID)
					break
				}
			}
			for _, ext := range iface.InterfaceDetails.Extends {
				queue = append(queue, namedRef{file: iface.File, name: ext})
			}
		}
	}
	return out
}

// namedRef is a (file, name) pair used to defer symbol lookups during
// interface chain expansion.
type namedRef struct{ file, name string }

func (ctx *resolveContext) findClassByName(fromFile, name string) (Symbol, bool) {
	return ctx.findSymbolByNameAndKind(fromFile, name, SymbolClass)
}

func (ctx *resolveContext) findInterfaceByName(fromFile, name string) (Symbol, bool) {
	return ctx.findSymbolByNameAndKind(fromFile, name, SymbolInterface)
}

// findSymbolByNameAndKind looks up (name, kind) in this order:
// same-file declarations, file's import scope, then a project-wide
// uniqueness fallback (one match across the project, no ambiguity).
// The fallback covers cases where a type isn't imported into the
// caller's file but resolves unambiguously elsewhere.
func (ctx *resolveContext) findSymbolByNameAndKind(fromFile, name string, kind SymbolKind) (Symbol, bool) {
	for _, id := range ctx.symbolsByFile[fromFile][name] {
		if s, ok := ctx.symbolsByID[id]; ok && s.Kind == kind {
			return s, true
		}
	}
	if sc := ctx.scopes[fromFile]; sc != nil {
		if b, ok := sc.aliases[name]; ok {
			for _, id := range ctx.symbolsByFile[b.file][b.remote] {
				if s, ok := ctx.symbolsByID[id]; ok && s.Kind == kind {
					return s, true
				}
			}
		}
	}
	var unique Symbol
	matches := 0
	for _, s := range ctx.p.Symbols {
		if s.Kind != kind || s.Name != name {
			continue
		}
		unique = s
		matches++
		if matches > 1 {
			return Symbol{}, false
		}
	}
	if matches == 1 {
		return unique, true
	}
	return Symbol{}, false
}

// copyIDs returns a shallow copy so callers cannot mutate
// symbolIndex's slice.
func copyIDs(ids []string) []string {
	out := make([]string, len(ids))
	copy(out, ids)
	return out
}
