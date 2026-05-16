package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
)

// defaultDBSubpath is the project-relative path where the CLI looks
// for and writes the canonical SQLite index.
const defaultDBSubpath = ".astanalyzer/index.db"

// defaultDBPath returns the canonical index location for a project
// root.
func defaultDBPath(root string) string {
	return filepath.Join(root, defaultDBSubpath)
}

// projectHandle pairs a Project with the resources backing it (parse
// trees for rebuilt projects, a DB handle for loaded ones). Close
// releases both.
type projectHandle struct {
	Project *graph.Project
	store   *store.Store
}

func (h *projectHandle) Close() {
	if h == nil {
		return
	}
	if h.Project != nil {
		h.Project.Close()
	}
	if h.store != nil {
		_ = h.store.Close()
	}
}

// loadProjectFromDB opens the SQLite index at dbPath and rebuilds the
// Project. A missing file produces a user-facing error with a
// recovery hint.
func loadProjectFromDB(root, dbPath string) (*projectHandle, error) {
	if _, err := os.Stat(dbPath); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf(
			"no index found at %s — run `astanalyzer index %s` first, or pass --rebuild",
			dbPath, root)
	} else if err != nil {
		return nil, fmt.Errorf("stat %s: %w", dbPath, err)
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	p, err := s.Load()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return &projectHandle{Project: p, store: s}, nil
}

// rebuildProject runs BuildProject + ResolveCalls without persisting.
func rebuildProject(root, tsconfig string) (*projectHandle, error) {
	p, err := graph.BuildProject(root, tsconfig)
	if err != nil {
		return nil, err
	}
	graph.ResolveCalls(p)
	return &projectHandle{Project: p}, nil
}

// emitWarnings flushes any accumulated graph.Project warnings to
// stderr, one per line, then resets the slice so the next pass starts
// fresh (relevant for watch mode where the same Project sees many
// rounds of UpdateFiles).
func emitWarnings(stderr io.Writer, p *graph.Project) {
	if p == nil {
		return
	}
	for _, w := range p.Warnings {
		fmt.Fprintln(stderr, "warning:", w)
	}
	p.Warnings = nil
}

// warnIfStale compares the on-disk file hashes against the index's
// recorded hashes and writes a one-line warning to stderr if they
// disagree. No-op when h has no DB backing (rebuilt projects have
// nothing to drift from).
func warnIfStale(stderr io.Writer, h *projectHandle, root string) error {
	if h == nil || h.store == nil {
		return nil
	}
	diskHashes, err := graph.HashSourceFiles(root)
	if err != nil {
		return err
	}
	prevHashes, err := h.store.LoadFileHashes()
	if err != nil {
		return err
	}
	added, changed, removed, _ := diffByHash(diskHashes, prevHashes)
	if len(added) == 0 && len(changed) == 0 && len(removed) == 0 {
		return nil
	}
	fmt.Fprintf(stderr,
		"warning: index is stale (%d added, %d changed, %d removed); run `astanalyzer index` to refresh\n",
		len(added), len(changed), len(removed))
	return nil
}
