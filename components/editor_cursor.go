package components

import (
	"strings"
	"time"

	"github.com/brohd11/bubblestack/core"
)

// Cursor movement, mouse gestures and selection for EditorScreen: arrow/click positioning,
// the drag and multi-click (word/line) selections, and the selection range itself.

// ---------- cursor movement ----------

func (s *EditorScreen) moveLeft() {
	if s.curX > 0 {
		s.curX--
	} else if s.curY > 0 {
		s.curY--
		s.curX = len(s.lines[s.curY])
	}
	s.wantX = s.curX
}

func (s *EditorScreen) moveRight() {
	if s.curX < len(s.lines[s.curY]) {
		s.curX++
	} else if s.curY < len(s.lines)-1 {
		s.curY++
		s.curX = 0
	}
	s.wantX = s.curX
}

// moveVertical moves the cursor delta lines, keeping the wantX target column so a
// run of up/down moves across short lines returns to the column the user started from.
//
// Off either end of the buffer the move becomes a horizontal one instead of a no-op:
// down on the last line lands at end of line, up on the first line at column zero.
// That is what makes holding an arrow reach the end of the document rather than stall
// mid-line, and both ends reset wantX — the caret really moved, so the sticky column
// follows it exactly as it does for home/end.
func (s *EditorScreen) moveVertical(delta int) {
	y := s.curY + delta
	if y < 0 {
		s.curX, s.wantX = 0, 0
		return
	}
	if y >= len(s.lines) {
		s.curX = len(s.lines[s.curY])
		s.wantX = s.curX
		return
	}
	s.curY = y
	if s.curX = s.wantX; s.curX > len(s.lines[y]) {
		s.curX = len(s.lines[y])
	}
}

// positionAt maps a mouse cell to a buffer position. Drag events may arrive beyond
// the pane because ModularScreen keeps the gesture with its originating slot; clamp
// keeps those endpoints on the nearest visible text cell without scrolling.
func (s *EditorScreen) positionAt(sh *core.Shared, x, y int, clamp bool) (textPos, bool) {
	if x -= s.insetX(); x < 0 {
		x = 0 // a press left of text reads as column zero, preserving click behavior
	}
	if s.barVisible() && x >= s.textW() {
		if !clamp {
			return textPos{}, false // a press/release on the scrollbar is not buffer text
		}
		x = s.textW() - 1
	}
	if x -= s.numGutterWidth(); x < 0 {
		x = 0 // a click on the line number reads as column 0, as one left of the body does
	}
	if x >= s.contentW() {
		x = s.contentW() - 1
	}
	rel := y - s.insetY()
	if !s.embedded {
		rel -= sh.BodyY() // absolute coordinates: the chrome rows come off too
	}
	if rel < 0 {
		if !clamp {
			return textPos{}, false
		}
		rel = 0
	}
	if rel >= s.h {
		if !clamp {
			return textPos{}, false
		}
		rel = s.h - 1
	}
	row, cell := s.scrY+rel, s.scrX+x
	if s.wrap {
		// The clicked row is a wrapped chunk: it names the line, and its start is the
		// origin the click's column counts from.
		s.rebuildWrapRows()
		r := s.wrapRows[min(row, len(s.wrapRows)-1)]
		row, cell = r.line, r.start+x
	} else if row >= len(s.lines) {
		row = len(s.lines) - 1
	}
	col := colAtCell(s.lines[row], cell)
	return textPos{row, col}, true
}

// clickAt preserves the editor's original single-click behavior. It is kept as a
// small wrapper because tests and callers inside this package use it directly.
func (s *EditorScreen) clickAt(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.resetMouseGesture()
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScroll()
}

// pressSelection handles a left selection press. Repeated presses on the same character
// inside editorMultiClickWindow promote to word and then line selection; a fourth starts
// over as a caret/drag gesture. The clock is a parameter so tests can drive click cadence.
func (s *EditorScreen) pressSelection(sh *core.Shared, x, y int, now time.Time) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		s.clickCount = 0
		s.resetMouseGesture()
		return
	}
	if s.clickCount > 0 && p == s.clickPos && now.Sub(s.clickTime) < editorMultiClickWindow {
		s.clickCount++
	} else {
		s.clickCount = 1
	}
	s.clickPos, s.clickTime = p, now
	s.resetMouseGesture()
	switch s.clickCount {
	case 2:
		s.selectWordAt(p)
	case 3:
		s.selectLineAt(p)
	default:
		s.clickCount = 1
		s.startDragAt(p)
		return
	}
	// Multi-click selection is complete on the press: no drag is left running, so motion
	// cannot extend it back onto the anchor.
	s.clampScrollBounds()
}

// pressContext is the whole right-button gesture. A press INSIDE the selection leaves the
// selection and the caret alone, so the menu acts on what is already highlighted; a press
// outside is an ordinary caret click that clears it, so a paste lands where the pointer
// did. A press that maps to no buffer position at all — the scrollbar column, the title
// bar, the search bar — opens nothing: there is no position for the menu to act on, and a
// menu raised there would silently act on wherever the caret happened to be. clickCount is
// dropped so a right press between two left clicks cannot become the middle of a
// multi-click, which is what the old per-button click bookkeeping prevented.
//
// The box clears the pressed row — one below it, flipping to one above when there is no
// room, left edge on the pressed column either way — and the selection has no say in that.
// Anchoring off the selection's far edge instead would read better on paper, but a press
// near the top of a long selection would then put the menu a whole selection's length away:
// having to chase the box down the screen is worse than having it cover text that is
// already highlighted.
func (s *EditorScreen) pressContext(sh *core.Shared, x, y int) core.Action {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return core.Action{}
	}
	if !s.positionSelected(p) {
		s.clickAt(sh, x, y)
	}
	s.resetMouseGesture()
	s.clickCount = 0
	ax, ay := s.absCell(sh, x, y)
	return core.Push(s.editMenu(sh, ax, ay))
}

