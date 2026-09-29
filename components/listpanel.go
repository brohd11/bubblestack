package components

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ListPanel is a picker-style list packaged as a ModularScreen panel (the sidebar of a
// list-plus-detail layout). It shares PickerScreen's dispatch, except Back: a panel
// shares its screen, so esc is left to the host ModularScreen's pop.
type ListPanel struct {
	list      list.Model
	focused   bool
	onSelect  func(*core.Shared, list.Item) core.Action
	onKey     func(*core.Shared, string, list.Item) (core.Action, bool)
	onPointer func(*core.Shared, list.Item, bool) (core.Action, bool)
	help      []key.Binding

	title    string // kept for the border legend (the list's own title bar is off when bordered)
	bordered bool   // ListPanelOpts.Border: draw the shared frame
	width    int    // outer cell width, for the frame's inner run
	height   int    // outer cell height, so the list can be re-sized when the filter line appears
	itemRows int    // delegate height + spacing; drives mouse and overlay geometry

	// selection is ListPanelOpts.Selection; selHidden is its HideUnfocused resolved against
	// the focus View was last drawn with. The delegate holds a pointer to it.
	selection core.SelectionOpts
	selHidden bool

	// ownFilter: the panel draws the filter line itself, so it costs a row only while a
	// filter is live. Compact panels only.
	ownFilter bool

	// Marquee state (compact panels, see startMarquee): marquee is the offset the delegate
	// reads, marqueeID tags this panel's ticks, hold is the remaining end-dwell frames, and
	// lastSel detects a cursor move.
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

// ListPanelOpts mirrors the PickerOpts hooks a sidebar needs: OnSelect on enter (default:
// a self-dispatching Item), OnKey for extra row keys before WrapNav, and Help for the bar
// while focused. Border draws the shared frame with the title as its focus-tinted legend.
type ListPanelOpts struct {
	OnSelect func(*core.Shared, list.Item) core.Action
	OnKey    func(*core.Shared, string, list.Item) (core.Action, bool)
	Help     []key.Binding
	Border   bool

	// Selection styles the selected row; the zero value is the accent left border, always
	// shown.
	Selection core.SelectionOpts

	// OnPointer handles a click on a row before the default, right reporting which button;
	// the row is already selected. It is how a list tells a click from enter. handled=false
	// (or a nil hook) falls back: left acts as enter, right does nothing.
	OnPointer func(*core.Shared, list.Item, bool) (core.Action, bool)
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
	// The panel draws the filter itself (filterLine), so bubbles draws no header. The
	// filter still runs: SetShowFilter only affects drawing.
	p.ownFilter = true
	p.list.SetShowFilter(false)
	// bubbles adds MarginTop(1) to pagination for zero-spacing delegates. Render it inline to
	// drop that blank row, restoring the left padding so the dots stay aligned.
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
	p := &ListPanel{
		list:      build(items, listTitle, opts.Help...),
		onSelect:  opts.OnSelect,
		onKey:     opts.OnKey,
		onPointer: opts.OnPointer,
		help:      opts.Help,
		title:     title,
		bordered:  opts.Border,
		itemRows:  itemRows,
		selection: opts.Selection,
	}
	p.applyDelegate()
	// A bordered panel has no title bar, but bubbles would still draw an empty header row.
	// Drawing the filter ourselves means that row appears only while a filter is live.
	if opts.Border {
		p.ownFilter = true
		p.list.SetShowFilter(false)
	}
	return p
}

// Marquee: in a compact row the selected row's name and suffix slide as one string,
// dwelling at each end, instead of being truncated. The panel owns the clock because it
// knows focus and cursor. It re-arms only while the focused panel's selected row
// overflows, so it stops by itself. Ticks ride ModularScreen's non-key broadcast.
const (
	marqueeInterval = 130 * time.Millisecond
	marqueeHold     = 8 // frames of dwell at each end, ~1s
)

// marqueeIDs gives each marqueeing panel a distinct non-zero clock id: every panel
// receives every tick, and acting on a sibling's would double the rate. Atomic because
// screens can be built off the tea goroutine.
var marqueeIDs atomic.Int64

type marqueeTickMsg struct{ id int64 }

func marqueeTick(id int64) tea.Cmd {
	return tea.Tick(marqueeInterval, func(time.Time) tea.Msg { return marqueeTickMsg{id: id} })
}

// startMarquee opts this panel in and points the delegate at the offset the ticks
// advance.
func (p *ListPanel) startMarquee() {
	p.marqueeID = marqueeIDs.Add(1)
	p.hold = marqueeHold
	p.applyDelegate()
}

// applyDelegate installs the delegate for the panel's kind (the marqueeing compact one, or
// the three-row one) wired to its selection options and the hidden flag View maintains.
func (p *ListPanel) applyDelegate() {
	if p.marqueeID != 0 {
		p.list.SetDelegate(core.CompactDelegate{Offset: &p.marquee, Selection: p.selection, Hidden: &p.selHidden})
		return
	}
	d := core.NewDelegate()
	d.Selection, d.Hidden = p.selection, &p.selHidden
	p.list.SetDelegate(d)
}

// marqueeOverflow reports the selected row's last useful offset, and false when it fits,
// the list is filtered, or the panel does not marquee. It measures against the same width
// Render uses.
func (p *ListPanel) marqueeOverflow() (int, bool) {
	if p.marqueeID == 0 || p.list.FilterState() != list.Unfiltered {
		return 0, false
	}
	tw := core.CompactTextWidth(p.list.Width())
	// CompactMarquee resolves the row's contract itself and rejects rows neither delegate can
	// render.
	row, over := core.CompactMarquee(p.list.SelectedItem(), tw)
	if !over {
		return 0, false
	}
	return row.MarqueeLimit(tw), true
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

// marqueeTicked drops other panels' ticks. Its own either advance and re-arm, or stop and
// reset the row when focus is lost or the row fits.
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

// marqueeStart arms the loop when idle and there is something to scroll, returning the
// tick to emit or nil.
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

// OnFocus implements FocusNotifier so the marquee starts on focus: the pane key that
// granted focus never reaches the panel.
func (p *ListPanel) OnFocus() tea.Cmd { return p.marqueeStart() }

// marqueeArm re-syncs after any other message: a cursor move resets the row, and an idle
// eligible marquee starts. It also covers focus returned through SetFocused, which carries
// no cmd.
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

// Init arms the first tick unconditionally: the panel is not sized yet, and
// marqueeTicked re-checks once it is.
func (p *ListPanel) Init(*core.Shared) tea.Cmd {
	// A second Init is a no-op: panels outlive the layout holding them, and a second clock
	// with the same id would double the ticks every pass.
	if p.marqueeID == 0 || p.ticking {
		return nil
	}
	p.ticking = true
	return marqueeTick(p.marqueeID)
}

func (p *ListPanel) Focus()        { p.focused = true }
func (p *ListPanel) Blur()         { p.focused = false }
func (p *ListPanel) Focused() bool { return p.focused }

// SetItems replaces the rows keeping any live filter (see SetListItems), re-sizing in case
// the filter's line appears or goes.
func (p *ListPanel) SetItems(items []list.Item) {
	SetListItems(&p.list, items)
	p.sizeList()
}

// List exposes the underlying list model for the read access the panel API
// doesn't cover (SelectedItem, Index, FilterState).
func (p *ListPanel) List() *list.Model { return &p.list }

// Capturing reports an active /-filter, so the host routes every key here.
func (p *ListPanel) Capturing() bool { return p.list.FilterState() == list.Filtering }

// UpdatePanel runs listDispatch but leaves Back to the host's pop, unless a filter is
// applied (esc clears it first). While typing a filter, esc cancels it. The wheel only
// moves the cursor while focused.
func (p *ListPanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if t, ok := msg.(marqueeTickMsg); ok {
		return p.marqueeTicked(t), true
	}
	if _, ok := msg.(tea.MouseMsg); ok && !p.focused {
		return core.Action{}, false
	}
	if km, ok := msg.(tea.KeyPressMsg); ok {
		if k := km.String(); core.MatchKey(k, core.Keys.Back) && !p.Capturing() {
			// An applied filter must be clearable: Back goes to the host below, so bubbles'
			// ClearFilter binding would otherwise be unreachable.
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
	// Must stay nil without a hook: listDispatch reads nil as "default for both buttons".
	var onPointer func(right bool) (core.Action, bool)
	if p.onPointer != nil {
		onPointer = func(right bool) (core.Action, bool) {
			return p.onPointer(sh, p.list.SelectedItem(), right)
		}
	}
	// Coordinates are already panel-local; subtract the panel's own chrome (frame row,
	// live filter line) for the list-local math.
	rows := p.filterRows()
	act := p.marqueeArm(listDispatch(sh, &p.list, msg, p.chromeRows(), p.itemRows, onSelect, onKey, onPointer))
	// Re-size only when the filter line appeared or went, which changes the list's height.
	if p.filterRows() != rows {
		p.sizeList()
	}
	return act, true
}

// PanelHelp adds the select hint and the caller's Help to the host's bar while focused.
// Not the filter key: panel help follows the bar's rule (navigation only, see
// core.ShortHelp), and "/" belongs in the (?) menu.
func (p *ListPanel) PanelHelp() []key.Binding {
	return append([]key.Binding{
		core.Hint("select", core.Keys.Select),
	}, p.help...)
}

// filterLine is the panel-drawn filter row, empty when none is live. bubbles only draws
// the filter while it is typed, and with the status bar off an applied filter would
// otherwise hide rows with no explanation. It uses bubbles' own look via core.RenderFilter.
func (p *ListPanel) filterLine() string {
	if !p.ownFilter {
		return ""
	}
	line := core.RenderFilter(&p.list)
	if line == "" {
		return ""
	}
	// Indented like bubbles' title bar, and truncated rather than wrapped: filterRows promises
	// exactly one row.
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

// RowY is the panel-relative row where visible item idx starts, frame edge and filter line
// included: what an overlay anchored on a row must use.
func (p *ListPanel) RowY(idx int) (int, bool) {
	row, ok := listItemRow(&p.list, idx, p.itemRows)
	if !ok {
		return 0, false
	}
	return row + p.chromeRows(), true
}

// chromeRows is what sits above the list in the panel (frame edge, filter line). Click
// math and RowY both use it.
func (p *ListPanel) chromeRows() int {
	rows := p.filterRows()
	if p.bordered {
		rows++
	}
	return rows
}

// View renders the list under its filter line, framed and focus-tinted when Border is set.
func (p *ListPanel) View(focused bool) string {
	p.selHidden = p.selection.HideUnfocused && !focused
	body := p.list.View()
	if line := p.filterLine(); line != "" {
		body = line + "\n" + body
	}
	if p.bordered {
		body = Frame(p.title, body, p.innerWidth(), focused)
	}
	// Clip to the allocation: the rendered footprint is also the host's hit-test geometry.
	return lipgloss.NewStyle().MaxHeight(p.height).Render(body)
}

// SetSize takes outer dims; the frame (when bordered) and the filter line come off before
// the list sees them.
func (p *ListPanel) SetSize(width, height int) {
	p.width, p.height = width, height
	p.sizeList()
}

// sizeList sizes the list to the stored dims minus the panel's chrome. It runs again when
// the filter line comes or goes, or the list's PerPage would clip the last row.
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
