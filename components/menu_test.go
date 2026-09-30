package components

import (
	"fmt"
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/bubblestack/tuitest"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// newMenu builds a menu already sized to an 80x20 body. Shared.bodyY is router-owned
// and unexported, so it is always 0 in a components test — the vertical cases below
// therefore state their anchors and body heights explicitly rather than leaning on it.
func newMenu(t *testing.T, opts MenuOpts) (*MenuScreen, *core.Shared) {
	t.Helper()
	m := NewMenu(opts)
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 20)
	return m, sh
}

// menuItems is the fixture the dimension/click tests share: two plain rows, a rule, and
// a fourth row, so separator handling is exercised everywhere by default.
func menuItems() []MenuItem {
	return []MenuItem{
		{Label: "Open", Hint: "enter"},
		{Label: "Rename", Hint: "r"},
		{Separator: true},
		{Label: "Delete", Hint: "d"},
	}
}

// press is tuitest.Press under the name every test in this package already calls.
func press(x, y int, b tea.MouseButton) tea.MouseMsg { return tuitest.Press(x, y, b) }

// TestMenuBoxDims pins the invariant every other behavior rests on: what place() reports
// is exactly what View() renders. Click hit-testing subtracts the placed position from
// absolute mouse coordinates, so a one-cell disagreement between the two lands clicks on
// the wrong row with nothing else failing.
func TestMenuBoxDims(t *testing.T) {
	// Widest row is "Open" + gap + "enter" = 4+2+5 = 11.
	m, sh := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(0, 0)})
	_, _, w, h := m.place()
	if w != 11+menuChromeW || h != 4+menuChromeH {
		t.Fatalf("place() = %dx%d, want %dx%d", w, h, 11+menuChromeW, 4+menuChromeH)
	}
	view := m.View(sh)
	if got := lipgloss.Width(view); got != w {
		t.Errorf("View width = %d, place() width = %d", got, w)
	}
	if got := lipgloss.Height(view); got != h {
		t.Errorf("View height = %d, place() height = %d", got, h)
	}

	// A title adds one row and does not widen the box when it is narrower than the rows.
	mt, sht := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(0, 0), Title: "Actions"})
	_, _, tw, th := mt.place()
	if tw != w || th != h+1 {
		t.Errorf("titled box = %dx%d, want %dx%d", tw, th, w, h+1)
	}
	if got := lipgloss.Height(mt.View(sht)); got != th {
		t.Errorf("titled View height = %d, want %d", got, th)
	}
}

// TestMenuTruncation: both caps (MaxWidth and the terminal) squeeze the content width,
// and the box stays exactly contentW+menuChromeW wide however hard it is squeezed.
func TestMenuTruncation(t *testing.T) {
	long := []MenuItem{{Label: "AVeryLongLabelIndeed"}}

	m, sh := newMenu(t, MenuOpts{Items: long, Anchor: AnchorAt(0, 0), MaxWidth: 8})
	_, _, w, _ := m.place()
	if w != 8+menuChromeW {
		t.Fatalf("MaxWidth 8 gave width %d, want %d", w, 8+menuChromeW)
	}
	view := m.View(sh)
	if !strings.Contains(view, "…") {
		t.Errorf("an over-long label should be truncated with an ellipsis:\n%s", view)
	}
	if got := lipgloss.Width(view); got != w {
		t.Errorf("View width = %d, place() width = %d", got, w)
	}

	// A narrow terminal clamps just the same, with no MaxWidth in play.
	narrow := NewMenu(MenuOpts{Items: long, Anchor: AnchorAt(0, 0)})
	shn := core.NewShared(nil)
	narrow.SetSize(shn, 10, 20)
	_, _, nw, _ := narrow.place()
	if nw != 10 {
		t.Errorf("a 10-cell terminal gave width %d, want 10", nw)
	}
	if got := lipgloss.Width(narrow.View(shn)); got != nw {
		t.Errorf("View width = %d, place() width = %d", got, nw)
	}

	// A hint is dropped whole rather than truncated when the field cannot hold it plus
	// a cell of label.
	tight, sht := newMenu(t, MenuOpts{
		Items:    []MenuItem{{Label: "Open", Hint: "enter"}},
		Anchor:   AnchorAt(0, 0),
		MaxWidth: 5,
	})
	if strings.Contains(tight.View(sht), "enter") {
		t.Errorf("a hint that cannot fit should be dropped, not truncated:\n%s", tight.View(sht))
	}
}

