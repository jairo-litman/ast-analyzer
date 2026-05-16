package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// FunctionCallContext describes one call site.
//
// Name is the leaf identifier being invoked when statically
// resolvable; empty otherwise. Receiver is the source text of the
// expression to the left of the final property access, or — when the
// callee isn't an identifier or member expression — the full callee
// text. Expression is the entire call source including arguments.
// IsConstructor is set for `new T(...)`.
type FunctionCallContext struct {
	Name          string       `json:"name"`
	Receiver      string       `json:"receiver,omitempty"`
	Expression    string       `json:"expression"`
	IsConstructor bool         `json:"is_constructor,omitempty"`
	Node          *sitter.Node `json:"-"`
}

// QueryFunctionCalls returns every call_expression and new_expression
// in node, in source order. Chained and nested calls each produce
// their own entry.
func (e *Extractor) QueryFunctionCalls(node *sitter.Node, source []byte) ([]FunctionCallContext, error) {
	var calls []FunctionCallContext

	err := e.runQuery("function_call", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)

		callNode, ok := view.first("call")
		if !ok {
			return nil
		}

		fc := FunctionCallContext{
			Expression: callNode.Utf8Text(source),
			Node:       &callNode,
		}

		// new_expression carries the callee under `constructor`;
		// call_expression carries it under `function`.
		var target *sitter.Node
		if callNode.Kind() == "new_expression" {
			fc.IsConstructor = true
			target = callNode.ChildByFieldName("constructor")
		} else {
			target = callNode.ChildByFieldName("function")
		}
		if target != nil {
			fc.Name, fc.Receiver = extractCallTarget(target, source)
		}
		calls = append(calls, fc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return calls, nil
}

// extractCallTarget returns the leaf name being invoked and any
// receiver expression. Identifiers, member expressions, and
// parenthesized variants resolve to a static name; everything else
// (subscript, call-on-call, type casts) leaves name empty and puts
// the full callee text in receiver.
func extractCallTarget(funcNode *sitter.Node, source []byte) (name, receiver string) {
	switch funcNode.Kind() {
	case "identifier":
		return funcNode.Utf8Text(source), ""

	case "member_expression":
		if prop := funcNode.ChildByFieldName("property"); prop != nil {
			name = prop.Utf8Text(source)
		}
		if obj := funcNode.ChildByFieldName("object"); obj != nil {
			receiver = obj.Utf8Text(source)
		}
		return

	case "parenthesized_expression":
		inner := funcNode.NamedChild(0)
		if inner != nil {
			switch inner.Kind() {
			case "identifier", "member_expression":
				return extractCallTarget(inner, source)
			}
		}
	}

	return "", funcNode.Utf8Text(source)
}
