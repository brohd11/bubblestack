package core

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// wrapperOutput reports the output pane when there is one and it can wrap.
func wrapperOutput(ch *Chrome) (Wrapper, bool) {
	if ch == nil || ch.Output == nil {
		return nil, false
	}
	w, ok := ch.Output.(Wrapper)
	return w, ok
}

// quitAction resolves a global quit (q / ctrl+c): the stack is consulted top-down
// for a QuitGater that handles it, so a modal pushed above the gating screen (a
// save-as line edit over the editor) doesn't silence it. With no taker the quit
// is an unconditional tea.Quit.
func (r *Router) quitAction() Action {
	for i := len(r.stack) - 1; i >= 0; i-- {
		if g, ok := r.stack[i].(QuitGater); ok {
			if act, handled := g.QuitGate(r.sh); handled {
				return act
			}
		}
	}
	return Async(tea.Quit)
}

// dirKeyAction resolves a DirLocator-based global key (Terminal, TerminalWindow, OpenDir):
// when action is wired, k matches b, the top screen isn't capturing text, and it advertises
// a directory, it returns (action(dir), true). Otherwise (Action{}, false) so the key falls through to
// the active screen — preserving any row-level handling it does itself.
func (r *Router) dirKeyAction(k string, b key.Binding, action func(string) Action) (Action, bool) {
	if action == nil || !MatchKey(k, b) {
		return Action{}, false
	}
	if f, ok := r.Top().(Filterer); ok && f.Filtering() && !modifiedKey(k) {
		return Action{}, false
	}
	if loc, ok := r.Top().(DirLocator); ok {
		if dir, ok := loc.LocateDir(); ok {
			return action(dir), true
		}
	}
	return Action{}, false
}

