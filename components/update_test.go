package components

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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
		pumpList(l, cmd)
	}
}

// pumpList runs cmd and feeds what it produces back into the list, flattening the
// batches — the filter's matches arrive inside one, so a pump that skips batches
// leaves the list "filtering" over an empty match set, a state no user ever sees.
//
// Each cmd is run under a deadline because the same batch carries the cursor's blink
// TIMER: calling that one straight through would block for the blink interval, and
// following the cmd it produces would loop forever.
func pumpList(l *list.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := runCmd(cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			pumpList(l, c)
		}
		return
	}
	if msg == nil {
		return
	}
	*l, _ = l.Update(msg)
}

// runCmd runs cmd, giving up on one that has not produced a message promptly — a
// timer cmd (the cursor blink) would otherwise stall the test for its whole interval.
func runCmd(cmd tea.Cmd) tea.Msg {
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
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

// ---------- wrap ----------

// wrapList builds a titled list of n items named "item i", sized so every item is on
// one page (wrapping across pages is Select's job, pinned by TestListItemAt).
func wrapList(n int) list.Model {
	items := make([]list.Item, n)
	for i := range items {
		items[i] = Item{Name: fmt.Sprintf("item %d", i)}
	}
	l := core.NewSelectList(items, "T")
	l.SetSize(40, 40)
	return l
}

// TestWrapNav: up on the first row selects the last and down on the last selects the
// first, and a list too short to have two ends does not wrap at all.
func TestWrapNav(t *testing.T) {
	l := wrapList(4)

	if !WrapNav(&l, "up") || l.Index() != 3 {
		t.Fatalf("up at the top should wrap to the last item, got %d", l.Index())
	}
	if !WrapNav(&l, "down") || l.Index() != 0 {
		t.Fatalf("down at the bottom should wrap to the first item, got %d", l.Index())
	}
	// Mid-list the key is not ours: the list moves the cursor itself.
	l.Select(1)
	if WrapNav(&l, "up") || WrapNav(&l, "down") {
		t.Error("mid-list keys must fall through to the list")
	}

	one := wrapList(1)
	if WrapNav(&one, "up") || WrapNav(&one, "down") {
		t.Error("a one-item list has no boundary to wrap at")
	}
}

// TestWrapNavCountsVisibleItems is the reported bug: under a filter the cursor could
// not reach either end. WrapNav counted len(l.Items()) — the WHOLE set — while Index
// and Select are indexed over the visible one, so down at the last match never met
// Index() == n-1 and up at the first ran Select on an index outside the filtered set,
// which left the selection nil and the page blank.
func TestWrapNavCountsVisibleItems(t *testing.T) {
	items := []list.Item{
		Item{Name: "alpha"}, Item{Name: "zulu"}, Item{Name: "alto"},
		Item{Name: "yankee"}, Item{Name: "also"},
	}
	l := core.NewSelectList(items, "T")
	l.SetSize(40, 40)
	startFiltering(&l, "al") // alpha, alto, also — 3 of 5
	var cmd tea.Cmd
	l, cmd = l.Update(keyMsg("enter"))
	pumpList(&l, cmd)
	if l.FilterState() != list.FilterApplied || len(l.VisibleItems()) != 3 {
		t.Fatalf("want 3 matches under an applied filter, got %d in state %v",
			len(l.VisibleItems()), l.FilterState())
	}

	l.Select(2) // the last match
	if !WrapNav(&l, "down") || l.Index() != 0 {
		t.Fatalf("down at the last match should wrap to the first, got %d", l.Index())
	}
	if !WrapNav(&l, "up") || l.Index() != 2 {
		t.Fatalf("up at the first match should wrap to the last, got %d", l.Index())
	}
	// The wrap has to land on a REAL row: an index past the filtered set selects
	// nothing and renders an empty page.
	if l.SelectedItem() == nil {
		t.Fatal("the wrap selected an index outside the filtered set")
	}
}

// TestListDispatchFilteringArrows: ↑/↓ move (and wrap) the cursor while the query is
// still being typed. bubbles' own filtering handler ignores them, so the cursor used to
// be stuck on the first match until the filter was accepted — and WrapNav was never
// even reached, since every message went straight to the list.
func TestListDispatchFilteringArrows(t *testing.T) {
	sh := core.NewShared(nil)
	l := wrapList(4)
	startFiltering(&l, "item") // matches all four
	if l.FilterState() != list.Filtering {
		t.Fatal("the list should still be filtering")
	}

	RootUpdate(sh, &l, keyMsg("down"))
	if l.Index() != 1 {
		t.Fatalf("down while filtering should move the cursor, got %d", l.Index())
	}
	RootUpdate(sh, &l, keyMsg("up"))
	if l.Index() != 0 {
		t.Fatalf("up while filtering should move the cursor back, got %d", l.Index())
	}
	RootUpdate(sh, &l, keyMsg("up"))
	if l.Index() != 3 {
		t.Fatalf("up at the top should wrap while filtering, got %d", l.Index())
	}
	RootUpdate(sh, &l, keyMsg("down"))
	if l.Index() != 0 {
		t.Fatalf("down at the bottom should wrap while filtering, got %d", l.Index())
	}

	// The query is still text: an arrow must not have disturbed it, and letters must
	// still reach the filter input rather than moving anything.
	if got := l.FilterInput.Value(); got != "item" {
		t.Fatalf("the arrows must leave the query alone, got %q", got)
	}
	RootUpdate(sh, &l, keyMsg("0"))
	if got := l.FilterInput.Value(); got != "item0" {
		t.Fatalf("typing should still filter, got %q", got)
	}
}

// ---------- reseeding a filtered list ----------

// docList is a titled list of Item rows named by names.
func docList(names ...string) list.Model {
	items := make([]list.Item, len(names))
	for i, n := range names {
		items[i] = Item{Name: n}
	}
	l := core.NewSelectList(items, "T")
	l.SetSize(40, 40)
	return l
}

func visibleNames(l *list.Model) []string {
	var out []string
	for _, it := range l.VisibleItems() {
		out = append(out, it.(Item).Name)
	}
	return out
}

// TestSetListItemsKeepsAppliedFilter is the "refresh emptied my sidebar" bug. bubbles'
// SetItems nils the match set on the spot and returns the recompute as a cmd; every
// wrapper in this workspace drops that cmd, so the list rendered blank for as long as
// the filter was applied. SetListItems recomputes inline instead.
func TestSetListItemsKeepsAppliedFilter(t *testing.T) {
	l := docList("alpha.md", "also.md", "beta.md")
	startFiltering(&l, "al")
	var cmd tea.Cmd
	l, cmd = l.Update(keyMsg("enter"))
	pumpList(&l, cmd)
	if l.FilterState() != list.FilterApplied || len(l.VisibleItems()) != 2 {
		t.Fatalf("setup: want 2 matches applied, got %d in %v", len(l.VisibleItems()), l.FilterState())
	}

	// A reseed that adds a match and drops one, the shape a refresh takes.
	SetListItems(&l, []list.Item{
		Item{Name: "alpha.md"}, Item{Name: "altimeter.md"}, Item{Name: "beta.md"},
	})

	if got := visibleNames(&l); len(got) != 2 {
		t.Fatalf("the filter should still be applied to the new rows, got %v", got)
	}
	if l.FilterValue() != "al" {
		t.Fatalf("the query should survive a reseed, got %q", l.FilterValue())
	}
	if l.SelectedItem() == nil {
		t.Fatal("a reseeded filtered list must still have a selection")
	}
	for _, name := range visibleNames(&l) {
		if name == "beta.md" {
			t.Error("a non-matching row leaked into the filtered view")
		}
	}
}

// TestSetListItemsKeepsLiveFilter: reseeding while the query is still being TYPED must
// leave the list in Filtering, not silently accept the filter under the user.
func TestSetListItemsKeepsLiveFilter(t *testing.T) {
	l := docList("alpha.md", "beta.md")
	startFiltering(&l, "al")

	SetListItems(&l, []list.Item{Item{Name: "alpha.md"}, Item{Name: "also.md"}, Item{Name: "beta.md"}})

	if l.FilterState() != list.Filtering {
		t.Fatalf("still typing: state should stay Filtering, got %v", l.FilterState())
	}
	if got := visibleNames(&l); len(got) != 2 {
		t.Fatalf("the live query should match the new rows, got %v", got)
	}
	// Typing must still work from here.
	var cmd tea.Cmd
	l, cmd = l.Update(keyMsg("p"))
	pumpList(&l, cmd)
	if got := visibleNames(&l); len(got) != 1 || got[0] != "alpha.md" {
		t.Fatalf("typing after a reseed should narrow further, got %v", got)
	}
}

// TestSetListItemsUnfiltered: no filter, no behavior change.
func TestSetListItemsUnfiltered(t *testing.T) {
	l := docList("a.md", "b.md")
	SetListItems(&l, []list.Item{Item{Name: "c.md"}})
	if got := visibleNames(&l); len(got) != 1 || got[0] != "c.md" {
		t.Fatalf("an unfiltered reseed just replaces the rows, got %v", got)
	}
	if l.FilterState() != list.Unfiltered {
		t.Fatalf("an unfiltered list must stay unfiltered, got %v", l.FilterState())
	}
}

// TestReseedThenReopenFilter is the second half of the report, verbatim: after the
// blanking reseed, pressing / and then an arrow key used to wipe the query, because
// bubbles treats up/down as "accept the filter" and its accept branch resets a filter
// that matches nothing. With the match set intact there is nothing to reset.
func TestReseedThenReopenFilter(t *testing.T) {
	l := docList("alpha.md", "also.md", "beta.md")
	startFiltering(&l, "al")
	var cmd tea.Cmd
	l, cmd = l.Update(keyMsg("enter"))
	pumpList(&l, cmd)

	SetListItems(&l, []list.Item{Item{Name: "alpha.md"}, Item{Name: "also.md"}, Item{Name: "beta.md"}})

	l, cmd = l.Update(keyMsg("/")) // reopen the filter
	pumpList(&l, cmd)
	l, cmd = l.Update(keyMsg("down"))
	pumpList(&l, cmd)

	if l.FilterState() == list.Unfiltered {
		t.Fatal("reopening the filter after a reseed must not reset it")
	}
	if l.FilterValue() != "al" {
		t.Fatalf("the query should still be there, got %q", l.FilterValue())
	}
	if got := visibleNames(&l); len(got) != 2 {
		t.Fatalf("the matches should still be there, got %v", got)
	}
}

// TestSelectByTitleUsesVisibleRows: Select indexes the visible list, so a title scan
// that walked every row put the cursor somewhere unrelated under a filter — the trap
// CycleSort would have stepped on the moment a sort toggle met a filtered list.
func TestSelectByTitleUsesVisibleRows(t *testing.T) {
	l := docList("zulu.md", "yankee.md", "xray.md", "alpha.md", "also.md")
	startFiltering(&l, "al")
	var cmd tea.Cmd
	l, cmd = l.Update(keyMsg("enter"))
	pumpList(&l, cmd)
	if len(l.VisibleItems()) != 2 {
		t.Fatalf("setup: want 2 matches, got %v", visibleNames(&l))
	}

	SelectByTitle(&l, "also.md")
	if it := l.SelectedItem(); it == nil || it.(Item).Name != "also.md" {
		t.Fatalf("the cursor should land on the named visible row, got %v", it)
	}
}

func TestCycleSortPreservesFilterTitleUntilClear(t *testing.T) {
	mode := SortAlpha
	l := docList("alpha.md", "also.md", "beta.md")
	l.Title = SortTitle("Repos", mode)
	l.SetFilterText("al")

	items := func(m SortMode) []list.Item {
		rows := []list.Item{
			Item{Name: "alpha.md"}, Item{Name: "also.md"}, Item{Name: "beta.md"},
		}
		SortItemsByTitle(rows, m == SortReverse)
		return rows
	}
	CycleSort(&l, &mode, []SortMode{SortAlpha, SortReverse}, "Repos", items)

	if mode != SortReverse || l.Title != SortTitle("Repos", SortReverse) {
		t.Fatalf("sort state/title = (%v, %q), want reverse title", mode, l.Title)
	}
	if got := ansi.Strip(core.RenderList(l)); !strings.Contains(got, "Filter: al") || strings.Contains(got, "Z→A") {
		t.Fatalf("active filter should hide the updated sort title, got:\n%s", got)
	}

	l.ResetFilter()
	if got := ansi.Strip(core.RenderList(l)); !strings.Contains(got, "Repos — Z→A") {
		t.Fatalf("clearing the filter should reveal the latest sort title, got:\n%s", got)
	}
}
