package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// withColor makes styling observable for the duration of one test. Under `go test`
// stdout is not a TTY, so lipgloss resolves the Ascii profile and renders every
// Foreground/Reverse as a no-op — which would make a focused render and a muted one
// byte-identical. Tests that assert on a tint or on the reverse-video cursor force a
// real profile and restore the ambient one after, so the rest of the suite keeps
// comparing plain strings.
func withColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// TestFrameTopLegend: the hand-drawn top edge is exactly innerWidth wide between the
// corners, with the legend interrupting the rule after the first dash.
func TestFrameTopLegend(t *testing.T) {
	top := frameTop("files", 20, false)
	if !strings.HasPrefix(top, "┌─ files ") {
		t.Fatalf("legend should interrupt the rule, got %q", top)
	}
	if !strings.HasSuffix(top, "┐") {
		t.Fatalf("top edge should close with a corner, got %q", top)
	}
	if w := lipgloss.Width(top); w != 22 { // innerWidth + the two corners
		t.Fatalf("top edge width = %d, want 22", w)
	}
	// The fill is whatever the legend leaves: "─ files " is 8 cells, so 12 remain.
	if n := strings.Count(top, "─"); n != 1+12 {
		t.Fatalf("fill dashes = %d, want %d", n, 1+12)
	}
}

// TestFrameTopNoLegend: without a legend the edge is a plain rule of the same width
// (the shape a framed element with no title draws).
func TestFrameTopNoLegend(t *testing.T) {
	top := frameTop("", 6, false)
	if top != "┌"+strings.Repeat("─", 6)+"┐" {
		t.Fatalf("empty legend should give a plain rule, got %q", top)
	}
}

// TestFrameTopOverlongLegend: a legend wider than the run pushes the corner out
// rather than being truncated — the fill clamps at zero.
func TestFrameTopOverlongLegend(t *testing.T) {
	top := frameTop("a very long pane title", 4, false)
	if !strings.HasPrefix(top, "┌─ a very long pane title ┐") {
		t.Fatalf("overlong legend should not be truncated, got %q", top)
	}
}

// TestFrameColorTracksFocus: focus is what the frame's tint means — the two states
// must render differently, and to the palette's two roles.
func TestFrameColorTracksFocus(t *testing.T) {
	if frameColor(true) != core.FocusedColor {
		t.Error("a focused frame should wear the accent")
	}
	if frameColor(false) != core.BorderColor {
		t.Error("an unfocused frame should wear the muted border color")
	}
	withColor(t)
	if frameTop("files", 20, true) == frameTop("files", 20, false) {
		t.Fatal("focused and unfocused frames must render differently")
	}
}

// TestFrameBoxSizing: the box under the legend renders at the same width as the top
// edge, so the two edges line up.
func TestFrameBoxSizing(t *testing.T) {
	f := frame("t", "body", 12, false)
	lines := strings.Split(f, "\n")
	if len(lines) != 3 { // top edge, the one body row, bottom border
		t.Fatalf("frame lines = %d, want 3: %q", len(lines), f)
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != 14 { // innerWidth + the two side borders
			t.Fatalf("line %d width = %d, want 14 (%q)", i, w, l)
		}
	}
}
