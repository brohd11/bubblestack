package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// shortPanel renders exactly h rows no matter what height it is allocated —
// the shape a boxed form has inside a ModularScreen: content-sized, leaving its
// Weight allocation half-used for the column's ExpandV slot to absorb.
type shortPanel struct{ h int }

func (p *shortPanel) SetSize(int, int) {}
func (p *shortPanel) View(bool) string {
	if p.h <= 1 {
		return "x"
	}
	return strings.Repeat("x\n", p.h-1) + "x"
}

// TestSlotAtUsesRenderedLayout is the commit-screen regression: the top panel
// renders 4 rows of its 10-row allocation, the ExpandV panel below grows into
// the slack, and a click in that grown region must hit the BOTTOM panel — the
// allocation rects would (did) call it the top one.
func TestSlotAtUsesRenderedLayout(t *testing.T) {
	top := &shortPanel{h: 4}
	bottom := NewScrollContainer("files")
	m := NewModularScreen([][]Slot{
		{{Panel: top, Weight: 1}, {Panel: bottom, Weight: 1, ExpandV: true}},
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

func TestMouseDragStaysWithOriginatingPane(t *testing.T) {
	for name, button := range map[string]tea.MouseButton{
		"left": tea.MouseButtonLeft, "right": tea.MouseButtonRight,
	} {
		t.Run(name, func(t *testing.T) {
			left, right := &capturePanel{}, &capturePanel{}
			m := NewModularScreen([][]Slot{
				{{Panel: left, ExpandH: true}},
				{{Panel: right, ExpandH: true}},
			}, ModularOpts{ColWidths: []int{10, 10}})
			sh := core.NewShared(nil)
			m.SetSize(sh, 20, 5)
			m.View(sh)

			m.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: 0, Y: 0})
			m.Update(sh, tea.MouseMsg{Action: tea.MouseActionMotion, Button: button, X: 15, Y: 0})
			m.Update(sh, tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonNone, X: 16, Y: 0})
			if len(left.got) != 3 {
				t.Fatalf("originating pane received %d events, want press/motion/release", len(left.got))
			}
			if len(right.got) != 0 {
				t.Fatalf("the pane crossed during a drag received %d events, want none", len(right.got))
			}
			motion := left.got[1].(tea.MouseMsg)
			if motion.X != 15 {
				t.Fatalf("motion x = %d, want coordinates relative to the originating pane", motion.X)
			}
			if m.mouseSlot != -1 {
				t.Fatal("release should clear the pane gesture owner")
			}
		})
	}
}

// paneKey builds the tea.KeyMsg for a pane binding's first keycode. Only PaneNext
// carries one today; the rest are keyless (see core.Keys.PanePrev), and a test wanting
// those drives cycleFocus or neighbor directly. Binding one is what makes the default
// below fire, and adding its tea.KeyType here is the whole fix.
func paneKey(t *testing.T, b key.Binding) tea.KeyMsg {
	t.Helper()
	if len(b.Keys()) == 0 {
		t.Fatal("binding carries no keycodes")
	}
	switch k := b.Keys()[0]; k {
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		t.Fatalf("no tea.KeyType mapping for pane key %q", k)
		return tea.KeyMsg{}
	}
}

