package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
)

// ScrollContainer is a read-only panel in the LogPane style: a bordered box with a title
// legend, scrolling caller-supplied lines (SetLines/SetStatus). It owns no navigation:
// esc falls through so the host keeps its pop.
type ScrollContainer struct {
	// OnLink handles a left click on a hyperlink from SetLinks; nil does nothing.
	OnLink func(*core.Shared, Link) core.Action

	vp         viewport.Model
	links      LinkMap
	title      string
	focused    bool
	pinned     bool // content has been set once (the first set opens at the top)
	noKeyHints bool // the focused border carries the title alone (SetKeyHints)
	width      int
	height     int
}

var _ Panel = (*ScrollContainer)(nil)
var _ Focusable = (*ScrollContainer)(nil)
var _ PanelUpdater = (*ScrollContainer)(nil)
var _ PanelHelper = (*ScrollContainer)(nil)

// NewScrollContainer builds a panel titled title (drawn as the top-border legend).
func NewScrollContainer(title string) *ScrollContainer {
	return &ScrollContainer{vp: viewport.New(), title: title}
}

// SetKeyHints turns the focused border's key legend on (the default) or off, e.g. to
// match a quiet ListPanel legend beside it. The keys stay in the help bar either way.
func (p *ScrollContainer) SetKeyHints(show bool) { p.noKeyHints = !show }

// SetTitle replaces the top-border legend. A pane whose content changes shape — a
// count, a warning — says so on its own edge rather than spending a content row on it.
func (p *ScrollContainer) SetTitle(title string) { p.title = title }

func (p *ScrollContainer) Focus()        { p.focused = true }
func (p *ScrollContainer) Blur()         { p.focused = false }
func (p *ScrollContainer) Focused() bool { return p.focused }

// SetLines replaces the content. The first set opens at the top; later refreshes keep the
// scroll position. SetStatus resets that, so content after a "loading…" status opens
// fresh.
func (p *ScrollContainer) SetLines(lines []string) {
	p.vp.SetContent(strings.Join(lines, "\n"))
	if !p.pinned {
		p.pinned = true
		p.vp.GotoTop()
	}
}

// SetStatus shows a single muted status line ("loading…", "none") in place of
// content, and re-arms the top pin for the next SetLines.
func (p *ScrollContainer) SetStatus(status string) {
	p.pinned = false
	p.vp.SetContent(core.Label(status))
	p.vp.GotoTop()
}

// SetLinks gives the pane the link spans for the content just set (ScanLinks over the same
// render). Set it with SetLines or not at all: a stale map points at moved text.
func (p *ScrollContainer) SetLinks(links LinkMap) { p.links = links }

// clickLink resolves a pane-relative click to a link, removing border, padding and the
// top edge and adding the scroll offset.
func (p *ScrollContainer) clickLink(sh *core.Shared, x, y int) (core.Action, bool) {
	if len(p.links) == 0 || p.OnLink == nil {
		return core.Action{}, false
	}
	l, ok := p.links.At(y-1+p.vp.YOffset(), x-2)
	if !ok {
		return core.Action{}, false
	}
	return p.OnLink(sh, l), true
}

// UpdatePanel scrolls on nav keys, page keys (the viewport's own keymap) and the wheel
// (only while focused, since mouse messages reach every panel). esc is left to the host.
func (p *ScrollContainer) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if m, ok := msg.(tea.MouseMsg); ok {
		if !p.focused {
			return core.Action{}, false
		}
		if w, isWheel := m.(tea.MouseWheelMsg); isWheel &&
			(w.Button == tea.MouseWheelUp || w.Button == tea.MouseWheelDown) {
			var cmd tea.Cmd
			p.vp, cmd = p.vp.Update(w)
			return core.Async(cmd), true
		}
		// A plain left click on a link. Modified clicks are left alone — the host
		// rewrites those (gote's retargetClick) and the terminal claims some itself.
		if c, isClick := m.(tea.MouseClickMsg); isClick && c.Button == tea.MouseLeft && c.Mod == 0 {
			if act, hit := p.clickLink(sh, c.X, c.Y); hit {
				return act, true
			}
		}
		return core.Action{}, false
	}
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return core.Action{}, false
	}
	k := km.String()
	switch {
	case core.MatchKey(k, core.Keys.Up):
		p.vp.ScrollUp(1)
		return core.Action{}, true
	case core.MatchKey(k, core.Keys.Down):
		p.vp.ScrollDown(1)
		return core.Action{}, true
	case core.MatchKey(k, core.Keys.Top):
		p.vp.GotoTop()
		return core.Action{}, true
	case core.MatchKey(k, core.Keys.Bottom):
		p.vp.GotoBottom()
		return core.Action{}, true
	case key.Matches(km, p.vp.KeyMap.PageUp, p.vp.KeyMap.PageDown):
		var cmd tea.Cmd
		p.vp, cmd = p.vp.Update(msg)
		return core.Async(cmd), true
	}
	return core.Action{}, false
}

// PanelHelp contributes only the scroll hint; g/G jumps are commands, which belong in the
// (?) menu (see core.ShortHelp).
func (p *ScrollContainer) PanelHelp() []key.Binding {
	return []key.Binding{
		core.Hint("scroll", core.Keys.Up, core.Keys.Down),
	}
}

// SetSize takes the outer dims; the viewport gets them minus borders and padding.
func (p *ScrollContainer) SetSize(width, height int) {
	p.width, p.height = width, height
	p.vp.SetWidth(p.innerWidth())
	p.vp.SetHeight(p.contentHeight())
}

// TextWidth is the width to wrap content to before SetLines: the viewport clips rather
// than wraps.
func (p *ScrollContainer) TextWidth() int { return p.innerWidth() }

// ScrollTo moves the content so line is the topmost visible row; the viewport
// clamps it to the content's extent.
func (p *ScrollContainer) ScrollTo(line int) { p.vp.SetYOffset(line) }

// ScrollOffset is the current top row — what a scroll-syncing host (or a test)
// reads back.
func (p *ScrollContainer) ScrollOffset() int { return p.vp.YOffset() }

// LineCount is the content's total rows.
func (p *ScrollContainer) LineCount() int { return p.vp.TotalLineCount() }

// MaxScrollOffset is the furthest ScrollTo can take the content.
func (p *ScrollContainer) MaxScrollOffset() int { return max(p.LineCount()-p.vp.Height(), 0) }

// VisibleRows is how many rows the pane shows at once — what a host centering content
// in it has to know.
func (p *ScrollContainer) VisibleRows() int { return p.vp.Height() }

// innerWidth is the text width inside the box (cell width minus side borders and
// the 1-col padding on each side).
func (p *ScrollContainer) innerWidth() int { return max(p.width-2-2, 10) }

// contentHeight is the viewport height inside the box (cell height minus the
// hand-drawn top border row and the bottom border row).
func (p *ScrollContainer) contentHeight() int { return max(p.height-2, 1) }

// View draws the content in a bordered box with the title (and, focused, a scroll hint)
// in its top edge, like LogPane.
func (p *ScrollContainer) View(focused bool) string {
	label := p.title
	if focused && !p.noKeyHints {
		// Only keys that act on this pane; pane navigation belongs to the help bar.
		label = p.title + " · " + core.Legend(
			core.Hint("scroll", core.Keys.Up, core.Keys.Down),
		)
	}
	return paddedFrame(label, p.innerWidth(), focused, p.vp.View())
}
