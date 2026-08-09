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

var actionsTestPages = []DocPage{{Title: "Getting started"}, {Title: "Controls"}}

func TestActionsMenuStandardRows(t *testing.T) {
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} }, nil)
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

// TestActionsMenuDocsRow: the standard docs row appears after Theme only when the
// app has docs pages — an empty (or nil) page set means no row.
func TestActionsMenuDocsRow(t *testing.T) {
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} }, actionsTestPages)
	items := actionsItems(s)
	if len(items) != 4 || items[1].Name != "? Docs" {
		t.Fatalf("with pages, rows should be Theme, Docs, Update, Refresh — got %+v", items)
	}
	if want := "getting started, controls"; items[1].Desc != want {
		t.Errorf("docs row desc = %q, want %q (derived from the page titles)", items[1].Desc, want)
	}
	// The row pushes the docs index over the given pages.
	if got := reflect.TypeOf(items[1].Pick(core.NewShared(nil)).Msg).String(); got != "core.pushMsg" {
		t.Errorf("the docs row should push its index, got %s", got)
	}

	s = NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} }, []DocPage{})
	if got := len(actionsItems(s)); got != 3 {
		t.Fatalf("no pages, no docs row: rows = %d, want 3", got)
	}
}

func TestActionsMenuExtraRowsAfterTheme(t *testing.T) {
	extra := Item{Name: "✦ Custom", Desc: "app-specific"}
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things", func(*core.Shared) core.Action { return core.Action{} }, nil, extra)
	items := actionsItems(s)
	if len(items) != 4 || items[1].Name != "✦ Custom" {
		t.Fatalf("extra rows should slot in after Theme, got %+v", items)
	}
}

func TestActionsMenuRowActions(t *testing.T) {
	refreshed := false
	s := NewActionsMenu(fakeHooks(SelfUpdateInfo{}, nil), "rescan things",
		func(*core.Shared) core.Action { refreshed = true; return core.SetStatus("refreshed") }, nil)
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

// TestDocsItem pins the standard row's shape: no pages, no row; the desc is the
// lowercased page titles, capped at four topics with a trailing ellipsis.
func TestDocsItem(t *testing.T) {
	if _, ok := DocsItem(nil); ok {
		t.Fatal("no pages should mean no row")
	}

	item, ok := DocsItem(actionsTestPages)
	if !ok {
		t.Fatal("pages should produce a row")
	}
	if item.(Item).Name != "? Docs" {
		t.Errorf("row name = %q, want %q", item.(Item).Name, "? Docs")
	}

	pages := []DocPage{{Title: "One"}, {Title: "Two"}, {Title: "Three"}, {Title: "Four"}, {Title: "Five"}}
	if got, want := docTopics(pages), "one, two, three, four, …"; got != want {
		t.Errorf("docTopics = %q, want %q (capped at four topics)", got, want)
	}
}
