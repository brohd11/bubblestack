package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// LogPane is the default core.Output: a scrollable log in a bordered box below the body.
// New lines reveal it, o toggles it, and it scrolls while focused. Truncated mode gives
// each entry one clipped row; wrapped mode (w) folds entries with a bullet so long paths
// stay readable.
type LogPane struct {
	vp     viewport.Model
	logs   []string
	shown  bool
	wrap   bool
	width  int
	height int
}

var _ core.Wrapper = (*LogPane)(nil)

// NewLogPane builds the default log output pane.
func NewLogPane() *LogPane { return &LogPane{vp: viewport.New()} }

// Log appends a line and reveals the pane. This is the logging capability beyond
// core.Output that Shared.Log reaches by type assertion; the router never calls it.
func (p *LogPane) Log(line string, forceShow bool) {
	p.logs = append(p.logs, line)
	if forceShow {
		p.shown = forceShow
	}
}

func (p *LogPane) Shown() bool { return p.shown }
func (p *LogPane) Toggle()     { p.shown = !p.shown }
func (p *LogPane) Hide()       { p.shown = false }

// ToggleWrap switches render mode immediately, landing at the bottom.
func (p *LogPane) ToggleWrap() {
	p.wrap = !p.wrap
	p.vp.SetContent(p.content())
	p.vp.GotoBottom()
}

func (p *LogPane) Wrapped() bool { return p.wrap }

func (p *LogPane) Clear() {
	p.logs = nil
	p.shown = false
	p.vp.SetContent("")
}

// SetSize lays out a full-width pane at about a quarter of the terminal height. The
// router re-sets the content each cycle.
func (p *LogPane) SetSize(termWidth, termHeight int) {
	p.width, p.height = termWidth, termHeight
	p.vp.SetWidth(p.innerWidth())
	p.vp.SetHeight(p.contentHeight())
	p.vp.SetContent(p.content())
}

// Height is the rows the pane occupies when shown (content + top/bottom border).
func (p *LogPane) Height() int {
	if !p.shown {
		return 0
	}
	return p.contentHeight() + 2
}

func (p *LogPane) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return cmd
}

func (p *LogPane) GotoBottom() { p.vp.GotoBottom() }
func (p *LogPane) GotoTop()    { p.vp.GotoTop() }

// innerWidth is the text width inside the box (full width minus header-margin parity,
// side borders, and the 1-col padding on each side).
func (p *LogPane) innerWidth() int { return max(p.width-2-2-2, 10) }

func (p *LogPane) contentHeight() int { return max(p.height/4, 3) }

// In wrapped mode an entry's first row carries the bullet and its continuations are
// indented under it, so entry boundaries survive folding.
const (
	logBullet = "- "
	logIndent = "  "
	// logBreaks are extra wrap points beyond whitespace, so a path folds at a
	// separator rather than mid-name ("-" is always a breakpoint in ansi.Wrap).
	logBreaks = "/_"
)

func (p *LogPane) content() string {
	style := core.LogStyle()
	var b strings.Builder
	for i, l := range p.logs {
		if i > 0 {
			b.WriteByte('\n')
		}
		if p.wrap {
			l = p.wrapEntry(l)
		}
		b.WriteString(style.Render(l))
	}
	return b.String()
}

// wrapEntry folds one entry to the pane width, splitting unbroken tokens (paths) too.
func (p *LogPane) wrapEntry(line string) string {
	w := max(p.innerWidth()-lipgloss.Width(logBullet), 1)
	return hangRows(ansi.Wrap(line, w, logBreaks), logBullet, logIndent)
}

// View draws the log in a box with an "Output" legend (plus a scroll hint while focused);
// wrapped mode shows in the legend either way.
func (p *LogPane) View(focused bool) string {
	label := "Output"
	if p.wrap {
		label = "Output [wrap]"
	}
	if focused {
		label += " · " + core.Legend(
			core.Hint("scroll", core.Keys.Up, core.Keys.Down),
			core.Hint("back", core.Keys.ToggleOutput, core.Keys.Back),
			core.Hint("hide", core.Keys.Output),
			core.Hint("wrap", core.Keys.Wrap),
		)
	}

	return paddedFrame(label, p.innerWidth(), focused, p.vp.View())
}
