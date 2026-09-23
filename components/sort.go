package components

import (
	"sort"
	"strings"

	"charm.land/bubbles/v2/list"
)

// SortMode is a list's ordering, a per-screen choice cycled by a key. The domain sort
// happens in each screen's row builder; a screen without a status concept omits
// SortStatus.
type SortMode int

const (
	SortAlpha   SortMode = iota // A→Z by name (case-insensitive)
	SortReverse                 // Z→A by name (case-insensitive)
	SortStatus                  // grouped by an app-defined attention rank
)

// SortTitle renders a list's base title with its active sort mode appended, e.g.
// "Repos — A→Z". Shared by a screen's New* constructor and CycleSort.
func SortTitle(base string, m SortMode) string { return base + " — " + m.Label() }

// Label is the short suffix shown in a list's Title, e.g. "Repos — A→Z".
func (m SortMode) Label() string {
	switch m {
	case SortReverse:
		return "Z→A"
	case SortStatus:
		return "status"
	default:
		return "A→Z"
	}
}

// NextSort advances cur within modes, wrapping; a cur not in modes gives the first.
func NextSort(cur SortMode, modes []SortMode) SortMode {
	for i, m := range modes {
		if m == cur {
			return modes[(i+1)%len(modes)]
		}
	}
	if len(modes) > 0 {
		return modes[0]
	}
	return cur
}

// SortItemsByTitle stably sorts rows by Title, case-insensitively (reverse flips it),
// for rows with no domain field to sort by.
func SortItemsByTitle(items []list.Item, reverse bool) {
	sort.SliceStable(items, func(i, j int) bool {
		a := strings.ToLower(itemTitle(items[i]))
		b := strings.ToLower(itemTitle(items[j]))
		if reverse {
			return a > b
		}
		return a < b
	})
}

// SelectedTitle returns the highlighted row's Title, or "" if there is none.
func SelectedTitle(l *list.Model) string { return itemTitle(l.SelectedItem()) }

// SelectByTitle moves the cursor to the first visible row titled title, to keep the
// cursor after a reorder. It scans visible rows because that is what Select indexes.
func SelectByTitle(l *list.Model, title string) {
	if title == "" {
		return
	}
	for i, it := range l.VisibleItems() {
		if itemTitle(it) == title {
			l.Select(i)
			return
		}
	}
}

// CycleSort advances *mode, rebuilds l from items(*mode) keeping the cursor, and retitles
// it.
func CycleSort(l *list.Model, mode *SortMode, modes []SortMode, base string, items func(SortMode) []list.Item) {
	sel := SelectedTitle(l)
	*mode = NextSort(*mode, modes)
	// SetListItems, not l.SetItems: the latter drops the filter's recompute cmd, which
	// leaves a filtered list rendering empty after a re-sort.
	SetListItems(l, items(*mode))
	SelectByTitle(l, sel)
	l.Title = SortTitle(base, *mode)
}

func itemTitle(it list.Item) string {
	if t, ok := it.(interface{ Title() string }); ok {
		return t.Title()
	}
	return ""
}
