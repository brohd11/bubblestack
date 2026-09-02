package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestFloatingPopupPassesInputByDefaultAndClamps(t *testing.T) {
	p := &FloatingPopup{
		Content:   func() string { return "notice" },
		Placement: PlacePopupTopRight(1),
	}
	if act, handled := p.Update(core.NewShared(nil), keyMsg("x")); handled || act.Msg != nil || act.Cmd != nil {
		t.Fatalf("passive popup handled input: handled=%v action=%+v", handled, act)
	}
	out := ansi.Strip(p.ViewOver(strings.Repeat(".", 12)+"\n"+strings.Repeat(".", 12), 12, 2))
	if !strings.Contains(out, "....notice.") {
		t.Fatalf("top-right popup was not composited at its inset:\n%s", out)
	}

	p.Placement = PlacePopupAt(PopupAnchor{X: 11, Y: 1, FlipX: 11, FlipY: 1})
	out = ansi.Strip(p.ViewOver(strings.Repeat(".", 12)+"\n"+strings.Repeat(".", 12), 12, 2))
	if !strings.Contains(out, "....notice.") {
		t.Fatalf("anchored popup did not flip/clamp into the frame:\n%s", out)
	}
}

func TestPopupListClaimsOnlyItsControlKeys(t *testing.T) {
	accepted := ""
	cancelled := false
	list := NewPopupList(PopupListOpts[string]{
		Items: []PopupListItem[string]{
			{Label: "alpha", FilterText: "alpha", Value: "a"},
			{Label: "beta", FilterText: "beta", Value: "b"},
		},
		Filter: func(query string, item PopupListItem[string]) bool {
			return strings.HasPrefix(strings.ToLower(item.FilterText), strings.ToLower(query))
		},
		OnAccept: func(_ *core.Shared, item PopupListItem[string]) core.Action {
			accepted = item.Value
			return core.Action{}
		},
		OnCancel: func(*core.Shared) core.Action { cancelled = true; return core.Action{} },
	})
	sh := core.NewShared(nil)
	for _, msg := range []tea.Msg{keyMsg("x"), keyMsg("left"), tea.MouseClickMsg{}} {
		if _, handled := list.Update(sh, msg); handled {
			t.Fatalf("PopupList handled pass-through message %#v", msg)
		}
	}
	if _, handled := list.Update(sh, keyMsg("down")); !handled {
		t.Fatal("down was not handled")
	}
	if _, handled := list.Update(sh, keyMsg("tab")); !handled || accepted != "b" {
		t.Fatalf("tab acceptance: handled=%v accepted=%q", handled, accepted)
	}
	if _, handled := list.Update(sh, keyMsg("esc")); !handled || !cancelled {
		t.Fatalf("escape cancellation: handled=%v cancelled=%v", handled, cancelled)
	}
}

func TestPopupListFiltersAndPreservesSelection(t *testing.T) {
	list := NewPopupList(PopupListOpts[int]{
		Items: []PopupListItem[int]{
			{Label: "apple", Value: 1}, {Label: "apricot", Value: 2}, {Label: "berry", Value: 3},
		},
		MaxVisible: 1,
		MaxWidth:   12,
		Filter: func(query string, item PopupListItem[int]) bool {
			return strings.HasPrefix(item.FilterText, query)
		},
	})
	list.Update(nil, keyMsg("down")) // apricot
	list.SetQuery("ap")
	if list.Len() != 2 || list.sel != 1 {
		t.Fatalf("filter lost surviving selection: len=%d sel=%d", list.Len(), list.sel)
	}
	if view := ansi.Strip(list.View()); !strings.Contains(view, "apricot") || strings.Contains(view, "berry") {
		t.Fatalf("filtered/windowed view is wrong:\n%s", view)
	}
	list.SetQuery("z")
	if list.Len() != 0 || list.View() != "" {
		t.Fatalf("empty filter should hide popup: len=%d view=%q", list.Len(), list.View())
	}
}
