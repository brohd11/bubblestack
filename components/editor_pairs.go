package components

// Language-configured delimiter handling for EditorScreen. The active profile supplies
// separate auto-closing and surrounding sets; this file owns their generic mutations.

// ---------- delimiter pairs ----------

// deleteEmptyAutoPair removes the delimiters immediately around the caret when they
// form one of the active profile's auto-closing pairs. Adjacency is deliberate: pair
// provenance is not stored, so a manually formed empty pair behaves like an inserted
// one. The caller's existing key transaction keeps both removals in one undo step.
func (s *EditorScreen) deleteEmptyAutoPair() bool {
	if s.curX == 0 {
		return false
	}
	line := s.lines[s.curY]
	if s.curX >= len(line) {
		return false
	}
	open, close := line[s.curX-1], line[s.curX]
	if configured, ok := s.autoPairs[open]; !ok || configured != close {
		return false
	}
	s.replaceText(textPos{s.curY, s.curX - 1}, textPos{s.curY, s.curX + 1}, "")
	s.curX--
	s.wantX = s.curX
	return true
}

// surroundSelection wraps the selection in open/close and keeps the original text
// selected, so repeating the key nests another pair: word → *word* → **word**. The
// closer goes in first — inserting the opener first would shift a single-line
// selection's end column out from under the second splice.
func (s *EditorScreen) surroundSelection(open, close rune) {
	start, end := s.selStart, s.selEnd
	if end.x == 0 && end.y > start.y {
		// A triple-clicked line takes the newline ending it, and a closer at column 0 of
		// the next line reads as wrapping the break rather than the text. Pull it back to
		// the end of the last selected line; the selection narrows to the text it wrapped.
		end = textPos{end.y - 1, len(s.lines[end.y-1])}
	}
	s.replaceText(end, end, string(close))
	s.replaceText(start, start, string(open))
	s.selStart = textPos{start.y, start.x + 1}
	if start.y == end.y {
		s.selEnd = textPos{end.y, end.x + 1} // the opener pushed the closer along too
	} else {
		s.selEnd = textPos{end.y, end.x} // a different line: only the closer moved it
	}
	s.curY, s.curX, s.wantX = s.selEnd.y, s.selEnd.x, s.selEnd.x
}