// absCell converts an incoming mouse cell into the absolute terminal cells an overlay
// anchor is stated in. The discriminator is s.embedded rather than s.hasOrigin, because
// embedded is the same bit positionAt uses to decide which frame the coordinates arrived
// in: standalone the router hands over absolute cells (BodyY included, which is why
// nothing is added back), embedded ModularScreen subtracts the slot's rect before
// forwarding (updateMouseSlot), and the pane origin pushed back every View is that same
// rect. Going through paneGeometry rather than the origin fields directly matters only
// before the first frame, when no origin has arrived: its (0, BodyY) fallback at least
// gets the chrome rows right, where the bare fields would be off by the whole header.
func (s *EditorScreen) absCell(sh *core.Shared, x, y int) (int, int) {
	if !s.embedded {
		return x, y
	}
	ox, oy, _, _ := s.paneGeometry(sh)
	return ox + x, oy + y
}

// positionSelected reports whether the buffer character at p belongs to the current
// half-open selection — the context menu's inside-the-selection test. For a multiline
// range, the insertion position after a line's final rune represents its selected newline.
func (s *EditorScreen) positionSelected(p textPos) bool {
	return s.selectionActive() && !posLess(p, s.selStart) && posLess(p, s.selEnd)
}

// selectWordAt selects the run of same-class runes under p. A blank line has no run, so
// it stays an ordinary caret placement.
func (s *EditorScreen) selectWordAt(p textPos) {
	from, to := wordBoundsAt(s.lines[p.y], p.x)
	if from == to {
		s.clearSelection()
		s.curY, s.curX, s.wantX = p.y, p.x, p.x
		return
	}
	s.selStart, s.selEnd = textPos{p.y, from}, textPos{p.y, to}
	s.curY, s.curX, s.wantX = p.y, to, to
}

// selectLineAt selects p's whole line including the newline ending it, so the selection
// deletes as a line and copies as one. The last line has no newline to take.
func (s *EditorScreen) selectLineAt(p textPos) {
	end := textPos{p.y, len(s.lines[p.y])}
	if p.y < len(s.lines)-1 {
		end = textPos{p.y + 1, 0}
	}
	s.selStart, s.selEnd = textPos{p.y, 0}, end
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

func (s *EditorScreen) startDrag(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.startDragAt(p)
}

func (s *EditorScreen) startDragAt(p textPos) {
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScroll()
	s.dragAnchor = p
	s.dragAnchorEnd = s.cellEnd(p)
	s.dragging = true
}

func (s *EditorScreen) resetMouseGesture() { s.dragging = false }

func (s *EditorScreen) extendDrag(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, true)
	if !ok {
		return
	}
	if p == s.dragAnchor {
		s.clearSelection()
		s.curY, s.curX, s.wantX = p.y, p.x, p.x
		return
	}
	end := s.cellEnd(p)
	if posLess(p, s.dragAnchor) {
		s.selStart, s.selEnd = p, s.dragAnchorEnd
		s.curY, s.curX = p.y, p.x
	} else {
		s.selStart, s.selEnd = s.dragAnchor, end
		s.curY, s.curX = end.y, end.x
	}
	if !posLess(s.selStart, s.selEnd) {
		s.clearSelection()
	}
	s.wantX = s.curX
	s.clampScrollBounds()
}

func (s *EditorScreen) cellEnd(p textPos) textPos {
	if p.x < len(s.lines[p.y]) {
		return textPos{p.y, p.x + 1}
	}
	return p
}

func posLess(a, b textPos) bool { return a.y < b.y || a.y == b.y && a.x < b.x }

func (s *EditorScreen) selectionActive() bool { return posLess(s.selStart, s.selEnd) }

func (s *EditorScreen) clearSelection() { s.selStart, s.selEnd = textPos{}, textPos{} }

func (s *EditorScreen) deleteSelection() {
	if !s.selectionActive() {
		return
	}
	start := s.selStart
	s.deleteRange(start.y, start.x, s.selEnd.y, s.selEnd.x)
	s.curY, s.curX, s.wantX = start.y, start.x, start.x
	s.clearSelection()
}

func (s *EditorScreen) selectedText() string {
	if !s.selectionActive() {
		return ""
	}
	if s.selStart.y == s.selEnd.y {
		return string(s.lines[s.selStart.y][s.selStart.x:s.selEnd.x])
	}
	var b strings.Builder
	b.WriteString(string(s.lines[s.selStart.y][s.selStart.x:]))
	for y := s.selStart.y + 1; y < s.selEnd.y; y++ {
		b.WriteByte('\n')
		b.WriteString(string(s.lines[y]))
	}
	b.WriteByte('\n')
	b.WriteString(string(s.lines[s.selEnd.y][:s.selEnd.x]))
	return b.String()
}

// scrollLines moves the viewport delta lines without touching the caret — the
// wheel's browse mode — clamped so the view never passes the buffer's ends. The
// caret may leave the screen; the next caret-moving key snaps the view back to it
// (clampScroll runs after every keystroke).
