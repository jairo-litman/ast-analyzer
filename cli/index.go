package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
)

func runIndex(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: astanalyzer index --tsconfig <path> [--output <db>] <root>")
		fs.PrintDefaults()
	}
	tsconfig := fs.String("tsconfig", "", "path to tsconfig.json (required)")
	output := fs.String("output", "", "path to output SQLite database (default: <root>/"+defaultDBSubpath+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("index requires one positional argument: <root>")
	}
	if *tsconfig == "" {
		return errors.New("--tsconfig is required")
	}

	root := fs.Arg(0)
	dbPath := *output
	if dbPath == "" {
		dbPath = defaultDBPath(root)
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return fmt.Errorf("create index directory: %w", err)
	}

	diskHashes, err := graph.HashSourceFiles(root)
	if err != nil {
		return err
	}

	// One store handle covers load, save, and metadata throughout.
	// store.Open bootstraps the schema, so a fresh DB just yields an
	// empty Project on the load path.
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	prev, prevHashes, err := loadPreviousFromStore(s, root)
	if err != nil {
		return err
	}
	defer prev.Close()

	added, changed, removed, unchanged := diffByHash(diskHashes, prevHashes)
	for _, f := range removed {
		graph.RemoveFile(prev, f)
	}
	if err := graph.UpdateFiles(prev, *tsconfig, append(append([]string{}, added...), changed...)); err != nil {
		return err
	}
	graph.ResolveCalls(prev)
	emitWarnings(stderr, prev)

	if err := s.SaveWithHashes(prev, diskHashes); err != nil {
		return err
	}

	fmt.Fprintf(stdout,
		"indexed %d symbols, %d calls, %d imports → %s\n"+
			"files: %d added, %d changed, %d removed, %d unchanged\n",
		len(prev.Symbols), len(prev.Calls), len(prev.Imports), dbPath,
		len(added), len(changed), len(removed), unchanged)
	return nil
}

// loadPreviousFromStore reads whatever the store holds into a Project
// and per-file hash map. A freshly bootstrapped (empty) database
// yields an empty Project so the caller can treat every disk file as
// added without branching.
func loadPreviousFromStore(s *store.Store, root string) (*graph.Project, map[string]string, error) {
	p, err := s.Load()
	if err != nil {
		return nil, nil, err
	}
	hashes, err := s.LoadFileHashes()
	if err != nil {
		return nil, nil, err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, fmt.Errorf("absolute root: %w", err)
	}
	p.Root = rootAbs
	if p.Files == nil {
		p.Files = map[string]*graph.FileResult{}
	}
	return p, hashes, nil
}

// diffByHash classifies every path in disk vs prev as added, changed,
// removed, or unchanged. Returned slices are lex-sorted.
func diffByHash(disk, prev map[string]string) (added, changed, removed []string, unchanged int) {
	for f, h := range disk {
		ph, ok := prev[f]
		switch {
		case !ok:
			added = append(added, f)
		case ph != h:
			changed = append(changed, f)
		default:
			unchanged++
		}
	}
	for f := range prev {
		if _, ok := disk[f]; !ok {
			removed = append(removed, f)
		}
	}
	sort.Strings(added)
	sort.Strings(changed)
	sort.Strings(removed)
	return added, changed, removed, unchanged
}
