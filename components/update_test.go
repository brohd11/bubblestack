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
