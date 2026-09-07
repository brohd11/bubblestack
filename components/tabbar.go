package components

import (
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/brohd11/bubblestack/core"
	"github.com/charmbracelet/x/ansi"
)

// TabItem identifies one tab. IDs must be unique and nonempty. Marker is a
// compact suffix whose space is reserved before Label is truncated.
type TabItem struct {
	ID, Label, Marker string
}

// TabBar is a single-row, horizontally scrolling panel. It deliberately does
// not take keyboard focus: the host owns shortcuts and activates Click's ID.
// Labels and markers are plain text; styles read the current framework theme.
type TabBar struct {
	items                []TabItem
	active               string
	width, height, first int
}

func NewTabBar() *TabBar { return &TabBar{} }

func (p *TabBar) SetItems(items []TabItem) {
	if slices.Equal(p.items, items) {
		return
	}
	p.items = slices.Clone(items)
	p.first = min(p.first, max(0, len(items)-1))
	p.revealActive()
}

func (p *TabBar) SetActive(id string) {
	if p.active == id {
		return
	}
	p.active = id
	p.revealActive()
}

func (p *TabBar) SetSize(width, height int) {
	changed := width != p.width || height != p.height
	p.width, p.height = max(0, width), max(0, height)
	if changed {
		p.revealActive()
	}
}

type tabCell struct {
	index, x, width int
}

// cells is shared by rendering and hit testing. With overflow, the outer cells
// are reserved for arrows; at widths below three the selected label wins.
func (p *TabBar) cells() (cells []tabCell, left, right bool) {
	if p.width == 0 || p.height == 0 || len(p.items) == 0 {
		return
	}
	total := 0
	for _, item := range p.items {
		total += ansi.StringWidth(item.Label+item.Marker) + 2
	}
	first, x, available := p.first, 0, p.width
	if total <= p.width {
		first = 0
	}
	if total > p.width && p.width >= 3 {
		x, available = 1, p.width-2
		left = first > 0
	}
	for i := first; i < len(p.items) && available > 0; i++ {
		w := ansi.StringWidth(p.items[i].Label+p.items[i].Marker) + 2
		if w > available && len(cells) > 0 {
			break
		}
		w = min(w, available)
		cells = append(cells, tabCell{i, x, w})
		x, available = x+w, available-w
	}
	if len(cells) > 0 {
		right = cells[len(cells)-1].index < len(p.items)-1 && p.width >= 3
	}
	return
}

func (p *TabBar) revealActive() {
	index := slices.IndexFunc(p.items, func(item TabItem) bool { return item.ID == p.active })
	if index < 0 || p.width == 0 || p.height == 0 {
		return
	}
	if index < p.first {
		p.first = index
	}
	for p.first < index {
		cells, _, _ := p.cells()
		if len(cells) > 0 && cells[len(cells)-1].index >= index {
			return
		}
		p.first++
	}
}

func tabText(item TabItem, width int) string {
	padding := 0
	if width >= 3 {
		padding = 1
	}
	inner := width - 2*padding
	marker := ansi.Truncate(item.Marker, inner, "")
	label := ansi.Truncate(item.Label, max(0, inner-ansi.StringWidth(marker)), "…")
	text := label + marker
	return strings.Repeat(" ", padding) + text + strings.Repeat(" ", max(0, width-padding-ansi.StringWidth(text)))
}

func (p *TabBar) View(bool) string {
	if p.width == 0 || p.height == 0 {
		return ""
	}
	cells, left, right := p.cells()
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	active := lipgloss.NewStyle().Foreground(core.OnFocusedColor).Background(core.FocusedColor).Bold(true)
	var row strings.Builder
	x := 0
	if len(cells) > 0 && cells[0].x > 0 {
		arrow := " "
		if left {
			arrow = "‹"
		}
		row.WriteString(muted.Render(arrow))
		x = 1
	}
	for _, cell := range cells {
		style := muted
		if p.items[cell.index].ID == p.active {
			style = active
		}
		row.WriteString(style.Render(tabText(p.items[cell.index], cell.width)))
		x += cell.width
	}
	if right {
		row.WriteString(strings.Repeat(" ", max(0, p.width-x-1)))
		row.WriteString(muted.Render("›"))
		x = p.width
	}
	row.WriteString(strings.Repeat(" ", max(0, p.width-x)))
	return row.String()
}

// Click accepts local cell coordinates. It returns a tab ID to activate, or an
// empty ID for an arrow/dead-space click. Arrow clicks scroll without selecting.
func (p *TabBar) Click(x, y int) (id string, handled bool) {
	if x < 0 || x >= p.width || y != 0 || p.height == 0 {
		return "", false
	}
	cells, left, right := p.cells()
	if left && x == 0 {
		p.first--
		return "", true
	}
	if right && x == p.width-1 {
		p.first++
		return "", true
	}
	for _, cell := range cells {
		if x >= cell.x && x < cell.x+cell.width {
			return p.items[cell.index].ID, true
		}
	}
	return "", true
}
