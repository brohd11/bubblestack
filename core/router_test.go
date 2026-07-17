package core

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// stubScreen is a minimal Screen for exercising the router's stack/chrome plumbing
// without pulling in any domain screen.
type stubScreen struct{}

func (stubScreen) Init(*Shared) tea.Cmd { return nil }
func (stubScreen) Update(*Shared, tea.Msg) (Screen, Action) {
	return stubScreen{}, Action{}
}
func (stubScreen) View(*Shared) string       { return "stub" }
func (stubScreen) HelpView(*Shared) string   { return "" }
func (stubScreen) SetSize(*Shared, int, int) {}
func (stubScreen) Filtering() bool           { return false }

// filterScreen is a stubScreen that reports it is capturing filter text, so a key the
// router would otherwise consume globally must pass through to it instead.
type filterScreen struct{ stubScreen }

func (filterScreen) Filtering() bool { return true }

// wrapScreen is a stubScreen that owns a wrap mode of its own (core.Wrapper) — a diff
// view, say. Pointer receiver: ToggleWrap mutates, and the router holds the screen.
type wrapScreen struct {
	stubScreen
	wrap bool
}

func (s *wrapScreen) ToggleWrap()                              { s.wrap = !s.wrap }
func (s *wrapScreen) Wrapped() bool                            { return s.wrap }
func (s *wrapScreen) Update(*Shared, tea.Msg) (Screen, Action) { return s, Action{} }

// wrapFilterScreen wraps *and* captures filter text, so w must be a literal key.
type wrapFilterScreen struct{ wrapScreen }

func (*wrapFilterScreen) Filtering() bool                            { return true }
func (s *wrapFilterScreen) Update(*Shared, tea.Msg) (Screen, Action) { return s, Action{} }

// wrapRouter is a router whose top screen wraps, alongside a wrappable output pane —
// the contended case the Wrap key has to arbitrate.
func wrapRouter() (Router, *wrapScreen, *fakeOutput) {
	screen := &wrapScreen{}
	out := &fakeOutput{}
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: out, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "Wrap", New: func(*Shared) Screen { return screen }}})
	return r, screen, out
}

// fakeOutput is a minimal core.Output (plus the Log and Wrapper capabilities) for
// exercising the router's output key/layout plumbing without importing components
// (core ← components forbids it).
type fakeOutput struct {
	logs    []string
	shown   bool
	wrap    bool
	tops    int // GotoTop calls
	bottoms int // GotoBottom calls
	// h is the rows the pane claims, so a test can place it on the canvas for the
	// wheel's row hit-test; 0 (the default) keeps it out of the layout as before.
	h       int
	updates []tea.Msg // messages forwarded to the pane, for the wheel routing tests
}

func (f *fakeOutput) Log(s string, show bool) { f.logs = append(f.logs, s); f.shown = show }
func (f *fakeOutput) Shown() bool             { return f.shown }
func (f *fakeOutput) Toggle()                 { f.shown = !f.shown }
func (f *fakeOutput) Hide()                   { f.shown = false }
func (f *fakeOutput) Clear()                  { f.logs = nil; f.shown = false }
func (f *fakeOutput) SetSize(_, _ int)        {}
func (f *fakeOutput) Height() int             { return f.h }
func (f *fakeOutput) View(bool) string        { return "OUT" }
func (f *fakeOutput) GotoBottom()             { f.bottoms++ }
func (f *fakeOutput) GotoTop()                { f.tops++ }
func (f *fakeOutput) ToggleWrap()             { f.wrap = !f.wrap }
func (f *fakeOutput) Wrapped() bool           { return f.wrap }

func (f *fakeOutput) Update(msg tea.Msg) tea.Cmd {
	f.updates = append(f.updates, msg)
	return nil
}

// plainOutput is an Output that does NOT implement Wrapper (no embedding — promoted
// methods would satisfy the interface): the Wrap key must pass through to the screen.
type plainOutput struct {
	shown bool
}

