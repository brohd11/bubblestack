package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// PickerScreen is the reusable list picker: a styled list that pops on back,
// runs OnSelect on enter, and optionally handles extra keys (e.g. archive on
// 'a'). It backs the asset/branch submenu and is the building block for any new
// flow that needs "pick one of these, then do X".
//
// Configure it with NewPicker. The closures return the navigation command to run
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

	// dens owns the delegate/pagination/row-height agreement. RootListScreen can
	// attach an app-owned preference so root reconstruction retains the choice.
	dens         Density
	densityKey   key.Binding
	compactState *bool
}

// PickerOpts configures a PickerScreen. OnKey is optional; when it reports
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

	// Compact picks the starting row density: the one-line core.CompactDelegate instead of
	// the default three-row title/description/spacer. DensityKey flips it live;
	// an unspecified binding defaults to DefaultDensityKey.
	//
	// Rows need no new method: in compact form an item's Description becomes the muted
	// suffix after its title (core.compactText). A row with a real SuffixText, or with a
	// prefix, mark or color, keeps all of those — see core.SuffixItem.
	//
	// DisableDensityToggle suppresses the built-in key and its help entry, even
	// with a custom DensityKey. Programmatic density methods remain available.
	Compact              bool
	DensityKey           key.Binding
	DisableDensityToggle bool
}

// DefaultDensityKey is the framework's row-density chord, so every picker (and every
// FilePanelOpts.DensityKey) that wants one spells the same key. D rather than the more
// obvious C: core.Keys.Clear owns "C" and the router consumes it in globalKey before any
// screen is offered the keystroke.
var DefaultDensityKey = key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "density"))

var _ core.Filterer = (*PickerScreen)(nil)
var _ core.PopStopper = (*PickerScreen)(nil)
var _ core.Crumber = (*PickerScreen)(nil)
var _ core.Receiver = (*PickerScreen)(nil)
var _ core.DirLocator = (*PickerScreen)(nil)

func NewPicker(items []list.Item, opts PickerOpts) *PickerScreen {
	return newPicker(items, opts, false)
}

