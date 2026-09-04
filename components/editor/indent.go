package editor

import "fmt"

// Block indentation for Screen: tab over a multi-line selection indents every line
// it touches, alt+, / alt+. dedent and indent the same span, and alt+i cycles the unit
// those gestures use.
//
// The chords are alt+, and alt+. because the bracket pair every other editor uses is
// unreachable here, in two different ways: alt+[ arrives intact but bubblestack.Run's
// mouseSGRFragmentFilter eats every one of them (it is the lead of a split SGR mouse
// report, and the filter cannot know at that moment that no mouse tail follows), and
// ctrl+[ is the byte 0x1b — indistinguishable from esc at the wire, in any framework.
// Shifted, the two that were left read as < and >.
//
// The unit governs ONLY these gestures. A plain tab keypress still inserts a literal
// '\t' (editor.go's "tab" case), because a soft-tab profile is exactly where a literal
// tab is hardest to type any other way, and a key that silently
// refused to produce one would be the worse trade.

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

// resolveIndent applies the active language config's unit. Called at construction and
// again when a save-as or SetPath resolves a new config.
//
// An explicit Opts.IndentWidth is a deliberate override and survives the rename,
// exactly as an explicit Highlighter does.
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

// indentLabel describes the live unit for the status line. Auto names what it resolved
// to as well as itself: "auto" alone would leave the one thing the reader pressed the
// key to find out unsaid.
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

// indentSpan is the inclusive line range one gesture covers, and the multi-line test:
// callers read last > first to decide whether tab indents a block or types a tab.
//
// A selection ending at column 0 of the line AFTER the last selected one took the
// newline, not the line — that is the form a triple click and shift+down both leave —
// so pull it back, the same way surroundSelection does. With no selection at all the
// span is the caret's own line, which is what makes alt+, useful without one.
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

// shiftLineIndent adds (dir > 0) or removes one indent level at the head of line y,
// returning the change in the line's rune length so the caller can carry columns along.
//
// A dedent takes whatever is actually there rather than what the mode would have put
// there: one leading tab, else up to a unit's worth of leading spaces. A file with mixed
// indentation then still dedents a line at a time instead of refusing half of them.
//
// An empty line is left alone in both directions: indenting it would leave a line of
// nothing but trailing whitespace, and a dedent has nothing to take, so the pair round
// trips.
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

// shiftSelectionIndent indents or dedents every line the span covers and keeps the
// selection over the same text, so the key repeats to nest — the same contract
// surroundSelection keeps, and the reason columns are carried by their own line's delta
// rather than snapped to whole lines: a partial selection survives the gesture.
//
// An endpoint on a line the loop never touched (the {y+1, 0} full-line form) keeps a
// zero delta and stays put, which is what leaves a triple-clicked selection intact.
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
		// A one-line selection whose ends both clamped onto column 0 selects nothing;
		// dropping it is what selectFrom does at the same point rather than leaving an
		// empty highlight behind.
		s.clearSelection()
		s.curX = shiftIndentCol(s.curX, dCaret)
	case caretAtEnd:
		s.curY, s.curX = s.selEnd.y, s.selEnd.x
	default:
		s.curY, s.curX = s.selStart.y, s.selStart.x
	}
	s.wantX = s.curX
}

// shiftIndentCol carries a column across an indent of delta runes at the head of its
// line, so the same characters stay selected. Column 0 is pinned: it is the line's edge
// rather than an offset into its text, and a whole-line selection that slid off the tab
// it just added would leave the indentation outside the highlight — and outside the next
// copy — which is not what selecting the line meant.
func shiftIndentCol(x, delta int) int {
	if x == 0 {
		return 0
	}
	return max(x+delta, 0)
}
