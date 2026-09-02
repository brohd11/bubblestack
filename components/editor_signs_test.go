package components

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// TestSignsColumnIndependentOfLineNums is the point of giving signs their own flag: a
// host wanting change markers without line numbers must get them. Riding gutterOn would
// have tied the two together and made ctrl+l blank the markers.
func TestSignsColumn(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha\nbeta")
	first := func() string { return ansi.Strip(strings.Split(s.body(), "\n")[0]) }

	s.SetSigns(map[int]Sign{0: {Text: "+"}})
	if got := first(); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("signs set but not shown should draw nothing, got %q", got)
	}

	s.ShowSigns(true)
	if got := first(); !strings.HasPrefix(got, "+alpha") {
		t.Fatalf("signs on, line numbers off should still draw the marker, got %q", got)
	}

	s.ToggleLineNums()
	if got := first(); !strings.HasPrefix(got, "+1 alpha") {
		t.Fatalf("the sign belongs left of the number, got %q", got)
	}

	// A line with no sign gets a blank cell, not a missing one — the text has to stay
	// in the same column on every row.
	if got := strings.Split(s.body(), "\n")[1]; !strings.HasPrefix(got, " 2 beta") {
		t.Fatalf("an unsigned line should hold the column open, got %q", got)
	}

	s.ToggleSigns()
	if got := first(); !strings.HasPrefix(got, "1 alpha") {
		t.Fatalf("toggling the column off should leave the numbers, got %q", got)
	}
	if s.SignsMode() {
		t.Error("SignsMode should follow the toggle")
	}
}

// TestSignsWrappedRows: a sign names a buffer line, not a display row, so it draws once
// on the line's first row. Repeating it down the continuations would read as several
// changed lines where there is one.
func TestSignsWrappedRows(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent(strings.Repeat("c", 200))
	s.ShowSigns(true)
	s.SetSigns(map[int]Sign{0: {Text: "+"}})
	s.ToggleWrap()

	rows := strings.Split(s.body(), "\n")
	if !strings.HasPrefix(rows[0], "+1 ") {
		t.Fatalf("the first row of a wrapped line carries the sign, got %q", rows[0])
	}
	if !strings.HasPrefix(rows[1], "  ") || strings.HasPrefix(rows[1], "+") {
		t.Fatalf("a continuation row must not repeat the sign, got %q", rows[1])
	}
}

// TestSignsWidthAndClicks pins the geometry the whole column has to keep honest: the
// body narrows by exactly one cell and a click still lands on the character under the
// pointer. Missing the positionAt half of this is invisible until someone clicks.
func TestSignsWidthAndClicks(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdefghij")

	before := s.contentW()
	s.ShowSigns(true)
	if got := s.contentW(); got != before-1 {
		t.Fatalf("the sign column should cost exactly one column: %d → %d", before, got)
	}
	if got, want := s.leftGutterWidth(), 1; got != want {
		t.Fatalf("leftGutterWidth = %d, want %d with signs on and numbers off", got, want)
	}

	s.Update(sh, tea.MouseClickMsg{X: s.leftGutterWidth() + 3, Y: s.titleH(), Button: tea.MouseLeft})
	if s.curX != 3 {
		t.Fatalf("a click three cells into the text should land on column 3, got %d", s.curX)
	}
}

// TestSignsNarrowViewport: below the width that holds both columns the numbers go first
// and the one-cell marker survives, and below that the gutter goes entirely. Whatever it
// decides, gutterText must measure exactly leftGutterWidth — a disagreement shifts every
// row of the body.
func TestSignsNarrowViewport(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abc")
	s.ShowSigns(true)
	s.ToggleLineNums()

	for _, w := range []int{80, 6, 4, 3, 2, 1} {
		s.SetSize(sh, w, 20)
		if got, want := len(s.gutterText(0, true)), s.leftGutterWidth(); got != want {
			t.Errorf("width %d: gutterText measures %d, leftGutterWidth says %d", w, got, want)
		}
	}
}

// TestSignsStyleRepaints: a Sign carries a style rather than pre-rendered ANSI, so the
// color is resolved at render time and a theme switch repaints it.
func TestSignsStyle(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha")
	s.ShowSigns(true)
	s.SetSigns(map[int]Sign{0: {Text: "▌", Style: lipgloss.NewStyle().Foreground(lipgloss.Color("2"))}})

	if got := s.body(); !strings.Contains(got, "\x1b[") {
		t.Fatalf("a styled sign should emit color, got %q", got)
	}
}

func TestNamedSignColumns(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("alpha\nbeta")
	s.SetSignColumnOrder("git", "diagnostics") // ordering may be declared before registration
	s.SetSignColumn("diagnostics", map[int]Sign{0: {Text: "E"}})
	s.SetSignColumn("git", map[int]Sign{0: {Text: "+"}, 1: {Text: "~"}})
	s.ShowSignColumn("git", true)
	s.ShowSignColumn("diagnostics", true)

	if got := ansi.Strip(strings.Split(s.body(), "\n")[0]); !strings.HasPrefix(got, "+Ealpha") {
		t.Fatalf("named columns should render outer-to-inner, got %q", got)
	}
	if got := ansi.Strip(strings.Split(s.body(), "\n")[1]); !strings.HasPrefix(got, "~ beta") {
		t.Fatalf("each missing sign should retain its cell, got %q", got)
	}

	s.ShowSignColumn("git", false)
	if got := ansi.Strip(strings.Split(s.body(), "\n")[0]); !strings.HasPrefix(got, "Ealpha") {
		t.Fatalf("hiding git should leave diagnostics, got %q", got)
	}
	if s.SignColumnMode("git") || !s.SignColumnMode("diagnostics") {
		t.Fatal("named column visibility should be independent")
	}

	s.ShowSignColumn("git", true)
	s.SetSize(sh, 3, 20) // one gutter cell and two text cells: retain the inner column
	if got := s.gutterText(0, true); got != "E" {
		t.Fatalf("narrow panes should retain the inner diagnostics column, got %q", got)
	}
	s.RemoveSignColumn("diagnostics")
	if s.SignColumnMode("diagnostics") {
		t.Fatal("removed columns should no longer be visible")
	}
}

// TestEditSeqTracksEdits pins the debounce key hosts compute against: it must move on an
// edit and hold still otherwise, or a host's "is my diff still current" check is either
// always stale or never.
func TestEditSeq(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha")
	seq := s.EditSeq()
	if s.EditSeq() != seq {
		t.Fatal("reading the sequence must not change it")
	}
	s.insertRunes('x')
	if s.EditSeq() == seq {
		t.Error("an edit should bump the sequence")
	}
}
