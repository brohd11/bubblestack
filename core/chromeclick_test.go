package core

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// chromeMaskScreen is a stubScreen that suppresses chrome elements, so a click aimed at
// one must fall through instead of firing.
type chromeMaskScreen struct {
	stubScreen
	mask ChromeMask
}

func (s chromeMaskScreen) ChromeMask() ChromeMask { return s.mask }

// clickTabRouter builds a sized router with two tabs and no header, so the strip sits
// at row 0. The second title contains a space on purpose: spans come from the
// title widths (tabSpans), not from parsing the rendered row. Span math: 1-cell
// strip padding, title + 2 cells each → "One" [1,6), "Two two" [6,15).
func clickTabRouter() tea.Model {
	sh := NewShared(nil)
	sh.Chrome = &Chrome{}
	return sized(NewRouter(sh, []TabEntry{
		{Title: "One", New: func(*Shared) Screen { return stubScreen{} }},
		{Title: "Two two", New: func(*Shared) Screen { return stubScreen{} }},
	}))
}

// TestTabClickSwitchesAndUnwinds: a click on an inactive tab activates it and
// unwinds the live stack to that tab's root in one shot (ShowTab) — from any depth.
func TestTabClickSwitchesAndUnwinds(t *testing.T) {
	tm := clickTabRouter()
	tm, _ = tm.Update(pushMsg{s: stubScreen{}})
	if got := len(tm.(Router).stack); got != 2 {
		t.Fatalf("setup: want stack 2, got %d", got)
	}
	tm, _ = tm.Update(clickAt(8, 0)) // "Two two" — past the space, mid-title
	r := tm.(Router)
	if r.active != 1 {
		t.Fatalf("clicking Two two should activate tab 1, active = %d", r.active)
	}
	if got := len(r.stack); got != 1 {
		t.Fatalf("the switch should unwind to the tab root, want stack 1, got %d", got)
	}
}

// TestTabClickBelowHeader: with a header present the strip moves down by the
// header's height — a click on the header's rows is not a tab click, and the tab
// is found on its shifted row.
func TestTabClickBelowHeader(t *testing.T) {
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Header: NewHeaderPane(func(*Shared) string { return "hdr" })}
	tm := sized(NewRouter(sh, []TabEntry{
		{Title: "One", New: func(*Shared) Screen { return stubScreen{} }},
		{Title: "Two two", New: func(*Shared) Screen { return stubScreen{} }},
	}))
	tm, _ = tm.Update(clickAt(8, 0)) // header row (no OnClick): falls through
	if got := tm.(Router).active; got != 0 {
		t.Fatalf("the header row is not the strip, active should stay 0, got %d", got)
	}
	tm, _ = tm.Update(clickAt(8, 1)) // the strip, shifted one row down
	if got := tm.(Router).active; got != 1 {
		t.Fatalf("clicking the shifted strip should activate tab 1, got %d", got)
	}
}

// TestTabClickDeadZones: the active tab consumes the click but goes nowhere; the
// rule row, the cells past the last tab, a masked strip, and a single-tab router
// (no strip at all) all fall through — active is the observable either way.
func TestTabClickDeadZones(t *testing.T) {
	for name, m := range map[string]tea.MouseMsg{
		"active tab":    clickAt(2, 0),
		"rule row":      clickAt(8, 1),
		"past last tab": clickAt(20, 0),
	} {
		tm := clickTabRouter()
		tm, _ = tm.Update(m)
		if got := tm.(Router).active; got != 0 {
			t.Fatalf("%s: active should stay 0, got %d", name, got)
		}
	}

	tm := clickTabRouter()
	tm, _ = tm.Update(pushMsg{s: chromeMaskScreen{mask: ChromeMask{TabStrip: true}}})
	tm, _ = tm.Update(clickAt(8, 0))
	if got := tm.(Router).active; got != 0 {
		t.Fatalf("masked strip: active should stay 0, got %d", got)
	}

	sh := NewShared(nil)
	sh.Chrome = &Chrome{}
	tm = sized(NewRouter(sh, []TabEntry{{Title: "One", New: func(*Shared) Screen { return stubScreen{} }}}))
	tm, _ = tm.Update(clickAt(2, 0))
	if got := tm.(Router).active; got != 0 {
		t.Fatalf("single tab draws no strip: active should stay 0, got %d", got)
	}
}

// TestHeaderClickFires: a click inside the header box fires OnClick with the
// click's cell coordinates; a click below the box does not.
func TestHeaderClickFires(t *testing.T) {
	var gotX, gotY, calls int
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Header: NewHeaderPane(func(*Shared) string { return "hdr" })}
	sh.Chrome.Header.OnClick = func(_ *Shared, x, y int) Action {
		gotX, gotY, calls = x, y, calls+1
		return Action{}
	}
	tm := sized(NewRouter(sh, []TabEntry{{Title: "One", New: func(*Shared) Screen { return stubScreen{} }}}))
	tm, _ = tm.Update(clickAt(3, 0))
	if calls != 1 || gotX != 3 || gotY != 0 {
		t.Fatalf("OnClick should fire once with (3,0), got calls=%d (%d,%d)", calls, gotX, gotY)
	}
	tm, _ = tm.Update(clickAt(3, 1)) // the header is one row tall
	if calls != 1 {
		t.Fatalf("a click below the header should not fire, calls = %d", calls)
	}
}

// TestHeaderClickGates: a hidden header, a masked header, and a header without an
// OnClick all let the click fall through.
func TestHeaderClickGates(t *testing.T) {
	var calls int
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Header: NewHeaderPane(func(*Shared) string { return "hdr" })}
	sh.Chrome.Header.OnClick = func(_ *Shared, _, _ int) Action { calls++; return Action{} }
	tabs := []TabEntry{{Title: "One", New: func(*Shared) Screen { return stubScreen{} }}}

	tm := sized(NewRouter(sh, tabs))
	tm.(Router).sh.Chrome.Header.Hide()
	tm, _ = tm.Update(clickAt(3, 0))
	if calls != 0 {
		t.Fatalf("a hidden header should not fire, calls = %d", calls)
	}

	tm.(Router).sh.Chrome.Header.Show()
	tm, _ = tm.Update(pushMsg{s: chromeMaskScreen{mask: ChromeMask{Header: true}}})
	tm, _ = tm.Update(clickAt(3, 0))
	if calls != 0 {
		t.Fatalf("a masked header should not fire, calls = %d", calls)
	}

	sh.Chrome.Header.OnClick = nil
	tm = sized(NewRouter(sh, tabs))
	tm, _ = tm.Update(clickAt(3, 0))
	if calls != 0 {
		t.Fatalf("a nil OnClick should not fire, calls = %d", calls)
	}
}
