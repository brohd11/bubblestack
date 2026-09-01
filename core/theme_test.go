package core

import (
	"reflect"
	"sort"
	"testing"
)

// restoreTheme snaps the active theme back after a test mutates the global palette.
func restoreTheme(t *testing.T) {
	prev := CurrentTheme()
	t.Cleanup(func() { SetTheme(prev) })
}

func TestSetThemeKnownUnknown(t *testing.T) {
	restoreTheme(t)
	if !SetTheme("godot") {
		t.Fatal("SetTheme should accept a known preset")
	}
	if CurrentTheme() != "godot" {
		t.Fatalf("CurrentTheme should track the switch, got %q", CurrentTheme())
	}
	if SetTheme("does-not-exist") {
		t.Error("SetTheme should reject an unknown preset")
	}
	if CurrentTheme() != "godot" {
		t.Errorf("a rejected SetTheme should leave the theme untouched, got %q", CurrentTheme())
	}
}

func TestRegisterThemeAndNames(t *testing.T) {
	restoreTheme(t)
	onFocused := Color{Light: 5, Dark: 5}
	RegisterTheme(Theme{Name: "zz-test", Muted: Color{Light: 1, Dark: 1}, Log: Color{Light: 2, Dark: 2}, Border: Color{Light: 3, Dark: 3}, Focused: Color{Light: 4, Dark: 4}, OnFocused: &onFocused})
	if !SetTheme("zz-test") {
		t.Fatal("a registered theme should be resolvable by SetTheme")
	}
	names := ThemeNames()
	if !sort.StringsAreSorted(names) {
		t.Errorf("ThemeNames should be sorted, got %v", names)
	}
	found := false
	for _, n := range names {
		if n == "zz-test" {
			found = true
		}
	}
	if !found {
		t.Error("ThemeNames should include a newly registered theme")
	}
}

func TestApplyThemeBroadcasts(t *testing.T) {
	restoreTheme(t)
	act := ApplyTheme("amber")
	if CurrentTheme() != "amber" {
		t.Fatalf("ApplyTheme should switch the active theme, got %q", CurrentTheme())
	}
	if !reflect.DeepEqual(act, PropagateAll(MsgThemeChanged{})) {
		t.Errorf("ApplyTheme should broadcast MsgThemeChanged, got %+v", act)
	}
}

func TestApplyThemeOnFocusedFallback(t *testing.T) {
	restoreTheme(t)
	// A theme leaving OnFocused empty falls back to defaultOnFocused.
	RegisterTheme(Theme{Name: "no-onfocused", Muted: Color{Light: 1, Dark: 1}, Log: Color{Light: 2, Dark: 2}, Border: Color{Light: 3, Dark: 3}, Focused: Color{Light: 4, Dark: 4}})
	SetTheme("no-onfocused")
	if OnFocusedColor != Resolve(defaultOnFocused) {
		t.Errorf("empty OnFocused should fall back to defaultOnFocused, got %v", OnFocusedColor)
	}
}

// TestMarkdownAccent: a theme may borrow another's accent for markdown, which is how
// mono keeps a rendered page legible — its own accent is the terminal's own extreme
// and would make headings, code spans and links read as body text.
func TestMarkdownAccent(t *testing.T) {
	restoreTheme(t)

	// A theme without MarkdownFrom uses its own accent.
	SetTheme("godot")
	if got := Resolve(MarkdownAccent()); !reflect.DeepEqual(got, FocusedColor) {
		t.Errorf("godot should use its own accent, got %v want %v", got, FocusedColor)
	}

	// mono borrows lipgloss's, and does NOT change its own FocusedColor doing so —
	// only the markdown page is affected, not selection or the title bar.
	SetTheme("mono")
	if got := MarkdownAccent(); !reflect.DeepEqual(got, themes["lipgloss"].Focused) {
		t.Errorf("mono should borrow lipgloss's accent, got %v", got)
	}
	if reflect.DeepEqual(FocusedColor, Resolve(MarkdownAccent())) {
		t.Error("borrowing a markdown accent must not move the theme's own FocusedColor")
	}

	// A MarkdownFrom naming nothing registered falls back rather than answering nil:
	// a typo should dim a page, not blank it.
	RegisterTheme(Theme{
		Name: "zz-badref", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder,
		Focused: Color{Light: 9, Dark: 9}, MarkdownFrom: "no-such-theme",
	})
	SetTheme("zz-badref")
	if got := Resolve(MarkdownAccent()); !reflect.DeepEqual(got, FocusedColor) {
		t.Errorf("an unresolvable MarkdownFrom should fall back to the active accent, got %v", got)
	}
}
