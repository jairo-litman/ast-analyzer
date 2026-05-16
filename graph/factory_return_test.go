package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveFactoryReturn(t *testing.T) *Project {
	t.Helper()
	root := "testdata/factory_return"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_factoryReturn_directName pins the core case:
// `const x = factory(); x.method()` resolves x's type via the
// factory function's declared return type.
func TestResolveCalls_factoryReturn_directName(t *testing.T) {
	p := buildAndResolveFactoryReturn(t)

	boxOpen := lookupMethodSymbolID(t, p, "box.ts", "Box", "open")
	useBox := lookupSymbolID(t, p, "consumer.ts", "useBox")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == useBox && c.Callee == "open" && c.Receiver == "box" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "useBox must contain a box.open() call site")
	assert.Equal(t, []string{boxOpen}, call.ResolvedTo,
		"box.open() must resolve to Box.open via makeBox's `: Box` return type")
}

// TestResolveCalls_factoryReturn_aliasedImport confirms the
// resolution survives an `import { makeBox as buildBox } from ...`
// rename — the callee identifier in source is `buildBox`, but it
// resolves to `makeBox`'s symbol which has the `Box` return type.
func TestResolveCalls_factoryReturn_aliasedImport(t *testing.T) {
	p := buildAndResolveFactoryReturn(t)

	boxOpen := lookupMethodSymbolID(t, p, "box.ts", "Box", "open")
	useAliased := lookupSymbolID(t, p, "consumer.ts", "useAliased")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == useAliased && c.Callee == "open" && c.Receiver == "b" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "useAliased must contain a b.open() call site")
	assert.Equal(t, []string{boxOpen}, call.ResolvedTo,
		"b.open() must resolve to Box.open via the aliased makeBox import")
}

// TestResolveCalls_factoryReturn_promiseUnwrapped pins that
// `Promise<Box>` is unwrapped to `Box` during type resolution, so
// `await makeBoxAsync(...).open()` resolves to Box.open. Same
// rule applies whether or not the variable was actually awaited:
// TypeScript treats Promise<T> as transparent for our purposes.
func TestResolveCalls_factoryReturn_promiseUnwrapped(t *testing.T) {
	p := buildAndResolveFactoryReturn(t)

	boxOpen := lookupMethodSymbolID(t, p, "box.ts", "Box", "open")
	useAsync := lookupSymbolID(t, p, "consumer.ts", "useAsync")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == useAsync && c.Callee == "open" && c.Receiver == "box" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "useAsync must contain a box.open() call site")
	assert.Equal(t, []string{boxOpen}, call.ResolvedTo,
		"await makeBoxAsync().open() must resolve to Box.open via Promise<Box> unwrapping")
}
