package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// DialogScreen is the shared y/n confirm/summary box. It serves two shapes behind the
// Overlay flag, which is the only behavioral switch:
//
//   - Overlay false (a confirm): full-screen, rendered in the body via core.WithTitle,
//     with its hints in the chrome help bar. Reused by the install/archive/new-plugin
//     confirms.
//   - Overlay true (a popup): the router draws it as a centered modal over the screen
//     below it (core.Overlayer), rendered via core.PopupBox with its hints inside the
//     box; the background screen's help bar stays, and so does its breadcrumb — a popup
//     contributes no segment of its own (see CrumbLabel).
//
// Either way it is context-agnostic: it renders its body via a closure, and the
// OnYes/OnKey closures (supplied by the caller) decide what happens — it names no domain
// type. OnYes runs on confirm (y/enter); No (esc/n) pops it; any other key is handed to
// OnKey when set.
type DialogScreen struct {
	Title      string // in-body title bar (confirm) / accent line (overlay); omitted ⇒ none
	Crumb      string // CONFIRM ONLY: breadcrumb segment (CrumbLabel); omitted ⇒ "Conf"
	CrumbShort string // CONFIRM ONLY: optional short breadcrumb-bar segment; defaults to Crumb
	Render     func(*core.Shared) string
	OnYes      func(*core.Shared) core.Action
	OnKey      func(*core.Shared, string) core.Action // handles keys other than the reserved confirm/cancel keys
	// OnQuit, when set, answers the router's quit-gate consultation while the
	// dialog is on top — a quit confirm uses it to keep q/ctrl+c as the
	// force-quit (without it the stack walk would find the gate that pushed the
	// dialog and stack another popup). Nil ⇒ the dialog abstains.
	OnQuit  func(*core.Shared) (core.Action, bool)
	Help    []key.Binding
	Overlay bool // draw as a centered modal over the screen below (core.Overlayer)
	Width   int  // overlay inner content width; 0 ⇒ size to content (overlay only)
}

type ConfirmSimple struct {
	Text  string
	OnYes core.Action
	// optional
	Title       string // optional in-body title bar; omitted ⇒ no bar
	Crumb       string
	CrumbShort  string
	Render      func(*core.Shared) string // this overides text if not nil
	OnYesLambda func(*core.Shared) core.Action
	OnKey       func(*core.Shared, string) core.Action
	Help        []key.Binding
}

var _ core.Crumber = (*DialogScreen)(nil)
var _ core.Overlayer = (*DialogScreen)(nil)
var _ core.QuitGater = (*DialogScreen)(nil)

func (s *DialogScreen) Init(*core.Shared) tea.Cmd { return nil }

// QuitGate implements core.QuitGater: it delegates to OnQuit when set and
// abstains otherwise, letting the router's stack walk continue below the dialog.
func (s *DialogScreen) QuitGate(sh *core.Shared) (core.Action, bool) {
	if s.OnQuit == nil {
		return core.Action{}, false
	}
	return s.OnQuit(sh)
}

// IsOverlay reports whether the router should draw this dialog as a centered modal
// over the screen below it (Overlay) rather than full-screen.
func (s *DialogScreen) IsOverlay() bool { return s.Overlay }

// CrumbLabel contributes the dialog's breadcrumb segment — for the CONFIRM shape only,
// which uses its Crumb (default "Conf"). An overlay returns "", which crumbTrail skips.
//
// That is the stance MenuScreen (menu.go) and LineEditScreen (lineedit.go) already take,
// and the rule behind all three: a screen that REPLACES the screen below it is somewhere
// you navigated to and gets a segment; a box drawn OVER one is not, and a trail that grew
// a segment each time a popup opened would flicker a step in and out under it.
func (s *DialogScreen) CrumbLabel(short bool) string {
	if s.Overlay {
		return ""
	}
	return crumbSeg(short, s.CrumbShort, s.Crumb, "Conf")
}

func (s *DialogScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return s, core.Action{}
	}
	k := key.String()
	switch {
	case core.MatchKey(k, core.Keys.Yes):
		if s.OnYes != nil {
			return s, s.OnYes(sh)
		}
		return s, core.Pop()
	case core.MatchKey(k, core.Keys.No):
		return s, core.Pop()
	}
	if s.OnKey != nil {
		return s, s.OnKey(sh, k)
	}
	return s, core.Action{}
}

func (s *DialogScreen) View(sh *core.Shared) string {
	if s.Overlay {
		// A popup renders its hints inside the box (the router keeps the background
		// screen's help bar), and composites as a centered modal.
		body := s.Render(sh)
		if hint := sh.BindingHelp(s.Help); hint != "" {
			body = body + "\n\n" + hint
		}
		return core.PopupBox(s.Title, body, s.Width)
	}
	return core.WithTitle(s.Title, s.Render(sh))
}

// HelpView is the chrome help bar for a confirm; empty for a popup (which renders its
// hints inside the box).
func (s *DialogScreen) HelpView(sh *core.Shared) string {
	if s.Overlay {
		return ""
	}
	return sh.BindingHelp(s.Help)
}

func (s *DialogScreen) SetSize(*core.Shared, int, int) {}

var DefaultHelpKeys = []key.Binding{
	core.Hint("confirm", core.Keys.Yes),
	core.Hint("cancel", core.Keys.No),
}

// DefaultPopupHelp is the standard single-key "done" hint for an acknowledgement popup.
var DefaultPopupHelp = []key.Binding{core.Hint("done", core.Keys.Yes)}

// CreateConfirmScreen builds a full-screen confirm (Overlay false) from the simplified
// ConfirmSimple config.
func CreateConfirmScreen(cs ConfirmSimple) *DialogScreen {
	if cs.Help == nil {
		cs.Help = DefaultHelpKeys
	}
	render := func(sh *core.Shared) string { return sh.Box(cs.Text) }
	if cs.Render != nil {
		render = cs.Render
	}
	onYes := func(sh *core.Shared) core.Action { return cs.OnYes }
	if cs.OnYesLambda != nil {
		onYes = cs.OnYesLambda
	}

	return &DialogScreen{
		Title:      cs.Title,
		Crumb:      cs.Crumb,
		CrumbShort: cs.CrumbShort,
		Render:     render,
		OnYes:      onYes,
		OnKey:      cs.OnKey,
		Help:       cs.Help,
	}
}

// CreatePopup builds a text-only acknowledgement popup (title + body, Overlay true):
// OnYes is the action taken when dismissed with y/enter (defaults to a plain Pop when
// nil), and Help defaults to the "done" hint.
func CreatePopup(title, body string, onYes core.Action, help ...key.Binding) *DialogScreen {
	if help == nil {
		help = DefaultPopupHelp
	}
	return &DialogScreen{
		Title:   title,
		Render:  func(*core.Shared) string { return body },
		OnYes:   func(*core.Shared) core.Action { return onYes },
		Help:    help,
		Overlay: true,
	}
}