// TestMenuFlipAbove: the box opens below its anchor when there is room, flips so its
// bottom row sits at FlipY-1 when there is not, and pins to the body top when it cannot
// fit either way.
func TestMenuFlipAbove(t *testing.T) {
	m, _ := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorBelow(0, 4)})
	_, y, _, h := m.place()
	if y != 5 {
		t.Fatalf("with room below, y = %d, want 5 (the row under the widget)", y)
	}

	// Body rows are 0..19; a 6-row box anchored at row 19 cannot open downward.
	flip, _ := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorBelow(0, 18)})
	_, fy, _, fh := flip.place()
	if bottom := fy + fh - 1; bottom != 17 {
		t.Fatalf("flipped box bottom row = %d, want 17 (FlipY-1)", bottom)
	}
	if fh != h {
		t.Errorf("flipping should not resize the box: %d vs %d", fh, h)
	}

	// A body too short for the box at all: pinned to the body's first row.
	tiny := NewMenu(MenuOpts{Items: menuItems(), Anchor: AnchorBelow(0, 1)})
	sht := core.NewShared(nil)
	tiny.SetSize(sht, 80, 2)
	_, ty, _, th := tiny.place()
	if ty != 0 {
		t.Errorf("a box taller than the body should pin to bodyY, got y = %d (h = %d)", ty, th)
	}
}

// TestMenuFlipLeft is the horizontal half: right edge at FlipX-1 on a flip, and x = 0
// when even the flipped box overflows.
func TestMenuFlipLeft(t *testing.T) {
	m, _ := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(70, 0)})
	x, _, w, _ := m.place()
	if right := x + w - 1; right != 70 {
		t.Fatalf("flipped box right edge = %d, want 70 (FlipX-1, back over the pointer)", right)
	}

	wide := NewMenu(MenuOpts{Items: menuItems(), Anchor: AnchorAt(70, 0)})
	shw := core.NewShared(nil)
	wide.SetSize(shw, 10, 20) // the box fills the whole terminal width
	wx, _, ww, _ := wide.place()
	if wx != 0 || ww != 10 {
		t.Errorf("a box as wide as the terminal should sit at x = 0 with w = 10, got x = %d w = %d", wx, ww)
	}
}

// TestMenuScrollWindow: a menu taller than the body scrolls a window instead of
// overflowing, the window follows the cursor, and the markers ride the gutter column the
// box widens to make room for.
func TestMenuScrollWindow(t *testing.T) {
	var items []MenuItem
	for i := 0; i < 10; i++ {
		items = append(items, MenuItem{Label: fmt.Sprintf("item%d", i)})
	}

	full, _ := newMenu(t, MenuOpts{Items: items, Anchor: AnchorAt(0, 0)}) // 20 rows of body: no window
	_, _, fullW, fullH := full.place()
	if fullH != 10+menuChromeH {
		t.Fatalf("an unconstrained menu should show every row, h = %d", fullH)
	}

	m := NewMenu(MenuOpts{Items: items, Anchor: AnchorAt(0, 0)})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 4+menuChromeH) // room for exactly 4 rows
	_, _, w, h := m.place()
	if h != 4+menuChromeH {
		t.Fatalf("windowed height = %d, want %d", h, 4+menuChromeH)
	}
	if w != fullW+menuGutterW {
		t.Errorf("the scroll gutter should widen the box by %d: %d vs %d", menuGutterW, w, fullW)
	}
	if view := m.View(sh); !strings.Contains(view, "↓") || strings.Contains(view, "↑") {
		t.Errorf("at the top of a windowed menu only the down marker should show:\n%s", view)
	}

	// Walking past the window's last row drags the window down with the cursor.
	for i := 0; i < 4; i++ {
		m.Update(sh, keyMsg("j"))
	}
	if m.Selected() != 4 || m.top != 1 {
		t.Fatalf("after 4 downs: sel = %d top = %d, want 4 and 1", m.Selected(), m.top)
	}
	if view := m.View(sh); !strings.Contains(view, "↑") || !strings.Contains(view, "↓") {
		t.Errorf("mid-list both markers should show:\n%s", view)
	}
	if got := lipgloss.Height(m.View(sh)); got != h {
		t.Errorf("scrolling must not change the box height: %d vs %d", got, h)
	}

	// Wrapping from the last row back to the first resets the window too.
	m.Update(sh, keyMsg("G"))
	if m.Selected() != 9 || m.top != 6 {
		t.Fatalf("Bottom: sel = %d top = %d, want 9 and 6", m.Selected(), m.top)
	}
	m.Update(sh, keyMsg("j"))
	if m.Selected() != 0 || m.top != 0 {
		t.Errorf("wrapping to the first row should reset the window: sel = %d top = %d", m.Selected(), m.top)
	}
}

