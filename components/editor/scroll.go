package editor

import (
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Viewport scrolling for Screen: the scroll offset, the clamps that keep it inside
// the buffer, and the proportional scrollbar that takes the rightmost column when the
// buffer overflows.

func (s *Screen) scrollLines(delta int) {
	s.scrY += delta
	s.clampScrollBounds()
}

// scrollbarRowAt reports the body-relative row of a click on the visible scrollbar.
// It follows positionAt's coordinate convention: standalone mouse rows are terminal
// absolute, while an embedded editor receives pane-relative coordinates.
func (s *Screen) scrollbarRowAt(sh *core.Shared, x, y int) (int, bool) {
	if !s.barVisible() || x-s.insetX() != s.textW() {
		return 0, false
	}
	row := y - s.insetY()
	if !s.embedded {
		row -= sh.BodyY()
	}
	return row, row >= 0 && row < s.h
}

// scrollToBarRow maps the track from top to bottom onto the complete valid scroll
// range. The caret deliberately stays put: a bar click is viewport browsing, like the
// wheel, and the next caret movement may snap the view back to it.
func (s *Screen) scrollToBarRow(row int) {
	limit := max(s.rowCount()-s.h, 0)
	if limit == 0 || s.h <= 1 {
		s.scrY = 0
		return
	}
	s.scrY = (row*limit + (s.h-1)/2) / (s.h - 1)
	s.clampScrollBounds()
}

// wheel routes one wheel notch. The vertical pair turns sideways while alt is held: the
// terminals that matter claim ctrl+wheel for their own font zoom and shift+wheel for
// bypassing mouse reporting, so alt is the one modifier that reaches the app. A
// trackpad's horizontal swipe arrives as its own button and needs no modifier at all.
func (s *Screen) wheel(m tea.Mouse) {
	switch m.Button {
	case tea.MouseWheelUp:
		if m.Mod.Contains(tea.ModAlt) {
			s.scrollCells(-editorHWheelStep)
		} else {
			s.scrollLines(-editorWheelStep)
		}
	case tea.MouseWheelDown:
		if m.Mod.Contains(tea.ModAlt) {
			s.scrollCells(editorHWheelStep)
		} else {
			s.scrollLines(editorWheelStep)
		}
	case tea.MouseWheelLeft:
		s.scrollCells(-editorHWheelStep)
	case tea.MouseWheelRight:
		s.scrollCells(editorHWheelStep)
	}
}

// scrollCells rolls the horizontal window without moving the caret, as scrollLines does
// vertically. Wrapped, there is nowhere to roll to: soft wrap puts every cell of a line
// on screen already, and renderWrappedRow windows on the chunk's start rather than scrX.
func (s *Screen) scrollCells(delta int) {
	if s.wrap {
		return
	}
	s.scrX += delta
	s.clampScrollBounds()
}

// maxScrollX is how far right browse mode may roll: one column past the widest line,
// which is exactly where clampScroll parks scrX when the caret sits at the end of that
// line. Any tighter and the two clamps would fight — the bounds clamp would pull the
// caret back under the overflow marker on every wheel tick.
func (s *Screen) maxScrollX() int {
	widest := 0
	for _, line := range s.lines {
		if c := cellOfCol(line, len(line)); c > widest {
			widest = c
		}
	}
	return max(widest-s.contentW()+1, 0)
}

// clampScrollBounds keeps the scroll offsets inside the buffer WITHOUT chasing the
// caret — the resize-time clamp. The router re-lays out after every message
// (core.Router.Update), so a caret-chasing clamp here (clampScroll) would snap the
// view back on every wheel tick and browse mode could never leave the caret behind.
// Typing or moving the caret re-asserts visibility through key's clampScroll.
func (s *Screen) clampScrollBounds() {
	if m := s.rowCount() - s.h; s.scrY > m {
		s.scrY = m
	}
	if s.scrY < 0 {
		s.scrY = 0
	}
	if s.scrX < 0 {
		s.scrX = 0
	}
	// Wrapped, scrX is inert, so it is left alone: unwrapping should restore the
	// horizontal position wrapping suspended. At 0 there is nothing to bound either, and
	// skipping the measurement there keeps ordinary editing off a whole-buffer scan.
	if !s.wrap && s.scrX > 0 {
		if m := s.maxScrollX(); s.scrX > m {
			s.scrX = m
		}
	}
}

// hCaretBand is the range of screen columns the caret may occupy: the view scrolls right
// once the caret passes hi, and left once it falls behind lo. Both ends are measured from the
// RIGHT edge of the content window, because the half of the window that matters is the one
// BEHIND the caret — the text already read. The gap between them is the hysteresis, and the
// floor at column 0 the two clamps apply is what makes a caret walking back leftwards restore
// the start of the line long before it reaches it.
//
// Since both clamps floor at 0, the band only ever engages on lines longer than roughly the
// window: ordinary short-line editing never sees it.
func (s *Screen) hCaretBand() (lo, hi int) {
	w := s.contentW()
	return w - 1 - w*editorHCaretFarPct/100, w - 1 - w*editorHCaretNearPct/100
}

// clampScroll scrolls the viewport to keep the cursor visible, and horizontally to park it
// inside hCaretBand. It is the KEY navigation clamp: typing, arrows, completion and Reveal.
// With wrap enabled it keeps only the row on screen (soft wrap means the whole wrapped line
// is visible horizontally).
func (s *Screen) clampScroll() {
	if s.w < 1 || s.h < 1 {
		return
	}
	if s.clampScrollRow() {
		return
	}
	s.clampScrollBand()
}

// clampScrollVisible is the MOUSE clamp: it keeps the caret on screen and otherwise leaves the
// view exactly where it is. A press puts the caret on text the user was already pointing at, so
// re-parking it inside the band would slide that text out from under the pointer — and, on the
// press that opens a drag, out from under the gesture that is about to extend from it.
// Vertically there is no band, so the two clamps are the same clamp there.
func (s *Screen) clampScrollVisible() {
	if s.w < 1 || s.h < 1 {
		return
	}
	if s.clampScrollRow() {
		return
	}
	s.clampScrollCell()
}

// clampScrollRow keeps the caret's row on screen, and reports whether that was the whole job:
// wrapped, it is.
func (s *Screen) clampScrollRow() bool {
	if s.wrap {
		row := s.wrapRowForCursor()
		if row < s.scrY {
			s.scrY = row
		}
		if row >= s.scrY+s.h {
			s.scrY = row - s.h + 1
		}
		return true
	}
	if s.curY < s.scrY {
		s.scrY = s.curY
	}
	if s.curY >= s.scrY+s.h {
		s.scrY = s.curY - s.h + 1
	}
	return false
}

// clampScrollBand parks the caret inside hCaretBand.
func (s *Screen) clampScrollBand() {
	line := s.lines[s.curY]
	curCell := cellOfCol(line, s.curX)
	lo, hi := s.hCaretBand()
	switch p := curCell - s.scrX; {
	case p > hi:
		// Scrolling right stops at the end of the CURRENT line: with nothing further to
		// reveal, the gap the band would hold open would be blank, so the caret takes the
		// last text cell instead — which is exactly where the minimal clamp leaves it, and
		// what keeps typing at the end of a long line feeling as it always did. Capping
		// against this line rather than maxScrollX is also what keeps the clamp off a
		// whole-buffer scan on every keystroke, and it lands at or below maxScrollX either
		// way, so the bounds clamp never has to disagree with this one.
		s.scrX = min(curCell-hi, max(cellOfCol(line, len(line))-s.contentW()+1, 0))
	case p < lo:
		s.scrX = max(curCell-lo, 0)
	}
	s.nudgeOffMarker(curCell)
}

// clampScrollCell scrolls the minimum that puts the caret's cell on screen, and no more.
func (s *Screen) clampScrollCell() {
	curCell := cellOfCol(s.lines[s.curY], s.curX)
	if curCell < s.scrX {
		s.scrX = curCell
	}
	if w := s.contentW(); curCell >= s.scrX+w {
		s.scrX = curCell - w + 1
	}
	s.nudgeOffMarker(curCell)
}

// nudgeOffMarker takes one more column when the overflow marker is about to claim the caret's
// own and the caret is standing in it — the marker would paint over the caret, which is a lie
// about where typing lands. Scrolling one further leaves the caret second from the right;
// whether the marker still draws after the nudge, the state is stable, so this never runs twice.
//
// The band cannot reach this case: it only lets the caret take the last column when the line
// ENDS there, and a line that ends inside the window draws no marker. It is the narrow-pane
// path, where the percentages round away to nothing, and the mouse clamp's, which parks nothing.
func (s *Screen) nudgeOffMarker(curCell int) {
	w := s.contentW()
	if w >= 2 && curCell == s.scrX+w-1 && len(expandLine(s.lines[s.curY])) > s.scrX+w {
		s.scrX++
	}
}

// barVisible reports whether the scrollbar column is drawn: only when the buffer
// overflows the viewport. Wrapped, the answer is the one rebuildWrapRows settled while
// measuring the rows — asking again from the row count here would be the same question
// the rebuild already had to answer to pick its width.
func (s *Screen) barVisible() bool {
	if s.wrap {
		s.rebuildWrapRows()
		return s.wrapBar
	}
	return len(s.lines) > s.h
}

// textW is the width the text window gets — one column short of s.w while the
// scrollbar takes the rightmost cell, so the caret can never hide under the bar.
func (s *Screen) textW() int {
	if s.barVisible() {
		return s.w - 1
	}
	return s.w
}

// contentW is what the buffer text itself gets: the text window net of the left gutter
// (the sign column and the line numbers). It is the horizontal window renderLine cuts
// and clampScroll scrolls, and the width buildWrapRows breaks lines at.
func (s *Screen) contentW() int {
	return max(s.textW()-s.leftGutterWidth(), 1)
}

// scrollbarCell renders row i of the scrollbar: a thumb sized to the viewport's
// share of the buffer and placed proportionally to scrY, on a full-height track.
// Track and thumb share the one glyph; the color does the talking — the track is
// dimmed, the thumb wears the theme's focus color. The styles are built per call
// so a theme switch repaints, as renderLine's muted style does.
func (s *Screen) scrollbarCell(row int) string {
	total := max(s.rowCount(), 1) // rows, not lines: wrapped, one line can be many
	thumb := max(s.h*s.h/total, 1)
	top := 0
	if d := total - s.h; d > 0 {
		top = min(s.scrY, d) * (s.h - thumb) / d
	}
	color := core.MutedColor
	if row >= top && row < top+thumb {
		color = core.FocusedColor
	}
	if !s.focused {
		color = core.MutedColor
	}
	return lipgloss.NewStyle().Foreground(color).Render("│")
}
