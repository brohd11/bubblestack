package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Span is a run of text sharing one style. A line's spans must concatenate to its text
// exactly, or the editor renders the line plain. A nil Style renders unstyled.
//
// Style is a pointer into the highlighter's palette table because lipgloss.Style is large
// (about 650 bytes) and documents have millions of spans. A palette change replaces the
// table, so older spans keep their colors until the reparse.
type Span struct {
	Text  string
	Style *lipgloss.Style
}

// SpanStyle is the style to render sp with, and false when the run is unstyled — the
// nil case callers must not dereference.
func (sp Span) SpanStyle() (lipgloss.Style, bool) {
	if sp.Style == nil {
		return lipgloss.Style{}, false
	}
	return *sp.Style, true
}

// Highlighter parses a whole document and answers per-row spans, keeping multi-line state
// (fenced blocks, block comments) internally. A direct Opts.Highlighter is parsed lazily
// after edits pause; a LanguageConfig factory lets the editor parse fresh instances
// asynchronously and preview fragments.
type Highlighter interface {
	// Parse ingests document text (lines joined with '\n'). Factory-created instances
	// may receive either the full document or a row-aligned preview fragment.
	Parse(text string)
	// HighlightLine returns the spans for 0-based row, or nil for unstyled. They must
	// concatenate to the row's text.
	HighlightLine(row int) []Span
}

// HighlightPreviewProvider is the optional stateful preview: from the on-screen snapshot,
// build an independent highlighter seeded for a fragment starting at snapshotLine.
// HighlightRestartProvider still picks the lexical restart row first. A constructor keeps
// parser state private to the host.
type HighlightPreviewProvider interface {
	NewHighlightPreview(snapshotLine int) Highlighter
}

// HighlightRestartProvider optionally names an earlier row (the start of a multi-line
// string or fence) from which a fragment can be parsed with useful context. It is a hint;
// invalid answers are ignored, and a full parse follows.
type HighlightRestartProvider interface {
	HighlightRestartLine(row int) int
}

// HighlightRange is a host style override over rune columns, on one line only (ranges
// spanning rows are ignored). A nil Style removes the lexical style.
type HighlightRange struct {
	Range Range
	Style *lipgloss.Style
}

// spansMatchLine reports whether spans concatenate to line. It walks rather than joining,
// since it runs for every visible row every frame.
func spansMatchLine(spans []Span, line []rune) bool {
	at := 0
	for _, sp := range spans {
		for _, r := range sp.Text {
			if at >= len(line) || line[at] != r {
				return false
			}
			at++
		}
	}
	return at == len(line)
}

// spansText is the concatenated text of spans — the same contract spelled out as a
// string, for the callers that need the text itself rather than the verdict.
func spansText(spans []Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}