// globalKey handles the keys available in any screen. It returns (act, true) when it
// consumed the key — act carries a control message resolved inline and/or an async cmd
// (e.g. tea.Quit or an output-scroll cmd) — or (Action{}, false) to let the active
// screen handle it. Pointer receiver: [ / ] mutate active/stack, which must persist
// back to Update's router.
func (r *Router) globalKey(msg tea.KeyPressMsg) (Action, bool) {
	k := msg.String()
	if k == "ctrl+c" {
		return r.quitAction(), true
	}

	// Refresh fires from any screen/depth except while text is captured (a filtering
	// list or a focused form, both reporting Filtering()). The action is
	// consumer-supplied so core names no domain type. Placed before the
	// output-focused branch so it works even with the output pane focused.
	if r.refreshAction != nil && MatchKey(k, Keys.Refresh) {
		if f, ok := r.Top().(Filterer); !ok || !f.Filtering() || modifiedKey(k) {
			return r.refreshAction(r.sh), true
		}
	}

	// Terminal ("t") hands this process's terminal to a shell, TerminalWindow ("T") opens a
	// detached window, and OpenDir ("ctrl+t") opens the file manager — all three at the top
	// screen's directory from any depth, which is the whole point: they work once you've
	// drilled into a repo, not only on the list row. dirKeyAction gates each on text capture
	// (a filtering list / focused form can still type the letter) and resolves the directory
	// from the top screen's DirLocator; a screen that isn't one lets the key fall through so
	// its own row-level handling still gets it.
	if act, ok := r.dirKeyAction(k, Keys.Terminal, r.terminalAction); ok {
		return act, true
	}
	if act, ok := r.dirKeyAction(k, Keys.TerminalWindow, r.terminalWindowAction); ok {
		return act, true
	}
	if act, ok := r.dirKeyAction(k, Keys.OpenDir, r.openDirAction); ok {
		return act, true
	}

	ch := r.sh.Chrome
	outputOn := ch != nil && ch.Output != nil

	// Wrap flips a render mode between truncated and folded. Two things can own that
	// mode — the output pane and the top screen (a diff, say) — so focus decides which
	// one the key means: the pane while it holds focus, otherwise the screen if it wraps
	// at all, and the pane when it doesn't. That keeps w pointed at whatever the user is
	// actually looking at, and leaves the pane reachable (O, then w) from a screen that
	// wraps.
	//
	// Both sides are optional capabilities (Wrapper), so neither an Output nor a screen
	// without one ever consumes the key. Filtering screens keep a literal w (a
	// modified-key combo would pass the gate — see modifiedKey); a focused pane means
	// the screen isn't reading keys anyway, so the gate doesn't apply there.
	if MatchKey(k, Keys.Wrap) {
		f, isFilterer := r.Top().(Filterer)
		filtering := isFilterer && f.Filtering() && !modifiedKey(k)
		paneFocused := outputOn && ch.outputFocused

		if paneFocused || !filtering {
			if w, ok := r.Top().(Wrapper); ok && !paneFocused {
				w.ToggleWrap()
				return Action{}, true
			}
			if w, ok := wrapperOutput(ch); ok {
				w.ToggleWrap()
				return Action{}, true
			}
		}
	}

	// Mouse capture costs the terminal's own drag-select, which is the only way to copy
	// a path back out of the log, so it toggles from any screen — like Output/Wrap, and
	// above the focused branch, whose fall-through would otherwise swallow the key. The
	// status line reports the trade rather than just the state, since reclaiming
	// selection is the whole reason to press it.
	if MatchKey(k, Keys.Mouse) {
		if f, ok := r.Top().(Filterer); !ok || !f.Filtering() || modifiedKey(k) {
			// v2 has no enable/disable command: mouse reporting is a field on the
			// tea.View the next render returns, which Router.View reads off mouseOn.
			r.mouseOn = !r.mouseOn
			if r.mouseOn {
				return SetStatus("mouse on · wheel scrolls"), true
			}
			return SetStatus("mouse off · text selection on"), true
		}
	}

	// When the output pane holds focus, navigation keys scroll it; everything
	// else either toggles back or clears. Top/Bottom are matched here rather than
	// left to the viewport's own keymap, which binds neither.
	if outputOn && ch.outputFocused {
		switch {
		case MatchKey(k, Keys.ToggleOutput), MatchKey(k, Keys.Back):
			r.setOutputFocused(false)
			return Action{}, true
		case MatchKey(k, Keys.Output):
			ch.Output.Hide()
			r.setOutputFocused(false)
			return Action{}, true
		case MatchKey(k, Keys.Clear):
			r.clearOutput()
			return Action{}, true
		case MatchKey(k, Keys.Quit):
			return r.quitAction(), true
		case MatchKey(k, Keys.Top):
			ch.Output.GotoTop()
			return Action{}, true
		case MatchKey(k, Keys.Bottom):
			ch.Output.GotoBottom()
			return Action{}, true
		}
		return Async(ch.Output.Update(msg)), true
	}

	// O jumps into the output pane, o shows/hides it, c clears the log, [ / ]
	// switch top-level tabs (only at the root, so the live stack always belongs
	// to the active tab), and alt+u unwinds a deep stack back to the root for a
	// quick exit — unless the active screen is capturing filter text (and even
	// then, a modified-key combo passes: it types no text — see modifiedKey).
	// The output keys pass through (no consume) when there is no output pane, so a
	// chromeless app can bind O/o itself.
	if f, ok := r.Top().(Filterer); !ok || !f.Filtering() || modifiedKey(k) {
		switch {
		case MatchKey(k, Keys.ToggleOutput):
			if !outputOn {
				break
			}
			if ch.Output.Shown() {
				r.setOutputFocused(true)
				ch.Output.GotoBottom()
			}
			return Action{}, true
		case MatchKey(k, Keys.Output):
			if !outputOn {
				break
			}
			ch.Output.Toggle()
			if !ch.Output.Shown() {
				r.setOutputFocused(false)
			}
			return Action{}, true
		case MatchKey(k, Keys.Clear):
			if ch == nil {
				break
			}
			r.clearOutput()
			return Action{}, true
		case MatchKey(k, Keys.Quit):
			// q is the global quit, handled once here for every screen (the filter
			// gate above keeps it from firing while a list/form is capturing text).
			return r.quitAction(), true
		case MatchKey(k, Keys.NextTab):
			return Action{}, r.switchTab(1)
		case MatchKey(k, Keys.PrevTab):
			return Action{}, r.switchTab(-1)
		case MatchKey(k, Keys.Unwind):
			// Unwind a deep stack back to the root for a quick exit. Only consume it
			// when there's something to unwind, so at the root the key passes through
			// to the active screen instead of being swallowed.
			if len(r.stack) > 1 {
				return ResetToRoot(), true
			}
		}
	}
	return Action{}, false
}

