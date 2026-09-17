package components

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/bubblestack/internal/tuitest"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// keyMsg is tuitest.KeyMsg under the name every test in this package already calls.
// The parser itself lives in internal/tuitest so components and components/editor drive
// Update from one string→Key implementation.
func keyMsg(s string) tea.KeyPressMsg { return tuitest.KeyMsg(s) }

func newList(items ...list.Item) list.Model {
	l := core.NewSelectList(items, "T")
	l.SetSize(40, 10)
	return l
}

// ---------- Item dispatch via RootUpdate ----------

func TestRootUpdateSelectRunsPick(t *testing.T) {
	ran := false
	it := Item{Name: "A", Pick: func(*core.Shared) core.Action { ran = true; return core.Action{} }}
	l := newList(it)
	RootUpdate(core.NewShared(nil), &l, keyMsg("enter"))
	if !ran {
		t.Error("Select should run the highlighted Item's Pick")
	}
}

func TestRootUpdateItemKeys(t *testing.T) {
	handled := false
	it := Item{
		Name: "A",
		Keys: func(_ *core.Shared, k string) (core.Action, bool) {
			if k == "g" {
				handled = true
				return core.Action{}, true
			}
			return core.Action{}, false
		},
	}
	l := newList(it)
	RootUpdate(core.NewShared(nil), &l, keyMsg("g"))
	if !handled {
		t.Error("an unmatched key should reach the Item's own Keys handler")
	}
}

func TestRootUpdateFallThroughToList(t *testing.T) {
	it := Item{Name: "A"} // nil Pick / Keys
	l := newList(it)
	act := RootUpdate(core.NewShared(nil), &l, keyMsg("g"))
	if act.Msg != nil {
		t.Errorf("an unhandled key should fall through to the list (Async, nil Msg), got %v", act.Msg)
	}
}

func TestRootUpdateFilteringRoutesToList(t *testing.T) {
	ran := false
	it := Item{Name: "A", Pick: func(*core.Shared) core.Action { ran = true; return core.Action{} }}
	l := newList(it)
	l, _ = l.Update(keyMsg("/")) // enter filtering
	if l.FilterState() != list.Filtering {
		t.Skip("list did not enter filtering mode")
	}
	RootUpdate(core.NewShared(nil), &l, keyMsg("a"))
	if ran {
		t.Error("while filtering, keys should go to the list, not run Pick")
	}
}

// ---------- DocScreen wheel ----------

// TestDocScreenWheelScrolls is the docs case the mouse support exists for. DocScreen
// needed no change for it: its Update already forwards non-key messages to its
// viewport, which handles the wheel itself — so this locks in that the router keeps
// letting a wheel over the body through rather than claiming it as chrome.
func TestDocScreenWheelScrolls(t *testing.T) {
	body := strings.Repeat("a line of prose\n", 200)
	d := NewDocScreen(DocOpts{Render: func(int) string { return body }})
	sh := core.NewShared(nil)
	d.SetSize(sh, 40, 10)

	if d.vp.YOffset() != 0 {
		t.Fatalf("a fresh doc should start at the top, got %d", d.vp.YOffset())
	}
	d.Update(sh, wheelMsg(tea.MouseWheelDown))
	if d.vp.YOffset() == 0 {
		t.Fatal("a wheel-down over a doc page should scroll it")
	}

	at := d.vp.YOffset()
	d.Update(sh, wheelMsg(tea.MouseWheelUp))
	if d.vp.YOffset() >= at {
		t.Fatalf("a wheel-up should scroll back, offset stayed at %d", d.vp.YOffset())
	}
}

// ---------- WheelNav ----------

func wheelMsg(b tea.MouseButton) tea.MouseWheelMsg {
	return tea.MouseWheelMsg{Button: b}
}

