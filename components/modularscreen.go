package components

import (
	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ModularScreen is the content-agnostic multi-pane screen: a grid of Panel cells
// laid out as columns of weighted rows, with one Focusable panel holding focus
// at a time. It is a pure layout shell — panels draw their own borders and own
// their keys; the screen only routes input (child first, itself as fallback),
// moves focus on the reserved pane keys, pops on esc, and composes the help bar.
// Like every component it names no domain type: the consumer fills the slots and
// answers the hooks.
//
// Focus moves two ways, both permanent and neither a fallback for the other:
//
//   - a CYCLE (shift+tab) walks the focusable slots in declaration order,
//     wrapping — the right gesture on the two- or three-pane screens that make up
//     most layouts, where "the next pane" is unambiguous. It is forward-only: no
//     terminal sends a "backtab" the other way, and PanePrev carries no keys, so
//     the wrap is what reaches every pane;
//   - DIRECTIONAL moves aim at a pane by its place in the grid: one column over
//     keeping the current row, or one row up inside the column (see neighbor).
//     That reads off the layout the user is looking at, and is what a grid big
//     enough to make "next" meaningless actually needs. Implemented, but its keys
//     are not chosen yet — see core.Keys.PaneLeft.
//
// Either way the cost to the panels is only the reserved keys themselves, which
// is what lets a pane that types everything else (an embedded EditorScreen) still
// be left from the keyboard.
type ModularScreen struct {
	cols        [][]Slot
	flat        []*Slot     // declaration order: column 0 top→bottom, then column 1, …
	pos         []gridPos   // per flat slot, its (column, row) in cols — the inverse of flat
	starts      []int       // per column, the flat index of its first slot: flatIndex(c,r) = starts[c]+r
	rects       []panelRect // per flat slot, body-relative Weight allocation; laid out by SetSize
	hitRects    []panelRect // per flat slot, as actually rendered (post-ExpandV/H); rebuilt by View
	bodyH       int         // post-title body height, stashed by SetSize for ExpandV
	colWidths   []int
	focus       int  // index into flat; -1 when no slot is focusable
	mouseSlot   int  // slot owning the current left/right gesture; -1 when none is active
	hostFocused bool // the screen itself holds focus (router drives it on output-pane focus)
	title       string
	crumb       string
	crumbShort  string
	help        []key.Binding
	refresh     func(*core.Shared, any) bool
	popStop     bool
	dir         string
	initFn      func(*core.Shared) tea.Cmd
}

// panelRect is a slot's body-relative bounding box, recorded by SetSize so Update
// can hit-test mouse coordinates (translated via Shared.BodyY).
type panelRect struct{ x, y, w, h int }

// gridPos is a slot's place in the declared grid — the address directional focus
// movement works in, as opposed to the flat index everything else uses.
type gridPos struct{ col, row int }

var _ core.Screen = (*ModularScreen)(nil)
var _ core.Filterer = (*ModularScreen)(nil)
var _ core.PopStopper = (*ModularScreen)(nil)
var _ core.Crumber = (*ModularScreen)(nil)
var _ core.Receiver = (*ModularScreen)(nil)
var _ core.DirLocator = (*ModularScreen)(nil)
var _ core.FocusableScreen = (*ModularScreen)(nil)

// ModularOpts configures a ModularScreen. ColWidths sizes columns in cells, one
// entry per column; 0 (or a missing entry) makes the column flex — flex columns
// share whatever width the fixed columns leave. Refresh, when set, makes the
// screen a Receiver (same semantics as PickerOpts.Refresh); Dir advertises a
// directory to the router's global terminal/open-dir keys (DirLocator).
type ModularOpts struct {
	Title      string // optional in-body title bar (core.WithTitle); omitted ⇒ no bar
	Crumb      string // breadcrumb segment; defaults to Title
	CrumbShort string // optional short breadcrumb segment; defaults to Crumb/Title
	ColWidths  []int  // per column, cells; 0 = flex
	Help       []key.Binding
	Refresh    func(sh *core.Shared, payload any) bool
	PopStop    bool   // mark this screen as a PopTo boundary (a command hub)
	Dir        string // directory this screen concerns; enables the global Terminal key (DirLocator)
	// Init, when set, runs from the screen's Init and its cmd is batched with the
	// panel initializers — the hook a consumer uses to kick off an async load on
	// open (a network read, say), whose result then arrives as a broadcast msg.
	Init func(*core.Shared) tea.Cmd
}

// NewModularScreen builds the screen, flattens the slots in declaration order,
// and focuses the first Focusable panel.
func NewModularScreen(columns [][]Slot, opts ModularOpts) *ModularScreen {
	// opts.Dir makes this a DirLocator, so the global terminal/open-dir keys fire on it —
	// but they are NOT added to the help here. Unlike PickerScreen, whose extras land in the
	// (?) full help, a ModularScreen's help is the bar itself, and the bar stays sparse
	// (see core.ShortHelp). The keys work unadvertised; opts.Help is the caller's alone.
	help := opts.Help
	s := &ModularScreen{
		cols:        columns,
		colWidths:   opts.ColWidths,
		focus:       -1,
		mouseSlot:   -1,
		hostFocused: true,
		title:       opts.Title,
		crumb:       opts.Crumb,
		crumbShort:  opts.CrumbShort,
		help:        help,
		refresh:     opts.Refresh,
		popStop:     opts.PopStop,
		dir:         opts.Dir,
		initFn:      opts.Init,
	}
	for c := range s.cols {
		s.starts = append(s.starts, len(s.flat))
		for i := range s.cols[c] {
			s.flat = append(s.flat, &s.cols[c][i])
			s.pos = append(s.pos, gridPos{col: c, row: i})
		}
	}
	if f := s.firstFocusable(); f >= 0 {
		s.focus = f
		s.focusedPanel().(Focusable).Focus()
	}
	return s
}

// Init runs any panel initializers (ScreenPanel starts its child screen), then
// the consumer's ModularOpts.Init hook, and batches all their cmds.
//
// It also drains the FocusNotifier of the slot focused at CONSTRUCTION. That focus
// is granted by NewModularScreen, which has no cmd lane at all, so a panel would
// otherwise never learn about the only focus event it does not receive as a
// transition — the state it was born into. Same reasoning as ScreenPanel.syncChild.
func (s *ModularScreen) Init(sh *core.Shared) tea.Cmd {
	var cmds []tea.Cmd
	for _, slot := range s.flat {
		if ini, ok := slot.Panel.(panelInitializer); ok {
			if cmd := ini.Init(sh); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if s.focus >= 0 {
		if cmd := panelFocusCmd(s.focusedPanel()); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if s.initFn != nil {
		if cmd := s.initFn(sh); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update routes input screen-first for the reserved pane keys, then child-first
// for everything else:
//  1. a pane-navigation key (core.Keys.PaneNext et al.) moves focus and is
//     consumed here, ABOVE the capture gate below. That ordering is the point: a
//     panel that captures every keystroke would otherwise have no keyboard exit,
//     and reserving a handful of keys buys one that works on every panel without
//     any of them cooperating;
//  2. a capturing FOCUSED panel (Capturing) claims every remaining keystroke — a
//     filter input losing characters would read as a bug, and the focus gate is
//     what stops a clicked-away textarea from keeping the keys;
//  3. other key msgs go to the focused panel's PanelUpdater — a handled result
//     is applied and done;
//  4. an unhandled Back pops the screen;
//  5. anything else is dropped — the focused panel already had its chance.
//
// Non-key msgs (ticks, broadcast results) are instead fanned out to every
// slot's PanelUpdater and their cmds batched, so a panel keeps live data without
// holding focus; a nav Msg is rare on this path, but the first non-nil one is
// honored.
//
// Mouse gestures are the exception to the broadcast: presses are hit-tested against
// the slot rects SetSize recorded (coordinates translated via Shared.BodyY — the
// same problem the router solves for the output pane with inOutput), and a press
// inside a Focusable slot moves focus there and goes to that panel alone, with
// the coordinates made slot-relative first so a panel maps clicks against its
// own layout. That's what makes a pane scrollable when keyboard focus can't
// reach it — a sibling form may be capturing every key, but the wheel still
// works over its neighbor. A left press also owns its following motion/release,
// even beyond the pane. Presses that miss every slot (or hit a non-focusable one)
// fall through to the broadcast.
func (s *ModularScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
		k := km.String()
		if cmd, moved := s.moveFocus(k); moved {
			// The pane key itself is consumed here and never reaches a panel, so
			// this Action is the newly focused panel's only chance to start work
			// off the focus change (FocusNotifier).
			return s, core.Async(cmd)
		}
		if ci := s.capturingSlot(); ci >= 0 {
			act, _ := s.flat[ci].Panel.(PanelUpdater).UpdatePanel(sh, msg)
			return s, act
		}
		if s.focus >= 0 {
			if u, ok := s.focusedPanel().(PanelUpdater); ok {
				if act, handled := u.UpdatePanel(sh, msg); handled {
					return s, act
				}
			}
		}
		if core.MatchKey(k, core.Keys.Back) {
			return s, core.Pop()
		}
		return s, core.Action{}
	}
	if mm, ok := msg.(tea.MouseMsg); ok {
		// v1 reported the wheel as a press, so "aims at a pane" means a click or a
		// wheel notch; motion and release continue whatever gesture a click started.
		_, isClick := mm.(tea.MouseClickMsg)
		_, isWheel := mm.(tea.MouseWheelMsg)
		_, isRelease := mm.(tea.MouseReleaseMsg)
		m := mm.Mouse()
		if isClick || isWheel {
			s.mouseSlot = -1
			if i := s.slotAt(sh, m.X, m.Y); i >= 0 && isFocusable(s.flat[i].Panel) {
				if m.Button == tea.MouseLeft || m.Button == tea.MouseRight {
					s.mouseSlot = i
				}
				focus := s.focusSlot(i)
				act := s.updateMouseSlot(sh, i, mm)
				// Batched, not replaced: the press has its own work (a row select,
				// a scroll) and the focus cmd rides alongside it.
				act.Cmd = tea.Batch(act.Cmd, focus)
				return s, act
			}
		} else if s.mouseSlot >= 0 {
			i := s.mouseSlot
			act := s.updateMouseSlot(sh, i, mm)
			if isRelease {
				s.mouseSlot = -1
			}
			return s, act
		}
	}
	var cmds []tea.Cmd
	var nav tea.Msg
	for _, slot := range s.flat {
		u, ok := slot.Panel.(PanelUpdater)
		if !ok {
			continue
		}
		act, _ := u.UpdatePanel(sh, msg)
		if act.Cmd != nil {
			cmds = append(cmds, act.Cmd)
		}
		if nav == nil {
			nav = act.Msg
		}
	}
	return s, core.Action{Msg: nav, Cmd: tea.Batch(cmds...)}
}

// updateMouseSlot forwards a mouse event to one pane in pane-relative coordinates.
// Keeping this translation for a full left- or right-button gesture lets a drag leave
// its pane without changing owners or exposing absolute terminal coordinates to the
// panel that began it. A panel that needs absolute cells anyway — one anchoring an
// overlay at the pointer, as EditorScreen's context menu does — adds back the origin
// this subtracts, which it receives through PaneOriginer.
func (s *ModularScreen) updateMouseSlot(sh *core.Shared, i int, mm tea.MouseMsg) core.Action {
	r := s.slotRect(i)
	if u, ok := s.flat[i].Panel.(PanelUpdater); ok {
		act, _ := u.UpdatePanel(sh, translateMouse(mm, r.x, sh.BodyY()+r.y))
		return act
	}
	return core.Action{}
}

// translateMouse shifts a mouse message's position by (dx, dy), preserving its concrete
// type so the receiving panel still reads a click as a click and a release as a release.
// v2 keeps the coordinates on the concrete message rather than a shared struct behind an
// Action field, so the shift is a rebuild per kind rather than two assignments.
func translateMouse(mm tea.MouseMsg, dx, dy int) tea.Msg {
	m := mm.Mouse()
	m.X -= dx
	m.Y -= dy
	switch mm.(type) {
	case tea.MouseClickMsg:
		return tea.MouseClickMsg(m)
	case tea.MouseReleaseMsg:
		return tea.MouseReleaseMsg(m)
	case tea.MouseWheelMsg:
		return tea.MouseWheelMsg(m)
	case tea.MouseMotionMsg:
		return tea.MouseMotionMsg(m)
	}
	return mm
}

// Filtering keeps the router's global single-key shortcuts (O, q, [ ]) from
// stealing keystrokes while the focused panel is capturing text (see
// capturingSlot).
func (s *ModularScreen) Filtering() bool { return s.capturingSlot() >= 0 }

func (s *ModularScreen) PopStop() bool { return s.popStop }

// SetFocused implements core.FocusableScreen: the router blurs the screen when
// the output pane takes the keys and refocuses it on return. The focus INDEX is
// untouched — hostFocused gates the focused render arg View passes (a panel
// that renders from the arg, like ScrollContainer, dims), and the Blur/Focus
// forwarding carries it to panels that render from their own state (a
// ScreenPanel's child form) — so the active pane dims and relights in place.
func (s *ModularScreen) SetFocused(focused bool) {
	s.hostFocused = focused
	if s.focus < 0 {
		return
	}
	if f, ok := s.focusedPanel().(Focusable); ok {
		if focused {
			f.Focus()
		} else {
			f.Blur()
		}
	}
}

// LocateDir reports the directory this screen concerns (ModularOpts.Dir), so the
// global Terminal key opens a terminal there. Empty dir ⇒ no locator (the key
// falls through).
func (s *ModularScreen) LocateDir() (string, bool) { return s.dir, s.dir != "" }

// Receive relays a PropagateAll broadcast to receiving panels and to the optional
// Refresh closure. The recursive relay lets an async result reach a ScreenPanel child
// even while another screen is on top of this root.
func (s *ModularScreen) Receive(sh *core.Shared, payload any) core.Action {
	var acts []core.Action
	for _, slot := range s.flat {
		if receiver, ok := slot.Panel.(core.Receiver); ok {
			act := receiver.Receive(sh, payload)
			if act.Msg != nil || act.Cmd != nil {
				acts = append(acts, act)
			}
		}
	}
	if s.refresh != nil {
		s.refresh(sh, payload)
	}
	switch len(acts) {
	case 0:
		return core.Action{}
	case 1:
		return acts[0]
	default:
		return core.Seq(acts...)
	}
}

// CrumbLabel contributes the screen's breadcrumb segment: the short form when
// set, else the explicit crumb, else the title.
func (s *ModularScreen) CrumbLabel(short bool) string {
	return crumbSeg(short, s.crumbShort, s.crumb, s.title)
}

// View stacks each column's panels vertically and joins the columns side by
// side. Panels draw their own borders; the screen adds only the optional title
// bar.
//
// Before joining, each column gets a measure-then-grow pass for its ExpandV
// slots: panels are rendered at their Weight allocation, and a panel whose
// content renders shorter (a form's box) would leave the slack pooling at the
// bottom of the terminal. The slack is split equally among the column's ExpandV
// slots (remainder to the last), which are re-sized and re-rendered — so an
// ExpandV slot fills whatever its siblings didn't use. Growth only, never a
// shrink; columns without ExpandV slots render exactly as allocated.
//
// ExpandH slots are then padded out to their allocated width. That pass is
// separate and comes last because it acts on the rendered STRING rather than on
// the layout: no width is being redistributed (SetSize already assigned it), the
// padding just squares off a panel that drew narrower than it was given.
func (s *ModularScreen) View(sh *core.Shared) string {
	cols := make([]string, len(s.cols))
	y0 := 0
	if s.title != "" {
		y0 = lipgloss.Height(core.RenderTitleBar(s.title))
	}
	// The mouse hit-test targets what was RENDERED, not the Weight allocation:
	// a short-rendering panel (a form's box) leaves its allocation half-used and
	// the ExpandV pass shifts everything below it up, so hit rects come from the
	// final row heights here, each frame.
	track := len(s.rects) == len(s.flat)
	var hit []panelRect
	if track {
		hit = make([]panelRect, len(s.flat))
	}
	for c, col := range s.cols {
		rows := make([]string, len(col))
		total := 0
		for i, slot := range col {
			rows[i] = slot.Panel.View(s.starts[c]+i == s.focus && s.hostFocused)
			total += lipgloss.Height(rows[i])
		}
		if slack := s.bodyH - total; slack > 0 {
			var expand []int // flat indices of this column's ExpandV slots
			for i := range col {
				if col[i].ExpandV {
					expand = append(expand, s.starts[c]+i)
				}
			}
			if len(expand) > 0 {
				share := slack / len(expand)
				for j, idx := range expand {
					grow := share
					if j == len(expand)-1 {
						grow = slack - share*(len(expand)-1) // last takes the remainder
					}
					r := s.rects[idx]
					s.flat[idx].Panel.SetSize(r.w, r.h+grow)
				}
				// Re-render only the grown slots.
				for i := range col {
					if col[i].ExpandV {
						rows[i] = col[i].Panel.View(s.starts[c]+i == s.focus && s.hostFocused)
					}
				}
			}
		}
		if track {
			y := y0
			for i := range col {
				idx := s.starts[c] + i
				// Square off an ExpandH slot against its allocation. PlaceHorizontal
				// is a no-op when the block is already at least that wide, so this
				// pads but can never truncate.
				if col[i].ExpandH {
					rows[i] = lipgloss.PlaceHorizontal(s.rects[idx].w, lipgloss.Left, rows[i])
				}
				hit[idx] = panelRect{x: s.rects[idx].x, y: y, w: s.rects[idx].w, h: lipgloss.Height(rows[i])}
				y += lipgloss.Height(rows[i])
			}
		}
		cols[c] = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	s.hitRects = hit
	// Publish each slot's rendered origin (absolute cells, like the mouse path's
	// BodyY translation) to panels that lay out overlays of their own — an editor
	// anchoring its save-as box at its own bottom edge.
	if track {
		for i, slot := range s.flat {
			if po, ok := slot.Panel.(PaneOriginer); ok {
				po.SetPaneOrigin(hit[i].x, sh.BodyY()+hit[i].y)
			}
		}
	}
	return core.WithTitle(s.title, lipgloss.JoinHorizontal(lipgloss.Top, cols...))
}

// HelpView composes the bar from the screen's own hints (pane navigation, back),
// the focused panel's PanelHelp bindings, and the caller's Help extras, rendered
// through the shared static-help style.
func (s *ModularScreen) HelpView(sh *core.Shared) string {
	var hints []key.Binding
	if s.focusableCount() > 1 {
		hints = append(hints, core.PaneHint())
	}
	hints = append(hints, core.Hint("back", core.Keys.Back))
	if s.focus >= 0 {
		if h, ok := s.focusedPanel().(PanelHelper); ok {
			hints = append(hints, h.PanelHelp()...)
		}
	}
	hints = append(hints, s.help...)
	return sh.BindingHelp(hints)
}

// SetSize splits the terminal width between the columns (fixed widths from
// ColWidths, flex columns sharing the rest, the division remainder going to the
// last flex column) and then each column's height between its slots by Weight —
// the last slot taking the remainder, so rounding can't drift the layout off the
// bottom. Panels receive outer cell dims; each subtracts its own borders. The
// same arithmetic also records each slot's body-relative rect, so Update can
// hit-test mouse presses (see slotAt).
func (s *ModularScreen) SetSize(_ *core.Shared, width, bodyHeight int) {
	y0 := 0
	if s.title != "" {
		y0 = lipgloss.Height(core.RenderTitleBar(s.title))
		bodyHeight -= y0
	}
	s.bodyH = bodyHeight
	widths := make([]int, len(s.cols))
	fixed, flex, lastFlex := 0, 0, -1
	for c := range s.cols {
		w := 0
		if c < len(s.colWidths) {
			w = s.colWidths[c]
		}
		if w > 0 {
			widths[c] = w
			fixed += w
		} else {
			flex++
			lastFlex = c
		}
	}
	if flex > 0 {
		share := (width - fixed) / flex
		if share < 1 {
			share = 1
		}
		for c := range s.cols {
			if widths[c] == 0 {
				widths[c] = share
			}
		}
		widths[lastFlex] += (width - fixed) - share*flex
		if widths[lastFlex] < 1 {
			widths[lastFlex] = 1
		}
	}
	s.rects = make([]panelRect, 0, len(s.flat))
	x := 0
	for c, col := range s.cols {
		total := 0
		for _, slot := range col {
			total += weightOf(slot)
		}
		used := 0
		for i, slot := range col {
			h := bodyHeight - used // the last slot takes the remainder
			if i < len(col)-1 {
				h = bodyHeight * weightOf(slot) / total
			}
			if h < 1 {
				h = 1
			}
			s.rects = append(s.rects, panelRect{x: x, y: y0 + used, w: widths[c], h: h})
			used += h
			slot.Panel.SetSize(widths[c], h)
		}
		x += widths[c]
	}
}

// weightOf resolves a slot's height weight: 0 (unset) counts as 1.
func weightOf(s Slot) int {
	if s.Weight > 0 {
		return s.Weight
	}
	return 1
}

// capturingSlot is the flat index of the panel to route every keystroke to
// while it captures text input (a filtering list, a typing form child), or -1.
// Capture is gated on FOCUS: the keyboard can't move focus mid-capture (capture
// claims tab), so focused-only capture loses nothing there — but the mouse
// moves focus without the capturing panel knowing, and a click on a sibling
// must not leave keystrokes flowing to a textarea the user has left. Clicking
// the panel back restores capture with the focus. Only a panel that is also a
// PanelUpdater counts — capturing without input routing is meaningless, and
// only one panel can sensibly capture at a time.
func (s *ModularScreen) capturingSlot() int {
	if s.focus < 0 {
		return -1
	}
	p := s.focusedPanel()
	c, ok := p.(Capturing)
	if !ok || !c.Capturing() {
		return -1
	}
	if _, ok := p.(PanelUpdater); !ok {
		return -1
	}
	return s.focus
}

// firstFocusable is the flat index of the first Focusable slot, or -1.
func (s *ModularScreen) firstFocusable() int {
	for i, slot := range s.flat {
		if isFocusable(slot.Panel) {
			return i
		}
	}
	return -1
}

func (s *ModularScreen) focusableCount() int {
	n := 0
	for _, slot := range s.flat {
		if isFocusable(slot.Panel) {
			n++
		}
	}
	return n
}

func (s *ModularScreen) focusedPanel() Panel { return s.flat[s.focus].Panel }

// moveFocus applies a pane-navigation key. It reports whether k was one of them —
// consumed either way, so a move that runs off the edge is a no-op rather than
// falling through to the focused panel. That is the whole contract of the
// reservation: these keys mean "move panes" everywhere, on every screen, or they
// mean nothing; a key that sometimes reaches the panel underneath would be worse
// than one that never does.
//
// The cycle cases and the directional ones are peers, not a primary and a
// fallback — see the type doc. The directional bindings carry no keycodes today,
// so their cases simply never match (MatchKey against an empty binding is false)
// and cost a comparison each.
func (s *ModularScreen) moveFocus(k string) (tea.Cmd, bool) {
	var dc, dr int
	switch {
	case core.MatchKey(k, core.Keys.PaneNext):
		return s.cycleFocus(1), true
	case core.MatchKey(k, core.Keys.PanePrev):
		return s.cycleFocus(-1), true
	case core.MatchKey(k, core.Keys.PaneLeft):
		dc = -1
	case core.MatchKey(k, core.Keys.PaneRight):
		dc = 1
	case core.MatchKey(k, core.Keys.PaneUp):
		dr = -1
	case core.MatchKey(k, core.Keys.PaneDown):
		dr = 1
	default:
		return nil, false
	}
	if s.focus >= 0 {
		if t := s.neighbor(s.focus, dc, dr); t >= 0 {
			return s.focusSlot(t), true
		}
	}
	return nil, true
}

// cycleFocus steps focus by delta through the Focusable slots in flat order
// (column 0 top→bottom, then column 1, …), wrapping at both ends. With fewer than
// two focusable slots it lands back where it started, which focusSlot treats as
// the no-op it is.
func (s *ModularScreen) cycleFocus(delta int) tea.Cmd {
	if s.focus < 0 {
		return nil
	}
	n := len(s.flat)
	for i := 1; i <= n; i++ {
		j := ((s.focus+i*delta)%n + n) % n
		if isFocusable(s.flat[j].Panel) {
			return s.focusSlot(j)
		}
	}
	return nil
}

// neighbor is the flat index of the Focusable slot one step in direction
// (dc, dr) from flat slot `from`, or -1 when there is none.
//
// This is live code with no keys on it yet: core.Keys.PaneLeft and friends carry
// no keycodes (Apple Terminal strips the modifier from shift+↑/↓, so the obvious
// binding would silently fail), and filling those lists in is all it takes to
// reach this. Its behavior is pinned by TestPaneNavOverUnevenGrid, which calls it
// directly for exactly that reason.
//
// A horizontal step walks column by column, aiming at the current ROW: a
// PaneRight from row 1 lands on row 1 of the next column, or its nearest focusable row
// when it is shorter or that row is informational. A vertical step walks row by
// row inside the current column. Either way a column or row with nothing
// focusable is skipped and the scan continues in the same direction.
//
// Movement CLAMPS at the grid's edge rather than wrapping, so a direction key
// always means the same thing — a PaneLeft that sometimes jumped to the far right
// would make the grid unreadable. One consequence worth knowing: across columns
// of unequal length the round trip isn't symmetric (row 1 → a one-row column →
// back to row 0), because the row index is clamped on the way over and there is
// nothing to restore it from on the way back.
func (s *ModularScreen) neighbor(from, dc, dr int) int {
	p := s.pos[from]
	if dr != 0 {
		for r := p.row + dr; r >= 0 && r < len(s.cols[p.col]); r += dr {
			if i := s.starts[p.col] + r; isFocusable(s.flat[i].Panel) {
				return i
			}
		}
		return -1
	}
	for c := p.col + dc; c >= 0 && c < len(s.cols); c += dc {
		if i := s.focusableNear(c, p.row); i >= 0 {
			return i
		}
	}
	return -1
}

// focusableNear is the flat index of column c's Focusable slot whose row is
// closest to `row` (ties going to the upper one), or -1 when the column holds
// none. `row` is clamped into the column first, so aiming past a short column's
// end lands on its last row rather than missing.
func (s *ModularScreen) focusableNear(c, row int) int {
	n := len(s.cols[c])
	if row >= n {
		row = n - 1
	}
	for d := 0; d < n; d++ {
		for _, r := range [2]int{row - d, row + d} {
			if r >= 0 && r < n {
				if i := s.starts[c] + r; isFocusable(s.flat[i].Panel) {
					return i
				}
			}
			if d == 0 {
				break // row-d and row+d are the same slot
			}
		}
	}
	return -1
}

// focusSlot moves focus to flat slot i, blurring the old panel and focusing the
// new, and returns the new panel's FocusNotifier cmd (nil when it has none). A
// no-op — and a nil cmd — when i already holds focus or isn't Focusable: landing
// on the pane you are already in is not a focus event.
func (s *ModularScreen) focusSlot(i int) tea.Cmd {
	if i == s.focus || !isFocusable(s.flat[i].Panel) {
		return nil
	}
	if s.focus >= 0 {
		if f, ok := s.focusedPanel().(Focusable); ok {
			f.Blur()
		}
	}
	s.focus = i
	s.focusedPanel().(Focusable).Focus()
	return panelFocusCmd(s.focusedPanel())
}

// panelFocusCmd asks a freshly focused panel for its on-focus work. Call it AFTER
// Focus(), so the panel's own Focused() already reports true.
func panelFocusCmd(p Panel) tea.Cmd {
	if n, ok := p.(FocusNotifier); ok {
		return n.OnFocus()
	}
	return nil
}

// FocusSlot moves keyboard focus to flat slot i (the declaration order: column 0
// top→bottom, then column 1, …) — the programmatic counterpart of the pane keys and
// the mouse click, for a consumer that needs focus to follow an event (a sidebar
// selection focusing the detail pane, say). Out-of-range and non-Focusable targets
// are a no-op.
//
// It returns the newly focused panel's FocusNotifier cmd, which the caller is
// responsible for emitting (core.Async, or batched into the Action it was already
// returning) — the same "returns the cmd, the caller emits it" shape as
// ScreenPanel.SetChild. Discarding it is safe and simply skips the panel's on-focus
// work until its next message.
func (s *ModularScreen) FocusSlot(i int) tea.Cmd {
	if i < 0 || i >= len(s.flat) {
		return nil
	}
	return s.focusSlot(i)
}

// slotRect is the rect input hit-testing and coordinate translation use: the
// rendered layout once View has run (it differs from the Weight allocation
// whenever a panel renders short and ExpandV shifts things up), the allocation
// before then.
func (s *ModularScreen) slotRect(i int) panelRect {
	if len(s.hitRects) == len(s.flat) {
		return s.hitRects[i]
	}
	return s.rects[i]
}

// slotAt is the flat index of the slot whose rect contains absolute terminal
// coordinates (x, y), or -1. Rects are body-relative; Shared.BodyY carries the
// chrome rows above the body.
func (s *ModularScreen) slotAt(sh *core.Shared, x, y int) int {
	if len(s.rects) != len(s.flat) {
		return -1 // not laid out yet
	}
	rel := y - sh.BodyY()
	for i := range s.flat {
		r := s.slotRect(i)
		if x >= r.x && x < r.x+r.w && rel >= r.y && rel < r.y+r.h {
			return i
		}
	}
	return -1
}
