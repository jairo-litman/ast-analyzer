package pruner

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRenderRedacted_keepsDocCommentOfTargetMethod confirms that the
// JSDoc above the target method survives into the output.
func TestRenderRedacted_keepsDocCommentOfTargetMethod(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "one")
	assert.Contains(t, out, "Doc for one.")
}

// TestRenderRedacted_keepsClassLevelDoc confirms that the class's own
// JSDoc is rendered above the class header.
func TestRenderRedacted_keepsClassLevelDoc(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "one")
	assert.Contains(t, out, "Class-level doc.")
}

// TestRenderRedacted_keepsDocCommentOfKeptCallee confirms that the
// JSDoc above a callee (kept method) is retained.
func TestRenderRedacted_keepsDocCommentOfKeptCallee(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "one")
	assert.Contains(t, out, "Doc for two.")
}

// TestRenderRedacted_elidesDocCommentOfElidedMethod confirms that
// the JSDoc above a non-kept method is subtracted along with the
// method body — no orphan doc above a cut marker.
func TestRenderRedacted_elidesDocCommentOfElidedMethod(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "one")
	assert.NotContains(t, out, "Doc for three.")
}

// TestRenderRedacted_keepsLineCommentDocForStandalone confirms that
// consecutive `//` lines immediately above a function are pulled in
// as the doc block.
func TestRenderRedacted_keepsLineCommentDocForStandalone(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "standalone")
	assert.Contains(t, out, "Line-style doc for standalone.")
	assert.Contains(t, out, "Second line of the same doc block.")
}

// TestRenderRedacted_keepsDocForArrowFunctionConst confirms that the
// doc comment above `export const fn = (...) => {...}` is pulled in.
// The Symbol's StartByte sits at the binding name or the `const`
// keyword, so the modifier walk has to skip past `const` (and
// `export`) before finding the JSDoc.
func TestRenderRedacted_keepsDocForArrowFunctionConst(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "arrowConst")
	assert.Contains(t, out, "Arrow-function const with JSDoc.")
}

// TestRenderRedacted_collapsesConsecutiveCutMarkers confirms that the
// renderer no longer emits the `<- cut content ->\n<blank>\n<- cut content ->`
// pattern that arose from blank lines between elided methods.
func TestRenderRedacted_collapsesConsecutiveCutMarkers(t *testing.T) {
	out, _ := renderTarget(t, "docs", "main.ts", "one")

	// Strip line-number prefixes so the regex sees the raw section
	// boundaries. The pattern matches a cut marker, then any number
	// of whitespace-only line-numbered lines, then a second cut marker
	// — which is exactly the noise we're collapsing.
	noiseRe := regexp.MustCompile(`<- cut content ->\n(?:\d+:\s*\n)*<- cut content ->`)
	assert.False(t, noiseRe.MatchString(out),
		"output still contains stacked cut markers separated only by blank lines:\n%s", out)

	// Sanity: at least one cut marker should still be present (one
	// is fine — elided methods need to be marked).
	assert.True(t, strings.Contains(out, "<- cut content ->"),
		"expected at least one cut marker in the output, got:\n%s", out)
}
