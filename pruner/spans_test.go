package pruner

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	redactedFileHeader = regexp.MustCompile(`^# (\S+)$`)
	redactedLine       = regexp.MustCompile(`^(\d+): `)
)

// redactedLines parses a RenderRedacted output into the ordered list
// of files and the set of line numbers printed for each.
func redactedLines(t *testing.T, out string) ([]string, map[string][]int) {
	t.Helper()
	var files []string
	lines := map[string][]int{}
	current := ""
	for _, l := range strings.Split(out, "\n") {
		if m := redactedFileHeader.FindStringSubmatch(l); m != nil {
			current = m[1]
			files = append(files, current)
			continue
		}
		if m := redactedLine.FindStringSubmatch(l); m != nil {
			n, err := strconv.Atoi(m[1])
			require.NoError(t, err)
			lines[current] = append(lines[current], n)
		}
	}
	return files, lines
}

func expandSpans(spans []LineSpan) []int {
	var out []int
	for _, s := range spans {
		for n := s.Start; n <= s.End; n++ {
			out = append(out, n)
		}
	}
	return out
}

// TestRenderSpans_matchesRedactedLines pins the contract the
// evaluation harness relies on: the spans cover exactly the lines
// RenderRedacted prints, file by file, in the same order.
func TestRenderSpans_matchesRedactedLines(t *testing.T) {
	cases := []struct{ file, name string }{
		{"src/models/todo.ts", "summary"},
		{"src/index.ts", "main"},
		{"src/services/api.ts", "createTodo"},
		{"src/services/storage.ts", "save"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := buildAndResolve(t, "full")
			id := symbolID(t, p, c.file, c.name)
			ctx, err := ExtractWithOptions(p, id, ExtractOptions{
				CallerDepth: 2, CalleeDepth: 2, CallerBodyDepth: 1,
				CalleeBodyDepth: 1, MaxPerLevel: 50, TypeDepth: 1,
			})
			require.NoError(t, err)

			out, err := RenderRedacted(ctx, p)
			require.NoError(t, err)
			wantFiles, wantLines := redactedLines(t, out)

			spans, err := RenderSpans(ctx, p)
			require.NoError(t, err)

			var gotFiles []string
			for _, fs := range spans {
				gotFiles = append(gotFiles, fs.File)
				assert.Equal(t, wantLines[fs.File], expandSpans(fs.Spans), fs.File)
			}
			assert.Equal(t, wantFiles, gotFiles)
		})
	}
}

// TestRenderSpans_spansAreDisjointAndOrdered checks each file's spans
// are sorted, 1-based, inclusive and separated by at least one line.
func TestRenderSpans_spansAreDisjointAndOrdered(t *testing.T) {
	p := buildAndResolve(t, "full")
	id := symbolID(t, p, "src/models/todo.ts", "summary")
	ctx, err := Extract(p, id)
	require.NoError(t, err)

	spans, err := RenderSpans(ctx, p)
	require.NoError(t, err)
	require.NotEmpty(t, spans)
	for _, fs := range spans {
		require.NotEmpty(t, fs.Spans, fs.File)
		for i, s := range fs.Spans {
			assert.GreaterOrEqual(t, s.Start, 1)
			assert.LessOrEqual(t, s.Start, s.End)
			if i > 0 {
				assert.Greater(t, s.Start, fs.Spans[i-1].End+1)
			}
		}
	}
}
