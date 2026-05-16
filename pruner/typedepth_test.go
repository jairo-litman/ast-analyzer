package pruner

import (
	"testing"

	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func typeEntryByName(entries []TypeEntry, name string) (TypeEntry, bool) {
	for _, e := range entries {
		if e.Symbol.Name == name {
			return e, true
		}
	}
	return TypeEntry{}, false
}

func TestExtract_typeDepth_zero(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	useAssetID := symbolID(t, p, "consumer.ts", "useAsset")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 0
	ctx, err := ExtractWithOptions(p, useAssetID, opts)
	require.NoError(t, err)
	assert.Empty(t, ctx.Types, "TypeDepth=0 should produce no Types entries")
}

func TestExtract_typeDepth_one(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	useAssetID := symbolID(t, p, "consumer.ts", "useAsset")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 1
	ctx, err := ExtractWithOptions(p, useAssetID, opts)
	require.NoError(t, err)

	asset, ok := typeEntryByName(ctx.Types, "Asset")
	require.True(t, ok, "Asset should appear in Types (param + return + local)")
	assert.Equal(t, 1, asset.Depth)
	assert.Equal(t, graph.SymbolClass, asset.Symbol.Kind)
	assert.NotEmpty(t, asset.Source, "type entry should carry the declaration source")

	service, ok := typeEntryByName(ctx.Types, "Service")
	require.True(t, ok, "Service should appear in Types (helper local)")
	assert.Equal(t, 1, service.Depth)

	_, hasTrackable := typeEntryByName(ctx.Types, "Trackable")
	assert.False(t, hasTrackable, "TypeDepth=1 must not transitively follow Asset.implements")
}

func TestExtract_typeDepth_two_transitive(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	useAssetID := symbolID(t, p, "consumer.ts", "useAsset")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 2
	ctx, err := ExtractWithOptions(p, useAssetID, opts)
	require.NoError(t, err)

	trackable, ok := typeEntryByName(ctx.Types, "Trackable")
	require.True(t, ok, "TypeDepth=2 should pull Trackable via Asset.implements")
	assert.Equal(t, 2, trackable.Depth)

	_, hasIdentifiable := typeEntryByName(ctx.Types, "Identifiable")
	assert.False(t, hasIdentifiable, "Identifiable lives at depth 3 from useAsset")
}

func TestExtract_typeDepth_class_extendsImplementsProperty(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	workflowID := symbolID(t, p, "consumer.ts", "Workflow")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 1
	ctx, err := ExtractWithOptions(p, workflowID, opts)
	require.NoError(t, err)

	_, hasService := typeEntryByName(ctx.Types, "Service")
	assert.True(t, hasService, "class target should include its extends type")
}

func TestRender_typeDepth_emitsTypesMetadata(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	useAssetID := symbolID(t, p, "consumer.ts", "useAsset")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 1
	ctx, err := ExtractWithOptions(p, useAssetID, opts)
	require.NoError(t, err)

	out, err := RenderRedacted(ctx, p)
	require.NoError(t, err)

	assert.Contains(t, out, "// types: ", "redacted output should announce types")
	assert.Contains(t, out, "Asset (depth=1, class, origins=", "Asset entry should show kind + origins")
	assert.Contains(t, out, "class Asset", "Asset's class declaration should appear in the rendered source")
}

func TestExtract_typeDepth_originsAreSurfaced(t *testing.T) {
	p := buildAndResolve(t, "typerefs")
	useAssetID := symbolID(t, p, "consumer.ts", "useAsset")

	opts := DefaultExtractOptions()
	opts.TypeDepth = 1
	ctx, err := ExtractWithOptions(p, useAssetID, opts)
	require.NoError(t, err)

	service, ok := typeEntryByName(ctx.Types, "Service")
	require.True(t, ok)
	require.NotEmpty(t, service.Origins)
	assert.Contains(t, service.Origins, graph.OriginInferredNew,
		"helper = new Service() must surface inferred:new among Service's origins")
}