// TestPaneCycle walks the pane cycle over the gote-shaped grid: two stacked sidebar
// panels beside a single editor pane. The cycle runs in flat declaration order (down
// column 0, then column 1), wraps at both ends, and skips panels that aren't Focusable.
//
// Forward goes through the real key; backward calls cycleFocus directly, because
// PanePrev carries no keycodes now that the shift+arrows are the editor's selection keys
// (core.Keys.PanePrev). The backward step is still live code, and wrapping is what makes
// the forward-only binding sufficient — both are worth pinning either way.
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

	// Flat order is [a, b, <informational>, c]; the cycle visits 0, 1, 3.
	for i, want := range []int{1, 3, 0, 1} {
		m.Update(sh, next)
		if m.focus != want {
			t.Fatalf("forward step %d: focus = %d, want %d", i+1, m.focus, want)
		}
	}
	for i, want := range []int{0, 3, 1, 0} {
		m.cycleFocus(-1)
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

// TestPaneCycleOnShiftTab pins shift+tab as the whole pane-navigation scheme: it is the
// ONLY keycode on PaneNext, so a grid is navigable through it alone, and it is not a
// capture-aware alias — it takes the key off a capturing panel too, even though that
// panel would otherwise use shift+tab itself (an embedded editor types a tab with it, a
// form moves to the previous field).
func TestPaneCycleOnShiftTab(t *testing.T) {
	if got := core.Keys.PaneNext.Keys(); len(got) != 1 || got[0] != "shift+tab" {
		// The rest of the pane binds are keyless, so this one key is the only way round
		// a grid from the keyboard, and PaneHint's bar entry reads off it.
		t.Fatalf("PaneNext must carry shift+tab and nothing else, got %q", got)
	}

	editor := &capturePanel{capturing: true}
	sidebar, aside := &capturePanel{}, &capturePanel{}
	m := NewModularScreen([][]Slot{
		{{Panel: sidebar}},
		{{Panel: editor}},
		{{Panel: aside}},
	}, ModularOpts{ColWidths: []int{30, 0, 20}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	tab := tea.KeyMsg{Type: tea.KeyShiftTab}
	for i, want := range []int{1, 2, 0} {
		m.Update(sh, tab)
		if m.focus != want {
			t.Fatalf("shift+tab step %d: focus = %d, want %d", i+1, m.focus, want)
		}
	}
	// Step 1 above left the capturing editor; nothing was forwarded on the way past.
	if len(editor.got) != 0 {
		t.Fatalf("capturing panel saw %d shift+tab presses; the host consumes them", len(editor.got))
	}
	// The editor still owns everything that isn't a pane key.
	m.focusSlot(1)
	m.Update(sh, tea.KeyMsg{Type: tea.KeyTab})
	if len(editor.got) != 1 {
		t.Fatal("bare tab must still reach the capturing panel")
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
	// Two panes, so the forward cycle wraps off the editor straight back to the sidebar —
	// which is exactly why a forward-only binding is enough to escape.
	m.Update(sh, paneKey(t, core.Keys.PaneNext))
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

// narrowPanel renders a fixed-width block regardless of its allocation — the shape
// an EditorScreen has on a short document, whose body is only as wide as its longest
// line and leaves the column a ragged right edge.
type narrowPanel struct{ w, h int }

func (p *narrowPanel) SetSize(int, int) {}
func (p *narrowPanel) View(bool) string {
	rows := make([]string, p.h)
	for i := range rows {
		rows[i] = strings.Repeat("x", p.w)
	}
	return strings.Join(rows, "\n")
}

// TestExpandHFillsColumnWidth: ExpandH squares a narrow-rendering panel off against
// its allocated column, so a two-column layout has a straight seam instead of one
// that follows the content. Without the flag the panel renders exactly as it drew.
func TestExpandHFillsColumnWidth(t *testing.T) {
	sh := core.NewShared(nil)

	for _, tc := range []struct {
		name    string
		expandH bool
		want    int
	}{
		{"off", false, 10},
		{"on", true, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			narrow := &narrowPanel{w: 10, h: 5}
			m := NewModularScreen([][]Slot{
				{{Panel: narrow, Weight: 1, ExpandH: tc.expandH}},
			}, ModularOpts{ColWidths: []int{40}})
			m.SetSize(sh, 80, 20)
			for _, line := range strings.Split(m.View(sh), "\n") {
				if got := lipgloss.Width(line); got != tc.want {
					t.Fatalf("ExpandH=%v: line width %d, want %d (%q)", tc.expandH, got, tc.want, line)
				}
			}
		})
	}
}

// TestExpandHKeepsWideContent: the padding can only add. A panel that renders WIDER
// than its allocation (its own bug, but a real one) must not be truncated by the
// fill — losing content is strictly worse than an overhang.
func TestExpandHKeepsWideContent(t *testing.T) {
	wide := &narrowPanel{w: 60, h: 2}
	m := NewModularScreen([][]Slot{
		{{Panel: wide, Weight: 1, ExpandH: true}},
	}, ModularOpts{ColWidths: []int{20}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)
	for _, line := range strings.Split(m.View(sh), "\n") {
		if got := lipgloss.Width(line); got != 60 {
			t.Fatalf("content wider than the column must survive, got width %d", got)
		}
	}
}

// TestExpandHWithExpandV: the two axes compose — the vertical pass re-renders the
// grown slot, and the horizontal fill runs after it, so a slot with both ends up
// filling its column in both directions.
func TestExpandHWithExpandV(t *testing.T) {
	top := &shortPanel{h: 4}
	bottom := &narrowPanel{w: 8, h: 3}
	m := NewModularScreen([][]Slot{
		{{Panel: top, Weight: 1}, {Panel: bottom, Weight: 1, ExpandV: true, ExpandH: true}},
	}, ModularOpts{ColWidths: []int{30}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	rows := strings.Split(m.View(sh), "\n")
	for i, line := range rows[4:] { // past the short top panel
		if got := lipgloss.Width(line); got != 30 {
			t.Fatalf("row %d of the grown slot should fill the column, got %d (%q)", i+4, got, line)
		}
	}
}

// focusPanel is a Focusable stub that implements FocusNotifier, recording how many
// times it was notified and handing back a distinguishable cmd.
type focusPanel struct {
	name     string
	focused  bool
	notified int
}

func (p *focusPanel) SetSize(int, int) {}
func (p *focusPanel) View(bool) string { return "x" }
func (p *focusPanel) Focus()           { p.focused = true }
func (p *focusPanel) Blur()            { p.focused = false }
func (p *focusPanel) Focused() bool    { return p.focused }
func (p *focusPanel) UpdatePanel(*core.Shared, tea.Msg) (core.Action, bool) {
	return core.Action{}, true
}

// OnFocus asserts the ordering the interface promises: Focus() has already run, so a
// panel deciding whether to start work can trust its own Focused().
func (p *focusPanel) OnFocus() tea.Cmd {
	p.notified++
	if !p.focused {
		panic("OnFocus must be called after Focus()")
	}
	name := p.name
	return func() tea.Msg { return name }
}

// cmdName runs a focus cmd and returns the panel name it identifies ("" for a nil cmd).
func cmdName(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	if cmd == nil {
		return ""
	}
	msg := cmd()
	s, ok := msg.(string)
	if !ok {
		t.Fatalf("focus cmd delivered %T, want the panel's name", msg)
	}
	return s
}

// TestFocusNotifierOnPaneKey is the case the hook exists for: the pane-navigation key
// is consumed by the host and never reaches a panel, so the Action it returns is the
// newly focused panel's only chance to start work off the focus change.
func TestFocusNotifierOnPaneKey(t *testing.T) {
	a, b := &focusPanel{name: "a"}, &focusPanel{name: "b"}
	m := NewModularScreen([][]Slot{{{Panel: a}, {Panel: b}}}, ModularOpts{})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	_, act := m.Update(sh, paneKey(t, core.Keys.PaneNext))
	if m.focus != 1 {
		t.Fatalf("the pane key should move focus to slot 1, got %d", m.focus)
	}
	if got := cmdName(t, act.Cmd); got != "b" {
		t.Errorf("pane key returned cmd %q, want the newly focused panel's (\"b\")", got)
	}
	if b.notified != 1 {
		t.Errorf("newly focused panel notified %d times, want 1", b.notified)
	}
	if a.notified != 0 {
		t.Errorf("the panel LOSING focus must not be notified, got %d", a.notified)
	}
}

// TestFocusNotifierNotOnReFocus: landing on the pane you are already in is not a focus
// event, so a panel doesn't restart its work every time focus is re-asserted.
func TestFocusNotifierNotOnReFocus(t *testing.T) {
	only := &focusPanel{name: "only"}
	m := NewModularScreen([][]Slot{{{Panel: only}, {Panel: &shortPanel{h: 1}}}}, ModularOpts{})
	m.SetSize(core.NewShared(nil), 80, 20)

	if cmd := m.FocusSlot(0); cmd != nil { // already focused at construction
		t.Error("re-focusing the already-focused slot must return no cmd")
	}
	if cmd := m.FocusSlot(1); cmd != nil { // not Focusable
		t.Error("a non-Focusable target must return no cmd")
	}
	if cmd := m.FocusSlot(9); cmd != nil {
		t.Error("an out-of-range target must return no cmd")
	}
	if only.notified != 0 {
		t.Errorf("no real focus change happened; notified %d times", only.notified)
	}
}

// TestFocusNotifierOnInit: the slot focused at CONSTRUCTION never sees a transition —
// NewModularScreen focuses it and has no cmd lane — so Init is what delivers it.
func TestFocusNotifierOnInit(t *testing.T) {
	a, b := &focusPanel{name: "a"}, &focusPanel{name: "b"}
	m := NewModularScreen([][]Slot{{{Panel: a}, {Panel: b}}}, ModularOpts{})

	if got := cmdName(t, m.Init(core.NewShared(nil))); got != "a" {
		t.Errorf("Init returned cmd %q, want the initially focused panel's (\"a\")", got)
	}
	if a.notified != 1 || b.notified != 0 {
		t.Errorf("notify counts = (a:%d, b:%d), want (1, 0)", a.notified, b.notified)
	}
}

// TestFocusNotifierOnMousePress: a click that moves focus carries the focus cmd
// ALONGSIDE the press's own work, rather than replacing it.
func TestFocusNotifierOnMousePress(t *testing.T) {
	a, b := &focusPanel{name: "a"}, &focusPanel{name: "b"}
	m := NewModularScreen([][]Slot{
		{{Panel: a}}, {{Panel: b}},
	}, ModularOpts{ColWidths: []int{30, 0}})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	press := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 60, Y: 5}
	_, act := m.Update(sh, press)
	if m.focus != 1 {
		t.Fatalf("a press in the second pane should focus it, got %d", m.focus)
	}
	if got := cmdName(t, act.Cmd); got != "b" {
		t.Errorf("press returned cmd %q, want the newly focused panel's (\"b\")", got)
	}
}

// TestFocusNotifierOptional: a panel without the hook is untouched — the capability is
// opt-in, and the focus move still happens.
func TestFocusNotifierOptional(t *testing.T) {
	plain, notifier := &capturePanel{}, &focusPanel{name: "n"}
	m := NewModularScreen([][]Slot{{{Panel: plain}, {Panel: notifier}}}, ModularOpts{})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)

	if cmd := m.Init(sh); cmd != nil {
		t.Error("a plain focused panel should contribute no focus cmd at Init")
	}
	next := paneKey(t, core.Keys.PaneNext)
	if _, act := m.Update(sh, next); cmdName(t, act.Cmd) != "n" {
		t.Error("focus should still move to, and notify, the notifier panel")
	}
	if _, act := m.Update(sh, next); act.Cmd != nil {
		t.Error("moving focus back to the plain panel should yield no cmd")
	}
	if m.focus != 0 {
		t.Fatalf("focus should have cycled back to slot 0, got %d", m.focus)
	}
}
