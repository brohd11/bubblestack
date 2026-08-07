package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
)

// This file holds the standard Actions menu: the small picker an app opens with "a"
// for the chores every bubblestack app shares — switch the theme, self-update, refresh.
// It was extracted from repoview's actions.go when golaunch grew the same menu; keeping
// it here means a new app gets the whole sheet (and the shared self-update flow behind
// it) for one function call.

// NewActionsMenu builds the standard Actions picker: ◑ Theme, then any app-specific
// rows (a docs index, say), then ⟲ Update <app> and ⟳ Refresh. refreshDesc/refresh are
// the app's own rescan row (what its global Refresh key fires, described in its terms).
// PopStop marks the menu as the hub its sub-flows (the theme picker, the update flow)
// return to.
func NewActionsMenu(hooks SelfUpdateHooks, refreshDesc string, refresh func(*core.Shared) core.Action, extra ...list.Item) *PickerScreen {
	items := []list.Item{
		Item{
			Name: "◑ Theme",
			Desc: "switch the color theme",
			Pick: func(sh *core.Shared) core.Action { return core.Push(ThemePicker()) },
		},
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
