package components

import (
	"github.com/brohd11/bubblestack/config"
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// ThemePicker lists the registered themes; selecting one applies it live via
// core.ApplyTheme (which repaints the chrome immediately and rebuilds every tab root so
// each re-bakes its list/delegate styles from the new palette) and persists it to
// ~/.bubblestack/config.yml, the framework-wide store every bubblestack app reads at
// startup — so the choice follows the user across tools. The picker stays open and
// recolors itself on each pick.
//
// This is the one theme picker for every consumer: previously gdaddon and repoview each
// carried a near-identical copy wired to their own config.
func ThemePicker() core.Screen {
	active := core.CurrentTheme()
	var items []list.Item
	initialIndex := 0
	for i, name := range core.ThemeNames() {
		name := name // capture per row
		if name == active {
			initialIndex = i
		}
		desc := ""
		if name == active {
			desc = "active"
		}
		items = append(items, Item{
			Name: name,
			Desc: desc,
			Pick: func(sh *core.Shared) core.Action {
				// Persist the choice so it loads next startup (here and in every other
				// bubblestack app); a write failure must never block the live switch, so
				// the error is dropped.
				_ = config.SaveTheme(name)
				return core.Seq(
					core.ApplyTheme(name),
					core.Replace(ThemePicker()),
				)
			},
		})
	}
	return NewPicker(items, PickerOpts{
		Crumb:        "Theme",
		InitialIndex: initialIndex,
	})
}
