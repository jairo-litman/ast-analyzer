package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractTypeAliases(t *testing.T, e *Extractor, source string) []TypeAliasContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryTypeAliases(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryTypeAliases(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []TypeAliasContext
	}{
		{
			name:   "predefined type",
			source: "type ID = string;",
			want: []TypeAliasContext{{
				Name:  "ID",
				Value: "string",
			}},
		},
		{
			name:   "type identifier alias",
			source: "type Alias = OtherType;",
			want: []TypeAliasContext{{
				Name:  "Alias",
				Value: "OtherType",
			}},
		},
		{
			name:   "union type",
			source: "type Direction = \"north\" | \"south\";",
			want: []TypeAliasContext{{
				Name:  "Direction",
				Value: `"north" | "south"`,
			}},
		},
		{
			name:   "object type",
			source: "type Point = { x: number; y: number };",
			want: []TypeAliasContext{{
				Name:  "Point",
				Value: "{ x: number; y: number }",
			}},
		},
		{
			name:   "no type aliases",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTypeAliases(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTypeAliases_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`type T = string;`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryTypeAliases(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node)
	assert.Equal(t, "type_alias_declaration", got[0].Node.Kind())
}
