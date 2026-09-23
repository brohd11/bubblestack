package components

import (
	"github.com/brohd11/bubblestack/config"
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
)

// ThemePicker lists the themes; picking one applies it live (core.ApplyTheme) and saves
// it to ~/.bubblestack/config.yml, which every bubblestack app reads at startup. The
// picker stays open.
func ThemePicker() core.Screen {
	active := core.CurrentTheme()
	var items []list.Item
	initialIndex := 0
	for i, name := range core.ThemeNames() {
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
				// Persist for next startup; a failed write must not block the live switch.
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
