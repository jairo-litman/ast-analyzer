package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// concurrentBuffer is bytes.Buffer behind a mutex so a watch loop
// goroutine and the test goroutine can write/read without races.
type concurrentBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *concurrentBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *concurrentBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// waitForOutput polls buf until contains substr or fails the test on
// timeout.
func waitForOutput(t *testing.T, buf *concurrentBuffer, substr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(buf.String(), substr) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("output did not contain %q within %v\nfull output:\n%s",
		substr, timeout, buf.String())
}

// startWatch launches the watch loop on root with a short debounce
// suitable for tests. Returns a cancel func and a wait func — defer
// cancel+wait at the start of every test.
func startWatch(t *testing.T, root, dbPath string) (cancel func(), wait func() error, stdout, stderr *concurrentBuffer) {
	t.Helper()
	ctx, cancelFn := context.WithCancel(context.Background())
	var sout, serr concurrentBuffer

	args := []string{
		"--tsconfig", filepath.Join(root, "tsconfig.json"),
		"--db", dbPath,
		"--debounce-ms", "50",
		root,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- runWatchLoop(ctx, args, &sout, &serr)
	}()
	return cancelFn, func() error { return <-errCh }, &sout, &serr
}

// TestWatch_initialIndex pins that the watch loop runs one full
// indexing pass on startup, before any events.
func TestWatch_initialIndex(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	cancel, wait, stdout, _ := startWatch(t, root, dbPath)
	defer func() {
		cancel()
		require.NoError(t, wait())
	}()

	// Simple fixture has main.ts + helper.ts.
	waitForOutput(t, stdout, "2 added", 5*time.Second)
}

// TestWatch_picksUpFileChange pins that modifying an indexed file
// triggers a re-index batch with the changed count.
func TestWatch_picksUpFileChange(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	cancel, wait, stdout, _ := startWatch(t, root, dbPath)
	defer func() {
		cancel()
		require.NoError(t, wait())
	}()

	waitForOutput(t, stdout, "2 added", 5*time.Second)

	mainPath := filepath.Join(root, "main.ts")
	orig, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mainPath,
		append(orig, []byte("\nfunction watchAddedSentinel() {}\n")...), 0o644))

	waitForOutput(t, stdout, "1 changed", 5*time.Second)

	// Confirm the new symbol made it into the index.
	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)
	var hasSentinel bool
	for _, sym := range loaded.Symbols {
		if sym.Name == "watchAddedSentinel" {
			hasSentinel = true
			break
		}
	}
	assert.True(t, hasSentinel, "watch should have re-indexed the modified file")
}

// TestWatch_picksUpNewFile pins that creating a new TypeScript file
// triggers a re-index recording it as added.
func TestWatch_picksUpNewFile(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	cancel, wait, stdout, _ := startWatch(t, root, dbPath)
	defer func() {
		cancel()
		require.NoError(t, wait())
	}()

	waitForOutput(t, stdout, "2 added", 5*time.Second)

	require.NoError(t, os.WriteFile(
		filepath.Join(root, "extra.ts"),
		[]byte("export function freshSentinel() {}\n"),
		0o644,
	))

	waitForOutput(t, stdout, "1 added", 5*time.Second)

	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)
	var hasFresh bool
	for _, sym := range loaded.Symbols {
		if sym.Name == "freshSentinel" {
			hasFresh = true
			break
		}
	}
	assert.True(t, hasFresh)
}

// TestWatch_picksUpDeletedFile pins deletion-triggered re-index.
func TestWatch_picksUpDeletedFile(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	cancel, wait, stdout, _ := startWatch(t, root, dbPath)
	defer func() {
		cancel()
		require.NoError(t, wait())
	}()

	waitForOutput(t, stdout, "2 added", 5*time.Second)

	require.NoError(t, os.Remove(filepath.Join(root, "helper.ts")))

	waitForOutput(t, stdout, "1 removed", 5*time.Second)
}

// TestWatch_picksUpFileInNewSubdir pins that a new subdirectory and
// the files dropped into it both get watched and indexed.
func TestWatch_picksUpFileInNewSubdir(t *testing.T) {
	root := copyFixtureToTemp(t, simpleFixtureRoot)
	dbPath := filepath.Join(t.TempDir(), "graph.db")

	cancel, wait, stdout, _ := startWatch(t, root, dbPath)
	defer func() {
		cancel()
		require.NoError(t, wait())
	}()

	waitForOutput(t, stdout, "2 added", 5*time.Second)

	subdir := filepath.Join(root, "lib")
	require.NoError(t, os.Mkdir(subdir, 0o755))
	// Tiny pause so the directory create event reaches the watcher
	// before the file create event arrives — fsnotify orders these
	// in real time but giving the goroutine a tick keeps the test
	// non-flaky.
	time.Sleep(40 * time.Millisecond)

	require.NoError(t, os.WriteFile(
		filepath.Join(subdir, "nested.ts"),
		[]byte("export function nestedSentinel() {}\n"),
		0o644,
	))

	waitForOutput(t, stdout, "1 added", 5*time.Second)

	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)
	for _, sym := range loaded.Symbols {
		if sym.Name == "nestedSentinel" {
			return
		}
	}
	t.Fatalf("watch did not pick up the new file in the new subdirectory; symbols: %v", symbolNames(loaded.Symbols))
}

// symbolNames is a debug helper for failure messages.
func symbolNames(syms []graph.Symbol) []string {
	names := make([]string, 0, len(syms))
	for _, s := range syms {
		names = append(names, s.File+"/"+s.Name)
	}
	return names
}

// TestWatch_dispatchedViaRunCommand pins that the `watch` subcommand
// is wired into the dispatcher (so users can invoke it from the CLI).
// Exercises the command lookup, not the watch loop body.
func TestWatch_dispatchedViaRunCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Missing required flag should produce a 1 exit; the "1" is the
	// signal that runWatch ran (as opposed to an "unknown command"
	// path, which would yield exit 2).
	code := Run([]string{"watch"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
}
