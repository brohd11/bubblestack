package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type compactTestItem struct{ title, suffix string }

func (i compactTestItem) Title() string       { return i.title }
func (i compactTestItem) SuffixText() string  { return i.suffix }
func (i compactTestItem) FilterValue() string { return i.title }

// TestListPanelBorder: with ListPanelOpts.Border the title moves out of the list's
// own title bar and into the frame's legend, and the list is sized to the inner run
// so the framed panel still fills its allocation exactly.
func TestListPanelBorder(t *testing.T) {
	items := []list.Item{Item{Name: "one"}, Item{Name: "two"}}
	p := NewListPanel(items, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 10)

	if p.List().ShowTitle() {
		t.Fatal("a bordered panel should hide the list's own title bar")
	}
	v := p.View(false)
	if !strings.HasPrefix(v, "┌─ Docs ") {
		t.Fatalf("bordered View should open with the title legend, got %q", strings.SplitN(v, "\n", 2)[0])
	}
	if w := lipgloss.Width(v); w != 30 {
		t.Fatalf("bordered View width = %d, want the allocated 30", w)
	}
	if h := lipgloss.Height(v); h != 10 {
		t.Fatalf("bordered View height = %d, want the allocated 10", h)
	}
	if !strings.Contains(v, "one") {
		t.Fatal("the rows should still render inside the frame")
	}
}

// TestListPanelBorderlessByDefault is the no-regression guard for existing sidebars
// (gitstack's tags panel): without the opt-in the panel renders the bare list, title
// bar and all.
func TestListPanelBorderlessByDefault(t *testing.T) {
	p := NewListPanel([]list.Item{Item{Name: "one"}}, "Tags", ListPanelOpts{})
	p.SetSize(30, 10)

	if !p.List().ShowTitle() {
		t.Fatal("an unbordered panel keeps the list's own title bar")
	}
	v := p.View(false)
	if strings.Contains(v, "┌") {
		t.Fatal("an unbordered panel must draw no frame")
	}
	if !strings.Contains(v, "Tags") {
		t.Fatal("the title should still show in the list's title bar")
	}
	if v != p.List().View() {
		t.Fatal("an unbordered panel should render the bare list")
	}
}

// TestListPanelFocusTint: the View arg selects the frame's color. The tint itself
// can't be asserted headless (lipgloss strips color without a TTY — see
// TestFrameColorTracksFocus), so this pins the geometry that must not change with it.
func TestListPanelFocusTint(t *testing.T) {
	p := NewListPanel([]list.Item{Item{Name: "one"}}, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 10)
	focused, blurred := p.View(true), p.View(false)
	if lipgloss.Width(focused) != lipgloss.Width(blurred) ||
		lipgloss.Height(focused) != lipgloss.Height(blurred) {
		t.Fatal("focus must not change the framed panel's footprint")
	}
	if !strings.HasPrefix(focused, "┌─ Docs ") {
		t.Fatal("the legend should survive the focused tint")
	}
}

func TestCompactListPanelRowsAndPagination(t *testing.T) {
	items := make([]list.Item, 10)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("doc%d.md", i), suffix: "notes/"}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 8)

	// 8 outer rows − 2 frame edges = 6 for the list: five entries and the pagination
	// dots. Neither bubbles' empty title section nor its forced pre-pagination margin
	// gets a row anymore (this was originally only three entries).
	if got := p.List().Paginator.PerPage; got != 5 {
		t.Fatalf("compact per-page count = %d, want 5", got)
	}
	if lines := strings.Split(p.View(false), "\n"); len(lines) != 8 {
		t.Fatalf("compact bordered panel height = %d, want 8", len(lines))
	} else {
		if !strings.Contains(lines[5], "doc4.md") {
			t.Fatalf("the fifth row should use the reclaimed pagination-margin row, got %q", lines[5])
		}
		if !strings.Contains(lines[6], "•") && !strings.Contains(lines[6], "○") {
			t.Fatalf("pagination should immediately follow the entries, got %q", lines[6])
		}
	}
	// The footprint must not change when the filter line appears — the list gives up a
	// row for it, rather than the panel growing past the height the layout assigned.
	startFiltering(p.List(), "doc")
	p.sizeList()
	if lines := strings.Split(p.View(false), "\n"); len(lines) != 8 {
		t.Fatalf("filtered compact panel height = %d, want 8", len(lines))
	}
	if got := p.List().Paginator.PerPage; got != 4 {
		t.Fatalf("the filter line should cost the list a row: per-page = %d, want 4", got)
	}
	p.List().ResetFilter()
	p.sizeList()
	if v := p.View(false); !strings.Contains(v, "doc0.md  notes/") {
		t.Fatalf("compact row should place suffix on the title line, got:\n%s", v)
	}
}

