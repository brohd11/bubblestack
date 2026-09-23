package editor

import "fmt"

// Block indentation for Screen: tab over a multi-line selection indents it, alt+, and
// alt+. dedent and indent the span, and alt+i cycles the unit. The usual alt+[ / alt+]
// are unavailable: alt+[ is eaten by bubblestack.Run's mouse-fragment filter and ctrl+[ is
// esc on the wire. The unit applies only to these gestures; a plain tab always types '\t'.

// ---------- the indent unit ----------

// IndentMode names how a block gesture picks its unit.
type IndentMode int

const (
	// IndentAuto reads the unit from the host's resolved language config. It is the zero
	// value; without a config the generic fallback is a literal tab.
	IndentAuto IndentMode = iota
	IndentTab
	IndentSpaces
)

// resolveIndent applies the language config's unit, at construction and after a rename.
// An explicit Opts.IndentWidth survives.
func (s *Screen) resolveIndent(spaces int) {
	s.autoIndentSpaces = max(spaces, 0)
	if s.indentWidthExplicit {
		return
	}
	if s.indentWidth = s.autoIndentSpaces; s.indentWidth <= 0 {
		s.indentWidth = editorTabWidth
	}
}

// indentUnit is the runes one indent level adds: a single tab, or the mode's spaces.
func (s *Screen) indentUnit() []rune {
	n := s.autoIndentSpaces
	switch s.indentMode {
	case IndentTab:
		n = 0
	case IndentSpaces:
		n = s.indentWidth
	}
	if n <= 0 {
		return []rune{'\t'}
	}
	unit := make([]rune, n)
	for i := range unit {
		unit[i] = ' '
	}
	return unit
}

// indentLabel describes the live unit for the status line, including what "auto" resolved
// to.
func (s *Screen) indentLabel() string {
	what := "tab"
	if unit := s.indentUnit(); unit[0] == ' ' {
		what = fmt.Sprintf("%d spaces", len(unit))
	}
	if s.indentMode == IndentAuto {
		return "auto (" + what + ")"
	}
	return what
}

// cycleIndentMode advances auto → tab → spaces → auto and returns the status text.
// It touches no text, so it takes no undo step and leaves the buffer clean.
func (s *Screen) cycleIndentMode() string {
	switch s.indentMode {
	case IndentAuto:
		s.indentMode = IndentTab
	case IndentTab:
		s.indentMode = IndentSpaces
	default:
		s.indentMode = IndentAuto
	}
	return "indent: " + s.indentLabel()
}

// ---------- the block gestures ----------

// indentSpan is the inclusive line range a gesture covers (last > first means a block).
// A selection ending at column 0 of the next line took only the newline, so that line is
// excluded. With no selection it is the caret's line.
func (s *Screen) indentSpan() (int, int) {
	if !s.selectionActive() {
		return s.curY, s.curY
	}
	first, last := s.selStart.y, s.selEnd.y
	if s.selEnd.x == 0 && last > first {
		last--
	}
	return first, last
}

// shiftLineIndent adds (dir > 0) or removes one level at the head of line y, returning the
// change in rune length. A dedent removes what is there (one tab, else up to a unit of
// spaces), so mixed indentation still dedents. Empty lines are left alone.
func (s *Screen) shiftLineIndent(y int, unit []rune, dir int) int {
	line := s.lines[y]
	if len(line) == 0 {
		return 0
	}
	if dir > 0 {
		s.replaceText(textPos{y, 0}, textPos{y, 0}, string(unit))
		return len(unit)
	}
	if line[0] == '\t' {
		s.replaceText(textPos{y, 0}, textPos{y, 1}, "")
		return -1
	}
	width := len(unit)
	if unit[0] == '\t' {
		width = editorTabWidth
	}
	n := 0
	for n < len(line) && n < width && line[n] == ' ' {
		n++
	}
	if n == 0 {
		return 0
	}
	s.replaceText(textPos{y, 0}, textPos{y, n}, "")
	return -n
}

// shiftSelectionIndent indents or dedents every line in the span, keeping the selection
// on the same text (columns move by their own line's delta) so the key repeats.
func (s *Screen) shiftSelectionIndent(dir int) {
	first, last := s.indentSpan()
	unit := s.indentUnit()
	selected := s.selectionActive()
	caretAtEnd := selected && s.curY == s.selEnd.y && s.curX == s.selEnd.x

	moved := false
	var dStart, dEnd, dCaret int
	for y := first; y <= last; y++ {
		d := s.shiftLineIndent(y, unit, dir)
		if d == 0 {
			continue
		}
		moved = true
		if y == s.selStart.y {
			dStart = d
		}
		if y == s.selEnd.y {
			dEnd = d
		}
		if y == s.curY {
			dCaret = d
		}
	}
	if !moved {
		return
	}
	if !selected {
		s.curX = shiftIndentCol(s.curX, dCaret)
		s.wantX = s.curX
		return
	}
	s.selStart.x = shiftIndentCol(s.selStart.x, dStart)
	s.selEnd.x = shiftIndentCol(s.selEnd.x, dEnd)
	switch {
	case !posLess(s.selStart, s.selEnd):
		// Both ends of a one-line selection clamped to column 0: it selects nothing, so drop it.
		s.clearSelection()
		s.curX = shiftIndentCol(s.curX, dCaret)
	case caretAtEnd:
		s.curY, s.curX = s.selEnd.y, s.selEnd.x
	default:
		s.curY, s.curX = s.selStart.y, s.selStart.x
	}
	s.wantX = s.curX
}

// shiftIndentCol moves a column by its line's indent delta. Column 0 stays pinned, so a
// whole-line selection includes the indent just added.
func shiftIndentCol(x, delta int) int {
	if x == 0 {
		return 0
	}
	return max(x+delta, 0)
}
