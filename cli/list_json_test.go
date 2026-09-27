package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_listFormatJSON checks --format json emits every symbol with
// byte offsets and 1-based inclusive line numbers consistent with the
// source.
func TestRun_listFormatJSON(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, "--format", "json", fullFixtureRoot}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var rows []ListedSymbol
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.NotEmpty(t, rows)

	var found bool
	for _, r := range rows {
		src, err := os.ReadFile(filepath.Join(fullFixtureRoot, r.File))
		require.NoError(t, err)
		lines := strings.Split(string(src), "\n")

		assert.Less(t, r.StartByte, r.EndByte, r.ID)
		assert.GreaterOrEqual(t, r.StartLine, 1, r.ID)
		assert.LessOrEqual(t, r.StartLine, r.EndLine, r.ID)
		assert.Equal(t, strings.Count(string(src[:r.StartByte]), "\n")+1, r.StartLine, r.ID)
		assert.Equal(t, strings.Count(string(src[:r.EndByte-1]), "\n")+1, r.EndLine, r.ID)

		if r.Kind == "function" && r.Name == "createTodo" {
			found = true
			assert.Contains(t, lines[r.StartLine-1], "function createTodo")
		}
	}
	assert.True(t, found, "createTodo not listed")
}

// TestRun_listFormatJSONAppliesFilters checks filters still apply.
func TestRun_listFormatJSONAppliesFilters(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--db", dbPath, "--format", "json", "--kind", "class", fullFixtureRoot}, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var rows []ListedSymbol
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &rows))
	require.NotEmpty(t, rows)
	for _, r := range rows {
		assert.Equal(t, "class", r.Kind)
	}
}

func TestRun_listRejectsUnknownFormat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"list", "--format", "xml", fullFixtureRoot}, &stdout, &stderr)
	assert.NotEqual(t, 0, code)
	assert.Contains(t, stderr.String(), "--format")
}
