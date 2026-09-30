package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

// TestScrollbarThumb: the thumb is the viewport's share of the content, at least one cell,
// hugging the top unscrolled and the bottom fully scrolled.
func TestScrollbarThumb(t *testing.T) {
	b := Scrollbar{Total: 100, Height: 20}
	if !b.Needed() {
		t.Fatal("100 rows over 20 should need a bar")
	}
	if top, size := b.Thumb(); top != 0 || size != 4 {
		t.Fatalf("unscrolled thumb = (%d, %d), want (0, 4)", top, size)
	}
	b.Offset = 80
	if top, size := b.Thumb(); top != 16 || size != 4 {
		t.Fatalf("bottom thumb = (%d, %d), want (16, 4)", top, size)
	}
	b.Offset = 40
	if top, _ := b.Thumb(); top != 8 {
		t.Fatalf("middle thumb top = %d, want 8", top)
	}
	if _, size := (Scrollbar{Total: 10000, Height: 20}).Thumb(); size != 1 {
		t.Fatalf("a huge document's thumb = %d cells, want 1", size)
	}
	if (Scrollbar{Total: 20, Height: 20}).Needed() {
		t.Fatal("content that fits needs no bar")
	}
}

// TestScrollbarCells: thumb and track differ only in color, and an unfocused bar is all
// track.
func TestScrollbarCells(t *testing.T) {
	thumb := lipgloss.NewStyle().Foreground(core.FocusedColor).Render("│")
	track := lipgloss.NewStyle().Foreground(core.MutedColor).Render("│")
	b := Scrollbar{Total: 100, Height: 20, Offset: 40, Focused: true}
	rows := strings.Split(b.Column(), "\n")
	if len(rows) != 20 {
		t.Fatalf("Column has %d rows, want 20", len(rows))
	}
	for i, got := range rows {
		want := track
		if i >= 8 && i < 12 {
			want = thumb
		}
		if got != want {
			t.Fatalf("row %d = %q, want %q", i, got, want)
		}
	}
	b.Focused = false
	if b.Cell(8) != track {
		t.Fatal("an unfocused bar should draw its thumb as track")
	}
}

// TestScrollbarPress: a track press jumps proportionally (first row top, last row bottom)
// and grabs; a thumb press grabs without moving the view.
func TestScrollbarPress(t *testing.T) {
	b := Scrollbar{Total: 100, Height: 20}
	for _, c := range []struct{ row, want int }{{0, 0}, {19, 80}, {10, (10*80 + 9) / 19}} {
		b.Offset = 0
		if c.row < 4 {
			b.Offset = 50 // off the thumb, so the press is a track press
		}
		if got := b.Press(c.row); got != c.want || b.Offset != got {
			t.Fatalf("track press at %d = %d (Offset %d), want %d", c.row, got, b.Offset, c.want)
		}
		if !b.Dragging() {
			t.Fatal("a press should grab the thumb")
		}
		b.Release()
	}

	b.Offset = 40 // thumb on rows 8..11
	if got := b.Press(10); got != 40 {
		t.Fatalf("a thumb press moved the view to %d, want 40", got)
	}
}

// TestScrollbarDrag: the view moves by the pointer's travel from the press, wherever on
// the thumb it was grabbed; it never snaps on the first motion, saturates at both ends of
// the range, and a released bar ignores drags.
func TestScrollbarDrag(t *testing.T) {
	// 100 rows over 20: a 4-cell thumb on a 16-row span over an 80-row range, so a row
	// of travel is 5 rows of scroll. 43 sits between two thumb positions.
	b := Scrollbar{Total: 100, Height: 20, Offset: 43}
	top, _ := b.Thumb()
	b.Press(top + 2)
	if got := b.DragTo(top + 2); got != 43 {
		t.Fatalf("no travel moved the view to %d, want 43", got)
	}
	if got := b.DragTo(top + 3); got != 48 {
		t.Fatalf("one row down = %d, want 48", got)
	}
	if got := b.DragTo(top + 1); got != 38 {
		t.Fatalf("one row up from the press = %d, want 38", got)
	}
	b.Release()
	b.Offset = 43
	b.Press(top) // the thumb's first cell: same travel, same result
	if got := b.DragTo(top + 1); got != 48 {
		t.Fatalf("grabbing the thumb's top, one row down = %d, want 48", got)
	}
	if got := b.DragTo(-50); got != 0 {
		t.Fatalf("dragging far above = %d, want 0", got)
	}
	if got := b.DragTo(500); got != 80 {
		t.Fatalf("dragging far below = %d, want 80", got)
	}
	if got := b.DragTo(top + 1); got != 48 {
		t.Fatalf("back to one row below the press after overshooting = %d, want 48 (no drift)", got)
	}
	b.Release()
	if got := b.DragTo(0); got != 48 || b.Dragging() {
		t.Fatalf("a released bar moved to %d", got)
	}

	fits := Scrollbar{Total: 10, Height: 20}
	fits.Press(3)
	if got := fits.DragTo(15); got != 0 {
		t.Fatalf("content that fits scrolled to %d", got)
	}
}
