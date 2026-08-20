package components

import (
	"fmt"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// clickList builds a titled select list of n items, sized so bubbles computes its
// pagination (PerPage comes out of SetSize, not the constructor).
func clickList(n, w, h int) list.Model {
	items := make([]list.Item, n)
	for i := range items {
		items[i] = Item{Name: fmt.Sprintf("item %d", i)}
	}
	l := core.NewSelectList(items, "Title")
	l.SetSize(w, h)
	return l
}

// startFiltering drives a list into Filtering with query typed into it, pumping the
// filter command the way the router's event loop does — bubbles computes the visible
// set in a cmd, so a list poked into Filtering without it filters to nothing and
// renders a layout no user ever sees.
func startFiltering(l *list.Model, query string) {
	*l, _ = l.Update(keyMsg("/"))
	for _, r := range query {
		var cmd tea.Cmd
		*l, cmd = l.Update(keyMsg(string(r)))
		if cmd == nil {
			continue
		}
		if msg := cmd(); msg != nil {
			*l, _ = l.Update(msg)
		}
	}
}

// TestListItemAt pins the bubbles layout the click math depends on: a TWO-row title
// section (the bar plus TitleBar's bottom padding), then items of delegate Height(2)
// + Spacing(1) rows, indexed across pages via Paginator.Page*PerPage. A bubbles
// upgrade that changes any of these should fail here, not mis-click silently.
//
// The header used to be assumed to be one row, which put every titled list — every
// tab root and picker — one row out.
func TestListItemAt(t *testing.T) {
	l := clickList(10, 80, 24) // PerPage = (24 - title - pagination) / 3 = 7

	if got := listHeaderHeight(&l); got != 2 {
		t.Fatalf("a titled list's header is the bar plus its bottom pad, got %d", got)
	}
	for _, row := range []int{0, 1} {
		if _, ok := listItemAt(&l, row); ok {
			t.Fatalf("row %d is the title section, not an item", row)
		}
	}
	for row, want := range map[int]int{
		2: 0, // title row of item 0
		3: 0, // desc row of item 0
		4: 0, // spacing row still reads as the item above
		5: 1, // first row of item 1
		8: 2,
	} {
		if got, ok := listItemAt(&l, row); !ok || got != want {
			t.Errorf("row %d: got (%d, %v), want (%d, true)", row, got, ok, want)
		}
	}
	// Past the last item (10 items → rows 2..31) is dead space.
	if _, ok := listItemAt(&l, 32); ok {
		t.Fatal("row 32 is past the last item")
	}

	// Page two: the same view row names a different item.
	if _, ok := listItemAt(&l, 2+l.Paginator.PerPage*listItemRows); ok {
		t.Fatal("the row after the current page must not select the next page")
	}
	l.Paginator.Page = 1
	if got, ok := listItemAt(&l, 2); !ok || got != 7 {
		t.Errorf("page 1 row 2: got (%d, %v), want (7, true)", got, ok)
	}
}

// TestListHeaderHeightFollowsFilter is the reported bug: turning the filter on adds
// the input line (and TitleBar's padding with it), so every row slides down — but the
// click math used a constant and kept pointing at the old rows, leaving the hit target
// for the top row sitting on the filter input. Keyboard nav never noticed because it
// does not go through this math.
//
// Applying the filter (enter) takes the input away again, so the header goes back.
func TestListHeaderHeightFollowsFilter(t *testing.T) {
	items := make([]list.Item, 6)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("row %d", i)}
	}
	// The bordered-panel shape: no title of its own (it moves to the border legend),
	// which is what made the one-row assumption look right until a filter opened.
	l := core.NewCompactList(items, "")
	l.SetSize(40, 12)

	if got := listHeaderHeight(&l); got != 1 {
		t.Fatalf("an untitled list's header is the one empty section row, got %d", got)
	}
	if got, ok := listItemAtRows(&l, 1, compactListItemRows); !ok || got != 0 {
		t.Fatalf("unfiltered, row 1 is item 0, got (%d, %v)", got, ok)
	}

	startFiltering(&l, "row")
	if l.FilterState() != list.Filtering {
		t.Fatal("the list should be filtering")
	}
	if got := listHeaderHeight(&l); got != 2 {
		t.Fatalf("the filter input adds a row to the header, got %d", got)
	}
	if _, ok := listItemAtRows(&l, 1, compactListItemRows); ok {
		t.Error("row 1 is the filter input while filtering, not the top item")
	}
	if got, ok := listItemAtRows(&l, 2, compactListItemRows); !ok || got != 0 {
		t.Fatalf("filtering, row 2 is item 0, got (%d, %v)", got, ok)
	}
	// The inverse has to move with it, or the overlay boxes anchored on it drift.
	if row, ok := CompactListItemRow(&l, 0); !ok || row != 2 {
		t.Fatalf("item 0 anchors at row 2 while filtering, got (%d, %v)", row, ok)
	}

	l, _ = l.Update(keyMsg("enter")) // apply the filter: the input goes away
	if got := listHeaderHeight(&l); got != 1 {
		t.Fatalf("an applied filter has no input line, header = %d", got)
	}
	if got, ok := listItemAtRows(&l, 1, compactListItemRows); !ok || got != 0 {
		t.Fatalf("applied, row 1 is item 0 again, got (%d, %v)", got, ok)
	}
}