// TestCompactListPanelFilterPaginationCrossing is the reported regression: a compact
// list filtered down to one page and then widened back to many pages used the old
// one-row pagination shape to compute PerPage. Its next View was one row taller than
// the panel, which ultimately made Bubble Tea discard the breadcrumb row above it.
func TestCompactListPanelFilterPaginationCrossing(t *testing.T) {
	items := make([]list.Item, 27)
	for i := range items {
		prefix := "dd"
		if i < 4 {
			prefix = "ddd"
		}
		items[i] = compactTestItem{title: fmt.Sprintf("%s-%02d.txt", prefix, i)}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	sh := core.NewShared(nil)
	const width, height = 30, 10
	p.SetSize(width, height)

	drive := func(k string) {
		t.Helper()
		act, handled := p.UpdatePanel(sh, keyMsg(k))
		if !handled {
			t.Fatalf("filter key %q was not handled", k)
		}
		pumpList(p.List(), act.Cmd)
		// This is the router's resize-after-every-message contract.
		p.SetSize(width, height)
		if got := lipgloss.Height(p.View(true)); got != height {
			t.Fatalf("query %q rendered %d rows, want %d", p.List().FilterValue(), got, height)
		}
	}

	drive("/")
	for _, k := range []string{"d", "d", "d"} {
		drive(k)
	}
	if got := len(p.List().VisibleItems()); got != 4 {
		t.Fatalf("narrow query matched %d rows, want 4", got)
	}
	if got := p.List().Paginator.TotalPages; got != 1 {
		t.Fatalf("narrow query has %d pages, want 1", got)
	}

	drive("backspace")
	if got := len(p.List().VisibleItems()); got != 27 {
		t.Fatalf("widened query matched %d rows, want 27", got)
	}
	if got := p.List().Paginator.TotalPages; got < 2 {
		t.Fatalf("widened query has %d pages, want at least 2", got)
	}
}

// TestCompactListPanelHeightSweep walks every visible count across the one-page
// boundary in both directions. Prefix length controls the count exactly: among
// a, aa, aaa, ... a query of length q matches n-q+1 rows.
func TestCompactListPanelHeightSweep(t *testing.T) {
	const n, width, height = 16, 30, 10
	items := make([]list.Item, n)
	for i := range items {
		items[i] = compactTestItem{title: strings.Repeat("a", i+1)}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	sh := core.NewShared(nil)
	p.SetSize(width, height)

	drive := func(k string, wantVisible int) {
		t.Helper()
		act, _ := p.UpdatePanel(sh, keyMsg(k))
		pumpList(p.List(), act.Cmd)
		p.SetSize(width, height)
		if got := len(p.List().VisibleItems()); got != wantVisible {
			t.Fatalf("query %q matched %d rows, want %d", p.List().FilterValue(), got, wantVisible)
		}
		if got := lipgloss.Height(p.View(true)); got != height {
			t.Fatalf("query %q with %d matches rendered %d rows, want %d",
				p.List().FilterValue(), wantVisible, got, height)
		}
	}

	drive("/", n)
	for q := 1; q <= n; q++ {
		drive("a", n-q+1)
	}
	for q := n - 1; q >= 1; q-- {
		drive("backspace", n-q+1)
	}
	drive("backspace", n)
}

func TestCompactListPanelHeightAfterSetItems(t *testing.T) {
	p := NewCompactListPanel([]list.Item{
		compactTestItem{title: "one"},
		compactTestItem{title: "two"},
	}, "Docs", ListPanelOpts{Border: true})
	const height = 10
	p.SetSize(30, height)

	items := make([]list.Item, 30)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("doc-%02d", i)}
	}
	p.SetItems(items)
	if got := lipgloss.Height(p.View(true)); got != height {
		t.Fatalf("panel after SetItems rendered %d rows, want %d", got, height)
	}
	if got := p.List().Paginator.TotalPages; got < 2 {
		t.Fatalf("grown list has %d pages, want at least 2", got)
	}
}

