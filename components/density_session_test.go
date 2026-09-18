package components

import (
	"errors"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

type densityApp struct{ compact bool }

func (a *densityApp) ListDensity() *bool { return &a.compact }
func (a *densityApp) Receive(_ *core.Shared, payload any) core.Action {
	return core.OnThemeChange(payload)
}

func TestSessionDensityAcrossRouterStack(t *testing.T) {
	app := &densityApp{}
	sh := core.NewShared(app)
	var saves []bool
	sh.SaveListDensity = func(compact bool) error {
		saves = append(saves, compact)
		return nil
	}
	items := []list.Item{Item{Name: "alpha", Desc: "first"}, Item{Name: "beta", Desc: "second"}}
	var roots []*RootListScreen
	refreshes, receives := 0, 0
	refresh := func(*core.Shared, any) ([]list.Item, bool) {
		refreshes++
		return nil, false
	}
	newRoot := func(*core.Shared) core.Screen {
		s := NewRootList(items, RootListOpts{
			PickerOpts: PickerOpts{Title: "Root", Refresh: refresh},
			Receive:    func(*core.Shared, any) core.Action { receives++; return core.Action{} },
		})
		roots = append(roots, s)
		return s
	}
	r := core.NewRouter(sh, []core.TabEntry{{Title: "One", New: newRoot}, {Title: "Two", New: newRoot}})
	step := func(msg tea.Msg) {
		t.Helper()
		m, _ := r.Update(msg)
		r = m.(core.Router)
	}
	step(tea.WindowSizeMsg{Width: 60, Height: 20})
	root := roots[0]
	root.List().SetFilterText("a")
	root.List().Select(1)
	step(keyMsg("D"))
	if !app.compact || !root.Compact() || !roots[1].Compact() {
		t.Fatal("a toggle must update the active and never-visited roots")
	}
	p := NewPicker(items, PickerOpts{Refresh: refresh})
	step(core.Push(p))
	if !p.Compact() {
		t.Fatal("a newly pushed picker must inherit compact density")
	}
	p.List().SetFilterText("a")
	p.List().Select(1)
	child := NewPicker(items, PickerOpts{Refresh: refresh})
	step(core.Push(child))
	step(keyMsg("D"))
	if app.compact || child.Compact() || p.Compact() || root.Compact() || roots[1].Compact() {
		t.Fatal("a deep picker toggle must update the whole retained stack and inactive roots")
	}
	for _, s := range []*PickerScreen{root.PickerScreen, p} {
		if s.List().FilterValue() != "a" || s.List().Index() != 1 || len(s.List().VisibleItems()) != 2 {
			t.Fatal("density broadcasts must preserve the filter and selected row")
		}
	}
	if len(roots) != 2 || refreshes != 0 || receives != 0 {
		t.Fatalf("density must not rebuild roots or run data callbacks: roots=%d refresh=%d receive=%d", len(roots), refreshes, receives)
	}
	step(keyMsg("esc"))
	if r.Top() != p || p.Compact() {
		t.Fatal("back must reveal the same picker at the new density")
	}
	step(keyMsg("D"))
	replacement := NewPicker(items, PickerOpts{})
	step(core.Replace(replacement))
	if !replacement.Compact() {
		t.Fatal("replaced pickers must inherit density too")
	}
	step(core.PropagateAll(core.MsgThemeChanged{}))
	if len(roots) != 4 || !replacement.Compact() {
		t.Fatal("theme reconstruction must retain session density")
	}
	step(core.ResetToRoot())
	if !r.Top().(*RootListScreen).Compact() {
		t.Fatal("a rebuilt root must read the same preference")
	}
	step(keyMsg("]"))
	if !r.Top().(*RootListScreen).Compact() {
		t.Fatal("switching to a rebuilt inactive root must retain density")
	}
	if len(saves) != 3 || !saves[0] || saves[1] || !saves[2] {
		t.Fatalf("expected one save per user toggle across roots/pickers, got %v", saves)
	}
}

func TestDensityPersistenceOwnership(t *testing.T) {
	for _, kind := range []string{"app", "same override", "independent override", "local", "disabled", "save failure"} {
		t.Run(kind, func(t *testing.T) {
			app := &densityApp{}
			sh := core.NewShared(app)
			var saves []bool
			sh.SaveListDensity = func(compact bool) error {
				saves = append(saves, compact)
				if kind == "save failure" {
					return errors.New("unwritable config")
				}
				return nil
			}
			opts := RootListOpts{}
			explicit := false
			switch kind {
			case "same override":
				opts.CompactState = &app.compact
			case "independent override":
				opts.CompactState = &explicit
			case "local":
				sh.App = nil
			case "disabled":
				opts.DisableDensityToggle = true
			}
			root := NewRootList(nil, opts)
			root.Init(sh)
			root.SetSize(sh, 40, 12)
			root.SetCompact(true)
			root.ToggleDensity()
			root.Receive(sh, core.MsgListDensityChanged{})
			if len(saves) != 0 {
				t.Fatal("lifecycle and programmatic changes must not save")
			}
			_, act := root.Update(sh, keyMsg("D"))
			wantSave := kind == "app" || kind == "same override" || kind == "save failure"
			if wantSave {
				if len(saves) != 1 || !saves[0] || !app.compact || act.Msg == nil {
					t.Fatalf("user toggle must save once and still broadcast on failure: saves=%v compact=%v action=%v", saves, app.compact, act)
				}
			} else if len(saves) != 0 {
				t.Fatalf("independent or disabled toggle saved app preference: %v", saves)
			}
		})
	}
}

func TestDensityProviderLifecycleAndIsolation(t *testing.T) {
	for _, lifecycle := range []string{"init", "resize", "receive", "update"} {
		t.Run(lifecycle, func(t *testing.T) {
			app := &densityApp{compact: true}
			sh := core.NewShared(app)
			p := NewPicker(nil, PickerOpts{DisableDensityToggle: true})
			switch lifecycle {
			case "init":
				p.Init(sh)
			case "resize":
				p.SetSize(sh, 40, 12)
			case "receive":
				p.Receive(sh, core.MsgListDensityChanged{})
			case "update":
				p.Update(sh, keyMsg("D"))
			}
			if !p.Compact() || !app.compact {
				t.Fatal("a disabled shortcut must still follow the app preference")
			}
			other := &densityApp{}
			q := NewPicker(nil, PickerOpts{})
			q.Init(core.NewShared(other))
			if q.Compact() {
				t.Fatal("separate app instances must not share density")
			}
		})
	}
	// Non-participating apps, including gote, retain screen-local behavior.
	sh := core.NewShared(nil)
	a, b := NewPicker(nil, PickerOpts{}), NewPicker(nil, PickerOpts{})
	a.Init(sh)
	b.Init(sh)
	_, act := a.Update(sh, keyMsg("D"))
	if !a.Compact() || b.Compact() || act.Msg != nil {
		t.Fatal("without a provider, a toggle must stay local and emit no broadcast")
	}
}

func TestExplicitDensityOverridesProvider(t *testing.T) {
	app := &densityApp{compact: true}
	sh := core.NewShared(app)
	explicit := false
	root := NewRootList(nil, RootListOpts{CompactState: &explicit})
	root.Init(sh)
	root.Receive(sh, core.MsgListDensityChanged{})
	if root.Compact() {
		t.Fatal("the explicit pointer must override the app provider")
	}
	root.ToggleDensity()
	root.SetCompact(false)
	if explicit || !app.compact {
		t.Fatal("programmatic changes must write only the bound preference")
	}
}
