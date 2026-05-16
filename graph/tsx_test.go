package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildProject_includesTSXFiles pins TSX file walking, parsing,
// and cross-file resolution from a .tsx into a sibling .ts file.
func TestBuildProject_includesTSXFiles(t *testing.T) {
	p, err := BuildProject(
		"testdata/tsx",
		"testdata/tsx/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)

	names := map[string]string{}
	for _, s := range p.Symbols {
		names[s.Name] = s.File
	}
	assert.Equal(t, "component.tsx", names["Counter"],
		"Counter from component.tsx should be captured")
	assert.Equal(t, "helper.ts", names["increment"],
		"increment from helper.ts should still be captured alongside the tsx file")

	ResolveCalls(p)
	var counterCall bool
	for _, c := range p.Calls {
		if c.Callee == "increment" && len(c.ResolvedTo) > 0 {
			counterCall = true
			break
		}
	}
	assert.True(t, counterCall,
		"Counter's call to increment() should resolve across the tsx → ts boundary")
}