// TestListPanelClampsOvertallList pins the panel boundary independently of FitList:
// even if the embedded list's pagination state is stale, the panel must keep its
// allocation so ModularScreen's rendered hit rectangles remain truthful.
func TestListPanelClampsOvertallList(t *testing.T) {
	items := make([]list.Item, 20)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("doc-%02d", i)}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	const height = 10
	p.SetSize(30, height)
	p.List().Paginator.PerPage += 3 // synthesize an upstream over-render

	raw := frame(p.title, p.List().View(), p.innerWidth(), true)
	if got := lipgloss.Height(raw); got <= height {
		t.Fatalf("test setup did not produce an over-tall list: got %d rows", got)
	}
	v := p.View(true)
	if got := lipgloss.Height(v); got != height {
		t.Fatalf("clamped panel rendered %d rows, want %d", got, height)
	}
	if !strings.HasPrefix(v, "┌─ Docs ") {
		t.Fatal("height clamping must preserve the panel's top edge")
	}
}

func TestBorderedListPanelMouseRows(t *testing.T) {
	sh := core.NewShared(nil)
	click := func(y int) tea.MouseMsg {
		return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: y}
	}

	t.Run("compact", func(t *testing.T) {
		items := []list.Item{
			compactTestItem{title: "zero"},
			compactTestItem{title: "one"},
			compactTestItem{title: "two"},
			compactTestItem{title: "three"},
		}
		picked := ""
		p := NewCompactListPanel(items, "Docs", ListPanelOpts{
			Border: true,
			OnSelect: func(_ *core.Shared, item list.Item) core.Action {
				picked = item.(compactTestItem).title
				return core.Action{}
			},
		})
		p.SetSize(30, 8)
		p.Focus()

		picked = ""
		p.UpdatePanel(sh, click(0)) // the frame's top edge, and nothing else above the rows
		if picked != "" {
			t.Fatalf("panel row 0 is the frame, picked %q", picked)
		}
		for y, want := range map[int]string{1: "zero", 2: "one", 3: "two"} {
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != want {
				t.Fatalf("panel row %d picked %q, want %q", y, picked, want)
			}
		}

		// Select moves the paginator to page two. Its first item occupies the
		// same rendered row as page one's first item. Shortened first, since four
		// compact rows now fit on one page in an 8-row panel.
		p.SetSize(30, 5)
		perPage := p.List().Paginator.PerPage
		if perPage >= len(items) {
			t.Fatalf("the panel should be too short for one page: per-page = %d", perPage)
		}
		p.List().Select(perPage)
		picked = ""
		p.UpdatePanel(sh, click(1))
		if want := items[perPage].(compactTestItem).title; picked != want {
			t.Fatalf("page-two first row picked %q, want %q", picked, want)
		}
	})

	// The reported bug, at the panel level: opening the filter pushes every row down
	// one, and the click regions have to follow. Before this, the row that still picked
	// the top item was the one the filter input had just moved onto.
	t.Run("compact while filtering", func(t *testing.T) {
		items := []list.Item{
			compactTestItem{title: "alpha"},
			compactTestItem{title: "beta"},
			compactTestItem{title: "gamma"},
		}
		picked := ""
		p := NewCompactListPanel(items, "Docs", ListPanelOpts{
			Border: true,
			OnSelect: func(_ *core.Shared, item list.Item) core.Action {
				picked = item.(compactTestItem).title
				return core.Action{}
			},
		})
		p.SetSize(30, 8)
		p.Focus()

		l := p.List()
		startFiltering(l, "a") // matches all three
		if l.FilterState() != list.Filtering {
			t.Fatal("the panel's list should be filtering")
		}

		for _, y := range []int{0, 1} { // the frame, then the panel's own filter line
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != "" {
				t.Fatalf("panel row %d is chrome while filtering, picked %q", y, picked)
			}
		}
		// The rows are the MATCHES in match order (bubbles ranks them), so the
		// expectation comes from the visible set rather than from the item order.
		visible := l.VisibleItems()
		if len(visible) != 3 {
			t.Fatalf("want 3 matches, got %d", len(visible))
		}
		for i, item := range visible {
			picked = ""
			p.UpdatePanel(sh, click(2+i))
			if want := item.(compactTestItem).title; picked != want {
				t.Fatalf("filtering, panel row %d picked %q, want %q", 2+i, picked, want)
			}
		}
	})

	t.Run("standard", func(t *testing.T) {
		items := []list.Item{Item{Name: "zero"}, Item{Name: "one"}}
		picked := ""
		p := NewListPanel(items, "Docs", ListPanelOpts{
			Border: true,
			OnSelect: func(_ *core.Shared, item list.Item) core.Action {
				picked = item.(Item).Name
				return core.Action{}
			},
		})
		p.SetSize(30, 12)
		p.Focus()
		for y, want := range map[int]string{2: "zero", 5: "one"} {
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != want {
				t.Fatalf("panel row %d picked %q, want %q", y, picked, want)
			}
		}
	})
}

