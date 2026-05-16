package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// EnumContext describes one enum declaration.
type EnumContext struct {
	Name    string       `json:"name"`
	Members []string     `json:"members"`
	Node    *sitter.Node `json:"-"`
}

// QueryEnums returns every enum declaration in node, in source order.
func (e *Extractor) QueryEnums(node *sitter.Node, source []byte) ([]EnumContext, error) {
	var enums []EnumContext

	err := e.runQuery("enum", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)

		ec := EnumContext{Name: view.text("enumName", source)}
		if decl, ok := view.first("enum"); ok {
			ec.Node = &decl
		}
		if body, ok := view.first("enumBody"); ok {
			ec.Members = walkEnumBody(&body, source)
		}
		enums = append(enums, ec)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return enums, nil
}

// walkEnumBody returns the member names of an enum_body. Valued
// members are wrapped in enum_assignment; unvalued ones appear as
// direct name children.
func walkEnumBody(body *sitter.Node, source []byte) []string {
	var members []string
	for i, count := uint(0), body.NamedChildCount(); i < count; i++ {
		child := body.NamedChild(i)
		if child == nil {
			continue
		}
		var name string
		if child.Kind() == "enum_assignment" {
			if n := child.ChildByFieldName("name"); n != nil {
				name = n.Utf8Text(source)
			}
		} else {
			name = child.Utf8Text(source)
		}
		if name != "" {
			members = append(members, name)
		}
	}
	return members
}