// TestWheelNavMovesCursor checks a notch moves the cursor exactly one row — not the
// viewport's three, which would skip items on a list.
func TestWheelNavMovesCursor(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"}, Item{Name: "C"})

	if !WheelNav(&l, wheelMsg(tea.MouseWheelDown).Mouse()) {
		t.Fatal("a wheel-down notch should be handled")
	}
	if l.Index() != 1 {
		t.Fatalf("a wheel-down notch should move the cursor one row, got %d", l.Index())
	}
	if !WheelNav(&l, wheelMsg(tea.MouseWheelUp).Mouse()) {
		t.Fatal("a wheel-up notch should be handled")
	}
	if l.Index() != 0 {
		t.Fatalf("a wheel-up notch should move back one row, got %d", l.Index())
	}
}

// TestWheelNavClamps checks the wheel stops at both ends rather than wrapping like
// WrapNav does for the arrow keys: a scroll that teleported end→top would overshoot by
// the whole list.
func TestWheelNavClamps(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"})

	WheelNav(&l, wheelMsg(tea.MouseWheelUp).Mouse()) // already at the top
	if l.Index() != 0 {
		t.Fatalf("a wheel-up at the first row should clamp, not wrap to the last, got %d", l.Index())
	}

	l.Select(1)
	WheelNav(&l, wheelMsg(tea.MouseWheelDown).Mouse()) // already at the bottom
	if l.Index() != 1 {
		t.Fatalf("a wheel-down at the last row should clamp, not wrap to the first, got %d", l.Index())
	}
}

// TestWheelNavIgnoresNonWheel checks a non-wheel mouse event is left unhandled, so the
// wheel-only scope doesn't quietly turn clicks into cursor moves.
func TestWheelNavIgnoresNonWheel(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"})
	msg := tea.MouseClickMsg{Button: tea.MouseLeft}
	if WheelNav(&l, msg.Mouse()) {
		t.Fatal("a click is not a wheel event and must be left unhandled")
	}
	if l.Index() != 0 {
		t.Fatal("a click must not move the cursor")
	}
}

// TestRootUpdateWheelMovesCursor checks the wheel reaches the list through RootUpdate —
// bubbles' list binds no mouse events itself, so without the wiring the wheel would do
// nothing on a tab root.
func TestRootUpdateWheelMovesCursor(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"})
	RootUpdate(core.NewShared(nil), &l, wheelMsg(tea.MouseWheelDown))
	if l.Index() != 1 {
		t.Fatalf("a wheel through RootUpdate should move the list cursor, got %d", l.Index())
	}
}

// ---------- PickerScreen ----------

// TestPickerWheelMovesCursor is the picker's half of the same wiring.
func TestPickerWheelMovesCursor(t *testing.T) {
	p := NewPicker([]list.Item{Item{Name: "A"}, Item{Name: "B"}}, PickerOpts{Title: "T"})
	p.Update(core.NewShared(nil), wheelMsg(tea.MouseWheelDown))
	if p.list.Index() != 1 {
		t.Fatalf("a wheel should move the picker's cursor, got %d", p.list.Index())
	}
}

func TestPickerBackPops(t *testing.T) {
	p := NewPicker([]list.Item{Item{Name: "A"}}, PickerOpts{Title: "T"})
	_, act := p.Update(core.NewShared(nil), keyMsg("esc"))
	if !reflect.DeepEqual(act, core.Pop()) {
		t.Errorf("Back should pop, got %+v", act)
	}
}

func TestPickerOnSelect(t *testing.T) {
	ran := false
	p := NewPicker([]list.Item{Item{Name: "A"}}, PickerOpts{
		Title:    "T",
		OnSelect: func(*core.Shared, list.Item) core.Action { ran = true; return core.Action{} },
	})
	p.SetSize(core.NewShared(nil), 40, 10)
	p.Update(core.NewShared(nil), keyMsg("enter"))
	if !ran {
		t.Error("Select should run OnSelect")
	}
}

