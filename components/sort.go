package components

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

// SortMode selects the ordering of a data-backed list. It is a per-screen session
// choice cycled by a key; the owning screen holds the value and rebuilds its list when
// it changes. Only the enum + label + cycle carry no bubbles/list dependency; the
// list-touching helpers below take a *list.Model. The domain sort (by name / by some
// app-specific attention rank) is applied in each screen's item builder, not here — a
// screen with no status concept simply omits SortStatus from the modes it cycles.
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

// NextSort advances cur to the next mode within the allowed set (wrapping), so a
// screen can restrict the cycle — e.g. {Alpha, Reverse, Status} vs. {Alpha, Reverse}.
// A cur not in modes (or an empty set) falls back to the first allowed mode.
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

// SortItemsByTitle reorders rows in place by their Title, case-insensitively;
// reverse flips it. Stable, so equal titles keep their prior order. For lists whose
// rows carry no sortable domain field — a screen that sorts real domain data should
// order that data before building rows, and use SelectedTitle/SelectByTitle to keep
// the cursor.
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

// SelectByTitle moves the cursor to the first row whose Title matches title (a
// no-op for an empty title or no match), so a caller can keep the cursor on the
// same row after SetItems reorders the list.
//
// It scans the VISIBLE rows, because that is the set Select indexes: under a filter,
// walking l.Items() would hand Select a position in the unfiltered slice and land the
// cursor somewhere unrelated — or past the end, where the selection reads as nil and
// the page renders blank. (The same trap WrapNav carried.)
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

// CycleSort advances *mode to the next value in modes and rebuilds l from items(*mode),
// keeping the cursor on the same row and retitling via SortTitle(base, *mode). The
// shared body of a screen's sort toggle; items adapts the screen's row builder to the
// (mode) signature.
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
