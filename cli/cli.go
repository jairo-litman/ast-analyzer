// Package cli is the command-line front-end for ast-analyzer. It
// exposes three subcommands — list, extract, index — through the Run
// dispatcher.
package cli

import (
	"fmt"
	"io"
	"sort"
)

// Run dispatches the named subcommand and returns:
//
//   - 0 on success (including explicit `--help` / `-h` / `help`)
//   - 1 on a subcommand-reported error
//   - 2 on a usage error (missing or unknown command name)
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	name := args[0]
	if name == "--help" || name == "-h" || name == "help" {
		printUsage(stdout)
		return 0
	}

	cmd, ok := commands[name]
	if !ok {
		fmt.Fprintf(stderr, "unknown command %q\n\n", name)
		printUsage(stderr)
		return 2
	}

	if err := cmd.run(args[1:], stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// command pairs a subcommand's metadata with its run function.
type command struct {
	name    string
	summary string
	run     func(args []string, stdout, stderr io.Writer) error
}

var commands = map[string]command{
	"list": {
		name:    "list",
		summary: "list every symbol in the project (id, kind, name, file)",
		run:     runList,
	},
	"extract": {
		name:    "extract",
		summary: "build the project and emit a context payload (JSON) for a target symbol",
		run:     runExtract,
	},
	"index": {
		name:    "index",
		summary: "build the project and persist the graph to a SQLite database",
		run:     runIndex,
	},
	"watch": {
		name:    "watch",
		summary: "keep the index up to date by re-running on every source change",
		run:     runWatch,
	},
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: astanalyzer <command> [flags] <args>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")

	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  %-8s  %s\n", name, commands[name].summary)
	}
}
