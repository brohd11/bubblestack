package components

import (
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// FocusableScreen aliases core.FocusableScreen. ScreenPanel also forwards the host
// ModularScreen's focus through it, so a nested screen dims when a sibling pane is
// focused.
type FocusableScreen = core.FocusableScreen

// PaneOriginer is a screen that lays things out in absolute cells and needs its pane's
// top-left corner (an editor anchoring its save-as box). ModularScreen pushes each slot's
// origin; ScreenPanel forwards it to its child.
type PaneOriginer interface{ SetPaneOrigin(x, y int) }

// ScreenPanel embeds a full core.Screen as one ModularScreen panel, for panes that need
// more than a single-purpose panel. The child keeps its whole Update contract: every
// message goes to child.Update and the returned screen replaces it. It tells the child it
// is embedded (fixing mouse geometry) and whether its pane is focused (see syncChild).
//
// Caveats, from host and child sharing one router:
//   - A child returning core.Pop() pops the host; core.Push stacks over it.
//   - There is no PanelHelp: a screen's help is a finished string.
//   - UpdatePanel always reports handled, so esc never falls through to the host. Which
//     keys reach the child at all is narrower (see Capturing); the host's pane keys are
//     claimed before any panel.
type ScreenPanel struct {
	child   core.Screen
	sh      *core.Shared // captured at Init; the child's View/SetSize need it
	width   int
	height  int
	focused bool
	ox, oy  int // the pane's absolute origin, pushed by the host ModularScreen
	hasOrig bool
}

var _ Panel = (*ScreenPanel)(nil)
var _ Focusable = (*ScreenPanel)(nil)
var _ PanelUpdater = (*ScreenPanel)(nil)
var _ Capturing = (*ScreenPanel)(nil)
var _ panelInitializer = (*ScreenPanel)(nil)
var _ PaneOriginer = (*ScreenPanel)(nil)
var _ core.Receiver = (*ScreenPanel)(nil)

// NewScreenPanel wraps child as a panel. The child's Init runs once, from the
// host ModularScreen's Init.
func NewScreenPanel(child core.Screen) *ScreenPanel { return &ScreenPanel{child: child} }

// SetChild swaps the wrapped screen (a detail pane following the selection, an editor
// switching buffers); keeping the old one alive is the caller's business. Once
// initialized, the new child gets the current size and its Init runs, the cmd returned
// for the caller to emit. Focus state carries over. Init runs on every swap, including
// back to an earlier child, so a child's one-time load must be idempotent (see
// editor.Screen.Init).
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

// Receive forwards broadcasts to the child, so async results reach it even while another
// screen sits on top of the root.
func (p *ScreenPanel) Receive(sh *core.Shared, payload any) core.Action {
	if child, ok := p.child.(core.Receiver); ok {
		return child.Receive(sh, payload)
	}
	return core.Action{}
}

// syncChild tells the child it is embedded and whether the pane is focused, before any
// SetSize. Focus is pushed rather than only forwarded on transitions: screens default to
// focused, and ModularScreen never blurs the slots it did not focus at construction.
func (p *ScreenPanel) syncChild() {
	if e, ok := p.child.(core.Embeddable); ok {
		e.SetEmbedded(true)
	}
	if f, ok := p.child.(FocusableScreen); ok {
		f.SetFocused(p.focused)
	}
	if p.hasOrig {
		if po, ok := p.child.(PaneOriginer); ok {
			po.SetPaneOrigin(p.ox, p.oy)
		}
	}
}

// SetPaneOrigin implements PaneOriginer, storing the origin and forwarding it to the
// child.
func (p *ScreenPanel) SetPaneOrigin(x, y int) {
	p.ox, p.oy, p.hasOrig = x, y, true
	if po, ok := p.child.(PaneOriginer); ok {
		po.SetPaneOrigin(x, y)
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

// SetSize forwards the outer dims to the child (the panel draws no chrome). Before Init
// they are stashed.
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

// UpdatePanel forwards msg to the child and always reports handled. The returned screen
// replaces the child only if the child was not swapped mid-update (an OnExit calling
// SetChild), which would otherwise be clobbered.
func (p *ScreenPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	before := p.child
	next, act := before.Update(sh, msg)
	if next != nil && p.child == before {
		p.child = next
	}
	return act, true
}

// Capturing proxies the child's capture: a Typable child captures only while a text field
// has focus, so sibling panels stay reachable from a form's toggle rows; others fall back
// to Filtering. Narrower than the router's rule on purpose, since FormScreen.Filtering is
// always true.
func (p *ScreenPanel) Capturing() bool {
	if t, ok := p.child.(Typable); ok {
		return t.Typing()
	}
	if f, ok := p.child.(core.Filterer); ok {
		return f.Filtering()
	}
	return false
}
