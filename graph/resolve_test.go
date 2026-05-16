package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCalls(t *testing.T) {
	p, err := BuildProject("testdata/resolution", "testdata/resolution/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)

	helperAdd := lookupSymbolID(t, p, "helper.ts", "add")
	helperMul := lookupSymbolID(t, p, "helper.ts", "multiply")
	helperSub := lookupSymbolID(t, p, "helper.ts", "subtract")
	helperGreeter := lookupSymbolID(t, p, "helper.ts", "Greeter")
	mainLocal := lookupSymbolID(t, p, "main.ts", "local")
	mainID := lookupSymbolID(t, p, "main.ts", "main")

	fromMain := callsFrom(p, mainID)

	cases := []struct {
		name         string
		callee       string
		receiver     string
		wantResolved []string
	}{
		{
			name:         "direct named import",
			callee:       "add",
			wantResolved: []string{helperAdd},
		},
		{
			name:         "aliased import resolves through alias to remote name",
			callee:       "times",
			wantResolved: []string{helperMul},
		},
		{
			name:         "namespace import + member call",
			callee:       "subtract",
			receiver:     "utils",
			wantResolved: []string{helperSub},
		},
		{
			name:         "constructor invocation resolves to class symbol",
			callee:       "Greeter",
			wantResolved: []string{helperGreeter},
		},
		{
			name:         "same-file direct call",
			callee:       "local",
			wantResolved: []string{mainLocal},
		},
		{
			name:         "external call leaves ResolvedTo empty",
			callee:       "log",
			receiver:     "console",
			wantResolved: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cs := findCallSite(t, fromMain, tc.callee, tc.receiver)
			assert.Equal(t, tc.wantResolved, cs.ResolvedTo)
		})
	}
}

// TestResolveCalls_topLevelCalls pins that module-scope calls
// resolve through the same per-file scope as function-scope calls.
func TestResolveCalls_topLevelCalls(t *testing.T) {
	p, err := BuildProject("testdata/module", "testdata/module/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)

	moduleID := lookupModuleSymbolID(t, p, "main.ts")
	helperAdd := lookupSymbolID(t, p, "helper.ts", "add")

	var addCall, initCall, logCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID != moduleID {
			continue
		}
		switch c.Callee {
		case "add":
			addCall = c
		case "initLogging":
			initCall = c
		case "log":
			logCall = c
		}
	}

	require.NotNil(t, addCall, "expected add() at module scope")
	require.NotNil(t, initCall, "expected initLogging() at module scope")
	require.NotNil(t, logCall, "expected console.log() at module scope")

	assert.Equal(t, []string{helperAdd}, addCall.ResolvedTo,
		"add() at module scope must resolve through the file's import scope")
	assert.Empty(t, initCall.ResolvedTo,
		"initLogging() has no matching import or local declaration; leave unresolved")
	assert.Empty(t, logCall.ResolvedTo,
		"console.log() — `console` isn't in scope, leave unresolved")
}

func TestResolveCalls_thisInSameClass(t *testing.T) {
	p := buildAndResolveInheritance(t)

	animalSpeak := lookupMethodSymbolID(t, p, "animal.ts", "Animal", "speak")
	introduceCall := findCallSiteByCallee(t, p, "animal.ts", "speak")
	assert.Equal(t, "this", introduceCall.Receiver)
	assert.Equal(t, []string{animalSpeak}, introduceCall.ResolvedTo,
		"this.speak() in Animal.introduce must resolve to Animal.speak (same class)")
}

func TestResolveCalls_thisOverridingMethodResolvesToOverride(t *testing.T) {
	// TS override semantics: this.speak() from Dog.nameMyself picks
	// Dog.speak, not Animal.speak.
	p := buildAndResolveInheritance(t)

	dogSpeak := lookupMethodSymbolID(t, p, "dog.ts", "Dog", "speak")

	dogNameMyself := lookupMethodSymbolID(t, p, "dog.ts", "Dog", "nameMyself")
	var thisSpeak *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == dogNameMyself && c.Callee == "speak" {
			thisSpeak = c
			break
		}
	}
	require.NotNil(t, thisSpeak)
	assert.Equal(t, []string{dogSpeak}, thisSpeak.ResolvedTo,
		"this.speak() in Dog.nameMyself must resolve to Dog.speak, not Animal.speak")
}

func TestResolveCalls_superCrossFile(t *testing.T) {
	// Dog.bark's super.introduce() reaches Animal.introduce in a
	// different file via Dog's import scope.
	p := buildAndResolveInheritance(t)

	animalIntroduce := lookupMethodSymbolID(t, p, "animal.ts", "Animal", "introduce")
	dogBark := lookupMethodSymbolID(t, p, "dog.ts", "Dog", "bark")

	var superIntroduce *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == dogBark && c.Callee == "introduce" {
			superIntroduce = c
			break
		}
	}
	require.NotNil(t, superIntroduce)
	assert.Equal(t, "super", superIntroduce.Receiver)
	assert.Equal(t, []string{animalIntroduce}, superIntroduce.ResolvedTo,
		"super.introduce() in Dog.bark must resolve to Animal.introduce via cross-file extends")
}

