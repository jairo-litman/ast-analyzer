package pruner

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRenderRedacted_moduleCallerShowsEnclosingBlock confirms that
// when the target is called from module scope inside a test block
// (e.g. `it('...', () => { target(...) })`), the rendered output
// includes the surrounding `it(...)` opener and not just the bare
// call expression. Same for the outer `describe(...)` when the call
// sits close to it.
func TestRenderRedacted_moduleCallerShowsEnclosingBlock(t *testing.T) {
	out, _ := renderTarget(t, "module_caller", "target.ts", "renderTitle")

	// The first call lives inside `it('returns the title', ...)`.
	// Expansion should pull that line into the rendered snippet.
	assert.Contains(t, out, "it('returns the title'",
		"module-scope caller rendering should include the enclosing `it(...)` opener")

	// And the second `it(...)`.
	assert.Contains(t, out, "it('escapes special chars'",
		"second module-scope call site's enclosing `it(...)` should also appear")
}
