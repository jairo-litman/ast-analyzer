package pruner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// calleeNames collects the names of the callees in the Context.
func calleeNames(callees []Callee) []string {
	out := make([]string, 0, len(callees))
	for _, c := range callees {
		out = append(out, c.Symbol.Name)
	}
	return out
}

// callerNames collects the names of the callers in the Context.
func callerNames(callers []Caller) []string {
	out := make([]string, 0, len(callers))
	for _, c := range callers {
		out = append(out, c.Symbol.Name)
	}
	return out
}

// TestExtractWithOptions_defaultsMatchExtract pins that the default
// options reproduce the legacy Extract output for shape and depth.
func TestExtractWithOptions_defaultsMatchExtract(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	base, err := Extract(p, aID)
	require.NoError(t, err)

	ctx, err := ExtractWithOptions(p, aID, DefaultExtractOptions())
	require.NoError(t, err)

	assert.Equal(t, base.Target.Source, ctx.Target.Source)
	assert.Equal(t, len(base.Callees), len(ctx.Callees))
	assert.Equal(t, len(base.Callers), len(ctx.Callers))
	for _, c := range ctx.Callees {
		assert.Equal(t, 1, c.Depth, "default callees are depth-1")
		assert.NotEmpty(t, c.Body, "default callees carry body (matches legacy renderer)")
	}
}

func TestExtractWithOptions_calleeDepthZeroOmitsCallees(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	ctx, err := ExtractWithOptions(p, aID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 0,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)
	assert.Empty(t, ctx.Callees)
}

func TestExtractWithOptions_callerDepthZeroOmitsCallers(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	bID := symbolID(t, p, "b.ts", "b")

	ctx, err := ExtractWithOptions(p, bID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 1,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)
	assert.Empty(t, ctx.Callers)
}

// TestExtractWithOptions_calleeDepth2 confirms the BFS reaches the
// second level on the a -> b -> c -> d chain.
func TestExtractWithOptions_calleeDepth2(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	ctx, err := ExtractWithOptions(p, aID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 2,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"b", "c"}, calleeNames(ctx.Callees))
	for _, c := range ctx.Callees {
		switch c.Symbol.Name {
		case "b":
			assert.Equal(t, 1, c.Depth)
		case "c":
			assert.Equal(t, 2, c.Depth)
		}
	}
}

func TestExtractWithOptions_calleeDepth3CoversFullChain(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	ctx, err := ExtractWithOptions(p, aID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 3,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"b", "c", "d"}, calleeNames(ctx.Callees))
}

// TestExtractWithOptions_selfCallTerminates exercises the visited-set
// cycle protection: selfRecursive() calls itself.
func TestExtractWithOptions_selfCallTerminates(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	selfID := symbolID(t, p, "self.ts", "selfRecursive")

	ctx, err := ExtractWithOptions(p, selfID, ExtractOptions{
		CallerDepth: 5,
		CalleeDepth: 5,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)
	// Target itself shouldn't appear as its own callee/caller — the
	// visited set seeds the target before BFS starts.
	assert.Empty(t, ctx.Callees, "self-call must not be reported as a callee of itself")
	assert.Empty(t, ctx.Callers, "self-call must not be reported as a caller of itself")
}

// TestExtractWithOptions_calleeBodyDepth1 confirms that body inclusion
// is capped: depth 1 carries Body, depth 2 carries only Signature.
func TestExtractWithOptions_calleeBodyDepth1(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	ctx, err := ExtractWithOptions(p, aID, ExtractOptions{
		CallerDepth:     0,
		CalleeDepth:     2,
		CalleeBodyDepth: 1,
		MaxPerLevel:     50,
	})
	require.NoError(t, err)

	for _, c := range ctx.Callees {
		switch c.Symbol.Name {
		case "b":
			assert.NotEmpty(t, c.Body, "depth-1 callee should have body")
			assert.Contains(t, c.Body, "c()")
		case "c":
			assert.Empty(t, c.Body, "depth-2 callee should have no body")
			assert.NotEmpty(t, c.Signature)
		}
	}
}

func TestExtractWithOptions_calleeBodyDepth2CoversBoth(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	aID := symbolID(t, p, "a.ts", "a")

	ctx, err := ExtractWithOptions(p, aID, ExtractOptions{
		CallerDepth:     0,
		CalleeDepth:     2,
		CalleeBodyDepth: 2,
		MaxPerLevel:     50,
	})
	require.NoError(t, err)
	for _, c := range ctx.Callees {
		assert.NotEmpty(t, c.Body, "all callees up to body-depth should carry body, got empty for %s", c.Symbol.Name)
	}
}

// TestExtractWithOptions_maxPerLevelTruncates uses fanoutCaller which
// calls f1..f5; capping per-level at 2 should drop the rest in a
// deterministic order (sorted by symbol ID).
func TestExtractWithOptions_maxPerLevelTruncates(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	fanID := symbolID(t, p, "fanout.ts", "fanoutCaller")

	ctx, err := ExtractWithOptions(p, fanID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 1,
		MaxPerLevel: 2,
	})
	require.NoError(t, err)
	assert.Len(t, ctx.Callees, 2)
}

// TestExtractWithOptions_callerDepth2Inverse walks the caller chain
// upward: target = c, depth 2 should reach a (caller of b, caller of c).
func TestExtractWithOptions_callerDepth2Inverse(t *testing.T) {
	p := buildAndResolve(t, "call_chain")
	cID := symbolID(t, p, "c.ts", "c")

	ctx, err := ExtractWithOptions(p, cID, ExtractOptions{
		CallerDepth: 2,
		CalleeDepth: 0,
		MaxPerLevel: 50,
	})
	require.NoError(t, err)

	names := callerNames(ctx.Callers)
	assert.Contains(t, names, "b")
	assert.Contains(t, names, "a")
	for _, c := range ctx.Callers {
		switch c.Symbol.Name {
		case "b":
			assert.Equal(t, 1, c.Depth)
		case "a":
			assert.Equal(t, 2, c.Depth)
		}
	}
}

// TestExtractWithOptions_nonFunctionTargetIgnoresCalleeDepth confirms
// that for a class/interface/enum/type_alias target, CalleeDepth has
// no effect — these symbols cannot themselves call.
func TestExtractWithOptions_nonFunctionTargetIgnoresCalleeDepth(t *testing.T) {
	p := buildAndResolve(t, "inheritance")
	// pick a class symbol from the inheritance fixture
	var classID string
	for _, s := range p.Symbols {
		if s.Kind == "class" {
			classID = s.ID
			break
		}
	}
	require.NotEmpty(t, classID, "fixture should contain at least one class")

	ctx, err := ExtractWithOptions(p, classID, ExtractOptions{
		CallerDepth: 0,
		CalleeDepth: 5, // ignored for non-function target
		MaxPerLevel: 50,
	})
	require.NoError(t, err)
	assert.Empty(t, ctx.Callees)
}
