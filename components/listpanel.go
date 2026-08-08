package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// ListPanel is a picker-style list.Model packaged as a ModularScreen panel — the
// sidebar half of a list-plus-detail layout. It is driven by the same
// listDispatch skeleton as PickerScreen, so enter picks (the opts hook, else a
// self-dispatching Item), the wheel moves the cursor, WrapNav wraps at the ends,
// and /-filtering all behave exactly as they do on a full picker screen. The one
// deliberate difference is Back: a PickerScreen binds esc to Pop because it IS
// the screen, while a ListPanel shares its screen with sibling panels — so esc
// (and shift+tab) return handled=false and the host ModularScreen's fallbacks
// (pop, pane cycle) fire instead.
type ListPanel struct {
	list     list.Model
	focused  bool
	onSelect func(*core.Shared, list.Item) core.Action
	onKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	help     []key.Binding
}

var _ Panel = (*ListPanel)(nil)
var _ Focusable = (*ListPanel)(nil)
var _ PanelUpdater = (*ListPanel)(nil)
var _ Capturing = (*ListPanel)(nil)
var _ PanelHelper = (*ListPanel)(nil)

// ListPanelOpts mirrors the PickerOpts hooks a sidebar list needs: OnSelect runs
// on enter (default: a self-dispatching Item picks itself), OnKey claims extra
// row keys before WrapNav, and Help adds help-bar bindings shown while the panel
// is focused.
type ListPanelOpts struct {
	OnSelect func(*core.Shared, list.Item) core.Action
	OnKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	Help     []key.Binding
}

// NewListPanel builds a sidebar list with the shared select-list styling.
func NewListPanel(items []list.Item, title string, opts ListPanelOpts) *ListPanel {
	return &ListPanel{
		list:     core.NewSelectList(items, title, opts.Help...),
		onSelect: opts.OnSelect,
		onKey:    opts.OnKey,
		help:     opts.Help,
	}
}

func (p *ListPanel) Focus()        { p.focused = true }
func (p *ListPanel) Blur()         { p.focused = false }
func (p *ListPanel) Focused() bool { return p.focused }

// SetItems replaces the rows (e.g. a refresh after the detail panel reloads).
func (p *ListPanel) SetItems(items []list.Item) { p.list.SetItems(items) }

// List exposes the underlying list model for the read access the panel API
// doesn't cover (SelectedItem, Index, FilterState).
func (p *ListPanel) List() *list.Model { return &p.list }

// Capturing reports an active /-filter: while filtering, the host ModularScreen
// routes every keystroke here regardless of focus, so the filter input never
// loses a character to the pane cycle or the router's global keys.
func (p *ListPanel) Capturing() bool { return p.list.FilterState() == list.Filtering }

// UpdatePanel runs the picker dispatch (listDispatch) with the two host-owned
// keys carved out first: Back is the screen's pop and shift+tab its pane cycle,
// so neither is consumed here (contrast PickerScreen, which binds Back to Pop
// itself). While filtering, esc stays — listDispatch's filtering branch feeds it
// to the list, which cancels the filter. The wheel only moves the cursor while
// focused: mouse msgs are broadcast to every panel, and a wheel that rolled an
// unfocused sidebar would read as a bug.
func (p *ListPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if _, ok := msg.(tea.MouseMsg); ok && !p.focused {
		return core.Action{}, false
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		k := km.String()
		// shift+tab has no core.Keys binding (it is ModularScreen's own key), so
		// it is matched as a raw string — the one sanctioned exception.
		if k == "shift+tab" || (core.MatchKey(k, core.Keys.Back) && !p.Capturing()) {
			return core.Action{}, false
		}
	}
	onSelect := func() core.Action {
		if p.onSelect != nil {
			return p.onSelect(sh, p.list.SelectedItem())
		}
		// No panel-level handler: let a self-dispatching Item pick itself.
		if it, ok := p.list.SelectedItem().(Item); ok && it.Pick != nil {
			return it.Pick(sh)
		}
		return core.Action{}
	}
	onKey := func(k string) (core.Action, bool) {
		if p.onKey != nil {
			return p.onKey(sh, k, p.list.SelectedItem())
		}
		if it, ok := p.list.SelectedItem().(Item); ok && it.Keys != nil {
			return it.Keys(sh, k)
		}
		return core.Action{}, false
	}
	return listDispatch(sh, &p.list, msg, onSelect, onKey), true
}

// PanelHelp contributes the list's select/filter hints plus any caller-supplied
// bindings to the host's help bar while this panel is focused.
func (p *ListPanel) PanelHelp() []key.Binding {
	return append([]key.Binding{
		core.Hint("select", core.Keys.Select),
		core.Hint("filter", p.list.KeyMap.Filter),
	}, p.help...)
}

// View renders the list itself. The panel draws no border of its own, so the
// focused arg only answers the Panel contract — the list cursor already marks
// which panel is live.
func (p *ListPanel) View(bool) string { return p.list.View() }

// SetSize takes the outer cell dims; with no border of its own, the list gets
// them verbatim.
func (p *ListPanel) SetSize(width, height int) { p.list.SetSize(width, height) }
