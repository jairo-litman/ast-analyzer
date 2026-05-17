package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveFactoryMethod(t *testing.T) *Project {
	t.Helper()
	root := "testdata/factory_method"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_methodReturn_chained pins a two-hop chain:
// `ctx = newContext(); user = ctx.newUser(); user.rename()`
// resolves to User.rename.
func TestResolveCalls_methodReturn_chained(t *testing.T) {
	p := buildAndResolveFactoryMethod(t)

	userRename := lookupMethodSymbolID(t, p, "types.ts", "User", "rename")
	runTest := lookupSymbolID(t, p, "spec.ts", "runTest")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == runTest && c.Callee == "rename" && c.Receiver == "user" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "runTest must contain a user.rename() call site")
	assert.Equal(t, []string{userRename}, call.ResolvedTo,
		"user.rename() must resolve to User.rename via the const user = ctx.newUser() binding")
}

// TestResolveCalls_methodReturn_promiseUnwrapped pins that
// `u = await ctx.newAsync(); u.rename()` unwraps Promise<User> to
// User and resolves through method-on-receiver enrichment.
func TestResolveCalls_methodReturn_promiseUnwrapped(t *testing.T) {
	p := buildAndResolveFactoryMethod(t)

	userRename := lookupMethodSymbolID(t, p, "types.ts", "User", "rename")
	runAsync := lookupSymbolID(t, p, "spec.ts", "runAsync")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == runAsync && c.Callee == "rename" && c.Receiver == "u" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "runAsync must contain a u.rename() call site")
	assert.Equal(t, []string{userRename}, call.ResolvedTo,
		"u.rename() through Promise<User> must resolve to User.rename")
}
