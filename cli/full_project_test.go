package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jairo-litman/ast-analyzer/pruner"
	"github.com/jairo-litman/ast-analyzer/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fullFixtureRoot     = "../graph/testdata/full"
	fullFixtureTsconfig = "../graph/testdata/full/tsconfig.json"
)

// runCLI invokes cli.Run with captured streams and fails the test on
// a non-zero exit.
func runCLI(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	var sout, serr bytes.Buffer
	code := Run(args, &sout, &serr)
	require.Equal(t, 0, code, "args=%v\nstderr=%s", args, serr.String())
	return sout.String(), serr.String()
}

// TestFullProject_listIncludesAllSymbols pins the symbol catalog
// across every supported declaration kind in the fixture.
func TestFullProject_listIncludesAllSymbols(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	stdout, _ := runCLI(t, "list", "--db", dbPath, fullFixtureRoot)

	// Header is always present.
	assert.Contains(t, stdout, "ID")
	assert.Contains(t, stdout, "KIND")
	assert.Contains(t, stdout, "NAME")
	assert.Contains(t, stdout, "FILE")

	// One row per expected (name, kind, file) triple. The tabwriter
	// pads with spaces, so substring matching is more reliable than
	// splitting.
	expectations := []struct{ name, kind, file string }{
		// models/types.ts
		{"Priority", "enum", "models/types.ts"},
		{"TodoId", "type_alias", "models/types.ts"},
		{"Identifiable", "interface", "models/types.ts"},
		{"TodoLike", "interface", "models/types.ts"},
		// models/todo.ts
		{"BaseTodo", "class", "models/todo.ts"},
		{"Todo", "class", "models/todo.ts"},
		{"describe", "function", "models/todo.ts"}, // method captured as function-kind
		{"summary", "function", "models/todo.ts"},
		{"complete", "function", "models/todo.ts"},
		// services/api.ts
		{"createTodo", "function", "services/api.ts"},
		{"findHighPriority", "function", "services/api.ts"},
		// services/storage.ts
		{"Storage", "class", "services/storage.ts"},
		{"save", "function", "services/storage.ts"},
		{"findAll", "function", "services/storage.ts"},
		// utils/format.ts (arrow-via-const)
		{"formatTodo", "function", "utils/format.ts"},
		{"formatList", "function", "utils/format.ts"},
		// src/index.ts
		{"main", "function", "index.ts"},
	}

	for _, exp := range expectations {
		assert.True(t,
			containsSymbolRow(stdout, exp.name, exp.kind, exp.file),
			"missing %s/%s in %s", exp.kind, exp.name, exp.file)
	}

	// index.ts has top-level calls, so it carries a module symbol.
	assert.True(t,
		strings.Contains(stdout, "module"),
		"index.ts should produce a module-kind synthetic symbol; output:\n%s", stdout)
}

func containsSymbolRow(out, name, kind, fileSuffix string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, name) &&
			strings.Contains(line, kind) &&
			strings.HasSuffix(strings.TrimRight(line, " \t"), fileSuffix) {
			return true
		}
	}
	return false
}

// TestFullProject_extractMain pins main()'s call graph: cross-file
// callee resolutions across default import, re-export chain, and
// namespace member, the deduped callee list, and the module-scope
// caller from `main()` at the bottom of src/index.ts.
func TestFullProject_extractMain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	mainID := lookupSymbolIDFromFullFixture(t, "src/index.ts", "main")

	stdout, _ := runCLI(t, "extract",
		"--db", dbPath,
		fullFixtureRoot, mainID)

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal([]byte(stdout), &ctx))

	assert.Equal(t, mainID, ctx.Target.Symbol.ID)
	assert.Contains(t, ctx.Target.Source, "function main()")
	assert.Contains(t, ctx.Target.Source, "Format.formatList(high)")

	// Four resolved callees: Storage (default import), createTodo
	// (aliased re-export), findHighPriority (star re-export),
	// formatList (namespace member). The storage.save / .findAll and
	// console.log calls stay unresolved (need type inference).
	calleeNames := map[string]bool{}
	for _, c := range ctx.Callees {
		calleeNames[c.Symbol.Name] = true
	}
	for _, expected := range []string{"Storage", "createTodo", "findHighPriority", "formatList"} {
		assert.True(t, calleeNames[expected],
			"main() should resolve a callee to %q; got %v", expected, calleeNames)
	}

	// src/index.ts has four imports, but `import type { TodoLike }`
	// is unused by main() and gets filtered out.
	require.Len(t, ctx.Imports, 3)
	for _, imp := range ctx.Imports {
		assert.NotContains(t, imp.Edge.Path, "@models/types",
			"unused TodoLike import should be filtered out")
	}

	// main() is invoked at module scope, so the synthetic module
	// symbol is its only caller.
	require.Len(t, ctx.Callers, 1)
	assert.Contains(t, ctx.Callers[0].Symbol.ID, "#module")
}

