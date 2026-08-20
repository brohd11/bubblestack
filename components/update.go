package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// This file collects the shared Update helpers: the small pieces of key-handling
// logic that screens would otherwise re-implement. They dispatch via the central
// core.Keys bindings (see core/keybinds.go) but operate on the reusable list.Model
// / Item pieces, so they live here in components rather than in core (core ←
// components, so core can't name Item or the list helpers).

// Typable is implemented by screens that hold a focused free-text field. When a
// text field has focus, printable keys that alias a navigation binding (e.g. "c"
// for Back, "e" for Select) must be typed, not dispatched. Typing reports whether
// a text field currently holds focus; UpdateInput feeds it the keystroke, so the
// screen (and under it the field) keeps ownership of whichever bubbles model it
// holds — a textinput and a textarea both route through here.
type Typable interface {
	Typing() bool
	UpdateInput(tea.Msg) tea.Cmd
}

// QueryUpdate centralizes the typing-vs-navigation split for any Typable screen.
// When the screen is typing and msg is a character/space keystroke, it feeds the
// input and reports handled=true so the caller skips its keybind switch. Otherwise
// it reports handled=false and the caller runs its normal core.Keys dispatch. Call
// it at the top of Update before the keybind switch. Reused by the Search query
// screen and the New Plugin form so the rule lives in one place. Backspace is
// diverted to the input too (it aliases Back/Keys.Back, so without this it would
// pop the screen instead of deleting a character while typing). The other control
// keys (esc/enter/tab/arrows) are never diverted, so field navigation and cancel
// reach the caller unchanged.
func QueryUpdate(s Typable, msg tea.Msg) (tea.Cmd, bool) {
	if !s.Typing() {
		return nil, false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	switch km.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace:
		return s.UpdateInput(msg), true
	}
	return nil, false
}

// RootUpdate is the shared tab-root key handling, factored out of every tab root's
// Update (project/global/archive/actions/search) since each was identical. While
// the list is filtering, keys go to the list; otherwise Select runs the highlighted
// Item's Pick closure (clearing the status line first); quit is the router's global
// q handler, not handled here. Any other
// key or message falls through to the list. A tab root's Update is then just
// `return s, components.RootUpdate(sh, &s.list, msg)`; roots that also react to
// broadcast notifications keep doing so via core.Receiver.Receive, which the router
// routes separately from Update. Returns the screen's Action.
//
// The dispatch skeleton itself (wheel/filter/key order) is listDispatch; what makes
// a root a root is only the two hooks below.
func RootUpdate(sh *core.Shared, l *list.Model, msg tea.Msg) core.Action {
	onSelect := func() core.Action {
		if pick := itemPick(l.SelectedItem()); pick != nil {
			sh.ClearStatus()
			return pick(sh)
		}
		return core.Action{}
	}
	onKey := func(k string) (core.Action, bool) {
		// Let a self-dispatching Item handle its own row keys (e.g. an addon
		// row's "t" → open terminal); unhandled keys fall through to WrapNav/list.
		if keys := itemKeys(l.SelectedItem()); keys != nil {
			return keys(sh, k)
		}
		return core.Action{}, false
	}
	return listDispatch(sh, l, msg, sh.BodyY(), onSelect, onKey)
}

func itemPick(item list.Item) func(*core.Shared) core.Action {
	switch it := item.(type) {
	case Item:
		return it.Pick
	case CompactItem:
		return it.Pick
	}
	return nil
}

func itemKeys(item list.Item) func(*core.Shared, string) (core.Action, bool) {
	switch it := item.(type) {
	case Item:
		return it.Keys
	case CompactItem:
		return it.Keys
	}
	return nil
}

