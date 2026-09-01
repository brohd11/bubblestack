package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

var testHighlightStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

// countingHL records the generic highlighter lifecycle without tying the editor tests
// to any language implementation.
type countingHL struct {
	parses int
	last   string
	lines  []string
}

func (c *countingHL) Parse(doc string) {
	c.parses++
	c.last = doc
	c.lines = strings.Split(doc, "\n")
}

func (c *countingHL) HighlightLine(row int) []Span {
	if row < 0 || row >= len(c.lines) || c.lines[row] == "" {
		return nil
	}
	return []Span{{Text: c.lines[row], Style: testHighlightStyle}}
}

type brokenHL struct{}

func (brokenHL) Parse(string) {}
func (brokenHL) HighlightLine(int) []Span {
	return []Span{{Text: "nope", Style: testHighlightStyle}}
}

func highlighterLanguage(path string) *EditorLanguageConfig {
	if strings.HasSuffix(strings.ToLower(path), ".lit") {
		return &EditorLanguageConfig{NewHighlighter: func() Highlighter { return &countingHL{} }}
	}
	return nil
}

func TestEditorLanguageHighlighterResolution(t *testing.T) {
	plain := NewEditorScreen(EditorOpts{Path: "notes.txt", ResolveLanguage: highlighterLanguage})
	if plain.hl != nil {
		t.Fatal("a nil language profile should leave the editor plain")
	}

	a := NewEditorScreen(EditorOpts{Path: "one.LIT", ResolveLanguage: highlighterLanguage})
	b := NewEditorScreen(EditorOpts{Path: "two.lit", ResolveLanguage: highlighterLanguage})
	if a.hl == nil || b.hl == nil || a.hl == b.hl {
		t.Fatal("each resolved language application should create a fresh highlighter")
	}

	explicit := &countingHL{}
	s := NewEditorScreen(EditorOpts{
		Path: "notes.txt", ResolveLanguage: highlighterLanguage, Highlighter: explicit,
	})
	s.applySaveName("notes.lit")
	if s.hl != explicit {
		t.Fatal("an explicit highlighter must win over a profile and survive a rename")
	}
}

func TestEditorConfiguredHighlightRender(t *testing.T) {
	styled, _ := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: highlighterLanguage})
	styled.setContent("alpha\na\tb")
	plain, _ := newEditor(EditorOpts{})
	plain.setContent("alpha\na\tb")
	styled.curY, plain.curY = 1, 1
	if got := styled.renderLine(0); !strings.Contains(got, testHighlightStyle.Render("alpha")) {
		t.Fatalf("configured row did not render through its highlighter: %q", got)
	}
	for row := range styled.lines {
		styled.curY, styled.curX = row, 0
		plain.curY, plain.curX = row, 0
		got, want := styled.renderLine(row), plain.renderLine(row)
		if lipgloss.Width(got) != lipgloss.Width(want) || strings.Contains(got, "\t") {
			t.Fatalf("row %d broke render geometry: styled %q plain %q", row, got, want)
		}
	}
	if strings.Contains(styled.View(core.NewShared(nil)), "\t") {
		t.Fatal("a highlighted View must never emit a raw tab")
	}
}

func TestEditorHighlighterMismatchFallsBack(t *testing.T) {
	s, _ := newEditor(EditorOpts{Highlighter: brokenHL{}})
	s.setContent("hello world")
	plain, _ := newEditor(EditorOpts{})
	plain.setContent("hello world")
	for _, cur := range []int{0, 5, len("hello world")} {
		s.curX, plain.curX = cur, cur
		if got, want := s.renderLine(0), plain.renderLine(0); got != want {
			t.Fatalf("cursor %d: broken spans rendered %q, want plain %q", cur, got, want)
		}
	}
}

func TestEditorHighlighterReparseCache(t *testing.T) {
	h := &countingHL{}
	s, sh := newEditor(EditorOpts{Highlighter: h})
	s.setContent("alpha\nbeta")
	s.View(sh)
	s.View(sh)
	if h.parses != 1 {
		t.Fatalf("unchanged frames parsed %d times, want 1", h.parses)
	}
	typeRunes(s, 'x')
	s.View(sh)
	if h.parses != 2 || h.last != buffer(s) {
		t.Fatalf("edit parse count/content = %d/%q, want 2/%q", h.parses, h.last, buffer(s))
	}
}

func TestEditorUnfocusedSuppressesHighlight(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{Highlighter: &countingHL{}})
	s.setContent("alpha")
	s.SetFocused(false)
	dark := s.View(sh)
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	if !strings.Contains(dark, muted.Render("alpha")) || strings.Contains(dark, testHighlightStyle.Render("alpha")) {
		t.Fatalf("unfocused render did not replace syntax styling with muted text: %q", dark)
	}
}