// TestMenuNavSkips: separators and disabled rows are never landed on, in either
// direction or across the wrap, and a menu with nothing selectable stays inert.
func TestMenuNavSkips(t *testing.T) {
	items := []MenuItem{
		{Label: "A"},
		{Separator: true},
		{Label: "B", Disabled: true},
		{Label: "C"},
	}
	m, sh := newMenu(t, MenuOpts{Items: items, Anchor: AnchorAt(0, 0)})
	if m.Selected() != 0 {
		t.Fatalf("cursor should start on the first selectable row, got %d", m.Selected())
	}
	m.Update(sh, keyMsg("j"))
	if m.Selected() != 3 {
		t.Errorf("down should skip the separator and the disabled row, landed on %d", m.Selected())
	}
	m.Update(sh, keyMsg("j"))
	if m.Selected() != 0 {
		t.Errorf("down from the last row should wrap to the first, landed on %d", m.Selected())
	}
	m.Update(sh, keyMsg("k"))
	if m.Selected() != 3 {
		t.Errorf("up from the first row should wrap to the last selectable, landed on %d", m.Selected())
	}

	inert, shi := newMenu(t, MenuOpts{
		Items:  []MenuItem{{Separator: true}, {Label: "x", Disabled: true}},
		Anchor: AnchorAt(0, 0),
	})
	if inert.Selected() != -1 {
		t.Errorf("a menu with no selectable row should report -1, got %d", inert.Selected())
	}
	if _, act := inert.Update(shi, keyMsg("enter")); act.Msg != nil || act.Cmd != nil {
		t.Errorf("enter on an inert menu should do nothing, got %#v", act)
	}
}

// TestMenuClickRows walks a click across every cell region of the box: each item row
// fires its own Pick, the border and the separator swallow the click, and one cell past
// any edge dismisses.
func TestMenuClickRows(t *testing.T) {
	var picked []string
	items := menuItems()
	for i := range items {
		label := items[i].Label
		if items[i].Separator {
			continue
		}
		items[i].Pick = func(*core.Shared) core.Action {
			picked = append(picked, label)
			return core.Action{}
		}
	}
	cancelled := false
	m, sh := newMenu(t, MenuOpts{
		Items:    items,
		Anchor:   AnchorAt(10, 3),
		OnCancel: func(*core.Shared) core.Action { cancelled = true; return core.Action{} },
	})
	x, y, w, h := m.place()
	top := m.contentTop()

	for i, want := range []string{"Open", "Rename", "", "Delete"} {
		picked = nil
		m.Update(sh, press(x+2, top+i, tea.MouseLeft))
		got := strings.Join(picked, ",")
		if got != want {
			t.Errorf("click on row %d picked %q, want %q", i, got, want)
		}
	}
	if cancelled {
		t.Fatal("clicks inside the box must never dismiss it")
	}

	// The chrome rows are part of the menu: they consume the click without acting.
	picked = nil
	m.Update(sh, press(x+2, y, tea.MouseLeft))
	if len(picked) != 0 || cancelled {
		t.Errorf("a click on the top border should be swallowed, picked = %v cancelled = %v", picked, cancelled)
	}

	for _, c := range []struct {
		name string
		x, y int
	}{
		{"left of", x - 1, top},
		{"right of", x + w, top},
		{"above", x + 2, y - 1},
		{"below", x + 2, y + h},
	} {
		cancelled = false
		m.Update(sh, press(c.x, c.y, tea.MouseLeft))
		if !cancelled {
			t.Errorf("a click %s the box should dismiss it", c.name)
		}
	}
}

