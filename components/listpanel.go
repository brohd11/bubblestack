package components

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	height   int    // outer cell height, so the list can be re-sized when the filter line appears
	itemRows int    // delegate height + spacing; drives mouse and overlay geometry

	// ownFilter: this panel draws the filter line itself (SetShowFilter is off on the
	// list), so the header costs a row only while a filter is live. Compact panels only —
	// see NewCompactListPanel.
	ownFilter bool

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
	// The panel draws the filter itself (filterLine), so bubbles draws no header at all:
	// with the title off too (Border blanks it) that is the empty row a compact sidebar
	// used to carry above its first item, and the second row the filter's own bottom
	// padding used to add. SetShowFilter is presentational ONLY — the filter still runs,
	// since bubbles dispatches on filterState alone (list.Model.Update).
	p.ownFilter = true
	p.list.SetShowFilter(false)
	// bubbles forces MarginTop(1) onto pagination whenever a delegate has zero
	// spacing. Inline rendering suppresses that margin while keeping the dots; add
	// back PaginationStyle's left padding through its transform so only the blank
	// ROW disappears and the paginator stays aligned exactly where it was.
	paginationIndent := strings.Repeat(" ", p.list.Styles.PaginationStyle.GetPaddingLeft())
	p.list.Styles.PaginationStyle = p.list.Styles.PaginationStyle.
		Inline(true).
		Transform(func(s string) string { return paginationIndent + s })
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

// SetItems replaces the rows (e.g. a refresh after the detail panel reloads), keeping
// any live filter applied to the new set — see SetListItems for why that needs saying.
// The re-size covers the filter collapsing to nothing on the new rows.
func (p *ListPanel) SetItems(items []list.Item) {
	SetListItems(&p.list, items)
	p.sizeList()
}

// List exposes the underlying list model for the read access the panel API
// doesn't cover (SelectedItem, Index, FilterState).
func (p *ListPanel) List() *list.Model { return &p.list }

// Capturing reports an active /-filter: while filtering, the host ModularScreen
// routes every keystroke here (bar its reserved pane keys), so the filter
// input never loses a character to the router's global single-key shortcuts.
func (p *ListPanel) Capturing() bool { return p.list.FilterState() == list.Filtering }

// UpdatePanel runs the picker dispatch (listDispatch) with the one host-owned key
// carved out first: Back is the screen's pop, so it is not consumed here (contrast
// PickerScreen, which binds Back to Pop itself) — unless a filter is APPLIED, which
// esc clears before the pop is reached. While filtering, esc stays —
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
			// An APPLIED filter is the one thing back must clear before it pops: the
			// carve-out below hands esc to the host, so bubbles' own ClearFilter binding
			// (live in exactly this state) could never be reached, leaving a filtered
			// list with no way out of the filter but to open it and empty it by hand.
			if p.list.FilterState() == list.FilterApplied {
				p.list.ResetFilter()
				p.sizeList()
				return core.Action{}, true
			}
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
	// coordinates. The panel's own chrome — the top frame row, and the filter line
	// when one is live — still sits above the list, so remove it before
	// listDispatchRows does its list-local math.
	rows := p.filterRows()
	act := p.marqueeArm(listDispatchRows(sh, &p.list, msg, p.chromeRows(), p.itemRows, onSelect, onKey))
	// The key just handled may have opened or closed the filter, which changes how much
	// height the list has. Re-size on the transition only — every message would otherwise
	// pay for a pagination recompute.
	if p.filterRows() != rows {
		p.sizeList()
	}
	return act, true
}

// PanelHelp contributes the list's select hint plus any caller-supplied bindings to the
// host's help bar while this panel is focused.
//
// Not the filter key. What a panel contributes lands on a bar, so it is held to the bar's
// own cap (see core.ShortHelp): panel-local NAVIGATION, not the panel's command set. Select
// is nav — it is in ShortHelp's sparse literal too — while "/" is a command and belongs in
// the (?) menu, where ShortHelp's full help gives it a whole column and gote's overlay names
// it. Filtering is unaffected: the key still dispatches, and filterLine still says so on
// screen once a filter is applied, which is the part that actually needed saying.
func (p *ListPanel) PanelHelp() []key.Binding {
	return append([]key.Binding{
		core.Hint("select", core.Keys.Select),
	}, p.help...)
}

