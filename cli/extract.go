package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/jairo-litman/ast-analyzer/pruner"
)

func runExtract(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: astanalyzer extract [--db <path> | --rebuild --tsconfig <path>] [--format json|redacted|markdown] [--caller-depth N] [--callee-depth N] [--caller-bodies-up-to N] [--callee-bodies-up-to N] [--type-depth N] [--max-per-level N] <root> <symbolID>")
		fs.PrintDefaults()
	}
	dbPath := fs.String("db", "", "path to the SQLite index (default: <root>/"+defaultDBSubpath+")")
	rebuild := fs.Bool("rebuild", false, "build the project from source instead of loading the index")
	tsconfig := fs.String("tsconfig", "", "path to tsconfig.json (required with --rebuild)")
	format := fs.String("format", "json", "output format: json | redacted | markdown")
	noStaleCheck := fs.Bool("no-stale-check", false, "skip the on-disk hash check that warns when the index is out of date")

	defaults := pruner.DefaultExtractOptions()
	callerDepth := fs.Int("caller-depth", defaults.CallerDepth, "BFS depth for callers (0 = none, 1 = direct, N = N hops)")
	calleeDepth := fs.Int("callee-depth", defaults.CalleeDepth, "BFS depth for callees (0 = none, 1 = direct, N = N hops)")
	callerBodyDepth := fs.Int("caller-bodies-up-to", defaults.CallerBodyDepth, "include full bodies for callers up to this depth; beyond, signatures only")
	calleeBodyDepth := fs.Int("callee-bodies-up-to", defaults.CalleeBodyDepth, "include full bodies for callees up to this depth; beyond, signatures only")
	maxPerLevel := fs.Int("max-per-level", defaults.MaxPerLevel, "cap entries kept at each BFS level (0 = no cap)")
	typeDepth := fs.Int("type-depth", defaults.TypeDepth, "BFS depth for type references (0 = none, 1 = direct, N = N hops)")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("extract requires two positional arguments: <root> <symbolID>")
	}
	if *format != "json" && *format != "redacted" && *format != "markdown" {
		return fmt.Errorf("--format must be 'json', 'redacted', or 'markdown', got %q", *format)
	}
	if *callerDepth < 0 || *calleeDepth < 0 || *callerBodyDepth < 0 || *calleeBodyDepth < 0 || *maxPerLevel < 0 || *typeDepth < 0 {
		return errors.New("depth and cap flags must be non-negative")
	}

	root := fs.Arg(0)
	symbolID := fs.Arg(1)

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

	ctx, err := pruner.ExtractWithOptions(p, symbolID, pruner.ExtractOptions{
		CallerDepth:     *callerDepth,
		CalleeDepth:     *calleeDepth,
		CallerBodyDepth: *callerBodyDepth,
		CalleeBodyDepth: *calleeBodyDepth,
		MaxPerLevel:     *maxPerLevel,
		TypeDepth:       *typeDepth,
	})
	if err != nil {
		return err
	}

	switch *format {
	case "redacted":
		out, err := pruner.RenderRedacted(ctx, p)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(stdout, out)
		return err
	case "markdown":
		out, err := pruner.RenderMarkdown(ctx, p)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(stdout, out)
		return err
	default:
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(ctx)
	}
}