// mouse claims a wheel over the output pane — router-owned chrome no screen can see —
// and leaves every other mouse event to the active screen, which is how a DocScreen's
// viewport receives it. It returns (act, true) only when it consumed the event.
//
// Scrolling the pane also focuses it. That isn't incidental: resize re-pins an
// unfocused pane to the bottom on every message, so a wheel that scrolled without
// focusing would snap straight back. Focus already means "the user is reading rather
// than tailing" here, so the wheel just says so — and the pane's border and legend
// announce it, with O/esc returning as they do from a keyboard focus.
func (r *Router) mouse(mm tea.MouseMsg) (Action, bool) {
	// v2 splits the old Action field into distinct message types. Only clicks and
	// wheel notches are the router's business; motion and release fall through
	// untouched to the active screen, which is what drives editor drag-select.
	switch mm.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		return Action{}, false
	}
	msg := mm.Mouse()
	// A press aimed at the body returns output-pane focus to it: the pane grabs
	// focus on wheel (below), and the keyboard has O/esc to come back — but a
	// click or wheel over the body must release it too, or the pane keeps the
	// keys (and keeps the screen dimmed) after the mouse has moved on.
	if ch := r.sh.Chrome; ch != nil && ch.outputFocused &&
		!(r.outputVisible() && r.inOutput(msg.Y)) {
		r.setOutputFocused(false)
	}
	// A left click on a breadcrumb segment pops the stack back to it — the mouse
	// analog of hammering esc. Router-owned chrome, so it lives here with the
	// output-pane wheel claim below.
	if msg.Button == tea.MouseLeft {
		if act, ok := r.headerClick(msg.X, msg.Y); ok {
			return act, true
		}
		if act, ok := r.tabClick(msg.X, msg.Y); ok {
			return act, true
		}
		if act, ok := r.crumbClick(msg.X, msg.Y); ok {
			return act, true
		}
		// A click over the output pane focuses it, as the wheel does — consumed,
		// so the body screen never sees clicks aimed at the log.
		if r.outputVisible() && !r.currentMask().Output && r.inOutput(msg.Y) {
			r.setOutputFocused(true)
			return Action{}, true
		}
	}
	if msg.Button != tea.MouseWheelUp && msg.Button != tea.MouseWheelDown {
		return Action{}, false
	}
	if !r.outputVisible() || r.currentMask().Output || !r.inOutput(msg.Y) {
		return Action{}, false
	}
	ch := r.sh.Chrome
	r.setOutputFocused(true)
	// The pane forwards to a bubbles viewport, which matches on the concrete message
	// type — so it gets the original mm, not the flattened tea.Mouse used for hit-testing.
	return Async(ch.Output.Update(mm)), true
}

// headerClick hit-tests a left click against the header box — the topmost chrome,
// rows [0, header height). A hit fires the consumer's HeaderPane.OnClick with the
// click's cell coordinates (y is also the header-local row, for a closure that
// sub-divides its box). Returns handled=false when the header is masked, hidden,
// or has no OnClick, so the click falls through to whatever lies below.
func (r *Router) headerClick(x, y int) (Action, bool) {
	if r.currentMask().Header {
		return Action{}, false
	}
	ch := r.sh.Chrome
	if ch == nil || ch.Header == nil || ch.Header.Hidden() || ch.Header.OnClick == nil {
		return Action{}, false
	}
	if y < 0 || y >= vheight(ch.Header.view(r.sh)) {
		return Action{}, false
	}
	return ch.Header.OnClick(r.sh, x, y), true
}

// tabClick hit-tests a left click against the tab strip: the tab under the cursor
// is activated and unwound to its root in one message (ShowTab) — the mouse analog
// of the [ / ] tab keys, but available from any depth. Only the strip's first row
// is live; the rule below it is dead space. Returns handled=false for clicks
// anywhere else (masked/absent strip, the rule row, past the last tab) so the
// active screen still gets them.
func (r *Router) tabClick(x, y int) (Action, bool) {
	mask := r.currentMask()
	if mask.TabStrip || len(r.tabs) < 2 {
		return Action{}, false
	}
	// The strip's row: right below the header, gated by the mask the same way
	// topChrome stacks them.
	stripY := 0
	if !mask.Header && r.sh.Chrome != nil {
		stripY = vheight(r.sh.Chrome.Header.view(r.sh))
	}
	if y != stripY {
		return Action{}, false
	}
	for i, sp := range r.tabSpans() {
		if x < sp.start || x >= sp.end {
			continue
		}
		if i == r.active {
			return Action{}, true // the current tab: consume the click, go nowhere
		}
		return ShowTab(r.tabs[i].Title), true
	}
	return Action{}, false
}

