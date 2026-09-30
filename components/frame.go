package components

import (
	"image/color"
	"strings"

	"github.com/brohd11/bubblestack/core"
	"github.com/charmbracelet/x/ansi"

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

// FrameFocus is how a frame shows that its pane holds focus.
type FrameFocus uint8

const (
	FocusEdges  FrameFocus = iota // the edges and legend take the accent
	FocusLegend                   // the edges stay the border color; only the legend reacts
)

// edgeColor is the tint of a frame's lines under focus mode f.
func (f FrameFocus) edgeColor(focused bool) color.Color {
	if f == FocusLegend {
		return core.BorderColor
	}
	return frameColor(focused)
}

// legendStyle styles the legend text: with the edges under FocusEdges, otherwise muted
// text that turns the bold accent (the current breadcrumb's look) while focused.
func (f FrameFocus) legendStyle(focused bool) lipgloss.Style {
	if f != FocusLegend {
		return lipgloss.NewStyle().Foreground(frameColor(focused))
	}
	if focused {
		return core.AccentStyle()
	}
	return core.MutedStyle()
}

// frameTop draws the top border with the legend: ┌─ legend ───┐. innerWidth is the run
// between the corners. An over-long legend pushes the corner out rather than truncating.
func frameTop(legend string, innerWidth int, edge color.Color, legendStyle lipgloss.Style) string {
	line := lipgloss.NewStyle().Foreground(edge)
	if legend == "" {
		return line.Render("┌" + strings.Repeat("─", innerWidth) + "┐")
	}
	fill := max(innerWidth-lipgloss.Width("─ "+legend+" "), 0)
	return line.Render("┌─ ") + legendStyle.Render(legend) + line.Render(" "+strings.Repeat("─", fill)+"┐")
}

// frameBox is the style for the sides and bottom, sized so its edges meet frameTop's.
// Width includes any padding a caller adds.
func frameBox(innerWidth int, edge color.Color) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderTop(false).
		BorderForeground(edge).
		Width(innerWidth + 2)
}

// Frame renders legend, top edge and body in the box, unpadded, the whole frame tinted
// by focus (BoxFrame's FocusEdges).
func Frame(legend, body string, innerWidth int, focused bool) string {
	return BoxFrame{}.Render(legend, body, innerWidth, focused)
}

// paddedFrame is Frame with one column of padding on each side of body: the legend run
// between the corners spans the text plus that padding.
func paddedFrame(label string, textWidth int, focused bool, body string) string {
	inner := textWidth + 2
	edge := frameColor(focused)
	return frameTop(label, inner, edge, FocusEdges.legendStyle(focused)) + "\n" +
		frameBox(inner, edge).Padding(0, 1).Render(body)
}

// Insets is the cells a frame takes from each side of its panel's allocation.
type Insets struct{ Top, Right, Bottom, Left int }

// FrameStyle is a panel's frame as one value: its look and the geometry that look costs,
// so a panel sizes and hit-tests its body from Insets rather than assuming a box. It is
// the instancer's choice, like ListPanelOpts.Border (see ListPanelOpts.Frame).
type FrameStyle interface {
	Insets() Insets
	// Render frames body, whose lines are innerWidth cells wide.
	Render(legend, body string, innerWidth int, focused bool) string
}

// BoxFrame is the shared box with the legend in its top edge: ┌─ legend ─┐.
type BoxFrame struct {
	Focus FrameFocus
}

func (BoxFrame) Insets() Insets { return Insets{Top: 1, Right: 1, Bottom: 1, Left: 1} }

func (f BoxFrame) Render(legend, body string, innerWidth int, focused bool) string {
	edge := f.Focus.edgeColor(focused)
	return frameTop(legend, innerWidth, edge, f.Focus.legendStyle(focused)) + "\n" +
		frameBox(innerWidth, edge).Render(body)
}

// TopEdge is what a TitledFrame draws above its title row.
type TopEdge uint8

const (
	TopNone TopEdge = iota // nothing: a rule or pane above supplies the edge
	TopBox                 // ┌──┐
	TopTee                 // ├──┤: continues the sides of a box above
)

// SideFrame is sides without a legend: an optional top edge (TopBox is a plain ┌──┐ a
// host may redraw, joining it to its neighbors) and an optional └──┘ bottom. It never
// reacts to focus — the pane shows that in its content, or its host elsewhere.
type SideFrame struct {
	Top    TopEdge
	Bottom bool
}

func (f SideFrame) Insets() Insets {
	in := Insets{Right: 1, Left: 1}
	if f.Top != TopNone {
		in.Top = 1
	}
	if f.Bottom {
		in.Bottom = 1
	}
	return in
}

func (f SideFrame) Render(_, body string, innerWidth int, _ bool) string {
	edge := lipgloss.NewStyle().Foreground(core.BorderColor)
	sides := frameBox(innerWidth, core.BorderColor).BorderBottom(f.Bottom).Render(body)
	switch f.Top {
	case TopBox:
		return edge.Render("┌"+strings.Repeat("─", innerWidth)+"┐") + "\n" + sides
	case TopTee:
		return edge.Render("├"+strings.Repeat("─", innerWidth)+"┤") + "\n" + sides
	}
	return sides
}

// TitledFrame gives the legend its own row, ruled off from the body:
//
//	│ legend │
//	├────────┤
//	│ body   │
//	└────────┘
//
// Top and Bottom choose the outer edges, so panes stacked in a column share them. Focus
// chooses what lights up while focused.
type TitledFrame struct {
	Top    TopEdge
	Bottom bool
	Focus  FrameFocus
}

func (f TitledFrame) Insets() Insets {
	in := Insets{Top: 2, Right: 1, Left: 1}
	if f.Top != TopNone {
		in.Top++
	}
	if f.Bottom {
		in.Bottom = 1
	}
	return in
}

func (f TitledFrame) Render(legend, body string, innerWidth int, focused bool) string {
	tint := f.Focus.edgeColor(focused)
	edge := lipgloss.NewStyle().Foreground(tint)
	rule := func(l, r string) string { return edge.Render(l + strings.Repeat("─", innerWidth) + r) }
	var rows []string
	switch f.Top {
	case TopBox:
		rows = append(rows, rule("┌", "┐"))
	case TopTee:
		rows = append(rows, rule("├", "┤"))
	}
	// The legend row is clipped and padded to the run, like a body row: clipped before
	// styling, so the style never changes its width.
	text := ansi.Truncate(legend, max(innerWidth-1, 0), "…")
	pad := max(innerWidth-1-lipgloss.Width(text), 0)
	title := " " + f.Focus.legendStyle(focused).Render(text) + strings.Repeat(" ", pad)
	if innerWidth < 1 {
		title = ""
	}
	side := edge.Render("│")
	rows = append(rows, side+title+side, rule("├", "┤"))
	sides := frameBox(innerWidth, tint).BorderBottom(f.Bottom)
	return strings.Join(rows, "\n") + "\n" + sides.Render(body)
}