// filterPanel is a bordered compact panel of named rows, sized and focused.
func filterPanel(t *testing.T, names ...string) *CompactListPanel {
	t.Helper()
	items := make([]list.Item, len(names))
	for i, n := range names {
		items[i] = compactTestItem{title: n}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 10)
	p.Focus()
	return p
}

// TestCompactPanelFilterLine: the panel draws the filter itself, so it survives the
// enter that accepts it. bubbles draws the filter only while it is being TYPED and the
// status bar that would name an applied one is off framework-wide — so an accepted
// filter used to leave the sidebar with rows missing and nothing saying why, which is
// how you set one and forget it.
func TestCompactPanelFilterLine(t *testing.T) {
	p := filterPanel(t, "notes.md", "note-2.md", "todo.md")
	l := p.List()

	if got := ansi.Strip(p.View(true)); strings.Contains(got, "Filter") {
		t.Fatalf("an unfiltered panel shows no filter line; got:\n%s", got)
	}

	startFiltering(l, "note")
	if got := ansi.Strip(p.filterLine()); !strings.Contains(got, "Filter: note") {
		t.Fatalf("while typing, the line shows the query; got %q", got)
	}

	var cmd tea.Cmd
	*l, cmd = l.Update(keyMsg("enter"))
	pumpList(l, cmd)
	if l.FilterState() != list.FilterApplied {
		t.Fatalf("enter should apply the filter, got %v", l.FilterState())
	}
	if got := ansi.Strip(p.filterLine()); got != "  Filter: note" {
		t.Fatalf("an applied filter stays on screen; got %q", got)
	}
	if got := ansi.Strip(p.View(true)); !strings.Contains(got, "Filter: note") ||
		strings.Contains(got, "todo.md") {
		t.Fatalf("the panel should show the filter and hide the non-matches; got:\n%s", got)
	}
	// The prompt keeps the color bubbles gives it — that yellow is what makes the line
	// read as a filter rather than as a row.
	if !strings.Contains(p.filterLine(), l.FilterInput.PromptStyle.Render(l.FilterInput.Prompt)) {
		t.Error("the applied line should carry the filter prompt's own styling")
	}
}

// TestCompactPanelFilterLineTruncates: filterRows promises exactly one row, and the
// click math is built on that — a query too wide for the column must not wrap into a
// second one.
func TestCompactPanelFilterLineTruncates(t *testing.T) {
	p := filterPanel(t, "aaaa.md")
	l := p.List()
	// The value is set directly rather than typed: this is a width assertion, and the
	// match set is beside the point.
	*l, _ = l.Update(keyMsg("/"))
	l.FilterInput.SetValue(strings.Repeat("a", 60))
	if got := lipgloss.Height(p.filterLine()); got != 1 {
		t.Fatalf("the filter line is one row, got %d", got)
	}
	if got := lipgloss.Width(p.filterLine()); got > p.listWidth() {
		t.Fatalf("filter line width %d overflows the list column %d", got, p.listWidth())
	}
}

