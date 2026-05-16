package pruner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renderTarget builds a fixture, extracts the named symbol, and
// returns the redacted render plus its Context.
func renderTarget(t *testing.T, fixture, file, name string) (string, *Context) {
	t.Helper()
	p := buildAndResolve(t, fixture)
	var id string
	for _, s := range p.Symbols {
		if s.File == file && s.Name == name {
			id = s.ID
			break
		}
	}
	require.NotEmpty(t, id, "no symbol %q in %s", name, file)
	ctx, err := Extract(p, id)
	require.NoError(t, err)
	out, err := RenderRedacted(ctx, p)
	require.NoError(t, err)
	return out, ctx
}

// TestRenderRedacted_classMethodTarget pins the central case: class
// method target produces a class scaffolding with non-target methods
// elided, constructor and callees still visible, and line numbers
// prefixed.
func TestRenderRedacted_classMethodTarget(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/models/todo.ts", "summary")

	assert.Contains(t, out, "# src/models/todo.ts")
	assert.Contains(t, out, `1: import { Priority, TodoLike } from "@models/types";`)
	assert.Contains(t, out, "export class Todo extends BaseTodo")

	// Constructors are exempt from elision.
	assert.Contains(t, out, "constructor(id: TodoId")
	assert.Contains(t, out, "super(id, title, priority)")

	// Target body and its callee both stay visible.
	assert.Contains(t, out, "summary(): string")
	assert.Contains(t, out, `return super.describe() + (this.completed ? " [done]" : "");`)
	assert.Contains(t, out, "describe(): string")
	assert.Contains(t, out, `return this.title + " (" + this.priority + ")";`)

	// Todo.complete() — sibling, not target/callee/caller/constructor
	// — is elided. BaseTodo's abstract `complete(): void;` stays
	// because abstract members have no body to subtract.
	assert.NotContains(t, out, "this.completed = true",
		"Todo.complete()'s body must be elided")
	assert.NotContains(t, out, "complete(): void {",
		"Todo.complete()'s opening line must be elided")

	assert.Contains(t, out, "<- cut content ->")
}

// TestRenderRedacted_topLevelFunctionTarget covers a target with no
// enclosing class: target body + imports stay, callees/callers in
// other files surface in their own sections.
func TestRenderRedacted_topLevelFunctionTarget(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/index.ts", "main")

	assert.Contains(t, out, "# src/index.ts")
	assert.Contains(t, out, `1: import Storage from "@services/storage";`)
	assert.Contains(t, out, "function main(): void")
	assert.Contains(t, out, "console.log(Format.formatList(high))")

	// Module-symbol callers surface their call-site lines so the LLM
	// can see where the target is invoked from at file scope. The
	// bottom-of-file `main();` is line 15.
	assert.Regexp(t, `(?m)^15: main\(\);`, out)

	// Each callee file surfaces in its own section.
	assert.Contains(t, out, "# src/services/api.ts")
	assert.Contains(t, out, "export function createTodo")
	assert.Contains(t, out, "export function findHighPriority")

	assert.Contains(t, out, "# src/services/storage.ts")
	assert.Contains(t, out, "export default class Storage")

	assert.Contains(t, out, "# src/utils/format.ts")
	assert.Contains(t, out, "formatList")
}

// TestRenderRedacted_calleeAndCallerAcrossFiles pins cross-file
// caller resolution: main reaches createTodo via the makeTodo aliased
// re-export and surfaces in its own file section.
func TestRenderRedacted_calleeAndCallerAcrossFiles(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/services/api.ts", "createTodo")

	assert.Contains(t, out, "# src/services/api.ts")
	assert.Contains(t, out, "export function createTodo")
	assert.Contains(t, out, "return new Todo(id, title, Priority.Medium)")

	// findHighPriority is a sibling — neither callee nor caller — so
	// its body is elided from api.ts.
	assert.NotContains(t, out, "for (const todo of todos)",
		"findHighPriority's body should be elided from api.ts")
	assert.NotContains(t, out, "todo.priority === Priority.High",
		"findHighPriority's body should be elided from api.ts")

	assert.Contains(t, out, "# src/index.ts")
	assert.Contains(t, out, "function main(): void")

	// Todo class (the constructor callee) appears in its own section.
	assert.Contains(t, out, "# src/models/todo.ts")
	assert.Contains(t, out, "export class Todo extends BaseTodo")
}

// TestRenderRedacted_lineNumbersAreAbsolute pins that line numbers
// reflect the original file's 1-based numbering, not the rendered
// output's local counter.
func TestRenderRedacted_lineNumbersAreAbsolute(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/models/todo.ts", "summary")

	// Imports are at lines 1-2 of todo.ts; their rendered prefixes
	// must match.
	assert.Regexp(t, `(?m)^1: import `, out)
	assert.Regexp(t, `(?m)^2: import type `, out)

	// Target method appears around line 34 in the source — the
	// absolute number, not "line 1" of its slice.
	assert.Regexp(t, `(?m)^\d+: +summary\(\): string `, out)
}

// TestRenderRedacted_cutMarkersAtFileBoundaries pins that markers
// appear at file start/end only when kept content isn't already at
// the boundary.
func TestRenderRedacted_cutMarkersAtFileBoundaries(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/index.ts", "main")

	// index.ts kept content starts at line 1 (imports), so no leading
	// cut marker.
	indexBody := sliceFileSectionBody(t, out, "# src/index.ts")
	assert.True(t,
		strings.HasPrefix(indexBody, "1: "),
		"index.ts body should start with `1: `, got: %q", indexBody[:min(len(indexBody), 80)])

	// api.ts kept content starts at line 4, so a leading cut marker
	// is required.
	apiBody := sliceFileSectionBody(t, out, "# src/services/api.ts")
	assert.True(t,
		strings.HasPrefix(apiBody, "<- cut content ->"),
		"api.ts body should start with a cut marker, got: %q", apiBody[:min(len(apiBody), 80)])
	assert.True(t,
		strings.Index(apiBody, "<- cut content ->") < strings.Index(apiBody, "4: "),
		"cut marker must precede line 4 in api.ts body")
}

