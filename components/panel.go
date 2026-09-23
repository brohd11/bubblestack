package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Panel is one cell of a ModularScreen. The screen owns layout and focus; a panel owns its
// content and, through optional capabilities, its input. SetSize gets the outer cells (a
// bordered panel subtracts its own border); View is told whether it holds focus.
type Panel interface {
	SetSize(width, height int)
	View(focused bool) string
}

// Optional panel capabilities, type-asserted by ModularScreen.

// Focusable marks a panel that can hold focus. A panel without it is
// informational-only: skipped in pane traversal and never routed keys.
type Focusable interface {
	Focus()
	Blur()
	Focused() bool
}

// FocusNotifier lets a panel act the moment it gains focus (an animation, a lazy load),
// since pane keys never reach it. Called after Focus(); the cmd is batched. It takes no
// Shared because focus is granted where none is in scope. It does not fire for
// ModularScreen.SetFocused (no cmd lane); see ListPanel.marqueeArm.
type FocusNotifier interface{ OnFocus() tea.Cmd }

// PanelUpdater receives keys while the panel is focused (or capturing) and every non-key
// message as a broadcast, reporting whether it consumed the message. Pane keys never
// arrive.
type PanelUpdater interface {
	UpdatePanel(sh *core.Shared, msg tea.Msg) (act core.Action, handled bool)
}

// Capturing reports text capture (a /-filter, a typing form). A focused capturing panel
// gets every key and the screen reports Filtering to the router. A panel that loses focus
// stops capturing. Pane keys are the exception, which keeps full-capture panels (an
// embedded editor) escapable.
type Capturing interface{ Capturing() bool }

// PanelHelper contributes the focused panel's key hints to the screen's help bar.
type PanelHelper interface{ PanelHelp() []key.Binding }

// panelInitializer is initialized with the host's Shared (ScreenPanel starts its child).
// ModularScreen calls it once from Init.
type panelInitializer interface{ Init(*core.Shared) tea.Cmd }

// isFocusable reports whether a panel opts into focus traversal.
func isFocusable(p Panel) bool {
	_, ok := p.(Focusable)
	return ok
}

// Slot places one Panel in a ModularScreen column.
type Slot struct {
	Panel Panel
	// Weight is the slot's share of its column's height (0 counts as 1). The last slot takes
	// the remainder.
	Weight int
	// ExpandV lets the slot absorb rows its column's siblings rendered short of their
	// allocation (split among ExpandV slots). One ExpandV slot means "take the rest".
	ExpandV bool
	// ExpandH pads the slot's render to its column width so a narrow render (a short
	// document) leaves no ragged edge. Padding never truncates.
	ExpandH bool
}
