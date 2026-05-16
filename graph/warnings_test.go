package graph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildProject_warnsOnMultipleDefaultExports pins the warning
// emitted when one source file flags two symbols as default exports.
// The data still loads (first wins on resolution), but the operator
// gets a heads-up that something is wrong with their input.
func TestBuildProject_warnsOnMultipleDefaultExports(t *testing.T) {
	p, err := BuildProject(
		"testdata/multi_default",
		"testdata/multi_default/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)

	require.NotEmpty(t, p.Warnings, "expected at least one warning for the bad fixture; got none")

	var found bool
	for _, w := range p.Warnings {
		if strings.Contains(w, "bad.ts") && strings.Contains(w, "default") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected a multi-default warning mentioning bad.ts; got: %v", p.Warnings)
}

// TestBuildProject_noWarningsOnValidProject pins the negative case:
// a clean fixture produces no warnings.
func TestBuildProject_noWarningsOnValidProject(t *testing.T) {
	p, err := BuildProject(
		"testdata/simple",
		"testdata/simple/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)

	assert.Empty(t, p.Warnings)
}