// TestResolveCalls_superConstructor_directParent confirms that bare
// `super(...)` calls inside a derived class's constructor resolve to
// the parent class's constructor symbol. CallSite shape: callee="",
// receiver="super".
func TestResolveCalls_superConstructor_directParent(t *testing.T) {
	p := buildAndResolveInheritance(t)

	baseCtor := lookupMethodSymbolID(t, p, "super_ctor.ts", "Base", "constructor")
	derivedCtor := lookupMethodSymbolID(t, p, "super_ctor.ts", "Derived", "constructor")

	var superCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == derivedCtor && c.Receiver == "super" && c.Callee == "" {
			superCall = c
			break
		}
	}
	require.NotNil(t, superCall, "Derived.constructor must contain a bare super() call site")
	assert.Equal(t, []string{baseCtor}, superCall.ResolvedTo,
		"super() in Derived.constructor must resolve to Base.constructor")
}

// TestResolveCalls_superConstructor_walksChain confirms that when the
// immediate parent has no explicit constructor, `super()` walks up
// the extends chain to find the nearest ancestor that does.
func TestResolveCalls_superConstructor_walksChain(t *testing.T) {
	p := buildAndResolveInheritance(t)

	derivedCtor := lookupMethodSymbolID(t, p, "super_ctor.ts", "Derived", "constructor")
	threeDeepCtor := lookupMethodSymbolID(t, p, "super_ctor.ts", "ThreeDeep", "constructor")

	var superCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.CallerID == threeDeepCtor && c.Receiver == "super" && c.Callee == "" {
			superCall = c
			break
		}
	}
	require.NotNil(t, superCall, "ThreeDeep.constructor must contain a bare super() call site")
	assert.Equal(t, []string{derivedCtor}, superCall.ResolvedTo,
		"super() in ThreeDeep.constructor must walk past GrandchildSkipMiddle (no explicit constructor) to Derived.constructor")
}

func TestResolveCalls_thisToInterfaceWhenNoImplementation(t *testing.T) {
	// Sprinter implements Athlete extends Walker (declares walk).
	// Sprinter.run's this.walk() must fall through to the interface
	// chain since the class chain has no implementation.
	p := buildAndResolveInheritance(t)

	walkerID := lookupSymbolID(t, p, "contracts.ts", "Walker")

	thisWalk := findCallSiteByCallee(t, p, "contracts.ts", "walk")
	assert.Equal(t, "this", thisWalk.Receiver)
	assert.Equal(t, []string{walkerID}, thisWalk.ResolvedTo,
		"this.walk() must fall through to the interface chain and resolve to Walker")
}

func TestResolveCalls_defaultImportFunction(t *testing.T) {
	p := buildAndResolveDefaultExport(t)

	calculateID := lookupSymbolID(t, p, "service.ts", "calculate")
	calcCall := findCallSiteByCallee(t, p, "main.ts", "calc")

	assert.Equal(t, []string{calculateID}, calcCall.ResolvedTo,
		"`import calc from \"./service\"` then `calc(5)` must resolve to service.ts's default-exported calculate")
}

func TestResolveCalls_defaultImportClassConstructor(t *testing.T) {
	p := buildAndResolveDefaultExport(t)

	widgetID := lookupSymbolID(t, p, "widget.ts", "Widget")
	widgetCall := findCallSiteByCallee(t, p, "main.ts", "Widget")
	require.True(t, widgetCall.IsConstructor, "expected the new-expression flag")

	assert.Equal(t, []string{widgetID}, widgetCall.ResolvedTo,
		"`new Widget()` against a default-imported class must resolve to widget.ts's Widget")
}

func TestResolveCalls_defaultImportIdentifierReference(t *testing.T) {
	// `export default helper;` flags helper IsDefaultExport, so an
	// `import h from "./utils"` then `h()` resolves to it.
	p := buildAndResolveDefaultExport(t)

	helperID := lookupSymbolID(t, p, "utils.ts", "helper")
	hCall := findCallSiteByCallee(t, p, "main.ts", "h")

	assert.Equal(t, []string{helperID}, hCall.ResolvedTo)
}

func TestBuildProject_marksDefaultExportSymbol(t *testing.T) {
	// Sanity check: the build step flags the right symbol.
	p := buildAndResolveDefaultExport(t)

	for _, expected := range []struct{ file, name string }{
		{"service.ts", "calculate"},
		{"widget.ts", "Widget"},
		{"utils.ts", "helper"},
	} {
		var found bool
		for _, s := range p.Symbols {
			if s.File == expected.file && s.Name == expected.name {
				assert.True(t, s.IsDefaultExport,
					"%s/%s should be flagged IsDefaultExport", expected.file, expected.name)
				found = true
				break
			}
		}
		require.True(t, found, "no symbol %s/%s", expected.file, expected.name)
	}
}

