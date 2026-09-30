package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

// Scrollbar is a one-column vertical scrollbar: a proportional thumb in the focus color on
// a dimmed track, both drawn with the one "│" glyph. It knows nothing of its host. The host
// sets Total, Height and Offset before each use, draws Cell per row (or Column), maps the
// pointer to a track row, and copies the offset that Press and DragTo return back into its
// view.
//
// A press on the thumb starts a drag without moving the view; a press on the track jumps
// there (the track maps proportionally onto the scroll range) and starts the drag from
// the new position. While dragging, the view moves by the pointer's travel since the
// press, one row of travel per row of thumb, so where on the thumb it was grabbed never
// matters. It reads only the row, so a drag survives the pointer wandering off the column
// or out of the pane.
type Scrollbar struct {
	Total   int  // content rows
	Height  int  // viewport rows, which is also the track's length
	Offset  int  // first visible row
	Focused bool // the thumb takes the focus color only while focused

	startRow, startOffset int // pointer row and Offset when the drag began
	dragging              bool
}

// Needed reports whether the content overflows the viewport, i.e. whether a bar is drawn.
func (b Scrollbar) Needed() bool { return b.Total > b.Height }

// limit is the largest Offset: how far the view can scroll.
func (b Scrollbar) limit() int { return max(b.Total-b.Height, 0) }

// Thumb is the thumb's first track row and its length: sized to the viewport's share of
// the content, at least one cell, and positioned by Offset's share of the scroll range.
func (b Scrollbar) Thumb() (top, size int) {
	total := max(b.Total, 1)
	size = max(b.Height*b.Height/total, 1)
	if d := b.limit(); d > 0 {
		top = min(max(b.Offset, 0), d) * (b.Height - size) / d
	}
	return top, size
}

// Cell renders track row row.
func (b Scrollbar) Cell(row int) string {
	color := core.MutedColor
	if top, size := b.Thumb(); b.Focused && row >= top && row < top+size {
		color = core.FocusedColor
	}
	return lipgloss.NewStyle().Foreground(color).Render("│")
}

// Column renders the whole track, Height cells joined by newlines.
func (b Scrollbar) Column() string {
	rows := make([]string, max(b.Height, 0))
	for i := range rows {
		rows[i] = b.Cell(i)
	}
	return strings.Join(rows, "\n")
}

// OffsetAt maps track row row proportionally onto the scroll range: the first row is the
// top, the last row the bottom.
func (b Scrollbar) OffsetAt(row int) int {
	d := b.limit()
	if d == 0 || b.Height <= 1 {
		return 0
	}
	row = min(max(row, 0), b.Height-1)
	return (row*d + (b.Height-1)/2) / (b.Height - 1)
}

// Press starts a drag at track row row and returns the new Offset: unchanged on the
// thumb, OffsetAt(row) on the track.
func (b *Scrollbar) Press(row int) int {
	if top, size := b.Thumb(); row < top || row >= top+size {
		b.Offset = b.OffsetAt(row)
	}
	b.startRow, b.startOffset = row, b.Offset
	b.dragging = true
	return b.Offset
}

// DragTo scrolls by the pointer's travel from the press to row, scaled so the thumb keeps
// pace with the pointer, and returns the new Offset. It works from the press every call,
// so the first motion never snaps and dragging past an end and back does not drift.
// Without a drag it returns Offset unchanged.
func (b *Scrollbar) DragTo(row int) int {
	if !b.dragging {
		return b.Offset
	}
	_, size := b.Thumb()
	span := b.Height - size
	if span <= 0 {
		return b.Offset
	}
	travel := (row - b.startRow) * b.limit()
	step := (max(travel, -travel) + span/2) / span // rounded half away from zero
	if travel < 0 {
		step = -step
	}
	b.Offset = min(max(b.startOffset+step, 0), b.limit())
	return b.Offset
}

// Release ends the drag.
func (b *Scrollbar) Release() { b.dragging = false }

// Dragging reports whether the thumb is grabbed.
func (b Scrollbar) Dragging() bool { return b.dragging }
