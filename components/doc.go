package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// DocScreen is the reusable read-only text page: a scrollable viewport under an
// optional title bar, popped with esc. It backs any "show the user a page of prose"
// flow (help, docs, a release note) without the caller reimplementing viewport
// plumbing.
//
// It is context-agnostic — the body comes from a Render closure that is handed the
// text width and returns the finished string, so the caller owns all formatting
// (markdown, plain text, a rendered table) and DocScreen owns only scrolling. Render
// is re-run when the width changes, which is what lets a caller re-wrap on resize.
type DocScreen struct {
	Title      string // in-body title bar (core.WithTitle); empty ⇒ none
	Crumb      string // breadcrumb segment (CrumbLabel); defaults to Title
	CrumbShort string
	Render     func(width int) string
	Help       []key.Binding                                  // help-bar hints; nil ⇒ the default scroll/back pair
	OnKey      func(*core.Shared, string) (core.Action, bool) // extra keys; handled=true consumes the key
	Links      LinkHooks                                      // what a click on a link does; zero ⇒ links are inert

	vp       viewport.Model
	width    int // last laid-out terminal width; -1 until the first SetSize
	links    LinkMap
	embedded bool // this page is one pane of a layout (core.Embeddable), not the whole body
}

// DocOpts configures a DocScreen. Only Render is required.
type DocOpts struct {
	Title      string
	Crumb      string
	CrumbShort string
	Render     func(width int) string
	Help       []key.Binding
	OnKey      func(*core.Shared, string) (core.Action, bool)
	Links      LinkHooks
}

var _ core.Crumber = (*DocScreen)(nil)
var _ core.Embeddable = (*DocScreen)(nil)

func NewDocScreen(opts DocOpts) *DocScreen {
	return &DocScreen{
		Title:      opts.Title,
		Crumb:      opts.Crumb,
		CrumbShort: opts.CrumbShort,
		Render:     opts.Render,
		Help:       opts.Help,
		OnKey:      opts.OnKey,
		Links:      opts.Links,
		vp:         viewport.New(),
		width:      -1,
	}
}

// CrumbLabel contributes the page's breadcrumb segment: the short form when set, else
// the explicit crumb, else the title.
func (s *DocScreen) CrumbLabel(short bool) string {
	return CrumbSegment(short, s.CrumbShort, s.Crumb, s.Title)
}

func (s *DocScreen) Init(*core.Shared) tea.Cmd { return nil }

// gutter is the blank margin on each side of the text, so prose doesn't run into the
// terminal edge. The Render closure is handed the width net of both gutters.
const gutter = "  "

// SetSize lays the viewport out under the title bar and re-renders the body when the
// width changed — Render is width-dependent (it wraps), so a resize must re-run it,
// while a height-only change (the output pane opening) must not.
func (s *DocScreen) SetSize(_ *core.Shared, width, bodyHeight int) {
	h := bodyHeight
	if s.Title != "" {
		h -= lipgloss.Height(core.RenderTitleBar(s.Title))
	}
	if h < 1 {
		h = 1
	}
	s.vp.SetWidth(width)
	s.vp.SetHeight(h)
	if width == s.width {
		return
	}
	s.width = width
	body := s.Render(s.textWidth())
	// The map belongs to THIS render: the rows it indexes are the ones just laid out,
	// and a re-wrap at a new width moves every one of them.
	s.links = ScanLinks(body)
	s.vp.SetContent(core.IndentLines(body, gutter))
}

// SetEmbedded implements core.Embeddable: hosted as a pane (components.ScreenPanel) the
// page is handed pane-relative mouse coordinates, so it must not subtract the router's
// body offset the way a standalone pushed page must. Geometry only — nothing about how
// the page looks changes.
func (s *DocScreen) SetEmbedded(v bool) { s.embedded = v }

// clickLink answers the link under a click, in the page's own content coordinates: the
// gutter off the left, the title bar and the scroll offset off the top, and the chrome
// above the body only when this page IS the body. hit=false means the click landed on
// ordinary text and belongs to the viewport.
func (s *DocScreen) clickLink(sh *core.Shared, x, y int) (core.Action, bool) {
	if len(s.links) == 0 {
		return core.Action{}, false
	}
	if !s.embedded {
		y -= sh.BodyY()
	}
	if s.Title != "" {
		y -= lipgloss.Height(core.RenderTitleBar(s.Title))
	}
	l, ok := s.links.At(y+s.vp.YOffset(), x-len(gutter))
	if !ok {
		return core.Action{}, false
	}
	return s.Links.Do(sh, l), true
}

// textWidth is the width handed to Render: the terminal minus a gutter on each side.
func (s *DocScreen) textWidth() int {
	w := s.width - 2*len(gutter)
	if w < 20 {
		w = 20
	}
	return w
}

// Update pops on back and otherwise hands the message to the viewport, which owns
// ↑/↓/pgup/pgdn scrolling itself.
func (s *DocScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		k := msg.String()
		if core.MatchKey(k, core.Keys.Back) {
			return s, core.Pop()
		}
		if s.OnKey != nil {
			if act, handled := s.OnKey(sh, k); handled {
				return s, act
			}
		}
		// The bar advertises core.Keys.Up/Down for scrolling, but the viewport's own
		// keymap knows only its subset (up/k, down/j) — so the alias keys (alt+w/alt+s,
		// and anything a new scheme adds) were advertised and dead. Scroll here instead,
		// after OnKey so a caller can still claim one of these.
		switch {
		case core.MatchKey(k, core.Keys.Up):
			s.vp.ScrollUp(1)
			return s, core.Action{}
		case core.MatchKey(k, core.Keys.Down):
			s.vp.ScrollDown(1)
			return s, core.Action{}
		}
	}
	// A plain left click on a link, before the viewport (which only wants the wheel).
	// Modified clicks are left alone: the terminal and the host claim those.
	if m, ok := msg.(tea.MouseClickMsg); ok && m.Button == tea.MouseLeft && m.Mod == 0 {
		if act, hit := s.clickLink(sh, m.X, m.Y); hit {
			return s, act
		}
	}
	var cmd tea.Cmd
	s.vp, cmd = s.vp.Update(msg)
	return s, core.Async(cmd)
}

func (s *DocScreen) View(*core.Shared) string { return core.WithTitle(s.Title, s.vp.View()) }

func (s *DocScreen) HelpView(sh *core.Shared) string {
	help := s.Help
	if help == nil {
		help = []key.Binding{
			core.Hint("scroll", core.Keys.Up, core.Keys.Down),
			core.Hint("back", core.Keys.Back),
		}
	}
	return sh.BindingHelp(help)
}
