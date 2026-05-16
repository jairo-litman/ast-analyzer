package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractInterfaces(t *testing.T, e *Extractor, source string) []InterfaceContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryInterfaces(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryInterfaces(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []InterfaceContext
	}{
		{
			name:   "properties only",
			source: "interface Point {\n    x: number;\n    y: number;\n}",
			want: []InterfaceContext{{
				Name: "Point",
				Properties: []ObjectProperty{
					{Name: "x", Type: "number"},
					{Name: "y", Type: "number"},
				},
			}},
		},
		{
			name:   "method signatures",
			source: "interface Greeter {\n    greet(name: string): string;\n}",
			want: []InterfaceContext{{
				Name: "Greeter",
				Methods: []MethodSignature{
					{
						Name:       "greet",
						Parameters: []FunctionParameter{{Name: "name", Type: "string"}},
						ReturnType: "string",
					},
				},
			}},
		},
		{
			name:   "multiple extends",
			source: "interface C extends A, B {\n    z: boolean;\n}",
			want: []InterfaceContext{{
				Name:    "C",
				Extends: []string{"A", "B"},
				Properties: []ObjectProperty{
					{Name: "z", Type: "boolean"},
				},
			}},
		},
		{
			name:   "no interfaces",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractInterfaces(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInterfaces_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`interface I { x: number; }`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryInterfaces(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node)
	assert.Equal(t, "interface_declaration", got[0].Node.Kind())
}
