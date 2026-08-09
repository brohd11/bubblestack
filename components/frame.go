package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/lipgloss"
)

// The framework's one framed-element look, shared by every element that draws a
// box: a NormalBorder whose top edge is hand-drawn so a title legend can sit
// mid-line, tinted muted or accent by focus. ScrollContainer wore it first (the
// LogPane idiom); the bordered EditorScreen and ListPanel reuse it verbatim, so a
// detail pane, a sidebar list and an editor pane all read as the same kind of
// element.

// frameColor is the border tint: the theme accent while focused, the muted border
// color otherwise. Read from the live palette per call (not cached) so a SetTheme
// switch repaints the next render.
func frameColor(focused bool) lipgloss.TerminalColor {
	if focused {
		return core.FocusedColor
	}
	return core.BorderColor
}

// frameTop hand-draws the top border row with the legend interrupting it:
// ┌─ legend ────────┐. innerWidth is the run between the corners — the same width
// the box below renders at, so the two edges line up. An empty legend gives a
// plain rule. A legend too long for the run simply pushes the corner out (fill
// clamps at 0) rather than truncating, matching what ScrollContainer has always
// done.
func frameTop(legend string, innerWidth int, focused bool) string {
	seg := ""
	if legend != "" {
		seg = "─ " + legend + " "
	}
	fill := innerWidth - lipgloss.Width(seg)
	if fill < 0 {
		fill = 0
	}
	return lipgloss.NewStyle().Foreground(frameColor(focused)).
		Render("┌" + seg + strings.Repeat("─", fill) + "┐")
}

// frameBox is the style for the rest of the box: sides and bottom of a
// NormalBorder (the top is frameTop's job), tinted to match, sized to innerWidth.
// Returned as a Style so a caller can add padding — Width counts the padding, as
// lipgloss does, so a padded box passes innerWidth including it.
func frameBox(innerWidth int, focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(frameColor(focused)).
		Width(innerWidth)
}

// frame is the whole element: the legend top edge over body in the box. The
// unpadded common case — a caller needing padding composes frameTop and frameBox
// itself (ScrollContainer does).
func frame(legend, body string, innerWidth int, focused bool) string {
	return frameTop(legend, innerWidth, focused) + "\n" + frameBox(innerWidth, focused).Render(body)
}
