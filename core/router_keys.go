package core

import (
	tea "github.com/charmbracelet/bubbletea"
)

// globalKey handles the keys available in any screen. It returns (act, true) when it
// consumed the key — act carries a control message resolved inline and/or an async cmd
// (e.g. tea.Quit or an output-scroll cmd) — or (Action{}, false) to let the active
// screen handle it. Pointer receiver: [ / ] mutate active/stack, which must persist
// back to Update's router.
func (r *Router) globalKey(msg tea.KeyMsg) (Action, bool) {
	k := msg.String()
	if k == "ctrl+c" {
		return Async(tea.Quit), true
	}

	// Refresh fires from any screen/depth except while text is captured (a filtering
	// list or a focused form, both reporting Filtering()). The action is
	// consumer-supplied so core names no domain type. Placed before the
	// output-focused branch so it works even with the output pane focused.
	if r.refreshAction != nil && MatchKey(k, Keys.Refresh) {
		if f, ok := r.Top().(Filterer); !ok || !f.Filtering() {
			return r.refreshAction(r.sh), true
		}
	}

	ch := r.sh.Chrome
	outputOn := ch != nil && ch.Output != nil

	// Wrap flips the output pane's render mode from any screen (like Output/Clear),
	// whether or not the pane holds focus — hence its place above the focused branch.
	// It is an optional capability (Wrapper), so an Output without it never consumes
	// the key. Filtering screens keep a literal w; a focused pane means the screen
	// isn't reading keys anyway, so the gate doesn't apply there.
	if outputOn && MatchKey(k, Keys.Wrap) {
		if w, ok := ch.Output.(Wrapper); ok {
			if f, isFilterer := r.Top().(Filterer); ch.outputFocused || !isFilterer || !f.Filtering() {
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
		if f, ok := r.Top().(Filterer); !ok || !f.Filtering() {
			r.mouseOn = !r.mouseOn
			if r.mouseOn {
				act := SetStatus("mouse on · wheel scrolls")
				act.Cmd = tea.EnableMouseCellMotion
				return act, true
			}
			act := SetStatus("mouse off · text selection on")
			act.Cmd = tea.DisableMouse
			return act, true
		}
	}

	// When the output pane holds focus, navigation keys scroll it; everything
	// else either toggles back or clears. Top/Bottom are matched here rather than
	// left to the viewport's own keymap, which binds neither.
	if outputOn && ch.outputFocused {
		switch {
		case MatchKey(k, Keys.ToggleOutput), MatchKey(k, Keys.Back):
			ch.outputFocused = false
			return Action{}, true
		case MatchKey(k, Keys.Output):
			ch.Output.Hide()
			ch.outputFocused = false
			return Action{}, true
		case MatchKey(k, Keys.Clear):
			r.clearOutput()
			return Action{}, true
		case MatchKey(k, Keys.Quit):
			return Async(tea.Quit), true
		case MatchKey(k, Keys.Top):
			ch.Output.GotoTop()
			return Action{}, true
		case MatchKey(k, Keys.Bottom):
			ch.Output.GotoBottom()
			return Action{}, true
		}
		return Async(ch.Output.Update(msg)), true
	}

	// tab jumps into the output pane, c clears the log, [ / ] switch top-level tabs
	// (only at the root, so the live stack always belongs to the active tab), and `
	// unwinds a deep stack back to the root for a quick exit — unless the active
	// screen is capturing filter text. The output keys pass through (no consume) when
	// there is no output pane, so a chromeless app can bind tab/o itself.
	if f, ok := r.Top().(Filterer); !ok || !f.Filtering() {
		switch {
		case MatchKey(k, Keys.ToggleOutput):
			if !outputOn {
				break
			}
			if ch.Output.Shown() {
				ch.outputFocused = true
				ch.Output.GotoBottom()
			}
			return Action{}, true
		case MatchKey(k, Keys.Output):
			if !outputOn {
				break
			}
			ch.Output.Toggle()
			if !ch.Output.Shown() {
				ch.outputFocused = false
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
			return Async(tea.Quit), true
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
// announce it, with tab/esc returning as they do from a keyboard focus.
func (r *Router) mouse(msg tea.MouseMsg) (Action, bool) {
	if msg.Action != tea.MouseActionPress {
		return Action{}, false
	}
	if msg.Button != tea.MouseButtonWheelUp && msg.Button != tea.MouseButtonWheelDown {
		return Action{}, false
	}
	if !r.outputVisible() || r.currentMask().Output || !r.inOutput(msg.Y) {
		return Action{}, false
	}
	ch := r.sh.Chrome
	ch.outputFocused = true
	return Async(ch.Output.Update(msg)), true
}

// inOutput reports whether terminal row y falls inside the output box. The box is
// bottom-anchored chrome — frame stacks the status line, the output box, then the help
// bar against the bottom edge — so its rows are the Height() sitting above the help.
// Clamped at 0 because frame pads a short body but doesn't clamp an overflowing one,
// which would otherwise drift the range negative on a very short terminal.
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
	ch.outputFocused = false
}
