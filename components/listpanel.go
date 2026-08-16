package components

import (
	"sync/atomic"
	"time"

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
// returns handled=false and the host ModularScreen's pop fires instead.
type ListPanel struct {
	list     list.Model
	focused  bool
	onSelect func(*core.Shared, list.Item) core.Action
	onKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	help     []key.Binding

	title    string // kept for the border legend (the list's own title bar is off when bordered)
	bordered bool   // ListPanelOpts.Border: draw the shared frame
	width    int    // outer cell width, for the frame's inner run
	itemRows int    // delegate height + spacing; drives mouse and overlay geometry

	// Marquee state, live only on a CompactListPanel (see startMarquee). marquee is the
	// cell offset core.CompactDelegate reads through a pointer; marqueeID identifies this
	// panel's own ticks; hold is the frames left in the current end-dwell; lastSel detects
	// a cursor move so each row starts from its left edge.
	marquee   int
	marqueeID int64 // 0 ⇒ this panel doesn't marquee (every non-compact ListPanel)
	ticking   bool
	hold      int
	lastSel   int
}

var _ Panel = (*ListPanel)(nil)
var _ Focusable = (*ListPanel)(nil)
var _ PanelUpdater = (*ListPanel)(nil)
var _ Capturing = (*ListPanel)(nil)
var _ PanelHelper = (*ListPanel)(nil)
var _ panelInitializer = (*ListPanel)(nil)
var _ FocusNotifier = (*ListPanel)(nil)

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
	return newListPanel(core.NewSelectList, items, title, opts, listItemRows)
}

// CompactListPanel is the single-line ListPanel variant. It preserves the panel's
// selection, filtering, help, mouse, wrapping, border, and pagination behavior.
type CompactListPanel struct{ *ListPanel }

var _ Panel = (*CompactListPanel)(nil)

// NewCompactListPanel builds a sidebar whose items implement core.SuffixItem. The selected
// row marquees whenever its name plus suffix is wider than the column — see startMarquee.
func NewCompactListPanel(items []list.Item, title string, opts ListPanelOpts) *CompactListPanel {
	p := newListPanel(core.NewCompactList, items, title, opts, compactListItemRows)
	p.startMarquee()
	return &CompactListPanel{p}
}

func newListPanel(build func([]list.Item, string, ...key.Binding) list.Model, items []list.Item, title string, opts ListPanelOpts, itemRows int) *ListPanel {
	listTitle := title
	if opts.Border {
		listTitle = "" // the title moves to the border legend; an empty one hides the bar
	}
	return &ListPanel{
		list:     build(items, listTitle, opts.Help...),
		onSelect: opts.OnSelect,
		onKey:    opts.OnKey,
		help:     opts.Help,
		title:    title,
		bordered: opts.Border,
		itemRows: itemRows,
	}
}

// ---------- marquee ----------
//
// A compact row's name and suffix compete for one narrow column, and the loser used to
// vanish without so much as an ellipsis (core.CompactDelegate.Render's leftovers rule). So
// the SELECTED row slides instead: name and suffix are windowed as one string, dwelling at
// each end, until the whole thing has been read. The panel owns the clock because it is the
// only piece that knows the two things which decide whether the clock should run at all —
// whether this pane has focus, and where the cursor is.
//
// The loop is self-limiting: it re-arms only while a focused panel's selected row actually
// overflows, so it stops on its own the moment focus moves to a sibling pane or the cursor
// lands on a row that fits. Nothing here is a router or Config change — the tick rides out
// on the core.Action{Cmd} lane ModularScreen.Update already batches, and comes back through
// its non-key broadcast.
const (
	marqueeInterval = 130 * time.Millisecond
	marqueeHold     = 8 // frames of dwell at each end, ~1s
)

// marqueeIDs hands each marqueeing panel a distinct, non-zero clock id. The broadcast lane
// delivers every tick to EVERY panel, so a panel that acted on a sibling's tick would
// re-arm alongside it and the tick count would double on each pass. Atomic because a screen
// can be built inside a cmd, off the tea goroutine.
var marqueeIDs atomic.Int64

type marqueeTickMsg struct{ id int64 }

func marqueeTick(id int64) tea.Cmd {
	return tea.Tick(marqueeInterval, func(time.Time) tea.Msg { return marqueeTickMsg{id: id} })
}

// startMarquee opts this panel in and points the delegate at the offset the tick advances.
// It replaces the delegate rather than taking a constructor argument, which keeps
// core.NewCompactList's signature (and every existing caller's output) untouched.
func (p *ListPanel) startMarquee() {
	p.marqueeID = marqueeIDs.Add(1)
	p.hold = marqueeHold
	p.list.SetDelegate(core.CompactDelegate{Offset: &p.marquee})
}

// marqueeOverflow reports the last useful offset for the selected row, and false when the
// row fits, the list is filtered, or this panel doesn't marquee at all. It measures against
// core.CompactTextWidth of the list's own width — the same number Render fits the row into,
// so the panel driving the offset and the delegate consuming it can't disagree about which
// rows move.
func (p *ListPanel) marqueeOverflow() (int, bool) {
	if p.marqueeID == 0 || p.list.FilterState() != list.Unfiltered {
		return 0, false
	}
	i, ok := p.list.SelectedItem().(core.SuffixItem)
	if !ok {
		return 0, false
	}
	tw := core.CompactTextWidth(p.list.Width())
	row, over := core.CompactMarquee(i, tw)
	if !over {
		return 0, false
	}
	return row.Width() - tw, true
}

