package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
)

// runWatch wires runWatchLoop to a context tied to SIGINT/SIGTERM so
// the binary exits cleanly on Ctrl-C.
func runWatch(args []string, stdout, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWatchLoop(ctx, args, stdout, stderr)
}

// runWatchLoop holds an open store handle, runs an initial index
// pass, then watches the project for source changes and re-indexes
// on demand. Returns when ctx is cancelled.
func runWatchLoop(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: astanalyzer watch --tsconfig <path> [--db <path>] [--debounce-ms <ms>] <root>")
		fs.PrintDefaults()
	}
	tsconfig := fs.String("tsconfig", "", "path to tsconfig.json (required)")
	output := fs.String("db", "", "path to the SQLite index (default: <root>/"+defaultDBSubpath+")")
	debounceMS := fs.Int("debounce-ms", 200, "milliseconds to coalesce events before re-indexing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("watch requires one positional argument: <root>")
	}
	if *tsconfig == "" {
		return errors.New("--tsconfig is required")
	}
	if *debounceMS < 0 {
		return errors.New("--debounce-ms must be non-negative")
	}

	root := fs.Arg(0)
	dbPath := *output
	if dbPath == "" {
		dbPath = defaultDBPath(root)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	project, prevHashes, err := loadPreviousFromStore(s, root)
	if err != nil {
		return err
	}
	defer project.Close()

	if err := watchIndexPass(stdout, stderr, s, project, &prevHashes, root, *tsconfig); err != nil {
		return err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	defer watcher.Close()

	if err := addDirsRecursive(watcher, root); err != nil {
		return err
	}

	debounce := time.Duration(*debounceMS) * time.Millisecond
	var timer *time.Timer
	timerC := func() <-chan time.Time {
		if timer == nil {
			return nil
		}
		return timer.C
	}
	resetDebounce := func() {
		if timer == nil {
			timer = time.NewTimer(debounce)
			return
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(debounce)
	}

	for {
		select {
		case <-ctx.Done():
			// Flush any pending changes so a Ctrl-C right after an
			// edit doesn't lose the re-index.
			if timer != nil {
				timer.Stop()
				timer = nil
				if err := watchIndexPass(stdout, stderr, s, project, &prevHashes, root, *tsconfig); err != nil {
					fmt.Fprintln(stderr, "watch:", err)
				}
			}
			return nil

		case ev, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			// New directories get added to the watcher so future
			// events from inside them surface.
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					if !graph.IsSkippedDir(filepath.Base(ev.Name)) {
						_ = addDirsRecursive(watcher, ev.Name)
					}
				}
			}
			if !isWatchableEvent(ev) {
				continue
			}
			resetDebounce()

		case <-timerC():
			timer = nil
			if err := watchIndexPass(stdout, stderr, s, project, &prevHashes, root, *tsconfig); err != nil {
				fmt.Fprintln(stderr, "watch:", err)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Fprintln(stderr, "watch:", err)
		}
	}
}

// isWatchableEvent reports whether ev concerns a tracked source file.
// Directory events are handled separately by the loop's Create case.
func isWatchableEvent(ev fsnotify.Event) bool {
	return graph.IsIncludedFile(ev.Name)
}

// addDirsRecursive registers root and every non-skipped subdirectory
// with watcher.
func addDirsRecursive(watcher *fsnotify.Watcher, root string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	return filepath.WalkDir(rootAbs, func(path string, d iofs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != rootAbs && graph.IsSkippedDir(d.Name()) {
			return iofs.SkipDir
		}
		return watcher.Add(path)
	})
}

// watchIndexPass runs one diff-and-apply iteration: hashes the disk,
// diffs against prevHashes, mutates project for any changes, saves to
// s, and prints a one-line summary. *prevHashes is replaced with the
// fresh hash table when anything actually changed.
func watchIndexPass(stdout, stderr io.Writer, s *store.Store, project *graph.Project, prevHashes *map[string]string, root, tsconfig string) error {
	diskHashes, err := graph.HashSourceFiles(root)
	if err != nil {
		return err
	}
	added, changed, removed, unchanged := diffByHash(diskHashes, *prevHashes)

	for _, f := range removed {
		graph.RemoveFile(project, f)
	}
	if len(added) > 0 || len(changed) > 0 {
		if err := graph.UpdateFiles(project, tsconfig, append(append([]string{}, added...), changed...)); err != nil {
			return err
		}
	}
	if len(added) > 0 || len(changed) > 0 || len(removed) > 0 {
		graph.ResolveCalls(project)
		if err := s.SaveWithHashes(project, diskHashes); err != nil {
			return err
		}
		*prevHashes = diskHashes
	}
	emitWarnings(stderr, project)
	fmt.Fprintf(stdout, "indexed: %d added, %d changed, %d removed, %d unchanged\n",
		len(added), len(changed), len(removed), unchanged)
	return nil
}
