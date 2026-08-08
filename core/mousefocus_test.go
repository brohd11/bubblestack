package core

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestBodyMouseReleasesOutputFocus: the wheel focuses the output pane when it
// rolls over it, and a body-directed mouse event must hand focus back — the
// keyboard has O/esc, the mouse had nothing, so the pane kept the keys (and the
// body stayed dimmed) after the user had moved on.
func TestBodyMouseReleasesOutputFocus(t *testing.T) {
	r, scr, _ := newWheelRouter()
	sh := r.sh
	tm := sized(r) // pane rows 18..23, body above (see newWheelRouter)

	tm = pump(tm, wheelAt(20, tea.MouseButtonWheelDown))
	if !sh.Chrome.outputFocused {
		t.Fatal("a wheel over the pane should focus it")
	}

	before := len(scr.seen)
	tm = pump(tm, wheelAt(5, tea.MouseButtonWheelDown))
	if sh.Chrome.outputFocused {
		t.Fatal("a wheel over the body should release output focus")
	}
	if len(scr.seen) != before+1 {
		t.Fatal("the body wheel should still reach the screen")
	}
}

// TestClickFocusesOutputPane: a click over the log grabs focus exactly as the
// wheel does — and is consumed, so the body screen never sees it.
func TestClickFocusesOutputPane(t *testing.T) {
	r, scr, _ := newWheelRouter()
	sh := r.sh
	tm := sized(r) // pane rows 18..23, body above (see newWheelRouter)

	tm = pump(tm, clickAt(10, 20))
	if !sh.Chrome.outputFocused {
		t.Fatal("a click over the pane should focus it")
	}
	for _, m := range scr.seen {
		if mm, ok := m.(tea.MouseMsg); ok && mm.Button == tea.MouseButtonLeft {
			t.Fatal("the click was aimed at the pane; the body screen must not see it")
		}
	}

	// Symmetry with the wheel: a click over the body hands focus back.
	tm = pump(tm, clickAt(10, 5))
	if sh.Chrome.outputFocused {
		t.Fatal("a click over the body should release output focus")
	}
}
