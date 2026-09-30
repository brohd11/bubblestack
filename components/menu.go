package components

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// MenuScreen is a floating dropdown or context menu: a small bordered box of rows
// composited over the screen below at a caller-supplied anchor, next to the thing it acts
// on (where a PickerScreen would replace the view).
//
// It is modal because only the top screen gets Update; Filtering always reports true so
// the router's single-key shortcuts cannot fire underneath, and QuitGate closes the menu
// on Keys.ForceQuit. Submenus are a Pick that pushes another menu at ChildAnchor. The callback
// owns dismissal: a Pick that wants the menu gone returns core.Pop(). Esc closes one
// level; set a child's OnCancel to core.Pop(n) to close a cascade. It adds no breadcrumb
// segment, since a dropdown is not a place.
//
// Rows are hand-rolled rather than a bubbles list so every rendered row maps to an item
// for hit-testing, and place() agrees with View() to the cell (menu_test.go pins both).
//
// Limitations:
//  1. It only sees the body rect, so the box never covers chrome; that also makes place()
//     exactly where the router draws it.
//  2. A parent menu keeps its old size if the terminal is resized while a submenu is open.
//  3. A click on the parent while a submenu is up only closes the submenu.
//  4. Tab-strip and breadcrumb clicks unwind the stack under a live menu; header clicks
//     run HeaderPane.OnClick; output-pane clicks take focus from it.
//  5. Right-click is claimed per call site (editor.Opts.ContextMenu); nothing arbitrates.
//  6. MenuItem.Hint is display-only: no accelerators or type-ahead are dispatched.
type MenuScreen struct {
	items    []MenuItem
	anchor   MenuAnchor
	title    string
	maxWidth int
	style    MenuStyle

	// parent is the menu whose Submenu row opened this one, at parentRow; nil for a root.
	// A child reads it to close when the pointer moves back over the parent.
	parent    *MenuScreen
	parentRow int
	// hoverRow is the row the pointer last moved over, -1 when it is off the rows. A
	// Submenu row opens when the pointer enters it — not on every twitch over it, so esc
	// closes a submenu until the pointer leaves the row and comes back.
	hoverRow int
	// onOutside is MenuOpts.OnPointerOutside; a child reads its root's.
	onOutside func(*core.Shared, int, int) (core.Action, bool)
	// onKeyOutside is MenuOpts.OnKeyOutside; a child reads its root's.
	onKeyOutside func(*core.Shared, string) (core.Action, bool)

	sel int // index into items; -1 when nothing is selectable
	top int // first item of the visible window

	termW int // terminal width, from SetSize
	bodyH int // body height, from SetSize
	bodyY int // absolute row the body starts at, from Shared.BodyY()

	// OnSelect, when set, handles every selection instead of the item's Pick.
	OnSelect func(*core.Shared, MenuItem, int) core.Action
	// OnCancel runs on esc/left and on a dismissing click; nil ⇒ a plain core.Pop.
	OnCancel func(*core.Shared) core.Action
}

// Items returns the menu's rows as built, separators included. Do not mutate it.
func (s *MenuScreen) Items() []MenuItem { return s.items }

var _ core.Overlayer = (*MenuScreen)(nil)
var _ core.OverlayPositioner = (*MenuScreen)(nil)
var _ core.Filterer = (*MenuScreen)(nil)
var _ core.QuitGater = (*MenuScreen)(nil)
var _ core.MotionWanter = (*MenuScreen)(nil)

// MenuItem is one row of a MenuScreen. Separator draws a muted rule and is never
// selectable; Disabled rows are muted and skipped. Hint is right-aligned display-only text
// (an accelerator label, or "›" for a submenu). Pick runs on enter or a click and owns the
// dismissal; a nil Pick is an inert row. Submenu, when set, makes the row open a child menu
// of those items beside it (enter, click, →, or hovering the row) instead of running Pick;
// its Hint defaults to "›". The child's rows own their dismissal as any Pick does — a leaf
// one level down returns core.Pop(2) to close both.
type MenuItem struct {
	Label string
	Hint  string
	// Key is the row's accelerator, a lowercase letter (0 for none): typing it picks the
	// row — opening a Submenu — ahead of the menu's navigation keys, and its first
	// occurrence in Label is underlined.
	Key       rune
	Pick      func(*core.Shared) core.Action
	Submenu   func() []MenuItem
	Disabled  bool
	Separator bool
}

