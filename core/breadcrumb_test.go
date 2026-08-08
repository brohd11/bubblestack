package core

import (
	"slices"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// crumbScreen is a stubScreen with a breadcrumb segment, so a test can click it.
type crumbScreen struct {
	stubScreen
	full, short string
}

func (s crumbScreen) CrumbLabel(shortForm bool) string {
	if shortForm {
		return s.short
	}
	return s.full
}

// clickAt builds a left-press mouse msg at terminal cell (x, y).
func clickAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
}

// crumbClickRouter pushes "Git" then "Tags" over the (crumbless) stub root, so the
// bar reads " Git › Tags" at row 0 — no header in the test chrome, one tab, so no
// rows above it. Span math: 1-cell padding, 3-cell separators → Git [1,4), Tags [7,11).
func crumbClickRouter(t *testing.T) tea.Model {
	t.Helper()
	tm := sized(newCoreTestRouter())
	tm, _ = tm.Update(pushMsg{s: crumbScreen{full: "Git", short: "Git"}})
	tm, _ = tm.Update(pushMsg{s: crumbScreen{full: "Tags", short: "Tags"}})
	return tm
}

// TestBreadcrumbClickPops: a click on an upstream segment pops the stack back to it.
func TestBreadcrumbClickPops(t *testing.T) {
	tm := crumbClickRouter(t)
	if got := len(tm.(Router).stack); got != 3 {
		t.Fatalf("setup: want stack 3, got %d", got)
	}
	tm, _ = tm.Update(clickAt(2, 0)) // "Git"
	if got := len(tm.(Router).stack); got != 2 {
		t.Fatalf("clicking Git should pop Tags, want stack 2, got %d", got)
	}
}

// TestBreadcrumbClickCurrentIsNoop: the current segment consumes the click but
// goes nowhere; separators and the rule row below the bar aren't clickable at
// all (the msg falls through to the screen, which the stub ignores — the stack
// is the observable either way).
func TestBreadcrumbClickCurrentIsNoop(t *testing.T) {
	tm := crumbClickRouter(t)
	for name, m := range map[string]tea.MouseMsg{
		"current segment": clickAt(8, 0),
		"separator":       clickAt(5, 0),
		"rule row":        clickAt(2, 1),
	} {
		tm, _ = tm.Update(m)
		if got := len(tm.(Router).stack); got != 3 {
			t.Fatalf("%s: stack should stay 3, got %d", name, got)
		}
	}
}

// TestCrumbSpans pins the span math against the renderer's label choice,
// including the truncated fallback refusing spans entirely.
func TestCrumbSpans(t *testing.T) {
	crumbs := []Crumb{{Full: "one"}, {Full: "two"}}
	spans, ok := crumbSpans(crumbs, 80)
	if !ok {
		t.Fatal("a fitting trail should produce spans")
	}
	want := []crumbSpan{{1, 4}, {7, 10}}
	if !slices.Equal(spans, want) {
		t.Fatalf("spans = %v, want %v", spans, want)
	}

	if _, ok := crumbSpans(crumbs, 4); ok {
		t.Fatal("a truncated trail must refuse spans — its segments are cut")
	}
	if _, ok := crumbSpans(nil, 80); ok {
		t.Fatal("no crumbs, no spans")
	}
}