func TestPickerSelectFallsBackToItemPick(t *testing.T) {
	ran := false
	it := Item{Name: "A", Pick: func(*core.Shared) core.Action { ran = true; return core.Action{} }}
	p := NewPicker([]list.Item{it}, PickerOpts{Title: "T"}) // no OnSelect
	p.SetSize(core.NewShared(nil), 40, 10)
	p.Update(core.NewShared(nil), keyMsg("enter"))
	if !ran {
		t.Error("with no OnSelect, Select should fall back to the Item's Pick")
	}
}

func TestPickerOnKey(t *testing.T) {
	handled := false
	p := NewPicker([]list.Item{Item{Name: "A"}}, PickerOpts{
		Title: "T",
		OnKey: func(_ *core.Shared, k string, _ list.Item) (core.Action, bool) {
			if k == "g" {
				handled = true
				return core.Action{}, true
			}
			return core.Action{}, false
		},
	})
	p.SetSize(core.NewShared(nil), 40, 10)
	p.Update(core.NewShared(nil), keyMsg("g"))
	if !handled {
		t.Error("OnKey should receive a non-reserved key")
	}
}

func TestPickerCrumbLabel(t *testing.T) {
	if got := NewPicker(nil, PickerOpts{Title: "Title"}).CrumbLabel(false); got != "Title" {
		t.Errorf("CrumbLabel default should be the list title, got %q", got)
	}
	if got := NewPicker(nil, PickerOpts{Title: "Title", Crumb: "Crumb"}).CrumbLabel(false); got != "Crumb" {
		t.Errorf("CrumbLabel should prefer Crumb, got %q", got)
	}
	if got := NewPicker(nil, PickerOpts{Title: "T", Crumb: "C", CrumbShort: "S"}).CrumbLabel(true); got != "S" {
		t.Errorf("CrumbLabel(short) should use CrumbShort, got %q", got)
	}
}

// ---------- DialogScreen ----------

func TestDialogYesRunsOnYes(t *testing.T) {
	ran := false
	d := &DialogScreen{
		Render: func(*core.Shared) string { return "" },
		OnYes:  func(*core.Shared) core.Action { ran = true; return core.Action{} },
	}
	d.Update(core.NewShared(nil), keyMsg("y"))
	if !ran {
		t.Error("y should run OnYes")
	}
}

func TestDialogYesDefaultsToPop(t *testing.T) {
	d := &DialogScreen{Render: func(*core.Shared) string { return "" }}
	_, act := d.Update(core.NewShared(nil), keyMsg("y"))
	if !reflect.DeepEqual(act, core.Pop()) {
		t.Errorf("y with nil OnYes should pop, got %+v", act)
	}
}

func TestDialogNoPops(t *testing.T) {
	d := &DialogScreen{Render: func(*core.Shared) string { return "" }}
	_, act := d.Update(core.NewShared(nil), keyMsg("n"))
	if !reflect.DeepEqual(act, core.Pop()) {
		t.Errorf("n should pop, got %+v", act)
	}
}

func TestDialogOnKey(t *testing.T) {
	handled := false
	d := &DialogScreen{
		Render: func(*core.Shared) string { return "" },
		OnKey:  func(_ *core.Shared, k string) core.Action { handled = (k == "z"); return core.Action{} },
	}
	d.Update(core.NewShared(nil), keyMsg("z"))
	if !handled {
		t.Error("a non-reserved key should reach OnKey")
	}
}