// MenuStyle is how a menu draws its selection and border. The zero value is the accent
// look: accent text on the selected row, accent border.
type MenuStyle struct {
	// Selection: SelectBorder (the default) accents the selected row's text; SelectBackground
	// fills the row with SelectionColor, its text accented unless NoAccent. HideUnfocused
	// does not apply — a menu always has focus.
	Selection core.SelectionOpts
	// Focus: FocusEdges (the default) draws the border in the accent; FocusLegend keeps it
	// BorderColor, as a pane frame's edges (the title still takes the accent).
	Focus FrameFocus
}

// MenuAnchor is where a menu opens, in absolute cells. X, Y is the preferred top-left;
// FlipX and FlipY are the edges it flips away from when it does not fit (right edge at
// FlipX-1, bottom row at FlipY-1), which expresses both "flip back over the pointer" and
// "flip clear of the button". Zero flips mean "same as X/Y".
type MenuAnchor struct{ X, Y, FlipX, FlipY int }

// AnchorAt is the context-menu anchor: top-left on the pointer cell, flipping back over it
// near an edge.
func AnchorAt(x, y int) MenuAnchor {
	return MenuAnchor{X: x, Y: y, FlipX: x + 1, FlipY: y + 1}
}

// AnchorBelow is the button anchor: the menu opens below cell (x, y), or entirely above it
// when there is no room.
func AnchorBelow(x, y int) MenuAnchor {
	return MenuAnchor{X: x, Y: y + 1, FlipX: x + 1, FlipY: y}
}

// AnchorListRow anchors a menu to visible item idx of a list whose view starts at absolute
// cell (originX, originY): below the item, flipping above it. ok is false when idx is
// off-page.
func AnchorListRow(l *list.Model, idx, originX, originY int) (MenuAnchor, bool) {
	return anchorListRow(l, idx, originX, originY, listItemRows, ListItemRow)
}

// AnchorCompactListRow is AnchorListRow for a NewCompactListPanel's one-row delegate.
func AnchorCompactListRow(l *list.Model, idx, originX, originY int) (MenuAnchor, bool) {
	return anchorListRow(l, idx, originX, originY, compactListItemRows, CompactListItemRow)
}

func anchorListRow(l *list.Model, idx, originX, originY, itemRows int,
	row func(*list.Model, int) (int, bool)) (MenuAnchor, bool) {
	r, ok := row(l, idx)
	if !ok {
		return MenuAnchor{}, false
	}
	top := originY + r
	return MenuAnchor{X: originX, Y: top + itemRows, FlipX: originX + 1, FlipY: top}, true
}

// MenuOpts configures a MenuScreen. Only Items and Anchor are required.
type MenuOpts struct {
	Items  []MenuItem
	Anchor MenuAnchor
	Title  string // optional accent line above the rows; want a rule under it? make the first item a Separator
	// MaxWidth caps the content width in cells; 0 sizes to the widest row. The box is
	// clamped to the terminal either way.
	MaxWidth int
	Style    MenuStyle // selection and border look; submenus inherit it
	OnSelect func(sh *core.Shared, it MenuItem, idx int) core.Action
	OnCancel func(sh *core.Shared) core.Action
	// OnPointerOutside is offered pointer motion and left clicks that land outside every
	// box of the cascade (this menu and any submenus it opened), in absolute cells. Handled,
	// the whole cascade closes and the action runs after — a menu bar switching to the
	// label under the pointer. Declined, motion does nothing and a click dismisses as usual.
	OnPointerOutside func(sh *core.Shared, x, y int) (core.Action, bool)
	// OnKeyOutside is offered keys the cascade has no use for: any alt chord, ← in the
	// root menu (which otherwise closes it) and → on a row that opens nothing — a menu
	// bar's way to its other menus. Handled, the whole cascade closes and the action runs
	// after; declined, the key does what it always did.
	OnKeyOutside func(sh *core.Shared, k string) (core.Action, bool)
}