// marqueeStep advances one frame: burn a dwell if one is pending, else step one cell,
// starting a dwell on arrival at the tail and snapping back to the left edge after it.
func (p *ListPanel) marqueeStep(max int) {
	if p.hold > 0 {
		p.hold--
		return
	}
	if p.marquee >= max {
		p.marquee, p.hold = 0, marqueeHold
		return
	}
	if p.marquee++; p.marquee >= max {
		p.hold = marqueeHold
	}
}

// marqueeTicked handles a tick. A tick from another panel's clock is consumed and dropped;
// our own either advances and re-arms, or — the panel having lost focus or the cursor having
// moved to a row that fits — stops the loop and resets the row to its left edge.
func (p *ListPanel) marqueeTicked(t marqueeTickMsg) core.Action {
	if t.id != p.marqueeID {
		return core.Action{}
	}
	max, ok := p.marqueeOverflow()
	if !ok || !p.focused {
		p.ticking, p.marquee, p.hold = false, 0, marqueeHold
		return core.Action{}
	}
	p.marqueeStep(max)
	return core.Async(marqueeTick(p.marqueeID))
}

// marqueeStart arms the loop if it is idle and there is something to scroll, and reports
// the tick to emit (nil when the marquee doesn't apply, is already running, the panel is
// unfocused, or the selected row fits).
func (p *ListPanel) marqueeStart() tea.Cmd {
	if p.marqueeID == 0 || p.ticking || !p.focused {
		return nil
	}
	if _, ok := p.marqueeOverflow(); !ok {
		return nil
	}
	p.ticking = true
	return marqueeTick(p.marqueeID)
}

// OnFocus implements FocusNotifier: taking focus starts the marquee immediately. Without
// it the pane-navigation key that granted focus is consumed by the host and never reaches
// here, so tabbing into a sidebar and pressing nothing left the row sitting still until
// some unrelated message happened along.
func (p *ListPanel) OnFocus() tea.Cmd { return p.marqueeStart() }

// marqueeArm re-syncs on any other message: a cursor move resets the row to its left edge,
// and an idle-but-eligible marquee gets started. OnFocus covers the focus transition, but
// this stays the safety net for the paths that carry no cmd — above all the host's
// SetFocused, which returns the keys from the output pane through core.FocusableScreen.
func (p *ListPanel) marqueeArm(act core.Action) core.Action {
	if p.marqueeID == 0 {
		return act
	}
	if sel := p.list.Index(); sel != p.lastSel {
		p.lastSel, p.marquee, p.hold = sel, 0, marqueeHold
	}
	act.Cmd = tea.Batch(act.Cmd, p.marqueeStart())
	return act
}

// Init implements panelInitializer, arming the very first tick. Unconditionally, unlike
// every later arm: at Init the panel has not been sized, so there is no width to measure a
// row against. By the time that tick lands SetSize has run, and marqueeTicked's own check
// either keeps the loop going or ends it there.
func (p *ListPanel) Init(*core.Shared) tea.Cmd {
	// The ticking guard makes a second Init a no-op: panels outlive the ModularScreen that
	// holds them (gote rebuilds its layout on every sidebar toggle), and a second clock on
	// one panel would share the first's id, so each pass would arm two ticks, then four.
	if p.marqueeID == 0 || p.ticking {
		return nil
	}
	p.ticking = true
	return marqueeTick(p.marqueeID)
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
// routes every keystroke here (bar its reserved pane keys), so the filter
// input never loses a character to the router's global single-key shortcuts.
func (p *ListPanel) Capturing() bool { return p.list.FilterState() == list.Filtering }

// UpdatePanel runs the picker dispatch (listDispatch) with the one host-owned key
// carved out first: Back is the screen's pop, so it is not consumed here (contrast
// PickerScreen, which binds Back to Pop itself). While filtering, esc stays —
// listDispatch's filtering branch feeds it to the list, which cancels the filter.
// tab needs no carve-out now that the host owns no such key: unfiltered the list
// binds nothing to it, and while filtering bubbles takes it as "accept the filter".
// The wheel only moves the cursor while
// focused: the host focuses the panel under the cursor before forwarding a
// press, and anything that still arrives unfocused (a broadcast) must not roll
// an unfocused sidebar.
func (p *ListPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if t, ok := msg.(marqueeTickMsg); ok {
		return p.marqueeTicked(t), true
	}
	if _, ok := msg.(tea.MouseMsg); ok && !p.focused {
		return core.Action{}, false
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		if k := km.String(); core.MatchKey(k, core.Keys.Back) && !p.Capturing() {
			return core.Action{}, false
		}
	}
	onSelect := func() core.Action {
		if p.onSelect != nil {
			return p.onSelect(sh, p.list.SelectedItem())
		}
		// No panel-level handler: let a self-dispatching Item pick itself.
		if pick := itemPick(p.list.SelectedItem()); pick != nil {
			return pick(sh)
		}
		return core.Action{}
	}
	onKey := func(k string) (core.Action, bool) {
		if p.onKey != nil {
			return p.onKey(sh, k, p.list.SelectedItem())
		}
		if keys := itemKeys(p.list.SelectedItem()); keys != nil {
			return keys(sh, k)
		}
		return core.Action{}, false
	}
	// ModularScreen has already translated a mouse event into panel-local
	// coordinates. A bordered panel still has its own top frame row before the
	// inner list, so remove that row as listDispatchRows does its list-local math.
	mouseYOff := 0
	if p.bordered {
		mouseYOff = 1
	}
	return p.marqueeArm(listDispatchRows(sh, &p.list, msg, mouseYOff, p.itemRows, onSelect, onKey)), true
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