// TestDialogCrumbLabel pins the split the Overlay flag makes: the confirm REPLACES the
// screen below it and gets a breadcrumb segment, the popup is drawn OVER one and gets
// none — the stance MenuScreen and LineEditScreen already take.
func TestDialogCrumbLabel(t *testing.T) {
	// Both crumb fields are ignored on an overlay, not just left unset: they are the
	// ones a caller sets out of habit, and either leaking would put the segment back.
	for _, d := range []*DialogScreen{
		{Overlay: true, Title: "Pop"},
		{Overlay: true, Crumb: "Pop"},
		{Overlay: true, CrumbShort: "P"},
	} {
		if got := d.CrumbLabel(false); got != "" {
			t.Errorf("an overlay must contribute no segment, got %q", got)
		}
		if got := d.CrumbLabel(true); got != "" {
			t.Errorf("an overlay must contribute no short segment, got %q", got)
		}
	}
	if got := (&DialogScreen{}).CrumbLabel(false); got != "Conf" {
		t.Errorf("confirm CrumbLabel default should be Conf, got %q", got)
	}
	if got := (&DialogScreen{Crumb: "X"}).CrumbLabel(false); got != "X" {
		t.Errorf("confirm CrumbLabel should use Crumb, got %q", got)
	}
	if got := (&DialogScreen{Crumb: "X", CrumbShort: "S"}).CrumbLabel(true); got != "S" {
		t.Errorf("confirm CrumbLabel(short) should use CrumbShort, got %q", got)
	}
}

func TestCreatePopupAndConfirm(t *testing.T) {
	p := CreatePopup("T", "B", core.Action{})
	if !p.Overlay {
		t.Error("CreatePopup should set Overlay")
	}
	if got := p.Render(core.NewShared(nil)); got != "B" {
		t.Errorf("popup body = %q, want B", got)
	}
	if len(p.Help) != 1 {
		t.Errorf("popup should default to the single done hint, got %d", len(p.Help))
	}

	c := CreateConfirmScreen(ConfirmSimple{Text: "hi", OnYes: core.Pop()})
	if c.Overlay {
		t.Error("CreateConfirmScreen should be full-screen (not overlay)")
	}
	if len(c.Help) != len(DefaultHelpKeys) {
		t.Errorf("confirm should default to DefaultHelpKeys, got %d", len(c.Help))
	}
	if !reflect.DeepEqual(c.OnYes(core.NewShared(nil)), core.Pop()) {
		t.Error("CreateConfirmScreen should wire OnYes")
	}
}

// ---------- QueryUpdate ----------

type fakeTypable struct {
	typing bool
	in     textinput.Model
}

func (f *fakeTypable) Typing() bool { return f.typing }

func (f *fakeTypable) UpdateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	f.in, cmd = f.in.Update(msg)
	return cmd
}

func newTypable(typing bool) *fakeTypable {
	ti := textinput.New()
	ti.Focus()
	return &fakeTypable{typing: typing, in: ti}
}

func TestQueryUpdateTyping(t *testing.T) {
	f := newTypable(true)
	cmd, handled := QueryUpdate(f, keyMsg("a"))
	_ = cmd
	if !handled {
		t.Fatal("a printable key should be handled while typing")
	}
	if f.in.Value() != "a" {
		t.Errorf("the keystroke should feed the input, value = %q", f.in.Value())
	}

	if _, h := QueryUpdate(f, keyMsg("backspace")); !h {
		t.Error("backspace should be diverted to the input while typing")
	}
}

func TestQueryUpdateControlKeysPassThrough(t *testing.T) {
	f := newTypable(true)
	if _, h := QueryUpdate(f, keyMsg("esc")); h {
		t.Error("esc should not be diverted (cancel must reach the caller)")
	}
	if _, h := QueryUpdate(f, keyMsg("enter")); h {
		t.Error("enter should not be diverted")
	}
}

func TestQueryUpdateNotTyping(t *testing.T) {
	f := newTypable(false)
	if _, h := QueryUpdate(f, keyMsg("a")); h {
		t.Error("a non-typing screen should never divert keys")
	}
}

// ---------- ToggleField / RenderToggle ----------

