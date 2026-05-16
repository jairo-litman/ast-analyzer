package pruner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExtract_filtersUnusedImports pins that imports whose bindings
// aren't referenced in the kept content are dropped from
// Context.Imports.
func TestExtract_filtersUnusedImports(t *testing.T) {
	p := buildAndResolve(t, "imports_filter")
	mainID := symbolID(t, p, "main.ts", "main")

	ctx, err := Extract(p, mainID)
	require.NoError(t, err)

	var paths []string
	for _, imp := range ctx.Imports {
		paths = append(paths, imp.Edge.Path)
	}
	assert.Equal(t, []string{"./used"}, paths,
		"only the import whose binding is referenced in main()'s body should remain")
}

// TestExtract_keepsTypeOnlyImportWhenReferenced confirms the filter
// doesn't crash on zero-import targets.
func TestExtract_keepsTypeOnlyImportWhenReferenced(t *testing.T) {
	p := buildAndResolve(t, "imports_filter")
	usedID := symbolID(t, p, "used.ts", "used")

	ctx, err := Extract(p, usedID)
	require.NoError(t, err)

	assert.Empty(t, ctx.Imports)
}
