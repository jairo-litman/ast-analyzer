package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	e := helperTestSetup(t)

	cases := []struct {
		name         string
		source       string
		hasSyntaxErr bool
	}{
		{
			name:   "well-formed source parses cleanly",
			source: `function valid() { console.log("Hello, World!"); }`,
		},
		{
			name:   "empty source is accepted",
			source: ``,
		},
		{
			name:         "missing close paren surfaces as HasError",
			source:       "function invalid() {\n    console.log(\"Hello, World!\"\n}",
			hasSyntaxErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree, err := e.Parse([]byte(tc.source))
			require.NoError(t, err)
			require.NotNil(t, tree)
			defer tree.Close()

			root := tree.RootNode()
			assert.Equal(t, "program", root.Kind())
			assert.Equal(t, tc.hasSyntaxErr, root.HasError())
		})
	}
}
