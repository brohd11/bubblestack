package core

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The router frame benchmark. Router.Update calls resize() after every message, and
// resize() renders the chrome to measure it before View renders the chrome again — so
// the per-message cost of the router is worth a number of its own, separate from
// whatever the active screen does.

// bodyScreen renders a fixed body of the size a full-screen pane layout produces, so the
// benchmark measures the router around it rather than the screen inside it.
type bodyScreen struct {
	body string
	sets int
}

func (s *bodyScreen) Init(*Shared) tea.Cmd { return nil }
func (s *bodyScreen) Update(*Shared, tea.Msg) (Screen, Action) {
	return s, Action{}
}
func (s *bodyScreen) View(*Shared) string     { return s.body }
func (s *bodyScreen) HelpView(*Shared) string { return "ctrl+s save • ctrl+f find • ctrl+q quit" }
func (s *bodyScreen) SetSize(*Shared, int, int) {
	s.sets++
}
func (s *bodyScreen) Filtering() bool { return false }

// benchRouter is gote's chrome shape: one tab (so no strip), no header, a breadcrumb
// and a status line. benchRouterFull is the heavier shape — a header and a tab strip —
// so the two together bracket what an app can be paying per message.
func benchRouter() (tea.Model, *bodyScreen) { return buildBenchRouter(false) }

func benchRouterFull() (tea.Model, *bodyScreen) { return buildBenchRouter(true) }

func buildBenchRouter(full bool) (tea.Model, *bodyScreen) {
	rows := make([]string, 40)
	for i := range rows {
		rows[i] = strings.Repeat("x", 100)
	}
	screen := &bodyScreen{body: strings.Join(rows, "\n")}
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Breadcrumb: NewBreadcrumbPane()}
	tabs := []TabEntry{{Title: "Editor", New: func(*Shared) Screen { return screen }}}
	if full {
		sh.Chrome.Header = NewHeaderPane(func(*Shared) string { return "gote — ~/notes" })
		tabs = append(tabs, TabEntry{Title: "Search", New: func(*Shared) Screen { return stubScreen{} }})
	}
	var tm tea.Model = NewRouter(sh, tabs)
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 120, Height: 45})
	return tm, screen
}

// BenchmarkRouterFrame is one whole message: Update (which re-lays-out) and the View
// that follows it.
func BenchmarkRouterFrame(b *testing.B) {
	tm, _ := benchRouter()
	b.ReportAllocs()
	for b.Loop() {
		tm, _ = tm.Update(keyMsg("ctrl+z"))
		benchView = tm.(Router).View()
	}
}

// BenchmarkRouterUpdateOnly isolates resize(): no View, just the bookkeeping the router
// does on the way through.
func BenchmarkRouterUpdateOnly(b *testing.B) {
	tm, _ := benchRouter()
	b.ReportAllocs()
	for b.Loop() {
		tm, _ = tm.Update(keyMsg("ctrl+z"))
	}
}

// TestRouterResizesScreenPerMessage records how many times a message costs the active
// screen a SetSize call. The router still makes one per message by design — a screen is
// the only thing that knows whether its own geometry moved — so what this pins is that the
// SAVING lives in the screen: ModularScreen and editor.Screen answer an unchanged size
// with nothing (see their layoutDirty/sizeDirty guards). The number here is the load those
// guards absorb.
func TestRouterResizesScreenPerMessage(t *testing.T) {
	tm, screen := benchRouter()
	before := screen.sets
	for range 10 {
		tm, _ = tm.Update(keyMsg("ctrl+z"))
	}
	t.Logf("10 key messages cost the active screen %d SetSize calls", screen.sets-before)
	before = screen.sets
	for range 10 {
		tm, _ = tm.Update(tea.MouseMotionMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	}
	t.Logf("10 mouse-motion messages cost the active screen %d SetSize calls", screen.sets-before)
}

// BenchmarkRouterUpdateOnlyFullChrome isolates resize() against chrome worth measuring.
// This is the shape the chrome cache was built for: before it, the header, tab strip and
// breadcrumb were rendered twice here and a third time in the View that followed.
func BenchmarkRouterUpdateOnlyFullChrome(b *testing.B) {
	tm, _ := benchRouterFull()
	b.ReportAllocs()
	for b.Loop() {
		tm, _ = tm.Update(keyMsg("ctrl+z"))
	}
}

// BenchmarkRouterFrameFullChrome is the same message against a header and a tab strip.
func BenchmarkRouterFrameFullChrome(b *testing.B) {
	tm, _ := benchRouterFull()
	b.ReportAllocs()
	for b.Loop() {
		tm, _ = tm.Update(keyMsg("ctrl+z"))
		benchView = tm.(Router).View()
	}
}

var benchView tea.View