// NewMenu builds a dropdown from opts, normalizes the anchor's flip edges, and puts the
// cursor on the first selectable row (-1 when every row is a separator or disabled).
func NewMenu(opts MenuOpts) *MenuScreen {
	a := opts.Anchor
	if a.FlipX == 0 {
		a.FlipX = a.X
	}
	if a.FlipY == 0 {
		a.FlipY = a.Y
	}
	items := append([]MenuItem(nil), opts.Items...) // the › default must not write through
	for i := range items {
		if items[i].Submenu != nil && items[i].Hint == "" {
			items[i].Hint = "›"
		}
	}
	s := &MenuScreen{
		items:    items,
		anchor:   a,
		title:    opts.Title,
		maxWidth: opts.MaxWidth,
		style:    opts.Style,
		hoverRow: -1,
		OnSelect: opts.OnSelect,
		OnCancel: opts.OnCancel,

		onOutside:    opts.OnPointerOutside,
		onKeyOutside: opts.OnKeyOutside,
	}
	s.sel = s.firstSelectable()
	return s
}

// ---------- geometry ----------

const (
	menuChromeW = 4 // border 1 + padding 1, on each side
	menuChromeH = 2 // top + bottom border
	menuHintGap = 2 // minimum cells between a label and its hint
	menuGutterW = 2 // the scroll-marker column, present only while the window is short
)

// dims derives every dimension from state. The gutter is added before the terminal clamp,
// or the box would overflow by the gutter's width.
func (s *MenuScreen) dims() (w, h, contentW, visible, titleRows int) {
	if s.title != "" {
		titleRows = 1
	}
	visible = len(s.items)
	if s.bodyH > 0 {
		avail := max(s.bodyH-menuChromeH-titleRows, 1)
		visible = min(visible, avail)
	}
	contentW = ansi.StringWidth(s.title)
	for _, it := range s.items {
		if it.Separator {
			continue
		}
		n := ansi.StringWidth(it.Label)
		if it.Hint != "" {
			n += menuHintGap + ansi.StringWidth(it.Hint)
		}
		contentW = max(contentW, n)
	}
	if visible < len(s.items) {
		contentW += menuGutterW
	}
	if s.maxWidth > 0 {
		contentW = min(contentW, s.maxWidth)
	}
	if s.termW > 0 {
		contentW = min(contentW, s.termW-menuChromeW)
	}
	contentW = max(contentW, 1)
	return contentW + menuChromeW, visible + menuChromeH + titleRows, contentW, visible, titleRows
}

// place resolves the anchor into the box's top-left, flipping then clamping into the body
// rect. The router's own clamp is then a no-op, so Update can hit-test against this.
func (s *MenuScreen) place() (x, y, w, h int) {
	w, h, _, _, _ = s.dims()

	top, bot := s.bodyY, s.bodyY+s.bodyH
	y = s.anchor.Y
	if s.bodyH > 0 && y+h > bot {
		y = s.anchor.FlipY - h
	}
	y = max(y, top)
	if s.bodyH > 0 && y+h > bot {
		// Taller than the body itself: pin to the top and let the bottom rows go.
		y = max(top, bot-h)
	}

	x = s.anchor.X
	if s.termW > 0 && x+w > s.termW {
		x = s.anchor.FlipX - w
	}
	x = max(x, 0)
	if s.termW > 0 && x+w > s.termW {
		x = max(0, s.termW-w)
	}
	return x, y, w, h
}

// contentTop is the absolute row of the first item row. Scroll markers share the gutter
// column, so window row i is always contentTop()+i.
func (s *MenuScreen) contentTop() int {
	_, y, _, _ := s.place()
	_, _, _, _, titleRows := s.dims()
	return y + 1 + titleRows
}

// clampWindow slides the visible window to contain the cursor, then back inside the
// item range. Called after every cursor move and from SetSize.
func (s *MenuScreen) clampWindow() {
	_, _, _, visible, _ := s.dims()
	s.top = clampTop(s.top, s.sel, visible, len(s.items))
}

// ChildAnchor is the anchor for a submenu off the selected row: overlapping this box's
// right border by one cell, flipping to the left or above when there is no room.
func (s *MenuScreen) ChildAnchor() MenuAnchor {
	x, _, w, _ := s.place()
	rowY := s.contentTop()
	if s.sel >= s.top {
		rowY += s.sel - s.top
	}
	return MenuAnchor{X: x + w - 1, Y: rowY, FlipX: x, FlipY: rowY + 1}
}

