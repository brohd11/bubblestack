package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
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

	const focusedLegend = "panes" // the focused ScrollContainer's border legend
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

// paneKey builds the tea.KeyMsg for a pane binding's first keycode.
func paneKey(t *testing.T, b key.Binding) tea.KeyMsg {
	t.Helper()
	if len(b.Keys()) == 0 {
		t.Fatal("binding carries no keycodes")
	}
	switch k := b.Keys()[0]; k {
	case "shift+up":
		return tea.KeyMsg{Type: tea.KeyShiftUp}
	case "shift+down":
		return tea.KeyMsg{Type: tea.KeyShiftDown}
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	default:
		t.Fatalf("no tea.KeyType mapping for pane key %q", k)
		return tea.KeyMsg{}
	}
}

// TestPaneCycle walks the shift+←/→ cycle over the gote-shaped grid: two stacked
// sidebar panels beside a single editor pane. The cycle runs in flat declaration
// order (down column 0, then column 1), wraps at both ends, and skips panels that
// aren't Focusable.
func TestPaneCycle(t *testing.T) {
	a, b, c := &capturePanel{}, &capturePanel{}, &capturePanel{}
	m := NewModularScreen([][]Slot{
		// The informational panel between b and c must be stepped over, not landed on.
		{{Panel: a, Weight: 1}, {Panel: b, Weight: 1}, {Panel: &shortPanel{h: 1}}},
		{{Panel: c}},
	}, ModularOpts{ColWidths: []int{30, 0}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	next := paneKey(t, core.Keys.PaneNext)
	prev := paneKey(t, core.Keys.PanePrev)

	// Flat order is [a, b, <informational>, c]; the cycle visits 0, 1, 3.
	for i, want := range []int{1, 3, 0, 1} {
		m.Update(sh, next)
		if m.focus != want {
			t.Fatalf("forward step %d: focus = %d, want %d", i+1, m.focus, want)
		}
	}
	for i, want := range []int{0, 3, 1, 0} {
		m.Update(sh, prev)
		if m.focus != want {
			t.Fatalf("backward step %d: focus = %d, want %d", i+1, m.focus, want)
		}
	}
	// Nothing reached the panels: the pane keys are the host's alone.
	for i, p := range []*capturePanel{a, b, c} {
		if len(p.got) != 0 {
			t.Errorf("panel %d saw %d pane keys; they must never reach a panel", i, len(p.got))
		}
	}
}

// TestPaneNavOverUnevenGrid pins the directional moves across a grid whose columns
// differ in length — the tags-screen shape (a sidebar beside two stacked lists).
// It calls neighbor directly because core.Keys.PaneLeft and friends carry no
// keycodes yet (Apple Terminal strips the modifier from shift+↑/↓, so the obvious
// binding would silently fail); this keeps the semantics locked in for whoever
// chooses those keys later.
//
// Three rules make the moves predictable: a vertical step stays inside its column,
// a horizontal step keeps the row (clamped into a shorter column), and the grid's
// edges clamp rather than wrap.
func TestPaneNavOverUnevenGrid(t *testing.T) {
	// col 0: two focusable rows (flat 0, 1); col 1: one focusable row (flat 2).
	a, b, c := &capturePanel{}, &capturePanel{}, &capturePanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: a, Weight: 1}, {Panel: b, Weight: 1}},
		{{Panel: c}},
	}, ModularOpts{ColWidths: []int{30, 0}})

	const (
		up    = -1 // dr
		down  = 1  // dr
		left  = -1 // dc
		right = 1  // dc
	)
	for _, tc := range []struct {
		name         string
		from, dc, dr int
		want         int
	}{
		{"down moves within the column", 0, 0, down, 1},
		{"down at the column's bottom clamps", 1, 0, down, -1},
		{"up at the column's top clamps", 0, 0, up, -1},
		// Row 1 aims past the one-row column and lands on its only row.
		{"right from row 1 clamps onto the short column", 1, right, 0, 2},
		{"right from row 0 keeps the row", 0, right, 0, 2},
		{"right at the last column clamps", 2, right, 0, -1},
		// Coming back lands on row 0: the row was clamped on the way over and there
		// is nothing to restore it from. Asymmetric on purpose.
		{"left returns to the clamped row of col 0", 2, left, 0, 0},
		{"left at the first column clamps", 0, left, 0, -1},
	} {
		if got := m.neighbor(tc.from, tc.dc, tc.dr); got != tc.want {
			t.Errorf("%s: neighbor(%d, %d, %d) = %d, want %d", tc.name, tc.from, tc.dc, tc.dr, got, tc.want)
		}
	}
}

// TestPaneNavEscapesCapturingPanel is the reason the pane keys are matched above
// the capture gate rather than falling out of it: a panel that claims every
// keystroke (an embedded EditorScreen, whose whole job is to type them) would
// otherwise be a trap with no keyboard exit. A reserved key must move focus off it
// while everything else still reaches it.
func TestPaneNavEscapesCapturingPanel(t *testing.T) {
	editor := &capturePanel{capturing: true}
	sidebar := &capturePanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: sidebar}},
		{{Panel: editor}},
	}, ModularOpts{ColWidths: []int{30, 0}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	m.focusSlot(1)
	m.Update(sh, tea.KeyMsg{Type: tea.KeyTab})
	if len(editor.got) != 1 {
		t.Fatal("tab is the panel's key now: a capturing panel must receive it")
	}
	m.Update(sh, paneKey(t, core.Keys.PanePrev))
	if m.focus != 0 {
		t.Fatalf("a pane key must escape a capturing panel, focus stayed at %d", m.focus)
	}
	if len(editor.got) != 1 {
		t.Fatal("the pane key must be consumed by the host, not forwarded")
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

// originPanel records the pane origin the host pushes from View (ScreenPanel's
// shape; EditorScreen is the real consumer).
type originPanel struct {
	x, y int
	has  bool
}

func (p *originPanel) SetSize(int, int)       {}
func (p *originPanel) View(bool) string       { return "x" }
func (p *originPanel) SetPaneOrigin(x, y int) { p.x, p.y, p.has = x, y, true }

// TestModularScreenPushesPaneOrigin: every View publishes each slot's rendered
// rect in absolute cells to PaneOriginer panels — the anchor an embedded editor's
// save-as box uses to cover its own bottom rather than the whole screen.
func TestModularScreenPushesPaneOrigin(t *testing.T) {
	a, b := &originPanel{}, &originPanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: a}},
		{{Panel: b}},
	}, ModularOpts{ColWidths: []int{30, 0}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)
	m.View(sh)
	if !a.has || a.x != 0 || a.y != 0 {
		t.Fatalf("slot 0 origin = (%d,%d) has=%v, want (0,0)", a.x, a.y, a.has)
	}
	if !b.has || b.x != 30 || b.y != 0 {
		t.Fatalf("slot 1 origin = (%d,%d) has=%v, want (30,0)", b.x, b.y, b.has)
	}
}
