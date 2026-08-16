package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Panel is one cell of a ModularScreen: informational or interactive. The screen
// owns layout and focus; a panel owns its content and (via the optional
// capability interfaces below) its input. SetSize receives the OUTER cell
// dimensions — a panel that draws a border subtracts it itself — and View
// renders the cell, told whether it currently holds focus. Panels name no domain
// type, like every component; the consumer fills the slots.
type Panel interface {
	SetSize(width, height int)
	View(focused bool) string
}

// The optional capabilities below are type-asserted by ModularScreen, the same
// opt-in idiom as the core screen capabilities (see core/screen.go): a panel
// only carries what it uses.

// Focusable marks a panel that can hold focus. A panel without it is
// informational-only: skipped in pane traversal and never routed keys.
type Focusable interface {
	Focus()
	Blur()
	Focused() bool
}

// FocusNotifier is the optional hook a panel implements to act the MOMENT it gains
// focus, instead of waiting for a message to reach it. The pane-navigation keys are
// the host's alone and never arrive at a panel (see PanelUpdater and Capturing), so a
// panel that wants to start work on focus — an animation, a lazy load — otherwise has
// to wait for some unrelated keystroke to notice it is live. ModularScreen calls it
// after Focus(), so Focused() already reports true, and batches the cmd.
//
// It takes no Shared, unlike panelInitializer: focus is granted from places that have
// none in scope (the constructor, FocusSlot, SetFocused), and a panel that needs shared
// state on focus already stashed it at Init.
//
// It does NOT fire for ModularScreen.SetFocused — the router's output-pane focus
// transitions come through core.FocusableScreen, which has no cmd lane to carry one. A
// panel that must also cover that path keeps a check on its normal message route; see
// ListPanel.marqueeArm.
type FocusNotifier interface{ OnFocus() tea.Cmd }

// PanelUpdater receives input: key msgs while the panel is focused (or
// capturing), and every non-key msg as a broadcast. The bool reports whether the
// msg was consumed — an unhandled key falls through to the screen's own Back
// handling. The pane-navigation keys never arrive here at all: the host claims
// them before the panel is consulted (see Capturing).
type PanelUpdater interface {
	UpdatePanel(sh *core.Shared, msg tea.Msg) (act core.Action, handled bool)
}

// Capturing reports that the panel is capturing text (a /-filter, a typing form
// child). While the FOCUSED panel is capturing, ModularScreen routes every
// keystroke to it and reports Filtering() to the router, so the router's global
// single-key shortcuts don't steal filter text. A capturing panel that loses
// focus (a click elsewhere) stops claiming keys until it is focused again.
//
// The one exception is the pane-navigation keys (core.Keys.PaneNext et al.),
// which the host matches above every panel: capture is total for everything a
// panel could plausibly want, and the handful of reserved keys are what keeps
// the pane escapable from the keyboard. Without that a full-capture panel
// (an embedded EditorScreen, whose whole job is to type every key) would be a
// trap needing a bespoke exit hook.
type Capturing interface{ Capturing() bool }

// PanelHelper contributes the focused panel's key hints to the screen's help bar.
type PanelHelper interface{ PanelHelp() []key.Binding }

// panelInitializer is the optional hook a panel implements to be initialized with
// the host screen's Shared — ScreenPanel uses it to Init and size its child
// screen. ModularScreen calls it once from its own Init and batches the cmds.
type panelInitializer interface{ Init(*core.Shared) tea.Cmd }

// isFocusable reports whether a panel opts into focus traversal.
func isFocusable(p Panel) bool {
	_, ok := p.(Focusable)
	return ok
}

// Slot places one Panel in a ModularScreen column.
type Slot struct {
	Panel Panel
	// Weight is the slot's share of its column's height relative to its siblings;
	// 0 counts as 1. The column's last slot always takes the rounding remainder,
	// so the column's heights sum exactly.
	Weight int
	// ExpandV lets the slot absorb its column's leftover ROWS: panels render at
	// most their Weight allocation, and a slot whose content renders shorter (a
	// form's box, say) leaves the rest unrendered. After measuring, the screen
	// splits that slack equally among the column's ExpandV slots (remainder to
	// the last) and re-sizes them — one ExpandV slot is "take the rest of the
	// screen". Panels that already fill their allocation (ScrollContainer,
	// ListPanel) never trigger it.
	ExpandV bool
	// ExpandH pads the slot's render out to its column's allocated WIDTH. Where
	// ExpandV redistributes rows nobody claimed, this claims nothing new — the
	// column's width is already assigned — it only stops a panel that renders
	// narrower than it was given (an EditorScreen showing a short document, whose
	// block is as wide as its longest line) from leaving the column a ragged right
	// edge. Padding is with spaces and never truncates, so it is invisible except
	// where it fixes the gap.
	ExpandH bool
}
