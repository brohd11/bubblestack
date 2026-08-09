package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestCompositeWidthPreserved splices a box onto a background and asserts every
// background row keeps its original display width — the core invariant that catches
// off-by-one slicing and ANSI bleed regressions in Composite.
func TestCompositeWidthPreserved(t *testing.T) {
	const w, h = 40, 12
	var rows []string
	for i := 0; i < h; i++ {
		rows = append(rows, strings.Repeat("·", w))
	}
	bg := strings.Join(rows, "\n")

	// A styled (colored, bordered) box, so the test exercises ANSI-aware slicing.
	box := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("5")).
		Foreground(lipgloss.Color("2")).
		Render("hello\nworld")
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)

	x := (w - bw) / 2
	y := (h - bh) / 2
	out := Composite(bg, box, x, y)

	gotLines := strings.Split(out, "\n")
	if len(gotLines) != h {
		t.Fatalf("row count changed: got %d, want %d", len(gotLines), h)
	}
	for i, line := range gotLines {
		if got := ansi.StringWidth(line); got != w {
			t.Errorf("row %d width = %d, want %d", i, got, w)
		}
	}
}

// TestCompositePunchesHole checks the box content actually lands at the target cell
// and the background still shows on the rows the box doesn't cover.
func TestCompositePunchesHole(t *testing.T) {
	bg := strings.Join([]string{
		"..........",
		"..........",
		"..........",
	}, "\n")
	out := Composite(bg, "AB", 4, 1)
	lines := strings.Split(out, "\n")

	if lines[0] != ".........." || lines[2] != ".........." {
		t.Errorf("uncovered rows changed: %q / %q", lines[0], lines[2])
	}
	// Row 1: 4 dots, then AB, then the remaining 4 dots (strip ANSI resets first).
	if got := ansi.Strip(lines[1]); got != "....AB...." {
		t.Errorf("row 1 = %q, want %q", got, "....AB....")
	}
}

// TestCompositeOutOfBoundsRows drops foreground rows that fall outside the background.
func TestCompositeOutOfBoundsRows(t *testing.T) {
	bg := "....\n...."
	// y = 1 places a two-line fg at rows 1 and 2; row 2 doesn't exist and must be dropped.
	out := Composite(bg, "XX\nYY", 1, 1)
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("row count = %d, want 2", len(lines))
	}
	if got := ansi.Strip(lines[1]); got != ".XX." {
		t.Errorf("row 1 = %q, want %q", got, ".XX.")
	}
}

// posOverlay is an overlay stub that anchors its box at a fixed cell, to exercise
// the router's OverlayPositioner branch and its on-screen clamping.
type posOverlay struct {
	stubScreen
	x, y int
}

func (posOverlay) IsOverlay() bool                  { return true }
func (p posOverlay) OverlayPos(int, int) (int, int) { return p.x, p.y }
func (posOverlay) View(*Shared) string              { return "AB" }

// TestOverlayPositioned asserts an OverlayPositioner's box lands at its anchor
// rather than centered, and that an anchor past the frame's edge clamps so the
// box stays fully on screen.
func TestOverlayPositioned(t *testing.T) {
	tm := sized(newCoreTestRouter()) // 80x24

	tm = pump(tm, Push(posOverlay{x: 4, y: 2}))
	lines := strings.Split(tm.(Router).View(), "\n")
	if got := ansi.Strip(lines[2]); got[4:6] != "AB" {
		t.Errorf("row 2 = %q, want the box anchored at (4,2)", got)
	}

	tm = pump(tm, Pop(1))
	tm = pump(tm, Push(posOverlay{x: 100, y: 100}))
	lines = strings.Split(tm.(Router).View(), "\n")
	if len(lines) != 24 {
		t.Fatalf("row count = %d, want 24 (frame height)", len(lines))
	}
	if got := ansi.Strip(lines[23]); !strings.HasSuffix(got, "AB") {
		t.Errorf("last row = %q, want the box clamped to the bottom-right corner", got)
	}
}