// TestMenuDismissGestures covers the ways a menu goes away, and the convention that the
// callback — not the menu — decides what dismissal means.
func TestMenuDismissGestures(t *testing.T) {
	m, sh := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(10, 3)})
	if _, act := m.Update(sh, keyMsg("esc")); !reflect.DeepEqual(act.Msg, core.Pop().Msg) {
		t.Errorf("esc with no OnCancel should pop, got %#v", act.Msg)
	}

	// A right press inside the box dismisses: the gesture that raises a context menu is
	// also the one that closes it.
	x, _, _, _ := m.place()
	if _, act := m.Update(sh, press(x+2, m.contentTop(), tea.MouseRight)); !reflect.DeepEqual(act.Msg, core.Pop().Msg) {
		t.Errorf("a right click should dismiss, got %#v", act.Msg)
	}

	// Motion and release are not gestures the menu acts on.
	moved := tea.MouseMotionMsg{X: 0, Y: 0, Button: tea.MouseLeft}
	if _, act := m.Update(sh, moved); act.Msg != nil || act.Cmd != nil {
		t.Errorf("a non-press mouse action should be ignored, got %#v", act)
	}

	// A custom OnCancel is returned verbatim — the menu adds no navigation of its own.
	custom, shc := newMenu(t, MenuOpts{
		Items:    menuItems(),
		Anchor:   AnchorAt(0, 0),
		OnCancel: func(*core.Shared) core.Action { return core.PopTo() },
	})
	if _, act := custom.Update(shc, keyMsg("esc")); !reflect.DeepEqual(act.Msg, core.PopTo().Msg) {
		t.Errorf("OnCancel's Action should be returned as-is, got %#v", act.Msg)
	}

	// And a Pick that returns nothing leaves the menu exactly where it was.
	stay, shs := newMenu(t, MenuOpts{
		Items:  []MenuItem{{Label: "A", Pick: func(*core.Shared) core.Action { return core.Action{} }}},
		Anchor: AnchorAt(0, 0),
	})
	if _, act := stay.Update(shs, keyMsg("enter")); act.Msg != nil || act.Cmd != nil {
		t.Errorf("the menu must not pop itself on select, got %#v", act)
	}
}

// TestMenuWheel: the wheel moves the cursor and drags the window, but clamps at the ends
// rather than wrapping — the arrows wrap, the wheel does not (WheelNav's convention).
func TestMenuWheel(t *testing.T) {
	var items []MenuItem
	for i := 0; i < 10; i++ {
		items = append(items, MenuItem{Label: fmt.Sprintf("item%d", i)})
	}
	m := NewMenu(MenuOpts{Items: items, Anchor: AnchorAt(0, 0)})
	sh := core.NewShared(nil)
	m.SetSize(sh, 80, 4+menuChromeH)

	for i := 0; i < 4; i++ {
		m.Update(sh, press(0, 0, tea.MouseWheelDown))
	}
	if m.Selected() != 4 || m.top != 1 {
		t.Fatalf("wheel down: sel = %d top = %d, want 4 and 1", m.Selected(), m.top)
	}
	for i := 0; i < 20; i++ {
		m.Update(sh, press(0, 0, tea.MouseWheelDown))
	}
	if m.Selected() != 9 {
		t.Errorf("the wheel should clamp at the last row, got %d", m.Selected())
	}
	for i := 0; i < 20; i++ {
		m.Update(sh, press(0, 0, tea.MouseWheelUp))
	}
	if m.Selected() != 0 || m.top != 0 {
		t.Errorf("the wheel should clamp at the first row, got sel = %d top = %d", m.Selected(), m.top)
	}
}

