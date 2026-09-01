package components

import tea "charm.land/bubbletea/v2"

func init() {
	registerEditorKeyHandler(yamlEditorKey, ".yaml", ".yml")
}

// yamlEditorKey carries the current line's indentation onto the next line without
// attempting to infer nesting from YAML syntax.
func yamlEditorKey(s *EditorScreen, msg tea.KeyPressMsg) bool {
	if msg.String() != "enter" {
		return false
	}
	indent := leadingWhitespace(s.lines[s.curY])
	if len(indent) == 0 || s.curX < len(indent) {
		return false
	}
	prefix := append([]rune{}, indent...)
	s.newline()
	s.insertRunes(prefix...)
	return true
}
