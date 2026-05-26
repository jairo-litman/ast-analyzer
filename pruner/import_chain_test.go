package pruner

import (
	"testing"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findChain returns the chain (if any) leading from importingFile to
// the symbol with the given (file, name). Test-only helper.
func findChain(chains []ImportChain, importingFile, targetName string) (ImportChain, bool) {
	for _, c := range chains {
		if c.ImportingFile == importingFile && c.TargetName == targetName {
			return c, true
		}
	}
	return ImportChain{}, false
}

func buildChainForTarget(t *testing.T, p *graph.Project, importingFile, targetFile, targetName string) (ImportChain, bool) {
	t.Helper()
	var target graph.Symbol
	for _, s := range p.Symbols {
		if s.File == targetFile && s.Name == targetName {
			target = s
			break
		}
	}
	require.NotEmpty(t, target.ID, "fixture must contain symbol %s in %s", targetName, targetFile)
	return buildImportChain(p, importingFile, target)
}

func TestImportChain_directNamedImport(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	chain, ok := buildChainForTarget(t, p, "main.ts", "helper.ts", "add")
	require.True(t, ok, "expected chain for direct named import")
	assert.Equal(t, "main.ts", chain.ImportingFile)
	assert.Equal(t, "add", chain.LocalName)
	assert.Equal(t, "add", chain.TargetName)
	if assert.Len(t, chain.Trail, 1) {
		assert.Equal(t, "main.ts", chain.Trail[0].File)
		assert.Equal(t, "import", chain.Trail[0].Kind)
		assert.Contains(t, chain.Trail[0].Source, "import")
	}
}

func TestImportChain_directAliasedImport(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	chain, ok := buildChainForTarget(t, p, "main.ts", "helper.ts", "multiply")
	require.True(t, ok, "expected chain for aliased import")
	assert.Equal(t, "times", chain.LocalName,
		"local name in main.ts is the alias used at import: `import { multiply as times }`")
	assert.Equal(t, "multiply", chain.TargetName)
	assert.Len(t, chain.Trail, 1)
}

func TestImportChain_directDefaultImport(t *testing.T) {
	p := buildAndResolve(t, "default_export")
	chain, ok := buildChainForTarget(t, p, "main.ts", "widget.ts", "Widget")
	require.True(t, ok, "expected chain for default import")
	assert.Equal(t, "Widget", chain.LocalName)
	assert.Equal(t, "Widget", chain.TargetName)
	assert.Len(t, chain.Trail, 1)
}

func TestImportChain_sameFileNoChain(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	_, ok := buildChainForTarget(t, p, "helper.ts", "helper.ts", "add")
	assert.False(t, ok, "same-file references should produce no chain")
}

func TestImportChain_namedReExport(t *testing.T) {
	p := buildAndResolve(t, "reexport")
	chain, ok := buildChainForTarget(t, p, "main.ts", "helpers/math.ts", "add")
	require.True(t, ok)
	assert.Equal(t, "add", chain.LocalName)
	if assert.Len(t, chain.Trail, 2, "import in main + re-export in helpers/index") {
		assert.Equal(t, "main.ts", chain.Trail[0].File)
		assert.Equal(t, "import", chain.Trail[0].Kind)
		assert.Equal(t, "helpers/index.ts", chain.Trail[1].File)
		assert.Equal(t, "re-export", chain.Trail[1].Kind)
	}
}

func TestImportChain_aliasedReExport(t *testing.T) {
	p := buildAndResolve(t, "reexport")
	chain, ok := buildChainForTarget(t, p, "main.ts", "helpers/math.ts", "multiply")
	require.True(t, ok, "multiply is re-exported as `times`")
	assert.Equal(t, "times", chain.LocalName,
		"local name is the alias from the re-export, propagated through the import")
	assert.Equal(t, "multiply", chain.TargetName)
	if assert.Len(t, chain.Trail, 2) {
		assert.Contains(t, chain.Trail[1].Source, "multiply as times")
	}
}

func TestImportChain_starReExport(t *testing.T) {
	p := buildAndResolve(t, "reexport")
	chain, ok := buildChainForTarget(t, p, "main.ts", "nested/inner.ts", "deep")
	require.True(t, ok, "star re-export should be traceable")
	assert.Equal(t, "deep", chain.LocalName)
	if assert.Len(t, chain.Trail, 2) {
		assert.Equal(t, "nested/index.ts", chain.Trail[1].File)
		assert.Contains(t, chain.Trail[1].Source, "export *")
	}
}

func TestImportChain_defaultAsNamedReExport(t *testing.T) {
	p := buildAndResolve(t, "reexport")
	chain, ok := buildChainForTarget(t, p, "main.ts", "helpers/cool.ts", "cool")
	require.True(t, ok, "export { default as Cool } should be traceable")
	assert.Equal(t, "Cool", chain.LocalName)
	if assert.Len(t, chain.Trail, 2) {
		assert.Contains(t, chain.Trail[1].Source, "default as Cool")
	}
}

func TestImportChain_endToEndRenderingAliasedReExport(t *testing.T) {
	p := buildAndResolve(t, "reexport")
	var multiply graph.Symbol
	for _, s := range p.Symbols {
		if s.File == "helpers/math.ts" && s.Name == "multiply" {
			multiply = s
			break
		}
	}
	require.NotEmpty(t, multiply.ID)

	ctx, err := Extract(p, multiply.ID)
	require.NoError(t, err)

	chain, ok := findChain(ctx.ImportChains, "main.ts", "multiply")
	require.True(t, ok, "extraction should record main.ts → multiply via re-export")
	assert.Equal(t, "times", chain.LocalName)
	assert.Len(t, chain.Trail, 2)

	out, err := RenderRedacted(ctx, p)
	require.NoError(t, err)

	assert.Contains(t, out, `import { times } from "./helpers"`,
		"caller file should include its import line bringing `times` into scope")
	assert.Contains(t, out, `export { multiply as times } from "./math"`,
		"re-export hop file should appear with its rename statement")
	assert.Contains(t, out, "# helpers/index.ts",
		"re-export hop file should get its own section header")
	assert.Contains(t, out, "// chains:",
		"metadata block should announce the chain")
	assert.Contains(t, out, "times -> multiply",
		"chain summary should show the local-to-canonical rename")
}

func TestImportChain_unresolvableReturnsFalse(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	// `add` is not imported into helper.ts from any other file,
	// so asking for the chain from "helper.ts" looking back at
	// itself yields no chain (same-file is the rule), and asking
	// from a file that doesn't import it should yield no chain.
	mainSym := graph.Symbol{File: "main.ts", Name: "main"}
	_, ok := buildImportChain(p, "helper.ts", mainSym)
	assert.False(t, ok, "no import statement in helper.ts brings `main` into scope")
}
