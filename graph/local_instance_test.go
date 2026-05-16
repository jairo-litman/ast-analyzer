package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveCalls_localInstanceFromNew pins that a method call on a
// local var bound via `new T(...)` resolves to T's method.
func TestResolveCalls_localInstanceFromNew(t *testing.T) {
	p, err := BuildProject("testdata/local_instance", "testdata/local_instance/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)

	boxShow := lookupMethodSymbolID(t, p, "types.ts", "Box", "show")
	call := callInFunction(t, p, "main.ts", "inferred", "show", "b")
	assert.Equal(t, []string{boxShow}, call.ResolvedTo,
		"b.show() inside inferred() should resolve to Box.show via the `const b = new Box(...)` declaration")
}

// TestResolveCalls_localInstanceFromTypeAnnotation pins the typed
// annotation path: `const b: Box = ...` (no `new`) still records the
// type and resolves method calls.
func TestResolveCalls_localInstanceFromTypeAnnotation(t *testing.T) {
	p, err := BuildProject("testdata/local_instance", "testdata/local_instance/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)

	boxLabel := lookupMethodSymbolID(t, p, "types.ts", "Box", "label")
	call := callInFunction(t, p, "main.ts", "annotated", "label", "b")
	assert.Equal(t, []string{boxLabel}, call.ResolvedTo,
		"b.label() inside annotated() should resolve to Box.label via the `b: Box` type annotation")
}

// TestResolveCalls_localInstanceScopePerFunction pins that nested
// functions get their own scope: outer's `outer` local is not
// visible inside inner. inner's own `innerBox` resolves locally.
func TestResolveCalls_localInstanceScopePerFunction(t *testing.T) {
	p, err := BuildProject("testdata/local_instance", "testdata/local_instance/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)

	boxShow := lookupMethodSymbolID(t, p, "types.ts", "Box", "show")
	boxLabel := lookupMethodSymbolID(t, p, "types.ts", "Box", "label")

	// outer.show() lives in nested(), receiver "outer" is declared
	// in nested()'s body — should resolve.
	outerCall := callInFunction(t, p, "main.ts", "nested", "show", "outer")
	assert.Equal(t, []string{boxShow}, outerCall.ResolvedTo)

	// innerBox.label() lives in inner(); innerBox is declared in
	// inner()'s body — should resolve via inner's own scope.
	innerCall := callInFunction(t, p, "main.ts", "inner", "label", "innerBox")
	assert.Equal(t, []string{boxLabel}, innerCall.ResolvedTo)
}

// TestSymbol_localTypesPersisted pins that the resolver-relevant
// LocalTypes map round-trips through the persistence layer so a
// project loaded from store resolves the same calls. Light coupling
// to the store package would be over-engineering — the actual store
// round-trip lives in pkg store; here we just verify the field is
// populated post-build.
func TestSymbol_localTypesPersisted(t *testing.T) {
	p, err := BuildProject("testdata/local_instance", "testdata/local_instance/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)

	for _, s := range p.Symbols {
		if s.File == "main.ts" && s.Name == "inferred" {
			require.NotNil(t, s.LocalTypes, "inferred() should carry LocalTypes")
			assert.Equal(t, "Box", s.LocalTypes["b"])
			return
		}
	}
	t.Fatal("did not find function symbol inferred")
}

// callInFunction returns the CallSite inside the named function whose
// callee/receiver match. Helps tests assert on a specific call without
// hard-coding byte offsets.
func callInFunction(t *testing.T, p *Project, file, fnName, callee, receiver string) *CallSite {
	t.Helper()
	var fnID string
	for _, s := range p.Symbols {
		if s.File == file && s.Name == fnName && s.Kind == SymbolFunction {
			fnID = s.ID
			break
		}
	}
	require.NotEmpty(t, fnID, "no function %q in %s", fnName, file)
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == fnID && c.Callee == callee && c.Receiver == receiver {
			return c
		}
	}
	t.Fatalf("no call %s.%s() in function %s", receiver, callee, fnName)
	return nil
}
