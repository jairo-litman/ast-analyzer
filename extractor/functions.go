package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// FunctionContext describes one function-like declaration. Node spans
// the whole declaration; BodyNode is the body subtree alone, suitable
// as a scope for sub-queries.
type FunctionContext struct {
	Name       string              `json:"name"`
	ReturnType string              `json:"return_type"`
	Body       string              `json:"body"`
	Parameters []FunctionParameter `json:"parameters"`
	Node       *sitter.Node        `json:"-"`
	BodyNode   *sitter.Node        `json:"-"`
}

// FunctionParameter describes one formal parameter. Type is empty when
// the source has no annotation.
type FunctionParameter struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// QueryFunctions returns every function-like declaration in node, in
// source order: function_declaration, method_definition, arrow
// functions bound via variable_declarator or class field, IIFE
// arrows, and any other anonymous arrow with a block body (test
// callbacks, inline closures). The generic anonymous-arrow capture
// overlaps with the named-bound and IIFE captures; matches sharing
// a start byte are deduped, preferring the named entry.
func (e *Extractor) QueryFunctions(node *sitter.Node, source []byte) ([]FunctionContext, error) {
	var functions []FunctionContext

	err := e.runQuery("function", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		fn, err := e.extractFunctionContext(captureNames, match, source)
		if err != nil {
			return err
		}
		functions = append(functions, fn)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return dedupeFunctionsByStartByte(functions), nil
}

// dedupeFunctionsByStartByte folds away duplicates from the
// catch-all anonymous-arrow capture. First filters generic entries
// whose parent is a variable_declarator or public_field_definition
// (those don't share a start byte with the named capture); then
// dedups remaining entries by start byte, preferring named over
// synthetic names.
func dedupeFunctionsByStartByte(functions []FunctionContext) []FunctionContext {
	out := functions[:0]
	for _, fn := range functions {
		if isOverlappingNamedBoundArrow(fn) {
			continue
		}
		out = append(out, fn)
	}

	seen := map[uint]int{}
	final := out[:0]
	for _, fn := range out {
		if fn.Node == nil {
			final = append(final, fn)
			continue
		}
		key := fn.Node.StartByte()
		idx, exists := seen[key]
		if !exists {
			seen[key] = len(final)
			final = append(final, fn)
			continue
		}
		if isSyntheticArrowName(final[idx].Name) && !isSyntheticArrowName(fn.Name) {
			final[idx] = fn
		}
	}
	return final
}

func isOverlappingNamedBoundArrow(fn FunctionContext) bool {
	if fn.Node == nil || fn.Node.Kind() != "arrow_function" || fn.Name != "(arrow)" {
		return false
	}
	parent := fn.Node.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "variable_declarator", "public_field_definition":
		return true
	}
	return false
}

func isSyntheticArrowName(name string) bool {
	return name == "" || name == "(arrow)" || name == "(iife)"
}

func (e *Extractor) extractFunctionContext(captureNames []string, match *sitter.QueryMatch, source []byte) (FunctionContext, error) {
	view := newMatchView(captureNames, match)

	fn := FunctionContext{
		Name:       view.text("funcName", source),
		ReturnType: view.text("funcReturnType", source),
	}
	if decl, ok := view.first("funcDecl"); ok {
		fn.Node = &decl
	}
	if body, ok := view.first("funcBody"); ok {
		fn.BodyNode = &body
		fn.Body = body.Utf8Text(source)
	}
	if params, ok := view.first("funcParams"); ok {
		fn.Parameters = walkFormalParameters(&params, source)
	}

	// Anonymous arrows: tag with a synthetic name so the symbol
	// catalog never carries empty names. IIFE form gets `(iife)`;
	// all other anonymous arrows get `(arrow)`.
	if fn.Name == "" && fn.Node != nil && fn.Node.Kind() == "arrow_function" {
		if parent := fn.Node.Parent(); parent != nil && parent.Kind() == "parenthesized_expression" {
			fn.Name = "(iife)"
		} else {
			fn.Name = "(arrow)"
		}
	}

	return fn, nil
}

// walkFormalParameters extracts identifier parameters and their
// optional type annotations from a formal_parameters node, or from a
// bare identifier (`x => x`). Destructuring patterns are skipped.
func walkFormalParameters(params *sitter.Node, source []byte) []FunctionParameter {
	if params == nil {
		return nil
	}

	if params.Kind() == "identifier" {
		return []FunctionParameter{{Name: params.Utf8Text(source)}}
	}

	count := params.NamedChildCount()
	var out []FunctionParameter

	for i := uint(0); i < count; i++ {
		child := params.NamedChild(i)
		if child == nil {
			continue
		}

		nameNode := child.ChildByFieldName("pattern")
		if nameNode == nil || nameNode.Kind() != "identifier" {
			continue
		}

		out = append(out, FunctionParameter{
			Name: nameNode.Utf8Text(source),
			Type: typeAnnotationText(child.ChildByFieldName("type"), source),
		})
	}
	return out
}
