package editor

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// LanguageResolver maps a path to its editing behavior. The editor calls it on
// construction and whenever the path changes; nil means literal editing.
type LanguageResolver func(path string) *LanguageConfig

// LanguageConfig describes language-aware behavior without the editor knowing any
// language. Configs may be shared (pair tables are copied). NewHighlighter must return an
// independent instance: previews and background parses may run concurrently.
// IndentSpaces is the automatic block-indent unit (0 means a tab); Opts overrides it.
type LanguageConfig struct {
	NewHighlighter   func() Highlighter
	AutoClosingPairs []Pair
	SurroundingPairs []Pair
	IndentSpaces     int
	OnEnter          EnterHandler

	// LineComment ("//", "#", "--") drives the comment toggle and Enter's comment
	// continuation. BlockComment is the toggle's fallback when there is no line form (CSS);
	// Enter never continues it. Types with neither have no comment gesture.
	LineComment  string
	BlockComment [2]string
}

// Pair is an opening and closing delimiter. AutoClosingPairs are inserted at a caret;
// SurroundingPairs wrap a selection.
type Pair struct {
	Open  rune
	Close rune
}

// EnterContext is what a language's Enter handler sees: Before and After split the line
// at the caret, LeadingIndent is its raw indent, and IndentUnit is the live indent choice.
type EnterContext struct {
	Before        string
	After         string
	LeadingIndent string
	IndentUnit    string
}

// EnterAction describes what Enter does, as one undo step. Prefix alone heads the new
// line after a split. A handler returning false leaves Enter a plain split.
type EnterAction struct {
	// Prefix heads the new line, ahead of the text the split carried past the caret.
	Prefix string

	// Block moves the carried text down one more line and leaves the caret on the empty line
	// between (Enter inside a bracket pair), with Closer as that line's indent. An empty
	// Closer is valid, hence the flag.
	Block  bool
	Closer string

	// Rewrite replaces the caret's line with Line and inserts no newline (ending a list on an
	// empty item).
	Rewrite bool
	Line    string
}

// EnterHandler returns the structured newline to apply and whether it claims the
// Enter gesture. Returning false delegates to the editor's ordinary line split.
type EnterHandler func(EnterContext) (EnterAction, bool)

// applyLanguage replaces every path-derived behavior at once. Explicit highlighters
// and indent overrides came directly from Opts and therefore survive a rename.
func (s *Screen) applyLanguage(path string) {
	s.cancelCompletionSession()
	s.autoPairs = nil
	s.surroundPairs = nil
	s.onEnter = nil
	s.lineComment = ""
	s.blockComment = [2]string{}
	s.resolveIndent(0)

	var cfg *LanguageConfig
	if s.resolveLanguage != nil {
		cfg = s.resolveLanguage(path)
	}
	if cfg != nil {
		s.autoPairs = pairMap(cfg.AutoClosingPairs)
		s.surroundPairs = pairMap(cfg.SurroundingPairs)
		s.onEnter = cfg.OnEnter
		s.lineComment = cfg.LineComment
		s.blockComment = cfg.BlockComment
		s.resolveIndent(cfg.IndentSpaces)
	}

	if s.hlExplicit {
		return
	}
	s.hl = nil
	s.hlFactory = nil
	if cfg != nil && cfg.NewHighlighter != nil {
		s.hlFactory = cfg.NewHighlighter
		s.hl = s.hlFactory()
	}
	s.hlEpoch++
	s.hlSeq = -1
	s.hlChanged = time.Time{}
	s.resetHighlightRows()
	s.ClearHighlightOverlay()
}

// RefreshHighlight schedules a fresh parse without clearing the current highlighting, for
// highlighters that depend on external state. Keeping the old snapshot until the new one
// lands avoids a full-viewport flash. The epoch bump discards any parse in flight.
func (s *Screen) RefreshHighlight() tea.Cmd {
	if s.hlExplicit || s.hlFactory == nil {
		return nil
	}
	s.hlEpoch++
	s.hlSeq = -1
	s.hlChanged = time.Time{}
	return s.startHighlightParse()
}

func pairMap(pairs []Pair) map[rune]rune {
	if len(pairs) == 0 {
		return nil
	}
	out := make(map[rune]rune, len(pairs))
	for _, pair := range pairs {
		out[pair.Open] = pair.Close
	}
	return out
}

// leadingWhitespace returns the raw space/tab prefix. Tabs stay tabs in the buffer;
// rendering remains responsible for expanding them to cells.
func leadingWhitespace(line []rune) []rune {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}

// languageEnter applies the profile's Enter action through the editor's mutation helpers
// inside the key's history step, ordered so undo replays them correctly.
func (s *Screen) languageEnter() bool {
	// A line comment is enough on its own: a profile may declare one and no Enter handler
	// (TOML, vimscript), and continuing its comments is still right.
	if s.onEnter == nil && s.lineComment == "" {
		return false
	}
	line := s.lines[s.curY]
	indent := leadingWhitespace(line)
	ctx := EnterContext{
		Before:        string(line[:s.curX]),
		After:         string(line[s.curX:]),
		LeadingIndent: string(indent),
		IndentUnit:    string(s.indentUnit()),
	}
	if action, ok := s.commentEnter(ctx, s.curY); ok {
		return s.applyEnterAction(action, line)
	}
	if s.onEnter == nil {
		return false
	}
	action, ok := s.onEnter(ctx)
	if !ok {
		return false
	}
	return s.applyEnterAction(action, line)
}

// commentEnter continues a line-comment run, ahead of the language handler (`// case 1:`
// is prose). An empty comment ends the run by clearing itself.
func (s *Screen) commentEnter(ctx EnterContext, row int) (EnterAction, bool) {
	if s.lineComment == "" || !strings.HasPrefix(ctx.Before, ctx.LeadingIndent) {
		return EnterAction{}, false
	}
	rest := strings.TrimPrefix(ctx.Before, ctx.LeadingIndent)
	if !strings.HasPrefix(rest, s.lineComment) {
		return EnterAction{}, false
	}
	body := rest[len(s.lineComment):]
	// A shebang is a file header, not the first line of a comment run: continuing it would
	// put "#" on line two of every script.
	if row == 0 && strings.HasPrefix(body, "!") {
		return EnterAction{}, false
	}
	gap := body[:len(body)-len(strings.TrimLeft(body, " \t"))]
	if strings.TrimSpace(body) == "" && strings.TrimSpace(ctx.After) == "" {
		return EnterAction{Rewrite: true, Line: ctx.LeadingIndent}, true
	}
	return EnterAction{Prefix: ctx.LeadingIndent + s.lineComment + gap}, true
}

func (s *Screen) applyEnterAction(action EnterAction, line []rune) bool {
	if action.Rewrite {
		// No split at all: the line the caret is on becomes Line, and the caret lands at
		// its end. deleteLine empties a lone line the same way.
		end := s.replaceText(textPos{s.curY, 0}, textPos{s.curY, len(line)}, action.Line)
		s.curY, s.curX, s.wantX = end.y, end.x, end.x
		return true
	}
	s.newline()
	if action.Prefix != "" {
		s.insertText(action.Prefix)
	}
	if action.Block {
		// Open the closer's own line from the caret, then walk the caret back onto the
		// line it just left — insertText would leave it past the closer instead.
		at := textPos{s.curY, s.curX}
		s.replaceText(at, at, "\n"+action.Closer)
		s.curY, s.curX, s.wantX = at.y, at.x, at.x
	}
	return true
}
