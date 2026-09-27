package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jairo-litman/ast-analyzer/pruner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_extractFormatSpans checks --format spans emits the rendered
// line spans as JSON, target file first.
func TestRun_extractFormatSpans(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	indexFixtureToDB(t, callChainRoot, callChainTsconfig, dbPath)
	aID := lookupSymbolIDFromFixture(t, "call_chain", "a.ts", "a")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract", "--db", dbPath, "--format", "spans",
		"--callee-depth", "2", callChainRoot, aID,
	}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var spans []pruner.FileSpans
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &spans))
	require.NotEmpty(t, spans)
	assert.Equal(t, "a.ts", spans[0].File)
	for _, fs := range spans {
		require.NotEmpty(t, fs.Spans, fs.File)
		for _, s := range fs.Spans {
			assert.GreaterOrEqual(t, s.Start, 1)
			assert.LessOrEqual(t, s.Start, s.End)
		}
	}
}
