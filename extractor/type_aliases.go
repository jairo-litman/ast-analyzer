package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TypeAliasContext describes one type alias. Value holds the source
// text of the right-hand side.
type TypeAliasContext struct {
	Name  string       `json:"name"`
	Value string       `json:"value"`
	Node  *sitter.Node `json:"-"`
}

// QueryTypeAliases returns every type alias declaration in node, in
// source order.
func (e *Extractor) QueryTypeAliases(node *sitter.Node, source []byte) ([]TypeAliasContext, error) {
	var aliases []TypeAliasContext

	err := e.runQuery("type_alias", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)

		ta := TypeAliasContext{
			Name:  view.text("typeAliasName", source),
			Value: view.text("typeAliasValue", source),
		}
		if decl, ok := view.first("typeAlias"); ok {
			ta.Node = &decl
		}
		aliases = append(aliases, ta)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return aliases, nil
}
