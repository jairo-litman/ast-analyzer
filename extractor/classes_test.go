package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractClasses(t *testing.T, e *Extractor, source string) []ClassContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryClasses(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryClasses(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []ClassContext
	}{
		{
			name:   "properties and method",
			source: "class Point {\n    x: number;\n    y: number;\n    distance(): number { return 0; }\n}",
			want: []ClassContext{{
				Name: "Point",
				Properties: []ObjectProperty{
					{Name: "x", Type: "number"},
					{Name: "y", Type: "number"},
				},
				Methods: []MethodSignature{
					{Name: "distance", ReturnType: "number"},
				},
			}},
		},
		{
			name:   "extends and implements",
			source: "class C extends B implements I, J { z: boolean; }",
			want: []ClassContext{{
				Name:       "C",
				Extends:    "B",
				Implements: []string{"I", "J"},
				Properties: []ObjectProperty{{Name: "z", Type: "boolean"}},
			}},
		},
		{
			name:   "abstract class flagged",
			source: "abstract class Shape {\n    abstract area(): number;\n}",
			want: []ClassContext{{
				Name:     "Shape",
				Abstract: true,
				Methods: []MethodSignature{
					{Name: "area", ReturnType: "number"},
				},
			}},
		},
		{
			name:   "no classes",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractClasses(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestClasses_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`class A { x: number; }`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryClasses(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node)
	assert.Equal(t, "class_declaration", got[0].Node.Kind())
}
