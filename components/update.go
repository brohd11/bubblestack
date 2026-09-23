package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Shared Update helpers for list screens. They live here rather than in core because they
// operate on list.Model and Item, which core cannot name.

// Typable is a screen holding a focused free-text field. While Typing, printable keys that
// alias a navigation binding ("c" for Back) must be typed; UpdateInput feeds them to
// whichever bubbles model the field holds.
type Typable interface {
	Typing() bool
	UpdateInput(tea.Msg) tea.Cmd
}

// QueryUpdate splits typing from navigation for a Typable screen. Call it before the
// keybind switch: while typing, printable keys and backspace (which aliases Back) go to
// the input and it reports handled. Other control keys (esc, enter, tab, arrows) always
// reach the caller.
func QueryUpdate(s Typable, msg tea.Msg) (tea.Cmd, bool) {
	if !s.Typing() {
		return nil, false
	}
	switch km := msg.(type) {
	case tea.PasteMsg:
		// v2 delivers a bracketed paste as its own message rather than a rune-bearing
		// key, so it is diverted explicitly instead of falling out of the rune case.
		return s.UpdateInput(msg), true
	case tea.KeyPressMsg:
		// Text is set only for printable keys; backspace carries none but must reach the input.
		if km.Text != "" || km.Code == tea.KeyBackspace {
			return s.UpdateInput(msg), true
		}
	}
	return nil, false
}

// RootUpdate is the shared tab-root Update: while filtering, keys go to the list;
// otherwise Select runs the highlighted Item's Pick (clearing the status line). Anything
// else goes to the list. A root's Update is just
// `return s, components.RootUpdate(sh, &s.list, msg)`.
func RootUpdate(sh *core.Shared, l *list.Model, msg tea.Msg) core.Action {
	return RootUpdateRows(sh, l, msg, listItemRows)
}

// RootUpdateRows is RootUpdate for a root whose row height is not the default delegate's
// (a compact Density). Pass Density.ItemRows(); mouse hit-testing divides by it.
func RootUpdateRows(sh *core.Shared, l *list.Model, msg tea.Msg, itemRows int) core.Action {
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
	return listDispatch(sh, l, msg, sh.BodyY(), itemRows, onSelect, onKey, nil)
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

// listDispatch is the dispatch skeleton shared by list screens and panels, in order:
// clicks (select the row, then onPointer or onSelect), wheel nav, keys to the list while
// filtering, then Select, onKey and WrapNav, and anything left to the list itself.
// mouseYOff converts absolute rows to the list view's (sh.BodyY() full-screen, 0 for a
// panel with slot-relative coordinates). onPointer, when set, lets a click differ from
// enter; handled=false falls back to the default for that button.
func listDispatch(sh *core.Shared, l *list.Model, msg tea.Msg, mouseYOff, itemRows int,
	onSelect func() core.Action, onKey func(k string) (core.Action, bool),
	onPointer func(right bool) (core.Action, bool)) core.Action {
	if mm, ok := msg.(tea.MouseMsg); ok {
		m := mm.Mouse()
		if _, isClick := mm.(tea.MouseClickMsg); isClick {
			switch m.Button {
			case tea.MouseLeft:
				if idx, ok := listItemAt(l, m.Y-mouseYOff, itemRows); ok {
					l.Select(idx)
					if onPointer != nil {
						if act, handled := onPointer(false); handled {
							return act
						}
					}
					return onSelect()
				}
			case tea.MouseRight:
				// Only for a list that asked for it; elsewhere a right press stays inert.
				if onPointer == nil {
					break
				}
				if idx, ok := listItemAt(l, m.Y-mouseYOff, itemRows); ok {
					l.Select(idx)
					if act, handled := onPointer(true); handled {
						return act
					}
					return core.Action{}
				}
			}
		}
		if _, isWheel := mm.(tea.MouseWheelMsg); isWheel && WheelNav(l, m) {
			return core.Action{}
		}
	}
	if l.FilterState() == list.Filtering {
		// ↑/↓ move through the matches while typing (bubbles only feeds the input then).
		// Raw keycodes, not core.Keys: Up/Down also carry k/j, which must stay text here.
		if km, ok := msg.(tea.KeyPressMsg); ok && (km.String() == "up" || km.String() == "down") {
			if !WrapNav(l, km.String()) {
				if km.String() == "up" {
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
	if key, ok := msg.(tea.KeyPressMsg); ok {
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

// Rows per item for the two delegates, pinned by update_test.go against bubbles.
const (
	listItemRows        = 3 // core.NewDelegate: Height 2 + Spacing 1
	compactListItemRows = 1 // core.CompactDelegate: Height 1 + Spacing 0
)

// listHeaderHeight is the measured height of the section bubbles draws above the items
// (title bar or filter input), mirroring bubbles' titleView. It varies with title and
// filter state, so it cannot be a constant:
//
//	titled (filtering or not)  2   the bar plus TitleBar's bottom padding
//	untitled                   1   an empty section is still a row
//	untitled, filtering        2
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

// listItemAt maps a row within the list's rendered view to a visible-item index,
// reporting false outside the items (header, trailing space, pagination).
func listItemAt(l *list.Model, relY, itemRows int) (int, bool) {
	row := relY - listHeaderHeight(l)
	if row < 0 {
		return 0, false
	}
	pageRow := row / itemRows
	// Page-local bound: the row after the page must not select the next page's first item.
	if pageRow >= l.Paginator.PerPage {
		return 0, false
	}
	idx := l.Paginator.Page*l.Paginator.PerPage + pageRow
	if idx < 0 || idx >= len(l.VisibleItems()) {
		return 0, false
	}
	return idx, true
}

// ListItemRow is the inverse of listItemAt: the view-relative row where visible item idx
// starts, for anchoring an overlay on a row. Add the list's own offset for terminal
// cells. ok is false when idx is off the current page.
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

// SetListItems replaces a list's rows and keeps a live filter working; use it instead of
// list.Model.SetItems. bubbles' SetItems clears the filtered set immediately and
// recomputes it in the returned cmd, which callers drop, so a filtered list renders empty
// (and the next accept key wipes the query). This re-runs the filter synchronously, so
// no caller can forget the cmd.
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

// FitList sizes l to (w, h), repeating until bubbles' pagination settles: crossing the
// one-page boundary changes the pagination row, so one SetSize can leave the list a row
// too tall. Three passes bound it.
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

// WrapNav wraps the cursor at the ends of the visible (filtered) rows, reporting whether
// it did. It follows core.Keys and works across pages.
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

// WheelNav moves the cursor one row per wheel notch (bubbles' list handles no mouse),
// clamping at the ends rather than wrapping.
func WheelNav(l *list.Model, msg tea.Mouse) bool {
	switch msg.Button {
	case tea.MouseWheelUp:
		l.CursorUp()
		return true
	case tea.MouseWheelDown:
		l.CursorDown()
		return true
	}
	return false
}