// TestMenuCascade drives a real router: a submenu pushed from an item's Pick lands beside
// its parent row, and esc closes exactly one level. core.Push's payload is unexported, so
// the only way to see where a pushed screen went is to let the router put it there.
func TestMenuCascade(t *testing.T) {
	sub := []MenuItem{{Label: "asc"}, {Label: "desc"}}
	var parent *MenuScreen
	parent = NewMenu(MenuOpts{
		Anchor: AnchorAt(10, 3),
		Items: []MenuItem{
			{Label: "one"},
			{Label: "Sort", Hint: "›", Pick: func(*core.Shared) core.Action {
				return core.Push(NewMenu(MenuOpts{Anchor: parent.ChildAnchor(), Items: sub}))
			}},
		},
	})

	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{
		Title: "T", New: func(*core.Shared) core.Screen { return stubRootScreen{} },
	}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(core.Push(parent))
	if _, ok := tm.(core.Router).Top().(*MenuScreen); !ok {
		t.Fatalf("the parent menu should be on top, got %T", tm.(core.Router).Top())
	}

	px, _, pw, _ := parent.place()
	parentRow := parent.contentTop() + 1 // the "Sort" row

	tm, _ = tm.Update(keyMsg("j"))
	tm, _ = tm.Update(keyMsg("enter"))
	child, ok := tm.(core.Router).Top().(*MenuScreen)
	if !ok || child == parent {
		t.Fatalf("enter on a submenu row should push the child, top is %T", tm.(core.Router).Top())
	}
	cx, cy, _, _ := child.place()
	if cx != px+pw-1 {
		t.Errorf("child x = %d, want %d (its left border on the parent's right)", cx, px+pw-1)
	}
	if cy != parentRow {
		t.Errorf("child y = %d, want %d (the parent row it opened from)", cy, parentRow)
	}

	tm, _ = tm.Update(keyMsg("esc"))
	if top := tm.(core.Router).Top(); top != core.Screen(parent) {
		t.Errorf("esc should close one level and reveal the parent, got %T", top)
	}
}

// TestMenuCapabilities pins the opt-in interfaces the router asserts for. Filtering in
// particular is load-bearing: without it the router's global q/t/o keys would fire while
// a menu is up.
func TestMenuCapabilities(t *testing.T) {
	m, sh := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(4, 6)})
	if !m.IsOverlay() {
		t.Error("a menu must be an overlay")
	}
	if !m.Filtering() {
		t.Error("a menu must report Filtering so global single-key shortcuts stay off")
	}
	if m.HelpView(sh) != "" {
		t.Error("a menu's hints live in its box; the background help bar stays")
	}
	if act, handled := m.QuitGate(sh); !handled || !reflect.DeepEqual(act.Msg, core.Pop().Msg) {
		t.Errorf("QuitGate = (%#v, %v), want a handled pop", act.Msg, handled)
	}
	x, y, _, _ := m.place()
	if ox, oy := m.OverlayPos(0, 0); ox != x || oy != y {
		t.Errorf("OverlayPos = (%d,%d), place() = (%d,%d) — they must agree", ox, oy, x, y)
	}
}

// TestMenuNoCrumb: a dropdown is not a navigation step, so it must leave the trail
// reading as the screen it floats over — the LineEditScreen stance (lineedit_test.go).
func TestMenuNoCrumb(t *testing.T) {
	m, _ := newMenu(t, MenuOpts{Items: menuItems(), Anchor: AnchorAt(0, 0)})
	if c, ok := any(m).(core.Crumber); ok {
		t.Errorf("a menu must contribute no breadcrumb segment, got %q", c.CrumbLabel(false))
	}
}

// TestMenuRowStyling needs a real color profile: under `go test` lipgloss resolves the
// Ascii profile and renders every Foreground as a no-op, which would make a selected row
// byte-identical to an unselected one.
func TestMenuRowStyling(t *testing.T) {
	m, sh := newMenu(t, MenuOpts{
		Items: []MenuItem{
			{Label: "Same", Hint: "x"},
			{Label: "Same", Hint: "x"},
			{Label: "Same", Hint: "x", Disabled: true},
		},
		Anchor: AnchorAt(0, 0),
	})
	rows := strings.Split(m.View(sh), "\n")
	if len(rows) != 5 { // top border, 3 items, bottom border
		t.Fatalf("expected 5 rendered rows, got %d", len(rows))
	}
	if rows[1] == rows[2] {
		t.Error("the selected row should be styled differently from an unselected one")
	}
	if rows[2] == rows[3] {
		t.Error("a disabled row should be styled differently from an enabled one")
	}
}

