package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractDefaultExports(t *testing.T, e *Extractor, source string) []string {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryDefaultExports(tree.RootNode(), src)
	require.NoError(t, err)
	return got
}

func TestQueryDefaultExports(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []string
	}{
		{
			name:   "named function default export",
			source: "export default function calculate(x: number): number { return x * 2; }",
			want:   []string{"calculate"},
		},
		{
			name:   "named class default export",
			source: "export default class Widget { render(): string { return \"<w/>\"; } }",
			want:   []string{"Widget"},
		},
		{
			name:   "identifier reference default export",
			source: "function helper(): void {}\nexport default helper;",
			want:   []string{"helper"},
		},
		{
			name:   "anonymous function is not surfaced",
			source: "export default function() { return 1; }",
			want:   nil,
		},
		{
			name:   "anonymous class is not surfaced",
			source: "export default class { render() {} }",
			want:   nil,
		},
		{
			name:   "object literal is not surfaced",
			source: "export default { method() {} };",
			want:   nil,
		},
		{
			name:   "no default export",
			source: "export function foo() {}",
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractDefaultExports(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}
