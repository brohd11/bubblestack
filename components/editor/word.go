package editor

import "unicode"

// Word and line motion for Screen, mirroring the bubbles/textinput KeyMap so the
// alt+arrow / ctrl+w chords behave the way they do in a plain input.

// ---------- word/line operations (the bubbles/textinput KeyMap mirror) ----------

// isWordSpace delimits words: whitespace only, the same notion textinput uses (no
// alnum/punct classes — keep it stupidly simple).
func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

// editorWordClass groups runes into whitespace / word (letters, digits, '_') / everything
// else. It is the notion of a word that double-click selection and backward word deletion
// share; the whitespace-only split word movement uses is too coarse for both — it would
// take all of "foo.bar(baz)" as one word — so punctuation forms its own runs and a
// double-click on the '.' in "foo.bar" takes just the dot.
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

// wordBoundsAt returns the half-open column range of the run of same-class runes around
// col. Unlike wordBackPos/wordForwardPos it is a pure function of the line and does not
// swallow the whitespace next to the word. A column past the end of the line takes the
// last run, which is where a click in the empty space right of the text lands.
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

// wordBackPos is the position WordBackward would move to from the cursor: at column 0 the
// previous line's end (the caller treats that one as a plain move), else back over the word
// runes left of the caret and then over the spaces before them. It lands on the BACK of the
// previous word ("foo bar|"), mirroring wordForwardPos landing on the FRONT of the next one
// ("foo |bar") — so the two stop on opposite sides of a gap, and a left/right round trip from
// a word front toggles across it rather than returning.
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

// deleteWordBackPos is the position alt+backspace deletes back to: one run of same-class
// runes, the classes being editorWordClass's, plus any whitespace before that run on the
// same line. A press with whitespace before the caret takes only the whitespace and stops
// at the text. Word and symbol runs stay separate, so repeated presses peel "src/foo.md"
// apart as "md", ".", "foo", "/", "src".
func (s *Screen) deleteWordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	class := editorWordClass(line[x-1])
	for x > 0 && editorWordClass(line[x-1]) == class {
		x--
	}
	for x > 0 && isWordSpace(line[x-1]) {
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

// deleteRange removes the text from (y1, x1) to (y2, x2), merging the two line ends
// into y1 and dropping the lines between. An empty range is a no-op (and stays
// clean); the caller owns the cursor afterwards.
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
