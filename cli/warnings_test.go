package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const multiDefaultRoot = "../graph/testdata/multi_default"
const multiDefaultTsconfig = "../graph/testdata/multi_default/tsconfig.json"

// TestRun_indexEmitsBuildWarnings pins that BuildProject's Warnings
// (e.g. multiple `export default`) reach stderr from the index path.
func TestRun_indexEmitsBuildWarnings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index",
		"--tsconfig", multiDefaultTsconfig,
		"--output", dbPath,
		multiDefaultRoot,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "warning:")
	assert.Contains(t, stderr.String(), "bad.ts")
	assert.Contains(t, stderr.String(), "default export")
}

// TestRun_listRebuildEmitsBuildWarnings pins the rebuild path: a
// fresh BuildProject during list also surfaces warnings.
func TestRun_listRebuildEmitsBuildWarnings(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list", "--rebuild",
		"--tsconfig", multiDefaultTsconfig,
		multiDefaultRoot,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "warning:")
	assert.Contains(t, stderr.String(), "default export")
}

// TestRun_listFromDBNoWarnings pins the load path: warnings aren't
// persisted, so list against a previously-built DB stays quiet on
// stderr (no double emission).
func TestRun_listFromDBNoWarnings(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	indexFixtureToDB(t, multiDefaultRoot, multiDefaultTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, "--no-stale-check", multiDefaultRoot}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())
	assert.NotContains(t, stderr.String(), "warning:")
}
