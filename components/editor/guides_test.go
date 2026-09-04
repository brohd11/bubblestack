package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestEditorIndentGuidesDefaultOff(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.setContent("    value")
	s.SetFocused(false)
	if got := ansi.Strip(s.renderLine(0)); strings.ContainsRune(got, editorIndentGuide) || got != "    value" {
		t.Fatalf("default rendering = %q, want unchanged leading spaces", got)
	}
}

func TestEditorIndentGuidesUseLiveUnit(t *testing.T) {
	resolver := func(string) *LanguageConfig { return &LanguageConfig{IndentSpaces: 2} }
	s, _ := newEditor(Opts{ResolveLanguage: resolver, IndentGuides: true})
	s.setContent("    value\n   partial\n\t tabbed")
	s.SetFocused(false)

	for row, want := range []string{"\u2502 \u2502 value", "\u2502  partial", "\u2502 \u2502  tabbed"} {
		if got := ansi.Strip(s.renderLine(row)); got != want {
			t.Errorf("row %d guides = %q, want %q", row, got, want)
		}
	}

	// auto (2 spaces) -> tab changes the guide interval without touching the text.
	s.cycleIndentMode()
	if got, want := ansi.Strip(s.renderLine(0)), "\u2502   value"; got != want {
		t.Fatalf("tab-mode guides = %q, want %q", got, want)
	}
}

func TestEditorIndentGuidesFollowLanguageAndHighlighting(t *testing.T) {
	resolver := func(path string) *LanguageConfig {
		spaces := 4
		if strings.HasSuffix(path, ".two") {
			spaces = 2
		}
		return &LanguageConfig{IndentSpaces: spaces}
	}
	hl := &countingHL{}
	s, _ := newEditor(Opts{
		Path: "file.two", ResolveLanguage: resolver, Highlighter: hl, IndentGuides: true,
	})
	s.setContent("    value\ntail")
	s.curY = 1 // keep the cursor from replacing the first guide cell
	if got, want := ansi.Strip(s.renderLine(0)), "\u2502 \u2502 value"; got != want {
		t.Fatalf("two-space highlighted guides = %q, want %q", got, want)
	}
	s.SetPath("file.four")
	if got, want := ansi.Strip(s.renderLine(0)), "\u2502   value"; got != want {
		t.Fatalf("four-space guides after language change = %q, want %q", got, want)
	}
}

func TestEditorIndentGuidesKeepWindowGeometry(t *testing.T) {
	s, _ := newEditor(Opts{
		IndentGuides:    true,
		ResolveLanguage: func(string) *LanguageConfig { return &LanguageConfig{IndentSpaces: 2} },
	})
	s.SetSize(nil, 8, 4)
	s.setContent("      value")
	s.curY, s.curX = 0, len(s.lines[0])
	s.scrX = 1
	if got, want := ansi.Strip(s.renderLine(0)), " \u2502 \u2502 va~"; got != want {
		t.Fatalf("horizontally windowed guides = %q, want %q", got, want)
	}
	s.ToggleWrap()
	for _, row := range strings.Split(s.body(), "\n") {
		if w := ansi.StringWidth(row); w > s.w {
			t.Fatalf("wrapped guide row width = %d, want <= %d: %q", w, s.w, ansi.Strip(row))
		}
	}
}