// TestCompactPanelEscClearsAppliedFilter: esc is the host's pop, carved out of the
// panel's dispatch — but with a filter merely APPLIED that carve-out made bubbles' own
// ClearFilter binding unreachable, so a filtered list had no way out of its filter.
// Now that the filter is permanently on screen, that is the first thing a user reaches
// for. Unfiltered, esc must still fall through to the host.
func TestCompactPanelEscClearsAppliedFilter(t *testing.T) {
	sh := core.NewShared(nil)
	p := filterPanel(t, "notes.md", "todo.md")
	l := p.List()

	if _, handled := p.UpdatePanel(sh, keyMsg("esc")); handled {
		t.Fatal("unfiltered, esc belongs to the host screen")
	}

	startFiltering(l, "note")
	var cmd tea.Cmd
	*l, cmd = l.Update(keyMsg("enter"))
	pumpList(l, cmd)

	if _, handled := p.UpdatePanel(sh, keyMsg("esc")); !handled {
		t.Fatal("esc should clear an applied filter instead of popping the screen")
	}
	if l.FilterState() != list.Unfiltered {
		t.Fatalf("the filter should be gone, state = %v", l.FilterState())
	}
	if got := ansi.Strip(p.View(true)); !strings.Contains(got, "todo.md") {
		t.Fatalf("clearing should bring the rows back; got:\n%s", got)
	}
}

// TestCompactPanelRowY: RowY is the anchor an overlay drawn over a row needs — the
// frame edge and the filter line included. It must agree with the click mapping in
// every filter state, since the two are the same geometry read in opposite directions.
func TestCompactPanelRowY(t *testing.T) {
	sh := core.NewShared(nil)
	picked := ""
	items := []list.Item{
		compactTestItem{title: "alpha"}, compactTestItem{title: "also"},
		compactTestItem{title: "beta"},
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{
		Border: true,
		OnSelect: func(_ *core.Shared, it list.Item) core.Action {
			picked = it.(compactTestItem).title
			return core.Action{}
		},
	})
	p.SetSize(30, 10)
	p.Focus()
	l := p.List()

	check := func(label string) {
		t.Helper()
		for i, item := range l.VisibleItems() {
			row, ok := p.RowY(i)
			if !ok {
				t.Fatalf("%s: item %d has no row", label, i)
			}
			picked = ""
			p.UpdatePanel(sh, tea.MouseMsg{
				Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: row,
			})
			if want := item.(compactTestItem).title; picked != want {
				t.Fatalf("%s: RowY(%d) = %d selects %q, want %q", label, i, row, picked, want)
			}
		}
	}
	check("unfiltered")
	startFiltering(l, "al")
	p.sizeList()
	check("filtering")
	var cmd tea.Cmd
	*l, cmd = l.Update(keyMsg("enter"))
	pumpList(l, cmd)
	p.sizeList()
	check("applied")
}

// marqueeRows: a row that overflows a 30-cell sidebar (26 cells of text) and one that
// doesn't, so a test can move the cursor between the two states.
func marqueeRows() []list.Item {
	return []list.Item{
		compactTestItem{title: "architecture-notes.md", suffix: "design/deep/"},
		compactTestItem{title: "a.md", suffix: "x/"},
	}
}