func TestToggleFieldCycling(t *testing.T) {
	tf := NewToggleField("k", "l", []string{"A", "B", "C"})
	if tf.Index() != 0 || tf.Value() != "A" {
		t.Fatalf("initial index/value = %d/%q", tf.Index(), tf.Value())
	}
	tf.OnToggle(true)
	tf.OnToggle(true)
	if tf.Index() != 2 || tf.Value() != "C" {
		t.Errorf("forward to 2: index/value = %d/%q", tf.Index(), tf.Value())
	}
	tf.OnToggle(true) // wrap forward
	if tf.Index() != 0 {
		t.Errorf("forward wrap should reach 0, got %d", tf.Index())
	}
	tf.OnToggle(false) // wrap backward
	if tf.Index() != 2 {
		t.Errorf("backward wrap should reach 2, got %d", tf.Index())
	}

	tf.SetIndex(1)
	if tf.Index() != 1 {
		t.Errorf("SetIndex(1) = %d", tf.Index())
	}
	tf.SetIndex(9) // out of range, ignored
	if tf.Index() != 1 {
		t.Errorf("out-of-range SetIndex should be ignored, got %d", tf.Index())
	}
}

func TestRenderToggle(t *testing.T) {
	out := RenderToggle([]string{"A", "B"}, 0, "")
	if !containsAll(out, "A", "B", "◄") {
		t.Errorf("default RenderToggle should show options and ◄ ► arrows, got %q", out)
	}
	if d := RenderToggle([]string{"A", "B"}, 0, "|"); !containsAll(d, "A", "B", "|") {
		t.Errorf("custom delim RenderToggle should join with it, got %q", d)
	}
}

// ---------- crumbSeg ----------

func TestCrumbSeg(t *testing.T) {
	if got := CrumbSegment(true, "S", "C", "F"); got != "S" {
		t.Errorf("short with crumbShort should be S, got %q", got)
	}
	if got := CrumbSegment(false, "S", "C", "F"); got != "C" {
		t.Errorf("non-short should prefer crumb, got %q", got)
	}
	if got := CrumbSegment(true, "", "C", "F"); got != "C" {
		t.Errorf("short with no crumbShort should fall to crumb, got %q", got)
	}
	if got := CrumbSegment(false, "", "", "F"); got != "F" {
		t.Errorf("empty crumb should use the fallback, got %q", got)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// ---------- PickerScreen density ----------

// densityPicker is the shared fixture: four described rows, the framework chord bound, and
// a real allocation, since pagination (and therefore every row-geometry assertion below)
// is derived from one.
func densityPicker(t *testing.T, opts PickerOpts) (*PickerScreen, *core.Shared) {
	t.Helper()
	opts.Title = "T"
	p := NewPicker([]list.Item{
		Item{Name: "alpha", Desc: "first"},
		Item{Name: "beta", Desc: "second"},
		Item{Name: "gamma", Desc: "third"},
		Item{Name: "delta", Desc: "fourth"},
	}, opts)
	sh := core.NewShared(nil)
	p.SetSize(sh, 40, 12)
	return p, sh
}

// lineWith returns the first rendered line containing want, so a test can ask whether two
// strings share a row without counting rows itself.
func lineWith(view, want string) string {
	for _, ln := range strings.Split(view, "\n") {
		if strings.Contains(ln, want) {
			return ln
		}
	}
	return ""
}

// TestPickerCompactPutsDescriptionOnTheTitleLine is the feature in one assertion, and the
// regression guard for core.compactText: a plain components.Item implements no SuffixText,
// so before the fallback core.CompactDelegate rendered nothing at all for these rows.
func TestPickerCompactPutsDescriptionOnTheTitleLine(t *testing.T) {
	compact, _ := densityPicker(t, PickerOpts{Compact: true})
	sh := core.NewShared(nil)

	line := lineWith(compact.View(sh), "alpha")
	if line == "" {
		t.Fatal("a compact picker must render a plain Item; it has no SuffixText method")
	}
	if !strings.Contains(line, "first") {
		t.Errorf("compact should carry the description as a suffix on the title line, got %q", line)
	}

	normal, _ := densityPicker(t, PickerOpts{})
	if line := lineWith(normal.View(sh), "alpha"); strings.Contains(line, "first") {
		t.Errorf("the default density keeps the description on its own row, got %q", line)
	}
}

// TestPickerDensityKeyToggles checks the chord flips both ways and that the flip is
// structural, not just cosmetic: a one-row delegate fits strictly more rows per page.
func TestPickerDensityKeyToggles(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{})
	wide := p.list.Paginator.PerPage

	p.Update(sh, keyMsg("D"))
	if !p.Compact() {
		t.Fatal("the density key should flip the picker to compact")
	}
	if p.list.Paginator.PerPage <= wide {
		t.Errorf("a one-row delegate should fit more rows per page, got %d want >%d",
			p.list.Paginator.PerPage, wide)
	}
	if line := lineWith(p.View(sh), "alpha"); !strings.Contains(line, "first") {
		t.Errorf("after the flip the description should be a suffix, got %q", line)
	}

	p.Update(sh, keyMsg("D"))
	if p.Compact() {
		t.Fatal("the density key should flip back")
	}
	if p.list.Paginator.PerPage != wide {
		t.Errorf("flipping back should restore the original pagination, got %d want %d",
			p.list.Paginator.PerPage, wide)
	}
}

func TestPickerDisabledDensityKeyIsInert(t *testing.T) {
	p := NewPicker([]list.Item{Item{Name: "alpha", Desc: "first"}}, PickerOpts{
		Title: "T", DisableDensityToggle: true, DensityKey: DefaultDensityKey,
	})
	sh := core.NewShared(nil)
	p.SetSize(sh, 40, 12)

	p.Update(sh, keyMsg("D"))
	if p.Compact() {
		t.Error("D must do nothing when density toggling is disabled")
	}
	for _, b := range p.list.AdditionalFullHelpKeys() {
		if b.Help().Desc == "density" {
			t.Error("a picker with a disabled density key must not advertise one")
		}
	}
}

func TestPickerCustomDensityKey(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{DensityKey: key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "density"))})
	p.Update(sh, keyMsg("D"))
	if p.Compact() {
		t.Fatal("a custom density key replaces the default")
	}
	p.Update(sh, keyMsg("X"))
	if !p.Compact() {
		t.Fatal("the custom density key should toggle")
	}
	for _, b := range p.list.AdditionalFullHelpKeys() {
		if b.Help().Desc == "density" && b.Help().Key != "X" {
			t.Errorf("density help should advertise X, got %q", b.Help().Key)
		}
	}
}

