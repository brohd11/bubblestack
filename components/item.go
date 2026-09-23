package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// Item is a self-dispatching list row: it carries its own Pick closure, run on enter by
// a PickerScreen or tab root, so building the rows is the whole flow. A nil Pick is an
// inert row; Keys handles per-row keys.
type Item struct {
	Name, Desc, Filter string
	Pick               func(*core.Shared) core.Action // sync control msg and/or async cmd
	Keys               func(*core.Shared, string) (core.Action, bool)
}

// CompactItem is Item's single-line counterpart for NewCompactListPanel. Suffix
// is optional context rendered after Name in the muted theme color.
type CompactItem struct {
	Name, Suffix, Filter string
	Pick                 func(*core.Shared) core.Action
	Keys                 func(*core.Shared, string) (core.Action, bool)
}

func (i CompactItem) Title() string      { return i.Name }
func (i CompactItem) SuffixText() string { return i.Suffix }
func (i CompactItem) FilterValue() string {
	if i.Filter != "" {
		return i.Filter
	}
	return i.Name
}

func (i Item) Title() string       { return i.Name }
func (i Item) Description() string { return i.Desc }
func (i Item) FilterValue() string {
	if i.Filter != "" {
		return i.Filter
	}
	return i.Name
}

// EnsurePlaceholder appends one inert row when items is empty.
func EnsurePlaceholder(items []list.Item, name, desc string) []list.Item {
	if len(items) == 0 {
		items = append(items, Item{Name: name, Desc: desc})
	}
	return items
}
