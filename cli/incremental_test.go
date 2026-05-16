package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reindex re-runs the `index` subcommand against an existing root +
// DB pair.
func reindex(t *testing.T, root, dbPath string) (stdout, stderr string) {
	t.Helper()
	var sout, serr bytes.Buffer
	code := Run([]string{
		"index",
		"--tsconfig", filepath.Join(root, "tsconfig.json"),
		"--output", dbPath,
		root,
	}, &sout, &serr)
	require.Equal(t, 0, code, "stderr=%s", serr.String())
	return sout.String(), serr.String()
}

// TestIndex_firstRunReportsAllAdded pins the baseline counts: every
// file shows as added on a fresh index.
func TestIndex_firstRunReportsAllAdded(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	stdout, _ := reindex(t, root, dbPath)
	// Simple fixture has main.ts and helper.ts.
	assert.Contains(t, stdout, "2 added")
	assert.Contains(t, stdout, "0 changed")
	assert.Contains(t, stdout, "0 removed")
	assert.Contains(t, stdout, "0 unchanged")
}

// TestIndex_secondRunReportsAllUnchanged pins the no-edits case: the
// hash check short-circuits parsing and counts report all unchanged.
func TestIndex_secondRunReportsAllUnchanged(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)
	stdout, _ := reindex(t, root, dbPath)

	assert.Contains(t, stdout, "0 added")
	assert.Contains(t, stdout, "0 changed")
	assert.Contains(t, stdout, "0 removed")
	assert.Contains(t, stdout, "2 unchanged")
}

// TestIndex_picksUpFileChange edits an indexed file and verifies the
// re-index reports the change and surfaces the new symbol.
func TestIndex_picksUpFileChange(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	mainPath := filepath.Join(root, "main.ts")
	orig, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mainPath,
		append(orig, []byte("\nfunction freshlyAdded() {}\n")...), 0o644))

	stdout, _ := reindex(t, root, dbPath)
	assert.Contains(t, stdout, "0 added")
	assert.Contains(t, stdout, "1 changed")
	assert.Contains(t, stdout, "0 removed")
	assert.Contains(t, stdout, "1 unchanged")

	var listOut, listErr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, root}, &listOut, &listErr)
	require.Equal(t, 0, code, "stderr=%s", listErr.String())
	assert.Contains(t, listOut.String(), "freshlyAdded",
		"newly added function should surface after re-index")
}

// TestIndex_picksUpNewFile adds a file to the project and checks its
// symbols land in the index with the added count incrementing.
func TestIndex_picksUpNewFile(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "extra.ts"),
		[]byte("export function brandNew() {}\n"),
		0o644,
	))

	stdout, _ := reindex(t, root, dbPath)
	assert.Contains(t, stdout, "1 added")
	assert.Contains(t, stdout, "0 changed")
	assert.Contains(t, stdout, "0 removed")
	assert.Contains(t, stdout, "2 unchanged")

	var listOut, listErr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, root}, &listOut, &listErr)
	require.Equal(t, 0, code, "stderr=%s", listErr.String())
	assert.Contains(t, listOut.String(), "brandNew")
	assert.Contains(t, listOut.String(), "extra.ts")
}

// TestIndex_picksUpDeletedFile removes a file and verifies the
// re-index drops its symbols and reports the removal.
func TestIndex_picksUpDeletedFile(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	require.NoError(t, os.Remove(filepath.Join(root, "helper.ts")))

	stdout, _ := reindex(t, root, dbPath)
	assert.Contains(t, stdout, "0 added")
	assert.Contains(t, stdout, "0 changed")
	assert.Contains(t, stdout, "1 removed")
	assert.Contains(t, stdout, "1 unchanged")

	var listOut, listErr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, root}, &listOut, &listErr)
	require.Equal(t, 0, code, "stderr=%s", listErr.String())
	assert.NotContains(t, listOut.String(), "helper.ts",
		"deleted file's symbols must be purged on re-index")
}
