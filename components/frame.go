package components

import (
	"image/color"
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

// The framework's one framed-element look, shared by every element that draws a
// box: a NormalBorder whose top edge is hand-drawn so a title legend can sit
// mid-line, tinted muted or accent by focus. ScrollContainer wore it first (the
// LogPane idiom); the bordered editor.Screen and ListPanel reuse it verbatim, so a
// detail pane, a sidebar list and an editor pane all read as the same kind of
// element.

// frameColor is the border tint: the theme accent while focused, the muted border
// color otherwise. Read from the live palette per call (not cached) so a SetTheme
// switch repaints the next render.
func frameColor(focused bool) color.Color {
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
//
// The +2 is the two border cells: lipgloss v2's Style.Width is the whole rendered
// width where v1's excluded the border. innerWidth stays "the run between the
// corners", which is what frameTop draws to, so the two edges still line up.
func frameBox(innerWidth int, focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(frameColor(focused)).
		Width(innerWidth + 2)
}

// Frame is the whole element: the legend top edge over body in the box. The
// unpadded common case — a caller needing padding composes frameTop and frameBox
// itself (ScrollContainer does).
func Frame(legend, body string, innerWidth int, focused bool) string {
	return frameTop(legend, innerWidth, focused) + "\n" + frameBox(innerWidth, focused).Render(body)
}
