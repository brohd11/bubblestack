package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// The standard Actions menu an app opens with "a": theme, docs, self-update, refresh.

// NewActionsMenu builds the Actions picker: Theme, Docs (when docs has pages), any app
// rows, Update <app>, and Refresh (the app's own rescan, described by refreshDesc). It is
// a PopStop hub its sub-flows return to.
func NewActionsMenu(hooks SelfUpdateHooks, refreshDesc string, refresh func(*core.Shared) core.Action, docs []DocPage, extra ...list.Item) *PickerScreen {
	items := []list.Item{
		Item{
			Name: "◑ Theme",
			Desc: "switch the color theme",
			Pick: func(sh *core.Shared) core.Action { return core.Push(ThemePicker()) },
		},
	}
	if docsRow, ok := DocsItem(docs); ok {
		items = append(items, docsRow)
	}
	items = append(items, extra...)
	items = append(items,
		Item{
			Name: "⟲ Update " + hooks.AppName,
			Desc: "check for a newer " + hooks.AppName + " release and install it",
			Pick: func(sh *core.Shared) core.Action { return core.Push(NewSelfUpdateLoading(hooks)) },
		},
		Item{
			Name: "⟳ Refresh",
			Desc: refreshDesc,
			Pick: func(sh *core.Shared) core.Action { return refresh(sh) },
		},
	)
	return NewPicker(items, PickerOpts{
		Title:   "Actions",
		Crumb:   "Actions",
		PopStop: true,
	})
}
