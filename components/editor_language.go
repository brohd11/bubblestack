package components

import "time"

// EditorLanguageResolver is the host-owned seam between a path and the editing
// behavior that path should receive. The editor calls it when it is constructed and
// whenever save-as or SetPath changes its identity. A nil resolver or nil result means
// literal editing: no pairs, no structured Enter, and no profile-selected highlighting.
type EditorLanguageResolver func(path string) *EditorLanguageConfig

// EditorLanguageConfig describes language-aware behavior without teaching the editor
// about any particular language or filename. Configs may be shared: the editor copies
// their pair tables. NewHighlighter must return an independent instance; the editor
// calls it for bounded viewport previews and background exact snapshots, and a preview
// may be created while another instance is parsing off the UI goroutine.
//
// IndentSpaces is the profile's automatic block-indent unit. A positive value means
// that many spaces; zero means one literal tab. EditorOpts.Indent and IndentWidth remain
// explicit user overrides of this default.
type EditorLanguageConfig struct {
	NewHighlighter   func() Highlighter
	AutoClosingPairs []EditorPair
	SurroundingPairs []EditorPair
	IndentSpaces     int
	OnEnter          EditorEnterHandler
}

// EditorPair is an opening and closing delimiter. AutoClosingPairs are inserted at an
// unselected caret; SurroundingPairs wrap an active selection. A language may put the same
// pair in both lists or make it surrounding-only.
type EditorPair struct {
	Open  rune
	Close rune
}

// EditorEnterContext is the immutable buffer view a language's Enter handler receives.
// Before and After are the current line split at the caret. LeadingIndent is the whole
// line's raw space/tab prefix, and IndentUnit reflects the editor's live auto/tab/spaces
// choice, including an alt+i override.
type EditorEnterContext struct {
	Before        string
	After         string
	LeadingIndent string
	IndentUnit    string
}

// EditorEnterAction describes the edit Enter performs. Prefix alone is the ordinary
// structured newline: the editor splits at the caret and heads the new line with it. The
// two flags cover the shapes a prefix cannot reach. Whichever shape is used, the whole
// gesture stays one undo step. A handler that returns false leaves Enter a plain split.
type EditorEnterAction struct {
	// Prefix heads the new line, ahead of the text the split carried past the caret.
	Prefix string

	// Block moves that carried text down one line further and leaves the caret on the
	// empty line between, so Enter inside a bracket pair opens a block instead of pushing
	// the closer along. Closer is that further line's own prefix — its indentation; the
	// delimiter itself is text the split already carried, not something named here. An
	// empty Closer is meaningful, which is why this takes a flag and not a non-empty test.
	Block  bool
	Closer string

	// Rewrite replaces the caret's whole line with Line and inserts no newline at all,
	// the caret landing at Line's end. It is how a list handler ends a list on an empty
	// item: leaving one is a deletion, not a split.
	Rewrite bool
	Line    string
}

// EditorEnterHandler returns the structured newline to apply and whether it claims the
// Enter gesture. Returning false delegates to the editor's ordinary line split.
type EditorEnterHandler func(EditorEnterContext) (EditorEnterAction, bool)

// applyLanguage replaces every path-derived behavior at once. Explicit highlighters
// and indent overrides came directly from EditorOpts and therefore survive a rename.
func (s *EditorScreen) applyLanguage(path string) {
	s.cancelCompletionSession()
	s.autoPairs = nil
	s.surroundPairs = nil
	s.onEnter = nil
	s.resolveIndent(0)

	var cfg *EditorLanguageConfig
	if s.resolveLanguage != nil {
		cfg = s.resolveLanguage(path)
	}
	if cfg != nil {
		s.autoPairs = pairMap(cfg.AutoClosingPairs)
		s.surroundPairs = pairMap(cfg.SurroundingPairs)
		s.onEnter = cfg.OnEnter
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
}

func pairMap(pairs []EditorPair) map[rune]rune {
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

// languageEnter asks the active profile what Enter should do here and applies it through
// the editor's own mutation helpers. It is called inside the key event's existing history
// boundary, so however many splices an action costs, they remain one undo operation — each
// one appends to the same activeEdit. The mutations are ordered so that every later splice
// sits past the last, which is what makes undo's reverse replay invert them correctly.
func (s *EditorScreen) languageEnter() bool {
	if s.onEnter == nil {
		return false
	}
	line := s.lines[s.curY]
	indent := leadingWhitespace(line)
	action, ok := s.onEnter(EditorEnterContext{
		Before:        string(line[:s.curX]),
		After:         string(line[s.curX:]),
		LeadingIndent: string(indent),
		IndentUnit:    string(s.indentUnit()),
	})
	if !ok {
		return false
	}
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
