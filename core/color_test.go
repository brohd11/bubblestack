package core

import (
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// The expansion and the reverse search must agree, or a Dim would drift a step every time
// it ran: every index 16..255 has to be its own nearest neighbour.
func TestANSIRoundTrip(t *testing.T) {
	for i := 16; i < 256; i++ {
		r, g, b := ansiRGB(i)
		if got := nearestANSI(r, g, b); got != i {
			t.Errorf("index %d expands to (%d,%d,%d) which resolves back to %d", i, r, g, b, got)
		}
	}
}

// The basic sixteen are expandable but never selectable — their RGB is whatever the
// terminal profile says, so a dim that landed on one would not be reproducible.
func TestNearestSkipsBasicSixteen(t *testing.T) {
	if got := nearestANSI(0, 0, 0); got < 16 {
		t.Errorf("pure black resolved to basic index %d, want 16..255", got)
	}
	if got := nearestANSI(255, 255, 255); got < 16 {
		t.Errorf("pure white resolved to basic index %d, want 16..255", got)
	}
}

// Dim moves each variant toward its OWN ground: the Light value lightens (toward a white
// terminal) while the Dark one darkens. This is the whole point of the helper — a literal
// darken would make a dimmed accent the LOUDER of the two on a light background.
func TestDimRecedesPerVariant(t *testing.T) {
	for _, name := range ThemeNames() {
		accent, ok := themes[name].Focused.(lipgloss.AdaptiveColor)
		if !ok {
			t.Fatalf("%s: preset accents are expected to be adaptive", name)
		}
		dimmed, ok := Dim(accent, 0.3).(lipgloss.AdaptiveColor)
		if !ok {
			t.Fatalf("%s: Dim turned an AdaptiveColor into %T", name, Dim(accent, 0.3))
		}
		if lum(t, dimmed.Light) <= lum(t, accent.Light) {
			t.Errorf("%s light: %s -> %s did not lighten", name, accent.Light, dimmed.Light)
		}
		if lum(t, dimmed.Dark) >= lum(t, accent.Dark) {
			t.Errorf("%s dark: %s -> %s did not darken", name, accent.Dark, dimmed.Dark)
		}
	}
}

// A dim that quantizes back onto its input is a no-op the caller cannot see, so every
// preset accent — including mono's near-black and near-white extremes — must actually move.
func TestDimAlwaysMovesPresetAccents(t *testing.T) {
	for _, name := range ThemeNames() {
		accent := themes[name].Focused.(lipgloss.AdaptiveColor)
		dimmed := Dim(accent, 0.3).(lipgloss.AdaptiveColor)
		if dimmed.Light == accent.Light || dimmed.Dark == accent.Dark {
			t.Errorf("%s: Dim(%v) returned an unchanged variant: %v", name, accent, dimmed)
		}
	}
}

// A value Dim cannot read as an index comes back untouched rather than guessed at.
func TestDimPassesThroughUnreadableColors(t *testing.T) {
	hex := lipgloss.Color("#ff00ff")
	if got := Dim(hex, 0.3); got != lipgloss.TerminalColor(hex) {
		t.Errorf("hex color: got %v, want it unchanged", got)
	}
	complete := lipgloss.CompleteColor{TrueColor: "#ff00ff", ANSI256: "212", ANSI: "5"}
	if got := Dim(complete, 0.3); got != lipgloss.TerminalColor(complete) {
		t.Errorf("CompleteColor: got %v, want it unchanged", got)
	}
}

// lum is a variant's grey level, for asserting the direction a dim moved it.
func lum(t *testing.T, s string) int {
	t.Helper()
	i, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("not an index: %q", s)
	}
	r, g, b := ansiRGB(i)
	return r + g + b
}
