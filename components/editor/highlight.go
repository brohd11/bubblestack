package editor

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Span is a run of text sharing one style — the unit a Highlighter answers with.
// The spans of a line, concatenated, must equal the line's text EXACTLY: the
// editor validates that before styling and falls back to the plain render on a
// mismatch, so a buggy highlighter can lose its colors but never corrupt the
// frame. A nil Style renders the run unstyled.
//
// The style is a POINTER into the highlighter's own palette, not a value, because a
// lipgloss.Style is roughly 650 bytes — a Border of thirteen strings, fifteen color
// interfaces and twenty-odd scalars — and a document is millions of spans. Held by
// value, baking a 20k-line file allocated 574 MB and spent a fifth of its CPU in the
// GC handing that memory back, on a goroutine the editor's own frames are waiting on.
// A highlighter therefore hands out pointers to a fixed palette table; a palette
// change replaces that table wholesale rather than writing through it, so spans from
// an earlier parse keep the colors they were baked with until the reparse that
// change triggers.
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

// Highlighter parses a full document and answers per-line highlighting, keeping
// any multi-line state (fenced code blocks, block comments) internally — the
// editor asks by row and never sees the state. It is the language-agnostic
// adapter the editor's syntax coloring hangs on. A host supplies one explicitly through
// Opts.Highlighter or from an LanguageConfig factory.
//
// A direct Opts.Highlighter is parsed lazily on first render and after editing
// pauses. A language factory lets the editor parse fresh instances asynchronously and
// may also receive bounded document fragments for an immediate viewport preview.
type Highlighter interface {
	// Parse ingests document text (lines joined with '\n'). Factory-created instances
	// may receive either the full document or a row-aligned preview fragment.
	Parse(text string)
	// HighlightLine returns styled spans for the 0-based row; nil means the
	// line renders unstyled. The concatenated span texts must equal the row's
	// text (see Span).
	HighlightLine(row int) []Span
}

// HighlightRestartProvider is the optional fast-preview half of Highlighter. An
// implementation may point the editor at a preceding row from which a fragment can be
// parsed in isolation with useful lexical context — the opening row of a multiline
// string or fenced block, for example. The answer is a hint, not the authoritative
// parse: the editor bounds synchronous fragment work and follows it with a full parse.
// Invalid or forward answers are ignored and the edited row is used instead.
type HighlightRestartProvider interface {
	HighlightRestartLine(row int) int
}

// spansMatchLine reports whether spans concatenate to exactly line — the Span contract,
// checked before the editor will style a row.
//
// It walks rather than joining. This runs for every visible row of every frame, and
// building two copies of the line only to compare them was two allocations per row per
// frame, which on a full viewport is most of what a plain render allocates.
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
