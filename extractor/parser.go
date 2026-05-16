package extractor

import (
	"fmt"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Parse turns source bytes into a tree-sitter parse tree. The returned
// tree owns C-side memory; the caller must call Tree.Close.
func (e *Extractor) Parse(source []byte) (*sitter.Tree, error) {
	parser := sitter.NewParser()
	if parser == nil {
		return nil, fmt.Errorf("failed to create parser")
	}
	defer parser.Close()

	if err := parser.SetLanguage(e.Language); err != nil {
		return nil, fmt.Errorf("failed to set parser language: %w", err)
	}

	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parser returned no tree")
	}
	return tree, nil
}
