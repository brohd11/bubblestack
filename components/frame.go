package components

import (
	"image/color"
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

// The shared framed look: a NormalBorder whose top edge is hand-drawn to carry a title
// legend, muted or accented by focus. ScrollContainer, ListPanel and the bordered editor
// all use it.

// frameColor is the border tint: accent when focused, border color otherwise, read per
// call.
func frameColor(focused bool) color.Color {
	if focused {
		return core.FocusedColor
	}
	return core.BorderColor
}

// frameTop draws the top border with the legend: ┌─ legend ───┐. innerWidth is the run
// between the corners. An over-long legend pushes the corner out rather than truncating.
func frameTop(legend string, innerWidth int, focused bool) string {
	seg := ""
	if legend != "" {
		seg = "─ " + legend + " "
	}
	fill := max(innerWidth-lipgloss.Width(seg), 0)
	return lipgloss.NewStyle().Foreground(frameColor(focused)).
		Render("┌" + seg + strings.Repeat("─", fill) + "┐")
}

// frameBox is the style for the sides and bottom, sized so its edges meet frameTop's.
// Width includes any padding a caller adds.
func frameBox(innerWidth int, focused bool) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(frameColor(focused)).
		Width(innerWidth + 2)
}

// Frame renders legend, top edge and body in the box, unpadded.
func Frame(legend, body string, innerWidth int, focused bool) string {
	return frameTop(legend, innerWidth, focused) + "\n" + frameBox(innerWidth, focused).Render(body)
}

// paddedFrame is Frame with one column of padding on each side of body: the legend run
// between the corners spans the text plus that padding.
func paddedFrame(label string, textWidth int, focused bool, body string) string {
	inner := textWidth + 2
	return frameTop(label, inner, focused) + "\n" + frameBox(inner, focused).Padding(0, 1).Render(body)
}
