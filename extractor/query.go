package extractor

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// runQuery executes a named query against node, invoking handle for
// each match. match.Captures is backed by the cursor's iteration
// buffer and must not be retained past the next match.
func (e *Extractor) runQuery(
	queryName string,
	node *sitter.Node,
	source []byte,
	handle func(captureNames []string, match *sitter.QueryMatch) error,
) error {
	query, ok := e.Queries[queryName]
	if !ok {
		return fmt.Errorf("query %q not loaded", queryName)
	}

	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	captureNames := query.CaptureNames()
	matches := cursor.Matches(query, node, source)

	for match := matches.Next(); match != nil; match = matches.Next() {
		if err := handle(captureNames, match); err != nil {
			return err
		}
	}
	return nil
}

// matchView indexes a single match's captures by capture name. Capture
// order is lost; iterate match.Captures directly when order matters.
type matchView struct {
	byName map[string][]sitter.Node
}

func newMatchView(captureNames []string, match *sitter.QueryMatch) *matchView {
	byName := make(map[string][]sitter.Node, len(match.Captures))
	for _, capture := range match.Captures {
		name := captureNames[capture.Index]
		byName[name] = append(byName[name], capture.Node)
	}
	return &matchView{byName: byName}
}

func (m *matchView) first(name string) (sitter.Node, bool) {
	nodes := m.byName[name]
	if len(nodes) == 0 {
		return sitter.Node{}, false
	}
	return nodes[0], true
}

func (m *matchView) text(name string, source []byte) string {
	node, ok := m.first(name)
	if !ok {
		return ""
	}
	return node.Utf8Text(source)
}

func nodeText(node *sitter.Node, source []byte) string {
	return node.Utf8Text(source)
}
