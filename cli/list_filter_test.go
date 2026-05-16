package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listAgainstFullFixture indexes the full fixture once and runs `list`
// with the supplied flags. Returns split, non-header rows from stdout.
func listAgainstFullFixture(t *testing.T, args ...string) []string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)

	full := append([]string{"list", "--db", dbPath}, args...)
	full = append(full, fullFixtureRoot)

	var stdout, stderr bytes.Buffer
	code := Run(full, &stdout, &stderr)
	require.Equal(t, 0, code, "stderr=%s", stderr.String())

	var rows []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "ID") {
			continue
		}
		rows = append(rows, line)
	}
	return rows
}

// rowMatchesKind reports whether a tabwriter row carries the named
// SymbolKind in its KIND column. Substring is enough — the kinds we
// emit are non-overlapping (`function` vs `function_call`, etc., aren't
// in the catalog).
func rowMatchesKind(row, kind string) bool {
	return strings.Contains(row, "  "+kind+"  ") || strings.Contains(row, "  "+kind+"\t")
}

// TestRun_listFilterByKindSingle pins --kind selecting one kind.
func TestRun_listFilterByKindSingle(t *testing.T) {
	rows := listAgainstFullFixture(t, "--kind", "class")
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.True(t, rowMatchesKind(row, "class"), "non-class row leaked through: %q", row)
	}
	// Spot-check expected classes are present.
	joined := strings.Join(rows, "\n")
	for _, name := range []string{"BaseTodo", "Todo", "Storage"} {
		assert.Contains(t, joined, name)
	}
}

// TestRun_listFilterByKindMulti pins --kind accepting a
// comma-separated list (OR semantics within the kind filter).
func TestRun_listFilterByKindMulti(t *testing.T) {
	rows := listAgainstFullFixture(t, "--kind", "interface,enum")
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.True(t,
			rowMatchesKind(row, "interface") || rowMatchesKind(row, "enum"),
			"unexpected kind in row: %q", row)
	}
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "Identifiable")
	assert.Contains(t, joined, "TodoLike")
	assert.Contains(t, joined, "Priority")
}

// TestRun_listFilterByFile pins --file as a regex against the file
// column.
func TestRun_listFilterByFile(t *testing.T) {
	rows := listAgainstFullFixture(t, "--file", "services/")
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.Contains(t, row, "services/", "row from outside services/ leaked: %q", row)
	}
}

// TestRun_listFilterByName pins --name as a regex.
func TestRun_listFilterByName(t *testing.T) {
	rows := listAgainstFullFixture(t, "--name", "^find")
	require.NotEmpty(t, rows)
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "findHighPriority")
	assert.Contains(t, joined, "findAll")
	assert.NotContains(t, joined, "createTodo")
}

// TestRun_listFilterCombined pins AND semantics across filters.
func TestRun_listFilterCombined(t *testing.T) {
	rows := listAgainstFullFixture(t,
		"--kind", "function",
		"--file", "services/",
		"--name", "^find",
	)
	require.NotEmpty(t, rows)
	for _, row := range rows {
		assert.True(t, rowMatchesKind(row, "function"))
		assert.Contains(t, row, "services/")
	}
	joined := strings.Join(rows, "\n")
	assert.Contains(t, joined, "findHighPriority")
	assert.Contains(t, joined, "findAll")
	// `formatList` matches the name regex but lives in utils/, not
	// services/, so it must be filtered out.
	assert.NotContains(t, joined, "formatList")
}

// TestRun_listFilterUnknownKindErrors pins validation: a typoed kind
// returns a usage error rather than silently producing zero rows.
func TestRun_listFilterUnknownKindErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list", "--db", dbPath, "--kind", "bogus",
		fullFixtureRoot,
	}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "kind")
}

// TestRun_listFilterInvalidNameRegexErrors pins regex parse errors
// surface to the user.
func TestRun_listFilterInvalidNameRegexErrors(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"list", "--db", dbPath, "--name", "[",
		fullFixtureRoot,
	}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "name")
}
