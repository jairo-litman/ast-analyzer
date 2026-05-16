package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildProject_attributesModuleCalls pins the synthetic
// "<file>#module" symbol for module-scope calls.
func TestBuildProject_attributesModuleCalls(t *testing.T) {
	p, err := BuildProject("testdata/module", "testdata/module/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)

	moduleID := lookupModuleSymbolID(t, p, "main.ts")

	moduleSym := lookupSymbolByID(t, p, moduleID)
	assert.Equal(t, SymbolModule, moduleSym.Kind)
	assert.Equal(t, "main.ts", moduleSym.Name)
	assert.Equal(t, "main.ts", moduleSym.File)
	assert.Equal(t, uint(0), moduleSym.StartByte)
	assert.Greater(t, moduleSym.EndByte, uint(0))

	// initLogging(), add(1, 2), and console.log(result) all live at
	// module scope; the inner-function calls do not.
	var moduleCallees []string
	for _, c := range p.Calls {
		if c.CallerID == moduleID {
			moduleCallees = append(moduleCallees, c.Callee)
		}
	}
	assert.ElementsMatch(t, []string{"initLogging", "add", "log"}, moduleCallees)
}

// TestBuildProject_innermostFunctionWins pins call attribution to
// the smallest enclosing function for nested declarations.
func TestBuildProject_innermostFunctionWins(t *testing.T) {
	p, err := BuildProject("testdata/module", "testdata/module/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)

	outerID := lookupSymbolID(t, p, "main.ts", "outer")
	innerID := lookupSymbolID(t, p, "main.ts", "inner")

	// `innerCall()` is inside `inner`'s body, which itself sits inside
	// `outer`'s body. The smaller enclosing range is `inner`.
	innerCall := findCallSiteByCallee(t, p, "main.ts", "innerCall")
	assert.Equal(t, innerID, innerCall.CallerID)

	// `inner()` is invoked from `outer`'s body but is not contained in
	// `inner`'s own range, so the attribution belongs to `outer`.
	innerInvoke := findCallSiteByCallee(t, p, "main.ts", "inner")
	assert.Equal(t, outerID, innerInvoke.CallerID)
}

// TestBuildProject_skipsModuleSymbolWhenNoTopLevelCalls pins that a
// pure-declaration file gets no synthetic module symbol.
func TestBuildProject_skipsModuleSymbolWhenNoTopLevelCalls(t *testing.T) {
	p, err := BuildProject("testdata/module", "testdata/module/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)

	for _, s := range p.Symbols {
		if s.File == "helper.ts" && s.Kind == SymbolModule {
			t.Fatalf("helper.ts should not have a module symbol, got %+v", s)
		}
	}
}

func lookupSymbolByID(t *testing.T, p *Project, id string) Symbol {
	t.Helper()
	for _, s := range p.Symbols {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no symbol with ID %q", id)
	return Symbol{}
}

func lookupModuleSymbolID(t *testing.T, p *Project, file string) string {
	t.Helper()
	for _, s := range p.Symbols {
		if s.File == file && s.Kind == SymbolModule {
			return s.ID
		}
	}
	t.Fatalf("no module symbol for file %q", file)
	return ""
}

func findCallSiteByCallee(t *testing.T, p *Project, file, callee string) CallSite {
	t.Helper()
	for _, c := range p.Calls {
		if c.File == file && c.Callee == callee {
			return c
		}
	}
	t.Fatalf("no call to %q in %s", callee, file)
	return CallSite{}
}
