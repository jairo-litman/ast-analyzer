package extractor

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ImportKind classifies the role an import plays at the module level.
type ImportKind string

const (
	KindValue      ImportKind = "value"       // Standard runtime import.
	KindType       ImportKind = "type"        // `import type ...`.
	KindSideEffect ImportKind = "side-effect" // `import "./styles.css"`.
)

// ImportContext describes a single import or require statement. Node
// references the raw AST and is omitted from JSON output.
type ImportContext struct {
	Path string     `json:"path"`
	Kind ImportKind `json:"kind"`

	// Namespace is set for `import * as x from "..."`.
	Namespace string `json:"namespace,omitempty"`

	// Identifiers lists named / default bindings introduced by this
	// statement, in source order.
	Identifiers []IdentifierContext `json:"identifiers,omitempty"`

	Node *sitter.Node `json:"-"`
}

// IdentifierContext is one binding pulled from an import clause.
type IdentifierContext struct {
	LocalName  string `json:"local_name"`   // Name used in this file (alias-aware).
	RemoteName string `json:"remote_name"`  // Name exported by the source module.
	IsTypeOnly bool   `json:"is_type_only"` // Set for inline `{ type Foo }`.
}

// QueryImports returns every import or require statement contained in
// node, in source order.
func (e *Extractor) QueryImports(node *sitter.Node, source []byte) ([]ImportContext, error) {
	var imports []ImportContext

	err := e.runQuery("import", node, source, func(captureNames []string, match *sitter.QueryMatch) error {
		imports = append(imports, extractImportContext(captureNames, match, source))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return imports, nil
}

// extractImportContext walks captures in source order: an
// `import.type` capture before any binding marks the whole statement
// as `import type ...`; after one, it's an inline `{ type Foo }`
// specifier.
func extractImportContext(captureNames []string, match *sitter.QueryMatch, source []byte) ImportContext {
	imp := ImportContext{Kind: KindValue}

	var pendingType, hasSymbols bool

	for _, capture := range match.Captures {
		name := captureNames[capture.Index]
		node := capture.Node

		switch name {
		case "import.statement":
			n := node
			imp.Node = &n
		case "import.source":
			imp.Path = nodeText(&node, source)
		case "import.type":
			if hasSymbols {
				pendingType = true
			} else {
				imp.Kind = KindType
			}
		case "import.default":
			hasSymbols = true
			imp.Identifiers = append(imp.Identifiers, IdentifierContext{
				LocalName:  nodeText(&node, source),
				RemoteName: "default",
				IsTypeOnly: pendingType,
			})
			pendingType = false
		case "import.named":
			hasSymbols = true
			text := nodeText(&node, source)
			imp.Identifiers = append(imp.Identifiers, IdentifierContext{
				LocalName:  text,
				RemoteName: text,
				IsTypeOnly: pendingType,
			})
			pendingType = false
		case "import.alias":
			if n := len(imp.Identifiers); n > 0 {
				imp.Identifiers[n-1].LocalName = nodeText(&node, source)
			}
		case "import.namespace":
			hasSymbols = true
			imp.Namespace = nodeText(&node, source)
		}
	}

	if !hasSymbols && imp.Namespace == "" {
		imp.Kind = KindSideEffect
	}
	return imp
}