func newPicker(items []list.Item, opts PickerOpts, root bool) *PickerScreen {
	if opts.DisableDensityToggle {
		opts.DensityKey = key.Binding{}
	} else if len(opts.DensityKey.Keys()) == 0 {
		opts.DensityKey = DefaultDensityKey
	}
	help := opts.Help
	if opts.Dir != "" {
		// A picker with a directory is a DirLocator, so the global terminal/open-dir keys fire
		// on it — advertise them in its (?) help alongside any caller-supplied bindings.
		help = append(append([]key.Binding{}, opts.Help...), core.DirKeyHints()...)
	}
	if len(opts.DensityKey.Keys()) > 0 {
		// A (?)-menu entry, never a bar one: ShortHelp's short branch is a fixed literal by
		// design, and a density flip is a command. newSelectList files extras into both
		// AdditionalShortHelpKeys and AdditionalFullHelpKeys; only the full one is read here.
		help = append(append([]key.Binding{}, help...), core.Hint("density", opts.DensityKey))
	}
	build := core.NewSelectList
	if opts.Compact {
		build = core.NewCompactList
	}
	s := &PickerScreen{
		list:       build(items, opts.Title, help...),
		crumb:      opts.Crumb,
		crumbShort: opts.CrumbShort,
		dir:        opts.Dir,
		OnSelect:   opts.OnSelect,
		OnKey:      opts.OnKey,
		refresh:    opts.Refresh,
		popStop:    opts.PopStop,
		dens:       Density{compact: opts.Compact},
		densityKey: opts.DensityKey,
	}
	if opts.InitialIndex > 0 {
		s.list.Select(opts.InitialIndex)
	}
	if root {
		// Roots have no Back action. Keep the same command hints as a picker,
		// but omit its built-in back hint and respect the active select binding.
		s.list.AdditionalFullHelpKeys = func() []key.Binding {
			return append([]key.Binding{core.FullHint("select", core.Keys.Select)}, help...)
		}
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
		// Restyle, not SetDelegate(core.NewDelegate()): it rebuilds the CURRENT density's
		// delegate, so a theme switch cannot silently undo a density the user had flipped.
		s.dens.Restyle(&s.list)
	}
	if s.refresh != nil {
		if items, ok := s.refresh(sh, payload); ok {
			s.SetItems(items)
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
	return s, s.update(sh, msg, false)
}

// update shares list dispatch with RootListScreen. Only navigation and hook
// fallback differ: roots do not pop, and their unhandled keys reach row handlers.
func (s *PickerScreen) update(sh *core.Shared, msg tea.Msg, root bool) core.Action {
	onSelect := func() core.Action {
		if s.OnSelect != nil {
			if root {
				sh.ClearStatus()
			}
			return s.OnSelect(sh, s.list.SelectedItem())
		}
		// No screen-level handler: let a self-dispatching Item pick itself.
		if pick := itemPick(s.list.SelectedItem()); pick != nil {
			if root {
				sh.ClearStatus()
			}
			return pick(sh)
		}
		return core.Action{}
	}
	onKey := func(k string) (core.Action, bool) {
		if !root && core.MatchKey(k, core.Keys.Back) {
			return core.Pop(), true
		}
		// The density flip is matched AFTER the screen's own hooks, so a consumer that
		// binds the same key keeps it. A disabled toggle has an empty binding.
		density := func() (core.Action, bool) {
			if !core.MatchKey(k, s.densityKey) {
				return core.Action{}, false
			}
			s.ToggleDensity()
			return core.Action{}, true
		}
		if s.OnKey != nil {
			// Preserve the picker's exclusive OnKey contract. Root hooks instead
			// augment the row's keys (e.g. sort alongside per-row Git shortcuts).
			if act, handled := s.OnKey(sh, k, s.list.SelectedItem()); handled {
				return act, true
			}
			if !root {
				return density()
			}
		}
		if keys := itemKeys(s.list.SelectedItem()); keys != nil {
			if act, handled := keys(sh, k); handled {
				return act, true
			}
		}
		return density()
	}
	// listDispatchRows rather than listDispatch: the row height a click is divided by is
	// the live delegate's, and a compact list that kept the three-row constant would select
	// a row three places off.
	return listDispatchRows(sh, &s.list, msg, sh.BodyY(), s.dens.ItemRows(), onSelect, onKey, nil)
}

func (s *PickerScreen) View(*core.Shared) string     { return core.RenderList(s.list) }
func (s *PickerScreen) HelpView(*core.Shared) string { return core.ShortHelp(s.list, core.HelpMinimal) }

func (s *PickerScreen) SetSize(sh *core.Shared, width, bodyHeight int) {
	if s.compactState != nil {
		s.dens.SetCompact(&s.list, *s.compactState)
	}
	s.dens.Fit(&s.list, width, bodyHeight)
}

// List exposes the model for selection, sorting and title customization. Use
// SetItems for row replacement so an active filter is recomputed synchronously.
func (s *PickerScreen) List() *list.Model { return &s.list }

// SetItems replaces rows while retaining an active filter and fitting pagination.
func (s *PickerScreen) SetItems(items []list.Item) {
	SetListItems(&s.list, items)
	if s.dens.w > 0 {
		s.dens.Fit(&s.list, s.dens.w, s.dens.h)
	}
}

// ---------- density ----------

// Compact reports the current row density.
func (s *PickerScreen) Compact() bool { return s.dens.Compact() }

// ToggleDensity flips between the one-row and three-row list.
func (s *PickerScreen) ToggleDensity() {
	compact := s.Compact()
	if s.compactState != nil {
		compact = *s.compactState
	}
	s.SetCompact(!compact)
}

// SetCompact sets the row density, carrying the cursor, the page and an applied /-filter
// across the flip. See Density.SetCompact.
func (s *PickerScreen) SetCompact(compact bool) {
	if s.compactState != nil {
		*s.compactState = compact
	}
	s.dens.SetCompact(&s.list, compact)
}
