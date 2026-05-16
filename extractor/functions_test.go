package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractFunctions(t *testing.T, e *Extractor, source string) []FunctionContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryFunctions(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
		got[i].BodyNode = nil
	}
	return got
}

func TestQueryFunctions(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []FunctionContext
	}{
		{
			name:   "untyped bare function",
			source: "function ping() {\n    console.log(\"pong\");\n}",
			want: []FunctionContext{{
				Name: "ping",
				Body: "{\n    console.log(\"pong\");\n}",
			}},
		},
		{
			name:   "typed parameters and return",
			source: "function calculateTotal(price: number, tax: number): number {\n    return price + price * tax;\n}",
			want: []FunctionContext{{
				Name:       "calculateTotal",
				ReturnType: "number",
				Body:       "{\n    return price + price * tax;\n}",
				Parameters: []FunctionParameter{
					{Name: "price", Type: "number"},
					{Name: "tax", Type: "number"},
				},
			}},
		},
		{
			name:   "untyped parameter precedes typed parameter",
			source: "function processData(data, isValid: boolean) {\n    if (isValid) {\n        console.log(data);\n    }\n}",
			want: []FunctionContext{{
				Name: "processData",
				Body: "{\n    if (isValid) {\n        console.log(data);\n    }\n}",
				Parameters: []FunctionParameter{
					{Name: "data", Type: ""},
					{Name: "isValid", Type: "boolean"},
				},
			}},
		},
		{
			name:   "typed parameter precedes untyped parameter",
			source: "function f(a: number, b) { return a; }",
			want: []FunctionContext{{
				Name: "f",
				Body: "{ return a; }",
				Parameters: []FunctionParameter{
					{Name: "a", Type: "number"},
					{Name: "b", Type: ""},
				},
			}},
		},
		{
			name:   "destructured parameters are skipped",
			source: "function point({ x, y }: { x: number; y: number }) { return x + y; }",
			want: []FunctionContext{{
				Name:       "point",
				Body:       "{ return x + y; }",
				Parameters: nil,
			}},
		},
		{
			name:   "class method",
			source: "class A {\n    greet(name: string): string {\n        return \"hi \" + name;\n    }\n}",
			want: []FunctionContext{{
				Name:       "greet",
				ReturnType: "string",
				Body:       "{\n        return \"hi \" + name;\n    }",
				Parameters: []FunctionParameter{{Name: "name", Type: "string"}},
			}},
		},
		{
			name:   "arrow function via const",
			source: "const add = (a: number, b: number): number => a + b;",
			want: []FunctionContext{{
				Name:       "add",
				ReturnType: "number",
				Body:       "a + b",
				Parameters: []FunctionParameter{
					{Name: "a", Type: "number"},
					{Name: "b", Type: "number"},
				},
			}},
		},
		{
			name:   "arrow shorthand single-identifier parameter",
			source: "const square = x => x * x;",
			want: []FunctionContext{{
				Name:       "square",
				Body:       "x * x",
				Parameters: []FunctionParameter{{Name: "x"}},
			}},
		},
		{
			name:   "multiple top-level declarations preserve source order",
			source: "function firstFunc(a: string): void {\n    return;\n}\n\nfunction secondFunc(b: number, c: number): number {\n    return b + c;\n}",
			want: []FunctionContext{
				{
					Name:       "firstFunc",
					ReturnType: "void",
					Body:       "{\n    return;\n}",
					Parameters: []FunctionParameter{{Name: "a", Type: "string"}},
				},
				{
					Name:       "secondFunc",
					ReturnType: "number",
					Body:       "{\n    return b + c;\n}",
					Parameters: []FunctionParameter{
						{Name: "b", Type: "number"},
						{Name: "c", Type: "number"},
					},
				},
			},
		},
		{
			name:   "class-field arrow with block body",
			source: "class A {\n    handle = (e: Event): void => {\n        console.log(e);\n    }\n}",
			want: []FunctionContext{{
				Name:       "handle",
				ReturnType: "void",
				Body:       "{\n        console.log(e);\n    }",
				Parameters: []FunctionParameter{{Name: "e", Type: "Event"}},
			}},
		},
		{
			name:   "class-field arrow with expression body",
			source: "class A {\n    square = (n: number) => n * n;\n}",
			want: []FunctionContext{{
				Name:       "square",
				Body:       "n * n",
				Parameters: []FunctionParameter{{Name: "n", Type: "number"}},
			}},
		},
		{
			name:   "iife arrow at top level",
			source: "(() => {\n    return 42;\n})();",
			want: []FunctionContext{{
				Name: "(iife)",
				Body: "{\n    return 42;\n}",
			}},
		},
		{
			name:   "no functions",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFunctions(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFunctions_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`function ping() { return; }`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryFunctions(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node, "function_declaration capture should populate Node")
	assert.Equal(t, "function_declaration", got[0].Node.Kind())
}

// TestFunctions_PopulatesBodyNode pins the BodyNode field for each
// function shape — it's the entry point for sub-queries scoped to a
// function body.
func TestFunctions_PopulatesBodyNode(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name     string
		source   string
		wantKind string
	}{
		{"function declaration", "function f() { return 1; }", "statement_block"},
		{"class method", "class A { f() { return 1; } }", "statement_block"},
		{"arrow with block body", "const f = () => { return 1; };", "statement_block"},
		{"arrow with expression body", "const f = () => 1;", "number"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte(tc.source)
			tree, err := e.Parse(src)
			require.NoError(t, err)
			defer tree.Close()

			got, err := e.QueryFunctions(tree.RootNode(), src)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.NotNil(t, got[0].BodyNode, "BodyNode should be populated")
			assert.Equal(t, tc.wantKind, got[0].BodyNode.Kind())
		})
	}
}