// ---------- cursor ----------

func (s *MenuScreen) selectable(i int) bool {
	return i >= 0 && i < len(s.items) && !s.items[i].Separator && !s.items[i].Disabled
}

func (s *MenuScreen) firstSelectable() int {
	for i := range s.items {
		if s.selectable(i) {
			return i
		}
	}
	return -1
}

// Selected is the cursor's item index, or -1 when the menu has no selectable row.
func (s *MenuScreen) Selected() int { return s.sel }

// Select puts the cursor on idx, or on the nearest selectable row to it, and slides the
// window to follow. A menu with no selectable row is left alone.
func (s *MenuScreen) Select(idx int) {
	if s.selectable(idx) {
		s.sel = idx
		s.clampWindow()
		return
	}
	for d := 1; d < len(s.items); d++ {
		switch {
		case s.selectable(idx - d):
			s.sel = idx - d
		case s.selectable(idx + d):
			s.sel = idx + d
		default:
			continue
		}
		s.clampWindow()
		return
	}
}

// move steps to the next selectable row in direction dir, giving up after one lap. Keys
// wrap; the wheel (wrap false) does not.
func (s *MenuScreen) move(dir int, wrap bool) {
	n := len(s.items)
	if n == 0 || s.sel < 0 {
		return
	}
	i := s.sel
	for k := 0; k < n; k++ {
		i += dir
		if i < 0 || i >= n {
			if !wrap {
				return
			}
			i = (i + n) % n
		}
		if s.selectable(i) {
			s.sel = i
			s.clampWindow()
			return
		}
	}
}

// ---------- screen ----------

func (s *MenuScreen) Init(*core.Shared) tea.Cmd { return nil }

// IsOverlay marks the screen for compositing over the screen below it.
func (s *MenuScreen) IsOverlay() bool { return true }

// OverlayPos returns place()'s position. The router's box dims are ignored; place() derives
// the same numbers from state, keeping View and hit-testing in agreement.
func (s *MenuScreen) OverlayPos(int, int) (int, int) {
	x, y, _, _ := s.place()
	return x, y
}

// Filtering always reports capture: a menu is modal, so the router's global single-key
// shortcuts must not fire under it (the LineEditScreen precedent).
func (s *MenuScreen) Filtering() bool { return true }

// QuitGate implements core.QuitGater: Keys.ForceQuit closes the menu, so a host's unsaved-changes
// confirm never stacks on top of an open menu. It pops directly rather than via OnCancel,
// which the host could make do something else.
func (s *MenuScreen) QuitGate(*core.Shared) (core.Action, bool) { return core.Pop(), true }

// WantsAllMotion asks the router for free pointer motion while the menu is on top, so the
// highlight can follow the pointer and a submenu row can open on hover.
func (s *MenuScreen) WantsAllMotion() bool { return true }

// HelpView is empty: the background screen's help bar stays, as with every other overlay.
func (s *MenuScreen) HelpView(*core.Shared) string { return "" }

func (s *MenuScreen) SetSize(sh *core.Shared, width, bodyHeight int) {
	s.termW, s.bodyH, s.bodyY = width, bodyHeight, sh.BodyY()
	s.clampWindow()
}

// Update consumes EVERY message. A modal menu that leaks keystrokes to the screen it is
// covering is the failure this ordering guards against.
func (s *MenuScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	switch m := msg.(type) {
	// Presses and wheel notches act on a menu; motion only moves the highlight (and opens a
	// submenu); release is ignored.
	case tea.MouseMotionMsg:
		return s, s.hover(sh, m.Mouse())
	case tea.MouseClickMsg:
		return s, s.mouse(sh, m.Mouse())
	case tea.MouseWheelMsg:
		return s, s.mouse(sh, m.Mouse())
	case tea.KeyPressMsg:
		return s, s.key(sh, m.String())
	}
	return s, core.Action{}
}