// TestMenuAnchorHelpers checks the flip edges each helper implies — the difference
// between "flip back over the pointer" and "flip clear of the widget" is entirely in
// these four numbers.
func TestMenuAnchorHelpers(t *testing.T) {
	if got, want := AnchorAt(5, 7), (MenuAnchor{X: 5, Y: 7, FlipX: 6, FlipY: 8}); got != want {
		t.Errorf("AnchorAt = %+v, want %+v", got, want)
	}
	if got, want := AnchorBelow(5, 7), (MenuAnchor{X: 5, Y: 8, FlipX: 6, FlipY: 7}); got != want {
		t.Errorf("AnchorBelow = %+v, want %+v", got, want)
	}

	l := newList(Item{Name: "a"}, Item{Name: "b"})
	row, ok := ListItemRow(&l, 1)
	if !ok {
		t.Fatal("item 1 should be on the first page; fixture is wrong")
	}
	a, ok := AnchorListRow(&l, 1, 4, 10)
	if !ok {
		t.Fatal("AnchorListRow should report ok for an on-page item")
	}
	want := MenuAnchor{X: 4, Y: 10 + row + listItemRows, FlipX: 5, FlipY: 10 + row}
	if a != want {
		t.Errorf("AnchorListRow = %+v, want %+v", a, want)
	}
	if _, ok := AnchorListRow(&l, 99, 0, 0); ok {
		t.Error("an off-page index should report ok = false")
	}

	cl := core.NewCompactList([]list.Item{CompactItem{Name: "a"}}, "")
	cl.SetSize(40, 10)
	crow, _ := CompactListItemRow(&cl, 0)
	ca, ok := AnchorCompactListRow(&cl, 0, 2, 5)
	if !ok {
		t.Fatal("AnchorCompactListRow should report ok for an on-page item")
	}
	cwant := MenuAnchor{X: 2, Y: 5 + crow + compactListItemRows, FlipX: 3, FlipY: 5 + crow}
	if ca != cwant {
		t.Errorf("AnchorCompactListRow = %+v, want %+v", ca, cwant)
	}
}

// quitGateRoot is a base screen that answers the quit gate, so a test can tell whether the
// router's walk reached past the menus stacked above it.
type quitGateRoot struct{ gated *int }

func (quitGateRoot) Init(*core.Shared) tea.Cmd { return nil }
func (r quitGateRoot) Update(*core.Shared, tea.Msg) (core.Screen, core.Action) {
	return r, core.Action{}
}
func (quitGateRoot) View(*core.Shared) string       { return "root" }
func (quitGateRoot) HelpView(*core.Shared) string   { return "" }
func (quitGateRoot) SetSize(*core.Shared, int, int) {}
func (r quitGateRoot) QuitGate(*core.Shared) (core.Action, bool) {
	*r.gated++
	return core.Action{}, true // consumed without quitting, so the test can keep driving
}

// TestMenuQuitGateClosesTheMenu: ctrl+c runs ahead of the Filtering gate that stops every
// other global key, so without a gate of its own a menu would let the walk reach the screen
// below and stack that screen's confirm on top of itself. Each press must instead unwind one
// menu, and only then reach the base.
func TestMenuQuitGateClosesTheMenu(t *testing.T) {
	gated := 0
	root := quitGateRoot{gated: &gated}
	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{
		Title: "T", New: func(*core.Shared) core.Screen { return root },
	}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	parent := NewMenu(MenuOpts{Items: menuItems(), Anchor: AnchorAt(4, 4)})
	child := NewMenu(MenuOpts{Items: menuItems(), Anchor: AnchorAt(20, 4)})
	tm, _ = tm.Update(core.Push(parent))
	tm, _ = tm.Update(core.Push(child))

	ctrlC := keyMsg("ctrl+c")

	// A nil cmd is the half that proves nothing quit: an ungated quit comes back with
	// tea.Quit in the cmd lane.
	tm, cmd := tm.Update(ctrlC)
	if cmd != nil {
		t.Error("the first ctrl+c should close the submenu, not quit")
	}
	if top := tm.(core.Router).Top(); top != core.Screen(parent) {
		t.Fatalf("after one ctrl+c the parent menu should be on top, got %T", top)
	}

	tm, cmd = tm.Update(ctrlC)
	if cmd != nil {
		t.Error("the second ctrl+c should close the parent menu, not quit")
	}
	if _, still := tm.(core.Router).Top().(*MenuScreen); still {
		t.Fatal("the second ctrl+c should have closed the last menu")
	}
	if gated != 0 {
		t.Fatalf("the base gate answered %d times while menus were up; they must answer first", gated)
	}

	// With the menus gone the walk reaches the base, which is what makes this a delay
	// rather than a way to suppress a host's unsaved-changes confirm.
	tm.Update(ctrlC)
	if gated != 1 {
		t.Errorf("with the menus gone the base gate should have answered once, ran %d times", gated)
	}
}

