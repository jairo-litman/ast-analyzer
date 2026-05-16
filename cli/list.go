package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jairo-litman/ast-analyzer/graph"
)

func runList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: astanalyzer list [--db <path> | --rebuild --tsconfig <path>] [--kind ...] [--file <re>] [--name <re>] <root>")
		fs.PrintDefaults()
	}
	dbPath := fs.String("db", "", "path to the SQLite index (default: <root>/"+defaultDBSubpath+")")
	rebuild := fs.Bool("rebuild", false, "build the project from source instead of loading the index")
	tsconfig := fs.String("tsconfig", "", "path to tsconfig.json (required with --rebuild)")
	noStaleCheck := fs.Bool("no-stale-check", false, "skip the on-disk hash check that warns when the index is out of date")
	kindList := fs.String("kind", "", "comma-separated list of kinds to include (function, class, interface, enum, type_alias, module)")
	fileRe := fs.String("file", "", "regex matched against the file column")
	nameRe := fs.String("name", "", "regex matched against the symbol name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("list requires exactly one positional argument: <root>")
	}
	root := fs.Arg(0)

	filter, err := newSymbolFilter(*kindList, *fileRe, *nameRe)
	if err != nil {
		return err
	}

	h, err := openProject(root, *dbPath, *tsconfig, *rebuild)
	if err != nil {
		return err
	}
	defer h.Close()
	emitWarnings(stderr, h.Project)
	if !*noStaleCheck {
		if err := warnIfStale(stderr, h, root); err != nil {
			return err
		}
	}
	p := h.Project

	syms := append([]graph.Symbol(nil), p.Symbols...)
	sort.Slice(syms, func(i, j int) bool {
		if syms[i].File != syms[j].File {
			return syms[i].File < syms[j].File
		}
		return syms[i].StartByte < syms[j].StartByte
	})

	w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tKIND\tNAME\tFILE")
	for _, s := range syms {
		if !filter.matches(s) {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.Kind, s.Name, s.File)
	}
	return w.Flush()
}

// validKinds is the set of SymbolKind strings the --kind filter
// accepts. Bound to the values BuildProject actually emits.
var validKinds = map[string]graph.SymbolKind{
	string(graph.SymbolFunction):  graph.SymbolFunction,
	string(graph.SymbolClass):     graph.SymbolClass,
	string(graph.SymbolInterface): graph.SymbolInterface,
	string(graph.SymbolEnum):      graph.SymbolEnum,
	string(graph.SymbolTypeAlias): graph.SymbolTypeAlias,
	string(graph.SymbolModule):    graph.SymbolModule,
}

// symbolFilter applies the --kind / --file / --name flags as an AND
// across each Symbol. A nil filter matches everything.
type symbolFilter struct {
	kinds map[graph.SymbolKind]bool
	file  *regexp.Regexp
	name  *regexp.Regexp
}

func newSymbolFilter(kindList, fileRe, nameRe string) (*symbolFilter, error) {
	f := &symbolFilter{}

	if kindList != "" {
		f.kinds = map[graph.SymbolKind]bool{}
		for _, raw := range strings.Split(kindList, ",") {
			k := strings.TrimSpace(raw)
			if k == "" {
				continue
			}
			kind, ok := validKinds[k]
			if !ok {
				return nil, fmt.Errorf("--kind %q is not one of function, class, interface, enum, type_alias, module", k)
			}
			f.kinds[kind] = true
		}
	}
	if fileRe != "" {
		re, err := regexp.Compile(fileRe)
		if err != nil {
			return nil, fmt.Errorf("--file regex: %w", err)
		}
		f.file = re
	}
	if nameRe != "" {
		re, err := regexp.Compile(nameRe)
		if err != nil {
			return nil, fmt.Errorf("--name regex: %w", err)
		}
		f.name = re
	}
	return f, nil
}

func (f *symbolFilter) matches(s graph.Symbol) bool {
	if f == nil {
		return true
	}
	if f.kinds != nil && !f.kinds[s.Kind] {
		return false
	}
	if f.file != nil && !f.file.MatchString(s.File) {
		return false
	}
	if f.name != nil && !f.name.MatchString(s.Name) {
		return false
	}
	return true
}

// openProject is the list/extract entry point. The rebuild and
// db-load paths are mutually exclusive.
func openProject(root, dbPath, tsconfig string, rebuild bool) (*projectHandle, error) {
	if rebuild {
		if dbPath != "" {
			return nil, errors.New("--db and --rebuild are mutually exclusive")
		}
		if tsconfig == "" {
			return nil, errors.New("--rebuild requires --tsconfig")
		}
		return rebuildProject(root, tsconfig)
	}
	if dbPath == "" {
		dbPath = defaultDBPath(root)
	}
	return loadProjectFromDB(root, dbPath)
}
