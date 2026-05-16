package extractor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helperTestSetup builds an Extractor configured with the resolver
// testdata. Read-only after construction, safe to share across tests.
func helperTestSetup(t *testing.T) *Extractor {
	t.Helper()
	e, err := NewExtractor("testdata/resolver/tsconfig.json")
	require.NoError(t, err)
	require.NotNil(t, e)
	return e
}

func TestNewExtractor(t *testing.T) {
	e := helperTestSetup(t)

	assert.NotNil(t, e.Language, "language should be initialized")
	assert.NotNil(t, e.Resolver, "resolver should be initialized")
	assert.NotEmpty(t, e.Queries, "queries should be loaded")

	for name, query := range e.Queries {
		assert.NotNil(t, query, "query %s should compile", name)
	}
}

// TestExtractorEndToEnd verifies that Parse and the per-domain query
// methods cooperate on a single source.
func TestExtractorEndToEnd(t *testing.T) {
	e := helperTestSetup(t)

	source := []byte(`
import { add } from "./math";

function double(n: number): number {
    return add(n, n);
}
`)

	tree, err := e.Parse(source)
	require.NoError(t, err)
	defer tree.Close()
	require.False(t, tree.RootNode().HasError(), "fixture should be syntactically valid")

	imports, err := e.QueryImports(tree.RootNode(), source)
	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "./math", imports[0].Path)

	functions, err := e.QueryFunctions(tree.RootNode(), source)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	assert.Equal(t, "double", functions[0].Name)
	assert.Equal(t, "number", functions[0].ReturnType)
}