// listDispatch is the dispatch skeleton shared by RootUpdate and PickerScreen.Update:
// wheel nav above the filter branch (so the wheel is not dead over a list being
// filtered), then the arrow keys and every other message to the list while filtering,
// then real keypresses
// through the select hook (always consumed) or the key hook + WrapNav, and anything
// left over to the list itself. What differs between screens — what Select runs,
// which extra keys exist, whether Back pops — lives in the hooks; the order lives
// here, so a dispatch-rule change (like the wheel-above-filter rule) is made once.
//
// A left click selects the row under the cursor and runs the select hook, exactly
// as enter would (listItemAt does the row math). mouseYOff translates the msg's
// absolute terminal row into the list view's: sh.BodyY() for a full-screen list,
// 0 for a panel whose host already made the coordinates slot-relative.
func listDispatch(sh *core.Shared, l *list.Model, msg tea.Msg, mouseYOff int,
	onSelect func() core.Action, onKey func(k string) (core.Action, bool)) core.Action {
	return listDispatchRows(sh, l, msg, mouseYOff, listItemRows, onSelect, onKey)
}

func listDispatchRows(sh *core.Shared, l *list.Model, msg tea.Msg, mouseYOff, itemRows int,
	onSelect func() core.Action, onKey func(k string) (core.Action, bool)) core.Action {
	if m, ok := msg.(tea.MouseMsg); ok {
		if m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			if idx, ok := listItemAtRows(l, m.Y-mouseYOff, itemRows); ok {
				l.Select(idx)
				return onSelect()
			}
		}
		if WheelNav(l, m) {
			return core.Action{}
		}
	}
	if l.FilterState() == list.Filtering {
		// ↑/↓ move the cursor through the matches while the query is still being typed.
		// bubbles does not: Model.Update routes to handleFiltering, which knows only
		// cancel/accept and feeds the text input, so without this the cursor is stuck on
		// the first match until the filter is accepted.
		//
		// Matched on the key TYPE, not the usual core.Keys strings: Keys.Up/Down carry
		// typable letters (k/j) that must stay text in a filter, and even the arrows'
		// string form is ambiguous here — a bracketed paste of "up" is one KeyRunes msg
		// whose String() is "up". The type is what a real arrow key alone produces.
		// (LineEditScreen.Update states the same rule for its enter/esc.)
		if km, ok := msg.(tea.KeyMsg); ok && (km.Type == tea.KeyUp || km.Type == tea.KeyDown) {
			if !WrapNav(l, km.String()) {
				if km.Type == tea.KeyUp {
					l.CursorUp()
				} else {
					l.CursorDown()
				}
			}
			return core.Action{}
		}
		var cmd tea.Cmd
		*l, cmd = l.Update(msg)
		return core.Async(cmd)
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		k := key.String()
		switch {
		case core.MatchKey(k, core.Keys.Select):
			return onSelect()
		default:
			if act, handled := onKey(k); handled {
				return act
			}
			if WrapNav(l, k) {
				return core.Action{}
			}
		}
	}
	var cmd tea.Cmd
	*l, cmd = l.Update(msg)
	return core.Async(cmd)
}

// The list layout constants are bubbles' internals, pinned by tests
// (update_test.go): each item is NewDelegate's Height(2) + Spacing(1) rows.
// listItemAt and ListItemRow are inverses over these; a bubbles upgrade that
// changes either breaks the tests loudly instead of misplacing clicks and
// overlays silently.
const (
	listItemRows        = 3 // core.NewDelegate: Height 2 + Spacing 1
	compactListItemRows = 1 // core.CompactDelegate: Height 1 + Spacing 0
)

