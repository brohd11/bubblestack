package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

	if got := p.List().Paginator.PerPage; got != 3 {
		t.Fatalf("compact per-page count = %d, want 3", got)
	}
	if lines := strings.Split(p.View(false), "\n"); len(lines) != 8 {
		t.Fatalf("compact bordered panel height = %d, want 8", len(lines))
	}
	if v := p.View(false); !strings.Contains(v, "doc0.md  notes/") {
		t.Fatalf("compact row should place suffix on the title line, got:\n%s", v)
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

		for _, y := range []int{0, 1} { // frame, then the list's filter/title row
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != "" {
				t.Fatalf("panel row %d should not pick an item, picked %q", y, picked)
			}
		}
		for y, want := range map[int]string{2: "zero", 3: "one", 4: "two"} {
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != want {
				t.Fatalf("panel row %d picked %q, want %q", y, picked, want)
			}
		}

		// Select moves the paginator to page two. Its first item occupies the
		// same rendered row as page one's first item.
		p.List().Select(p.List().Paginator.PerPage)
		picked = ""
		p.UpdatePanel(sh, click(2))
		if picked != "three" {
			t.Fatalf("page-two first row picked %q, want three", picked)
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