func (p *plainOutput) Log(_ string, show bool) { p.shown = show }
func (p *plainOutput) Shown() bool             { return p.shown }
func (p *plainOutput) Toggle()                 { p.shown = !p.shown }
func (p *plainOutput) Hide()                   { p.shown = false }
func (p *plainOutput) Clear()                  { p.shown = false }
func (p *plainOutput) SetSize(_, _ int)        {}
func (p *plainOutput) Height() int             { return 0 }
func (p *plainOutput) View(bool) string        { return "OUT" }
func (p *plainOutput) Update(tea.Msg) tea.Cmd  { return nil }
func (p *plainOutput) GotoBottom()             {}
func (p *plainOutput) GotoTop()                {}

// fakeStatus is a minimal core.Status for exercising the router's status rendering and
// auto-clear plumbing without importing components.
type fakeStatus struct {
	msg string
	gen int
}

func (f *fakeStatus) Set(line string) { f.msg = line; f.gen++ }
func (f *fakeStatus) Clear()          { f.msg = "" }
func (f *fakeStatus) Shown() bool     { return f.msg != "" }
func (f *fakeStatus) Height() int     { return 0 }
func (f *fakeStatus) View() string    { return f.msg }
func (f *fakeStatus) Gen() int        { return f.gen }

func newCoreTestRouter() Router {
	// nil App: stubScreen reads no context, so the router needs no domain dependency.
	// Chrome carries fake output/status panes (no header) to exercise the output keys
	// and the status rendering.
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	return NewRouter(sh, []TabEntry{{Title: "Stub", New: func(*Shared) Screen { return stubScreen{} }}})
}

func sized(tm tea.Model) tea.Model {
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return tm
}

// pump delivers msg, then runs the returned command and feeds its (single,
// non-batch) result back — enough to drive the navigation commands.
func pump(tm tea.Model, msg tea.Msg) tea.Model {
	tm, cmd := tm.Update(msg)
	for i := 0; i < 8 && cmd != nil; i++ {
		out := cmd()
		if out == nil {
			break
		}
		if _, isBatch := out.(tea.BatchMsg); isBatch {
			break
		}
		tm, cmd = tm.Update(out)
	}
	return tm
}

// TestRouterStackPushPop checks the stack semantics: push grows it, pop shrinks it,
// and popping at the root (single screen) is ignored.
func TestRouterStackPushPop(t *testing.T) {
	tm := sized(newCoreTestRouter())

	tm, _ = tm.Update(pushMsg{s: stubScreen{}})
	if got := len(tm.(Router).stack); got != 2 {
		t.Fatalf("after push want 2, got %d", got)
	}

	tm, _ = tm.Update(popMsg{n: 1})
	if got := len(tm.(Router).stack); got != 1 {
		t.Fatalf("after pop want 1, got %d", got)
	}

	tm, _ = tm.Update(popMsg{n: 1})
	if got := len(tm.(Router).stack); got != 1 {
		t.Fatalf("root pop should be ignored, want 1, got %d", got)
	}
}

// TestOutputFocusAndClear seeds a log line (which reveals the output box), then
// checks the ToggleOutput key focuses the output pane and the Clear key clears it
// (returning focus to the list) — the router's global keys. The keys are taken from
// the central keymap so the test tracks rebinds.
func TestOutputFocusAndClear(t *testing.T) {
	tm := sized(newCoreTestRouter())
	sh := tm.(Router).sh
	out := sh.Chrome.Output.(*fakeOutput)
	sh.Log("hello")
	tm = sized(tm) // re-lay-out with the log present

	tm = pump(tm, keyMsg(Keys.ToggleOutput.Keys()[0]))
	if !sh.Chrome.outputFocused {
		t.Fatal("ToggleOutput should focus the output pane")
	}

	tm = pump(tm, keyMsg(Keys.Clear.Keys()[0]))
	if len(out.logs) != 0 {
		t.Fatalf("Clear should clear the logs, got %d", len(out.logs))
	}
	if sh.Chrome.outputFocused {
		t.Fatal("clearing should return focus to the list")
	}
}

