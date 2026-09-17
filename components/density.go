package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// Density is the row-density state of one list: which of the two delegates is live, and
// the allocation to re-fit against when that changes. It exists because the flip is four
// things that must agree — the delegate, the pagination re-fit, the row height mouse
// hit-testing divides by, and the fact that a theme rebuild has to restore the CURRENT
// density rather than the default one — and a screen that re-derived them inline would
// get one of them wrong.
//
// It is for a screen owning a bare list.Model: a tab root driven by RootUpdate, or
// PickerScreen, which embeds one. A sidebar does not use it — ListPanel's two densities
// differ in more than the delegate (own filter line, inline pagination, marquee clock), so
// FilePanel rebuilds the panel instead; see FilePanel.SetCompact.
//
// The zero value is the default three-row density, unsized. Every method is a no-op or a
// plain read until the first Fit, so a screen can hold one and wire it up at leisure.
type Density struct {
	compact bool
	w, h    int
}

// Compact reports which density is live.
func (d *Density) Compact() bool { return d.compact }

// Delegate is the row renderer for the live density. The compact one carries no Offset:
// the marquee is driven by a ListPanel's tick loop, and a screen owning a bare list has no
// such clock, so an overflowing row is statically truncated.
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

// SetCompact swaps l's delegate when the density actually changes, and reports whether it
// did. This is a delegate swap, not a rebuild, so the cursor, the page AND an applied
// /-filter all survive it: bubbles' SetDelegate re-paginates around the saved Index(). The
// stored allocation is replayed afterwards because PerPage is derived from the new
// delegate's row height; before the first Fit there is none to replay and the caller's own
// sizing does it.
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

// Restyle rebuilds the LIVE density's delegate, for a screen reacting to a theme change.
// Both delegates cache theme colors at construction, so a screen that reached for
// core.NewDelegate() directly would repaint correctly and silently undo the user's density.
func (d *Density) Restyle(l *list.Model) {
	l.SetDelegate(d.Delegate())
	core.StyleList(l)
}

// Fit records the allocation and sizes l through FitList. Callers use it in place of
// list.SetSize: a compact delegate has zero spacing, which makes bubbles pad the pagination
// row, and crossing the one-page boundary can leave a single SetSize a row short of a fixed
// point. Harmless at the default density, where the first pass already converges.
func (d *Density) Fit(l *list.Model, w, h int) {
	d.w, d.h = w, h
	FitList(l, w, h)
}
