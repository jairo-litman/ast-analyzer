package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveInferredReturns(t *testing.T) *Project {
	t.Helper()
	root := "testdata/inferred_returns"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_inferredReturn_newT pins Alt-2: a function with
// no explicit return type whose body is a single `return new T(...)`
// gains an inferred ReturnType of `T`, so consumers can chain
// `const a = factory(); a.method()`.
func TestResolveCalls_inferredReturn_newT(t *testing.T) {
	p := buildAndResolveInferredReturns(t)

	assetRename := lookupMethodSymbolID(t, p, "types.ts", "Asset", "rename")
	useMake := lookupSymbolID(t, p, "spec.ts", "useMake")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == useMake && c.Callee == "rename" && c.Receiver == "a" {
			call = c
			break
		}
	}
	require.NotNil(t, call)
	assert.Equal(t, []string{assetRename}, call.ResolvedTo,
		"a.rename() must resolve via inferred return type Asset of makeAsset()")
}

// TestResolveCalls_inferredReturn_objectLiteral pins Alt-3: a
// function with no explicit return type that returns an object
// literal exposes per-property types, so destructuring resolves.
// Three key shapes:
//   - { asset: new Asset() }   — new-expression rhs
//   - { service: new Service() } — same
//   - { helper }               — shorthand for a local var typed
//     via new in the same body
func TestResolveCalls_inferredReturn_objectLiteral(t *testing.T) {
	p := buildAndResolveInferredReturns(t)

	assetRename := lookupMethodSymbolID(t, p, "types.ts", "Asset", "rename")
	svcHandle := lookupMethodSymbolID(t, p, "types.ts", "Service", "handle")
	useSetup := lookupSymbolID(t, p, "spec.ts", "useSetup")

	var assetCall, svcCall, helperCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID != useSetup {
			continue
		}
		switch {
		case c.Receiver == "asset" && c.Callee == "rename":
			assetCall = c
		case c.Receiver == "service" && c.Callee == "handle":
			svcCall = c
		case c.Receiver == "helper" && c.Callee == "handle":
			helperCall = c
		}
	}
	require.NotNil(t, assetCall)
	require.NotNil(t, svcCall)
	require.NotNil(t, helperCall)
	assert.Equal(t, []string{assetRename}, assetCall.ResolvedTo)
	assert.Equal(t, []string{svcHandle}, svcCall.ResolvedTo)
	assert.Equal(t, []string{svcHandle}, helperCall.ResolvedTo,
		"shorthand { helper } must inherit helper's local type (Service)")
}
