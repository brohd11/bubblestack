package editor

// Language-configured delimiter handling for Screen. The active profile supplies
// separate auto-closing and surrounding sets; this file owns their generic mutations.

// ---------- delimiter pairs ----------

// deleteEmptyAutoPair removes an auto-closing pair around the caret (typed or not: no
// provenance is kept), inside the key's undo step.
func (s *Screen) deleteEmptyAutoPair() bool {
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

// surroundSelection wraps the selection in open/close and keeps the text selected, so the
// key nests (word → *word* → **word**). The closer goes in first so the selection's end
// column stays valid.
func (s *Screen) surroundSelection(open, close rune) {
	start, end := s.selStart, s.selEnd
	if end.x == 0 && end.y > start.y {
		// A selection ending at column 0 took the newline; wrap the text of the last selected
		// line instead.
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