// TestListItemAtEmpty guards the degenerate list: every click misses.
func TestListItemAtEmpty(t *testing.T) {
	l := clickList(0, 80, 24)
	if _, ok := listItemAt(&l, 1); ok {
		t.Fatal("an empty list has no clickable row")
	}
}

// TestListItemRow pins the inverse of listItemAt over the same bubbles layout
// constants: item idx starts at row header + (idx - Page*PerPage) * itemRows,
// and an item scrolled off the current page (or out of range) reports false.
// A floating editor (LineEditScreen) anchors over the row this returns.
func TestListItemRow(t *testing.T) {
	l := clickList(10, 80, 24) // PerPage = (24 - title - pagination) / 3 = 7

	for idx, want := range map[int]int{0: 2, 1: 5, 2: 8, 6: 20} {
		row, ok := ListItemRow(&l, idx)
		if !ok || row != want {
			t.Errorf("idx %d: got (%d, %v), want (%d, true)", idx, row, ok, want)
		}
		// Inverse consistency: the row maps back to the item.
		if back, ok := listItemAt(&l, row); !ok || back != idx {
			t.Errorf("idx %d: listItemAt(row %d) = (%d, %v), want (%d, true)", idx, row, back, ok, idx)
		}
	}
	// Off-page and out-of-range items have no row on screen.
	for _, idx := range []int{-1, 7, 10} {
		if _, ok := ListItemRow(&l, idx); ok {
			t.Errorf("idx %d: got ok=true, want false (off-page or out of range)", idx)
		}
	}

	// Page two: item 7 is the first row of the page.
	l.Paginator.Page = 1
	if got, ok := ListItemRow(&l, 7); !ok || got != 2 {
		t.Errorf("page 1 idx 7: got (%d, %v), want (2, true)", got, ok)
	}
}

func TestCompactListGeometry(t *testing.T) {
	items := make([]list.Item, 10)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("item %d", i)}
	}
	l := core.NewCompactList(items, "Title")
	l.SetSize(80, 8)

	if got := l.Paginator.PerPage; got != 4 {
		t.Fatalf("compact PerPage = %d, want 4", got)
	}
	header := listHeaderHeight(&l) // titled, so the bar plus its bottom pad
	if header != 2 {
		t.Fatalf("a titled compact list's header = %d, want 2", header)
	}
	for idx := 0; idx < l.Paginator.PerPage; idx++ {
		row, ok := CompactListItemRow(&l, idx)
		if !ok || row != header+idx {
			t.Fatalf("idx %d row = (%d, %v), want (%d, true)", idx, row, ok, header+idx)
		}
		if back, ok := listItemAtRows(&l, row, compactListItemRows); !ok || back != idx {
			t.Fatalf("row %d maps back to (%d, %v), want (%d, true)", row, back, ok, idx)
		}
	}
	if _, ok := listItemAtRows(&l, header+l.Paginator.PerPage, compactListItemRows); ok {
		t.Fatal("compact pagination/blank row must not select the next page")
	}
	firstNextPage := l.Paginator.PerPage
	l.Paginator.Page = 1
	if row, ok := CompactListItemRow(&l, firstNextPage); !ok || row != header {
		t.Fatalf("page-two first row = (%d, %v), want (%d, true)", row, ok, header)
	}
}
