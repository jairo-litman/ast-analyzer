package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveDestructure(t *testing.T) *Project {
	t.Helper()
	root := "testdata/destructure"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_destructure_shorthand confirms shorthand object
// destructuring (`const { ctx, sut, asset } = setup()`) populates
// LocalTypes for each destructured key.
func TestResolveCalls_destructure_shorthand(t *testing.T) {
	p := buildAndResolveDestructure(t)

	tcHandle := lookupMethodSymbolID(t, p, "types.ts", "TestContext", "handle")
	svcHandle := lookupMethodSymbolID(t, p, "types.ts", "Service", "handle")
	assetRename := lookupMethodSymbolID(t, p, "types.ts", "Asset", "rename")
	runShorthand := lookupSymbolID(t, p, "spec.ts", "runShorthand")

	var ctxCall, sutCall, assetCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID != runShorthand {
			continue
		}
		switch {
		case c.Callee == "handle" && c.Receiver == "ctx":
			ctxCall = c
		case c.Callee == "handle" && c.Receiver == "sut":
			sutCall = c
		case c.Callee == "rename" && c.Receiver == "asset":
			assetCall = c
		}
	}
	require.NotNil(t, ctxCall)
	require.NotNil(t, sutCall)
	require.NotNil(t, assetCall)
	assert.Equal(t, []string{tcHandle}, ctxCall.ResolvedTo)
	assert.Equal(t, []string{svcHandle}, sutCall.ResolvedTo)
	assert.Equal(t, []string{assetRename}, assetCall.ResolvedTo)
}

// TestResolveCalls_destructure_awaited confirms destructuring from
// an awaited factory works via Promise<SetupBag> unwrapping.
func TestResolveCalls_destructure_awaited(t *testing.T) {
	p := buildAndResolveDestructure(t)

	tcHandle := lookupMethodSymbolID(t, p, "types.ts", "TestContext", "handle")
	assetRename := lookupMethodSymbolID(t, p, "types.ts", "Asset", "rename")
	runAwaited := lookupSymbolID(t, p, "spec.ts", "runAwaited")

	var ctxCall, assetCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID != runAwaited {
			continue
		}
		switch {
		case c.Callee == "handle" && c.Receiver == "ctx":
			ctxCall = c
		case c.Callee == "rename" && c.Receiver == "asset":
			assetCall = c
		}
	}
	require.NotNil(t, ctxCall)
	require.NotNil(t, assetCall)
	assert.Equal(t, []string{tcHandle}, ctxCall.ResolvedTo)
	assert.Equal(t, []string{assetRename}, assetCall.ResolvedTo)
}

// TestResolveCalls_destructure_renamed pins the `{ source: local }`
// renaming form: a's type comes from SetupBag.asset, not from a.
func TestResolveCalls_destructure_renamed(t *testing.T) {
	p := buildAndResolveDestructure(t)

	assetRename := lookupMethodSymbolID(t, p, "types.ts", "Asset", "rename")
	runRenamed := lookupSymbolID(t, p, "spec.ts", "runRenamed")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == runRenamed && c.Callee == "rename" && c.Receiver == "a" {
			call = c
			break
		}
	}
	require.NotNil(t, call)
	assert.Equal(t, []string{assetRename}, call.ResolvedTo)
}