func (s *MenuScreen) mouse(sh *core.Shared, m tea.Mouse) core.Action {
	switch m.Button {
	case tea.MouseWheelUp:
		s.move(-1, false)
		return core.Action{}
	case tea.MouseWheelDown:
		s.move(1, false)
		return core.Action{}
	case tea.MouseLeft:
	default:
		// Any other button — a right-click especially — dismisses, so the gesture that
		// raised a context menu also closes it.
		return s.cancel(sh)
	}

	// Mouse coordinates reach an overlay untranslated (the router only claims clicks on
	// its own chrome), so the box hit-tests in absolute cells against its own placement.
	x, y, w, h := s.place()
	if m.X < x || m.X >= x+w || m.Y < y || m.Y >= y+h {
		if act, ok := s.outside(sh, m.X, m.Y); ok {
			return act
		}
		return s.cancel(sh)
	}
	_, _, _, visible, _ := s.dims()
	row := m.Y - s.contentTop()
	if row < 0 || row >= visible {
		// The border and title rows swallow the click rather than dismissing: a click
		// that landed ON the menu never means "close the menu".
		return core.Action{}
	}
	idx := s.top + row
	if !s.selectable(idx) {
		return core.Action{}
	}
	s.Select(idx)
	return s.pick(sh)
}

// rowAt is the item index under absolute cell (x, y), -1 for none, and whether the cell
// is inside the box at all.
func (s *MenuScreen) rowAt(x, y int) (idx int, inBox bool) {
	bx, by, bw, bh := s.place()
	if x < bx || x >= bx+bw || y < by || y >= by+bh {
		return -1, false
	}
	_, _, _, visible, _ := s.dims()
	row := y - s.contentTop()
	if row < 0 || row >= visible {
		return -1, true
	}
	return s.top + row, true
}

// hover follows the pointer. Over this box it selects the row the pointer enters, opening
// a Submenu row. Over a parent's box — anywhere but the row that
// opened this child — the child closes, and the parent takes the next motion. Motion
// elsewhere does nothing: only a click or esc dismisses.
func (s *MenuScreen) hover(sh *core.Shared, m tea.Mouse) core.Action {
	idx, in := s.rowAt(m.X, m.Y)
	if in {
		entered := idx != s.hoverRow
		s.hoverRow = idx
		if !entered || !s.selectable(idx) {
			return core.Action{}
		}
		s.Select(idx)
		if s.items[idx].Submenu != nil {
			return s.openSubmenu()
		}
		return core.Action{}
	}
	s.hoverRow = -1
	for p, row := s.parent, s.parentRow; p != nil; p, row = p.parent, p.parentRow {
		pidx, pin := p.rowAt(m.X, m.Y)
		if !pin {
			continue
		}
		if p == s.parent && pidx == row {
			return core.Action{} // still on the row that opened this menu
		}
		return core.Pop()
	}
	if act, ok := s.outside(sh, m.X, m.Y); ok {
		return act
	}
	return core.Action{}
}

// accelerated is the selectable row whose Key is k (a single letter, either case), or -1.
func (s *MenuScreen) accelerated(k string) int {
	r := []rune(k)
	if len(r) != 1 {
		return -1
	}
	want := unicode.ToLower(r[0])
	for i, it := range s.items {
		if it.Key != 0 && unicode.ToLower(it.Key) == want && s.selectable(i) {
			return i
		}
	}
	return -1
}

// keyOutside offers k to the root's OnKeyOutside, closing the whole cascade first when
// the host takes it (see outside).
func (s *MenuScreen) keyOutside(sh *core.Shared, k string) (core.Action, bool) {
	root, depth := s, 0
	for root.parent != nil {
		root, depth = root.parent, depth+1
	}
	if root.onKeyOutside == nil {
		return core.Action{}, false
	}
	act, ok := root.onKeyOutside(sh, k)
	if !ok {
		return core.Action{}, false
	}
	return core.Seq(core.Pop(depth+1), act), true
}

// outside offers (x, y) to the root's OnPointerOutside when it lies outside every box of
// the cascade, closing the whole cascade first when the host takes it.
func (s *MenuScreen) outside(sh *core.Shared, x, y int) (core.Action, bool) {
	root, depth := s, 0
	for ; ; root, depth = root.parent, depth+1 {
		if _, in := root.rowAt(x, y); in {
			return core.Action{}, false
		}
		if root.parent == nil {
			break
		}
	}
	if root.onOutside == nil {
		return core.Action{}, false
	}
	act, ok := root.onOutside(sh, x, y)
	if !ok {
		return core.Action{}, false
	}
	return core.Seq(core.Pop(depth+1), act), true
}

