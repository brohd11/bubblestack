package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// MenuScreen is the floating dropdown / context menu: a small bordered box of rows the
// router composites over the screen below it (core.Overlayer) at a caller-supplied
// anchor (core.OverlayPositioner), rather than the full-screen list a PickerScreen
// draws. It is the chooser that stays next to the thing it acts on — the menu a button
// drops down, the menu a right-click raises over a row — where a PickerScreen would
// replace the very view the choice is about.
//
// It inherits three things from the overlay machinery and adds nothing to the router:
//
//   - MODAL FOR FREE: only Router.Top() receives Update, so a pushed menu owns the keys.
//     Filtering() reports true unconditionally (the LineEditScreen precedent) so the
//     router's global single-key shortcuts (q/O/o/c/t/T/[/]) can't fire underneath it.
//     ctrl+c is the one key that runs ahead of that gate, so QuitGate answers it instead:
//     it closes the menu rather than letting a host's unsaved-changes confirm stack on
//     top of one.
//   - CASCADES FOR FREE: z-order is stack order and every overlay composites over the
//     deepest non-overlay screen's whole frame, so a submenu pushed over its parent
//     leaves the parent drawn. An item opens one by returning core.Push of a second
//     menu anchored at the parent's ChildAnchor(); MenuItem carries no "has submenu"
//     bit, because a submenu is just a Pick that pushes.
//   - THE CALLBACK OWNS THE DISMISSAL: like DialogScreen.OnYes and LineEditScreen.OnDone,
//     a Pick that wants the menu gone returns core.Pop() (or core.Seq(core.Pop(), act))
//     itself. The menu never pops on select, which is exactly what lets a Pick push a
//     submenu instead.
//
// Rows are hand-rolled rather than a wrapped bubbles list.Model: a menu wants to be
// exactly as wide as its widest label and exactly as tall as its item count, with no
// title bar, filter row, pagination row or page-based scrolling to make the row math
// conditional. That math is the whole ballgame here — every rendered row has to map back
// to an item index for click hit-testing, and place() has to agree with View() to the
// cell (menu_test.go pins both).
//
// Esc closes ONE level: a plain core.Pop, the same thing esc means everywhere else in
// the stack. A caller who wants esc to collapse a whole cascade sets the child's
// OnCancel to core.Pop(n) — nesting depth is known where the menu is built.
//
// It contributes NO breadcrumb segment — deliberately not a core.Crumber, the stance
// LineEditScreen takes (lineedit.go) and the opposite of every full-screen component's.
// A dropdown is not a place you navigated to, so a trail that grew a segment each time
// one opened would flicker a step in and out on every right-click.
//
// Known compromises, all of them consequences of keeping this inside the component:
//
//  1. core.Shared has no Height() accessor, so the menu can only see the BODY rect
//     (Shared.BodyY() plus the bodyHeight handed to SetSize). It confines its box to
//     that rect and never spills over the header, tab strip, output pane or help bar.
//     That is also what makes it correct: the body rect is strictly inside the region
//     the router clamps to, so the router's clamp is a no-op and place() is exactly
//     where the box lands — which is what Update can then hit-test against.
//  2. The router only re-SetSizes the base and the top screen, so a parent menu in a
//     cascade keeps a stale termW/bodyH if the terminal is resized while a submenu is
//     open. LineEditScreen has the same latent issue; fixing it is a core change.
//  3. A click on the parent's box while a submenu is up only closes the submenu — it is
//     "outside" the child, and cannot also be forwarded to the parent, which is not
//     Top(). The second click acts on the parent.
//  4. The router claims some clicks before the menu sees them (router_keys.go): tab-strip
//     and breadcrumb clicks unwind the stack out from under a live menu, dismissing it
//     (the menu draws no segment of its own, but the ones behind it are still hittable);
//     a header click
//     runs the consumer's HeaderPane.OnClick, which could push over a live menu; an
//     output-pane click moves keyboard focus, leaving the menu drawn but keyless until
//     esc. All three are call-site concerns.
//  5. Right-click is claimed per call site, not globally. editor.Screen takes it when its
//     host sets editor.Opts.ContextMenu — the first in-tree consumer, and the worked
//     example of anchoring with AnchorAt in absolute cells from inside a pane. Everywhere
//     else the button is still up for grabs, and two screens on one stack must not both
//     claim it; this component arbitrates nothing.
//  6. MenuItem.Hint is INERT TEXT. It renders the accelerator column, but this version
//     dispatches no accelerators and does no type-ahead; a hint that reads like a key is
//     a promise the menu does not yet keep.
type MenuScreen struct {
	items    []MenuItem
	anchor   MenuAnchor
	title    string
	maxWidth int

	sel int // index into items; -1 when nothing is selectable
	top int // first item of the visible window

	termW int // terminal width, from SetSize
	bodyH int // body height, from SetSize
	bodyY int // absolute row the body starts at, from Shared.BodyY()

	// OnSelect, when set, claims every selection wholesale — the item's own Pick is
	// not consulted (the PickerScreen precedent). Both follow the callback-owns-the-
	// dismissal convention.
	OnSelect func(*core.Shared, MenuItem, int) core.Action
	// OnCancel runs on esc/left and on a dismissing click; nil ⇒ a plain core.Pop.
	OnCancel func(*core.Shared) core.Action
}

