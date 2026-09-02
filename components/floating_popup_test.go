package components

import (
	"slices"
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

func TestPopupListFuzzyRanksAndPreservesSelection(t *testing.T) {
	list := NewPopupList(PopupListOpts[int]{
		Items: []PopupListItem[int]{
			{Label: "sprint", Value: 1},
			{Label: "private", Value: 2},
			{Label: "print", Value: 3},
			{Label: "unrelated", Value: 4},
		},
		Fuzzy: true,
	})
	list.Select(0)
	list.SetQuery("PRI")
	if list.Len() != 3 || list.visible[0] != 2 {
		t.Fatalf("ranked sources = %v, want print first and three matches", list.visible)
	}
	if list.visible[list.sel] != 0 {
		t.Fatalf("selection did not follow sprint through reranking: visible=%v sel=%d", list.visible, list.sel)
	}

	list.SetQuery("spt") // a non-prefix subsequence, matched case-insensitively
	if list.Len() != 1 || list.visible[0] != 0 {
		t.Fatalf("subsequence sources = %v, want only sprint", list.visible)
	}
	list.SetQuery("")
	if !slices.Equal(list.visible, []int{0, 1, 2, 3}) {
		t.Fatalf("empty query order = %v, want provider order", list.visible)
	}
}

func TestPopupListFuzzyUsesFilterAsPrefilterAndKeepsStableTies(t *testing.T) {
	list := NewPopupList(PopupListOpts[int]{
		Items: []PopupListItem[int]{
			{Label: "first", FilterText: "same", Value: 1},
			{Label: "second", FilterText: "same", Value: 2},
			{Label: "third", FilterText: "same", Value: 3},
		},
		Filter: func(_ string, item PopupListItem[int]) bool { return item.Value != 2 },
		Fuzzy:  true,
	})
	list.SetQuery("sme")
	if !slices.Equal(list.visible, []int{0, 2}) {
		t.Fatalf("filtered stable ties = %v, want [0 2]", list.visible)
	}
}
