package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func extractReExports(t *testing.T, e *Extractor, source string) []ReExportContext {
	t.Helper()

	src := []byte(source)
	tree, err := e.Parse(src)
	require.NoError(t, err)
	t.Cleanup(tree.Close)

	got, err := e.QueryReExports(tree.RootNode(), src)
	require.NoError(t, err)

	for i := range got {
		got[i].Node = nil
	}
	return got
}

func TestQueryReExports(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name   string
		source string
		want   []ReExportContext
	}{
		{
			name:   "named re-export",
			source: `export { foo } from "./other";`,
			want: []ReExportContext{{
				Path: "./other",
				Kind: KindValue,
				Bindings: []ReExportBindingContext{
					{LocalName: "foo", RemoteName: "foo"},
				},
			}},
		},
		{
			name:   "aliased re-export",
			source: `export { foo as bar } from "./other";`,
			want: []ReExportContext{{
				Path: "./other",
				Kind: KindValue,
				Bindings: []ReExportBindingContext{
					{LocalName: "bar", RemoteName: "foo"},
				},
			}},
		},
		{
			name:   "multiple bindings, mixed alias",
			source: `export { a, b as c } from "./other";`,
			want: []ReExportContext{{
				Path: "./other",
				Kind: KindValue,
				Bindings: []ReExportBindingContext{
					{LocalName: "a", RemoteName: "a"},
					{LocalName: "c", RemoteName: "b"},
				},
			}},
		},
		{
			name:   "star re-export",
			source: `export * from "./other";`,
			want: []ReExportContext{{
				Path: "./other",
				Kind: KindValue,
			}},
		},
		{
			name:   "namespace re-export",
			source: `export * as ns from "./other";`,
			want: []ReExportContext{{
				Path:      "./other",
				Kind:      KindValue,
				Namespace: "ns",
			}},
		},
		{
			name:   "top-level type re-export",
			source: `export type { Foo } from "./types";`,
			want: []ReExportContext{{
				Path: "./types",
				Kind: KindType,
				Bindings: []ReExportBindingContext{
					{LocalName: "Foo", RemoteName: "Foo"},
				},
			}},
		},
		{
			name:   "inline type binding flag",
			source: `export { type Foo, bar } from "./mixed";`,
			want: []ReExportContext{{
				Path: "./mixed",
				Kind: KindValue,
				Bindings: []ReExportBindingContext{
					{LocalName: "Foo", RemoteName: "Foo", IsTypeOnly: true},
					{LocalName: "bar", RemoteName: "bar"},
				},
			}},
		},
		{
			name:   "no re-exports",
			source: `export const x = 1;`,
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractReExports(t, e, tc.source)
			assert.Equal(t, tc.want, got)
		})
	}
}
