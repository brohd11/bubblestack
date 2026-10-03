package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func newLineEdit(width int) (*LineEditScreen, *core.Shared) {
	s := NewLineEdit("name", 0, 0, width, false, nil, nil)
	sh := core.NewShared(nil)
	s.SetSize(sh, 80, 24)
	return s, sh
}

var lineEditValues = map[string]string{
	"empty":            "",
	"short":            "notes.md",
	"fills the window": strings.Repeat("a", 24),
	"overflows":        strings.Repeat("a", 200),
	"with spaces":      strings.Repeat("dir/long name ", 6),
}

// TestLineEditBoxGeometry pins the box to one input row inside its border at exactly
// the covered width, whatever the value, the caret position or the blink phase.
//
// The caret is a real cell textinput renders past the last character, and the wrap
// counts it only while it is reversed (blink-on) — a plain trailing space is dropped
// at end of line. Budget it wrong and the box gains a row on every flash. The test
// forces a color profile because the caret's reverse attribute is what the wrap keys
// on, and the default renderer strips styling with no TTY attached, which hides the
// whole defect.
// lineEditKey drives one key into the screen and returns the action it answers with.
func lineEditKey(s *LineEditScreen, sh *core.Shared, msg tea.KeyPressMsg) core.Action {
	_, act := s.Update(sh, msg)
	return act
}

func TestLineEditBoxGeometry(t *testing.T) {
	const width = 30
	for name, value := range lineEditValues {
		t.Run(name, func(t *testing.T) {
			s, sh := newLineEdit(width)
			s.SetValue(value) // caret at the end: the wrapping case

			// v2 keeps the blink phase inside textinput's unexported virtual
			// cursor, so the two states are driven through SetVirtualCursor:
			// off is the caret-less phase, on the one that renders the block.
			for _, caret := range []bool{false, true} {
				s.input.SetVirtualCursor(caret)
				v := s.View(sh)
				if got := lipgloss.Height(v); got != 3 {
					t.Errorf("height with caret=%v = %d, want 3:\n%s", caret, got, v)
				}
				if got := lipgloss.Width(v); got != width {
					t.Errorf("width with caret=%v = %d, want %d", caret, got, width)
				}
			}

			// Caret off the end renders inline over a character instead — the state
			// that always fit, and it must stay that way.
			lineEditKey(s, sh, keyMsg("left"))
			if got := lipgloss.Height(s.View(sh)); got != 3 {
				t.Errorf("height with the caret moved back = %d, want 3", got)
			}
		})
	}
}

// TestLineEditNarrowBox sweeps the widths a cramped pane can hand the box, including
// the degenerate ones where the prompt alone outgrows the content area: the input line
// is cell-truncated rather than wrapped, so the box holds its one row and its width.
func TestLineEditNarrowBox(t *testing.T) {
	for width := 4; width <= 40; width++ {
		for name, value := range lineEditValues {
			s, sh := newLineEdit(width)
			s.SetValue(value)
			for _, caret := range []bool{false, true} {
				s.input.SetVirtualCursor(caret)
				v := s.View(sh)
				want := max(width, 4) // border and padding: the box has no narrower shape
				if h, w := lipgloss.Height(v), lipgloss.Width(v); h != 3 || w != want {
					t.Errorf("width=%d %s caret=%v: box is %dx%d, want %dx3", width, name, caret, w, h, want)
				}
			}
		}
	}
}

// TestLineEditNoCrumb: the box is a popup over another screen, so it must leave the
// breadcrumb reading as the screen it covers.
func TestLineEditNoCrumb(t *testing.T) {
	s, _ := newLineEdit(30)
	if c, ok := any(s).(core.Crumber); ok {
		t.Errorf("a line edit must contribute no breadcrumb segment, got %q", c.CrumbLabel(false))
	}
}

// TestLineEditHelpToggle: the hint row is opt-in through the constructor. Without it
// the box is one input row inside its border; with it, one row taller.
func TestLineEditHelpToggle(t *testing.T) {
	sh := core.NewShared(nil)
	for _, tc := range []struct {
		help bool
		want int
	}{{false, 3}, {true, 4}} {
		s := NewLineEdit("name", 0, 0, 40, tc.help, nil, nil)
		s.SetSize(sh, 80, 24)
		v := s.View(sh)
		if got := lipgloss.Height(v); got != tc.want {
			t.Errorf("help=%v: height %d, want %d:\n%s", tc.help, got, tc.want, v)
		}
		if got := strings.Contains(v, "done"); got != tc.help {
			t.Errorf("help=%v: hint shown = %v:\n%s", tc.help, got, v)
		}
	}
}

// TestLineEditOnKeyAndTrailer: a claimed key never reaches the input, the trailer sits
// right-aligned on the one input row, and a left click on it reports the cell offset.
func TestLineEditOnKeyAndTrailer(t *testing.T) {
	s, sh := newLineEdit(30)
	var keys []string
	s.OnKey = func(_ *core.Shared, k string) (core.Action, bool) {
		keys = append(keys, k)
		return core.Action{}, k == "up"
	}
	s.Trailer = func(*core.Shared) string { return "2/5 ↑ ↓" }
	clicked := -1
	s.OnTrailerClick = func(_ *core.Shared, off int) core.Action { clicked = off; return core.Action{} }

	lineEditKey(s, sh, keyMsg("up"))
	lineEditKey(s, sh, keyMsg("x"))
	if s.Value() != "x" || len(keys) != 2 {
		t.Fatalf("value = %q keys = %v, want the claimed up to stay out of the input", s.Value(), keys)
	}

	view := s.View(sh)
	if lipgloss.Width(view) != 30 || lipgloss.Height(view) != 3 {
		t.Fatalf("box = %dx%d, want 30x3", lipgloss.Width(view), lipgloss.Height(view))
	}
	row := strings.Split(view, "\n")[1]
	if !strings.HasSuffix(strings.TrimRight(ansi.Strip(row), " │"), "2/5 ↑ ↓") {
		t.Fatalf("input row %q should end with the trailer", ansi.Strip(row))
	}

	// Content spans cells 2..27; the 7-cell trailer starts at 21. The ↓ is offset 6.
	s.Update(sh, tea.MouseClickMsg{X: 27, Y: 1, Button: tea.MouseLeft})
	if clicked != 6 {
		t.Fatalf("trailer click offset = %d, want 6", clicked)
	}
	clicked = -1
	s.Update(sh, tea.MouseClickMsg{X: 5, Y: 1, Button: tea.MouseLeft})
	if clicked != -1 {
		t.Fatal("a click on the input reached OnTrailerClick")
	}
}

// TestLineEditTrailerDroppedWhenNarrow keeps the box one row when the trailer can't fit.
func TestLineEditTrailerDroppedWhenNarrow(t *testing.T) {
	s, sh := newLineEdit(12)
	s.Trailer = func(*core.Shared) string { return "12 matches ↑ ↓" }
	view := s.View(sh)
	if lipgloss.Height(view) != 3 || strings.Contains(view, "↓") {
		t.Fatalf("narrow box should drop the trailer and stay one row:\n%s", view)
	}
}
