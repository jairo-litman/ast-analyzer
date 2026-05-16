package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// InterfaceContext describes one interface declaration.
type InterfaceContext struct {
	Name       string            `json:"name"`
	Extends    []string          `json:"extends,omitempty"`
	Properties []ObjectProperty  `json:"properties,omitempty"`
	Methods    []MethodSignature `json:"methods,omitempty"`
	Node       *sitter.Node      `json:"-"`
}

// QueryInterfaces returns every interface declaration in node, in
// source order.
func (e *Extractor) QueryInterfaces(node *sitter.Node, source []byte) ([]InterfaceContext, error) {
	var interfaces []InterfaceContext

	err := e.runQuery("interface", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)

		ic := InterfaceContext{Name: view.text("interfaceName", source)}
		if decl, ok := view.first("interface"); ok {
			ic.Node = &decl
		}
		for _, n := range view.byName["interfaceExtends"] {
			ic.Extends = append(ic.Extends, n.Utf8Text(source))
		}
		if body, ok := view.first("interfaceBody"); ok {
			ic.Properties, ic.Methods = walkObjectBody(&body, source)
		}
		interfaces = append(interfaces, ic)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return interfaces, nil
}
