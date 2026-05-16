package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildProject(t *testing.T) {
	p, err := BuildProject("testdata/simple", "testdata/simple/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	require.NotNil(t, p)

	t.Run("walks all non-excluded TypeScript files", func(t *testing.T) {
		assert.Contains(t, p.Files, "main.ts")
		assert.Contains(t, p.Files, "helper.ts")
		assert.Len(t, p.Files, 2)
	})

	t.Run("emits one Symbol per top-level declaration", func(t *testing.T) {
		var names []string
		var kinds []SymbolKind
		for _, s := range p.Symbols {
			names = append(names, s.Name)
			kinds = append(kinds, s.Kind)
		}
		assert.ElementsMatch(t, []string{"main", "add", "Greeter", "greet"}, names)
		assert.ElementsMatch(t, []SymbolKind{
			SymbolFunction, SymbolFunction, SymbolFunction, SymbolClass,
		}, kinds)
	})

	t.Run("Symbol IDs are file-and-position keyed", func(t *testing.T) {
		seen := map[string]bool{}
		for _, s := range p.Symbols {
			assert.False(t, seen[s.ID], "Symbol ID %q is duplicated", s.ID)
			seen[s.ID] = true
		}
	})

	t.Run("call sites carry the enclosing CallerID", func(t *testing.T) {
		mainID := lookupSymbolID(t, p, "main.ts", "main")

		var fromMain []CallSite
		for _, c := range p.Calls {
			if c.CallerID == mainID {
				fromMain = append(fromMain, c)
			}
		}

		require.Len(t, fromMain, 2)

		callees := map[string]CallSite{}
		for _, c := range fromMain {
			callees[c.Callee] = c
		}

		addCall, ok := callees["add"]
		require.True(t, ok, "expected an `add` call from main")
		assert.Equal(t, "", addCall.Receiver)
		assert.Equal(t, "main.ts", addCall.File)

		logCall, ok := callees["log"]
		require.True(t, ok, "expected a `log` call from main")
		assert.Equal(t, "console", logCall.Receiver)
	})

	t.Run("relative imports resolve against the project root", func(t *testing.T) {
		var mainImports []ImportEdge
		for _, e := range p.Imports {
			if e.File == "main.ts" {
				mainImports = append(mainImports, e)
			}
		}
		require.Len(t, mainImports, 1)
		assert.Equal(t, "./helper", mainImports[0].Path)
		assert.Equal(t, "helper.ts", mainImports[0].Resolved,
			"resolver output should be relativized to the project root")
	})
}

func TestBuildProject_skipsExcludedDirs(t *testing.T) {
	for _, dir := range []string{"node_modules", ".git", "dist", "build"} {
		assert.True(t, IsSkippedDir(dir), "%q should be skipped", dir)
	}
	assert.False(t, IsSkippedDir("src"))
}

func lookupSymbolID(t *testing.T, p *Project, file, name string) string {
	t.Helper()
	for _, s := range p.Symbols {
		if s.File == file && s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no symbol %q in %s", name, file)
	return ""
}
