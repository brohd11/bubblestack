package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func newLineEdit(width int) (*LineEditScreen, *core.Shared) {
	s := NewLineEdit("name", 0, 0, width, nil, nil)
	s.Help = []key.Binding{} // the slim shape both narrow callers use
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

// TestLineEditHelpRow keeps the default hint row inside the box (one row taller) for
// callers wide enough to hold it — save-as's shape.
func TestLineEditHelpRow(t *testing.T) {
	s, sh := newLineEdit(80)
	s.Help = nil // default enter/esc hints
	s.SetValue(strings.Repeat("a", 200))
	if got := lipgloss.Height(s.View(sh)); got != 4 {
		t.Errorf("height with the default help row = %d, want 4", got)
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
