package editor

import (
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Viewport scrolling for Screen: offsets, bounds clamps and the scrollbar.

func (s *Screen) scrollLines(delta int) {
	s.scrY += delta
	s.clampScrollBounds()
}

// scrollbarRowAt reports the body row of a click on the scrollbar, using positionAt's
// coordinate convention.
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

// scrollToBarRow maps the track onto the scroll range. The caret stays put, as with the
// wheel.
func (s *Screen) scrollToBarRow(row int) {
	limit := max(s.rowCount()-s.h, 0)
	if limit == 0 || s.h <= 1 {
		s.scrY = 0
		return
	}
	s.scrY = (row*limit + (s.h-1)/2) / (s.h - 1)
	s.clampScrollBounds()
}

// wheel routes one notch. Alt turns vertical scrolling sideways: terminals keep ctrl+wheel
// (zoom) and shift+wheel (selection). Horizontal swipes arrive as their own buttons.
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

// scrollCells scrolls horizontally without moving the caret; a no-op when wrapped.
func (s *Screen) scrollCells(delta int) {
	if s.wrap {
		return
	}
	s.scrX += delta
	s.clampScrollBounds()
}

// maxScrollX is one column past the widest line, where clampScroll parks scrX for a caret
// at that line's end, so the two clamps never fight.
func (s *Screen) maxScrollX() int {
	widest := 0
	for _, line := range s.lines {
		if c := cellOfCol(line, len(line)); c > widest {
			widest = c
		}
	}
	return max(widest-s.contentW()+1, 0)
}

// clampScrollBounds keeps the offsets inside the buffer without chasing the caret. The
// router re-lays out after every message, so chasing here would undo every wheel scroll;
// caret moves re-assert visibility through clampScroll.
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
	// Wrapped, scrX is left alone so unwrapping restores it; at 0 there is nothing to bound.
	if !s.wrap && s.scrX > 0 {
		if m := s.maxScrollX(); s.scrX > m {
			s.scrX = m
		}
	}
}

// hCaretBand is the caret's allowed column range: scroll right past hi, left behind lo,
// both measured from the right edge so the text behind the caret stays in view. The gap
// is hysteresis. It only engages on lines about as long as the window.
func (s *Screen) hCaretBand() (lo, hi int) {
	w := s.contentW()
	return w - 1 - w*editorHCaretFarPct/100, w - 1 - w*editorHCaretNearPct/100
}

// clampScroll keeps the caret visible and, horizontally, inside hCaretBand: the clamp for
// keys. Wrapped, only the row matters.
func (s *Screen) clampScroll() {
	if s.w < 1 || s.h < 1 {
		return
	}
	if s.clampScrollRow() {
		return
	}
	s.clampScrollBand()
}

// clampScrollVisible is the mouse clamp: keep the caret on screen and otherwise leave the
// view alone, so clicked text does not slide from under the pointer.
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
		// Stop scrolling right at the end of the current line (the band would only show blank),
		// which also avoids a whole-buffer scan per keystroke and never exceeds maxScrollX.
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

// nudgeOffMarker scrolls one more column when the overflow marker would cover the caret.
// Only reachable in narrow panes and via the mouse clamp; it is stable, so it never runs
// twice.
func (s *Screen) nudgeOffMarker(curCell int) {
	w := s.contentW()
	if w >= 2 && curCell == s.scrX+w-1 && len(expandLine(s.lines[s.curY])) > s.scrX+w {
		s.scrX++
	}
}

// barVisible reports whether the scrollbar is drawn (the buffer overflows). Wrapped, it is
// what rebuildWrapRows settled.
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

// contentW is the text window's width net of the left gutter: what renderLine cuts,
// clampScroll scrolls and buildWrapRows wraps at.
func (s *Screen) contentW() int {
	return max(s.textW()-s.leftGutterWidth(), 1)
}

// scrollbarCell renders one scrollbar row: a proportional thumb in the focus color on a
// dimmed track, styles built per call.
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