// sliceFileSectionBody returns the source body of one `# <file>`
// section, excluding the header line and any leading metadata
// comments (`// callees: ...`, `// callers: ...`).
func sliceFileSectionBody(t *testing.T, out, header string) string {
	t.Helper()
	start := strings.Index(out, header)
	require.GreaterOrEqual(t, start, 0, "header %q not in output", header)
	bodyStart := start + len(header)
	if bodyStart < len(out) && out[bodyStart] == '\n' {
		bodyStart++
	}
	rest := out[bodyStart:]
	if next := strings.Index(rest, "\n# "); next >= 0 {
		rest = rest[:next]
	}
	for strings.HasPrefix(rest, "// ") {
		nl := strings.Index(rest, "\n")
		if nl < 0 {
			break
		}
		rest = rest[nl+1:]
	}
	return rest
}

// TestRenderRedacted_classTargetShownInFull pins that when the class
// itself is the target, the class is rendered without method elision.
// Other classes pulled in via callee/caller paths keep the redaction.
func TestRenderRedacted_classTargetShownInFull(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/services/storage.ts", "Storage")

	assert.Contains(t, out, "# src/services/storage.ts")
	assert.Contains(t, out, "class Storage")

	// Every method body is shown when the class itself is the target.
	assert.Contains(t, out, "constructor() {")
	assert.Contains(t, out, "this.items = [];")
	assert.Contains(t, out, "save(todo: Todo): void")
	assert.Contains(t, out, "this.items.push(todo)")
	assert.Contains(t, out, "findAll(): Todo[]")
	assert.Contains(t, out, "return this.items;")
}

// TestRenderRedacted_interfaceTargetShown pins interface-kind
// targets: just the declaration, no surrounding scaffolding.
func TestRenderRedacted_interfaceTargetShown(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/models/types.ts", "TodoLike")

	assert.Contains(t, out, "# src/models/types.ts")
	assert.Contains(t, out, "interface TodoLike extends Identifiable")
	assert.Contains(t, out, "complete(): void;")
}

// TestRenderRedacted_enumTargetShown pins enum-kind targets.
func TestRenderRedacted_enumTargetShown(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/models/types.ts", "Priority")

	assert.Contains(t, out, "# src/models/types.ts")
	assert.Contains(t, out, "enum Priority")
	assert.Contains(t, out, "Medium")
	assert.Contains(t, out, "High")
}

// TestRenderRedacted_typeAliasTargetShown pins type-alias targets.
func TestRenderRedacted_typeAliasTargetShown(t *testing.T) {
	out, _ := renderTarget(t, "full", "src/models/types.ts", "TodoId")

	assert.Contains(t, out, "# src/models/types.ts")
	assert.Contains(t, out, "type TodoId = string")
}

func TestSubtractRanges(t *testing.T) {
	cases := []struct {
		name             string
		ranges, subtract []byteRange
		want             []byteRange
	}{
		{
			name:     "no overlap leaves range untouched",
			ranges:   []byteRange{{10, 20}},
			subtract: []byteRange{{30, 40}},
			want:     []byteRange{{10, 20}},
		},
		{
			name:     "subtract in middle splits left and right",
			ranges:   []byteRange{{0, 100}},
			subtract: []byteRange{{30, 50}},
			want:     []byteRange{{0, 30}, {50, 100}},
		},
		{
			name:     "subtract at start leaves only right",
			ranges:   []byteRange{{0, 100}},
			subtract: []byteRange{{0, 50}},
			want:     []byteRange{{50, 100}},
		},
		{
			name:     "subtract at end leaves only left",
			ranges:   []byteRange{{0, 100}},
			subtract: []byteRange{{50, 100}},
			want:     []byteRange{{0, 50}},
		},
		{
			name:     "fully covering subtract eliminates range",
			ranges:   []byteRange{{0, 100}},
			subtract: []byteRange{{0, 100}},
			want:     nil,
		},
		{
			name:     "two non-overlapping subtracts split into three pieces",
			ranges:   []byteRange{{0, 100}},
			subtract: []byteRange{{20, 30}, {60, 70}},
			want:     []byteRange{{0, 20}, {30, 60}, {70, 100}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := subtractRanges(tc.ranges, tc.subtract)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMergeRanges(t *testing.T) {
	cases := []struct {
		name string
		in   []byteRange
		want []byteRange
	}{
		{
			name: "non-overlapping kept separate",
			in:   []byteRange{{10, 20}, {30, 40}},
			want: []byteRange{{10, 20}, {30, 40}},
		},
		{
			name: "overlapping merged",
			in:   []byteRange{{10, 25}, {20, 40}},
			want: []byteRange{{10, 40}},
		},
		{
			name: "adjacent merged (half-open)",
			in:   []byteRange{{10, 20}, {20, 30}},
			want: []byteRange{{10, 30}},
		},
		{
			name: "out-of-order input sorted before merging",
			in:   []byteRange{{30, 40}, {10, 20}, {15, 25}},
			want: []byteRange{{10, 25}, {30, 40}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeRanges(tc.in)
			assert.Equal(t, tc.want, got)
		})
	}
}
