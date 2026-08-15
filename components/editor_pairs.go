package components

// Delimiter-pair handling for EditorScreen: typing one of editorPairs over a selection
// wraps the selection rather than replacing it, and keeps it selected so the key repeats
// to nest.

// ---------- delimiter pairs ----------

// editorPairs maps an opening delimiter to the one that closes it. Typing a key in this
// table over a selection wraps the selection in the pair; typing it with no selection
// inserts both and leaves the caret between them. Nothing else in the pair story is
// clever: backspace does not swallow the closer, and typing the closing delimiter over
// an auto-inserted one inserts a second.
var editorPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '*': '*', '_': '_'}

// emphasisPairRune marks the pairs that only auto-close in prose. In code '*' is
// multiplication or a pointer and '_' is an identifier character, so closing them on
// every keystroke would fight ordinary typing; wrapping a SELECTION in them stays
// available everywhere because that gesture is always deliberate.
func emphasisPairRune(r rune) bool { return r == '*' || r == '_' }

// emphasisPairExt reports the file types where emphasisPairRune delimiters auto-close.
func emphasisPairExt(ext string) bool {
	switch ext {
	case ".md", ".markdown":
		return true
	}
	return false
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
	s.lines[end.y] = spliceRune(s.lines[end.y], end.x, close)
	s.lines[start.y] = spliceRune(s.lines[start.y], start.x, open)
	s.selStart = textPos{start.y, start.x + 1}
	if start.y == end.y {
		s.selEnd = textPos{end.y, end.x + 1} // the opener pushed the closer along too
	} else {
		s.selEnd = textPos{end.y, end.x} // a different line: only the closer moved it
	}
	s.curY, s.curX, s.wantX = s.selEnd.y, s.selEnd.x, s.selEnd.x
	s.dirty = true
	s.editSeq++
}

// spliceRune inserts r at column x, returning a line that shares no backing array with
// the original. The editing helpers splice in place around the cursor; this one is for
// the two edits surroundSelection makes away from it.
func spliceRune(line []rune, x int, r rune) []rune {
	out := make([]rune, 0, len(line)+1)
	out = append(out, line[:x]...)
	out = append(out, r)
	return append(out, line[x:]...)
}