// TestMenuStyle: the zero Style is the accent look; SelectBackground fills the selected
// row with SelectionColor (text un-accented with NoAccent), and FocusLegend draws the
// border in BorderColor.
func TestMenuStyle(t *testing.T) {
	items := []MenuItem{{Label: "One", Hint: "x"}, {Label: "Two"}}
	plain, sh := newMenu(t, MenuOpts{Items: items, Anchor: AnchorAt(0, 0)})
	styled, _ := newMenu(t, MenuOpts{Items: items, Anchor: AnchorAt(0, 0), Style: MenuStyle{
		Selection: core.SelectionOpts{Style: core.SelectBackground, NoAccent: true},
		Focus:     FocusLegend,
	}})
	pv, sv := plain.View(sh), styled.View(sh)
	if ansi.Strip(pv) != ansi.Strip(sv) {
		t.Fatalf("style must not change the layout:\n%s\n%s", ansi.Strip(pv), ansi.Strip(sv))
	}

	bg := lipgloss.NewStyle().Background(core.SelectionColor).Render(" ")
	bg = bg[:strings.Index(bg, " ")] // the background's opening sequence
	selRow := strings.Split(sv, "\n")[1]
	if !strings.Contains(selRow, bg) {
		t.Fatalf("SelectBackground should put SelectionColor on the selected row: %q", selRow)
	}
	if strings.Contains(strings.Split(sv, "\n")[2], bg) {
		t.Fatal("only the selected row takes the background")
	}
	if strings.Contains(selRow, core.AccentStyle().Render("One")) {
		t.Fatal("NoAccent should keep the selected label un-accented")
	}

	border := func(v string, c color.Color) bool {
		want := lipgloss.NewStyle().Foreground(c).Render("╭")
		return strings.HasPrefix(v, want[:strings.Index(want, "╭")])
	}
	if !border(pv, core.FocusedColor) || !border(sv, core.BorderColor) {
		t.Fatalf("border colors: default should be the accent, FocusLegend the border color")
	}
}

// motion is a free pointer move to (x, y), as all-motion reporting delivers it.
func motion(x, y int) tea.MouseMotionMsg { return tea.MouseMotionMsg{X: x, Y: y} }

