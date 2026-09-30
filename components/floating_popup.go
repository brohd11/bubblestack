package components

import (
	"image/color"
	"strings"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"
)

// PopupPlacement places a popup within its parent's frame; FloatingPopup clamps the
// result.
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

// FloatingPopup is a parent-owned overlay that never enters the router stack or takes
// focus. The parent offers messages to Update first and handles those it declines.
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

// PopupListOpts configures a selectable popup list. With a query, Fuzzy matches against
// FilterText and selects the best match on each change; Filter runs first. Empty queries
// keep source order.
type PopupListOpts[T any] struct {
	Items      []PopupListItem[T]
	MaxVisible int
	MaxWidth   int
	Filter     func(query string, item PopupListItem[T]) bool
	Fuzzy      bool
	OnAccept   func(*core.Shared, PopupListItem[T]) core.Action
	OnCancel   func(*core.Shared) core.Action
}

// PopupList is the selection state inside a FloatingPopup. It handles only up/down,
// tab/enter and esc; everything else returns handled=false.
type PopupList[T any] struct {
	items      []PopupListItem[T]
	visible    []int
	query      string
	sel, top   int
	maxVisible int
	maxWidth   int
	filter     func(string, PopupListItem[T]) bool
	fuzzy      bool
	onAccept   func(*core.Shared, PopupListItem[T]) core.Action
	onCancel   func(*core.Shared) core.Action
}

func NewPopupList[T any](opts PopupListOpts[T]) *PopupList[T] {
	p := &PopupList[T]{maxVisible: opts.MaxVisible, maxWidth: opts.MaxWidth,
		filter: opts.Filter, fuzzy: opts.Fuzzy, onAccept: opts.OnAccept, onCancel: opts.OnCancel}
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
	if p.fuzzy {
		// The query changed, so accept should follow the new best match.
		p.sel = -1
	}
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
	candidates := make([]int, 0, len(p.items))
	for i, item := range p.items {
		if p.filter == nil || p.query == "" || p.filter(p.query, item) {
			candidates = append(candidates, i)
		}
	}
	p.visible = p.visible[:0]
	if p.fuzzy && p.query != "" {
		matches := fuzzy.FindFrom(p.query, popupListFuzzySource[T]{items: p.items, indexes: candidates})
		for _, match := range matches {
			p.visible = append(p.visible, candidates[match.Index])
		}
	} else {
		p.visible = append(p.visible, candidates...)
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

// popupListFuzzySource lets the matcher rank a filtered subset while preserving each
// result's source index in PopupList.items. fuzzy.FindFrom is stable for equal scores.
type popupListFuzzySource[T any] struct {
	items   []PopupListItem[T]
	indexes []int
}

func (s popupListFuzzySource[T]) String(i int) string { return s.items[s.indexes[i]].FilterText }
func (s popupListFuzzySource[T]) Len() int            { return len(s.indexes) }

func (p *PopupList[T]) clampWindow() {
	if p.sel < 0 {
		p.top = 0
		return
	}
	p.top = clampTop(p.top, p.sel, p.visibleRows(), len(p.visible))
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

	gutter := rows < len(p.visible)
	field := contentW
	if gutter {
		field -= menuGutterW
	}
	out := make([]string, 0, rows)
	for row := 0; row < rows; row++ {
		idx := p.top + row
		item := p.items[p.visible[idx]]
		labelStyle, detailStyle := lipgloss.NewStyle(), core.MutedStyle()
		if idx == p.sel {
			labelStyle, detailStyle = core.AccentStyle(), core.AccentStyle()
		}
		line := menuRow(item.Label, item.Detail, field, labelStyle, detailStyle)
		if gutter {
			line += scrollGutter(row, rows, p.top, len(p.visible))
		}
		out = append(out, line)
	}
	return PopupPanel(strings.Join(out, "\n"), contentW)
}

// PopupPanel renders body in the slim bordered box every floating panel uses. width is
// the inner width. The fixed width makes the box opaque: core.Composite punches a hole
// only as wide as each line, so ragged lines would let the background through.
func PopupPanel(body string, width int) string {
	return popupPanel(body, width, core.FocusedColor)
}

// popupPanel is PopupPanel with the border in border (a menu's MenuStyle.Focus).
func popupPanel(body string, width int, border color.Color) string {
	return menuBox().BorderForeground(border).Width(width + menuChromeW).Render(body)
}
