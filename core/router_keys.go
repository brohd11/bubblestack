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

// quitAction resolves q / ctrl+c by walking the stack top-down for a QuitGater, so a modal
// above the gating screen does not silence it; otherwise it quits.
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

// capturingText reports whether the top screen is capturing typed text that k would feed.
// A modified key types nothing, so it never counts.
func (r *Router) capturingText(k string) bool {
	f, ok := r.Top().(Filterer)
	return ok && f.Filtering() && !modifiedKey(k)
}

// dirKeyAction resolves a DirLocator-based global key (Terminal, TerminalWindow, OpenDir)
// at the top screen's directory. It reports false, letting the key fall through to the
// screen, when the action is unwired, the screen is capturing text, or it has no directory.
func (r *Router) dirKeyAction(k string, b key.Binding, action func(string) Action) (Action, bool) {
	if action == nil || !MatchKey(k, b) || r.capturingText(k) {
		return Action{}, false
	}
	if loc, ok := r.Top().(DirLocator); ok {
		if dir, ok := loc.LocateDir(); ok {
			return action(dir), true
		}
	}
	return Action{}, false
}

// globalKey handles the keys available in any screen, reporting whether it consumed k.
// Pointer receiver: tab switching mutates active/stack.
func (r *Router) globalKey(msg tea.KeyPressMsg) (Action, bool) {
	k := msg.String()
	if k == "ctrl+c" {
		return r.quitAction(), true
	}

	// Before the output-focused branch, so refresh works with the pane focused too.
	if r.refreshAction != nil && MatchKey(k, Keys.Refresh) && !r.capturingText(k) {
		return r.refreshAction(r.sh), true
	}

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

	// Wrap goes to the output pane while it is focused, otherwise to the top screen if it
	// wraps, otherwise to the pane.
	if MatchKey(k, Keys.Wrap) {
		paneFocused := outputOn && ch.outputFocused
		if paneFocused || !r.capturingText(k) {
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

	// Mouse capture costs the terminal's drag-select, so it toggles from any screen (above
	// the focused branch, which would swallow it). Router.View reads mouseOn.
	if MatchKey(k, Keys.Mouse) && !r.capturingText(k) {
		r.mouseOn = !r.mouseOn
		if r.mouseOn {
			return SetStatus("mouse on · wheel scrolls"), true
		}
		return SetStatus("mouse off · text selection on"), true
	}

	// A focused output pane takes navigation keys. Top/Bottom are matched here because the
	// viewport's own keymap binds neither.
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

	// The output keys pass through when there is no output pane, so a chromeless app can
	// bind them itself.
	if !r.capturingText(k) {
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
			return r.quitAction(), true
		case MatchKey(k, Keys.NextTab):
			return Action{}, r.switchTab(1)
		case MatchKey(k, Keys.PrevTab):
			return Action{}, r.switchTab(-1)
		case MatchKey(k, Keys.Unwind):
			// At the root there is nothing to unwind, so the key passes through.
			if len(r.stack) > 1 {
				return ResetToRoot(), true
			}
		}
	}
	return Action{}, false
}

// mouse claims clicks on router chrome and wheel notches over the output pane, reporting
// whether it consumed the event; everything else goes to the screen. Scrolling the pane
// also focuses it, or resize would re-pin it to the bottom.
func (r *Router) mouse(mm tea.MouseMsg) (Action, bool) {
	// Only clicks and wheel notches are the router's; motion and release go to the screen
	// (editor drag-select).
	switch mm.(type) {
	case tea.MouseClickMsg, tea.MouseWheelMsg:
	default:
		return Action{}, false
	}
	msg := mm.Mouse()
	// A click or wheel over the body takes focus back from the output pane.
	if ch := r.sh.Chrome; ch != nil && ch.outputFocused &&
		!(r.outputVisible() && r.inOutput(msg.Y)) {
		r.setOutputFocused(false)
	}
	// A left click on a breadcrumb segment pops back to it.
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

// headerClick fires HeaderPane.OnClick for a left click in the header box, with cell
// coordinates (y is also the header-local row). False when the header is masked, hidden or
// has no handler.
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

// tabClick activates the clicked tab, unwound to its root (ShowTab), from any depth. Only
// the strip's first row is live; other clicks fall through.
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

// crumbClick pops back to the clicked breadcrumb segment. Rule rows, separators, a
// truncated trail and a hidden bar fall through.
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

// inOutput reports whether row y is inside the output box, which sits above the help bar
// at the bottom.
func (r Router) inOutput(y int) bool {
	top := r.Top()
	last := r.sh.height - r.helpHeightFor(top, r.maskOf(top)) - 1
	first := max(last-r.sh.Chrome.Output.Height()+1, 0)
	return y >= first && y <= last
}

// switchTab moves the active tab by delta (wrapping), only at the root, and reports
// whether it did; otherwise the key passes to the screen.
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

// setOutputFocused is the single writer of the output pane's focus, telling the top screen
// via FocusableScreen on each transition.
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
