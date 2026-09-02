package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// PopupPlacement places a floating popup within its parent's rendered frame. All
// coordinates are relative to that frame; FloatingPopup clamps the result before
// compositing it.
type PopupPlacement func(frameW, frameH, popupW, popupH int) (x, y int)

// PopupAnchor describes a preferred popup corner and the edges it should flip away
// from when it cannot fit. It is the parent-owned counterpart of MenuAnchor.
type PopupAnchor struct{ X, Y, FlipX, FlipY int }

// PlacePopupAt returns anchored placement suitable for a caret, row, or button. A
// zero flip edge means the corresponding preferred coordinate.
func PlacePopupAt(anchor PopupAnchor) PopupPlacement {
	if anchor.FlipX == 0 {
		anchor.FlipX = anchor.X
	}
	if anchor.FlipY == 0 {
		anchor.FlipY = anchor.Y
	}
	return func(frameW, frameH, popupW, popupH int) (x, y int) {
		x, y = anchor.X, anchor.Y
		if x+popupW > frameW {
			x = anchor.FlipX - popupW
		}
		if y+popupH > frameH {
			y = anchor.FlipY - popupH
		}
		return x, y
	}
}

// PlacePopupTopRight positions a popup inset by margin cells from the frame's top
// and right edges. It is useful for passive notices whose handler claims no input.
func PlacePopupTopRight(margin int) PopupPlacement {
	margin = max(margin, 0)
	return func(frameW, _ int, popupW, _ int) (int, int) {
		return frameW - popupW - margin, margin
	}
}

// FloatingPopup is a parent-owned visual overlay. Unlike an overlay Screen it never
// enters the router stack and therefore never takes focus. The parent offers messages
// to Update first and forwards every message for which handled is false.
type FloatingPopup struct {
	Content   func() string
	Placement PopupPlacement
	Handle    func(*core.Shared, tea.Msg) (core.Action, bool)
}

// Update offers msg to the optional handler. With no handler, a floating popup is a
// completely passive notice and every input remains the parent's.
func (p *FloatingPopup) Update(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if p == nil || p.Handle == nil {
		return core.Action{}, false
	}
	return p.Handle(sh, msg)
}

// View returns the popup content without compositing it.
func (p *FloatingPopup) View() string {
	if p == nil || p.Content == nil {
		return ""
	}
	return p.Content()
}

// ViewOver composites the popup over background inside a frameW by frameH parent.
func (p *FloatingPopup) ViewOver(background string, frameW, frameH int) string {
	box := p.View()
	if box == "" {
		return background
	}
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x, y := 0, 0
	if p.Placement != nil {
		x, y = p.Placement(frameW, frameH, bw, bh)
	}
	x = max(0, min(x, max(frameW-bw, 0)))
	y = max(0, min(y, max(frameH-bh, 0)))
	return core.Composite(background, box, x, y)
}

// PopupListItem is one row in a PopupList. FilterText defaults to Label when empty.
// Value is kept opaque by the component and returned intact to OnAccept.
type PopupListItem[T any] struct {
	Label, Detail, FilterText string
	Value                     T
}

// PopupListOpts configures an input-transparent selectable popup list.
type PopupListOpts[T any] struct {
	Items      []PopupListItem[T]
	MaxVisible int
	MaxWidth   int
	Filter     func(query string, item PopupListItem[T]) bool
	OnAccept   func(*core.Shared, PopupListItem[T]) core.Action
	OnCancel   func(*core.Shared) core.Action
}

// PopupList is the reusable selection/filter state used inside a FloatingPopup.
// It claims only arrow-up/down, tab/enter, and escape; all other messages return
// handled=false so the owning parent can continue processing them.
type PopupList[T any] struct {
	items      []PopupListItem[T]
	visible    []int
	query      string
	sel, top   int
	maxVisible int
	maxWidth   int
	filter     func(string, PopupListItem[T]) bool
	onAccept   func(*core.Shared, PopupListItem[T]) core.Action
	onCancel   func(*core.Shared) core.Action
}

func NewPopupList[T any](opts PopupListOpts[T]) *PopupList[T] {
	p := &PopupList[T]{maxVisible: opts.MaxVisible, maxWidth: opts.MaxWidth,
		filter: opts.Filter, onAccept: opts.OnAccept, onCancel: opts.OnCancel}
	p.SetItems(opts.Items)
	return p
}

func (p *PopupList[T]) SetItems(items []PopupListItem[T]) {
	p.items = append(p.items[:0], items...)
	for i := range p.items {
		if p.items[i].FilterText == "" {
			p.items[i].FilterText = p.items[i].Label
		}
	}
	p.rebuild()
}

