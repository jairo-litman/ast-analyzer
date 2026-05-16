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

const (
	callChainRoot     = "../graph/testdata/call_chain"
	callChainTsconfig = "../graph/testdata/call_chain/tsconfig.json"
)

// TestRun_extractFlagsControlDepthAndBodies covers the new
// --caller-depth / --callee-depth / --caller-bodies-up-to /
// --callee-bodies-up-to flags against the call_chain fixture
// (a -> b -> c -> d).
func TestRun_extractFlagsControlDepthAndBodies(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "chain.db")
	indexFixtureToDB(t, callChainRoot, callChainTsconfig, dbPath)

	aID := lookupSymbolIDFromFixture(t, "call_chain", "a.ts", "a")

	t.Run("depth 2 reaches second hop", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"extract",
			"--db", dbPath,
			"--callee-depth", "2",
			"--callee-bodies-up-to", "1",
			callChainRoot, aID,
		}, &stdout, &stderr)
		require.Equal(t, 0, code, "stderr=%s", stderr.String())

		var ctx pruner.Context
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &ctx))

		// Expect b (depth 1, body) and c (depth 2, signature only).
		names := map[string]pruner.Callee{}
		for _, c := range ctx.Callees {
			names[c.Symbol.Name] = c
		}
		require.Contains(t, names, "b")
		require.Contains(t, names, "c")
		assert.Equal(t, 1, names["b"].Depth)
		assert.NotEmpty(t, names["b"].Body, "depth-1 callee should carry body")
		assert.Equal(t, 2, names["c"].Depth)
		assert.Empty(t, names["c"].Body, "depth-2 callee should not carry body")
	})

	t.Run("zero depths omit callers and callees", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"extract",
			"--db", dbPath,
			"--caller-depth", "0",
			"--callee-depth", "0",
			callChainRoot, aID,
		}, &stdout, &stderr)
		require.Equal(t, 0, code, "stderr=%s", stderr.String())

		var ctx pruner.Context
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &ctx))
		assert.Empty(t, ctx.Callees)
		assert.Empty(t, ctx.Callers)
	})

	t.Run("negative flag value is rejected", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run([]string{
			"extract",
			"--db", dbPath,
			"--callee-depth", "-1",
			callChainRoot, aID,
		}, &stdout, &stderr)
		assert.Equal(t, 1, code)
		assert.Contains(t, stderr.String(), "non-negative")
	})
}