// crumbClick hit-tests a left click against the breadcrumb bar: the segment
// under the cursor maps to a stack depth (crumbTrail pairs segments with stack
// indices), and the router pops back to it. Returns handled=false for clicks
// anywhere else — the bar's rule row, separators, a truncated trail (segments
// are cut, so no span is trustworthy), or with the pane hidden/masked — so the
// active screen still gets its own clicks.
func (r *Router) crumbClick(x, y int) (Action, bool) {
	mask := r.currentMask()
	if mask.Breadcrumb || len(r.stack) < 2 {
		return Action{}, false
	}
	ch := r.sh.Chrome
	if ch != nil && ch.Breadcrumb != nil && ch.Breadcrumb.Hidden() {
		return Action{}, false
	}
	// The bar's row: below the header and tab strip, each gated by the mask the
	// same way topChrome stacks them.
	barY := 0
	if !mask.Header && ch != nil {
		barY += vheight(ch.Header.view(r.sh))
	}
	if !mask.TabStrip {
		barY += vheight(r.tabStripView())
	}
	if y != barY {
		return Action{}, false
	}
	crumbs, idxs := r.crumbTrail()
	spans, ok := crumbSpans(crumbs, r.sh.width)
	if !ok {
		return Action{}, false
	}
	for i, sp := range spans {
		if x < sp.start || x >= sp.end {
			continue
		}
		if n := len(r.stack) - 1 - idxs[i]; n > 0 {
			return Pop(n), true
		}
		return Action{}, true // the current segment: consume the click, go nowhere
	}
	return Action{}, false
}

// inOutput reports whether terminal row y falls inside the output box. The box is
// bottom-anchored chrome — frame stacks the status line, the output box, then the help
// bar against the bottom edge — so its rows are the Height() sitting above the help.
// Clamped at 0 for terminals too short to hold all router-owned chrome.
func (r Router) inOutput(y int) bool {
	top := r.Top()
	last := r.sh.height - r.helpHeightFor(top, r.maskOf(top)) - 1
	first := last - r.sh.Chrome.Output.Height() + 1
	if first < 0 {
		first = 0
	}
	return y >= first && y <= last
}

// switchTab moves the active tab by delta (wrapping), but only at the root — when
// drilled into a sub-screen the live stack belongs to the active tab and must not
// be swapped out from under it. The cached root preserves the tab's prior state.
// Reports whether it switched; when it didn't, the key passes through to the
// active screen (so [ / ] can be typed into a form at depth).
func (r *Router) switchTab(delta int) bool {
	if len(r.tabs) < 2 || len(r.stack) != 1 {
		return false
	}
	r.active = (r.active + delta + len(r.tabs)) % len(r.tabs)
	r.stack = []Screen{r.roots[r.active]}
	return true
}

// clearOutput empties the output pane and the status line and returns focus to the
// body (the Clear key). No-op without chrome.
func (r *Router) clearOutput() {
	ch := r.sh.Chrome
	if ch == nil {
		return
	}
	if ch.Output != nil {
		ch.Output.Clear()
	}
	if ch.Status != nil {
		ch.Status.Clear()
	}
	r.setOutputFocused(false)
}

// setOutputFocused is the single writer for the output pane's focus flag, so the
// body screen's focus always tracks it: on a transition the top screen is told
// via FocusableScreen (a ModularScreen dims its active pane, a form its border)
// and told again when the keys return. Screens without the capability render
// the same either way.
func (r *Router) setOutputFocused(on bool) {
	ch := r.sh.Chrome
	if ch == nil || ch.outputFocused == on {
		return
	}
	ch.outputFocused = on
	if f, ok := r.Top().(FocusableScreen); ok {
		f.SetFocused(!on)
	}
}
