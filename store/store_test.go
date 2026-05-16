package store

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadFixtureProject builds the resolution fixture and resolves it.
// Shared across the store tests so failures point at persistence
// rather than upstream extraction drift.
func loadFixtureProject(t *testing.T) *graph.Project {
	t.Helper()
	p, err := graph.BuildProject(
		"../graph/testdata/resolution",
		"../graph/testdata/resolution/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	graph.ResolveCalls(p)
	return p
}

func TestRoundTripPreservesReExports(t *testing.T) {
	// Reexport fixture exercises named, aliased, and star forms.
	original, err := graph.BuildProject(
		"../graph/testdata/reexport",
		"../graph/testdata/reexport/tsconfig.json",
	)
	require.NoError(t, err)
	t.Cleanup(original.Close)
	graph.ResolveCalls(original)

	s, err := Open(":memory:")
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.Save(original))

	loaded, err := s.Load()
	require.NoError(t, err)

	assert.ElementsMatch(t, original.ReExports, loaded.ReExports,
		"ReExports must round-trip including bindings")
}

func TestRoundTripInMemory(t *testing.T) {
	original := loadFixtureProject(t)

	s, err := Open(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Save(original))

	loaded, err := s.Load()
	require.NoError(t, err)

	assertProjectGraphsEqual(t, original, loaded)
}

func TestRoundTripOnDisk(t *testing.T) {
	original := loadFixtureProject(t)

	dbPath := filepath.Join(t.TempDir(), "graph.db")

	// Save, close, reopen: the file must survive process boundaries.
	{
		s, err := Open(dbPath)
		require.NoError(t, err)
		require.NoError(t, s.Save(original))
		require.NoError(t, s.Close())
	}

	s, err := Open(dbPath)
	require.NoError(t, err)
	defer s.Close()

	loaded, err := s.Load()
	require.NoError(t, err)

	assertProjectGraphsEqual(t, original, loaded)
}

func TestSaveIsIdempotent(t *testing.T) {
	original := loadFixtureProject(t)

	s, err := Open(":memory:")
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Save(original))
	require.NoError(t, s.Save(original))

	loaded, err := s.Load()
	require.NoError(t, err)

	// Repeated Save rebuilds rather than appends.
	assertProjectGraphsEqual(t, original, loaded)
}

func TestResolvedToSurvivesRoundTrip(t *testing.T) {
	original := loadFixtureProject(t)

	// Pick one resolved and one unresolved call to cover both
	// branches of the call_resolutions table.
	var resolvedExpr, unresolvedExpr string
	for _, c := range original.Calls {
		if len(c.ResolvedTo) > 0 && resolvedExpr == "" {
			resolvedExpr = c.Expression
		}
		if len(c.ResolvedTo) == 0 && unresolvedExpr == "" {
			unresolvedExpr = c.Expression
		}
	}
	require.NotEmpty(t, resolvedExpr, "fixture should contain at least one resolved call")
	require.NotEmpty(t, unresolvedExpr, "fixture should contain at least one unresolved call")

	s, err := Open(":memory:")
	require.NoError(t, err)
	defer s.Close()
	require.NoError(t, s.Save(original))

	loaded, err := s.Load()
	require.NoError(t, err)

	loadedByExpr := map[string]graph.CallSite{}
	for _, c := range loaded.Calls {
		loadedByExpr[c.Expression] = c
	}

	var origResolved graph.CallSite
	for _, c := range original.Calls {
		if c.Expression == resolvedExpr && len(c.ResolvedTo) > 0 {
			origResolved = c
			break
		}
	}

	assert.ElementsMatch(t, origResolved.ResolvedTo, loadedByExpr[resolvedExpr].ResolvedTo)
	assert.Empty(t, loadedByExpr[unresolvedExpr].ResolvedTo)
}

// assertProjectGraphsEqual compares the persisted parts of two
// Projects (Symbols, Calls, Imports). Root and Files are not in the
// persistence contract.
func assertProjectGraphsEqual(t *testing.T, expected, actual *graph.Project) {
	t.Helper()

	expSymbols := append([]graph.Symbol(nil), expected.Symbols...)
	actSymbols := append([]graph.Symbol(nil), actual.Symbols...)
	sortSymbols(expSymbols)
	sortSymbols(actSymbols)
	assert.Equal(t, expSymbols, actSymbols, "symbols")

	expCalls := normalizeCalls(expected.Calls)
	actCalls := normalizeCalls(actual.Calls)
	assert.Equal(t, expCalls, actCalls, "calls")

	expImports := normalizeImports(expected.Imports)
	actImports := normalizeImports(actual.Imports)
	assert.Equal(t, expImports, actImports, "imports")
}

func sortSymbols(s []graph.Symbol) {
	sort.Slice(s, func(i, j int) bool { return s[i].ID < s[j].ID })
}

func normalizeCalls(in []graph.CallSite) []graph.CallSite {
	out := append([]graph.CallSite(nil), in...)
	for i := range out {
		// Sort ResolvedTo so equality is order-insensitive.
		sort.Strings(out[i].ResolvedTo)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].StartByte != out[j].StartByte {
			return out[i].StartByte < out[j].StartByte
		}
		return out[i].Expression < out[j].Expression
	})
	return out
}

func normalizeImports(in []graph.ImportEdge) []graph.ImportEdge {
	out := append([]graph.ImportEdge(nil), in...)
	for i := range out {
		ids := append(out[i].Identifiers[:0:0], out[i].Identifiers...)
		sort.Slice(ids, func(a, b int) bool {
			if ids[a].LocalName != ids[b].LocalName {
				return ids[a].LocalName < ids[b].LocalName
			}
			return ids[a].RemoteName < ids[b].RemoteName
		})
		out[i].Identifiers = ids
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Namespace < out[j].Namespace
	})
	return out
}
