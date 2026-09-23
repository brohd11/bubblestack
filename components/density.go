package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// Density is one list's row-density state, keeping four things in agreement: the
// delegate, the pagination re-fit, the row height used for hit-testing, and restoring the
// current density (not the default) after a theme rebuild. It is for screens owning a
// bare list.Model (tab roots, PickerScreen); ListPanel's densities differ in more, so
// FilePanel rebuilds instead. The zero value is the default density; methods are no-ops
// until the first Fit.
type Density struct {
	compact bool
	w, h    int
}

// Compact reports which density is live.
func (d *Density) Compact() bool { return d.compact }

// Delegate is the live density's renderer. The compact one has no marquee Offset (no
// clock without a ListPanel), so long rows truncate.
func (d *Density) Delegate() list.ItemDelegate {
	if d.compact {
		return core.CompactDelegate{}
	}
	return core.NewDelegate()
}

// ItemRows is the screen rows one item occupies at the live density — the divisor mouse
// hit-testing needs. A click divided by the wrong one selects a row up to three places off.
func (d *Density) ItemRows() int {
	if d.compact {
		return compactListItemRows
	}
	return listItemRows
}

// SetCompact swaps the delegate if the density changes, reporting whether it did. The
// cursor, page and applied filter survive; the stored size is replayed because PerPage
// depends on row height.
func (d *Density) SetCompact(l *list.Model, compact bool) bool {
	if compact == d.compact {
		return false
	}
	d.compact = compact
	l.SetDelegate(d.Delegate())
	if d.w > 0 {
		FitList(l, d.w, d.h)
	}
	return true
}

// Toggle flips the density. It is the whole body of a screen's density key.
func (d *Density) Toggle(l *list.Model) { d.SetCompact(l, !d.compact) }

// Restyle rebuilds the live density's delegate after a theme change (both cache colors),
// without resetting the density.
func (d *Density) Restyle(l *list.Model) {
	l.SetDelegate(d.Delegate())
	core.StyleList(l)
}

// Fit records the size and applies it through FitList; use it instead of list.SetSize
// (compact pagination can need a second pass).
func (d *Density) Fit(l *list.Model, w, h int) {
	d.w, d.h = w, h
	FitList(l, w, h)
}
