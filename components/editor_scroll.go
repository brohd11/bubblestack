package components

import (
	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Viewport scrolling for EditorScreen: the scroll offset, the clamps that keep it inside
// the buffer, and the proportional scrollbar that takes the rightmost column when the
// buffer overflows.

func (s *EditorScreen) scrollLines(delta int) {
	s.scrY += delta
	s.clampScrollBounds()
}

// wheel routes one wheel notch. The vertical pair turns sideways while alt is held: the
// terminals that matter claim ctrl+wheel for their own font zoom and shift+wheel for
// bypassing mouse reporting, so alt is the one modifier that reaches the app. A
// trackpad's horizontal swipe arrives as its own button and needs no modifier at all.
func (s *EditorScreen) wheel(m tea.MouseMsg) {
	switch m.Button {
	case tea.MouseButtonWheelUp:
		if m.Alt {
			s.scrollCells(-editorHWheelStep)
		} else {
			s.scrollLines(-editorWheelStep)
		}
	case tea.MouseButtonWheelDown:
		if m.Alt {
			s.scrollCells(editorHWheelStep)
		} else {
			s.scrollLines(editorWheelStep)
		}
	case tea.MouseButtonWheelLeft:
		s.scrollCells(-editorHWheelStep)
	case tea.MouseButtonWheelRight:
		s.scrollCells(editorHWheelStep)
	}
}

// scrollCells rolls the horizontal window without moving the caret, as scrollLines does
// vertically. Wrapped, there is nowhere to roll to: soft wrap puts every cell of a line
// on screen already, and renderWrappedRow windows on the chunk's start rather than scrX.
func (s *EditorScreen) scrollCells(delta int) {
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
func (s *EditorScreen) maxScrollX() int {
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
func (s *EditorScreen) clampScrollBounds() {
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

// clampScroll scrolls the viewport just enough to keep the cursor visible. In normal
// mode it tracks the cursor in both axes; with wrap enabled it keeps only the row on
// screen (soft wrap means the whole wrapped line is visible horizontally).
func (s *EditorScreen) clampScroll() {
	if s.w < 1 || s.h < 1 {
		return
	}
	if s.wrap {
		row := s.wrapRowForCursor()
		if row < s.scrY {
			s.scrY = row
		}
		if row >= s.scrY+s.h {
			s.scrY = row - s.h + 1
		}
		return
	}
	if s.curY < s.scrY {
		s.scrY = s.curY
	}
	if s.curY >= s.scrY+s.h {
		s.scrY = s.curY - s.h + 1
	}
	curCell := cellOfCol(s.lines[s.curY], s.curX)
	if curCell < s.scrX {
		s.scrX = curCell
	}
	w := s.contentW()
	if curCell >= s.scrX+w {
		s.scrX = curCell - w + 1
	}
	// One more column when the overflow marker is about to claim the last one and the
	// caret is standing in it — walking right along a long line lands there on every
	// keystroke, and the marker would paint over the caret. Scrolling one further leaves
	// the caret second from the right; whether the marker still draws after the nudge,
	// the state is stable, so this never runs twice.
	if w >= 2 && curCell == s.scrX+w-1 && len(expandLine(s.lines[s.curY])) > s.scrX+w {
		s.scrX++
	}
}

// barVisible reports whether the scrollbar column is drawn: only when the buffer
// overflows the viewport. Wrapped, the answer is the one rebuildWrapRows settled while
// measuring the rows — asking again from the row count here would be the same question
// the rebuild already had to answer to pick its width.
func (s *EditorScreen) barVisible() bool {
	if s.wrap {
		s.rebuildWrapRows()
		return s.wrapBar
	}
	return len(s.lines) > s.h
}

// textW is the width the text window gets — one column short of s.w while the
// scrollbar takes the rightmost cell, so the caret can never hide under the bar.
func (s *EditorScreen) textW() int {
	if s.barVisible() {
		return s.w - 1
	}
	return s.w
}

// contentW is what the buffer text itself gets: the text window net of the left gutter
// (the sign column and the line numbers). It is the horizontal window renderLine cuts
// and clampScroll scrolls, and the width buildWrapRows breaks lines at.
func (s *EditorScreen) contentW() int {
	return max(s.textW()-s.leftGutterWidth(), 1)
}

// scrollbarCell renders row i of the scrollbar: a thumb sized to the viewport's
// share of the buffer and placed proportionally to scrY, on a full-height track.
// Track and thumb share the one glyph; the color does the talking — the track is
// dimmed, the thumb wears the theme's focus color. The styles are built per call
// so a theme switch repaints, as renderLine's muted style does.
func (s *EditorScreen) scrollbarCell(row int) string {
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
