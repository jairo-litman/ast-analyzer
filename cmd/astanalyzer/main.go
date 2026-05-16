// astanalyzer is the command-line front-end for the ast-analyzer
// library. The dispatcher and all subcommands live in the cli package.
package main

import (
	"os"

	"github.com/jairo-litman/ast-analyzer/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
