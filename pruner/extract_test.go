package pruner

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/jairo-litman/ast-analyzer/extractor"
	"github.com/jairo-litman/ast-analyzer/graph"
	"github.com/jairo-litman/ast-analyzer/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildAndResolve(t *testing.T, fixture string) *graph.Project {
	t.Helper()
	root := "../graph/testdata/" + fixture
	p, err := graph.BuildProject(root, root+"/tsconfig.json")
	require.NoError(t, err)
	t.Cleanup(p.Close)
	graph.ResolveCalls(p)
	return p
}

func symbolID(t *testing.T, p *graph.Project, file, name string) string {
	t.Helper()
	for _, s := range p.Symbols {
		if s.File == file && s.Name == name {
			return s.ID
		}
	}
	t.Fatalf("no symbol %q in %s", name, file)
	return ""
}

func TestExtract_topLevelFunction(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	mainID := symbolID(t, p, "main.ts", "main")

	ctx, err := Extract(p, mainID)
	require.NoError(t, err)

	t.Run("target source contains the full body", func(t *testing.T) {
		assert.Equal(t, mainID, ctx.Target.Symbol.ID)
		assert.Contains(t, ctx.Target.Source, "function main()")
		assert.Contains(t, ctx.Target.Source, "console.log(a, b, c, g)")
		assert.Contains(t, ctx.Target.Source, "local(a)")
	})

	t.Run("top-level functions have no enclosing type", func(t *testing.T) {
		assert.Nil(t, ctx.EnclosingType)
	})

	t.Run("imports include every import statement from the target's file", func(t *testing.T) {
		require.Len(t, ctx.Imports, 3)

		var paths []string
		for _, imp := range ctx.Imports {
			paths = append(paths, imp.Edge.Path)
			assert.NotEmpty(t, imp.Source, "Source must carry the raw import statement text")
		}
		// All three import statements share the same module path in
		// this fixture; preserve them one-to-one.
		assert.Equal(t, []string{"./helper", "./helper", "./helper"}, paths)
	})

	t.Run("callees are deduped by resolved symbol", func(t *testing.T) {
		var names []string
		for _, c := range ctx.Callees {
			names = append(names, c.Symbol.Name)
			assert.NotEmpty(t, c.Signature, "callee %q should carry a signature", c.Symbol.Name)
			assert.NotEmpty(t, c.CallSites, "callee %q should reference at least one call site", c.Symbol.Name)
		}
		// console.log is unresolved (not imported) so it's absent.
		assert.ElementsMatch(t,
			[]string{"add", "multiply", "subtract", "Greeter", "local"},
			names)
	})

	t.Run("nobody calls main in this fixture", func(t *testing.T) {
		assert.Empty(t, ctx.Callers)
	})
}

func TestExtract_callerEntryCarriesCallExpression(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	addID := symbolID(t, p, "helper.ts", "add")

	ctx, err := Extract(p, addID)
	require.NoError(t, err)

	require.Len(t, ctx.Callers, 1, "main is the only caller of add in the fixture")
	caller := ctx.Callers[0]
	assert.Equal(t, "main", caller.Symbol.Name)
	assert.Contains(t, caller.Signature, "function main")

	require.Len(t, caller.CallSites, 1)
	assert.Equal(t, "add(1, 2)", caller.CallSites[0].Expression,
		"caller entry must surface the actual call expression")
}

func TestExtract_classMethodCarriesEnclosingType(t *testing.T) {
	p := buildAndResolve(t, "simple")
	greetID := symbolID(t, p, "main.ts", "greet")

	ctx, err := Extract(p, greetID)
	require.NoError(t, err)

	require.NotNil(t, ctx.EnclosingType)
	assert.Equal(t, "Greeter", ctx.EnclosingType.Symbol.Name)
	assert.Equal(t, graph.SymbolClass, ctx.EnclosingType.Symbol.Kind)
	assert.Contains(t, ctx.EnclosingType.Source, "class Greeter")
	assert.Contains(t, ctx.EnclosingType.Source, "greet(name: string)")
}

func TestExtract_classMethodHeaderIsStripped(t *testing.T) {
	p := buildAndResolve(t, "simple")
	greetID := symbolID(t, p, "main.ts", "greet")

	ctx, err := Extract(p, greetID)
	require.NoError(t, err)
	require.NotNil(t, ctx.EnclosingType)

	header := ctx.EnclosingType.Source
	assert.Contains(t, header, "class Greeter")
	assert.Contains(t, header, "greet(name: string): string;",
		"method should render as a signature with a trailing ;")
	assert.NotContains(t, header, "return ",
		"body content must not leak into the rendered header")
	assert.NotContains(t, header, `"hi "`,
		"string literals from the body must not leak into the rendered header")
}

