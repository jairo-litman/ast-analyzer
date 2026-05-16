package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// QueryDefaultExports returns the names of every `export default
// <name>` and `export default <decl>` form. Anonymous default exports
// (`export default function() {}`, `export default 42`) are skipped —
// they have no name to anchor a Symbol to.
func (e *Extractor) QueryDefaultExports(node *sitter.Node, source []byte) ([]string, error) {
	var names []string

	err := e.runQuery("default_export", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		view := newMatchView(captureNames, match)
		if name := view.text("default.name", source); name != "" {
			names = append(names, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}
