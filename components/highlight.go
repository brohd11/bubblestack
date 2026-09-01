package components

import (
	"strings"
	"sync"

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
// adapter the editor's syntax coloring hangs on: implementations register
// themselves by file extension (RegisterHighlighter) and EditorOpts.Highlighter
// (or the registry, keyed off the buffer's path) picks one per screen.
//
// Parse is called lazily, once per buffer edit (not per frame, not per row),
// so implementations may reparse the whole document each time.
type Highlighter interface {
	// Parse ingests the full document text (lines joined with '\n').
	Parse(text string)
	// HighlightLine returns styled spans for the 0-based row; nil means the
	// line renders unstyled. The concatenated span texts must equal the row's
	// text (see Span).
	HighlightLine(row int) []Span
}

// highlighters is the extension registry: lowercase extension WITH the dot
// (".md") → factory. A factory returns a fresh Highlighter per editor screen,
// so screens never share parser state.
var (
	hlMu         sync.RWMutex
	highlighters = map[string]func() Highlighter{}
)

// RegisterHighlighter makes ext (lowercase, with the dot — ".md") highlightable:
// an editor whose path carries it gets a fresh Highlighter from factory unless
// EditorOpts.Highlighter overrides. Implementations register from init(), so
// linking the package is the whole setup.
func RegisterHighlighter(ext string, factory func() Highlighter) {
	hlMu.Lock()
	defer hlMu.Unlock()
	highlighters[strings.ToLower(ext)] = factory
}

// lookupHighlighter returns a fresh Highlighter for ext (lowercase, with the
// dot), or nil when none is registered — the plain-render case.
func lookupHighlighter(ext string) Highlighter {
	hlMu.RLock()
	defer hlMu.RUnlock()
	if factory, ok := highlighters[ext]; ok {
		return factory()
	}
	return nil
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
