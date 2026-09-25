package editor

import (
	"strings"
	"time"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// Cursor movement, mouse gestures and selection for Screen.

// ---------- cursor movement ----------

func (s *Screen) moveLeft() {
	s.wrapGoalValid = false
	if s.curX > 0 {
		s.curX--
	} else if s.curY > 0 {
		s.curY--
		s.curX = len(s.lines[s.curY])
	}
	s.wantX = s.curX
}

func (s *Screen) moveRight() {
	s.wrapGoalValid = false
	if s.curX < len(s.lines[s.curY]) {
		s.curX++
	} else if s.curY < len(s.lines)-1 {
		s.curY++
		s.curX = 0
	}
	s.wantX = s.curX
}

func (s *Screen) moveHome() {
	s.wrapGoalValid = false
	s.curX, s.wantX = 0, 0
}

func (s *Screen) moveEnd() {
	s.wrapGoalValid = false
	s.curX = len(s.lines[s.curY])
	s.wantX = s.curX
}

// moveVertical moves delta display rows, preserving the desired column across short
// rows. Unwrapped it uses the rune column wantX; wrapped it uses a row-relative cell.
// Holding an arrow past either boundary reaches the start or end of the document.
func (s *Screen) moveVertical(delta int) {
	if s.wrap {
		s.moveWrappedVertical(delta)
		return
	}
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

// moveWrappedVertical uses the same display rows as rendering. The goal is a cell
// offset within a row, retained even when a short row or a tab clamps the caret.
func (s *Screen) moveWrappedVertical(delta int) {
	row := s.wrapRowForCursor()
	if !s.wrapGoalValid {
		s.wrapGoal = cellOfCol(s.lines[s.curY], s.curX) - s.wrapRows[row].start
		s.wrapGoalValid = true
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for target := row + delta; target >= 0 && target < len(s.wrapRows); target += dir {
		p, ok := s.wrappedPosition(target, s.wrapGoal)
		if !ok {
			continue // this row contains only the middle of an expanded tab
		}
		s.curY, s.curX, s.wantX = p.y, p.x, p.x
		return
	}
	// Holding an arrow at the document boundary still reaches its first/last cell.
	if dir < 0 {
		s.curY = 0
		s.moveHome()
	} else {
		s.curY = len(s.lines) - 1
		s.moveEnd()
	}
}

// wrappedPosition clamps a row-relative cell to an insertion position on that row.
// Blank padding must not map into the next row's text. If only a tab's interior is
// present, false lets vertical navigation skip the row; mouse gestures use the tab's
// start, preserving the usual click-inside-a-tab behavior.
func (s *Screen) wrappedPosition(row, x int) (textPos, bool) {
	r := s.wrapRows[row]
	line := s.lines[r.line]
	end := r.end
	if !s.lastRowOfLine(row) {
		end-- // the boundary itself belongs to the next row
	}
	col := colAtCell(line, min(r.start+max(x, 0), end))
	if cellOfCol(line, col) < r.start {
		if col < len(line) && cellOfCol(line, col+1) <= end {
			col++ // a tab began on the preceding row; use its other side
		} else {
			return textPos{r.line, col}, false
		}
	}
	return textPos{r.line, col}, true
}

// positionAt maps a mouse cell to a buffer position. Drags can arrive outside the pane
// (ModularScreen keeps the gesture); clamp pins those to the nearest visible cell without
// scrolling.
func (s *Screen) positionAt(sh *core.Shared, x, y int, clamp bool) (textPos, bool) {
	s.wrapGoalValid = false
	if x -= s.insetX(); x < 0 {
		x = 0 // a press left of text reads as column zero, preserving click behavior
	}
	if s.barVisible() && x >= s.textW() {
		if !clamp {
			return textPos{}, false // a press/release on the scrollbar is not buffer text
		}
		x = s.textW() - 1
	}
	if x -= s.leftGutterWidth(); x < 0 {
		x = 0 // a click on the gutter reads as column 0, as one left of the body does
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
		p, _ := s.wrappedPosition(min(row, len(s.wrapRows)-1), x)
		return p, true
	} else if row >= len(s.lines) {
		row = len(s.lines) - 1
	}
	col := colAtCell(s.lines[row], cell)
	return textPos{row, col}, true
}

// clickAt preserves the editor's original single-click behavior. It is kept as a
// small wrapper because tests and callers inside this package use it directly.
func (s *Screen) clickAt(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.resetMouseGesture()
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScrollVisible()
}

// pressSelection handles a left press. Repeats on the same character within
// editorMultiClickWindow select a word, then a line; a fourth starts over. now is a
// parameter for tests.
func (s *Screen) pressSelection(sh *core.Shared, x, y int, now time.Time) {
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

// pressContext is the right-button gesture. Inside the selection it keeps it, so the menu
// acts on it; outside it moves the caret (so a paste lands at the pointer). A press on no
// buffer position (scrollbar, title, search bar) opens nothing. The menu opens below the
// pressed row (above when there is no room), not relative to the selection, so it stays
// near the pointer.
func (s *Screen) pressContext(sh *core.Shared, x, y int) core.Action {
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

// absCell converts an incoming mouse cell to absolute terminal cells for an overlay
// anchor. Keyed on s.embedded, like positionAt: standalone cells are already absolute;
// embedded ones are slot-relative, so the pane origin is added back (paneGeometry falls
// back to (0, BodyY) before the first frame).
func (s *Screen) absCell(sh *core.Shared, x, y int) (int, int) {
	if !s.embedded {
		return x, y
	}
	ox, oy, _, _ := s.paneGeometry(sh)
	return ox + x, oy + y
}

// positionSelected reports whether the character at p is inside the selection; the
// position after a line's last rune stands for its selected newline.
func (s *Screen) positionSelected(p textPos) bool {
	return s.selectionActive() && !posLess(p, s.selStart) && posLess(p, s.selEnd)
}

// selectWordAt selects the run of same-class runes under p. A blank line has no run, so
// it stays an ordinary caret placement.
func (s *Screen) selectWordAt(p textPos) {
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
func (s *Screen) selectLineAt(p textPos) {
	end := textPos{p.y, len(s.lines[p.y])}
	if p.y < len(s.lines)-1 {
		end = textPos{p.y + 1, 0}
	}
	s.selStart, s.selEnd = textPos{p.y, 0}, end
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

func (s *Screen) startDrag(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.startDragAt(p)
}

func (s *Screen) startDragAt(p textPos) {
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScrollVisible()
	s.dragAnchor = p
	s.dragAnchorEnd = s.cellEnd(p)
	s.dragging = true
}

// resetMouseGesture ends the active mouse gesture and bumps the drag generation, so any
// in-flight auto-scroll tick arrives stale.
func (s *Screen) resetMouseGesture() {
	s.dragging, s.dragScrolling = false, false
	s.dragSeq++
}

// dragScrollCmd schedules one auto-scroll frame, broadcast so it reaches this editor even
// under a dialog.
func (s *Screen) dragScrollCmd() tea.Cmd {
	seq := s.dragSeq
	return tea.Tick(editorDragScrollInterval, func(time.Time) tea.Msg {
		return core.PropagateAll(editorDragScrollMsg{target: s, seq: seq})
	})
}

// trackDrag records the pointer, extends the selection to it and arms the auto-scroll
// clock when it is in an edge band. Each frame re-arms itself and stops when the pointer
// returns or the gesture ends.
func (s *Screen) trackDrag(sh *core.Shared, x, y int) tea.Cmd {
	// Nothing to do when neither the pointer cell nor the view moved; a duplicate would cost
	// a full re-render. The scroll offsets matter because a mid-drag wheel notch moves the
	// view under a still pointer.
	if s.dragging && x == s.dragX && y == s.dragY && s.scrY == s.dragScrY && s.scrX == s.dragScrX {
		return nil
	}
	s.dragX, s.dragY = x, y
	s.dragScrY, s.dragScrX = s.scrY, s.scrX
	s.extendDrag(sh, x, y)
	if s.dragScrolling {
		return nil
	}
	if dx, dy := s.dragEdgeScroll(sh, x, y); dx == 0 && dy == 0 {
		return nil
	}
	s.dragScrolling = true
	return s.dragScrollCmd()
}

// handleDragScroll runs one auto-scroll frame: scroll (clamped, caret untouched), then
// re-extend the selection over what was revealed, so it follows the pointer.
func (s *Screen) handleDragScroll(sh *core.Shared, m editorDragScrollMsg) core.Action {
	if m.target != s || m.seq != s.dragSeq || !s.dragging || !s.dragScrolling {
		return core.Action{} // a stale frame, or the gesture is over: the clock stops here
	}
	// Consume the tick before re-arming: the broadcast can deliver it twice (pane and host
	// registry), and each delivery would otherwise start another timer.
	s.dragSeq++
	dx, dy := s.dragEdgeScroll(sh, s.dragX, s.dragY)
	if dx == 0 && dy == 0 {
		s.dragScrolling = false // back inside the pane; the next motion re-arms
		return core.Action{}
	}
	wasY, wasX := s.scrY, s.scrX
	s.scrollLines(dy)
	// Stop horizontal scrolling at the line under the pointer, not the widest line, or the
	// line can scroll off screen and every motion maps to its end.
	if dx > 0 {
		if p, ok := s.positionAt(sh, s.dragX, s.dragY, true); ok {
			line := s.lines[p.y]
			limit := max(cellOfCol(line, len(line))-s.contentW()+1, 0)
			dx = min(dx, max(limit-s.scrX, 0))
		}
	}
	s.scrollCells(dx)
	if s.scrY == wasY && s.scrX == wasX {
		s.dragScrolling = false // no work remains, even if the terminal lost the release
		return core.Action{}
	}
	s.extendDrag(sh, s.dragX, s.dragY)
	return core.Async(s.dragScrollCmd())
}

// dragEdgeScroll returns the per-frame scroll deltas for a pointer at (x, y), measured
// against the content window. Off-pane coordinates are normal (ModularScreen keeps the
// gesture) and the overshoot drives the ramp.
func (s *Screen) dragEdgeScroll(sh *core.Shared, x, y int) (dx, dy int) {
	cy := y - s.insetY()
	if !s.embedded {
		cy -= sh.BodyY() // absolute coordinates: the chrome rows come off too
	}
	zy := max(s.h*editorDragEdgePct/100, 1)
	switch {
	case cy < zy:
		dy = -editorDragStep(zy-cy, zy, editorDragScrollUnitY)
	case cy >= s.h-zy:
		dy = editorDragStep(cy-(s.h-zy)+1, zy, editorDragScrollUnitY)
	}
	// Wrapped, there is nowhere to roll sideways: every cell of a line is on screen and
	// scrX is inert (scrollCells).
	if s.wrap {
		return 0, dy
	}
	cx := x - s.insetX() - s.leftGutterWidth()
	w := s.contentW()
	zx := max(w*editorDragEdgePct/100, 2)
	switch {
	case cx < zx:
		dx = -editorDragStep(zx-cx, zx, editorDragScrollUnitX)
	case cx >= w-zx:
		dx = editorDragStep(cx-(w-zx)+1, zx, editorDragScrollUnitX)
	}
	return dx, dy
}

// editorDragStep turns an overshoot into one frame's step: one unit inside the band, one
// more per further band-width, capped.
func editorDragStep(over, zone, unit int) int {
	zone = max(zone, 1)
	return unit * min(1+(over-1)/zone, editorDragScrollMaxUnits)
}

func (s *Screen) extendDrag(sh *core.Shared, x, y int) {
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

func (s *Screen) cellEnd(p textPos) textPos {
	if p.x < len(s.lines[p.y]) {
		return textPos{p.y, p.x + 1}
	}
	return p
}

func posLess(a, b textPos) bool { return a.y < b.y || a.y == b.y && a.x < b.x }

func (s *Screen) selectionActive() bool { return posLess(s.selStart, s.selEnd) }

// ---------- keyboard selection ----------

// selectionAnchor is the fixed end of the selection that shifted motions pivot on. It is
// derived, not stored, so shifted keys pick up mouse selections too: with no selection
// the caret is the anchor, otherwise the caret sits on one end and the anchor is the
// other.
func (s *Screen) selectionAnchor() textPos {
	caret := textPos{s.curY, s.curX}
	switch {
	case !s.selectionActive():
		return caret
	case caret == s.selEnd:
		return s.selStart
	default:
		return s.selEnd
	}
}

// selectFrom selects the range between anchor and caret; a caret back on its anchor
// clears it. It uses clampScroll so the view follows the caret (unlike drags, whose
// off-pane ends use the mouse clamp in extendSelectionTo).
func (s *Screen) selectFrom(anchor textPos) {
	s.selectRangeFrom(anchor)
	s.clampScroll()
}

func (s *Screen) selectRangeFrom(anchor textPos) {
	caret := textPos{s.curY, s.curX}
	switch {
	case posLess(caret, anchor):
		s.selStart, s.selEnd = caret, anchor
	case posLess(anchor, caret):
		s.selStart, s.selEnd = anchor, caret
	default:
		s.clearSelection()
	}
}

// extendSelection runs an ordinary caret move as a selection gesture: read the anchor
// BEFORE the move (the move is what shifts the caret off it), then span the two.
func (s *Screen) extendSelection(move func()) {
	anchor := s.selectionAnchor()
	move()
	s.selectFrom(anchor)
}

// selectMove returns the caret move a shifted chord extends the selection over, or nil.
// Word moves use ctrl+shift+←/→: bubbletea has no alt+shift arrow keys. shift+↑/↓ lose
// their shift on Apple Terminal and degrade to plain moves, which is acceptable here.
func (s *Screen) selectMove(k string) func() {
	switch k {
	case "shift+left":
		return s.moveLeft
	case "shift+right":
		return s.moveRight
	case "shift+up":
		return func() { s.moveVertical(-1) }
	case "shift+down":
		return func() { s.moveVertical(1) }
	case "shift+home":
		return s.moveHome
	case "shift+end":
		return s.moveEnd
	case "ctrl+shift+left":
		return s.moveWordBack
	case "ctrl+shift+right":
		return s.moveWordForward
	}
	return nil
}

// extendSelectionTo is shift+click: keep the anchor, move the caret to the pointer.
// Presses off the text are ignored. The drag continues from the same anchor, using caret
// positions at both ends so keyboard and mouse agree on the anchored character.
func (s *Screen) extendSelectionTo(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	anchor := s.selectionAnchor()
	s.clickCount = 0 // an extend is never a step in a multi-click run
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.selectRangeFrom(anchor)
	s.clampScrollVisible()
	s.dragAnchor, s.dragAnchorEnd = anchor, anchor
	s.dragging = true
}

func (s *Screen) clearSelection() { s.selStart, s.selEnd = textPos{}, textPos{} }

func (s *Screen) deleteSelection() {
	if !s.selectionActive() {
		return
	}
	start := s.selStart
	s.deleteRange(start.y, start.x, s.selEnd.y, s.selEnd.x)
	s.curY, s.curX, s.wantX = start.y, start.x, start.x
	s.clearSelection()
}

func (s *Screen) selectedText() string {
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

// scrollLines moves the viewport delta lines without moving the caret (wheel browsing),
// clamped to the buffer. The next caret-moving key snaps the view back.
