package components

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

func TestRootListNavigationAndLifecycle(t *testing.T) {
	sh := core.NewShared(nil)
	sh.Chrome = &core.Chrome{Status: NewStatusLine()}
	initialized, received := false, false
	root := NewRootList([]list.Item{Item{Name: "open", Pick: func(sh *core.Shared) core.Action {
		if sh.Chrome.Status.Shown() {
			t.Error("root selection must clear status before invoking the row")
		}
		return core.Push(NewPicker([]list.Item{Item{Name: "child"}}, PickerOpts{}))
	}}}, RootListOpts{
		Init: func(*core.Shared) tea.Cmd {
			return func() tea.Msg { initialized = true; return "initialized" }
		},
		Receive: func(*core.Shared, any) core.Action {
			received = true
			return core.Async(func() tea.Msg { return "received" })
		},
	})
	r := core.NewRouter(sh, []core.TabEntry{{Title: "Root", New: func(*core.Shared) core.Screen { return root }}})
	if cmd := r.Init(); cmd == nil || cmd() != "initialized" || !initialized {
		t.Fatal("root initialization must reach its callback")
	}
	m, _ := r.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	r = m.(core.Router)
	if r.Top() != root || root.CrumbLabel(false) != "Tab" {
		t.Fatal("Update must retain the root wrapper and its breadcrumb")
	}
	m, _ = r.Update(keyMsg("esc"))
	r = m.(core.Router)
	if r.Top() != root {
		t.Fatal("back at a root must not navigate")
	}
	// Assert the component itself does not emit a pop, which a router at depth 1
	// would otherwise hide by ignoring it.
	if _, act := root.Update(sh, keyMsg("esc")); act.Msg != nil {
		t.Fatal("root back must not emit navigation")
	}
	if act := r.Top().(core.Receiver).Receive(sh, "refresh"); !received || act.Cmd == nil || act.Cmd() != "received" {
		t.Fatal("root must retain its receive callback and return its action")
	}
	sh.Chrome.Status.Set("old status")
	m, _ = r.Update(keyMsg("enter"))
	r = m.(core.Router)
	if _, ok := r.Top().(*PickerScreen); !ok {
		t.Fatalf("select must push a picker, got %T", r.Top())
	}
	m, _ = r.Update(keyMsg("esc"))
	r = m.(core.Router)
	if r.Top() != root {
		t.Fatal("picker back must return to the same root")
	}
	_, cmd := r.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("the router must still own quitting")
	}
	if msg := cmd(); reflect.TypeOf(msg) != reflect.TypeOf(tea.QuitMsg{}) {
		t.Fatalf("q should quit, got %T", msg)
	}
}

func TestRootListKeyPrecedenceAndFilter(t *testing.T) {
	sh := core.NewShared(nil)
	var calls []string
	root := NewRootList([]list.Item{Item{Name: "alpha", Keys: func(_ *core.Shared, k string) (core.Action, bool) {
		calls = append(calls, "row:"+k)
		return core.Action{}, k == "v" || k == "D"
	}}}, RootListOpts{PickerOpts: PickerOpts{
		OnKey: func(_ *core.Shared, k string, _ list.Item) (core.Action, bool) {
			calls = append(calls, "screen:"+k)
			return core.Action{}, k == "s"
		},
	}})
	root.SetSize(sh, 40, 12)
	for _, k := range []string{"s", "v", "D"} {
		root.Update(sh, keyMsg(k))
	}
	want := []string{"screen:s", "screen:v", "row:v", "screen:D", "row:D"}
	if !reflect.DeepEqual(calls, want) || root.Compact() {
		t.Fatalf("root keys should augment row keys and outrank density: %v, compact=%v", calls, root.Compact())
	}
	root.Update(sh, keyMsg("/"))
	calls = nil
	root.Update(sh, keyMsg("D"))
	if len(calls) != 0 || root.List().FilterValue() != "D" || root.Compact() {
		t.Fatal("filter text must bypass both key callbacks and density")
	}
}

func TestRootListSharedDensityAndRefresh(t *testing.T) {
	sh := core.NewShared(nil)
	compact := false
	items := []list.Item{Item{Name: "alpha", Desc: "first"}, Item{Name: "beta", Desc: "second"}, Item{Name: "gamma", Desc: "third"}}
	opts := RootListOpts{CompactState: &compact, PickerOpts: PickerOpts{
		Title: "Root",
		Refresh: func(_ *core.Shared, payload any) ([]list.Item, bool) {
			return items, payload == "refresh"
		},
	}}
	a, b := NewRootList(items, opts), NewRootList(items, opts)
	a.SetSize(sh, 40, 12)
	b.SetSize(sh, 40, 12)
	a.List().SetFilterText("a")
	a.List().Select(2)
	a.Update(sh, keyMsg("D"))
	b.SetSize(sh, 40, 12)
	if !compact || !a.Compact() || !b.Compact() || a.List().Index() != 2 {
		t.Fatal("density should propagate without moving the cursor")
	}
	a.Receive(sh, "refresh")
	a.Receive(sh, core.MsgThemeChanged{})
	if a.List().FilterValue() != "a" || len(a.List().VisibleItems()) != 3 || !a.Compact() {
		t.Fatal("refresh and theme change must preserve filter and density")
	}
	if line := lineWith(ansi.Strip(a.View(sh)), "alpha"); !strings.Contains(line, "first") {
		t.Fatalf("theme change must retain the compact delegate, got %q", line)
	}
	rebuilt := NewRootList(items, opts)
	if !rebuilt.Compact() {
		t.Fatal("root reconstruction must read the saved density")
	}
	b.SetCompact(false)
	a.SetSize(sh, 40, 12)
	if compact || a.Compact() {
		t.Fatal("programmatic changes must write through the shared preference")
	}
	if strings.Contains(a.HelpView(sh), "density") || !strings.Contains(a.HelpView(sh), "tabs") {
		t.Fatal("roots need tabbed short help with density confined to full help")
	}
	foundDensity := false
	for _, b := range a.List().AdditionalFullHelpKeys() {
		if b.Help().Desc == "back" {
			t.Error("root full help must not advertise the picker's Back action")
		}
		foundDensity = foundDensity || b.Help().Desc == "density"
	}
	if !foundDensity {
		t.Error("root full help must advertise density by default")
	}
}

func TestRootListMouseGeometryBothDensities(t *testing.T) {
	for _, compact := range []bool{false, true} {
		sh := core.NewShared(nil)
		picked := ""
		root := NewRootList([]list.Item{Item{Name: "alpha"}, Item{Name: "beta"}, Item{Name: "gamma"}}, RootListOpts{
			PickerOpts: PickerOpts{Title: "Root", Compact: compact, OnSelect: func(_ *core.Shared, it list.Item) core.Action {
				picked = it.(Item).Name
				return core.Action{}
			}},
		})
		root.SetSize(sh, 40, 12)
		y := 8 // titled list header (2) + two three-row items
		if compact {
			y = 4 // header (2) + two one-row items
		}
		root.Update(sh, tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
		if picked != "gamma" {
			t.Fatalf("compact=%v: clicked third row, picked %q", compact, picked)
		}
	}
}
