package components

import (
	"strings"
	"unicode"
)

// Word and line motion for EditorScreen, mirroring the bubbles/textinput KeyMap so the
// alt+arrow / ctrl+w chords behave the way they do in a plain input.

// ---------- word/line operations (the bubbles/textinput KeyMap mirror) ----------

// isWordSpace delimits words: whitespace only, the same notion textinput uses (no
// alnum/punct classes — keep it stupidly simple).
func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

// isBackwardDeleteSymbol marks punctuation that forms its own token for
// alt+backspace/ctrl+w. Word movement and forward deletion keep their existing
// whitespace-only behavior; this is intentionally the narrower editing gesture the
// user invokes when peeling a path or expression apart from right to left.
func isBackwardDeleteSymbol(r rune) bool {
	return strings.ContainsRune("()[]{}.,|/", r)
}

// editorWordClass groups runes for double-click selection. The whitespace-only split
// word movement uses is too coarse here — it would take all of "foo.bar(baz)" as one
// word — so punctuation forms its own runs and a double-click on the '.' in "foo.bar"
// takes just the dot.
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

// wordBackPos is the position WordBackward would move to from the cursor: at column 0
// the previous line's end (the caller treats that one as a plain join/move), else
// past any spaces then the word before them.
func (s *EditorScreen) wordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	for x > 0 && !isWordSpace(line[x-1]) {
		x--
	}
	return y, x
}

// wordForwardPos is the position WordForward would move to: at end of line the next
// line's start, else past the rest of the current word then any spaces after it.
func (s *EditorScreen) wordForwardPos() (int, int) {
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

// deleteWordBackPos is wordBackPos with punctuation-aware token boundaries. It
// preserves the existing whitespace rule (trailing spaces are removed together with
// the token before them), then removes either one run of configured symbols or one
// run of ordinary word characters. Thus repeated alt+backspace on "file.md" removes
// "md", then ".", then "file".
func (s *EditorScreen) deleteWordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	if x == 0 {
		return y, x
	}
	if isBackwardDeleteSymbol(line[x-1]) {
		for x > 0 && isBackwardDeleteSymbol(line[x-1]) {
			x--
		}
		return y, x
	}
	for x > 0 && !isWordSpace(line[x-1]) && !isBackwardDeleteSymbol(line[x-1]) {
		x--
	}
	return y, x
}

func (s *EditorScreen) moveWordBack() {
	y, x := s.wordBackPos()
	s.curY, s.curX, s.wantX = y, x, x
}

func (s *EditorScreen) moveWordForward() {
	y, x := s.wordForwardPos()
	s.curY, s.curX, s.wantX = y, x, x
}

// deleteRange removes the text from (y1, x1) to (y2, x2), merging the two line ends
// into y1 and dropping the lines between. An empty range is a no-op (and stays
// clean); the caller owns the cursor afterwards.
func (s *EditorScreen) deleteRange(y1, x1, y2, x2 int) {
	s.replaceText(textPos{y1, x1}, textPos{y2, x2}, "")
}

// deleteWordBack is DeleteWordBackward: deletes from deleteWordBackPos to the
// cursor. At column 0 it is a plain line join, exactly like backspace.
func (s *EditorScreen) deleteWordBack() {
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
func (s *EditorScreen) deleteWordForward() {
	y, x := s.wordForwardPos()
	s.deleteRange(s.curY, s.curX, y, x)
}
