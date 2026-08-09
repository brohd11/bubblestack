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
// (and tab) return handled=false and the host ModularScreen's fallbacks
// (pop, pane cycle) fire instead.
type ListPanel struct {
	list     list.Model
	focused  bool
	onSelect func(*core.Shared, list.Item) core.Action
	onKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	help     []key.Binding

	title    string // kept for the border legend (the list's own title bar is off when bordered)
	bordered bool   // ListPanelOpts.Border: draw the shared frame
	width    int    // outer cell width, for the frame's inner run
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
//
// Border opts the panel into the framework's framed look (the one ScrollContainer
// and a bordered EditorScreen wear): the list's own title bar is dropped and the
// title becomes the frame's top-edge legend, tinted by focus — so a sidebar denotes
// which pane is live even when its cursor doesn't. It is a plain option rather than
// a core.Borderer implementation because a panel only ever lives inside a
// ModularScreen: there is no standalone case for the embedder to distinguish.
// Default off, so existing sidebars render unchanged.
type ListPanelOpts struct {
	OnSelect func(*core.Shared, list.Item) core.Action
	OnKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	Help     []key.Binding
	Border   bool
}

// NewListPanel builds a sidebar list with the shared select-list styling.
func NewListPanel(items []list.Item, title string, opts ListPanelOpts) *ListPanel {
	listTitle := title
	if opts.Border {
		listTitle = "" // the title moves to the border legend; an empty one hides the bar
	}
	return &ListPanel{
		list:     core.NewSelectList(items, listTitle, opts.Help...),
		onSelect: opts.OnSelect,
		onKey:    opts.OnKey,
		help:     opts.Help,
		title:    title,
		bordered: opts.Border,
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
// keys carved out first: Back is the screen's pop and tab its pane cycle,
// so neither is consumed here (contrast PickerScreen, which binds Back to Pop
// itself). While filtering, esc stays — listDispatch's filtering branch feeds it
// to the list, which cancels the filter. The wheel only moves the cursor while
// focused: the host focuses the panel under the cursor before forwarding a
// press, and anything that still arrives unfocused (a broadcast) must not roll
// an unfocused sidebar.
func (p *ListPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if _, ok := msg.(tea.MouseMsg); ok && !p.focused {
		return core.Action{}, false
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		k := km.String()
		// tab has no core.Keys binding (it is ModularScreen's own key), so
		// it is matched as a raw string — the one sanctioned exception.
		if k == "tab" || (core.MatchKey(k, core.Keys.Back) && !p.Capturing()) {
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
	return listDispatch(sh, &p.list, msg, 0, onSelect, onKey), true
}

// PanelHelp contributes the list's select/filter hints plus any caller-supplied
// bindings to the host's help bar while this panel is focused.
func (p *ListPanel) PanelHelp() []key.Binding {
	return append([]key.Binding{
		core.Hint("select", core.Keys.Select),
		core.Hint("filter", p.list.KeyMap.Filter),
	}, p.help...)
}

// View renders the list, framed when ListPanelOpts.Border asked for it — then the
// focused arg tints the frame and its title legend. Unbordered (the default) the
// panel draws nothing of its own and the arg only answers the Panel contract: the
// list cursor already marks which panel is live.
func (p *ListPanel) View(focused bool) string {
	if !p.bordered {
		return p.list.View()
	}
	return frame(p.title, p.list.View(), p.innerWidth(), focused)
}

// SetSize takes the outer cell dims; the list gets them verbatim unless the panel
// is bordered, in which case the frame comes off both axes first.
func (p *ListPanel) SetSize(width, height int) {
	p.width = width
	if !p.bordered {
		p.list.SetSize(width, height)
		return
	}
	if height -= 2; height < 1 {
		height = 1
	}
	p.list.SetSize(p.innerWidth(), height)
}

// innerWidth is the run between the frame's corners: the outer width minus the two
// side borders.
func (p *ListPanel) innerWidth() int {
	if w := p.width - 2; w > 1 {
		return w
	}
	return 1
}
