package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveStaticMethod(t *testing.T) *Project {
	t.Helper()
	root := "testdata/static_method"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_staticMethod_crossFile confirms that
// `ClassName.method()` resolves to the class's method when ClassName
// is imported from another file.
func TestResolveCalls_staticMethod_crossFile(t *testing.T) {
	p := buildAndResolveStaticMethod(t)

	pathUtilJoin := lookupMethodSymbolID(t, p, "util.ts", "PathUtil", "join")
	pathUtilNormalize := lookupMethodSymbolID(t, p, "util.ts", "PathUtil", "normalize")
	buildPath := lookupSymbolID(t, p, "consumer.ts", "buildPath")

	var joinCall, normalizeCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID != buildPath {
			continue
		}
		switch c.Callee {
		case "join":
			joinCall = c
		case "normalize":
			normalizeCall = c
		}
	}
	require.NotNil(t, joinCall)
	require.NotNil(t, normalizeCall)
	assert.Equal(t, []string{pathUtilJoin}, joinCall.ResolvedTo,
		"PathUtil.join() must resolve to PathUtil.join across files")
	assert.Equal(t, []string{pathUtilNormalize}, normalizeCall.ResolvedTo,
		"PathUtil.normalize() must resolve to PathUtil.normalize across files")
}

// TestResolveCalls_staticMethod_sameFile confirms resolution when the
// class is declared in the same file as the caller.
func TestResolveCalls_staticMethod_sameFile(t *testing.T) {
	p := buildAndResolveStaticMethod(t)

	counterNext := lookupMethodSymbolID(t, p, "consumer.ts", "Counter", "next")
	nextId := lookupSymbolID(t, p, "consumer.ts", "nextId")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == nextId && c.Callee == "next" && c.Receiver == "Counter" {
			call = c
			break
		}
	}
	require.NotNil(t, call)
	assert.Equal(t, []string{counterNext}, call.ResolvedTo,
		"Counter.next() must resolve to Counter.next in the same file")
}
