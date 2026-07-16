package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The failure the wrap mode exists for: a line far wider than the pane, whose tail the
// viewport clips away with no way to scroll to it (a deep path, no spaces to fold at).
const longPath = "xattr: [Errno 13] permission denied: " +
	"'/Users/brohd/godot/Godot-Plugin-Dev-No-Sub/addons/syntax_plus/.git/objects/24/c2a606d5b73cc522d3fee8d3b8fe2f48866047'"

// paneAt builds a sized pane holding lines. 80x24 gives innerWidth 74.
func paneAt(lines ...string) *LogPane {
	p := NewLogPane()
	for _, l := range lines {
		p.Log(l, true)
	}
	p.SetSize(80, 24)
	return p
}

// TestContentTruncatedMode is the baseline: unwrapped, every entry is one row (the
// viewport is what clips it), so the long line is a single overlong row.
func TestContentTruncatedMode(t *testing.T) {
	p := paneAt("short", longPath)

	rows := strings.Split(p.content(), "\n")
	if len(rows) != 2 {
		t.Fatalf("unwrapped content should be one row per entry, got %d rows", len(rows))
	}
	if w := ansi.StringWidth(rows[1]); w <= p.innerWidth() {
		t.Fatalf("the long entry should overflow the pane (width %d, inner %d)", w, p.innerWidth())
	}
}

// TestContentWrapMode: every row fits the pane, entries stay distinguishable (bullet
// then hanging indent), and no text is lost in the fold.
func TestContentWrapMode(t *testing.T) {
	p := paneAt("short", longPath)
	p.ToggleWrap()

	rows := strings.Split(p.content(), "\n")
	if len(rows) < 3 {
		t.Fatalf("the long entry should fold across rows, got %d rows total", len(rows))
	}

	var bullets, recovered []string
	for _, row := range rows {
		if w := ansi.StringWidth(row); w > p.innerWidth() {
			t.Errorf("row wider than the pane (%d > %d): %q", w, p.innerWidth(), row)
		}
		r := ansi.Strip(row) // LogStyle wraps each row in escape codes on a color terminal
		switch {
		case strings.HasPrefix(r, logBullet):
			bullets = append(bullets, r)
			recovered = append(recovered, strings.TrimPrefix(r, logBullet))
		case strings.HasPrefix(r, logIndent):
			recovered = append(recovered, strings.TrimPrefix(r, logIndent))
		default:
			t.Errorf("row carries neither bullet nor indent: %q", r)
		}
	}
	if len(bullets) != 2 {
		t.Errorf("want one bullet per entry (2), got %d", len(bullets))
	}

	// Folding is lossless: rejoining the rows recovers both entries. ansi.Wrap eats the
	// space it breaks at, so compare with whitespace collapsed.
	joined := strings.Join(recovered, "")
	for _, want := range []string{"short", longPath} {
		if !strings.Contains(collapse(joined), collapse(want)) {
			t.Errorf("wrapping lost text: %q not recoverable from the rows", want)
		}
	}
}

// TestToggleWrapRoundTrip: wrap is a plain mode flag over the same in-memory lines.
func TestToggleWrapRoundTrip(t *testing.T) {
	p := paneAt(longPath)
	before := p.content()
	if p.Wrapped() {
		t.Fatal("wrap should start off")
	}

	p.ToggleWrap()
	if !p.Wrapped() {
		t.Fatal("ToggleWrap should turn wrap on")
	}
	if p.content() == before {
		t.Fatal("wrapped content should differ from truncated content")
	}

	p.ToggleWrap()
	if p.Wrapped() {
		t.Fatal("ToggleWrap should turn wrap back off")
	}
	if p.content() != before {
		t.Fatal("toggling back should restore the truncated rendering exactly")
	}
}

