package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// modifyFixtureFile is the standard staling: append a declaration to
// an indexed file so its content hash drifts from the DB's record.
func modifyFixtureFile(t *testing.T, root, file string) {
	t.Helper()
	path := filepath.Join(root, file)
	orig, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path,
		append(orig, []byte("\nfunction staleSentinel() {}\n")...), 0o644))
}

// TestRun_listWarnsOnStaleDB pins that `list` against an out-of-date
// index emits a stale warning to stderr but still succeeds.
func TestRun_listWarnsOnStaleDB(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	modifyFixtureFile(t, root, "main.ts")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, root}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "stale")
	assert.Contains(t, stderr.String(), "1 changed")
}

// TestRun_listSuppressesStaleWarningWithFlag pins that
// --no-stale-check disables the warning even when the index is out
// of date.
func TestRun_listSuppressesStaleWarningWithFlag(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	modifyFixtureFile(t, root, "main.ts")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, "--no-stale-check", root}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.NotContains(t, stderr.String(), "stale")
}

// TestRun_listFreshIndexNoStaleWarning pins that an unmodified
// project produces no stale warning.
func TestRun_listFreshIndexNoStaleWarning(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, root}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.NotContains(t, stderr.String(), "stale")
}

// TestRun_extractWarnsOnStaleDB extends the stale check to the
// extract subcommand.
func TestRun_extractWarnsOnStaleDB(t *testing.T) {
	root := copyFixtureToTemp(t, "../graph/testdata/resolution")
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	mainID := lookupSymbolIDFromFixture(t, "resolution", "main.ts", "main")

	modifyFixtureFile(t, root, "helper.ts")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"extract", "--db", dbPath, root, mainID}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "stale")
}

// TestRun_extractSuppressesStaleWarningWithFlag pins
// --no-stale-check on extract.
func TestRun_extractSuppressesStaleWarningWithFlag(t *testing.T) {
	root := copyFixtureToTemp(t, "../graph/testdata/resolution")
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"), dbPath)

	mainID := lookupSymbolIDFromFixture(t, "resolution", "main.ts", "main")
	modifyFixtureFile(t, root, "helper.ts")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"extract", "--db", dbPath, "--no-stale-check", root, mainID},
		&stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.NotContains(t, stderr.String(), "stale")
}

// TestRun_listRebuildSkipsStaleCheck pins that --rebuild builds from
// source so the DB-versus-disk staleness check has nothing to compare
// against and emits no warning.
func TestRun_listRebuildSkipsStaleCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list", "--rebuild",
		"--tsconfig", simpleFixtureTsconfig,
		simpleFixtureRoot,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.NotContains(t, stderr.String(), "stale")
}