// listHeaderHeight is the height of the section bubbles draws above the items —
// what a view row must have subtracted from it to become an item row. It is
// MEASURED rather than assumed, because it is not one number: this was a
// `listHeaderRows = 1` constant, and the row it named was only right for an
// untitled list that is not filtering.
//
//	titled                          2   the bar, plus TitleBar's bottom padding
//	titled, filtering               2   the filter input replaces the title in it
//	untitled                        1   an empty section is still a row
//	untitled, filtering             2   the input arrives, and the padding with it
//	untitled, filter APPLIED        1   the input is gone again
//
// Two of those cost a row the constant never gave back. The visible symptom was
// the filter case — the rows slide down while the click regions stay put, so the
// hit target for the top row sits on the filter input — but a TITLED list (every
// tab root and picker) was off by one all the time, masked by 3-row items where a
// one-row slip usually lands inside the same item.
//
// The shape mirrors bubbles' own Model.View/titleView (list.go), which is the
// contract being tracked: the section is drawn whenever the title shows or
// filtering is enabled, it holds the filter input while filtering and the title
// otherwise, and an EMPTY section still occupies the row JoinVertical gives it.
// Everything read here is exported, so this stays a measurement, not a copy of
// bubbles' internals. The status bar and help are off for every list built through
// core.StyleList, so the title section is the whole header.
func listHeaderHeight(l *list.Model) int {
	if !l.ShowTitle() && !(l.ShowFilter() && l.FilteringEnabled()) {
		return 0
	}
	var view string
	switch {
	case l.ShowFilter() && l.FilterState() == list.Filtering:
		view = l.FilterInput.View()
	case l.ShowTitle():
		view = l.Styles.Title.Render(l.Title)
	}
	if view == "" {
		return 1
	}
	return lipgloss.Height(l.Styles.TitleBar.Render(view))
}

// listItemAt maps a row within the list's rendered view to a visible-item
// index, reporting false for clicks outside the items (the header, the spacing
// below the last item, the pagination row). Select takes a visible-item index
// and paginates to it, so a click lands even mid-page.
func listItemAt(l *list.Model, relY int) (int, bool) {
	return listItemAtRows(l, relY, listItemRows)
}

func listItemAtRows(l *list.Model, relY, itemRows int) (int, bool) {
	row := relY - listHeaderHeight(l)
	if row < 0 {
		return 0, false
	}
	pageRow := row / itemRows
	// The content area is followed by blank fill and/or pagination. Without
	// this page-local bound, the first row after the page selects the first item
	// of the next page whenever more visible items exist.
	if pageRow >= l.Paginator.PerPage {
		return 0, false
	}
	idx := l.Paginator.Page*l.Paginator.PerPage + pageRow
	if idx < 0 || idx >= len(l.VisibleItems()) {
		return 0, false
	}
	return idx, true
}

// ListItemRow is the inverse of listItemAt: the row, relative to the list's own
// view, at which visible item idx starts, and whether idx is on the current
// page. A screen that overlays a floating editor (LineEditScreen) over the
// selected row gets its anchor here — add the list's absolute offsets (BodyY
// for a full-screen list, the pane offset for a ListPanel) for terminal
// coordinates. ok is false when the item is scrolled off-page; the caller
// picks the fallback (clamp to a visible row, or center).
func ListItemRow(l *list.Model, idx int) (int, bool) {
	return listItemRow(l, idx, listItemRows)
}

// CompactListItemRow is ListItemRow for NewCompactListPanel's one-row delegate.
func CompactListItemRow(l *list.Model, idx int) (int, bool) {
	return listItemRow(l, idx, compactListItemRows)
}

func listItemRow(l *list.Model, idx, itemRows int) (int, bool) {
	if idx < 0 || idx >= len(l.VisibleItems()) {
		return 0, false
	}
	start := l.Paginator.Page * l.Paginator.PerPage
	if idx < start || idx >= start+l.Paginator.PerPage {
		return 0, false
	}
	return listHeaderHeight(l) + (idx-start)*itemRows, true
}

