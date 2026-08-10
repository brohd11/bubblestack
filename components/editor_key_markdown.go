package components

import tea "github.com/charmbracelet/bubbletea"

func init() {
	registerEditorKeyHandler(markdownEditorKey, ".md", ".markdown")
}

// markdownEditorKey continues dash list items, preserving their exact indentation.
// It intentionally recognizes no other marker and keeps an empty item going: list
// termination and the other Markdown list forms are outside this first behavior.
func markdownEditorKey(s *EditorScreen, msg tea.KeyMsg) bool {
	if msg.String() != "enter" {
		return false
	}
	line := s.lines[s.curY]
	indent := leadingWhitespace(line)
	markerEnd := len(indent) + 2
	if len(line) < markerEnd || s.curX < markerEnd || line[len(indent)] != '-' || line[len(indent)+1] != ' ' {
		return false
	}
	prefix := append(append([]rune{}, indent...), '-', ' ')
	s.newline()
	s.insertRunes(prefix...)
	return true
}
