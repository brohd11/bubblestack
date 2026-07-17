package components

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// keyMsg builds a tea.KeyMsg whose String() matches the given key string.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

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

	if d.vp.YOffset != 0 {
		t.Fatalf("a fresh doc should start at the top, got %d", d.vp.YOffset)
	}
	d.Update(sh, wheelMsg(tea.MouseButtonWheelDown))
	if d.vp.YOffset == 0 {
		t.Fatal("a wheel-down over a doc page should scroll it")
	}

	at := d.vp.YOffset
	d.Update(sh, wheelMsg(tea.MouseButtonWheelUp))
	if d.vp.YOffset >= at {
		t.Fatalf("a wheel-up should scroll back, offset stayed at %d", d.vp.YOffset)
	}
}

// ---------- WheelNav ----------

func wheelMsg(b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Button: b, Action: tea.MouseActionPress}
}

// TestWheelNavMovesCursor checks a notch moves the cursor exactly one row — not the
// viewport's three, which would skip items on a list.
func TestWheelNavMovesCursor(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"}, Item{Name: "C"})

	if !WheelNav(&l, wheelMsg(tea.MouseButtonWheelDown)) {
		t.Fatal("a wheel-down notch should be handled")
	}
	if l.Index() != 1 {
		t.Fatalf("a wheel-down notch should move the cursor one row, got %d", l.Index())
	}
	if !WheelNav(&l, wheelMsg(tea.MouseButtonWheelUp)) {
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

	WheelNav(&l, wheelMsg(tea.MouseButtonWheelUp)) // already at the top
	if l.Index() != 0 {
		t.Fatalf("a wheel-up at the first row should clamp, not wrap to the last, got %d", l.Index())
	}

	l.Select(1)
	WheelNav(&l, wheelMsg(tea.MouseButtonWheelDown)) // already at the bottom
	if l.Index() != 1 {
		t.Fatalf("a wheel-down at the last row should clamp, not wrap to the first, got %d", l.Index())
	}
}

// TestWheelNavIgnoresNonWheel checks a non-wheel mouse event is left unhandled, so the
// wheel-only scope doesn't quietly turn clicks into cursor moves.
func TestWheelNavIgnoresNonWheel(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"})
	msg := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
	if WheelNav(&l, msg) {
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
	RootUpdate(core.NewShared(nil), &l, wheelMsg(tea.MouseButtonWheelDown))
	if l.Index() != 1 {
		t.Fatalf("a wheel through RootUpdate should move the list cursor, got %d", l.Index())
	}
}

// ---------- PickerScreen ----------

// TestPickerWheelMovesCursor is the picker's half of the same wiring.
func TestPickerWheelMovesCursor(t *testing.T) {
	p := NewPicker([]list.Item{Item{Name: "A"}, Item{Name: "B"}}, PickerOpts{Title: "T"})
	p.Update(core.NewShared(nil), wheelMsg(tea.MouseButtonWheelDown))
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

func TestDialogCrumbLabel(t *testing.T) {
	if got := (&DialogScreen{Overlay: true, Title: "Pop"}).CrumbLabel(false); got != "Pop" {
		t.Errorf("overlay CrumbLabel should be the Title, got %q", got)
	}
	if got := (&DialogScreen{}).CrumbLabel(false); got != "Conf" {
		t.Errorf("confirm CrumbLabel default should be Conf, got %q", got)
	}
	if got := (&DialogScreen{Crumb: "X"}).CrumbLabel(false); got != "X" {
		t.Errorf("confirm CrumbLabel should use Crumb, got %q", got)
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
	if got := crumbSeg(true, "S", "C", "F"); got != "S" {
		t.Errorf("short with crumbShort should be S, got %q", got)
	}
	if got := crumbSeg(false, "S", "C", "F"); got != "C" {
		t.Errorf("non-short should prefer crumb, got %q", got)
	}
	if got := crumbSeg(true, "", "C", "F"); got != "C" {
		t.Errorf("short with no crumbShort should fall to crumb, got %q", got)
	}
	if got := crumbSeg(false, "", "", "F"); got != "F" {
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
