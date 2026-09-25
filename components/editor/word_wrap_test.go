package editor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestWordWrapRows(t *testing.T) {
	cases := []struct {
		name, text string
		width      int
		rows       []string
	}{
		{"period", "the end.", 7, []string{"the ", "end."}},
		{"quotes", "say \"hi!\"", 7, []string{"say ", "\"hi!\""}},
		{"brackets", "(one) [two]", 10, []string{"(one) ", "[two]"}},
		{"hyphen", "x two-part", 8, []string{"x ", "two-part", ""}},
		{"path", "x a/b.txt", 7, []string{"x ", "a/b.txt", ""}},
		{"URL", "a https://x.y", 11, []string{"a ", "https://x.y", ""}},
		{"oversized word", "a abcdefghijkl", 5, []string{"a ", "abcde", "fghij", "kl"}},
		{"exact word", "abc def", 3, []string{"abc", " ", "def", ""}},
		{"repeated spaces", "aa  bbbb", 5, []string{"aa  ", "bbbb"}},
		{"tab", "a\tbbbb", 6, []string{"a    ", "bbbb"}},
		{"indentation", "  one two", 6, []string{"  one ", "two"}},
		{"whitespace only", " \t  ", 3, []string{"   ", "   ", " "}},
		{"nonbreaking space", "x a\u00a0b", 4, []string{"x ", "a\u00a0b"}},
		{"one cell", "a\tb", 1, []string{"a", " ", " ", " ", " ", "b", ""}},
		{"empty", "", 4, []string{""}},
		{"short final row", "a bbbb", 3, []string{"a ", "bbb", "b"}},
		{"full final row", "a abc", 3, []string{"a ", "abc", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, sh := newEditor(Opts{Wrap: true})
			s.SetSize(sh, tc.width, 30)
			s.setContent(tc.text)
			s.rebuildWrapRows()
			disp := expandLine(s.lines[0])
			var got []string
			end := 0
			for _, r := range s.wrapRows {
				if r.start != end || r.end-r.start > tc.width {
					t.Fatalf("invalid row geometry: %+v", s.wrapRows)
				}
				got = append(got, string(disp[r.start:r.end]))
				end = r.end
			}
			if !reflect.DeepEqual(got, tc.rows) {
				t.Fatalf("rows = %q, want %q", got, tc.rows)
			}
			if strings.Join(got, "") != string(disp) || s.Text() != tc.text {
				t.Fatal("wrapping lost or changed text")
			}
			s.Reveal(Position{0, len(s.lines[0])})
			if row := s.wrapRowForCursor(); row != len(tc.rows)-1 {
				t.Fatalf("end caret on row %d, want %d", row, len(tc.rows)-1)
			}
			if row := caretRow(s); row != len(tc.rows)-1 {
				t.Fatalf("rendered caret on row %d, want %d", row, len(tc.rows)-1)
			}
			if count := strings.Count(s.body(), "\x1b[7m"); count != 1 {
				t.Fatalf("rendered %d carets, want exactly one", count)
			}
			assertRowGeometry(t, s, tc.name)
		})
	}
}

func TestWordWrapNavigationAndPadding(t *testing.T) {
	s, sh := newEditor(Opts{Wrap: true})
	s.SetSize(sh, 7, 20)
	s.setContent("the end. next") // "the ", "end. ", "next"
	s.Reveal(Position{0, 2})
	for _, want := range []int{6, 11} {
		s.key(sh, keyMsg("down"))
		if s.curX != want {
			t.Fatalf("down at col %d, want %d", s.curX, want)
		}
	}
	s.key(sh, keyMsg("shift+up"))
	if got := s.selectedText(); got != "d. ne" {
		t.Fatalf("selection = %q", got)
	}
	s.key(sh, keyMsg("shift+up"))
	if got := s.selectedText(); got != "e end. ne" {
		t.Fatalf("selection = %q", got)
	}

	// A click in unused cells must stay on its row, including a final row's EOL.
	for row, want := range []int{3, 8, 13} {
		s.clickAt(sh, s.insetX()+6, s.insetY()+row)
		if s.curX != want || s.wrapRowForCursor() != row {
			t.Fatalf("padding row %d: cursor %v on row %d", row, s.CursorPosition(), s.wrapRowForCursor())
		}
	}
	s.startDrag(sh, s.insetX()+1, s.insetY())
	s.extendDrag(sh, s.insetX()+6, s.insetY()+1)
	if got := s.selectedText(); got != "he end. " {
		t.Fatalf("drag in padding selected %q", got)
	}

	s.Reveal(Position{0, 12}) // column 3 on the final row
	s.key(sh, keyMsg("up"))
	s.key(sh, keyMsg("up"))
	if s.curX != 3 {
		t.Fatalf("up should clamp to the first row's last cell, got %d", s.curX)
	}
	s.key(sh, keyMsg("down"))
	if s.curX != 7 {
		t.Fatalf("desired column lost after short row: %d", s.curX)
	}
}

func TestWordWrapKeepsGoalAcrossShortRows(t *testing.T) {
	s, sh := newEditor(Opts{Wrap: true})
	s.SetSize(sh, 7, 20)
	s.setContent("abcdef ghijk lmnopqr")
	s.Reveal(Position{0, 6})
	s.key(sh, keyMsg("down"))
	if s.curX != 12 {
		t.Fatalf("short row should clamp to column 5, got %v", s.CursorPosition())
	}
	s.key(sh, keyMsg("down"))
	if s.curX != 19 {
		t.Fatalf("full row should restore column 6, got %v", s.CursorPosition())
	}
}

func TestWordWrapRenderingAndReflow(t *testing.T) {
	for _, highlighted := range []bool{false, true} {
		for _, numbers := range []bool{false, true} {
			opts := Opts{Wrap: true, LineNumbers: numbers}
			if highlighted {
				opts.Highlighter = &countingHL{}
			}
			s, sh := newEditor(opts)
			s.setContent("the end.\n" + strings.Repeat("a few words with punctuation. ", 30))
			s.ShowSigns(true)
			s.SetSigns(map[int]Sign{0: {Text: "+"}})
			// Seven text cells, plus signs, optional numbers, and the scrollbar.
			width := 9
			if numbers {
				width += 2
			}
			s.SetSize(sh, width, 6)
			s.Reveal(Position{0, 7})
			if s.contentW() != 7 {
				t.Fatalf("fixture width = %d", s.contentW())
			}
			if row := s.wrapRowForCursor(); row != 1 {
				t.Fatalf("period should stay with end, on row 1, got %d", row)
			}
			if row := ansi.Strip(s.renderWrappedRow(1)); !strings.HasSuffix(row, "end.") {
				t.Fatalf("rendered continuation = %q", row)
			}
			assertRowGeometry(t, s, "word wrapped")
			for i := 0; i < 25; i++ {
				s.key(sh, keyMsg("down"))
				row := s.wrapRowForCursor()
				if row < s.scrY || row >= s.scrY+s.h {
					t.Fatal("caret scrolled out of view")
				}
			}
			s.ToggleLineNums()
			s.SetSize(sh, width-2, 6)
			s.key(sh, keyMsg("down"))
			assertRowGeometry(t, s, "word wrap resized and numbers toggled")
		}
	}
}
