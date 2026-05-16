package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractEnums(t *testing.T, e *Extractor, source string) []EnumContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryEnums(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryEnums(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []EnumContext
	}{
		{
			name:   "unvalued members",
			source: "enum Direction { North, East, South, West }",
			want: []EnumContext{{
				Name:    "Direction",
				Members: []string{"North", "East", "South", "West"},
			}},
		},
		{
			name:   "valued members",
			source: "enum Status {\n    Active = \"active\",\n    Inactive = \"inactive\",\n}",
			want: []EnumContext{{
				Name:    "Status",
				Members: []string{"Active", "Inactive"},
			}},
		},
		{
			name:   "no enums",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractEnums(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestEnums_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`enum E { A, B }`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryEnums(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node)
	assert.Equal(t, "enum_declaration", got[0].Node.Kind())
}
