package pruner

import (
	"fmt"

	"github.com/jairo-litman/ast-analyzer/graph"
)

// LineSpan is a 1-based, inclusive line interval.
type LineSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// FileSpans lists the lines of one file that a render keeps.
type FileSpans struct {
	File  string     `json:"file"`
	Spans []LineSpan `json:"spans"`
}

// LineIndex maps byte offsets of one source file to line numbers.
type LineIndex []uint

// NewLineIndex indexes the line starts of source.
func NewLineIndex(source []byte) LineIndex {
	return LineIndex(computeLineStarts(source))
}

// Span converts the half-open byte range [start, end) to the
// 1-based inclusive lines it covers.
func (ix LineIndex) Span(start, end uint) LineSpan {
	last := end
	if last > start {
		last--
	}
	return LineSpan{Start: lineOf(ix, start), End: lineOf(ix, last)}
}

// RenderSpans returns, per file and in render order, the line spans
// that RenderRedacted and RenderMarkdown print for ctx.
func RenderSpans(ctx *Context, p *graph.Project) ([]FileSpans, error) {
	cache := newSourceCache(p)
	perFile, files, err := computeFileSections(ctx, p, cache)
	if err != nil {
		return nil, err
	}

	out := []FileSpans{}
	for _, f := range files {
		ranges, ok := perFile[f]
		if !ok || len(ranges) == 0 {
			continue
		}
		source, err := cache.source(f)
		if err != nil {
			return nil, fmt.Errorf("source for %s: %w", f, err)
		}
		visible := visibleRanges(source, ranges)
		if len(visible) == 0 {
			continue
		}
		ix := NewLineIndex(source)
		spans := make([]LineSpan, 0, len(visible))
		for _, r := range visible {
			spans = append(spans, ix.Span(r.Start, r.End))
		}
		out = append(out, FileSpans{File: f, Spans: spans})
	}
	return out, nil
}
