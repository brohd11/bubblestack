package components

import (
	"fmt"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
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

// TestListItemAt pins the bubbles layout the click math depends on: one title
// row, then items of delegate Height(2) + Spacing(1) rows, indexed across pages
// via Paginator.Page*PerPage. A bubbles upgrade that changes any of these
// should fail here, not mis-click silently.
func TestListItemAt(t *testing.T) {
	l := clickList(10, 80, 24) // PerPage = (24 - title - pagination) / 3 = 7

	if _, ok := listItemAt(&l, 0); ok {
		t.Fatal("row 0 is the title bar, not an item")
	}
	for row, want := range map[int]int{
		1: 0, // title row of item 0
		2: 0, // desc row of item 0
		3: 0, // spacing row still reads as the item above
		4: 1, // first row of item 1
		7: 2,
	} {
		if got, ok := listItemAt(&l, row); !ok || got != want {
			t.Errorf("row %d: got (%d, %v), want (%d, true)", row, got, ok, want)
		}
	}
	// Past the last item (10 items → rows 1..30) is dead space.
	if _, ok := listItemAt(&l, 31); ok {
		t.Fatal("row 31 is past the last item")
	}

	// Page two: the same view row names a different item.
	if _, ok := listItemAt(&l, 1+l.Paginator.PerPage*listItemRows); ok {
		t.Fatal("the row after the current page must not select the next page")
	}
	l.Paginator.Page = 1
	if got, ok := listItemAt(&l, 1); !ok || got != 7 {
		t.Errorf("page 1 row 1: got (%d, %v), want (7, true)", got, ok)
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

	for idx, want := range map[int]int{0: 1, 1: 4, 2: 7, 6: 19} {
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
	if got, ok := ListItemRow(&l, 7); !ok || got != 1 {
		t.Errorf("page 1 idx 7: got (%d, %v), want (1, true)", got, ok)
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
	for idx := 0; idx < l.Paginator.PerPage; idx++ {
		row, ok := CompactListItemRow(&l, idx)
		if !ok || row != 1+idx {
			t.Fatalf("idx %d row = (%d, %v), want (%d, true)", idx, row, ok, 1+idx)
		}
		if back, ok := listItemAtRows(&l, row, compactListItemRows); !ok || back != idx {
			t.Fatalf("row %d maps back to (%d, %v), want (%d, true)", row, back, ok, idx)
		}
	}
	if _, ok := listItemAtRows(&l, 1+l.Paginator.PerPage, compactListItemRows); ok {
		t.Fatal("compact pagination/blank row must not select the next page")
	}
	firstNextPage := l.Paginator.PerPage
	l.Paginator.Page = 1
	if row, ok := CompactListItemRow(&l, firstNextPage); !ok || row != 1 {
		t.Fatalf("page-two first row = (%d, %v), want (1, true)", row, ok)
	}
}