// TestOutputToggle checks the o key hides/shows the output box independently of the
// log contents (force-show), and hiding while focused returns focus to the list.
func TestOutputToggle(t *testing.T) {
	tm := sized(newCoreTestRouter())
	sh := tm.(Router).sh
	out := sh.Chrome.Output.(*fakeOutput)
	sh.Log("hello") // logging reveals the box
	if !out.Shown() {
		t.Fatal("appending a log should show the output box")
	}

	tm = pump(tm, keyMsg(Keys.ToggleOutput.Keys()[0])) // focus the pane
	tm = pump(tm, keyMsg(Keys.Output.Keys()[0]))       // o hides it
	if out.Shown() {
		t.Fatal("o should hide the output box")
	}
	if sh.Chrome.outputFocused {
		t.Fatal("hiding the output while focused should return focus to the list")
	}

	pump(tm, keyMsg(Keys.Output.Keys()[0])) // o shows it again
	if !out.Shown() {
		t.Fatal("o should show the output box again")
	}
}

// TestOutputWrapKey checks the w key flips the pane's render mode from any screen —
// the pane need not hold focus (it is a global chrome key like o) — and that it is a
// plain toggle.
func TestOutputWrapKey(t *testing.T) {
	tm := sized(newCoreTestRouter())
	sh := tm.(Router).sh
	out := sh.Chrome.Output.(*fakeOutput)
	sh.Log("hello")

	tm = pump(tm, keyMsg(Keys.Wrap.Keys()[0])) // unfocused: still consumed
	if !out.Wrapped() {
		t.Fatal("w should wrap the output pane without focusing it first")
	}
	tm = pump(tm, keyMsg(Keys.Wrap.Keys()[0]))
	if out.Wrapped() {
		t.Fatal("w should toggle wrap back off")
	}

	tm = pump(tm, keyMsg(Keys.ToggleOutput.Keys()[0])) // focus the pane
	pump(tm, keyMsg(Keys.Wrap.Keys()[0]))
	if !out.Wrapped() {
		t.Fatal("w should wrap while the pane holds focus too")
	}
}

// TestWrapKeyPassesThrough checks the two cases where w must NOT be swallowed: a top
// screen capturing filter text (it is typing a literal w), and an Output that doesn't
// implement Wrapper (nothing to toggle).
func TestWrapKeyPassesThrough(t *testing.T) {
	t.Run("filtering screen", func(t *testing.T) {
		sh := NewShared(nil)
		sh.Chrome = &Chrome{Output: &fakeOutput{}}
		r := NewRouter(sh, []TabEntry{{Title: "Filter", New: func(*Shared) Screen { return filterScreen{} }}})
		sh.Log("hello")

		pump(sized(r), keyMsg(Keys.Wrap.Keys()[0]))
		if sh.Chrome.Output.(*fakeOutput).Wrapped() {
			t.Fatal("w must reach a filtering screen as a literal key, not toggle wrap")
		}
	})

	t.Run("output without Wrapper", func(t *testing.T) {
		sh := NewShared(nil)
		sh.Chrome = &Chrome{Output: &plainOutput{}}
		r := NewRouter(sh, []TabEntry{{Title: "Stub", New: func(*Shared) Screen { return stubScreen{} }}})
		sh.Log("hello")

		// The key is simply not consumed; the run must not panic on the assertion.
		pump(sized(r), keyMsg(Keys.Wrap.Keys()[0]))
	})
}

