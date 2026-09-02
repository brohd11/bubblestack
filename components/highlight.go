package components

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Span is a run of text sharing one style — the unit a Highlighter answers with.
// The spans of a line, concatenated, must equal the line's text EXACTLY: the
// editor validates that before styling and falls back to the plain render on a
// mismatch, so a buggy highlighter can lose its colors but never corrupt the
// frame. A zero Style renders the run unstyled.
type Span struct {
	Text  string
	Style lipgloss.Style
}

// Highlighter parses a full document and answers per-line highlighting, keeping
// any multi-line state (fenced code blocks, block comments) internally — the
// editor asks by row and never sees the state. It is the language-agnostic
// adapter the editor's syntax coloring hangs on. A host supplies one explicitly through
// EditorOpts.Highlighter or from an EditorLanguageConfig factory.
//
// A direct EditorOpts.Highlighter is parsed lazily on first render and after editing
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

// spansText is the concatenated text of spans — the editor's validation of the
// Span contract against the buffer line.
func spansText(spans []Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}
