package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseTypeRef_empty(t *testing.T) {
	assert.Nil(t, ParseTypeRef("", OriginAnnotation))
	assert.Nil(t, ParseTypeRef("   ", OriginAnnotation))
}

func TestParseTypeRef_bareIdentifier(t *testing.T) {
	r := ParseTypeRef("HistoryBuilder", OriginAnnotation)
	assert.Equal(t, "HistoryBuilder", r.Raw)
	assert.Equal(t, "HistoryBuilder", r.BaseName)
	assert.Empty(t, r.TypeArgs)
	assert.Empty(t, r.Union)
	assert.False(t, r.IsArray)
	assert.Equal(t, OriginAnnotation, r.Origin)
}

func TestParseTypeRef_qualifiedIdentifier(t *testing.T) {
	r := ParseTypeRef("mod.Foo", OriginAnnotation)
	assert.Equal(t, "mod.Foo", r.BaseName)
}

func TestParseTypeRef_generic(t *testing.T) {
	r := ParseTypeRef("Promise<HistoryBuilder>", OriginAnnotation)
	assert.Equal(t, "Promise", r.BaseName)
	if assert.Len(t, r.TypeArgs, 1) {
		assert.Equal(t, "HistoryBuilder", r.TypeArgs[0].BaseName)
	}
}

func TestParseTypeRef_nestedGeneric(t *testing.T) {
	r := ParseTypeRef("Promise<Array<Foo>>", OriginAnnotation)
	assert.Equal(t, "Promise", r.BaseName)
	if assert.Len(t, r.TypeArgs, 1) {
		inner := r.TypeArgs[0]
		assert.Equal(t, "Array", inner.BaseName)
		if assert.Len(t, inner.TypeArgs, 1) {
			assert.Equal(t, "Foo", inner.TypeArgs[0].BaseName)
		}
	}
}

func TestParseTypeRef_multipleTypeArgs(t *testing.T) {
	r := ParseTypeRef("Map<string, Foo>", OriginAnnotation)
	assert.Equal(t, "Map", r.BaseName)
	if assert.Len(t, r.TypeArgs, 2) {
		assert.Equal(t, "string", r.TypeArgs[0].BaseName)
		assert.Equal(t, "Foo", r.TypeArgs[1].BaseName)
	}
}

func TestParseTypeRef_array(t *testing.T) {
	r := ParseTypeRef("Foo[]", OriginAnnotation)
	assert.Equal(t, "Foo", r.BaseName)
	assert.True(t, r.IsArray)
}

func TestParseTypeRef_arrayOfGeneric(t *testing.T) {
	r := ParseTypeRef("Promise<Foo>[]", OriginAnnotation)
	assert.Equal(t, "Promise", r.BaseName)
	assert.True(t, r.IsArray)
	if assert.Len(t, r.TypeArgs, 1) {
		assert.Equal(t, "Foo", r.TypeArgs[0].BaseName)
	}
}

func TestParseTypeRef_union(t *testing.T) {
	r := ParseTypeRef("Foo | Bar | Baz", OriginAnnotation)
	assert.Empty(t, r.BaseName)
	if assert.Len(t, r.Union, 3) {
		assert.Equal(t, "Foo", r.Union[0].BaseName)
		assert.Equal(t, "Bar", r.Union[1].BaseName)
		assert.Equal(t, "Baz", r.Union[2].BaseName)
	}
}

func TestParseTypeRef_unionWithGeneric(t *testing.T) {
	r := ParseTypeRef("Array<A | B> | C", OriginAnnotation)
	if assert.Len(t, r.Union, 2) {
		first := r.Union[0]
		assert.Equal(t, "Array", first.BaseName)
		if assert.Len(t, first.TypeArgs, 1) {
			arg := first.TypeArgs[0]
			if assert.Len(t, arg.Union, 2) {
				assert.Equal(t, "A", arg.Union[0].BaseName)
				assert.Equal(t, "B", arg.Union[1].BaseName)
			}
		}
		assert.Equal(t, "C", r.Union[1].BaseName)
	}
}

func TestParseTypeRef_intersection(t *testing.T) {
	r := ParseTypeRef("Foo & Bar", OriginAnnotation)
	if assert.Len(t, r.Intersection, 2) {
		assert.Equal(t, "Foo", r.Intersection[0].BaseName)
		assert.Equal(t, "Bar", r.Intersection[1].BaseName)
	}
}

func TestParseTypeRef_origin(t *testing.T) {
	r := ParseTypeRef("Foo", OriginInferredNew)
	assert.Equal(t, OriginInferredNew, r.Origin)
}

func TestParseTypeRef_primitivesIdentifiedAsSuch(t *testing.T) {
	for _, p := range []string{"string", "number", "boolean", "void", "any", "unknown", "never", "null", "undefined", "object", "symbol", "bigint"} {
		r := ParseTypeRef(p, OriginAnnotation)
		assert.True(t, r.IsPrimitive(), "%s should be primitive", p)
	}
	r := ParseTypeRef("Foo", OriginAnnotation)
	assert.False(t, r.IsPrimitive())
}

// TestTypeRef_WalkBaseTypes pins depth-first left-to-right traversal
// over every node with a non-empty BaseName.
func TestTypeRef_WalkBaseTypes(t *testing.T) {
	r := ParseTypeRef("Promise<A | B<C>>[]", OriginAnnotation)
	var names []string
	r.WalkBaseTypes(func(ref *TypeRef) {
		names = append(names, ref.BaseName)
	})
	assert.Equal(t, []string{"Promise", "A", "B", "C"}, names)
}
