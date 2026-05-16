package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractFunctionCalls(t *testing.T, e *Extractor, source string) []FunctionCallContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryFunctionCalls(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryFunctionCalls(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []FunctionCallContext
	}{
		{
			name:   "direct call by identifier",
			source: "function f() { foo(); }",
			want: []FunctionCallContext{
				{Name: "foo", Expression: "foo()"},
			},
		},
		{
			name:   "method call via member expression",
			source: "function f() { obj.foo(); }",
			want: []FunctionCallContext{
				{Name: "foo", Receiver: "obj", Expression: "obj.foo()"},
			},
		},
		{
			name:   "deep member access chain",
			source: "function f() { foo.bar.zoo.func(); }",
			want: []FunctionCallContext{
				{Name: "func", Receiver: "foo.bar.zoo", Expression: "foo.bar.zoo.func()"},
			},
		},
		{
			// Pre-order: outer first (receiver is the inner call's
			// source text), then the inner.
			name:   "chained calls produce one entry per call site",
			source: "function f() { foo.bar().baz(); }",
			want: []FunctionCallContext{
				{Name: "baz", Receiver: "foo.bar()", Expression: "foo.bar().baz()"},
				{Name: "bar", Receiver: "foo", Expression: "foo.bar()"},
			},
		},
		{
			name:   "this method call",
			source: "class A { run() { this.helper(); } }",
			want: []FunctionCallContext{
				{Name: "helper", Receiver: "this", Expression: "this.helper()"},
			},
		},
		{
			name:   "subscript call has no static name",
			source: "function f() { arr[0](); }",
			want: []FunctionCallContext{
				{Name: "", Receiver: "arr[0]", Expression: "arr[0]()"},
			},
		},
		{
			name:   "call on call result has no static name on the outer site",
			source: "function f() { getFn()(); }",
			want: []FunctionCallContext{
				{Name: "", Receiver: "getFn()", Expression: "getFn()()"},
				{Name: "getFn", Expression: "getFn()"},
			},
		},
		{
			name:   "parenthesized identifier resolves to its inner name",
			source: "function f() { (foo)(); }",
			want: []FunctionCallContext{
				{Name: "foo", Expression: "(foo)()"},
			},
		},
		{
			name:   "argument containing a nested call yields a second entry",
			source: "function f() { foo(bar()); }",
			want: []FunctionCallContext{
				{Name: "foo", Expression: "foo(bar())"},
				{Name: "bar", Expression: "bar()"},
			},
		},
		{
			name:   "constructor invocation with arguments",
			source: "function f() { new Foo(1); }",
			want: []FunctionCallContext{
				{Name: "Foo", Expression: "new Foo(1)", IsConstructor: true},
			},
		},
		{
			name:   "constructor invocation without parentheses",
			source: "function f() { new Foo; }",
			want: []FunctionCallContext{
				{Name: "Foo", Expression: "new Foo", IsConstructor: true},
			},
		},
		{
			name:   "constructor on member expression",
			source: "function f() { new pkg.sub.Foo(); }",
			want: []FunctionCallContext{
				{Name: "Foo", Receiver: "pkg.sub", Expression: "new pkg.sub.Foo()", IsConstructor: true},
			},
		},
		{
			name:   "regular call mixed with constructor",
			source: "function f() { foo(new Bar()); }",
			want: []FunctionCallContext{
				{Name: "foo", Expression: "foo(new Bar())"},
				{Name: "Bar", Expression: "new Bar()", IsConstructor: true},
			},
		},
		{
			name:   "no calls",
			source: `const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractFunctionCalls(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFunctionCalls_PopulatesNode(t *testing.T) {
	e := helperTestSetup(t)
	src := []byte(`function f() { foo(); }`)

	tree, err := e.Parse(src)
	require.NoError(t, err)
	defer tree.Close()

	got, err := e.QueryFunctionCalls(tree.RootNode(), src)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Node)
	assert.Equal(t, "call_expression", got[0].Node.Kind())
}