// TestFullProject_extractClassMethodWithEnclosingType pins the
// stripped class header and super.describe() resolution for
// Todo.summary.
func TestFullProject_extractClassMethodWithEnclosingType(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	summaryID := lookupSymbolIDFromFullFixture(t, "src/models/todo.ts", "summary")

	stdout, _ := runCLI(t, "extract",
		"--db", dbPath,
		fullFixtureRoot, summaryID)

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal([]byte(stdout), &ctx))

	require.NotNil(t, ctx.EnclosingType, "Todo.summary must carry its enclosing class header")
	assert.Equal(t, "Todo", ctx.EnclosingType.Symbol.Name)
	assert.Contains(t, ctx.EnclosingType.Source, "class Todo extends BaseTodo")
	assert.Contains(t, ctx.EnclosingType.Source, "summary(): string;")
	// Method bodies are stripped from the header.
	assert.NotContains(t, ctx.EnclosingType.Source, `return super.describe()`)

	// super.describe() resolves up to BaseTodo.describe.
	calleeNames := map[string]bool{}
	for _, c := range ctx.Callees {
		calleeNames[c.Symbol.Name] = true
	}
	assert.True(t, calleeNames["describe"],
		"super.describe() in Todo.summary should resolve through extends to BaseTodo.describe")
}

// TestFullProject_extractCallerThroughReExportChain pins re-export
// chain following: createTodo lives in api.ts, main imports it via
// services/index.ts's `export { createTodo as makeTodo }`, and
// Callers must still link main back to createTodo.
func TestFullProject_extractCallerThroughReExportChain(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	createTodoID := lookupSymbolIDFromFullFixture(t, "src/services/api.ts", "createTodo")

	stdout, _ := runCLI(t, "extract",
		"--db", dbPath,
		fullFixtureRoot, createTodoID)

	var ctx pruner.Context
	require.NoError(t, json.Unmarshal([]byte(stdout), &ctx))

	calleeNames := map[string]bool{}
	for _, c := range ctx.Callees {
		calleeNames[c.Symbol.Name] = true
	}
	assert.True(t, calleeNames["Todo"], "createTodo's `new Todo(...)` should resolve to the Todo class")

	callerNames := map[string]bool{}
	for _, c := range ctx.Callers {
		callerNames[c.Symbol.Name] = true
	}
	assert.True(t, callerNames["main"],
		"main should appear as a caller of createTodo via the re-export chain")
}

// TestFullProject_extractRedactedFormat pins --format=redacted on a
// class-method target: file headers, line numbers, cut markers, and
// off-target methods elided.
func TestFullProject_extractRedactedFormat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	summaryID := lookupSymbolIDFromFullFixture(t, "src/models/todo.ts", "summary")

	stdout, _ := runCLI(t, "extract",
		"--db", dbPath,
		"--format", "redacted",
		fullFixtureRoot, summaryID)

	assert.Contains(t, stdout, "# src/models/todo.ts")
	assert.Contains(t, stdout, "<- cut content ->")
	// Line numbers are 1-based and absolute.
	assert.Regexp(t, `(?m)^1: import `, stdout)
	// Target method body and its callee both stay visible.
	assert.Contains(t, stdout, "summary(): string")
	assert.Contains(t, stdout, "super.describe()")
	assert.Contains(t, stdout, "describe(): string")

	// Todo.complete() is a sibling method — body and signature must
	// be elided. BaseTodo's abstract `complete(): void;` declaration
	// stays because abstract members have no function-kind symbol to
	// subtract.
	assert.NotContains(t, stdout, "this.completed = true",
		"Todo.complete()'s body must be elided")
	assert.NotContains(t, stdout, "complete(): void {",
		"Todo.complete()'s opening line must be elided")
}

// TestFullProject_extractRejectsBadFormat checks that --format
// validation fires before the project build.
func TestFullProject_extractRejectsBadFormat(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")
	indexFixtureToDB(t, fullFixtureRoot, fullFixtureTsconfig, dbPath)
	summaryID := lookupSymbolIDFromFullFixture(t, "src/models/todo.ts", "summary")

	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"extract",
		"--db", dbPath,
		"--format", "yaml",
		fullFixtureRoot, summaryID,
	}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "format")
}

// TestFullProject_indexRoundtrip persists and reloads the project,
// confirming the persisted graph reflects the fixture's broad shape.
// Counts are lower bounds to stay decoupled from fixture edits.
func TestFullProject_indexRoundtrip(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "full.db")

	stdout, _ := runCLI(t, "index",
		"--tsconfig", fullFixtureTsconfig,
		"--output", dbPath,
		fullFixtureRoot)

	assert.Contains(t, stdout, "indexed")
	assert.Contains(t, stdout, dbPath)

	s, err := store.Open(dbPath)
	require.NoError(t, err)
	defer s.Close()

	loaded, err := s.Load()
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(loaded.Symbols), 15,
		"expected at least 15 symbols across the fixture")
	assert.GreaterOrEqual(t, len(loaded.Imports), 8,
		"expected at least 8 import edges across the fixture")
	assert.GreaterOrEqual(t, len(loaded.ReExports), 2,
		"services/index.ts contributes at least two re-export edges")

	resolved := 0
	for _, c := range loaded.Calls {
		if len(c.ResolvedTo) > 0 {
			resolved++
		}
	}
	assert.GreaterOrEqual(t, resolved, 5,
		"at least five calls should resolve cross-file (Storage, makeTodo×2, findHighPriority, formatList, super.describe, new Todo)")
}

// lookupSymbolIDFromFullFixture rebuilds the full fixture just to
// obtain a stable Symbol ID for the CLI command under test.
func lookupSymbolIDFromFullFixture(t *testing.T, file, name string) string {
	t.Helper()
	return lookupSymbolIDFromFixture(t, "full", file, name)
}
