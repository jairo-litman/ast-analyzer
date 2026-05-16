package graph

import (
	"fmt"
	"sort"
)

// populateTypeRefs fills Symbol.TypeRefs on every kind and resolves
// each ref's BaseName to project Symbol IDs. Runs after ResolveCalls
// so LocalTypes and LocalTypeOrigins are fully enriched.
func populateTypeRefs(ctx *resolveContext) {
	ctx.symbolsByID = buildSymbolByIDIndex(ctx.p)

	for i := range ctx.p.Symbols {
		sym := &ctx.p.Symbols[i]
		refs := collectSymbolTypeRefs(sym, ctx.p)
		for j := range refs {
			ctx.resolveTypeRefTargets(&refs[j].Ref, sym.File)
		}
		if len(refs) == 0 {
			sym.TypeRefs = nil
			continue
		}
		sym.TypeRefs = refs
	}
}

// collectSymbolTypeRefs builds the unresolved SymbolTypeRef slice
// for sym, sorted by Slot.
func collectSymbolTypeRefs(sym *Symbol, p *Project) []SymbolTypeRef {
	var out []SymbolTypeRef

	switch sym.Kind {
	case SymbolFunction:
		out = appendFunctionTypeRefs(out, sym, p)
	case SymbolClass:
		out = appendClassTypeRefs(out, sym)
	case SymbolInterface:
		out = appendInterfaceTypeRefs(out, sym)
	case SymbolTypeAlias:
		out = appendTypeAliasTypeRefs(out, sym, p)
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Slot < out[j].Slot
	})
	return out
}

// appendFunctionTypeRefs emits one ref per typed parameter, one for
// the return type, and one per LocalTypes entry.
func appendFunctionTypeRefs(out []SymbolTypeRef, sym *Symbol, p *Project) []SymbolTypeRef {
	params := lookupFunctionParameters(p, sym)
	for _, param := range params {
		if param.Type == "" {
			continue
		}
		ref := ParseTypeRef(param.Type, OriginAnnotation)
		if ref == nil {
			continue
		}
		out = append(out, SymbolTypeRef{
			Slot: "param:" + param.Name,
			Ref:  *ref,
		})
	}

	if sym.ReturnType != "" {
		ref := ParseTypeRef(sym.ReturnType, returnTypeOrigin(sym))
		if ref != nil {
			out = append(out, SymbolTypeRef{Slot: "return", Ref: *ref})
		}
	}

	for name, typ := range sym.LocalTypes {
		ref := ParseTypeRef(typ, localOrigin(sym, name))
		if ref == nil {
			continue
		}
		out = append(out, SymbolTypeRef{
			Slot: "local:" + name,
			Ref:  *ref,
		})
	}

	return out
}

// returnTypeOrigin returns OriginInferredObjectLit when the function
// has an inline object-literal return shape; otherwise the ReturnType
// is either an explicit annotation or a new-expression inference and
// both have already been copied into sym.ReturnType verbatim, so the
// distinction can't be recovered here and defaults to annotation.
func returnTypeOrigin(sym *Symbol) TypeOrigin {
	if len(sym.InlineReturnProperties) > 0 {
		return OriginInferredObjectLit
	}
	return OriginAnnotation
}

// localOrigin reads sym.LocalTypeOrigins for name, defaulting to
// OriginInferredNew when the entry predates origin tracking.
func localOrigin(sym *Symbol, name string) TypeOrigin {
	if origin, ok := sym.LocalTypeOrigins[name]; ok && origin != "" {
		return origin
	}
	return OriginInferredNew
}

// lookupFunctionParameters returns the FunctionParameter slice for
// sym. Match by EndByte because method symbols may expand StartByte
// backward to cover preceding decorators.
func lookupFunctionParameters(p *Project, sym *Symbol) []ParameterShape {
	fr, ok := p.Files[sym.File]
	if !ok {
		return nil
	}
	for _, fn := range fr.Functions {
		if fn.Node == nil || fn.Node.EndByte() != sym.EndByte {
			continue
		}
		out := make([]ParameterShape, 0, len(fn.Parameters))
		for _, param := range fn.Parameters {
			out = append(out, ParameterShape{Name: param.Name, Type: param.Type})
		}
		return out
	}
	return nil
}

// ParameterShape decouples populateTypeRefs from the extractor's
// FunctionParameter type.
type ParameterShape struct {
	Name string
	Type string
}

// appendClassTypeRefs emits refs for extends, each implements entry,
// and each typed property.
func appendClassTypeRefs(out []SymbolTypeRef, sym *Symbol) []SymbolTypeRef {
	d := sym.ClassDetails
	if d == nil {
		return out
	}
	if d.Extends != "" {
		if ref := ParseTypeRef(d.Extends, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{Slot: "extends", Ref: *ref})
		}
	}
	for i, impl := range d.Implements {
		if ref := ParseTypeRef(impl, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{
				Slot: fmt.Sprintf("implements:%d", i),
				Ref:  *ref,
			})
		}
	}
	for _, prop := range d.Properties {
		if prop.Type == "" {
			continue
		}
		if ref := ParseTypeRef(prop.Type, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{
				Slot: "property:" + prop.Name,
				Ref:  *ref,
			})
		}
	}
	return out
}

// appendInterfaceTypeRefs emits refs for each extends entry and
// each typed property.
func appendInterfaceTypeRefs(out []SymbolTypeRef, sym *Symbol) []SymbolTypeRef {
	d := sym.InterfaceDetails
	if d == nil {
		return out
	}
	for i, ext := range d.Extends {
		if ref := ParseTypeRef(ext, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{
				Slot: fmt.Sprintf("extends:%d", i),
				Ref:  *ref,
			})
		}
	}
	for _, prop := range d.Properties {
		if prop.Type == "" {
			continue
		}
		if ref := ParseTypeRef(prop.Type, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{
				Slot: "property:" + prop.Name,
				Ref:  *ref,
			})
		}
	}
	return out
}

// appendTypeAliasTypeRefs emits one "value" ref parsed from the
// alias RHS.
func appendTypeAliasTypeRefs(out []SymbolTypeRef, sym *Symbol, p *Project) []SymbolTypeRef {
	fr, ok := p.Files[sym.File]
	if !ok {
		return out
	}
	for _, ta := range fr.TypeAliases {
		if ta.Node == nil || ta.Node.StartByte() != sym.StartByte {
			continue
		}
		if ref := ParseTypeRef(ta.Value, OriginAnnotation); ref != nil {
			out = append(out, SymbolTypeRef{Slot: "value", Ref: *ref})
		}
		break
	}
	return out
}

// resolveTypeRefTargets fills Targets on every BaseName-bearing node
// with the IDs of project Symbols matching that name in fromFile's
// scope. Tries class, interface, type_alias, enum; records all that
// resolve. Primitives are skipped.
func (ctx *resolveContext) resolveTypeRefTargets(ref *TypeRef, fromFile string) {
	ref.WalkBaseTypes(func(node *TypeRef) {
		if node.IsPrimitive() {
			return
		}
		seen := map[string]bool{}
		var targets []string
		for _, kind := range []SymbolKind{SymbolClass, SymbolInterface, SymbolTypeAlias, SymbolEnum} {
			if s, ok := ctx.findSymbolByNameAndKind(fromFile, node.BaseName, kind); ok {
				if !seen[s.ID] {
					seen[s.ID] = true
					targets = append(targets, s.ID)
				}
			}
		}
		if len(targets) > 0 {
			node.Targets = targets
		}
	})
}