// TestWrapNarrowPane: the bullet leaves no room at all — wrapping must still terminate
// and never produce a negative width.
func TestWrapNarrowPane(t *testing.T) {
	p := NewLogPane()
	p.Log(longPath, true)
	p.SetSize(1, 1) // innerWidth clamps to its 10-col floor
	p.ToggleWrap()

	for _, r := range strings.Split(p.content(), "\n") {
		if w := ansi.StringWidth(r); w > p.innerWidth() {
			t.Fatalf("row wider than the clamped pane (%d > %d): %q", w, p.innerWidth(), r)
		}
	}
}

// collapse strips whitespace so a comparison ignores where the wrap fell.
func collapse(s string) string { return strings.Join(strings.Fields(s), "") }

// TestLogPaneWheelScrolls exercises the pane's real viewport, which is the half of the
// wheel path the router's fake Output can't prove: the router forwards the event, and
// the viewport is what turns it into a scroll.
func TestLogPaneWheelScrolls(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	p := paneAt(lines...)
	p.GotoBottom()

	bottom := p.vp.YOffset
	if bottom == 0 {
		t.Fatal("100 lines in a 24-row pane should scroll; the fixture is wrong")
	}

	p.Update(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if p.vp.YOffset >= bottom {
		t.Fatalf("a wheel-up should scroll the pane back from the bottom, offset stayed at %d", p.vp.YOffset)
	}
}

// TestHeightMatchesRenderedRows guards the assumption the router's wheel hit-test rests
// on: Height() is the rows the pane actually draws. The router locates the pane by
// counting Height() rows up from the help bar, so if the two ever drift the wheel
// silently targets the wrong rows — a failure no test of either side alone would catch.
func TestHeightMatchesRenderedRows(t *testing.T) {
	p := paneAt("one", "two", "three")
	if got, want := lipgloss.Height(p.View(false)), p.Height(); got != want {
		t.Fatalf("pane renders %d rows but reports Height() == %d", got, want)
	}
}

// stubRootScreen is a minimal core.Screen (no help bar) so a router test can place the
// output pane at a known row range.
type stubRootScreen struct{}

func (stubRootScreen) Init(*core.Shared) tea.Cmd { return nil }
func (stubRootScreen) Update(*core.Shared, tea.Msg) (core.Screen, core.Action) {
	return stubRootScreen{}, core.Action{}
}
func (stubRootScreen) View(*core.Shared) string       { return "body" }
func (stubRootScreen) HelpView(*core.Shared) string   { return "" }
func (stubRootScreen) SetSize(*core.Shared, int, int) {}

// TestRouterWheelScrollsRealLogPane is the end-to-end check: a real router routing a
// real wheel to a real LogPane, exercising the row math against the pane's true
// Height() rather than a fake's. The router's own tests use a fake Output, so this is
// what proves the two halves agree.
func TestRouterWheelScrollsRealLogPane(t *testing.T) {
	pane := NewLogPane()
	for i := 0; i < 100; i++ {
		pane.Log(fmt.Sprintf("line %d", i), true)
	}
	sh := core.NewShared(nil)
	sh.Chrome = &core.Chrome{Output: pane}
	r := core.NewRouter(sh, []core.TabEntry{{
		Title: "T", New: func(*core.Shared) core.Screen { return stubRootScreen{} },
	}})

	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24}) // pins the pane to the bottom
	bottom := pane.vp.YOffset
	if bottom == 0 {
		t.Fatal("100 lines should leave the pane scrolled to a non-zero offset; fixture is wrong")
	}

	// At 24 rows with no help bar the pane's 8 rows are 16..23, so 20 is inside it.
	if got := pane.Height(); got != 8 {
		t.Fatalf("fixture assumes an 8-row pane at height 24, got %d", got)
	}
	tm.Update(tea.MouseMsg{Y: 20, Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if pane.vp.YOffset >= bottom {
		t.Fatalf("a wheel over the pane should scroll it through the router, offset stayed at %d", pane.vp.YOffset)
	}
}