// openSubmenu pushes the selected Submenu row's items beside it, in this menu's style.
func (s *MenuScreen) openSubmenu() core.Action {
	it := s.items[s.sel]
	child := NewMenu(MenuOpts{Items: it.Submenu(), Anchor: s.ChildAnchor(), Style: s.style})
	child.parent, child.parentRow = s, s.sel
	return core.Push(child)
}

func (s *MenuScreen) key(sh *core.Shared, k string) core.Action {
	if i := s.accelerated(k); i >= 0 {
		s.Select(i)
		return s.pick(sh)
	}
	onSubmenu := s.sel >= 0 && s.sel < len(s.items) && s.items[s.sel].Submenu != nil
	if strings.HasPrefix(k, "alt+") ||
		(s.parent == nil && core.MatchKey(k, core.Keys.Left)) ||
		(!onSubmenu && core.MatchKey(k, core.Keys.Right)) {
		if act, ok := s.keyOutside(sh, k); ok {
			return act
		}
	}
	switch {
	// Left is "back one level", which in a cascade is the same thing as esc.
	case core.MatchKey(k, core.Keys.Back), core.MatchKey(k, core.Keys.Left):
		return s.cancel(sh)
	case core.MatchKey(k, core.Keys.Select):
		return s.pick(sh)
	case core.MatchKey(k, core.Keys.Right):
		// → opens a submenu; on any other row it does nothing (it is not a second enter).
		if onSubmenu {
			return s.openSubmenu()
		}
	case core.MatchKey(k, core.Keys.Up):
		s.move(-1, true)
	case core.MatchKey(k, core.Keys.Down):
		s.move(1, true)
	case core.MatchKey(k, core.Keys.Top):
		s.Select(0)
	case core.MatchKey(k, core.Keys.Bottom):
		s.Select(len(s.items) - 1)
	}
	return core.Action{}
}

// pick runs OnSelect, or the item's Pick; the callback owns the dismissal.
func (s *MenuScreen) pick(sh *core.Shared) core.Action {
	if s.sel < 0 || s.sel >= len(s.items) {
		return core.Action{}
	}
	it := s.items[s.sel]
	if it.Submenu != nil {
		return s.openSubmenu()
	}
	if s.OnSelect != nil {
		return s.OnSelect(sh, it, s.sel)
	}
	if it.Pick != nil {
		return it.Pick(sh)
	}
	return core.Action{}
}

func (s *MenuScreen) cancel(sh *core.Shared) core.Action {
	if s.OnCancel != nil {
		return s.OnCancel(sh)
	}
	return core.Pop()
}

// ---------- render ----------

func (s *MenuScreen) View(*core.Shared) string {
	_, _, contentW, visible, _ := s.dims()
	gutter := visible < len(s.items)
	field := contentW
	if gutter {
		field = max(contentW-menuGutterW, 1)
	}

	muted := core.MutedStyle()
	accent := core.AccentStyle()

	rows := make([]string, 0, visible+1)
	if s.title != "" {
		rows = append(rows, accent.Render(fitCells(s.title, contentW)))
	}
	for i := 0; i < visible; i++ {
		idx := s.top + i
		body := s.row(s.items[idx], idx == s.sel, field, muted, accent)
		if gutter {
			body += scrollGutter(i, visible, s.top, len(s.items))
		}
		rows = append(rows, body)
	}
	// PopupPanel adds the chrome place() already accounts for, so the box is exactly
	// the width place() reports.
	return popupPanel(strings.Join(rows, "\n"), contentW, s.style.Focus.edgeColor(true))
}