// filterLine is the panel-drawn filter row, empty when no filter is live. It exists
// because bubbles draws the filter only while it is being TYPED (list.titleView), and
// the status bar that would otherwise name an applied one is off framework-wide: an
// accepted filter left the list with rows missing and nothing on screen saying why.
//
// The look is bubbles' own, deliberately — the yellow "Filter: " prompt with the query
// in plain text is what the user already recognizes. core.RenderFilter owns that shared
// rendering for both this panel and full-screen lists.
func (p *ListPanel) filterLine() string {
	if !p.ownFilter {
		return ""
	}
	line := core.RenderFilter(&p.list)
	if line == "" {
		return ""
	}
	// The indent is the one bubbles' TitleBar carried (its left padding), so the line
	// still sits over the rows' own left pad; only the bottom padding — the blank row
	// under it — is gone. Truncated rather than left to wrap: a query wider than the
	// column would become a second row, and filterRows promises exactly one.
	w := max(p.listWidth()-filterIndent, 1)
	return lipgloss.NewStyle().PaddingLeft(filterIndent).Render(ansi.Truncate(line, w, "…"))
}

// filterIndent aligns the filter line with the rows below it.
const filterIndent = 2

// listWidth is the cell width the list itself renders at: the panel's, net of the frame.
func (p *ListPanel) listWidth() int {
	if p.bordered {
		return p.innerWidth()
	}
	return p.width
}

// filterRows is filterLine's height: the row the list body loses while a filter is live.
func (p *ListPanel) filterRows() int {
	if p.filterLine() == "" {
		return 0
	}
	return 1
}

// RowY is the panel-relative row at which visible item idx starts — the frame's top
// edge and the filter line included, where CompactListItemRow counts only rows inside
// the list. It is what an overlay anchored over a row must use: those two offsets used
// to be a constant a caller could hard-code, and the filter line makes them vary.
func (p *ListPanel) RowY(idx int) (int, bool) {
	row, ok := listItemRow(&p.list, idx, p.itemRows)
	if !ok {
		return 0, false
	}
	return row + p.chromeRows(), true
}

// chromeRows is what sits above the list inside the panel: the frame's top edge, then
// the filter line. Both the click math (mouseYOff) and RowY are built on it, so they
// cannot drift apart.
func (p *ListPanel) chromeRows() int {
	rows := p.filterRows()
	if p.bordered {
		rows++
	}
	return rows
}

// View renders the list under its filter line (when one is live), framed when
// ListPanelOpts.Border asked for it — then the focused arg tints the frame and its
// title legend. Unbordered (the default) the panel draws nothing of its own and the
// arg only answers the Panel contract: the list cursor already marks which panel is
// live.
func (p *ListPanel) View(focused bool) string {
	body := p.list.View()
	if line := p.filterLine(); line != "" {
		body = line + "\n" + body
	}
	if p.bordered {
		body = frame(p.title, body, p.innerWidth(), focused)
	}
	// A panel's rendered footprint is also ModularScreen's hit-test geometry. Keep
	// it within the allocation even if an embedded model ever over-renders again;
	// clipping the bottom preserves every panel and the router chrome above it.
	return lipgloss.NewStyle().MaxHeight(p.height).Render(body)
}

// SetSize takes the outer cell dims; the list gets them verbatim unless the panel
// is bordered, in which case the frame comes off both axes first. The filter line
// comes off the height too, so the list's own pagination knows about the row the
// panel is drawing above it.
func (p *ListPanel) SetSize(width, height int) {
	p.width, p.height = width, height
	p.sizeList()
}

// sizeList applies the stored outer dims to the list, net of the panel's own chrome.
// Called again whenever the filter line appears or goes (see UpdatePanel): the list
// computes PerPage from the height it was given and View clamps its body to the same
// number, so a header height that changes without this clips the last row.
func (p *ListPanel) sizeList() {
	w, h := p.listWidth(), p.height
	if p.bordered {
		h -= 2 // the frame's top and bottom edges
	}
	if h -= p.filterRows(); h < 1 {
		h = 1
	}
	FitList(&p.list, w, h)
}

// innerWidth is the run between the frame's corners: the outer width minus the two
// side borders.
func (p *ListPanel) innerWidth() int {
	if w := p.width - 2; w > 1 {
		return w
	}
	return 1
}
