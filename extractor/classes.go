package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ClassContext describes one class declaration.
type ClassContext struct {
	Name       string            `json:"name"`
	Abstract   bool              `json:"abstract,omitempty"`
	Extends    string            `json:"extends,omitempty"`
	Implements []string          `json:"implements,omitempty"`
	Properties []ObjectProperty  `json:"properties,omitempty"`
	Methods    []MethodSignature `json:"methods,omitempty"`
	Node       *sitter.Node      `json:"-"`
}

// QueryClasses returns every concrete or abstract class declaration
// in node, in source order.
func (e *Extractor) QueryClasses(node *sitter.Node, source []byte) ([]ClassContext, error) {
	var classes []ClassContext

	err := e.runQuery("class", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)

		cc := ClassContext{
			Name:    view.text("className", source),
			Extends: view.text("classExtends", source),
		}
		if decl, ok := view.first("class"); ok {
			cc.Node = &decl
		} else if decl, ok := view.first("abstractClass"); ok {
			cc.Node = &decl
			cc.Abstract = true
		}
		for _, n := range view.byName["classImplements"] {
			cc.Implements = append(cc.Implements, n.Utf8Text(source))
		}
		if body, ok := view.first("classBody"); ok {
			cc.Properties, cc.Methods = walkObjectBody(&body, source)
		}
		classes = append(classes, cc)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return classes, nil
}
