package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ReExportContext describes one `export ... from "..."` statement.
// Variants are encoded by which fields are populated:
//
//   - Bindings non-empty  → `export { a, b as c } from "..."`
//   - Namespace non-empty → `export * as ns from "..."`
//   - both empty          → `export * from "..."`
//
// Kind == KindType marks `export type { ... } from "..."`; inline
// `type` modifiers on individual bindings set IsTypeOnly.
type ReExportContext struct {
	Path      string                   `json:"path"`
	Kind      ImportKind               `json:"kind"`
	Namespace string                   `json:"namespace,omitempty"`
	Bindings  []ReExportBindingContext `json:"bindings,omitempty"`

	Node *sitter.Node `json:"-"`
}

// ReExportBindingContext is one entry inside an `export { ... } from`
// clause. LocalName is the alias-aware name visible in the
// re-exporting file; RemoteName is the source module's export name.
type ReExportBindingContext struct {
	LocalName  string `json:"local_name"`
	RemoteName string `json:"remote_name"`
	IsTypeOnly bool   `json:"is_type_only"`
}

// QueryReExports returns every `export ... from "..."` statement in
// node, in source order.
func (e *Extractor) QueryReExports(node *sitter.Node, source []byte) ([]ReExportContext, error) {
	var out []ReExportContext

	err := e.runQuery("re_export", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		out = append(out, extractReExportContext(captureNames, match, source))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// extractReExportContext walks captures in source order so an inline
// `type` modifier pairs with the binding that follows it.
func extractReExportContext(captureNames []string, match *sitter.QueryMatch, source []byte) ReExportContext {
	re := ReExportContext{Kind: KindValue}
	var pendingType bool

	for _, capture := range match.Captures {
		name := captureNames[capture.Index]
		node := capture.Node

		switch name {
		case "reexport":
			n := node
			re.Node = &n
		case "reexport.source":
			re.Path = nodeText(&node, source)
		case "reexport.type":
			re.Kind = KindType
		case "reexport.specifier_type":
			pendingType = true
		case "reexport.name":
			text := nodeText(&node, source)
			re.Bindings = append(re.Bindings, ReExportBindingContext{
				LocalName:  text,
				RemoteName: text,
				IsTypeOnly: pendingType,
			})
			pendingType = false
		case "reexport.alias":
			if n := len(re.Bindings); n > 0 {
				re.Bindings[n-1].LocalName = nodeText(&node, source)
			}
		case "reexport.namespace":
			re.Namespace = nodeText(&node, source)
		}
	}
	return re
}
