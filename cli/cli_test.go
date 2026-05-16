package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun_indexPersistsToSQLite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"index",
		"--tsconfig", "../graph/testdata/simple/tsconfig.json",
		"--output", dbPath,
		"../graph/testdata/simple",
	}, &stdout, &stderr)

	require.Equal(t, 0, code, "stderr=%q", stderr.String())
	assert.Contains(t, stdout.String(), "indexed",
		"index command should report what it wrote")

	// Confirm the DB is populated by loading it back.
	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)
	assert.NotEmpty(t, loaded.Symbols)
	assert.NotEmpty(t, loaded.Imports)
}

func TestRun_unknownCommandIsExitCode2(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	assert.Contains(t, stderr.String(), "unknown command")
}

func TestRun_noArgsPrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{}, &stdout, &stderr)
	assert.Equal(t, 2, code)
	// Usage names every subcommand so users can discover them.
	for _, sub := range []string{"list", "extract", "index", "watch"} {
		assert.Contains(t, stderr.String(), sub)
	}
}

// TestRun_helpFlagsPrintToStdout pins the POSIX-style help convention:
// explicit `--help` / `-h` / `help` arguments produce usage on stdout
// with exit 0, distinct from the stderr/exit-2 path that fires for
// missing or unknown commands.
func TestRun_helpFlagsPrintToStdout(t *testing.T) {
	cases := []string{"--help", "-h", "help"}
	for _, arg := range cases {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run([]string{arg}, &stdout, &stderr)
			assert.Equal(t, 0, code, "stderr=%s", stderr.String())
			assert.Empty(t, stderr.String(), "help should not write to stderr")
			assert.Contains(t, stdout.String(), "usage:")
			for _, sub := range []string{"list", "extract", "index", "watch"} {
				assert.Contains(t, stdout.String(), sub)
			}
		})
	}
}

// lookupSymbolIDFromFixture rebuilds a fixture to obtain a stable
// Symbol ID for the CLI command under test.
func lookupSymbolIDFromFixture(t *testing.T, fixture, file, name string) string {
	t.Helper()
	root := "../graph/testdata/" + fixture
	p, err := graph.BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	for _, s := range p.Symbols {
		if s.File == file && s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("symbol %q not in %s of fixture %s", name, file, fixture)
	return ""
}