// TestMenuHover: the highlight follows the pointer; hovering a Submenu row opens it in the
// parent's style; moving back onto another parent row closes the child, while its own
// parent row keeps it open.
func TestMenuHover(t *testing.T) {
	style := MenuStyle{Focus: FocusLegend}
	parent := NewMenu(MenuOpts{Anchor: AnchorAt(10, 3), Style: style, Items: []MenuItem{
		{Label: "one"},
		{Label: "Sort", Submenu: func() []MenuItem { return []MenuItem{{Label: "asc"}, {Label: "desc"}} }},
		{Label: "three"},
	}})
	if parent.items[1].Hint != "›" {
		t.Fatalf("a Submenu row should default its hint to ›, got %q", parent.items[1].Hint)
	}
	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{Title: "T", New: func(*core.Shared) core.Screen { return stubRootScreen{} }}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(core.Push(parent))
	if tm.(core.Router).View().MouseMode != tea.MouseModeAllMotion {
		t.Fatal("a menu on top should ask for all-motion reporting")
	}

	px, _, _, _ := parent.place()
	top := parent.contentTop()
	tm, _ = tm.Update(motion(px+2, top+2)) // "three"
	if parent.Selected() != 2 || tm.(core.Router).Top() != core.Screen(parent) {
		t.Fatalf("hover should select the row under the pointer, sel %d", parent.Selected())
	}
	tm, _ = tm.Update(motion(px+2, top+1)) // "Sort"
	child, ok := tm.(core.Router).Top().(*MenuScreen)
	if !ok || child == parent || child.style != style {
		t.Fatalf("hovering a submenu row should open it in the parent's style, top %T", tm.(core.Router).Top())
	}
	cx, _, _, _ := child.place()
	ctop := child.contentTop()
	tm, _ = tm.Update(motion(cx+2, ctop+1)) // "desc" in the child
	if child.Selected() != 1 {
		t.Fatal("hover inside the child should move its highlight")
	}
	tm, _ = tm.Update(motion(px+2, top+1)) // back on "Sort", the row that opened it
	if tm.(core.Router).Top() != core.Screen(child) {
		t.Fatal("the pointer on the child's own parent row must keep it open")
	}
	tm, _ = tm.Update(motion(px+2, top)) // "one"
	if tm.(core.Router).Top() != core.Screen(parent) {
		t.Fatal("moving onto another parent row should close the child")
	}
	tm, _ = tm.Update(motion(px+2, top))
	if parent.Selected() != 0 {
		t.Fatal("the parent should take the next motion")
	}
	tm, _ = tm.Update(motion(70, 20)) // far outside every box
	if tm.(core.Router).Top() != core.Screen(parent) {
		t.Fatal("motion must never dismiss a menu")
	}

	// A fresh menu's first row is already selected; entering it still opens it.
	fresh := NewMenu(MenuOpts{Anchor: AnchorAt(10, 3), Items: []MenuItem{
		{Label: "Sort", Submenu: func() []MenuItem { return []MenuItem{{Label: "asc"}} }},
	}})
	fresh.SetSize(sh, 80, 20)
	fx, _, _, _ := fresh.place()
	if _, act := fresh.Update(sh, motion(fx+2, fresh.contentTop())); act.Msg == nil {
		t.Fatal("entering an already-selected submenu row should open it")
	}
	if _, act := fresh.Update(sh, motion(fx+3, fresh.contentTop())); act.Msg != nil {
		t.Fatal("moving within the same row must not open it again (esc would never stick)")
	}

	// → opens a submenu from the keyboard.
	parent.Select(1)
	tm, _ = tm.Update(keyMsg("right"))
	if next, ok := tm.(core.Router).Top().(*MenuScreen); !ok || next == parent {
		t.Fatal("→ on a submenu row should open it")
	}
	tm, _ = tm.Update(keyMsg("esc"))
	tm, _ = tm.Update(keyMsg("esc"))
	if tm.(core.Router).View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("with no menu up the router should be back to cell motion")
	}
}

// TestMenuPointerOutside: positions outside every box of the cascade go to the root's
// OnPointerOutside. Taken, the whole cascade closes before the host's action runs (a menu
// bar switching menus); declined, motion does nothing and a click dismisses.
func TestMenuPointerOutside(t *testing.T) {
	var take bool
	other := NewMenu(MenuOpts{Anchor: AnchorAt(40, 3), Items: []MenuItem{{Label: "other"}}})
	root := NewMenu(MenuOpts{
		Anchor: AnchorAt(10, 3),
		Items:  []MenuItem{{Label: "Sort", Submenu: func() []MenuItem { return []MenuItem{{Label: "asc"}} }}},
		OnPointerOutside: func(_ *core.Shared, x, y int) (core.Action, bool) {
			return core.Push(other), take && y == 0
		},
	})
	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{Title: "T", New: func(*core.Shared) core.Screen { return stubRootScreen{} }}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(core.Push(root))
	tm, _ = tm.Update(keyMsg("right")) // open the submenu: the hook must reach through it
	child, _ := tm.(core.Router).Top().(*MenuScreen)
	if child == root || child == nil {
		t.Fatal("setup: the submenu should be open")
	}

	tm, _ = tm.Update(motion(60, 0)) // declined
	if tm.(core.Router).Top() != core.Screen(child) {
		t.Fatal("declined motion outside must leave the cascade alone")
	}
	take = true
	tm, _ = tm.Update(motion(60, 0))
	if top := tm.(core.Router).Top(); top != core.Screen(other) {
		t.Fatalf("taken motion should close the cascade and run the host action, top %T", top)
	}
	tm, _ = tm.Update(core.Pop())
	if _, isMenu := tm.(core.Router).Top().(*MenuScreen); isMenu {
		t.Fatal("the old cascade must be gone, not buried under the new menu")
	}

	// Declined clicks still dismiss.
	take = false
	tm, _ = tm.Update(core.Push(root))
	tm, _ = tm.Update(press(60, 0, tea.MouseLeft))
	if _, isMenu := tm.(core.Router).Top().(*MenuScreen); isMenu {
		t.Fatal("a declined click outside should dismiss the menu")
	}
}
