package core

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The tests here pin the three v2 message-shape traps. None of them is a compile error:
// tea.KeyMsg and tea.MouseMsg are interfaces in v2, so code written against v1's structs
// keeps building while quietly matching events v1 never delivered.

// TestKeyReleaseDoesNotFireBindings: v2's tea.KeyMsg interface covers presses AND
// releases, so a dispatch that still matched it would run every global binding twice the
// moment a terminal negotiates keyboard enhancements. Dispatch is on KeyPressMsg.
func TestKeyReleaseDoesNotFireBindings(t *testing.T) {
	r := newCoreTestRouter()
	tm := sized(r)

	// ctrl+g toggles mouse capture on press. The matching release must do nothing.
	before := tm.(Router).mouseOn
	tm, _ = tm.Update(tea.KeyReleaseMsg(keyMsg("ctrl+g")))
	if got := tm.(Router).mouseOn; got != before {
		t.Fatal("a key RELEASE fired the mouse toggle; dispatch must match tea.KeyPressMsg only")
	}
	tm, _ = tm.Update(keyMsg("ctrl+g"))
	if got := tm.(Router).mouseOn; got == before {
		t.Fatal("the press should still toggle: the test proves nothing otherwise")
	}
}

// TestMouseMotionAndReleaseFallThrough: v1's Router.mouse opened by discarding anything
// that was not MouseActionPress, and a discarded event still fell through to the active
// screen — which is what drives editor drag-select. v2 splits the kinds into types, so
// the router claims clicks and wheels only, and motion/release must still reach the
// screen rather than being claimed or dropped.
func TestMouseMotionAndReleaseFallThrough(t *testing.T) {
	r, scr, _ := newWheelRouter()
	tm := sized(r) // pane rows 18..23, body above

	// Aimed at the output pane, where a CLICK would be claimed as chrome.
	before := len(scr.seen)
	tm, _ = tm.Update(tea.MouseMotionMsg{X: 10, Y: 20, Button: tea.MouseLeft})
	tm, _ = tm.Update(tea.MouseReleaseMsg{X: 10, Y: 20, Button: tea.MouseLeft})
	if got := len(scr.seen) - before; got != 2 {
		t.Fatalf("the screen saw %d of the motion/release pair, want both: the router must not claim them", got)
	}

	// And the click at the same cell IS claimed, so the assertion above is about the
	// event kind rather than about the coordinates missing the pane.
	before = len(scr.seen)
	tm.Update(tea.MouseClickMsg{X: 10, Y: 20, Button: tea.MouseLeft})
	if got := len(scr.seen) - before; got != 0 {
		t.Fatalf("a click over the pane is chrome and must be consumed, screen saw %d", got)
	}
}

// TestBackgroundColorMsgRepaintsPalette: v1 asked lipgloss for the terminal background
// once, through a process-global cache primed before the program started. v2 answers with
// a message, so the palette resolves against whatever the terminal reports — and both
// answers have to reach the exported colors.
func TestBackgroundColorMsgRepaintsPalette(t *testing.T) {
	restoreTheme(t)
	SetTheme("lipgloss")
	accent := themes["lipgloss"].Focused

	r := newCoreTestRouter()
	tm := sized(r)

	tm, _ = tm.Update(tea.BackgroundColorMsg{Color: lightBackground{}})
	if BackgroundIsDark() {
		t.Fatal("a light background message should clear isDark")
	}
	if got := FocusedColor; got != Resolve(Color{Light: accent.Light, Dark: accent.Light}) {
		t.Fatalf("on a light terminal the accent should resolve to its Light variant, got %v", got)
	}

	tm.Update(tea.BackgroundColorMsg{Color: darkBackground{}})
	if !BackgroundIsDark() {
		t.Fatal("a dark background message should set isDark")
	}
	if got := FocusedColor; got != Resolve(Color{Light: accent.Dark, Dark: accent.Dark}) {
		t.Fatalf("on a dark terminal the accent should resolve to its Dark variant, got %v", got)
	}
}

// The two grounds, as bare color.Color values: BackgroundColorMsg.IsDark reads the
// luminance of whatever it carries.
type lightBackground struct{}

func (lightBackground) RGBA() (r, g, b, a uint32) { return 0xffff, 0xffff, 0xffff, 0xffff }

type darkBackground struct{}

func (darkBackground) RGBA() (r, g, b, a uint32) { return 0, 0, 0, 0xffff }
