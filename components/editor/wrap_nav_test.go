package editor

import (
	"strings"
	"testing"
)

func TestWrappedVerticalNavigation(t *testing.T) {
	for _, nums := range []bool{false, true} {
		s, sh := newEditor(Opts{Wrap: true, LineNumbers: nums})
		s.SetSize(sh, 12, 30)
		w := s.contentW()
		s.setContent(strings.Repeat("a", w*2+2) + "\n\n" + strings.Repeat("b", w+6))
		s.Reveal(Position{0, 5})
		for i, want := range []Position{{0, w + 5}, {0, w*2 + 2}, {1, 0}, {2, 5}, {2, w + 5}} {
			s.key(sh, keyMsg("down"))
			if got := s.CursorPosition(); got != want {
				t.Fatalf("numbers=%v step=%d: got %v, want %v", nums, i, got, want)
			}
		}
		s.key(sh, keyMsg("up"))
		if got := s.CursorPosition(); got != (Position{2, 5}) {
			t.Fatal(got)
		}
		s.key(sh, keyMsg("shift+up"))
		s.key(sh, keyMsg("shift+up"))
		if got := s.selectedText(); got != "\n\n"+strings.Repeat("b", 5) {
			t.Fatalf("selection = %q", got)
		}
		s.key(sh, keyMsg("shift+down"))
		s.key(sh, keyMsg("shift+down"))
		if s.selectionActive() {
			t.Fatal("returning to anchor should clear selection")
		}
	}
}

func TestWrappedVerticalExactWidthAndBoundaries(t *testing.T) {
	s, sh := newEditor(Opts{Wrap: true})
	s.SetSize(sh, 10, 20)
	s.setContent(strings.Repeat("x", 20))
	s.Reveal(Position{0, 7})
	for _, col := range []int{17, 20, 20} {
		s.key(sh, keyMsg("down"))
		if s.curX != col {
			t.Fatalf("down: got %d, want %d", s.curX, col)
		}
	}
	for _, col := range []int{10, 0, 0} {
		s.key(sh, keyMsg("up"))
		if s.curX != col {
			t.Fatalf("up: got %d, want %d", s.curX, col)
		}
	}
	s.Reveal(Position{0, 8})
	s.key(sh, keyMsg("up"))
	if s.curX != 0 {
		t.Fatal("up on first row should reach document start")
	}
}

func TestWrappedVerticalTabs(t *testing.T) {
	cases := []struct {
		text               string
		width, start, want int
		key                string
	}{
		{"abcdef\tghijk", 6, 3, 6, "down"}, // inside tab: clamp to its start
		{"abcde\tghijk", 6, 0, 6, "down"},  // tab crosses boundary: use its end
		{"a\tb", 1, 0, 1, "down"},
		{"a\tb", 1, 1, 2, "down"}, // skip rows with no insertion position
		{"a\tb", 1, 2, 1, "up"},
	}
	for _, tc := range cases {
		s, sh := newEditor(Opts{Wrap: true})
		s.SetSize(sh, tc.width, 30)
		s.setContent(tc.text)
		s.Reveal(Position{0, tc.start})
		s.key(sh, keyMsg(tc.key))
		if s.curX != tc.want {
			t.Fatalf("%+v: got column %d", tc, s.curX)
		}
	}
}

func TestWrappedVerticalGoalReset(t *testing.T) {
	for _, action := range []string{"left", "edit", "undo", "reveal", "click", "resize", "numbers", "wrap", "signs"} {
		t.Run(action, func(t *testing.T) {
			s, sh := newEditor(Opts{Wrap: true})
			s.SetSize(sh, 10, 30)
			s.setContent("abcdefghijk\nabcdefghijklmnopqr")
			s.Reveal(Position{0, 7})
			s.key(sh, keyMsg("down")) // short row clamps column 7 to 1
			switch action {
			case "left":
				s.key(sh, keyMsg("left"))
			case "edit":
				s.key(sh, keyMsg("x"))
			case "undo":
				s.key(sh, keyMsg("x"))
				s.key(sh, keyMsg("up"))
				s.key(sh, keyMsg("ctrl+z"))
			case "reveal":
				s.Reveal(s.CursorPosition())
			case "click":
				s.clickAt(sh, s.insetX()+1, s.insetY()+1)
			case "resize":
				s.SetSize(sh, 9, 30)
			case "numbers":
				s.ToggleLineNums()
			case "wrap":
				s.ToggleWrap()
				s.ToggleWrap()
			case "signs":
				s.ShowSigns(true)
			}
			row := s.wrapRowForCursor()
			want := cellOfCol(s.lines[s.curY], s.curX) - s.wrapRows[row].start
			s.key(sh, keyMsg("down"))
			if s.curY != 1 || s.curX != want {
				t.Fatalf("got %v, want line 1 col %d", s.CursorPosition(), want)
			}
		})
	}
}

func TestWrappedNavigationScrollAndNumberToggle(t *testing.T) {
	s, sh := newEditor(Opts{Wrap: true})
	s.SetSize(sh, 15, 6)
	s.setContent(strings.Repeat(strings.Repeat("x", 35)+"\n", 30))
	s.Reveal(Position{0, 4})
	for i := 0; i < 50; i++ {
		s.key(sh, keyMsg("down"))
		row := s.wrapRowForCursor()
		if row < s.scrY || row >= s.scrY+s.h {
			t.Fatalf("caret row %d outside viewport %d+%d", row, s.scrY, s.h)
		}
	}
	top := s.TopLine()
	s.ToggleLineNums()
	if s.TopLine() != top {
		t.Fatalf("top line moved: %d → %d", top, s.TopLine())
	}
	assertRowGeometry(t, s, "wrapped numbered scrolled")
	s.ToggleLineNums()
	if s.TopLine() != top {
		t.Fatal("hiding numbers moved top line")
	}
	assertRowGeometry(t, s, "wrapped unnumbered scrolled")
}