func TestResolveCalls_namedReExport(t *testing.T) {
	// `add` re-exported via helpers/index.ts: an
	// `import { add } from "./helpers"` must resolve to math.ts's add.
	p := buildAndResolveReExport(t)

	mathAdd := lookupSymbolID(t, p, "helpers/math.ts", "add")
	addCall := findCallSiteByCallee(t, p, "main.ts", "add")
	assert.Equal(t, []string{mathAdd}, addCall.ResolvedTo)
}

func TestResolveCalls_aliasedReExport(t *testing.T) {
	// `multiply as times` re-export: the chain follower rewrites the
	// name when following.
	p := buildAndResolveReExport(t)

	mathMultiply := lookupSymbolID(t, p, "helpers/math.ts", "multiply")
	timesCall := findCallSiteByCallee(t, p, "main.ts", "times")
	assert.Equal(t, []string{mathMultiply}, timesCall.ResolvedTo)
}

func TestResolveCalls_starReExport(t *testing.T) {
	// `export * from "./inner"`: the chain follower's star branch
	// keeps the name and tries the source module.
	p := buildAndResolveReExport(t)

	innerDeep := lookupSymbolID(t, p, "nested/inner.ts", "deep")
	deepCall := findCallSiteByCallee(t, p, "main.ts", "deep")
	assert.Equal(t, []string{innerDeep}, deepCall.ResolvedTo)
}

// TestResolveCalls_defaultReExport pins
// `export { default as X } from "./other"`: a consumer importing X
// resolves through to the source module's default-exported symbol.
func TestResolveCalls_defaultReExport(t *testing.T) {
	p := buildAndResolveReExport(t)

	coolFn := lookupSymbolID(t, p, "helpers/cool.ts", "cool")
	coolCall := findCallSiteByCallee(t, p, "main.ts", "Cool")
	assert.Equal(t, []string{coolFn}, coolCall.ResolvedTo,
		"Cool() must resolve through `export { default as Cool }` to cool.ts's default export")
}

// TestResolveCalls_namespaceReExport pins
// `export * as Foo from "./other"`: an importer that brings in `Foo`
// gets a namespace alias whose member access resolves into the source
// module.
func TestResolveCalls_namespaceReExport(t *testing.T) {
	p := buildAndResolveReExport(t)

	mathAdd := lookupSymbolID(t, p, "helpers/math.ts", "add")
	var nsCall *CallSite
	for i := range p.Calls {
		c := &p.Calls[i]
		if c.Callee == "add" && c.Receiver == "MathNS" {
			nsCall = c
			break
		}
	}
	require.NotNil(t, nsCall, "expected MathNS.add(...) call site")
	assert.Equal(t, []string{mathAdd}, nsCall.ResolvedTo,
		"MathNS.add() must resolve through the `export * as MathNS` re-export to math.ts:add")
}

func buildAndResolveReExport(t *testing.T) *Project {
	t.Helper()
	p, err := BuildProject("testdata/reexport", "testdata/reexport/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

func buildAndResolveDefaultExport(t *testing.T) *Project {
	t.Helper()
	p, err := BuildProject("testdata/default_export", "testdata/default_export/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

func buildAndResolveInheritance(t *testing.T) *Project {
	t.Helper()
	p, err := BuildProject("testdata/inheritance", "testdata/inheritance/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	ResolveCalls(p)
	return p
}

// lookupMethodSymbolID finds the function-kind symbol named method
// inside the named class in file. Used to assert against specific
// class methods without hard-coding byte offsets.
func lookupMethodSymbolID(t *testing.T, p *Project, file, className, method string) string {
	t.Helper()
	var class Symbol
	for _, s := range p.Symbols {
		if s.File == file && s.Kind == SymbolClass && s.Name == className {
			class = s
			break
		}
	}
	require.NotEmpty(t, class.ID, "no class %q in %s", className, file)

	for _, s := range p.Symbols {
		if s.File != file || s.Kind != SymbolFunction || s.Name != method {
			continue
		}
		if class.StartByte <= s.StartByte && s.EndByte <= class.EndByte {
			return s.ID
		}
	}
	t.Fatalf("no method %q on class %q in %s", method, className, file)
	return ""
}

func callsFrom(p *Project, callerID string) []CallSite {
	var calls []CallSite
	for _, c := range p.Calls {
		if c.CallerID == callerID {
			calls = append(calls, c)
		}
	}
	return calls
}

func findCallSite(t *testing.T, calls []CallSite, callee, receiver string) CallSite {
	t.Helper()
	for _, c := range calls {
		if c.Callee == callee && c.Receiver == receiver {
			return c
		}
	}
	t.Fatalf("call site not found: callee=%q receiver=%q", callee, receiver)
	return CallSite{}
}
