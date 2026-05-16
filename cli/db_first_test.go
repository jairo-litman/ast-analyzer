package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jairo-litman/ast-analyzer/pruner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	simpleFixtureRoot     = "../graph/testdata/simple"
	simpleFixtureTsconfig = "../graph/testdata/simple/tsconfig.json"
)

// indexFixtureToDB runs the index subcommand to populate dbPath.
func indexFixtureToDB(t *testing.T, root, tsconfig, dbPath string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index",
		"--tsconfig", tsconfig,
		"--output", dbPath,
		root,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "index failed: stderr=%s", stderr.String())
}

// copyFixtureToTemp returns a writable copy of a fixture in t.TempDir().
// Used when a test writes to <root>/.astanalyzer/index.db.
func copyFixtureToTemp(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	require.NoError(t, os.CopyFS(dst, os.DirFS(src)))
	return dst
}

// === list ===

func TestRun_listLoadsFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, simpleFixtureRoot, simpleFixtureTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list",
		"--db", dbPath,
		simpleFixtureRoot,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	out := stdout.String()
	for _, name := range []string{"main", "Greeter", "greet", "add"} {
		assert.True(t, strings.Contains(out, name), "expected %q in:\n%s", name, out)
	}
}

func TestRun_listMissingDBErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", missing, simpleFixtureRoot}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	// Error message hints at the recovery path.
	assert.Contains(t, stderr.String(), "index")
}

func TestRun_listRebuildBuildsInMemory(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list", "--rebuild",
		"--tsconfig", simpleFixtureTsconfig,
		simpleFixtureRoot,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "main")
}

func TestRun_listRebuildRequiresTsconfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--rebuild", simpleFixtureRoot}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "tsconfig")
}

func TestRun_listDefaultDBPath(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"),
		filepath.Join(root, ".astanalyzer", "index.db"))

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", root}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stdout.String(), "main")
}

// === extract ===

func TestRun_extractLoadsFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, "../graph/testdata/resolution",
		"../graph/testdata/resolution/tsconfig.json", dbPath)

	mainID := lookupSymbolIDFromFixture(t, "resolution", "main.ts", "main")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract",
		"--db", dbPath,
		"../graph/testdata/resolution",
		mainID,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &ctx))
	assert.Equal(t, mainID, ctx.Target.Symbol.ID)
	assert.Contains(t, ctx.Target.Source, "function main()")
	assert.NotEmpty(t, ctx.Callees)
}

func TestRun_extractMissingDBErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract", "--db", missing,
		simpleFixtureRoot, "main.ts#0",
	}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "index")
}

func TestRun_extractRebuildBuildsInMemory(t *testing.T) {
	mainID := lookupSymbolIDFromFixture(t, "resolution", "main.ts", "main")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract", "--rebuild",
		"--tsconfig", "../graph/testdata/resolution/tsconfig.json",
		"../graph/testdata/resolution", mainID,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &ctx))
	assert.Equal(t, mainID, ctx.Target.Symbol.ID)
}

func TestRun_extractRebuildRequiresTsconfig(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract", "--rebuild",
		simpleFixtureRoot, "main.ts#0",
	}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "tsconfig")
}

func TestRun_extractDefaultDBPath(t *testing.T) {
	root := copyFixtureToTemp(t, "../graph/testdata/resolution")
	indexFixtureToDB(t, root, filepath.Join(root, "tsconfig.json"),
		filepath.Join(root, ".astanalyzer", "index.db"))

	mainID := lookupSymbolIDFromFixture(t, "resolution", "main.ts", "main")

	var stdout, stderr bytes.Buffer
	code := Run([]string{"extract", root, mainID}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &ctx))
	assert.Equal(t, mainID, ctx.Target.Symbol.ID)
}

// === index ===

func TestRun_indexDefaultOutputPath(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index",
		"--tsconfig", filepath.Join(root, "tsconfig.json"),
		root,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	expected := filepath.Join(root, ".astanalyzer", "index.db")
	_, err := os.Stat(expected)
	assert.NoError(t, err, "default index path %s should exist", expected)
	// Output references the actual destination path.
	assert.Contains(t, stdout.String(), expected)
}
