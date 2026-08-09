package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// shortPanel renders exactly h rows no matter what height it is allocated —
// the shape a boxed form has inside a ModularScreen: content-sized, leaving its
// Weight allocation half-used for the column's Expand slot to absorb.
type shortPanel struct{ h int }

func (p *shortPanel) SetSize(int, int) {}
func (p *shortPanel) View(bool) string {
	if p.h <= 1 {
		return "x"
	}
	return strings.Repeat("x\n", p.h-1) + "x"
}

// TestSlotAtUsesRenderedLayout is the commit-screen regression: the top panel
// renders 4 rows of its 10-row allocation, the Expand panel below grows into
// the slack, and a click in that grown region must hit the BOTTOM panel — the
// allocation rects would (did) call it the top one.
func TestSlotAtUsesRenderedLayout(t *testing.T) {
	top := &shortPanel{h: 4}
	bottom := NewScrollContainer("files")
	m := NewModularScreen([][]Slot{
		{{Panel: top, Weight: 1}, {Panel: bottom, Weight: 1, Expand: true}},
	}, ModularOpts{})
	sh := core.NewShared(nil) // BodyY 0: slotAt takes body-relative rows directly
	m.SetSize(sh, 80, 20)
	m.View(sh) // rendered layout: top rows 0..3, bottom rows 4..19

	if got := m.slotAt(sh, 5, 1); got != 0 {
		t.Errorf("row 1 is inside the top panel, got slot %d", got)
	}
	if got := m.slotAt(sh, 5, 7); got != 1 {
		t.Errorf("row 7 is inside the grown bottom panel (allocation would say top), got slot %d", got)
	}
}

// TestHostBlurDimsFocusedPanel: when the router hands focus to the output pane
// (SetFocused(false)), the focused panel's render must reflect it — the arg
// View passes is the only focus signal a ScrollContainer has (a nested form
// gets the forwarded SetFocused instead, which is why this regressed unseen).
func TestHostBlurDimsFocusedPanel(t *testing.T) {
	pane := NewScrollContainer("files")
	m := NewModularScreen([][]Slot{{{Panel: pane}}}, ModularOpts{})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	const focusedLegend = "tab next" // the focused ScrollContainer's border legend
	if v := m.View(sh); !strings.Contains(v, focusedLegend) {
		t.Fatal("the focused pane should show its focused legend")
	}
	m.SetFocused(false) // output pane took the keys
	if v := m.View(sh); strings.Contains(v, focusedLegend) {
		t.Fatal("a host-blurred pane must not render as focused")
	}
	m.SetFocused(true)
	if v := m.View(sh); !strings.Contains(v, focusedLegend) {
		t.Fatal("regaining host focus should relight the pane")
	}
}

// capturePanel is a focusable stub that reports Capturing and records the msgs
// routed to it — the shape of a form whose textarea holds focus.
type capturePanel struct {
	capturing bool
	got       []tea.Msg
}

func (p *capturePanel) SetSize(int, int) {}
func (p *capturePanel) View(bool) string { return "x" }
func (p *capturePanel) Focus()           {}
func (p *capturePanel) Blur()            {}
func (p *capturePanel) Focused() bool    { return true }
func (p *capturePanel) Capturing() bool  { return p.capturing }
func (p *capturePanel) UpdatePanel(_ *core.Shared, msg tea.Msg) (core.Action, bool) {
	p.got = append(p.got, msg)
	return core.Action{}, true
}

// TestCaptureFollowsFocus is the commit-screen regression: typing in the form,
// then clicking the file list — the click moves panel focus, and a capturing
// panel that is no longer focused must stop claiming keystrokes (and stop
// gating the router's globals via Filtering). Clicking back restores both.
func TestCaptureFollowsFocus(t *testing.T) {
	form := &capturePanel{capturing: true}
	files := &capturePanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: form, Weight: 1}, {Panel: files, Weight: 1}},
	}, ModularOpts{})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)
	m.View(sh)
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}

	m.Update(sh, key)
	if len(form.got) != 1 {
		t.Fatal("the focused capturing panel should get the keystroke")
	}
	if !m.Filtering() {
		t.Fatal("a capturing focused panel should assert Filtering")
	}

	m.focusSlot(1) // what a click on the file list does
	m.Update(sh, key)
	if len(form.got) != 1 {
		t.Fatal("an unfocused capturing panel must not keep claiming keys")
	}
	if len(files.got) != 1 {
		t.Fatal("the newly focused panel should get the keystroke")
	}
	if m.Filtering() {
		t.Fatal("capture moved away: Filtering must release the router's globals")
	}

	m.focusSlot(0) // clicking the form back
	m.Update(sh, key)
	if len(form.got) != 2 || !m.Filtering() {
		t.Fatal("refocusing the capturing panel should restore capture")
	}
}

// TestFocusSlot covers the exported focus move: a valid Focusable target takes
// focus (blurring the old panel), while out-of-range and non-Focusable indexes
// leave focus where it is.
func TestFocusSlot(t *testing.T) {
	first := &capturePanel{}
	second := &capturePanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: first}, {Panel: &shortPanel{h: 1}}, {Panel: second}},
	}, ModularOpts{})
	if m.focus != 0 {
		t.Fatalf("initial focus should be the first Focusable slot, got %d", m.focus)
	}

	m.FocusSlot(2)
	if m.focus != 2 {
		t.Fatalf("FocusSlot(2) should move focus, got %d", m.focus)
	}

	m.FocusSlot(1) // shortPanel is not Focusable
	if m.focus != 2 {
		t.Fatalf("a non-Focusable target should be a no-op, got %d", m.focus)
	}

	m.FocusSlot(9)
	m.FocusSlot(-1)
	if m.focus != 2 {
		t.Fatalf("out-of-range targets should be a no-op, got %d", m.focus)
	}

	m.FocusSlot(0)
	if m.focus != 0 {
		t.Fatalf("FocusSlot(0) should move focus back, got %d", m.focus)
	}
}