// SetListItems replaces a list's rows and keeps any live filter working. Use it in place
// of list.Model.SetItems everywhere.
//
// bubbles' SetItems nils filteredItems SYNCHRONOUSLY and puts the recompute in the
// tea.Cmd it returns (list.go SetItems). Drop that cmd — which every call site in this
// workspace did, because the wrappers it travels through return a core.Action — and the
// list renders EMPTY for as long as the filter is applied, with the row count sitting in
// a "N filtered" counter nobody can see. That is the "refresh emptied my sidebar" bug.
//
// It gets worse on the way out: pressing / again keeps the old query (bubbles tests the
// input's VALUE, not the match set, before repopulating), so nothing recomputes, and the
// next enter — or up, or down, which are all "accept" keys — hits bubbles' "filtered down
// to nothing, clear the filter" branch and silently wipes the query. Hence the second
// half of the report: the filter appears to reject the text you just gave it.
//
// The repair is synchronous rather than a propagated cmd: SetFilterText runs the filter
// inline and assigns the result. A cmd-returning signature would be correct too, but it
// has to be threaded through every Receive hook and item-refresh path to work, and one
// forgetful caller re-arms the whole trap. This cannot rot.
func SetListItems(l *list.Model, items []list.Item) {
	state, query := l.FilterState(), l.FilterValue()
	l.SetItems(items)
	if state == list.Unfiltered || query == "" {
		return
	}
	// Recomputes the matches against the new rows and lands in FilterApplied.
	l.SetFilterText(query)
	if state == list.Filtering {
		// Back to typing, with the matches just computed left in place.
		l.SetFilterState(list.Filtering)
	}
}

// FitList sizes l to (w, h), repeating the calculation until bubbles' pagination
// reaches a fixed point. bubbles computes PerPage from the CURRENT pagination view,
// then updates TotalPages from that new PerPage; for a compact delegate using bubbles'
// default pagination style, crossing the one-page boundary changes the pagination view
// by one row, so one SetSize can leave the rendered list a row taller than h. Three
// bounded passes cover the possible 1 -> many -> adjusted-many transition without
// risking an unbounded layout loop.
func FitList(l *list.Model, w, h int) {
	pages := l.Paginator.TotalPages
	for range 3 {
		l.SetSize(w, h)
		if l.Paginator.TotalPages == pages {
			return
		}
		pages = l.Paginator.TotalPages
	}
}

// WrapNav wraps the cursor at a list boundary: up on the first row selects the
// last, down on the last selects the first. Returns handled=true when it wrapped
// (caller skips forwarding the key to the list). Uses the central core.Keys bindings,
// so the wrap follows any added scheme (e.g. wasd); l.Select adjusts pagination
// itself, so wrapping works across pages.
//
// The count is the VISIBLE set, which is what Index() and Select() are indexed over.
// It used to be len(l.Items()) with a "call only when not filtering" note that the
// dispatch above did not honor — so under a filter matching 3 of 100 rows, down at the
// last match never met Index() == n-1 and did not wrap, while up at the first ran
// Select(99): unclamped, so the cursor landed outside the filtered set, SelectedItem
// went nil and the page rendered blank.
func WrapNav(l *list.Model, k string) bool {
	n := len(l.VisibleItems())
	if n < 2 {
		return false
	}
	switch {
	case core.MatchKey(k, core.Keys.Up) && l.Index() == 0:
		l.Select(n - 1)
		return true
	case core.MatchKey(k, core.Keys.Down) && l.Index() == n-1:
		l.Select(0)
		return true
	}
	return false
}

// WheelNav moves the list cursor one row per wheel notch, reporting handled=true when
// it consumed the event. bubbles' list binds no mouse events at all, so without this the
// wheel would scroll a doc or the log but do nothing on a tab root or picker — the
// screens a user is on most of the time, which reads as a broken wheel rather than a
// keyboard-only list.
//
// One row per notch, not the viewport's three: three is right for a wall of text, but
// on a list it skips items. Unlike WrapNav this clamps at the boundaries (CursorUp /
// CursorDown do so unless InfiniteScrolling is set) — a wheel that teleported from the
// last row back to the first would be a scroll that overshoots by the whole list.
func WheelNav(l *list.Model, msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress {
		return false
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		l.CursorUp()
		return true
	case tea.MouseButtonWheelDown:
		l.CursorDown()
		return true
	}
	return false
}