// TestWrapKeyPrefersScreen checks the arbitration when both the top screen and the output
// pane can wrap: focus decides. An unfocused pane means the user is looking at the screen,
// so w is the screen's; focusing the pane (tab) points w back at the pane, which is what
// keeps the log's own wrap reachable from a screen that wraps.
func TestWrapKeyPrefersScreen(t *testing.T) {
	r, screen, out := wrapRouter()
	tm := sized(r)
	r.sh.Log("hello")

	tm = pump(tm, keyMsg(Keys.Wrap.Keys()[0]))
	if !screen.Wrapped() {
		t.Fatal("w should wrap the top screen when the output pane is unfocused")
	}
	if out.Wrapped() {
		t.Fatal("w must not also wrap the output pane — the screen owns the key here")
	}

	tm = pump(tm, keyMsg(Keys.Wrap.Keys()[0]))
	if screen.Wrapped() {
		t.Fatal("w should toggle the screen's wrap back off")
	}

	tm = pump(tm, keyMsg(Keys.ToggleOutput.Keys()[0])) // tab: focus the pane
	pump(tm, keyMsg(Keys.Wrap.Keys()[0]))
	if !out.Wrapped() {
		t.Fatal("w should wrap the output pane once it holds focus")
	}
	if screen.Wrapped() {
		t.Fatal("w must not reach the screen while the pane holds focus")
	}
}

// TestWrapKeyScreenWithoutOutput checks a wrapping screen still gets w when there is no
// output pane at all — the branch used to be gated on the pane existing.
func TestWrapKeyScreenWithoutOutput(t *testing.T) {
	screen := &wrapScreen{}
	sh := NewShared(nil)
	sh.Chrome = &Chrome{} // no Output
	r := NewRouter(sh, []TabEntry{{Title: "Wrap", New: func(*Shared) Screen { return screen }}})

	pump(sized(r), keyMsg(Keys.Wrap.Keys()[0]))
	if !screen.Wrapped() {
		t.Fatal("w should wrap the screen even with no output pane present")
	}
}

// TestWrapKeyFilteringScreenWins checks that a screen which both wraps and captures text
// keeps w as a literal: typing must beat the toggle.
func TestWrapKeyFilteringScreenWins(t *testing.T) {
	screen := &wrapFilterScreen{}
	out := &fakeOutput{}
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: out, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "Wrap", New: func(*Shared) Screen { return screen }}})
	sh.Log("hello")

	pump(sized(r), keyMsg(Keys.Wrap.Keys()[0]))
	if screen.Wrapped() {
		t.Fatal("w must reach a filtering screen as a literal key, not toggle its wrap")
	}
	if out.Wrapped() {
		t.Fatal("w must not fall through to the output pane while a screen is filtering")
	}
}

// TestOutputJumpKeys checks that every Top/Bottom keycode jumps the focused output pane
// to the corresponding end. The router matches these itself because the viewport's own
// keymap binds neither — hence the per-keycode loop rather than one representative key.
func TestOutputJumpKeys(t *testing.T) {
	tm := sized(newCoreTestRouter())
	r := tm.(Router)
	r.sh.Log("hello") // reveal the pane
	out := r.sh.Chrome.Output.(*fakeOutput)

	tm = pump(tm, keyMsg(Keys.ToggleOutput.Keys()[0])) // focus it (and pin to bottom)

	for _, k := range Keys.Top.Keys() {
		out.tops = 0
		tm = pump(tm, keyMsg(k))
		if out.tops != 1 {
			t.Fatalf("%q should jump the focused pane to the top, got %d GotoTop calls", k, out.tops)
		}
	}
	for _, k := range Keys.Bottom.Keys() {
		out.bottoms = 0
		tm = pump(tm, keyMsg(k))
		if out.bottoms != 1 {
			t.Fatalf("%q should jump the focused pane to the bottom, got %d GotoBottom calls", k, out.bottoms)
		}
	}
}

// TestOutputJumpKeysNeedFocus: the jump keys belong to the focused pane only. Unfocused,
// the pane is already pinned to the newest line by resize, and g must stay available to
// the screen below.
func TestOutputJumpKeysNeedFocus(t *testing.T) {
	tm := sized(newCoreTestRouter())
	r := tm.(Router)
	r.sh.Log("hello") // shown, but not focused
	out := r.sh.Chrome.Output.(*fakeOutput)

	pump(tm, keyMsg(Keys.Top.Keys()[0]))
	if out.tops != 0 {
		t.Fatalf("g must pass through to the screen while the pane is unfocused, got %d GotoTop calls", out.tops)
	}
}