func TestPickerDefaultDensityKeyIsFilterText(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{})
	p.Update(sh, keyMsg("/"))
	p.Update(sh, keyMsg("D"))
	if p.Compact() || p.list.FilterValue() != "D" {
		t.Fatalf("D should stay filter text: compact=%v, query=%q", p.Compact(), p.list.FilterValue())
	}
}

// TestPickerDensityKeepsCursorAndFilter: the flip is a SetDelegate, not a rebuild, so
// unlike FilePanel.SetCompact even an applied /-filter survives it.
func TestPickerDensityKeepsCursorAndFilter(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{})
	p.list.SetFilterText("a") // alpha, beta, gamma, delta all match
	p.list.Select(2)
	before := p.list.SelectedItem()

	p.Update(sh, keyMsg("D"))

	if p.list.FilterState() != list.FilterApplied {
		t.Errorf("an applied filter should survive the flip, got %v", p.list.FilterState())
	}
	if p.list.FilterValue() != "a" {
		t.Errorf("the filter query should survive the flip, got %q", p.list.FilterValue())
	}
	if got := p.list.SelectedItem(); !reflect.DeepEqual(got, before) {
		t.Errorf("the cursor should hold still across the flip, got %v want %v", got, before)
	}
}

// TestPickerCompactClickSelectsClickedRow guards the listDispatchRows swap: a compact list
// hit-tested with the three-row constant would select a row three places off.
func TestPickerCompactClickSelectsClickedRow(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{Compact: true})
	// Rows start below the titled list's two-row header and are one row tall each.
	p.Update(sh, tea.MouseClickMsg{X: 5, Y: 2 + 2, Button: tea.MouseLeft})
	if p.list.Index() != 2 {
		t.Fatalf("a click on the third compact row should select item 2, got %d", p.list.Index())
	}
}

