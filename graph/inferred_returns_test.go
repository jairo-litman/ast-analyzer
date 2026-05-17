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

// TestResolveCalls_inferredReturn_newT pins that an un-annotated
// `return new T(...)` body gains ReturnType T so `a = factory();
// a.method()` resolves.
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

// TestResolveCalls_inferredReturn_objectLiteral pins that an
// un-annotated function returning `{ asset: new Asset(), service:
// new Service(), helper }` exposes per-property types so
// destructuring on the caller resolves.
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
