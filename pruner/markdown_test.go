package pruner

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// markdownTarget builds the named fixture, runs Extract, and renders
// markdown for the requested symbol.
func markdownTarget(t *testing.T, fixture, file, name string) string {
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
	out, err := RenderMarkdown(ctx, p)
	require.NoError(t, err)
	return out
}

// TestRenderMarkdown_classMethodTarget pins the markdown shape on a
// class-method extraction: top-level title, per-file `## file:`
// headings, fenced ts code blocks, line numbers, and cut markers
// inside the fence.
func TestRenderMarkdown_classMethodTarget(t *testing.T) {
	out := markdownTarget(t, "full", "src/models/todo.ts", "summary")

	// Top-level title.
	assert.Regexp(t, `(?m)^# Extracted context`, out)

	// File heading uses `## file:` so multiple files don't collide
	// with the title's heading level.
	assert.Contains(t, out, "## file: src/models/todo.ts")

	// Each file section is wrapped in a fenced ts code block.
	assert.Contains(t, out, "```ts\n")
	assert.Contains(t, out, "```\n")

	// Line-numbered source lives inside the fence; an unnumbered raw
	// line of the source should NOT appear at column zero (would
	// indicate the format dropped line-numbering).
	assert.Regexp(t, `(?m)^1: import `, out)

	// Cut markers are kept verbatim — markdown doesn't escape them.
	assert.Contains(t, out, cutMarker)

	// Target body intact.
	assert.Contains(t, out, "summary(): string")
}

// TestRenderMarkdown_fileSectionsAreFencedIndividually pins that each
// file section opens a fence and closes it, so an LLM can chunk the
// markdown safely.
func TestRenderMarkdown_fileSectionsAreFencedIndividually(t *testing.T) {
	out := markdownTarget(t, "full", "src/index.ts", "main")

	// `\`\`\`ts\n` opens, `\`\`\`\n` closes. The two counts must
	// match for the markdown to be balanced.
	openCount := strings.Count(out, "```ts\n")
	closeCount := strings.Count(out, "```\n")
	assert.Greater(t, openCount, 1, "expected multiple files in main()'s context")
	assert.Equal(t, openCount, closeCount, "open/close fence count mismatch:\n%s", out)
}

// TestRenderMarkdown_nonClassTarget pins markdown rendering on a
// non-function kind (interface) — same general format but no
// enclosing scaffolding.
func TestRenderMarkdown_nonClassTarget(t *testing.T) {
	out := markdownTarget(t, "full", "src/models/types.ts", "TodoLike")
	assert.Contains(t, out, "## file: src/models/types.ts")
	assert.Contains(t, out, "interface TodoLike extends Identifiable")
}