// Items are the menu's rows as built, including any separators — the finished row set
// a caller assembled through MenuOpts plus whatever the constructor added. Read-only:
// the slice backs the live menu, so callers must not mutate it.
func (s *MenuScreen) Items() []MenuItem { return s.items }

var _ core.Overlayer = (*MenuScreen)(nil)
var _ core.OverlayPositioner = (*MenuScreen)(nil)
var _ core.Filterer = (*MenuScreen)(nil)
var _ core.QuitGater = (*MenuScreen)(nil)

// MenuItem is one row of a MenuScreen.
//
// Separator draws a muted rule and is never selectable; Disabled renders muted and is
// skipped by keyboard nav and ignored by clicks. Hint is right-aligned muted text — an
// accelerator label, or a "›" marking a row that opens a submenu — and is display-only
// (see the type comment). Pick runs on enter or a click and owns its own dismissal; a
// nil Pick is an inert row (the Item convention), consuming the enter and leaving the
// menu up.
type MenuItem struct {
	Label     string
	Hint      string
	Pick      func(*core.Shared) core.Action
	Disabled  bool
	Separator bool
}

// MenuAnchor is where a menu's box opens, in absolute terminal cells.
//
// X, Y is the PREFERRED top-left corner. FlipX and FlipY are the edges the box flips
// away from when it doesn't fit in that direction: a right→left flip puts the box's
// right edge at FlipX-1, a below→above flip puts its bottom row at FlipY-1. That is why
// they are edges rather than "the anchor cell" — it is the only way to express both
// "flip back over the pointer" (a context menu) and "flip clear of the button" (a
// dropdown) with one rule. A zero FlipX/FlipY means "same as X/Y"; NewMenu normalizes,
// so a bare MenuAnchor{X: x, Y: y} is a legal no-flip anchor.
type MenuAnchor struct{ X, Y, FlipX, FlipY int }

// AnchorAt is the context-menu anchor: the box's top-left lands on the pointer cell, and
// when it doesn't fit it flips back OVER the pointer (right edge / bottom row on the
// pointer cell itself), the way a desktop context menu behaves near a screen edge.
func AnchorAt(x, y int) MenuAnchor {
	return MenuAnchor{X: x, Y: y, FlipX: x + 1, FlipY: y + 1}
}

// AnchorBelow is the button anchor: the widget occupies cell (x, y), the menu opens on
// the row BELOW it, and when there is no room it flips to sit entirely above it — the
// button is never covered either way.
func AnchorBelow(x, y int) MenuAnchor {
	return MenuAnchor{X: x, Y: y + 1, FlipX: x + 1, FlipY: y}
}

