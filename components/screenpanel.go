package components

import (
	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// ScreenPanel embeds a full core.Screen as one panel of a ModularScreen — a
// nested screen for when a pane needs behavior no single-purpose panel has (a
// form next to a detail view, say). The child keeps its whole Update contract:
// every message routed to the panel goes to child.Update, and the returned
// (possibly new) screen replaces the child, exactly as the router would. That
// makes the panel a focus sink by design: UpdatePanel always reports
// handled=true, so shift+tab and esc never fall through to the host while a
// ScreenPanel is the route target — the child decides everything, including esc.
//
// Two caveats follow from host and child sharing one router:
//   - a child that returns core.Pop() pops the host ModularScreen (a child may
//     dismiss its host); core.Push works normally, stacking over the host.
//   - PanelHelp is not implemented: core.Screen renders its help as a finished
//     string (HelpView), not as []key.Binding, so there is nothing to merge into
//     the host's help bar.
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

// Init captures the Shared the child's View/SetSize signatures need and runs the
// child's own Init. A size assigned before Init is applied now.
func (p *ScreenPanel) Init(sh *core.Shared) tea.Cmd {
	p.sh = sh
	if p.width > 0 {
		p.child.SetSize(sh, p.width, p.height)
	}
	return p.child.Init(sh)
}

func (p *ScreenPanel) Focus()        { p.focused = true }
func (p *ScreenPanel) Blur()         { p.focused = false }
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
// panel is the route target (see the type doc for what that costs shift+tab).
func (p *ScreenPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	next, act := p.child.Update(sh, msg)
	if next != nil {
		p.child = next
	}
	return act, true
}

// Capturing proxies the child's capture state: a filtering child (core.Filterer)
// or a typing child (Typable) claims the host's keystrokes, exactly as it would
// claim the router's global keys were it a full screen.
func (p *ScreenPanel) Capturing() bool {
	if f, ok := p.child.(core.Filterer); ok && f.Filtering() {
		return true
	}
	if t, ok := p.child.(Typable); ok && t.Typing() {
		return true
	}
	return false
}
