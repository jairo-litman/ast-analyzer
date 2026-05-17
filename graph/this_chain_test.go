package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveThisChain(t *testing.T) *Project {
	t.Helper()
	root := "testdata/this_chain"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_thisChain_twoSegments pins that
// `this.mid.leaf.sing()` walks two typed-field hops across files
// and resolves to Leaf.sing.
func TestResolveCalls_thisChain_twoSegments(t *testing.T) {
	p := buildAndResolveThisChain(t)

	leafSing := lookupMethodSymbolID(t, p, "inner.ts", "Leaf", "sing")
	outerPlay := lookupMethodSymbolID(t, p, "outer.ts", "Outer", "play")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == outerPlay && c.Callee == "sing" && c.Receiver == "this.mid.leaf" {
			call = c
			break
		}
	}
	require.NotNil(t, call, "Outer.play must contain a this.mid.leaf.sing() call site")
	assert.Equal(t, []string{leafSing}, call.ResolvedTo,
		"this.mid.leaf.sing() must walk two field hops and resolve to Leaf.sing")
}