// maskScreen is a stub screen that claims the whole canvas via ChromeMasker.
type maskScreen struct{ stubScreen }

func (maskScreen) ChromeMask() ChromeMask { return FullscreenMask() }

// TestChromeMaskSuppressesChrome checks a screen returning FullscreenMask hides the
// chrome the router would otherwise draw (here the output pane), and that popping
// back to an unmasked screen restores it — the per-screen suppression lever.
func TestChromeMaskSuppressesChrome(t *testing.T) {
	tm := sized(newCoreTestRouter())
	r := tm.(Router)
	r.sh.Log("hello") // reveal the output pane
	r.sh.Chrome.Status.Set("working")
	if r.belowChrome(r.currentMask()) == "" {
		t.Fatal("output/status should render under an unmasked screen")
	}

	tm = pump(tm, Push(maskScreen{}))
	r = tm.(Router)
	if got := r.belowChrome(r.currentMask()); got != "" {
		t.Fatalf("FullscreenMask should suppress the below chrome, got %q", got)
	}
	if got := r.topChrome(r.currentMask()); got != "" {
		t.Fatalf("FullscreenMask should suppress the top chrome, got %q", got)
	}

	tm = pump(tm, Pop())
	r = tm.(Router)
	if r.belowChrome(r.currentMask()) == "" {
		t.Fatal("popping back to the unmasked screen should restore the chrome")
	}
}

// ---------- mouse ----------

// wheelScreen records what the router dispatches to the active screen, so a test can
// assert a wheel outside the output pane reaches the body (where a DocScreen's viewport
// would consume it) rather than being swallowed as chrome.
type wheelScreen struct{ seen []tea.Msg }

func (s *wheelScreen) Init(*Shared) tea.Cmd { return nil }
func (s *wheelScreen) Update(_ *Shared, msg tea.Msg) (Screen, Action) {
	s.seen = append(s.seen, msg)
	return s, Action{}
}
func (s *wheelScreen) View(*Shared) string       { return "wheel" }
func (s *wheelScreen) HelpView(*Shared) string   { return "" }
func (s *wheelScreen) SetSize(*Shared, int, int) {}

func wheelAt(y int, b tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Y: y, Button: b, Action: tea.MouseActionPress}
}

// newWheelRouter is a router whose root records dispatched messages, with a 6-row
// output pane revealed. sized() lays it out at 80x24 and wheelScreen draws no help bar,
// so the pane's rows are 18..23 and anything above is body.
func newWheelRouter() (Router, *wheelScreen, *fakeOutput) {
	sh := NewShared(nil)
	out := &fakeOutput{h: 6}
	sh.Chrome = &Chrome{Output: out, Status: &fakeStatus{}}
	scr := &wheelScreen{}
	r := NewRouter(sh, []TabEntry{{Title: "Wheel", New: func(*Shared) Screen { return scr }}})
	sh.Log("hello") // reveal the pane
	return r, scr, out
}

// TestWheelOverOutputFocusesAndScrolls checks a wheel inside the output pane's rows is
// forwarded to the pane AND focuses it. The focus is the load-bearing part: resize
// re-pins an unfocused pane to the bottom on every message, so a wheel that scrolled
// without focusing would snap straight back and appear to do nothing.
func TestWheelOverOutputFocusesAndScrolls(t *testing.T) {
	r, _, out := newWheelRouter()
	tm := sized(r)
	sh := tm.(Router).sh
	// resize pins the unfocused pane during layout, so count re-pins across the wheel
	// rather than from zero.
	pinned := out.bottoms

	tm = pump(tm, wheelAt(20, tea.MouseButtonWheelUp))
	if !sh.Chrome.outputFocused {
		t.Fatal("a wheel over the output pane should focus it, or resize re-pins it to the bottom")
	}
	if len(out.updates) == 0 {
		t.Fatal("the wheel should be forwarded to the output pane")
	}
	if out.bottoms != pinned {
		t.Fatal("once the wheel focuses the pane, resize must stop re-pinning it to the bottom")
	}
}

