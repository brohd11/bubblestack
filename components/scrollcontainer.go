package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ScrollContainer is a read-only content panel in the LogPane visual idiom: a
// bordered box whose hand-drawn top edge carries a title legend, scrolling a
// viewport of caller-supplied lines. It is the ModularScreen building block for
// "show this text here" — a detail view, a log tail, a diff — without owning any
// navigation: esc deliberately falls through (handled=false) so the host screen
// keeps its pop, and the pane keys never reach a panel at all. It is purely
// presentational —
// no nav keys, no Push/Pop, no domain type; the caller owns the content via
// SetLines/SetStatus.
type ScrollContainer struct {
	vp         viewport.Model
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
	return &ScrollContainer{vp: viewport.New(0, 0), title: title}
}

// SetKeyHints turns the key legend the focused border carries on or off; on is the
// default, so a panel that says nothing keeps it. Off leaves the title alone on the
// edge, which is what a ListPanel's bordered legend has always shown (see its View) —
// so a layout that pairs the two reads as one set of elements rather than one pane
// shouting its keys next to a quiet one. The keys are unchanged either way: they are
// still in the host's help bar via PanelHelp, which is the bar's job.
func (p *ScrollContainer) SetKeyHints(show bool) { p.noKeyHints = !show }

func (p *ScrollContainer) Focus()        { p.focused = true }
func (p *ScrollContainer) Blur()         { p.focused = false }
func (p *ScrollContainer) Focused() bool { return p.focused }

// SetLines replaces the content. The first set opens at the top — a listing is
// read from the first entry down, matching git's own tag output — after which
// the position is the user's: later refreshes keep it instead of yanking the
// reader back up. SetStatus resets the pin, so content arriving after a
// "loading…" status opens afresh.
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

// UpdatePanel scrolls the viewport on the nav keys and the wheel, and nothing
// else: esc/back returns handled=false so the host ModularScreen keeps its pop
// fallback. The page keys match against the
// viewport's own keymap (they have no core.Keys binding). The wheel only scrolls
// while focused — mouse msgs are broadcast to every panel, and a wheel that
// rolled every pane at once would read as a bug.
func (p *ScrollContainer) UpdatePanel(_ *core.Shared, msg tea.Msg) (core.Action, bool) {
	if m, ok := msg.(tea.MouseMsg); ok {
		if !p.focused {
			return core.Action{}, false
		}
		if m.Action == tea.MouseActionPress &&
			(m.Button == tea.MouseButtonWheelUp || m.Button == tea.MouseButtonWheelDown) {
			var cmd tea.Cmd
			p.vp, cmd = p.vp.Update(m)
			return core.Async(cmd), true
		}
		return core.Action{}, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return core.Action{}, false
	}
	k := km.String()
	switch {
	case core.MatchKey(k, core.Keys.Up):
		p.vp.LineUp(1)
		return core.Action{}, true
	case core.MatchKey(k, core.Keys.Down):
		p.vp.LineDown(1)
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

// PanelHelp contributes the scroll hints shown in the host's help bar while this
// panel is focused.
func (p *ScrollContainer) PanelHelp() []key.Binding {
	return []key.Binding{
		core.Hint("scroll", core.Keys.Up, core.Keys.Down),
		core.Hint("top/bottom", core.Keys.Top, core.Keys.Bottom),
	}
}

// SetSize takes the OUTER cell dimensions (borders included) assigned by the
// host layout; the viewport gets the inner dims net of the side borders, the
// top/bottom border rows, and the 1-col padding on each side.
func (p *ScrollContainer) SetSize(width, height int) {
	p.width, p.height = width, height
	p.vp.Width = p.innerWidth()
	p.vp.Height = p.contentHeight()
}

// TextWidth is the width content must be wrapped to before SetLines: the viewport
// clips rather than wraps, so a caller folding prose (or a rendered markdown page)
// into this pane needs the box's inner measurement.
func (p *ScrollContainer) TextWidth() int { return p.innerWidth() }

// ScrollTo moves the content so line is the topmost visible row; the viewport
// clamps it to the content's extent.
func (p *ScrollContainer) ScrollTo(line int) { p.vp.SetYOffset(line) }

// ScrollOffset is the current top row — what a scroll-syncing host (or a test)
// reads back.
func (p *ScrollContainer) ScrollOffset() int { return p.vp.YOffset }

// LineCount is the content's total rows.
func (p *ScrollContainer) LineCount() int { return p.vp.TotalLineCount() }

// MaxScrollOffset is the furthest ScrollTo can take the content.
func (p *ScrollContainer) MaxScrollOffset() int { return max(p.LineCount()-p.vp.Height, 0) }

// VisibleRows is how many rows the pane shows at once — what a host centering content
// in it has to know.
func (p *ScrollContainer) VisibleRows() int { return p.vp.Height }

// innerWidth is the text width inside the box (cell width minus side borders and
// the 1-col padding on each side).
func (p *ScrollContainer) innerWidth() int {
	w := p.width - 2 - 2
	if w < 10 {
		w = 10
	}
	return w
}

// contentHeight is the viewport height inside the box (cell height minus the
// hand-drawn top border row and the bottom border row).
func (p *ScrollContainer) contentHeight() int {
	h := p.height - 2
	if h < 1 {
		h = 1
	}
	return h
}

// View draws the content inside a bordered box whose top edge is interrupted by
// the title legend (plus a scroll/pane hint while focused) — the LogPane shape,
// so a detail pane and the output pane read as the same kind of element.
func (p *ScrollContainer) View(focused bool) string {
	label := p.title
	if focused && !p.noKeyHints {
		// Built from the live bindings rather than spelled out: this legend once read
		// "⇧←→ panes" long after the pane keys became shift+tab alone, contradicting
		// the host's own help bar one row below it.
		label = p.title + " · " + core.Legend(
			core.Hint("scroll", core.Keys.Up, core.Keys.Down),
			core.PaneHint(),
		)
	}
	// The run between the corners is the same width as the bottom border: the
	// inner text plus the 1-col padding on each side. Composed from the frame
	// helpers rather than frame() because of that padding.
	inner := p.innerWidth() + 2
	content := frameBox(inner, focused).Padding(0, 1).Render(p.vp.View())
	return frameTop(label, inner, focused) + "\n" + content
}