// row renders one item into field cells: label left, hint right (see menuRow). A
// SelectBackground selection fills the whole field, gap included.
func (s *MenuScreen) row(it MenuItem, selected bool, field int, muted, accent lipgloss.Style) string {
	if it.Separator {
		return muted.Render(strings.Repeat("─", field))
	}

	label, hint, fill := lipgloss.NewStyle(), muted, lipgloss.NewStyle()
	switch {
	case it.Disabled:
		label, hint = muted, muted
	case selected && s.style.Selection.Style == core.SelectBackground:
		fill = fill.Background(core.SelectionColor)
		label, hint = fill, muted.Background(core.SelectionColor)
		if !s.style.Selection.NoAccent {
			label = accent.Background(core.SelectionColor)
		}
	case selected:
		label, hint = accent, accent
		if s.style.Selection.NoAccent {
			label, hint = lipgloss.NewStyle().Bold(true), muted
		}
	}

	text := it.Label
	if it.Key != 0 && !it.Disabled {
		// Pre-styled with the accelerator underlined; the row's own style is spent here.
		text, label = AccelLabel(it.Label, it.Key, label), lipgloss.NewStyle()
	}
	if selected && !it.Disabled && s.style.Selection.Style == core.SelectBackground {
		return menuRowFilled(text, it.Hint, field, label, hint, fill)
	}
	return menuRow(text, it.Hint, field, label, hint)
}

// AccelLabel renders label in style with the first occurrence of key (either case)
// underlined, for a menu row or a menu bar label; without one, it is style.Render(label).
func AccelLabel(label string, key rune, style lipgloss.Style) string {
	want := unicode.ToLower(key)
	for i, r := range label {
		if unicode.ToLower(r) != want {
			continue
		}
		end := i + utf8.RuneLen(r)
		out := style.Underline(true).Render(label[i:end])
		if i > 0 {
			out = style.Render(label[:i]) + out
		}
		if end < len(label) {
			out += style.Render(label[end:])
		}
		return out
	}
	return style.Render(label)
}

// menuRow lays out one floating-list row in exactly field cells: label left, hint right.
// A hint that cannot fit beside at least one label cell is dropped whole.
func menuRow(labelText, hintText string, field int, labelStyle, hintStyle lipgloss.Style) string {
	hintW := ansi.StringWidth(hintText)
	reserve := 0
	if hintText != "" && field-menuHintGap-hintW >= 1 {
		reserve = menuHintGap + hintW
	} else {
		hintW = 0
	}
	text := ansi.Truncate(labelText, max(field-reserve, 1), "…")
	out := labelStyle.Render(text) + strings.Repeat(" ", max(field-ansi.StringWidth(text)-hintW, 0))
	if hintW > 0 {
		out += hintStyle.Render(hintText)
	}
	return out
}

// menuRowFilled is menuRow for a background-filled selection: the whole field, the gap
// between label and hint included, sits on fill. labelStyle and hintStyle carry the same
// background.
func menuRowFilled(labelText, hintText string, field int, labelStyle, hintStyle, fill lipgloss.Style) string {
	hintW := ansi.StringWidth(hintText)
	reserve := 0
	if hintText != "" && field-menuHintGap-hintW >= 1 {
		reserve = menuHintGap + hintW
	} else {
		hintW = 0
	}
	text := ansi.Truncate(labelText, max(field-reserve, 1), "…")
	out := labelStyle.Render(text)
	if gap := max(field-ansi.StringWidth(text)-hintW, 0); gap > 0 {
		out += fill.Render(strings.Repeat(" ", gap))
	}
	if hintW > 0 {
		out += hintStyle.Render(hintText)
	}
	return out
}

// scrollGutter is the muted gutter cell for window row i: ↑/↓ when more items lie beyond
// that edge of the window.
func scrollGutter(i, rows, top, total int) string {
	mark := " "
	switch {
	case i == 0 && top > 0:
		mark = "↑"
	case i == rows-1 && top+rows < total:
		mark = "↓"
	}
	return core.MutedStyle().Render(strings.Repeat(" ", menuGutterW-1) + mark)
}

// clampTop slides a window of visible rows to contain sel (when sel >= 0), then back
// inside the item range.
func clampTop(top, sel, visible, total int) int {
	if sel >= 0 {
		top = max(min(top, sel), sel-visible+1)
	}
	return max(min(top, total-visible), 0)
}

// fitCells truncates or pads text to exactly w display cells.
func fitCells(text string, w int) string {
	text = ansi.Truncate(text, w, "…")
	return text + strings.Repeat(" ", max(w-ansi.StringWidth(text), 0))
}

// menuBox is LineEditBox, so the floating overlays share one frame.
func menuBox() lipgloss.Style { return LineEditBox() }
