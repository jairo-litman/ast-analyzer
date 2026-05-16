package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractImports parses source and runs QueryImports. The Node
// field is stripped from each result so test cases can compare
// against literal ImportContext values.
func extractImports(t *testing.T, e *Extractor, source string) []ImportContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryImports(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryImports(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []ImportContext
	}{
		{
			name:   "default import",
			source: `import foo from "./bar";`,
			want: []ImportContext{{
				Path: "./bar",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "foo", RemoteName: "default"},
				},
			}},
		},
		{
			name:   "named import",
			source: `import { bar } from "./baz";`,
			want: []ImportContext{{
				Path: "./baz",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "bar", RemoteName: "bar"},
				},
			}},
		},
		{
			name:   "named import with alias",
			source: `import { qux as quux } from "./qux";`,
			want: []ImportContext{{
				Path: "./qux",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "quux", RemoteName: "qux"},
				},
			}},
		},
		{
			name:   "namespace import",
			source: `import * as baz from "./baz";`,
			want: []ImportContext{{
				Path:      "./baz",
				Kind:      KindValue,
				Namespace: "baz",
			}},
		},
		{
			name:   "side-effect import",
			source: `import "./styles.css";`,
			want: []ImportContext{{
				Path: "./styles.css",
				Kind: KindSideEffect,
			}},
		},
		{
			name:   "top-level type import",
			source: `import type { TypeA } from "./types";`,
			want: []ImportContext{{
				Path: "./types",
				Kind: KindType,
				Identifiers: []IdentifierContext{
					{LocalName: "TypeA", RemoteName: "TypeA"},
				},
			}},
		},
		{
			name:   "top-level type namespace import",
			source: `import type * as Types from "./types";`,
			want: []ImportContext{{
				Path:      "./types",
				Kind:      KindType,
				Namespace: "Types",
			}},
		},
		{
			name:   "top-level type import with alias",
			source: `import type { TypeB as TypeC } from "./types";`,
			want: []ImportContext{{
				Path: "./types",
				Kind: KindType,
				Identifiers: []IdentifierContext{
					{LocalName: "TypeC", RemoteName: "TypeB"},
				},
			}},
		},
		{
			name:   "explicit default via named clause",
			source: `import { default as DefaultExport } from "./mod";`,
			want: []ImportContext{{
				Path: "./mod",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "DefaultExport", RemoteName: "default"},
				},
			}},
		},
		{
			name:   "mixed default, named, alias and inline type",
			source: `import a, { b, c as d, type e, type f as g } from "./mod";`,
			want: []ImportContext{{
				Path: "./mod",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "a", RemoteName: "default"},
					{LocalName: "b", RemoteName: "b"},
					{LocalName: "d", RemoteName: "c"},
					{LocalName: "e", RemoteName: "e", IsTypeOnly: true},
					{LocalName: "g", RemoteName: "f", IsTypeOnly: true},
				},
			}},
		},
		{
			name:   "import attributes are tolerated",
			source: `import data from "./data.json" with { type: "json" };`,
			want: []ImportContext{{
				Path: "./data.json",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "data", RemoteName: "default"},
				},
			}},
		},
		{
			name:   "CommonJS require",
			source: `const fs = require("fs");`,
			want: []ImportContext{{
				Path: "fs",
				Kind: KindValue,
				Identifiers: []IdentifierContext{
					{LocalName: "fs", RemoteName: "default"},
				},
			}},
		},
		{
			name:   "multiple statements preserve source order",
			source: "import a from \"./a\";\nimport { b } from \"./b\";",
			want: []ImportContext{
				{
					Path: "./a", Kind: KindValue,
					Identifiers: []IdentifierContext{{LocalName: "a", RemoteName: "default"}},
				},
				{
					Path: "./b", Kind: KindValue,
					Identifiers: []IdentifierContext{{LocalName: "b", RemoteName: "b"}},
				},
			},
		},
		{
			name:   "no imports",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractImports(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestImports_PopulatesNode verifies the AST handle is populated on
// the returned ImportContext.
func TestImports_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`import foo from "./bar";`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryImports(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node, "import.statement capture should populate Node")
	assert.Equal(t, "import_statement", got[0].Node.Kind())
}
