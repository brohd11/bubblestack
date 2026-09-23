// Package bubblestack is the entry point to the TUI framework: Run wires Shared, Router
// and bubbletea from a Config. Navigation, chrome helpers and reusable screens are
// imported directly from core and components.
package bubblestack

import (
	"github.com/brohd11/bubblestack/config"
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
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

// Config is the input to Run. App and Tabs are required. Nil Header, Output or Status
// leave that chrome out (components.NewLogPane and NewStatusLine are the defaults).
type Config struct {
	App    any                       // consumer context, recovered via core.App[T]
	Header func(*core.Shared) string // persistent context box (nil ⇒ none)
	// HeaderClick fires on a left click in the header box, with cell coordinates (y is also
	// the header-local row). nil lets clicks fall through.
	HeaderClick func(sh *core.Shared, x, y int) core.Action
	Output      core.Output     // below-body pane (nil ⇒ none)
	Status      core.Status     // transient status line (nil ⇒ none)
	Tabs        []core.TabEntry // top-level tabs
	// Theme forces a startup theme, bypassing the user's saved choice in
	// ~/.bubblestack/config.yml. Empty uses the saved theme, else the default.
	Theme string

	// RefreshAction backs the global Refresh key (not while text is captured); nil leaves the
	// key to the screen.
	RefreshAction func(*core.Shared) core.Action

	// TerminalAction backs the Terminal key with the top screen's DirLocator directory (e.g.
	// sysopen.TerminalInline); nil leaves the key to the screen.
	TerminalAction func(dir string) core.Action

	// TerminalWindowAction backs the TerminalWindow key the same way (e.g. sysopen.Terminal).
	TerminalWindowAction func(dir string) core.Action

	// OpenDirAction backs the OpenDir key the same way (e.g. sysopen.Path(dir, false)).
	OpenDirAction func(dir string) core.Action

	// Init is an app-wide startup command run once alongside the first screen's Init (a
	// self-update check, say).
	Init func(*core.Shared) tea.Cmd
}

// Run builds the chrome from the config, applies the theme, wires the router over
// the tabs, and blocks on the bubbletea program until the user quits.
func Run(cfg Config) error {
	sh := core.NewShared(cfg.App)
	initListDensity(sh)
	sh.Chrome = &core.Chrome{Breadcrumb: core.NewBreadcrumbPane(), Output: cfg.Output, Status: cfg.Status}
	if cfg.Header != nil {
		sh.Chrome.Header = core.NewHeaderPane(cfg.Header)
		sh.Chrome.Header.OnClick = cfg.HeaderClick
	}
	// An explicit Config.Theme wins over the shared saved theme.
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
	r.SetTerminalWindowAction(cfg.TerminalWindowAction)
	r.SetOpenDirAction(cfg.OpenDirAction)
	r.SetInit(cfg.Init)
	// Alt screen, mouse reporting and background detection are handled through the router's
	// View and messages in v2, so nothing needs priming here.
	_, err := tea.NewProgram(r).Run()
	return err
}

// initListDensity runs before any root is constructed. Persistence is an app
// startup concern: NewShared and screens used directly never touch the config.
func initListDensity(sh *core.Shared) {
	app, ok := sh.App.(core.ListDensityProvider)
	if !ok {
		return
	}
	state := app.ListDensity()
	if state == nil {
		return
	}
	if compact, saved := config.ListDensity(); saved {
		*state = compact
	}
	sh.SaveListDensity = config.SaveListDensity
}
