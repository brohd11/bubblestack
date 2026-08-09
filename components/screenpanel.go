package components

import (
	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// FocusableScreen is implemented by a full screen that can render a focused and
// an unfocused state (a form tinting its box border, say). It aliases the core
// capability the router drives on output-pane focus transitions; ScreenPanel
// additionally forwards the host ModularScreen's focus through it, so a nested
// screen dims when a sibling pane takes focus. Opt-in, like every other
// capability — a screen without it renders the same either way.
type FocusableScreen = core.FocusableScreen

// ScreenPanel embeds a full core.Screen as one panel of a ModularScreen — a
// nested screen for when a pane needs behavior no single-purpose panel has (a
// form next to a detail view, say). The child keeps its whole Update contract:
// every message routed to the panel goes to child.Update, and the returned
// (possibly new) screen replaces the child, exactly as the router would. That
// makes the panel a key sink by design: UpdatePanel always reports
// handled=true, so esc never falls through to the host while a ScreenPanel is
// the route target — the child decides everything, including esc.
//
// Embedding also tells the child what it needs to know to be a pane rather than a
// whole body — see syncChild: a core.Embeddable child learns it is embedded (which
// fixes its mouse geometry), and a FocusableScreen child learns whether this pane
// currently holds focus. Neither imposes a look: what chrome a child draws is its
// own construction-time business.
//
// Two caveats follow from host and child sharing one router:
//   - a child that returns core.Pop() pops the host ModularScreen (a child may
//     dismiss its host); core.Push works normally, stacking over the host.
//   - PanelHelp is not implemented: core.Screen renders its help as a finished
//     string (HelpView), not as []key.Binding, so there is nothing to merge into
//     the host's help bar.
//   - UpdatePanel always reports handled=true, so esc never falls through to
//     the host while a ScreenPanel is the route target. Capture (which keys
//     reach the child at all) is narrower — see Capturing. The host's
//     pane-navigation keys sit outside both: ModularScreen claims them before
//     any panel is consulted, so a sink is never a trap and this panel needs no
//     release logic of its own.
type ScreenPanel struct {
	child   core.Screen
	sh      *core.Shared // captured at Init; the child's View/SetSize need it
	width   int
	height  int
	focused bool
}

var _ Panel = (*ScreenPanel)(nil)
var _ Focusable = (*ScreenPanel)(nil)
var _ PanelUpdater = (*ScreenPanel)(nil)
var _ Capturing = (*ScreenPanel)(nil)
var _ panelInitializer = (*ScreenPanel)(nil)

// NewScreenPanel wraps child as a panel. The child's Init runs once, from the
// host ModularScreen's Init.
func NewScreenPanel(child core.Screen) *ScreenPanel { return &ScreenPanel{child: child} }

// SetChild swaps the wrapped screen — the move a pane that cycles through same-type
// children makes (a detail pane showing whichever row is selected, an editor pane
// switching buffers). The old child is dropped as-is; keeping it alive (and thereby
// its state) is the caller's business. Once the panel is initialized the new child
// gets the panel's current size and its Init runs, the returned cmd being the
// caller's to emit (the framework idiom — IO only in the cmd lane); before Init the
// swap is silent and the host's own Init will start the new child. The panel's focus
// state is untouched and pushed onto the new child (syncChild), so a focused pane
// stays focused on it.
func (p *ScreenPanel) SetChild(child core.Screen) tea.Cmd {
	p.child = child
	p.syncChild()
	if p.sh == nil {
		return nil
	}
	if p.width > 0 {
		p.child.SetSize(p.sh, p.width, p.height)
	}
	return p.child.Init(p.sh)
}

// Init captures the Shared the child's View/SetSize signatures need and runs the
// child's own Init. A size assigned before Init is applied now.
func (p *ScreenPanel) Init(sh *core.Shared) tea.Cmd {
	p.sh = sh
	p.syncChild()
	if p.width > 0 {
		p.child.SetSize(sh, p.width, p.height)
	}
	return p.child.Init(sh)
}

// syncChild hands the child the two facts only the panel knows. It runs before any
// SetSize, so the child computes its very first layout against both; a child that
// implements neither capability is left alone.
//
// Focus is pushed (not just forwarded on transitions) because a child otherwise never
// learns the state it was born into: a screen defaults to focused — standalone it
// always is — so a pane that doesn't hold focus would render its child lit, and a
// child swapped into a focused pane would render it dark. The Focus/Blur transitions
// alone can't cover either case: ModularScreen focuses one slot at construction and
// never blurs the rest, and FocusSlot is a no-op when the target already holds focus.
func (p *ScreenPanel) syncChild() {
	if e, ok := p.child.(core.Embeddable); ok {
		e.SetEmbedded(true)
	}
	if f, ok := p.child.(FocusableScreen); ok {
		f.SetFocused(p.focused)
	}
}

func (p *ScreenPanel) Focus() {
	p.focused = true
	if f, ok := p.child.(FocusableScreen); ok {
		f.SetFocused(true)
	}
}

func (p *ScreenPanel) Blur() {
	p.focused = false
	if f, ok := p.child.(FocusableScreen); ok {
		f.SetFocused(false)
	}
}

func (p *ScreenPanel) Focused() bool { return p.focused }

// SetSize takes the outer cell dims and forwards them to the child verbatim (a
// ScreenPanel draws no chrome of its own). Before Init the dims are stashed and
// applied once the Shared arrives.
func (p *ScreenPanel) SetSize(width, height int) {
	p.width, p.height = width, height
	if p.sh != nil {
		p.child.SetSize(p.sh, width, height)
	}
}

func (p *ScreenPanel) View(bool) string {
	if p.sh == nil {
		return ""
	}
	return p.child.View(p.sh)
}

// UpdatePanel forwards the message to the child and keeps the returned screen,
// reporting handled=true unconditionally — the child owns every key while this
// panel is the route target (see the type doc for what that costs tab).
func (p *ScreenPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	next, act := p.child.Update(sh, msg)
	if next != nil {
		p.child = next
	}
	return act, true
}

// Capturing proxies the child's capture state, preferring the precise signal:
// a Typable child captures only while a text field actually holds focus
// (Typing), so a nested form claims the host's keystrokes over its message
// field but releases them on a toggle row — the sibling panels stay reachable.
// A child that isn't a Typable falls back to Filterer.Filtering(). This is
// narrower than the router-level rule on purpose: FormScreen.Filtering is
// unconditionally true so a *standalone* form keeps the router's single-key
// shortcuts off its fields, and nested that would hand the form every keystroke
// on the screen.
func (p *ScreenPanel) Capturing() bool {
	if t, ok := p.child.(Typable); ok {
		return t.Typing()
	}
	if f, ok := p.child.(core.Filterer); ok {
		return f.Filtering()
	}
	return false
}