// TestPickerOnKeyOutranksDensity: the flip is matched after the screen's own hooks, so a
// consumer that already binds the chord keeps it.
func TestPickerOnKeyOutranksDensity(t *testing.T) {
	claimed := false
	p, sh := densityPicker(t, PickerOpts{
		OnKey: func(_ *core.Shared, k string, _ list.Item) (core.Action, bool) {
			if k == "D" {
				claimed = true
				return core.Action{}, true
			}
			return core.Action{}, false
		},
	})
	p.Update(sh, keyMsg("D"))
	if !claimed {
		t.Fatal("OnKey should be offered the density chord first")
	}
	if p.Compact() {
		t.Error("a key claimed by OnKey must not also flip the density")
	}
}

// TestPickerDensityKeyIsFullHelpOnly: a density flip is a command, so it belongs in the (?)
// menu and never on the bar (core.ShortHelp's short branch is a fixed literal by design).
func TestPickerDensityKeyIsFullHelpOnly(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{})

	found := false
	for _, b := range p.list.AdditionalFullHelpKeys() {
		if b.Help().Desc == "density" {
			found = true
		}
	}
	if !found {
		t.Error("a bound density key should appear in the (?) full help")
	}
	if strings.Contains(p.HelpView(sh), "density") {
		t.Error("the short help bar must stay sparse; density belongs in the (?) menu")
	}
}

// TestPickerThemeChangeKeepsDensity: Receive rebuilds the delegate to refresh cached theme
// colors, and rebuilding the wrong one would silently undo a flip.
func TestPickerThemeChangeKeepsDensity(t *testing.T) {
	p, sh := densityPicker(t, PickerOpts{Compact: true})
	p.Receive(sh, core.MsgThemeChanged{})
	if !p.Compact() {
		t.Fatal("a theme broadcast must not reset the density")
	}
	if line := lineWith(p.View(sh), "alpha"); !strings.Contains(line, "first") {
		t.Errorf("the compact delegate should still be live after a theme change, got %q", line)
	}
}

// ---------- RootUpdateRows ----------

// TestRootUpdateRowsCompactClick guards the tab-root half of the density work: a compact
// root hit-tested with the default three-row constant selects a row up to three places off.
func TestRootUpdateRowsCompactClick(t *testing.T) {
	items := []list.Item{
		Item{Name: "alpha", Desc: "first"},
		Item{Name: "beta", Desc: "second"},
		Item{Name: "gamma", Desc: "third"},
	}
	l := core.NewCompactList(items, "T")
	var dens Density
	dens.SetCompact(&l, true)
	dens.Fit(&l, 40, 12)

	// Rows start below the titled list's two-row header and are one row tall each.
	RootUpdateRows(core.NewShared(nil), &l, tea.MouseClickMsg{X: 5, Y: 2 + 2, Button: tea.MouseLeft}, dens.ItemRows())
	if l.Index() != 2 {
		t.Fatalf("a click on the third compact row should select item 2, got %d", l.Index())
	}
}

// TestDensityRestyleKeepsCompact: a screen reacting to a theme change goes through Restyle
// rather than core.NewDelegate(), so repainting cannot silently undo a flip.
func TestDensityRestyleKeepsCompact(t *testing.T) {
	l := core.NewSelectList([]list.Item{Item{Name: "alpha", Desc: "first"}}, "")
	var dens Density
	dens.Fit(&l, 40, 12)
	dens.SetCompact(&l, true)
	perPage := l.Paginator.PerPage

	dens.Restyle(&l)
	if !dens.Compact() {
		t.Fatal("Restyle must not change the density")
	}
	if l.Paginator.PerPage != perPage {
		t.Errorf("Restyle should rebuild the compact delegate, got PerPage %d want %d",
			l.Paginator.PerPage, perPage)
	}
	if got := lineWith(core.RenderList(l), "alpha"); !strings.Contains(got, "first") {
		t.Errorf("the compact delegate should still be live after Restyle, got %q", got)
	}
}
