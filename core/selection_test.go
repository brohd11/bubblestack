package core

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// bgSeq is colorSeq for a background.
func bgSeq(t *testing.T, c color.Color) string {
	t.Helper()
	return styleSeq(t, lipgloss.NewStyle().Background(c))
}

// barSeq is the one SGR lipgloss emits for fg text on the selection bar.
func barSeq(t *testing.T, fg color.Color) string {
	t.Helper()
	return styleSeq(t, lipgloss.NewStyle().Foreground(fg).Background(SelectionColor))
}

func styleSeq(t *testing.T, s lipgloss.Style) string {
	t.Helper()
	rendered := s.Render("x")
	seq, _, ok := strings.Cut(rendered, "x")
	if !ok || seq == "" {
		t.Fatal("no escape sequence")
	}
	return seq
}

// renderCompactWith renders the row at index with the cursor on cursor.
func renderCompactWith(t *testing.T, d CompactDelegate, cursor, index, width int, items ...list.Item) string {
	t.Helper()
	l := NewCompactList(items, "")
	l.SetSize(width, 10)
	l.Select(cursor)
	var b strings.Builder
	d.Render(&b, l, index, items[index])
	return b.String()
}

// TestCompactSelectBackground: the bar replaces the border glyph with the selection
// background across the full row, keeps the text where it was, and leaves other rows alone.
func TestCompactSelectBackground(t *testing.T) {
	items := []list.Item{
		marqueeItem{title: "notes.md", suffix: "docs/"},
		markedItem{marqueeItem: marqueeItem{title: "todo.md"}, mark: "(*)"},
	}
	bar := CompactDelegate{Selection: SelectionOpts{Style: SelectBackground}}
	for i := range items {
		border := renderCompactWith(t, CompactDelegate{}, i, i, 30, items...)
		got := renderCompactWith(t, bar, i, i, 30, items...)
		if strings.Contains(got, "│") {
			t.Fatalf("row %d: a background selection must draw no border glyph: %q", i, got)
		}
		if !strings.Contains(got, bgSeq(t, SelectionColor)) {
			t.Fatalf("row %d: the selected row should carry the selection background", i)
		}
		if w := lipgloss.Width(got); w != 30 {
			t.Fatalf("row %d: the bar should span the list width, got %d", i, w)
		}
		if strings.TrimRight(ansi.Strip(got), " ") != strings.TrimRight(strings.Replace(ansi.Strip(border), "│", " ", 1), " ") {
			t.Fatalf("row %d: the text must not move: %q vs %q", i, ansi.Strip(got), ansi.Strip(border))
		}
		if !strings.Contains(got, barSeq(t, FocusedColor)) {
			t.Fatalf("row %d: the bar keeps the accent text by default", i)
		}
		other := 1 - i
		if renderCompactWith(t, bar, i, other, 30, items...) != renderCompactWith(t, CompactDelegate{}, i, other, 30, items...) {
			t.Fatalf("row %d: an unselected row must render as before", other)
		}
	}
}

// TestCompactNoAccent: NoAccent draws the selected text in the normal title color, under
// either style; the border style keeps its glyph.
func TestCompactNoAccent(t *testing.T) {
	items := []list.Item{marqueeItem{title: "notes.md"}}
	normal := list.NewDefaultItemStyles(isDark).NormalTitle.GetForeground()
	for _, style := range []SelectionStyle{SelectBorder, SelectBackground} {
		d := CompactDelegate{Selection: SelectionOpts{Style: style, NoAccent: true}}
		got := renderCompactWith(t, d, 0, 0, 30, items...)
		want := colorSeq(t, normal)
		if style == SelectBackground {
			want = barSeq(t, normal)
		}
		if !strings.Contains(got, want) {
			t.Fatalf("style %d: NoAccent should use the normal title foreground", style)
		}
		if style == SelectBorder && !strings.Contains(got, "│") {
			t.Fatal("NoAccent must keep the border marker")
		}
	}
	// A ColorItem keeps its own color under the cursor, as a KeepColorItem would.
	colored := []list.Item{coloredItem{marqueeItem: marqueeItem{title: "notes.md"}, color: lipgloss.Color("12")}}
	d := CompactDelegate{Selection: SelectionOpts{Style: SelectBackground, NoAccent: true}}
	if got := renderCompactWith(t, d, 0, 0, 30, colored...); !strings.Contains(got, barSeq(t, lipgloss.Color("12"))) {
		t.Fatal("NoAccent should keep a ColorItem's own color")
	}
}

// TestCompactHidden: a hidden selection renders the cursor row exactly as a normal row.
func TestCompactHidden(t *testing.T) {
	items := []list.Item{marqueeItem{title: "notes.md"}, marqueeItem{title: "todo.md"}}
	hidden := true
	for _, style := range []SelectionStyle{SelectBorder, SelectBackground} {
		d := CompactDelegate{Selection: SelectionOpts{Style: style}, Hidden: &hidden}
		normal := renderCompactWith(t, CompactDelegate{}, 1, 0, 30, items...)
		if got := renderCompactWith(t, d, 0, 0, 30, items...); got != normal {
			t.Fatalf("style %d: hidden cursor row = %q, want the normal row %q", style, got, normal)
		}
	}
	shown := false
	d := CompactDelegate{Hidden: &shown}
	if renderCompactWith(t, d, 0, 0, 30, items...) != renderCompactWith(t, CompactDelegate{}, 0, 0, 30, items...) {
		t.Fatal("a false Hidden must not change the selected row")
	}
}

// TestColorDelegateSelection: the three-row delegate honors the same options.
func TestColorDelegateSelection(t *testing.T) {
	items := []list.Item{describedItem{title: "notes.md", desc: "docs"}, describedItem{title: "todo.md", desc: "docs"}}
	l := NewSelectList(items, "")
	l.SetSize(30, 12)
	render := func(d ColorDelegate, cursor, index int) string {
		l.Select(cursor)
		var b strings.Builder
		d.Render(&b, l, index, items[index])
		return b.String()
	}
	base := NewDelegate()

	bar := NewDelegate()
	bar.Selection.Style = SelectBackground
	got := render(bar, 0, 0)
	if strings.Contains(got, "│") || !strings.Contains(got, bgSeq(t, SelectionColor)) {
		t.Fatalf("background selection should swap the border for the bar: %q", got)
	}
	for i, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w != 30 {
			t.Fatalf("bar line %d is %d wide, want 30", i, w)
		}
	}

	hidden := true
	hide := NewDelegate()
	hide.Hidden = &hidden
	if render(hide, 0, 0) != render(base, 1, 0) {
		t.Fatal("a hidden selection should render the cursor row as a normal row")
	}

	plain := NewDelegate()
	plain.Selection.NoAccent = true
	normal := base.Styles.NormalTitle.GetForeground()
	if got := render(plain, 0, 0); !strings.Contains(got, colorSeq(t, normal)) || !strings.Contains(got, "│") {
		t.Fatalf("NoAccent should keep the border and use the normal title color: %q", got)
	}
}
