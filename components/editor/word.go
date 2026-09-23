package editor

import "unicode"

// Word and line motion for Screen, mirroring the bubbles/textinput KeyMap so the
// alt+arrow / ctrl+w chords behave the way they do in a plain input.

// ---------- word/line operations (the bubbles/textinput KeyMap mirror) ----------

// isWordSpace delimits words: whitespace only, the same notion textinput uses (no
// alnum/punct classes — keep it stupidly simple).
func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

// editorWordClass groups runes into whitespace, word (letters, digits, '_') and
// punctuation, for double-click selection and word deletion (finer than word movement,
// so "foo.bar" selects "foo", ".", "bar" separately).
func editorWordClass(r rune) int {
	switch {
	case isWordSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	default:
		return 2
	}
}

// wordBoundsAt returns the [start, end) run of same-class runes around col, without the
// adjacent whitespace. Past the end of the line it takes the last run.
func wordBoundsAt(line []rune, col int) (int, int) {
	if len(line) == 0 {
		return 0, 0
	}
	if col >= len(line) {
		col = len(line) - 1
	}
	class := editorWordClass(line[col])
	from, to := col, col+1
	for from > 0 && editorWordClass(line[from-1]) == class {
		from--
	}
	for to < len(line) && editorWordClass(line[to]) == class {
		to++
	}
	return from, to
}

// wordBackPos is where WordBackward moves: at column 0 the previous line's end, else back
// over word runes then the spaces before them. It lands behind the previous word, the
// mirror of wordForwardPos.
func (s *Screen) wordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	for x > 0 && !isWordSpace(line[x-1]) {
		x--
	}
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	return y, x
}

// wordForwardPos is the position WordForward would move to: at end of line the next
// line's start, else past the rest of the current word then any spaces after it.
func (s *Screen) wordForwardPos() (int, int) {
	y, x := s.curY, s.curX
	line := s.lines[y]
	if x >= len(line) {
		if y >= len(s.lines)-1 {
			return y, len(line)
		}
		return y + 1, 0
	}
	for x < len(line) && !isWordSpace(line[x]) {
		x++
	}
	for x < len(line) && isWordSpace(line[x]) {
		x++
	}
	return y, x
}

// deleteWordBackPos deletes one editorWordClass run, plus one ordinary space before the
// caret; longer whitespace is deleted on its own. "src/foo.md" peels as md . foo / src.
func (s *Screen) deleteWordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	end := x
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	if x == 0 || (x < end && (end-x != 1 || line[x] != ' ')) {
		return y, x
	}
	class := editorWordClass(line[x-1])
	for x > 0 && editorWordClass(line[x-1]) == class {
		x--
	}
	return y, x
}

func (s *Screen) moveWordBack() {
	y, x := s.wordBackPos()
	s.curY, s.curX, s.wantX = y, x, x
}

func (s *Screen) moveWordForward() {
	y, x := s.wordForwardPos()
	s.curY, s.curX, s.wantX = y, x, x
}

// deleteRange removes (y1, x1)..(y2, x2), joining the ends. The caller owns the caret.
func (s *Screen) deleteRange(y1, x1, y2, x2 int) {
	s.replaceText(textPos{y1, x1}, textPos{y2, x2}, "")
}

// deleteWordBack is DeleteWordBackward: deletes from deleteWordBackPos to the
// cursor. At column 0 it is a plain line join, exactly like backspace.
func (s *Screen) deleteWordBack() {
	y, x := s.deleteWordBackPos()
	if y == s.curY && x == s.curX {
		return // start of buffer
	}
	if x == len(s.lines[y]) && y == s.curY-1 {
		s.backspace() // column 0: join, not a word delete
		return
	}
	s.deleteRange(y, x, s.curY, s.curX)
	s.curY, s.curX, s.wantX = y, x, x
}

// deleteWordForward is DeleteWordForward: deletes from the cursor to wordForwardPos,
// which pulls the next line up when the cursor sits at end of line.
func (s *Screen) deleteWordForward() {
	y, x := s.wordForwardPos()
	s.deleteRange(s.curY, s.curX, y, x)
}
