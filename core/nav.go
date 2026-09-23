package core

// Navigation messages. Screens return them (via the constructors below) instead of
// touching the stack, and the router applies them in the same tick; an async cmd can
// return one too.
type (
	pushMsg         struct{ s Screen }      // push a new screen on top
	popMsg          struct{ n int }         // pop n screens (back / cancel)
	popToMsg        struct{}                // pop to the nearest PopStopper, or the root
	replaceMsg      struct{ s Screen }      // pop current + push (e.g. fetching -> versions)
	resetToRootMsg  struct{}                // unwind to the root (browse) screen
	showTabMsg      struct{ title string }  // make the tab with this title active, at its root
	seqMsg          struct{ acts []Action } // a sequence of Actions applied in order
	refreshRootsMsg struct{}                // rebuild every cached tab root from its constructor
)

func (pushMsg) isCtrl()         {}
func (popMsg) isCtrl()          {}
func (popToMsg) isCtrl()        {}
func (replaceMsg) isCtrl()      {}
func (resetToRootMsg) isCtrl()  {}
func (showTabMsg) isCtrl()      {}
func (seqMsg) isCtrl()          {}
func (refreshRootsMsg) isCtrl() {}

// Nav constructors return an Action with only the control lane set.

func Push(s Screen) Action { return Action{Msg: pushMsg{s}} }

// Pop pops one screen, or n. The root is never popped.
func Pop(n ...int) Action {
	count := GetOptional(1, n...)
	return Action{Msg: popMsg{count}}
}

// PopTo unwinds to the nearest PopStopper (or the root).
func PopTo() Action { return Action{Msg: popToMsg{}} }

func Replace(s Screen) Action { return Action{Msg: replaceMsg{s}} }
func ResetToRoot() Action     { return Action{Msg: resetToRootMsg{}} }

// PropagateAll broadcasts payload to every tab root, the active stack and the App (each
// a Receiver), which may answer with Actions such as ShowTab or RefreshRoots.
func PropagateAll(payload any) Action { return Action{Msg: propagateMsg{payload}} }

// RefreshRoots rebuilds every cached tab root so it re-bakes its styles, the App's
// answer to a theme change.
func RefreshRoots() Action { return Action{Msg: refreshRootsMsg{}} }

// ShowTab activates the tab titled title, unwound to its root; a no-op if none matches.
func ShowTab(title string) Action { return Action{Msg: showTabMsg{title}} }

// Seq applies several Actions in order in the same tick (the control-lane counterpart of
// tea.Batch); zero Actions are skipped.
func Seq(acts ...Action) Action { return Action{Msg: seqMsg{acts}} }
