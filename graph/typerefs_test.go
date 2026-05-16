package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveTypeRefs(t *testing.T) *Project {
	t.Helper()
	root := "testdata/typerefs"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

func findTypeRef(t *testing.T, sym Symbol, slot string) SymbolTypeRef {
	t.Helper()
	for _, tr := range sym.TypeRefs {
		if tr.Slot == slot {
			return tr
		}
	}
	t.Fatalf("symbol %q has no TypeRef for slot %q (got %+v)", sym.ID, slot, sym.TypeRefs)
	return SymbolTypeRef{}
}

func TestTypeRefs_function_parametersAndReturn(t *testing.T) {
	p := buildAndResolveTypeRefs(t)

	asset := lookupSymbolID(t, p, "types.ts", "Asset")
	useAssetID := lookupSymbolID(t, p, "consumer.ts", "useAsset")
	useAsset := symbolByID(t, p, useAssetID)

	paramRef := findTypeRef(t, useAsset, "param:input")
	assert.Equal(t, OriginAnnotation, paramRef.Ref.Origin)
	assert.Equal(t, "Asset", paramRef.Ref.BaseName)
	assert.Equal(t, []string{asset}, paramRef.Ref.Targets,
		"param:input should resolve to Asset class symbol")

	returnRef := findTypeRef(t, useAsset, "return")
	assert.Equal(t, OriginAnnotation, returnRef.Ref.Origin)
	assert.Equal(t, "Promise", returnRef.Ref.BaseName)
	if assert.Len(t, returnRef.Ref.TypeArgs, 1) {
		inner := returnRef.Ref.TypeArgs[0]
		assert.Equal(t, "Asset", inner.BaseName)
		assert.Equal(t, []string{asset}, inner.Targets)
	}
}

func TestTypeRefs_function_locals(t *testing.T) {
	p := buildAndResolveTypeRefs(t)

	assetID := lookupSymbolID(t, p, "types.ts", "Asset")
	serviceID := lookupSymbolID(t, p, "types.ts", "Service")
	useAssetID := lookupSymbolID(t, p, "consumer.ts", "useAsset")
	useAsset := symbolByID(t, p, useAssetID)

	helper := findTypeRef(t, useAsset, "local:helper")
	assert.Equal(t, OriginInferredNew, helper.Ref.Origin,
		"const helper = new Service() should be tagged inferred:new")
	assert.Equal(t, "Service", helper.Ref.BaseName)
	assert.Equal(t, []string{serviceID}, helper.Ref.Targets)

	out := findTypeRef(t, useAsset, "local:out")
	assert.Equal(t, OriginInferredMethodReturn, out.Ref.Origin,
		"const out = helper.handle(input) should be tagged inferred:method-return")
	assert.Equal(t, "Asset", out.Ref.BaseName)
	assert.Equal(t, []string{assetID}, out.Ref.Targets)
}

func TestTypeRefs_class_extendsImplementsProperties(t *testing.T) {
	p := buildAndResolveTypeRefs(t)

	serviceID := lookupSymbolID(t, p, "types.ts", "Service")
	trackableID := lookupSymbolID(t, p, "types.ts", "Trackable")
	assetID := lookupSymbolID(t, p, "types.ts", "Asset")

	workflowID := lookupSymbolID(t, p, "consumer.ts", "Workflow")
	workflow := symbolByID(t, p, workflowID)
	asset := symbolByID(t, p, assetID)

	ext := findTypeRef(t, workflow, "extends")
	assert.Equal(t, OriginAnnotation, ext.Ref.Origin)
	assert.Equal(t, "Service", ext.Ref.BaseName)
	assert.Equal(t, []string{serviceID}, ext.Ref.Targets)

	impl := findTypeRef(t, asset, "implements:0")
	assert.Equal(t, OriginAnnotation, impl.Ref.Origin)
	assert.Equal(t, "Trackable", impl.Ref.BaseName)
	assert.Equal(t, []string{trackableID}, impl.Ref.Targets)

	parent := findTypeRef(t, asset, "property:parent")
	assert.Equal(t, "Asset", parent.Ref.BaseName)
	assert.Equal(t, []string{assetID}, parent.Ref.Targets,
		"Asset.parent: Asset should resolve to Asset itself")
}

func TestTypeRefs_interface_extendsProperty(t *testing.T) {
	p := buildAndResolveTypeRefs(t)

	identifiableID := lookupSymbolID(t, p, "types.ts", "Identifiable")
	trackableID := lookupSymbolID(t, p, "types.ts", "Trackable")
	trackable := symbolByID(t, p, trackableID)

	ext := findTypeRef(t, trackable, "extends:0")
	assert.Equal(t, OriginAnnotation, ext.Ref.Origin)
	assert.Equal(t, "Identifiable", ext.Ref.BaseName)
	assert.Equal(t, []string{identifiableID}, ext.Ref.Targets)
}

func TestTypeRefs_typeAlias_unionMembers(t *testing.T) {
	p := buildAndResolveTypeRefs(t)

	assetID := lookupSymbolID(t, p, "types.ts", "Asset")
	serviceID := lookupSymbolID(t, p, "types.ts", "Service")
	aliasID := lookupSymbolID(t, p, "types.ts", "AssetOrService")
	alias := symbolByID(t, p, aliasID)

	value := findTypeRef(t, alias, "value")
	require.NotEmpty(t, value.Ref.Union, "AssetOrService should have a Union ref")
	if assert.Len(t, value.Ref.Union, 2) {
		assert.Equal(t, "Asset", value.Ref.Union[0].BaseName)
		assert.Equal(t, []string{assetID}, value.Ref.Union[0].Targets)
		assert.Equal(t, "Service", value.Ref.Union[1].BaseName)
		assert.Equal(t, []string{serviceID}, value.Ref.Union[1].Targets)
	}
}

func symbolByID(t *testing.T, p *Project, id string) Symbol {
	t.Helper()
	for _, s := range p.Symbols {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("symbol id %q not found", id)
	return Symbol{}
}
