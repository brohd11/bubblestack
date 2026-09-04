package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// pickerScreen is the reusable list picker: a styled list that pops on esc/q,
// runs onSelect on enter, and optionally handles extra keys (e.g. archive on
// 'a'). It backs the asset/branch submenu and is the building block for any new
// flow that needs "pick one of these, then do X".
//
// Configure it with newPicker. The closures return the navigation command to run
// (push/pop/…); the picker stays on screen itself, so they never need a reference
// back to it.
type PickerScreen struct {
	list       list.Model
	crumb      string // breadcrumb segment; defaults to the list title when ""
	crumbShort string
	dir        string // directory this picker concerns; enables the global Terminal key (DirLocator)
	OnSelect   func(*core.Shared, list.Item) core.Action
	OnKey      func(*core.Shared, string, list.Item) (core.Action, bool)
	refresh    func(*core.Shared, any) ([]list.Item, bool)
	popStop    bool
}

// pickerOpts configures a pickerScreen. onKey is optional; when it reports
// handled=true the key is consumed (and its command, if any, run), otherwise the
// key falls through to the list.
type PickerOpts struct {
	Title      string
	Crumb      string        // optional breadcrumb segment; defaults to Title
	CrumbShort string        // optional short breadcrumb segment; defaults to Crumb/Title
	Help       []key.Binding // extra help/hint bindings shown in the list help
	OnSelect   func(*core.Shared, list.Item) core.Action
	OnKey      func(*core.Shared, string, list.Item) (core.Action, bool)
	// Refresh, when set, makes the picker a Receiver: on a PropagateAll broadcast it
	// is called with the payload; returning ok=true rebuilds the rows from items.
	Refresh      func(sh *core.Shared, payload any) (items []list.Item, ok bool)
	PopStop      bool   // mark this picker as a PopTo boundary (a command hub)
	InitialIndex int    // cursor starts here; 0 = first item (default)
	Dir          string // directory this picker concerns; enables the global Terminal key (DirLocator)
}

var _ core.Filterer = (*PickerScreen)(nil)
var _ core.PopStopper = (*PickerScreen)(nil)
var _ core.Crumber = (*PickerScreen)(nil)
var _ core.Receiver = (*PickerScreen)(nil)
var _ core.DirLocator = (*PickerScreen)(nil)

func NewPicker(items []list.Item, opts PickerOpts) *PickerScreen {
	help := opts.Help
	if opts.Dir != "" {
		// A picker with a directory is a DirLocator, so the global terminal/open-dir keys fire
		// on it — advertise them in its (?) help alongside any caller-supplied bindings.
		help = append(append([]key.Binding{}, opts.Help...), core.DirKeyHints()...)
	}
	s := &PickerScreen{
		list:       core.NewSelectList(items, opts.Title, help...),
		crumb:      opts.Crumb,
		crumbShort: opts.CrumbShort,
		dir:        opts.Dir,
		OnSelect:   opts.OnSelect,
		OnKey:      opts.OnKey,
		refresh:    opts.Refresh,
		popStop:    opts.PopStop,
	}
	if opts.InitialIndex > 0 {
		s.list.Select(opts.InitialIndex)
	}
	return s
}

func (s *PickerScreen) PopStop() bool { return s.popStop }

// LocateDir reports the directory this picker concerns (PickerOpts.Dir), so the global
// Terminal key opens a terminal there. Empty dir ⇒ no locator (the key falls through).
func (s *PickerScreen) LocateDir() (string, bool) { return s.dir, s.dir != "" }

// Receive restyles the live list on a theme broadcast: bubbles caches both its
// delegate and list styles, so changing core's palette alone does not repaint a
// picker already on the navigation stack. It also lets a picker rebuild its rows
// on any broadcast claimed by a configured Refresh closure.
func (s *PickerScreen) Receive(sh *core.Shared, payload any) core.Action {
	if _, ok := payload.(core.MsgThemeChanged); ok {
		s.list.SetDelegate(core.NewDelegate())
		core.StyleList(&s.list)
	}
	if s.refresh != nil {
		if items, ok := s.refresh(sh, payload); ok {
			SetListItems(&s.list, items)
		}
	}
	return core.Action{}
}

// CrumbLabel contributes the picker's breadcrumb segment: the short form when set,
// else the explicit crumb, else the list title (the default — crumb and title agree).
func (s *PickerScreen) CrumbLabel(short bool) string {
	return CrumbSegment(short, s.crumbShort, s.crumb, s.list.Title)
}

func (s *PickerScreen) Init(*core.Shared) tea.Cmd { return nil }

func (s *PickerScreen) Filtering() bool { return s.list.FilterState() == list.Filtering }

func (s *PickerScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	onSelect := func() core.Action {
		if s.OnSelect != nil {
			return s.OnSelect(sh, s.list.SelectedItem())
		}
		// No screen-level handler: let a self-dispatching Item pick itself.
		if pick := itemPick(s.list.SelectedItem()); pick != nil {
			return pick(sh)
		}
		return core.Action{}
	}
	onKey := func(k string) (core.Action, bool) {
		if core.MatchKey(k, core.Keys.Back) {
			return core.Pop(), true
		}
		if s.OnKey != nil {
			// A screen-level OnKey owns the row's extra keys wholesale: when it
			// doesn't handle one, the key falls to WrapNav, not to the Item's Keys.
			return s.OnKey(sh, k, s.list.SelectedItem())
		}
		if keys := itemKeys(s.list.SelectedItem()); keys != nil {
			return keys(sh, k)
		}
		return core.Action{}, false
	}
	return s, listDispatch(sh, &s.list, msg, sh.BodyY(), onSelect, onKey)
}

func (s *PickerScreen) View(*core.Shared) string     { return core.RenderList(s.list) }
func (s *PickerScreen) HelpView(*core.Shared) string { return core.ShortHelp(s.list, core.HelpMinimal) }

func (s *PickerScreen) SetSize(sh *core.Shared, width, bodyHeight int) {
	s.list.SetSize(width, bodyHeight)
}
