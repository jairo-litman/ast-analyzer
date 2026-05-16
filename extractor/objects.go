package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ObjectProperty is one name-typed member of a class or interface
// (e.g. `foo: number`).
type ObjectProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// MethodSignature is one class or interface method header. Method
// bodies are surfaced separately on FunctionContext.
type MethodSignature struct {
	Name       string              `json:"name"`
	Parameters []FunctionParameter `json:"parameters"`
	ReturnType string              `json:"return_type"`
}

// walkObjectProperty extracts a property from a public_field_definition
// or property_signature.
func walkObjectProperty(node *sitter.Node, source []byte) (ObjectProperty, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ObjectProperty{}, false
	}
	return ObjectProperty{
		Name: nameNode.Utf8Text(source),
		Type: typeAnnotationText(node.ChildByFieldName("type"), source),
	}, true
}

// walkObjectBody splits a class_body or interface_body's named
// children into properties and method signatures. Constructor
// parameter properties (TS-only shorthand:
// `constructor(private foo: Foo)`) are surfaced alongside explicit
// fields, since the type system treats them identically.
func walkObjectBody(body *sitter.Node, source []byte) ([]ObjectProperty, []MethodSignature) {
	var properties []ObjectProperty
	var methods []MethodSignature

	for i, count := uint(0), body.NamedChildCount(); i < count; i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "public_field_definition", "property_signature":
			if prop, ok := walkObjectProperty(child, source); ok {
				properties = append(properties, prop)
			}
		case "method_definition", "method_signature", "abstract_method_signature":
			if sig, ok := walkMethodSignature(child, source); ok {
				methods = append(methods, sig)
				if child.Kind() == "method_definition" && sig.Name == "constructor" {
					properties = append(properties, walkConstructorParameterProperties(child.ChildByFieldName("parameters"), source)...)
				}
			}
		}
	}
	return properties, methods
}

// walkConstructorParameterProperties scans a constructor's
// formal_parameters for TypeScript parameter properties — those
// with an accessibility modifier (`public`/`private`/`protected`)
// or `readonly`. These declare class fields by side effect, and
// the resolver needs them to follow `this.foo.bar()` calls.
func walkConstructorParameterProperties(params *sitter.Node, source []byte) []ObjectProperty {
	if params == nil {
		return nil
	}
	var out []ObjectProperty
	for i, count := uint(0), params.NamedChildCount(); i < count; i++ {
		child := params.NamedChild(i)
		if child == nil {
			continue
		}
		if !hasParameterPropertyModifier(child, source) {
			continue
		}
		nameNode := child.ChildByFieldName("pattern")
		if nameNode == nil || nameNode.Kind() != "identifier" {
			continue
		}
		out = append(out, ObjectProperty{
			Name: nameNode.Utf8Text(source),
			Type: typeAnnotationText(child.ChildByFieldName("type"), source),
		})
	}
	return out
}

// hasParameterPropertyModifier reports whether a formal-parameter
// node carries an accessibility modifier or a `readonly` keyword —
// the markers that turn a constructor parameter into a class field.
func hasParameterPropertyModifier(param *sitter.Node, source []byte) bool {
	for i, n := uint(0), param.NamedChildCount(); i < n; i++ {
		c := param.NamedChild(i)
		if c == nil {
			continue
		}
		switch c.Kind() {
		case "accessibility_modifier", "override_modifier":
			return true
		}
	}
	// `readonly` is a plain token in tree-sitter, not a named child.
	for i, n := uint(0), param.ChildCount(); i < n; i++ {
		c := param.Child(i)
		if c != nil && c.Utf8Text(source) == "readonly" {
			return true
		}
	}
	return false
}

// walkMethodSignature extracts a header from a method_definition,
// method_signature, or abstract_method_signature.
func walkMethodSignature(node *sitter.Node, source []byte) (MethodSignature, bool) {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return MethodSignature{}, false
	}
	return MethodSignature{
		Name:       nameNode.Utf8Text(source),
		Parameters: walkFormalParameters(node.ChildByFieldName("parameters"), source),
		ReturnType: typeAnnotationText(node.ChildByFieldName("return_type"), source),
	}, true
}

// typeAnnotationText returns the inner type's source text from a
// type_annotation node. Returns "" when the node is nil or empty.
func typeAnnotationText(node *sitter.Node, source []byte) string {
	if node == nil || node.Kind() != "type_annotation" || node.NamedChildCount() == 0 {
		return ""
	}
	inner := node.NamedChild(0)
	if inner == nil {
		return ""
	}
	return inner.Utf8Text(source)
}
