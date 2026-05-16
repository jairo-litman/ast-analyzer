package graph

import "strings"

// TypeOrigin tags the rule (annotation or inference) that produced
// a TypeRef.
type TypeOrigin string

const (
	OriginAnnotation           TypeOrigin = "annotation"
	OriginInferredNew          TypeOrigin = "inferred:new"
	OriginInferredThis         TypeOrigin = "inferred:this"
	OriginInferredAwait        TypeOrigin = "inferred:await"
	OriginInferredObjectLit    TypeOrigin = "inferred:object-literal"
	OriginInferredCallReturn   TypeOrigin = "inferred:call-return"
	OriginInferredMethodReturn TypeOrigin = "inferred:method-return"
	OriginInferredDestructure  TypeOrigin = "inferred:destructure"
)

// TypeRef is a parsed type expression linked to the project graph.
// Exactly one of BaseName, Union, or Intersection is populated.
// Targets is filled by the resolver with the IDs of Symbols whose
// name matches BaseName in scope.
type TypeRef struct {
	Raw          string     `json:"raw"`
	BaseName     string     `json:"base_name,omitempty"`
	TypeArgs     []TypeRef  `json:"type_args,omitempty"`
	Union        []TypeRef  `json:"union,omitempty"`
	Intersection []TypeRef  `json:"intersection,omitempty"`
	IsArray      bool       `json:"is_array,omitempty"`
	Origin       TypeOrigin `json:"origin,omitempty"`
	Targets      []string   `json:"targets,omitempty"`
}

// tsPrimitives lists the TS built-ins that have no project-level
// declaration to resolve against.
var tsPrimitives = map[string]bool{
	"string": true, "number": true, "boolean": true, "void": true,
	"any": true, "unknown": true, "never": true,
	"null": true, "undefined": true,
	"object": true, "symbol": true, "bigint": true,
	"this": true,
}

// IsPrimitive reports whether BaseName is a TS built-in.
func (r *TypeRef) IsPrimitive() bool {
	if r == nil || r.BaseName == "" {
		return false
	}
	return tsPrimitives[r.BaseName]
}

// WalkBaseTypes visits every node with a non-empty BaseName in
// depth-first, left-to-right order.
func (r *TypeRef) WalkBaseTypes(fn func(*TypeRef)) {
	if r == nil {
		return
	}
	if r.BaseName != "" {
		fn(r)
	}
	for i := range r.TypeArgs {
		r.TypeArgs[i].WalkBaseTypes(fn)
	}
	for i := range r.Union {
		r.Union[i].WalkBaseTypes(fn)
	}
	for i := range r.Intersection {
		r.Intersection[i].WalkBaseTypes(fn)
	}
}

// ParseTypeRef parses a TS type annotation into a TypeRef tree.
// Returns nil for empty input. Object-literal and function-type
// expressions degrade to a Raw-only ref with empty BaseName.
func ParseTypeRef(raw string, origin TypeOrigin) *TypeRef {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	ref := parseTypeExpression(trimmed)
	ref.Raw = trimmed
	ref.Origin = origin
	return ref
}

// parseTypeExpression splits the outermost union / intersection
// before delegating to parseApplication for the leaf form.
func parseTypeExpression(s string) *TypeRef {
	s = strings.TrimSpace(s)
	if s == "" {
		return &TypeRef{}
	}

	if parts := splitTopLevel(s, '|'); len(parts) > 1 {
		ref := &TypeRef{}
		for _, p := range parts {
			child := parseTypeExpression(p)
			child.Raw = strings.TrimSpace(p)
			ref.Union = append(ref.Union, *child)
		}
		return ref
	}

	if parts := splitTopLevel(s, '&'); len(parts) > 1 {
		ref := &TypeRef{}
		for _, p := range parts {
			child := parseTypeExpression(p)
			child.Raw = strings.TrimSpace(p)
			ref.Intersection = append(ref.Intersection, *child)
		}
		return ref
	}

	return parseApplication(s)
}

// parseApplication parses an identifier optionally followed by
// `<T, ...>` and/or trailing `[]`.
func parseApplication(s string) *TypeRef {
	s = strings.TrimSpace(s)

	isArray := false
	for strings.HasSuffix(s, "[]") {
		isArray = true
		s = strings.TrimSpace(strings.TrimSuffix(s, "[]"))
	}

	ref := &TypeRef{IsArray: isArray}

	if lt := indexTopLevel(s, '<'); lt >= 0 {
		if !strings.HasSuffix(s, ">") {
			return ref
		}
		ref.BaseName = strings.TrimSpace(s[:lt])
		inner := s[lt+1 : len(s)-1]
		for _, arg := range splitTopLevel(inner, ',') {
			child := parseTypeExpression(arg)
			child.Raw = strings.TrimSpace(arg)
			ref.TypeArgs = append(ref.TypeArgs, *child)
		}
		return ref
	}

	if looksLikeIdentifier(s) {
		ref.BaseName = s
	}
	return ref
}

// splitTopLevel splits s on sep at bracket-depth zero.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<', '(', '[', '{':
			depth++
		case '>', ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	if len(out) == 1 {
		return out
	}
	return out
}

// indexTopLevel reports the first index of sep at depth zero, or -1.
func indexTopLevel(s string, sep byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case '<':
			if s[i] == sep && depth == 0 {
				return i
			}
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if s[i] == sep && depth == 0 {
				return i
			}
		}
	}
	return -1
}

// looksLikeIdentifier reports whether s is a (possibly dotted) TS
// identifier.
func looksLikeIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == '_' || b == '$' || b == '.':
		case b >= 'a' && b <= 'z':
		case b >= 'A' && b <= 'Z':
		case b >= '0' && b <= '9':
		default:
			return false
		}
	}
	return true
}