func TestExtract_classMethodHeaderIsStrippedOnLoadedProject(t *testing.T) {
	original := buildAndResolve(t, "simple")
	greetID := symbolID(t, original, "main.ts", "greet")

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	{
		s, err := store.Open(dbPath)
		require.NoError(t, err)
		require.NoError(t, s.Save(original))
		require.NoError(t, s.Close())
	}

	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)

	ctx, err := Extract(loaded, greetID)
	require.NoError(t, err)
	require.NotNil(t, ctx.EnclosingType)

	// Loaded-project rendering matches in-memory rendering, proving
	// ClassDetails round-trips through SQLite.
	inMemory, err := Extract(original, greetID)
	require.NoError(t, err)
	assert.Equal(t, inMemory.EnclosingType.Source, ctx.EnclosingType.Source)
}

func TestRenderClassHeader(t *testing.T) {
	cases := []struct {
		name    string
		input   *graph.ClassDetails
		clsName string
		want    string
	}{
		{
			name:    "empty class",
			clsName: "Empty",
			input:   &graph.ClassDetails{},
			want:    "class Empty {\n}",
		},
		{
			name:    "abstract class with extends",
			clsName: "Shape",
			input: &graph.ClassDetails{
				Abstract: true,
				Extends:  "Drawable",
			},
			want: "abstract class Shape extends Drawable {\n}",
		},
		{
			name:    "implements multiple",
			clsName: "Foo",
			input: &graph.ClassDetails{
				Implements: []string{"I1", "I2"},
			},
			want: "class Foo implements I1, I2 {\n}",
		},
		{
			name:    "properties and method",
			clsName: "Point",
			input: &graph.ClassDetails{
				Properties: []extractor.ObjectProperty{
					{Name: "x", Type: "number"},
					{Name: "y", Type: "number"},
				},
				Methods: []extractor.MethodSignature{
					{
						Name:       "distance",
						ReturnType: "number",
						Parameters: []extractor.FunctionParameter{
							{Name: "other", Type: "Point"},
						},
					},
				},
			},
			want: "class Point {\n" +
				"    x: number;\n" +
				"    y: number;\n" +
				"    distance(other: Point): number;\n" +
				"}",
		},
		{
			name:    "untyped property and untyped return",
			clsName: "Untyped",
			input: &graph.ClassDetails{
				Properties: []extractor.ObjectProperty{{Name: "x"}},
				Methods: []extractor.MethodSignature{
					{Name: "noop", Parameters: []extractor.FunctionParameter{{Name: "data"}}},
				},
			},
			want: "class Untyped {\n" +
				"    x;\n" +
				"    noop(data);\n" +
				"}",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderClassHeader(tc.clsName, tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestExtract_unknownSymbolErrors(t *testing.T) {
	p := buildAndResolve(t, "resolution")
	_, err := Extract(p, "nonexistent#0")
	require.Error(t, err)
}

func TestExtract_moduleSymbolTargetErrors(t *testing.T) {
	// The synthetic per-file caller isn't user-addressable.
	p := buildAndResolve(t, "module")

	var moduleID string
	for _, s := range p.Symbols {
		if s.File == "main.ts" && s.Kind == graph.SymbolModule {
			moduleID = s.ID
			break
		}
	}
	require.NotEmpty(t, moduleID, "module fixture must produce a synthetic module symbol")

	_, err := Extract(p, moduleID)
	require.Error(t, err)
}

// TestExtract_classTarget pins Extract for class-kind targets.
// `new Storage()` in main.ts resolves to the class symbol, so main
// shows up as a caller.
func TestExtract_classTarget(t *testing.T) {
	p := buildAndResolve(t, "full")
	storageID := symbolID(t, p, "src/services/storage.ts", "Storage")

	ctx, err := Extract(p, storageID)
	require.NoError(t, err)

	assert.Equal(t, storageID, ctx.Target.Symbol.ID)
	assert.Equal(t, graph.SymbolClass, ctx.Target.Symbol.Kind)
	// `export default` lives on the wrapping export_statement, not
	// the class_declaration node, so the class symbol's range
	// covers only `class Storage { ... }`. The default-export
	// status is reflected in Symbol.IsDefaultExport instead.
	assert.Contains(t, ctx.Target.Source, "class Storage")
	assert.Contains(t, ctx.Target.Source, "save(todo: Todo): void")
	assert.True(t, ctx.Target.Symbol.IsDefaultExport,
		"Storage is the default export of storage.ts")

	// Top-level class targets have no enclosing type.
	assert.Nil(t, ctx.EnclosingType)

	// Class targets don't aggregate method callees.
	assert.Empty(t, ctx.Callees)

	// main is the sole caller via its `new Storage()`.
	require.Len(t, ctx.Callers, 1)
	assert.Equal(t, "main", ctx.Callers[0].Symbol.Name)
}

// TestExtract_interfaceTarget pins interface-kind targets: source
// filled in, no enclosing type, no callees.
func TestExtract_interfaceTarget(t *testing.T) {
	p := buildAndResolve(t, "full")
	todoLikeID := symbolID(t, p, "src/models/types.ts", "TodoLike")

	ctx, err := Extract(p, todoLikeID)
	require.NoError(t, err)

	assert.Equal(t, graph.SymbolInterface, ctx.Target.Symbol.Kind)
	assert.Contains(t, ctx.Target.Source, "interface TodoLike extends Identifiable")
	assert.Contains(t, ctx.Target.Source, "complete(): void;")

	assert.Nil(t, ctx.EnclosingType)
	assert.Empty(t, ctx.Callees)
}

// TestExtract_enumTarget pins enum-kind targets.
func TestExtract_enumTarget(t *testing.T) {
	p := buildAndResolve(t, "full")
	priorityID := symbolID(t, p, "src/models/types.ts", "Priority")

	ctx, err := Extract(p, priorityID)
	require.NoError(t, err)

	assert.Equal(t, graph.SymbolEnum, ctx.Target.Symbol.Kind)
	assert.Contains(t, ctx.Target.Source, "enum Priority")
	assert.Contains(t, ctx.Target.Source, "Medium")

	assert.Nil(t, ctx.EnclosingType)
	assert.Empty(t, ctx.Callees)
}

// TestExtract_typeAliasTarget pins type-alias-kind targets.
func TestExtract_typeAliasTarget(t *testing.T) {
	p := buildAndResolve(t, "full")
	todoIDID := symbolID(t, p, "src/models/types.ts", "TodoId")

	ctx, err := Extract(p, todoIDID)
	require.NoError(t, err)

	assert.Equal(t, graph.SymbolTypeAlias, ctx.Target.Symbol.Kind)
	assert.Contains(t, ctx.Target.Source, "type TodoId = string")

	assert.Nil(t, ctx.EnclosingType)
	assert.Empty(t, ctx.Callees)
}

func TestExtract_worksOnLoadedProject(t *testing.T) {
	// A Project freshly loaded from SQLite must produce the same
	// Context as the in-memory original.
	original := buildAndResolve(t, "resolution")
	mainID := symbolID(t, original, "main.ts", "main")

	inMemory, err := Extract(original, mainID)
	require.NoError(t, err)

	dbPath := filepath.Join(t.TempDir(), "graph.db")
	{
		s, err := store.Open(dbPath)
		require.NoError(t, err)
		require.NoError(t, s.Save(original))
		require.NoError(t, s.Close())
	}

	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()
	loaded, err := s.Load()
	require.NoError(t, err)
	require.Nil(t, loaded.Files, "store.Load leaves Files unset")

	fromLoaded, err := Extract(loaded, mainID)
	require.NoError(t, err)

	assertContextsMatch(t, inMemory, fromLoaded)
}

// assertContextsMatch compares Context fields with order-insensitive
// slice comparison for Imports / Callees / Callers.
func assertContextsMatch(t *testing.T, want, got *Context) {
	t.Helper()
	assert.Equal(t, want.Target, got.Target, "target")
	assert.Equal(t, want.EnclosingType, got.EnclosingType, "enclosing type")

	wantImports := append([]ImportEntry(nil), want.Imports...)
	gotImports := append([]ImportEntry(nil), got.Imports...)
	sortImports(wantImports)
	sortImports(gotImports)
	assert.Equal(t, wantImports, gotImports, "imports")

	assertCalleesMatch(t, want.Callees, got.Callees)
	assertCallersMatch(t, want.Callers, got.Callers)
}

func sortImports(in []ImportEntry) {
	sort.Slice(in, func(i, j int) bool {
		if in[i].Edge.StartByte != in[j].Edge.StartByte {
			return in[i].Edge.StartByte < in[j].Edge.StartByte
		}
		return in[i].Edge.Path < in[j].Edge.Path
	})
}

func assertCalleesMatch(t *testing.T, want, got []Callee) {
	t.Helper()
	wantByID := map[string]Callee{}
	for _, c := range want {
		wantByID[c.Symbol.ID] = c
	}
	require.Equal(t, len(want), len(got), "callee count")
	for _, c := range got {
		w, ok := wantByID[c.Symbol.ID]
		require.True(t, ok, "unexpected callee %q", c.Symbol.ID)
		assert.Equal(t, w.Symbol, c.Symbol)
		assert.Equal(t, w.Signature, c.Signature)
		assert.Equal(t, w.File, c.File)
		assert.ElementsMatch(t, w.CallSites, c.CallSites)
	}
}

func assertCallersMatch(t *testing.T, want, got []Caller) {
	t.Helper()
	wantByID := map[string]Caller{}
	for _, c := range want {
		wantByID[c.Symbol.ID] = c
	}
	require.Equal(t, len(want), len(got), "caller count")
	for _, c := range got {
		w, ok := wantByID[c.Symbol.ID]
		require.True(t, ok, "unexpected caller %q", c.Symbol.ID)
		assert.Equal(t, w.Symbol, c.Symbol)
		assert.Equal(t, w.Signature, c.Signature)
		assert.Equal(t, w.File, c.File)
		assert.ElementsMatch(t, w.CallSites, c.CallSites)
	}
}
