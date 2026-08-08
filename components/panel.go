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
// informational-only: skipped in tab traversal and never routed keys.
type Focusable interface {
	Focus()
	Blur()
	Focused() bool
}

// PanelUpdater receives input: key msgs while the panel is focused (or
// capturing), and every non-key msg as a broadcast. The bool reports whether the
// msg was consumed — an unhandled key falls through to the screen's own
// tab/Back handling.
type PanelUpdater interface {
	UpdatePanel(sh *core.Shared, msg tea.Msg) (act core.Action, handled bool)
}

// Capturing reports that the panel is capturing text (a /-filter, a typing form
// child). While any panel is capturing, ModularScreen routes every keystroke to
// it regardless of focus bookkeeping and reports Filtering() to the router, so
// neither the pane cycle nor the router's global single-key shortcuts steal
// filter text.
type Capturing interface{ Capturing() bool }

// PanelHelper contributes the focused panel's key hints to the screen's help bar.
type PanelHelper interface{ PanelHelp() []key.Binding }

// panelInitializer is the optional hook a panel implements to be initialized with
// the host screen's Shared — ScreenPanel uses it to Init and size its child
// screen. ModularScreen calls it once from its own Init and batches the cmds.
type panelInitializer interface{ Init(*core.Shared) tea.Cmd }

// FocusEnd ends a slot's focus chain: tab from a slot whose NextFocus is
// FocusEnd returns focus to the first Focusable slot instead of advancing (see
// Slot.NextFocus).
const FocusEnd = -1

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
	// Expand lets the slot absorb its column's leftover rows: panels render at
	// most their Weight allocation, and a slot whose content renders shorter (a
	// form's box, say) leaves the rest unrendered. After measuring, the screen
	// splits that slack equally among the column's Expand slots (remainder to
	// the last) and re-sizes them — one Expand slot is "take the rest of the
	// screen". Panels that already fill their allocation (ScrollContainer,
	// ListPanel) never trigger it.
	Expand bool
	// NextFocus is the tab focus target, by 1-based index into the
	// flattened slot order (column 0 top→bottom, then column 1 top→bottom, …):
	//
	//   - 0 (unset): advance to the next Focusable slot in flattened order,
	//     wrapping — the default loop, so Slot{Panel: x} needs no NextFocus.
	//   - N > 0: jump to flattened slot N, which must be Focusable (an invalid
	//     target leaves focus where it is).
	//   - FocusEnd: the chain ends here; tab returns focus to the first
	//     Focusable slot.
	NextFocus int
}
