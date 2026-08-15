// Package bubblestack is the consumer-facing entry point to the TUI framework: a
// thin facade over core that hides the Shared/Router/bubbletea wiring behind a
// single Run call. A consumer supplies only its own context, optional header/output
// chrome, theme, and tabs; everything else is constructed here.
//
// This is deliberately a small surface. The deeper API — navigation commands,
// chrome/style helpers, reusable screens — still lives in core and components and
// is imported directly; only the few names the entry point touches are re-exported
// below.
package bubblestack

import (
	"github.com/brohd11/bubblestack/config"
	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Re-exported as aliases (not new types) so a consumer can build these at the call
// site without importing core, while screens still satisfy core.Screen unchanged.
type (
	Shared     = core.Shared
	TabEntry   = core.TabEntry
	Screen     = core.Screen
	Output     = core.Output
	Status     = core.Status
	ChromeMask = core.ChromeMask
	Action     = core.Action
)

// FullscreenMask re-exports core.FullscreenMask so a consumer's screen can return it
// from ChromeMask() to claim the whole canvas without importing core.
var FullscreenMask = core.FullscreenMask

// Config is the consumer-supplied input to Run. App and Tabs are required; Header,
// Output, Status, and Theme are optional. A nil Header ⇒ no header box; a nil Output ⇒
// no output pane (pass components.NewLogPane() for the default scrollable log); a nil
// Status ⇒ no status line (pass components.NewStatusLine() for the default).
type Config struct {
	App    any                       // consumer context, recovered via core.App[T]
	Header func(*core.Shared) string // persistent context box (nil ⇒ none)
	// HeaderClick fires on a left click anywhere in the header box, given the click's
	// terminal cell coordinates (the header starts at row 0, so y is also the
	// header-local row). nil ⇒ header clicks fall through to the body screen.
	HeaderClick func(sh *core.Shared, x, y int) core.Action
	Output      core.Output     // below-body pane (nil ⇒ none)
	Status      core.Status     // transient status line (nil ⇒ none)
	Tabs        []core.TabEntry // top-level tabs
	// Theme names a startup theme. Empty ⇒ the framework loads the shared
	// ~/.bubblestack/config.yml theme (what the theme picker persists), falling back to
	// the built-in default when that too is unset. Set it only to force a theme and
	// bypass the user's saved choice.
	Theme string

	// RefreshAction is the Action returned by the global Refresh key (Keys.Refresh),
	// fired from any screen/depth except while text is captured. nil ⇒ the key is
	// left to the active screen.
	RefreshAction func(*core.Shared) core.Action

	// TerminalAction is the Action returned by the global Terminal key (Keys.Terminal),
	// given the directory resolved from the top screen's core.DirLocator, fired from any
	// screen/depth except while text is captured. Wire it to a launcher (e.g.
	// sysopen.Terminal). nil ⇒ the key is left to the active screen.
	TerminalAction func(dir string) core.Action

	// OpenDirAction is the Action returned by the global OpenDir key (Keys.OpenDir) — the
	// file-manager sibling of TerminalAction, resolved from the same core.DirLocator. Wire
	// it to a launcher (e.g. sysopen.Path(dir, false)). nil ⇒ the key is left to the active
	// screen.
	OpenDirAction func(dir string) core.Action

	// Init is an app-level startup command, batched with the initial screen's Init
	// when the program starts (run once, asynchronously). Use it for app-wide
	// background work that isn't tied to any one tab (e.g. a self-update check whose
	// result writes the shared status line). nil ⇒ no app-level startup command.
	Init func(*core.Shared) tea.Cmd
}

// Run builds the chrome from the config, applies the theme, wires the router over
// the tabs, and blocks on the bubbletea program until the user quits.
func Run(cfg Config) error {
	sh := core.NewShared(cfg.App)
	sh.Chrome = &core.Chrome{Breadcrumb: core.NewBreadcrumbPane(), Output: cfg.Output, Status: cfg.Status}
	if cfg.Header != nil {
		sh.Chrome.Header = core.NewHeaderPane(cfg.Header)
		sh.Chrome.Header.OnClick = cfg.HeaderClick
	}
	// An explicit Config.Theme wins; otherwise fall back to the shared store (the picker's
	// persisted choice, applied across every bubblestack app). An empty result leaves the
	// built-in default.
	theme := cfg.Theme
	if theme == "" {
		theme = config.Theme()
	}
	if theme != "" {
		core.SetTheme(theme)
	}
	r := core.NewRouter(sh, cfg.Tabs)
	r.SetRefreshAction(cfg.RefreshAction)
	r.SetTerminalAction(cfg.TerminalAction)
	r.SetOpenDirAction(cfg.OpenDirAction)
	r.SetInit(cfg.Init)
	// Prime terminal background detection while the terminal is still in normal mode: the
	// adaptive palette (see core.Theme) resolves each color against this cached result. The
	// query is an OSC 11 report; running it here rather than on the first alt-screen render
	// keeps its response from racing bubbletea's input reader. Non-TTY runs fall back to the
	// default (dark) gracefully. sync.Once inside lipgloss caches the answer for every later
	// resolution.
	_ = lipgloss.HasDarkBackground()
	// Cell motion (not all motion) reports the wheel and clicks but only streams motion
	// while a button is held, so there's no hover traffic through Update. It costs the
	// terminal's own drag-select; the Mouse key (m) turns it back off to copy text out.
	_, err := tea.NewProgram(r, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}