// armedMarquee returns a sized, focused compact panel whose Init armed the marquee.
func armedMarquee(t *testing.T) (*CompactListPanel, tea.Cmd) {
	t.Helper()
	p := NewCompactListPanel(marqueeRows(), "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 8)
	p.Focus()
	cmd := p.Init(core.NewShared(nil))
	if cmd == nil {
		t.Fatal("a compact panel must arm its marquee at Init")
	}
	return p, cmd
}

// tick feeds the panel the message its armed cmd would eventually deliver, and reports
// whether it re-armed — i.e. whether the marquee is still running. It synthesizes the
// message rather than running cmd, which would really sleep marqueeInterval: a full cycle
// is ~40 frames, and TestMarqueeDelivers already pins that the cmd delivers this exact
// message. Only the non-nil-ness of cmd is read here, never its timer.
func tick(t *testing.T, p *CompactListPanel, cmd tea.Cmd) (tea.Cmd, int) {
	t.Helper()
	if cmd == nil {
		t.Fatal("no tick pending — the marquee stopped earlier than the test expected")
	}
	act, handled := p.UpdatePanel(core.NewShared(nil), marqueeTickMsg{id: p.marqueeID})
	if !handled {
		t.Fatal("a marquee tick must be consumed by the panel")
	}
	return act.Cmd, p.marquee
}

// TestMarqueeDelivers is the one test that runs the timer, pinning what the rest assume:
// an armed cmd delivers a tick stamped with this panel's own clock id.
func TestMarqueeDelivers(t *testing.T) {
	p, cmd := armedMarquee(t)
	msg, ok := cmd().(marqueeTickMsg)
	if !ok {
		t.Fatalf("armed cmd delivered %T, want marqueeTickMsg", msg)
	}
	if msg.id != p.marqueeID {
		t.Errorf("tick id = %d, want this panel's %d", msg.id, p.marqueeID)
	}
}

// TestMarqueeRunsAndDwells: a focused panel whose selected row overflows keeps re-arming,
// holds at the left edge before moving, walks one cell per frame, and snaps back to 0
// after dwelling on the tail — so the whole name and path are eventually readable.
func TestMarqueeRunsAndDwells(t *testing.T) {
	p, cmd := armedMarquee(t)

	max, ok := p.marqueeOverflow()
	if !ok {
		t.Fatal("the fixture row must overflow a 30-cell sidebar")
	}

	// The leading dwell: marqueeHold frames before the first cell of movement.
	for i := 0; i < marqueeHold; i++ {
		cmd, _ = tick(t, p, cmd)
		if cmd == nil {
			t.Fatalf("marquee stopped during the leading hold at frame %d", i)
		}
		if p.marquee != 0 {
			t.Fatalf("frame %d moved to %d during the leading hold, want 0", i, p.marquee)
		}
	}
	for want := 1; want <= max; want++ {
		var off int
		if cmd, off = tick(t, p, cmd); off != want {
			t.Fatalf("marquee at %d, want one cell per frame (%d)", off, want)
		}
	}
	// Dwell on the tail, then back to the left edge to start over.
	for i := 0; i < marqueeHold; i++ {
		if cmd, _ = tick(t, p, cmd); p.marquee != max {
			t.Fatalf("frame %d left the tail dwell early (offset %d, max %d)", i, p.marquee, max)
		}
	}
	if cmd, _ = tick(t, p, cmd); p.marquee != 0 {
		t.Fatalf("after the tail dwell the marquee should snap back to 0, got %d", p.marquee)
	}
	if cmd == nil {
		t.Fatal("the marquee should still be running after a full cycle")
	}
}

// TestMarqueeForeignTickIgnored is the doubling guard. ModularScreen broadcasts non-key
// messages to EVERY panel, so a sibling's tick lands here too — acting on it would re-arm
// a second clock on this panel and the tick rate would double on every pass.
func TestMarqueeForeignTickIgnored(t *testing.T) {
	p, _ := armedMarquee(t)
	act, handled := p.UpdatePanel(core.NewShared(nil), marqueeTickMsg{id: p.marqueeID + 1})
	if !handled {
		t.Fatal("a foreign tick should still be consumed, not passed on")
	}
	if act.Cmd != nil {
		t.Fatal("a foreign tick must not re-arm this panel's marquee")
	}
}

// TestMarqueeStopsUnfocused: the loop is self-limiting. Focus moving to a sibling pane
// (gote's editor) ends it and returns the row to its left edge, and focus coming back
// picks it up again on the next message.
func TestMarqueeStopsUnfocused(t *testing.T) {
	p, cmd := armedMarquee(t)
	for i := 0; i < marqueeHold+2; i++ {
		cmd, _ = tick(t, p, cmd)
	}
	if p.marquee == 0 {
		t.Fatal("the fixture should have moved off the left edge by now")
	}

	p.Blur()
	if cmd, _ = tick(t, p, cmd); cmd != nil {
		t.Fatal("an unfocused panel must stop re-arming the marquee")
	}
	if p.marquee != 0 {
		t.Fatalf("stopping should reset the row to its left edge, got offset %d", p.marquee)
	}

	// Regaining focus restarts it THERE AND THEN, via FocusNotifier — no unrelated
	// keystroke needed, which is the whole point of the hook.
	p.Focus()
	if cmd := p.OnFocus(); cmd == nil {
		t.Fatal("OnFocus should re-arm the marquee immediately on regaining focus")
	}
}

// TestMarqueeOnFocusIdempotent: the hook and the per-message fallback both route through
// marqueeStart, so whichever fires first wins and the second is a no-op. Two clocks on one
// panel would share an id and double the tick rate on every pass.
func TestMarqueeOnFocusIdempotent(t *testing.T) {
	p, _ := armedMarquee(t) // Init already armed it
	if cmd := p.OnFocus(); cmd != nil {
		t.Fatal("OnFocus over an already-running marquee must not arm a second clock")
	}
	act := p.marqueeArm(core.Action{})
	if act.Cmd != nil {
		t.Fatal("the per-message fallback must not arm a second clock either")
	}
}

// TestMarqueeNoFocusCmdWhenNothingToScroll: focus alone doesn't start the clock — an
// unfocused panel and a row that fits both stay still.
func TestMarqueeNoFocusCmdWhenNothingToScroll(t *testing.T) {
	p := NewCompactListPanel(marqueeRows(), "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 8)

	if cmd := p.OnFocus(); cmd != nil {
		t.Error("an unfocused panel must not arm on OnFocus")
	}
	p.Focus()
	p.List().Select(1) // the short row
	if cmd := p.OnFocus(); cmd != nil {
		t.Error("a selected row that fits must not arm the marquee")
	}
}

// TestMarqueeResetsOnCursorMove: each row starts from its left edge, and a row that fits
// stops the loop rather than jittering in place.
func TestMarqueeResetsOnCursorMove(t *testing.T) {
	p, cmd := armedMarquee(t)
	for i := 0; i < marqueeHold+2; i++ {
		cmd, _ = tick(t, p, cmd)
	}
	if p.marquee == 0 {
		t.Fatal("the fixture should have moved off the left edge by now")
	}

	p.List().Select(1) // the short row
	p.UpdatePanel(core.NewShared(nil), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	if p.marquee != 0 {
		t.Fatalf("a cursor move should restart the row from its left edge, got %d", p.marquee)
	}
	if _, ok := p.marqueeOverflow(); ok {
		t.Fatal("the short row must not report overflow")
	}
	if cmd, _ = tick(t, p, cmd); cmd != nil {
		t.Fatal("a row that fits must not keep the marquee running")
	}
}

// TestMarqueeOffForPlainListPanel: only the compact panel marquees; a NewListPanel keeps
// the two-line delegate and no clock at all.
func TestMarqueeOffForPlainListPanel(t *testing.T) {
	p := NewListPanel([]list.Item{Item{Name: "one"}}, "Tags", ListPanelOpts{})
	p.SetSize(30, 10)
	if cmd := p.Init(core.NewShared(nil)); cmd != nil {
		t.Fatal("a plain list panel should arm no marquee")
	}
	if _, ok := p.marqueeOverflow(); ok {
		t.Fatal("a plain list panel should never report marquee overflow")
	}
}

// TestMarqueeInitIdempotent: gote rebuilds its ModularScreen on every sidebar toggle while
// keeping the same panels, so Init can run again over a live clock. A second arm would
// share the first's id and double the tick rate on each pass.
func TestMarqueeInitIdempotent(t *testing.T) {
	p, _ := armedMarquee(t)
	if cmd := p.Init(core.NewShared(nil)); cmd != nil {
		t.Fatal("a second Init over a running marquee must not arm a second clock")
	}
}