// AnchorListRow anchors a menu to visible item idx of a list whose own view has its
// top-left at absolute cell (originX, originY) — Shared.BodyY() for a full-screen list,
// the PaneOriginer origin for one inside a pane. The menu opens on the first row below
// the whole item and flips clear above it, so the row it acts on stays visible. ok is
// false when idx is scrolled off-page; the caller picks the fallback, as with
// ListItemRow.
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
	OnSelect func(sh *core.Shared, it MenuItem, idx int) core.Action
	OnCancel func(sh *core.Shared) core.Action
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
	s := &MenuScreen{
		items:    opts.Items,
		anchor:   a,
		title:    opts.Title,
		maxWidth: opts.MaxWidth,
		OnSelect: opts.OnSelect,
		OnCancel: opts.OnCancel,
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

// dims derives every dimension from state, so nothing has to be cached from a render
// pass. The gutter is added to contentW BEFORE the terminal clamp — the other order
// lets the box overflow the terminal by the gutter's width.
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

// place resolves the anchor into the box's actual top-left cell, flipping and then
// clamping into the body rect. Because the body rect is strictly inside the frame the
// router clamps to, the router's own clamp is a no-op and this is where the box really
// lands — which is what makes it safe for Update to hit-test against.
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

// contentTop is the absolute row of the first item row — the box's top border and any
// title line above it. Window row i is always contentTop()+i: the scroll markers share
// the item rows' gutter column rather than taking rows of their own, precisely so this
// stays exception-free.
func (s *MenuScreen) contentTop() int {
	_, y, _, _ := s.place()
	_, _, _, _, titleRows := s.dims()
	return y + 1 + titleRows
}

// clampWindow slides the visible window to contain the cursor, then back inside the
// item range. Called after every cursor move and from SetSize.
func (s *MenuScreen) clampWindow() {
	_, _, _, visible, _ := s.dims()
	if visible >= len(s.items) {
		s.top = 0
		return
	}
	if s.sel >= 0 {
		s.top = min(s.top, s.sel)
		s.top = max(s.top, s.sel-visible+1)
	}
	s.top = min(s.top, len(s.items)-visible)
	s.top = max(s.top, 0)
}

// ChildAnchor is the anchor a submenu opening off the selected row should use: its left
// border overlaps this box's right border by one cell so the cascade reads as connected,
// and its own flip rules put it on the left (right edge flush against this box's left
// border) or above when there is no room.
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

func (s *MenuScreen) lastSelectable() int {
	for i := len(s.items) - 1; i >= 0; i-- {
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

// move steps the cursor to the next selectable row in direction dir, skipping separators
// and disabled rows. It gives up after one full lap, so an all-inert menu is a no-op.
// wrap is the arrows' behavior; the wheel passes false (WheelNav's convention: keys wrap,
// the wheel doesn't).
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

// OverlayPos hands the router the position place() already resolved. The box dims the
// router passes are ignored: they come from the rendered View, and place() computes the
// same numbers from state — which is what keeps View and the click hit-test in agreement
// within a single frame.
func (s *MenuScreen) OverlayPos(int, int) (int, int) {
	x, y, _, _ := s.place()
	return x, y
}

// Filtering always reports capture: a menu is modal, so the router's global single-key
// shortcuts must not fire under it (the LineEditScreen precedent).
func (s *MenuScreen) Filtering() bool { return true }

// QuitGate implements core.QuitGater: a quit attempt while the menu is up closes the menu
// instead, and a second press meets whatever gate lies underneath. Without it the router's
// stack walk skips straight past the menu to the screen below — which is how a host's
// unsaved-changes confirm ends up drawn on top of an open context menu, with cancelling it
// dropping the user back into a menu they had already tried to leave. In a cascade each
// press unwinds one level, since the walk reaches the innermost menu first.
//
// It pops directly rather than going through cancel(): OnCancel belongs to the host, and
// one that returned anything other than a pop would leave ctrl+c — the escape hatch of
// last resort — unable to make progress. This dismissal is the framework's call.
func (s *MenuScreen) QuitGate(*core.Shared) (core.Action, bool) { return core.Pop(), true }

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
	// Only presses and wheel notches act on a menu; motion and release pass through
	// untouched, as they do everywhere else in v2.
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

func (s *MenuScreen) key(sh *core.Shared, k string) core.Action {
	switch {
	// Left is "back one level", which in a cascade is the same thing as esc.
	case core.MatchKey(k, core.Keys.Back), core.MatchKey(k, core.Keys.Left):
		return s.cancel(sh)
	case core.MatchKey(k, core.Keys.Select):
		return s.pick(sh)
	case core.MatchKey(k, core.Keys.Up):
		s.move(-1, true)
	case core.MatchKey(k, core.Keys.Down):
		s.move(1, true)
	case core.MatchKey(k, core.Keys.Top):
		s.Select(0)
	case core.MatchKey(k, core.Keys.Bottom):
		s.Select(len(s.items) - 1)
	}
	// Keys.Right is deliberately unbound: opening a submenu with → would need to know an
	// item HAS one, and a submenu here is just a Pick that pushes — so → would have to
	// mean Select, firing real actions on leaf rows.
	return core.Action{}
}

// pick runs the selection handler. OnSelect claims the row wholesale when set, else the
// item's own Pick; either way the callback owns the dismissal, so a menu that should
// close returns core.Pop() from it.
func (s *MenuScreen) pick(sh *core.Shared) core.Action {
	if s.sel < 0 || s.sel >= len(s.items) {
		return core.Action{}
	}
	it := s.items[s.sel]
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

	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	accent := lipgloss.NewStyle().Foreground(core.FocusedColor).Bold(true)

	rows := make([]string, 0, visible+1)
	if s.title != "" {
		rows = append(rows, accent.Render(fitCells(s.title, contentW)))
	}
	for i := 0; i < visible; i++ {
		idx := s.top + i
		body := s.row(s.items[idx], idx == s.sel, field, muted, accent)
		if gutter {
			mark := " "
			switch {
			case i == 0 && s.top > 0:
				mark = "↑"
			case i == visible-1 && s.top+visible < len(s.items):
				mark = "↓"
			}
			body += muted.Render(strings.Repeat(" ", menuGutterW-1) + mark)
		}
		rows = append(rows, body)
	}
	// PopupPanel adds the chrome place() already accounts for, so the box is exactly
	// the width place() reports.
	return PopupPanel(strings.Join(rows, "\n"), contentW)
}

// row renders one item into exactly field cells: label left, hint right. A hint is
// dropped whole rather than truncated when the field can't hold it plus a cell of label —
// half an accelerator is worse than none.
func (s *MenuScreen) row(it MenuItem, selected bool, field int, muted, accent lipgloss.Style) string {
	if it.Separator {
		return muted.Render(strings.Repeat("─", field))
	}

	label, hint := lipgloss.NewStyle(), muted
	switch {
	case it.Disabled:
		label, hint = muted, muted
	case selected:
		label, hint = accent, accent
	}

	hintW := ansi.StringWidth(it.Hint)
	reserve := 0
	if it.Hint != "" && field-menuHintGap-hintW >= 1 {
		reserve = menuHintGap + hintW
	} else {
		hintW = 0
	}
	text := ansi.Truncate(it.Label, field-reserve, "…")
	pad := max(field-ansi.StringWidth(text)-hintW, 0)

	out := label.Render(text) + strings.Repeat(" ", pad)
	if hintW > 0 {
		out += hint.Render(it.Hint)
	}
	return out
}

// fitCells truncates or pads text to exactly w display cells.
func fitCells(text string, w int) string {
	text = ansi.Truncate(text, w, "…")
	return text + strings.Repeat(" ", max(w-ansi.StringWidth(text), 0))
}

// menuBox is the menu's frame. It is lineEditBox deliberately and not by accident: the
// floating overlays are one family and should read as one, so the border lives in a
// single place and a change to it moves them together.
func menuBox() lipgloss.Style { return LineEditBox() }