// TestWheelOverBodyReachesScreen checks the router leaves a wheel outside the pane
// alone: it must fall through to the active screen (the DocScreen path) and must not
// steal focus into the output pane.
func TestWheelOverBodyReachesScreen(t *testing.T) {
	r, scr, _ := newWheelRouter()
	tm := sized(r)
	sh := tm.(Router).sh

	pump(tm, wheelAt(5, tea.MouseButtonWheelDown))
	if sh.Chrome.outputFocused {
		t.Fatal("a wheel over the body must not focus the output pane")
	}
	var got bool
	for _, m := range scr.seen {
		if _, ok := m.(tea.MouseMsg); ok {
			got = true
		}
	}
	if !got {
		t.Fatal("a wheel over the body should reach the active screen")
	}
}

// TestWheelBoundary pins the edges of the pane's row range: the row just above its top
// border belongs to the body, its top border row belongs to the pane.
func TestWheelBoundary(t *testing.T) {
	r, _, _ := newWheelRouter()
	tm := sized(r)
	rr := tm.(Router)

	if rr.inOutput(17) {
		t.Fatal("row 17 is above the 6-row pane (18..23) and belongs to the body")
	}
	if !rr.inOutput(18) {
		t.Fatal("row 18 is the pane's first row")
	}
	if !rr.inOutput(23) {
		t.Fatal("row 23 is the pane's last row")
	}
}

// TestWheelIgnoredWhenOutputHidden checks a hidden pane claims no rows, so a wheel
// anywhere falls through to the screen instead of scrolling an invisible log.
func TestWheelIgnoredWhenOutputHidden(t *testing.T) {
	r, _, out := newWheelRouter()
	out.Hide()
	tm := sized(r)
	sh := tm.(Router).sh

	pump(tm, wheelAt(20, tea.MouseButtonWheelUp))
	if sh.Chrome.outputFocused {
		t.Fatal("a wheel must not focus a hidden output pane")
	}
	if len(out.updates) != 0 {
		t.Fatal("a hidden pane should receive no wheel events")
	}
}

// TestMouseToggleKey checks m flips mouse capture both ways and emits a command each
// time (tea.DisableMouse / tea.EnableMouseCellMotion).
func TestMouseToggleKey(t *testing.T) {
	tm := sized(newCoreTestRouter())
	if !tm.(Router).mouseOn {
		t.Fatal("mouse should start enabled; the Run facade opens the program with cell motion")
	}

	tm, cmd := tm.Update(keyMsg(Keys.Mouse.Keys()[0]))
	if tm.(Router).mouseOn {
		t.Fatal("m should toggle mouse capture off")
	}
	if cmd == nil {
		t.Fatal("toggling off should emit a command to stop mouse reporting")
	}

	tm, cmd = tm.Update(keyMsg(Keys.Mouse.Keys()[0]))
	if !tm.(Router).mouseOn {
		t.Fatal("m should toggle mouse capture back on")
	}
	if cmd == nil {
		t.Fatal("toggling on should emit a command to resume mouse reporting")
	}
}

// TestMouseKeyPassesThroughWhileFiltering checks m is not swallowed by a screen that is
// capturing filter text — it is typing a literal m (mirrors the w case).
func TestMouseKeyPassesThroughWhileFiltering(t *testing.T) {
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}}
	r := NewRouter(sh, []TabEntry{{Title: "Filter", New: func(*Shared) Screen { return filterScreen{} }}})

	tm := pump(sized(r), keyMsg(Keys.Mouse.Keys()[0]))
	if !tm.(Router).mouseOn {
		t.Fatal("m must reach a filtering screen as a literal key, not toggle mouse capture")
	}
}

// keyMsg builds a tea.KeyMsg whose String() matches the given key string, so tests
// can drive the router from central-keymap key strings.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}
