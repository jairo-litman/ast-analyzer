package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolveThisField(t *testing.T) *Project {
	t.Helper()
	root := "testdata/this_field"
	p, err := BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// TestResolveCalls_thisField_constructorParamShorthand pins the
// dominant NestJS pattern: a constructor-parameter property
// (`constructor(protected foo: Foo)`) declared on a parent class,
// called as `this.foo.bar()` inside a subclass method.
func TestResolveCalls_thisField_constructorParamShorthand(t *testing.T) {
	p := buildAndResolveThisField(t)

	fooFind := lookupMethodSymbolID(t, p, "repo.ts", "FooRepository", "find")
	fooSave := lookupMethodSymbolID(t, p, "repo.ts", "FooRepository", "save")
	barLookup := lookupMethodSymbolID(t, p, "repo.ts", "BarRepository", "lookup")
	run := lookupMethodSymbolID(t, p, "service.ts", "ConcreteService", "run")
	count := lookupMethodSymbolID(t, p, "service.ts", "ConcreteService", "count")

	var findCall, saveCall, lookupCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		switch c.CallerID {
		case run:
			if c.Callee == "find" {
				findCall = c
			}
			if c.Callee == "save" {
				saveCall = c
			}
		case count:
			if c.Callee == "lookup" {
				lookupCall = c
			}
		}
	}
	require.NotNil(t, findCall, "ConcreteService.run must contain a this.fooRepository.find() call site")
	require.NotNil(t, saveCall, "ConcreteService.run must contain a this.fooRepository.save() call site")
	require.NotNil(t, lookupCall, "ConcreteService.count must contain a this.barRepository.lookup() call site")

	assert.Equal(t, []string{fooFind}, findCall.ResolvedTo)
	assert.Equal(t, []string{fooSave}, saveCall.ResolvedTo)
	assert.Equal(t, []string{barLookup}, lookupCall.ResolvedTo)
}

// TestResolveCalls_thisField_explicitDeclaration confirms that the
// resolver also handles `protected/private/public foo: T = ...;` —
// the non-shorthand form — within the same class.
func TestResolveCalls_thisField_explicitDeclaration(t *testing.T) {
	p := buildAndResolveThisField(t)

	fooFind := lookupMethodSymbolID(t, p, "repo.ts", "FooRepository", "find")
	lookup := lookupMethodSymbolID(t, p, "service.ts", "WithExplicitField", "lookup")

	var call *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == lookup && c.Callee == "find" {
			call = c
			break
		}
	}
	require.NotNil(t, call)
	assert.Equal(t, []string{fooFind}, call.ResolvedTo,
		"this.foo.find() must resolve via the explicit `private foo: FooRepository` declaration")
}
