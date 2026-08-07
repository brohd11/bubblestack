package components

import (
	"reflect"
	"testing"

	"github.com/brohd11/bubblestack/core"
)

// The Actions menu tests inspect the built picker's rows and run their Pick closures
// against a shared — no router involved.

func actionsItems(s *PickerScreen) []Item {
	var out []Item
	for _, li := range s.list.Items() {
		if it, ok := li.(Item); ok {
			out = append(out, it)
		}
	}
	return out
}

func TestActionsMenuStandardRows(t *testing.T) {
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} })
	items := actionsItems(s)
	if len(items) != 3 {
		t.Fatalf("rows = %d, want 3 (Theme, Update, Refresh)", len(items))
	}
	want := []string{"◑ Theme", "⟲ Update testapp", "⟳ Refresh"}
	for i, name := range want {
		if items[i].Name != name {
			t.Errorf("row %d = %q, want %q", i, items[i].Name, name)
		}
	}
	if !s.PopStop() {
		t.Error("the Actions menu should be a PopTo boundary (PopStop)")
	}
}

func TestActionsMenuExtraRowsAfterTheme(t *testing.T) {
	extra := Item{Name: "? Docs", Desc: "docs"}
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} }, extra)
	items := actionsItems(s)
	if len(items) != 4 || items[1].Name != "? Docs" {
		t.Fatalf("extra rows should slot in after Theme, got %+v", items)
	}
}

func TestActionsMenuRowActions(t *testing.T) {
	refreshed := false
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things",
		func(*core.Shared) core.Action { refreshed = true; return core.SetStatus("refreshed") })
	items := actionsItems(s)
	sh := core.NewShared(nil)

	// Theme pushes the theme picker; Update pushes the loading screen. Both are Push
	// navigation — assert the type, the screens are unexported inside core's pushMsg.
	for _, i := range []int{0, 1} {
		if got := reflect.TypeOf(items[i].Pick(sh).Msg).String(); got != "core.pushMsg" {
			t.Errorf("row %d should push its screen, got %s", i, got)
		}
	}
	// Refresh runs the app's own action verbatim.
	if act := items[2].Pick(sh); !refreshed || !reflect.DeepEqual(act, core.SetStatus("refreshed")) {
		t.Errorf("Refresh should run the app's refresh action, got refreshed=%v act=%+v", refreshed, act)
	}
}