func (p *PopupList[T]) SetQuery(query string) {
	if p.query == query {
		return
	}
	p.query = query
	p.rebuild()
}

func (p *PopupList[T]) Query() string { return p.query }

func (p *PopupList[T]) Len() int { return len(p.visible) }

// Select moves the cursor to a source item index when that item survives the current
// filter. It is primarily useful for a provider's preselected result.
func (p *PopupList[T]) Select(sourceIndex int) {
	for i, source := range p.visible {
		if source == sourceIndex {
			p.sel = i
			p.clampWindow()
			return
		}
	}
}

// SetMaxWidth changes the content-width cap used by the next render.
func (p *PopupList[T]) SetMaxWidth(width int) { p.maxWidth = width }

func (p *PopupList[T]) rebuild() {
	selectedSource := -1
	if p.sel >= 0 && p.sel < len(p.visible) {
		selectedSource = p.visible[p.sel]
	}
	p.visible = p.visible[:0]
	for i, item := range p.items {
		if p.filter == nil || p.query == "" || p.filter(p.query, item) {
			p.visible = append(p.visible, i)
		}
	}
	p.sel = -1
	for i, source := range p.visible {
		if source == selectedSource {
			p.sel = i
			break
		}
	}
	if p.sel < 0 && len(p.visible) > 0 {
		p.sel = 0
	}
	p.clampWindow()
}

func (p *PopupList[T]) clampWindow() {
	n := p.visibleRows()
	if p.sel < 0 {
		p.top = 0
		return
	}
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+n {
		p.top = p.sel - n + 1
	}
	p.top = max(0, min(p.top, max(len(p.visible)-n, 0)))
}

func (p *PopupList[T]) visibleRows() int {
	n := len(p.visible)
	if p.maxVisible > 0 {
		n = min(n, p.maxVisible)
	}
	return n
}

func (p *PopupList[T]) Update(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	km, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return core.Action{}, false
	}
	switch km.String() {
	case "up":
		if len(p.visible) > 0 {
			p.sel = (p.sel - 1 + len(p.visible)) % len(p.visible)
			p.clampWindow()
		}
		return core.Action{}, true
	case "down":
		if len(p.visible) > 0 {
			p.sel = (p.sel + 1) % len(p.visible)
			p.clampWindow()
		}
		return core.Action{}, true
	case "tab", "enter":
		if p.sel >= 0 && p.sel < len(p.visible) && p.onAccept != nil {
			return p.onAccept(sh, p.items[p.visible[p.sel]]), true
		}
		return core.Action{}, true
	case "esc":
		if p.onCancel != nil {
			return p.onCancel(sh), true
		}
		return core.Action{}, true
	default:
		return core.Action{}, false
	}
}

func (p *PopupList[T]) View() string {
	if len(p.visible) == 0 {
		return ""
	}
	rows := p.visibleRows()
	contentW := 1
	for _, source := range p.visible {
		item := p.items[source]
		w := ansi.StringWidth(item.Label)
		if item.Detail != "" {
			w += menuHintGap + ansi.StringWidth(item.Detail)
		}
		contentW = max(contentW, w)
	}
	if rows < len(p.visible) {
		contentW += menuGutterW
	}
	if p.maxWidth > 0 {
		contentW = min(contentW, p.maxWidth)
	}

	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	accent := lipgloss.NewStyle().Foreground(core.FocusedColor).Bold(true)
	out := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		idx := p.top + row
		item := p.items[p.visible[idx]]
		field := contentW
		if rows < len(p.visible) {
			field -= menuGutterW
		}
		labelStyle, detailStyle := lipgloss.NewStyle(), muted
		if idx == p.sel {
			labelStyle, detailStyle = accent, accent
		}
		detailW := ansi.StringWidth(item.Detail)
		reserve := 0
		if item.Detail != "" && field-menuHintGap-detailW >= 1 {
			reserve = menuHintGap + detailW
		} else {
			detailW = 0
		}
		label := ansi.Truncate(item.Label, max(field-reserve, 1), "…")
		line := labelStyle.Render(label) + strings.Repeat(" ", max(field-ansi.StringWidth(label)-detailW, 0))
		if detailW > 0 {
			line += detailStyle.Render(item.Detail)
		}
		if rows < len(p.visible) {
			mark := " "
			if row == 0 && p.top > 0 {
				mark = "↑"
			} else if row == rows-1 && p.top+rows < len(p.visible) {
				mark = "↓"
			}
			line += muted.Render(strings.Repeat(" ", menuGutterW-1) + mark)
		}
		out = append(out, line)
	}
	return menuBox().Width(contentW + menuChromeW).Render(strings.Join(out, "\n"))
}
