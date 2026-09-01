package components

// EditorLanguageResolver is the host-owned seam between a path and the editing
// behavior that path should receive. The editor calls it when it is constructed and
// whenever save-as or SetPath changes its identity. A nil resolver or nil result means
// literal editing: no pairs, no structured Enter, and no profile-selected highlighting.
type EditorLanguageResolver func(path string) *EditorLanguageConfig

// EditorLanguageConfig describes language-aware behavior without teaching the editor
// about any particular language or filename. Configs may be shared: the editor copies
// their pair tables and creates a fresh highlighter whenever it applies one.
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

// EditorEnterAction describes a structured newline. The editor performs the split and
// inserts Prefix at the head of the new line, keeping the entire gesture in one undo
// step. A handler that returns false leaves Enter as a plain line split.
type EditorEnterAction struct {
	Prefix string
}

// EditorEnterHandler returns the structured newline to apply and whether it claims the
// Enter gesture. Returning false delegates to the editor's ordinary line split.
type EditorEnterHandler func(EditorEnterContext) (EditorEnterAction, bool)

// applyLanguage replaces every path-derived behavior at once. Explicit highlighters
// and indent overrides came directly from EditorOpts and therefore survive a rename.
func (s *EditorScreen) applyLanguage(path string) {
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
	if cfg != nil && cfg.NewHighlighter != nil {
		s.hl = cfg.NewHighlighter()
	}
	s.hlSeq = -1
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

// languageEnter asks the active profile for a newline prefix and applies it through the
// editor's own mutation helpers. It is called inside the key event's existing history
// boundary, so newline plus prefix remains one undo operation.
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
	s.newline()
	if action.Prefix != "" {
		s.insertText(action.Prefix)
	}
	return true
}
