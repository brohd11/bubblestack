package core

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Composite splices fg onto bg at cell (x, y), line by line: each bg line keeps its cells
// before x and after x+width(fg line), so the box punches a hole exactly its own width.
// Measurement is cell-aware, resets bracket the fg segment so colors do not bleed, and fg
// lines beyond bg are dropped.
func Composite(bg, fg string, x, y int) string {
	if fg == "" {
		return bg
	}
	x = max(x, 0)
	bgLines := strings.Split(bg, "\n")
	for i, fgLine := range strings.Split(fg, "\n") {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]

		// Left slice: the first x cells of the bg line, padded with spaces when the
		// bg line is shorter than x so the box still lands at column x.
		left := ansi.Truncate(bgLine, x, "")
		if gap := x - ansi.StringWidth(left); gap > 0 {
			left += strings.Repeat(" ", gap)
		}

		// Right slice: bg cells from x+width(fg) onward (everything the box doesn't cover).
		right := ansi.TruncateLeft(bgLine, x+ansi.StringWidth(fgLine), "")

		bgLines[row] = left + "\x1b[0m" + fgLine + "\x1b[0m" + right
	}
	return strings.Join(bgLines, "\n")
}

// popupBox is the bordered popup box, a rounded border in the theme accent. It is built
// per call from the current FocusedColor rather than cached by rebuildStyles.
func popupBox(width int) lipgloss.Style {
	s := lipgloss.NewStyle().
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(FocusedColor)
	if width > 0 {
		s = s.Width(width)
	}
	return s
}

// PopupBox renders body in a themed bordered popup with an optional accent title. width
// is the inner width (0 sizes to content). It is the overlay DialogScreen's renderer.
func PopupBox(title, body string, width int) string {
	content := body
	if title != "" {
		head := lipgloss.NewStyle().Bold(true).Foreground(FocusedColor).Render(title)
		content = lipgloss.JoinVertical(lipgloss.Left, head, "", body)
	}
	return popupBox(width).Render(content)
}
