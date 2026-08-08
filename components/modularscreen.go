package components

import (
	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ModularScreen is the content-agnostic multi-pane screen: a grid of Panel cells
// laid out as columns of weighted rows, with one Focusable panel holding focus
// at a time. It is a pure layout shell — panels draw their own borders and own
// their keys; the screen only routes input (child first, itself as fallback),
// cycles focus on tab, pops on esc, and composes the help bar. Like every
// component it names no domain type: the consumer fills the slots and answers
// the hooks.
type ModularScreen struct {
	cols       [][]Slot
	flat       []*Slot     // declaration order: column 0 top→bottom, then column 1, …
	rects      []panelRect // per flat slot, body-relative; laid out by SetSize
	bodyH      int         // post-title body height, stashed by SetSize for Expand
	colWidths  []int
	focus      int // index into flat; -1 when no slot is focusable
	title      string
	crumb      string
	crumbShort string
	help       []key.Binding
	refresh    func(*core.Shared, any) bool
	popStop    bool
	dir        string
	initFn     func(*core.Shared) tea.Cmd
}

// panelRect is a slot's body-relative bounding box, recorded by SetSize so Update
// can hit-test mouse coordinates (translated via Shared.BodyY).
type panelRect struct{ x, y, w, h int }

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
	help := opts.Help
	if opts.Dir != "" {
		// A screen with a directory is a DirLocator, so the global terminal/open-dir
		// keys fire on it — advertise them in its help alongside any caller-supplied
		// bindings, as PickerScreen does.
		help = append(append([]key.Binding{}, opts.Help...), core.DirKeyHints()...)
	}
	s := &ModularScreen{
		cols:       columns,
		colWidths:  opts.ColWidths,
		focus:      -1,
		title:      opts.Title,
		crumb:      opts.Crumb,
		crumbShort: opts.CrumbShort,
		help:       help,
		refresh:    opts.Refresh,
		popStop:    opts.PopStop,
		dir:        opts.Dir,
		initFn:     opts.Init,
	}
	for c := range s.cols {
		for i := range s.cols[c] {
			s.flat = append(s.flat, &s.cols[c][i])
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
func (s *ModularScreen) Init(sh *core.Shared) tea.Cmd {
	var cmds []tea.Cmd
	for _, slot := range s.flat {
		if ini, ok := slot.Panel.(panelInitializer); ok {
			if cmd := ini.Init(sh); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}
	if s.initFn != nil {
		if cmd := s.initFn(sh); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update routes input child-first, the screen itself as fallback:
//  1. a capturing panel (Capturing) claims every keystroke, focused or not — a
//     filter input losing characters to the pane cycle would read as a bug;
//  2. other key msgs go to the focused panel's PanelUpdater — a handled result
//     is applied and done;
//  3. an unhandled tab cycles focus (matched as a raw key string: it is
//     ModularScreen's own key, so it has no core.Keys binding — the one
//     sanctioned exception to the MatchKey rule);
//  4. an unhandled Back pops the screen;
//  5. anything else is dropped — the focused panel already had its chance.
//
// Non-key msgs (ticks, broadcast results) are instead fanned out to every
// slot's PanelUpdater and their cmds batched, so a panel keeps live data without
// holding focus; a nav Msg is rare on this path, but the first non-nil one is
// honored.
//
// Mouse presses are the exception to the broadcast: they are hit-tested against
// the slot rects SetSize recorded (coordinates translated via Shared.BodyY — the
// same problem the router solves for the output pane with inOutput), and a press
// inside a Focusable slot moves focus there and goes to that panel alone. That's
// what makes a pane scrollable when keyboard focus can't reach it — a sibling
// form may be capturing every key, but the wheel still works over its neighbor.
// Presses that miss every slot (or hit a non-focusable one) fall through to the
// broadcast, as do motion/release events.
func (s *ModularScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if km, ok := msg.(tea.KeyMsg); ok {
		k := km.String()
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
		switch {
		case k == "tab":
			s.advanceFocus()
		case core.MatchKey(k, core.Keys.Back):
			return s, core.Pop()
		}
		return s, core.Action{}
	}
	if mm, ok := msg.(tea.MouseMsg); ok && mm.Action == tea.MouseActionPress {
		if i := s.slotAt(sh, mm.X, mm.Y); i >= 0 && isFocusable(s.flat[i].Panel) {
			s.focusSlot(i)
			if u, ok := s.flat[i].Panel.(PanelUpdater); ok {
				act, _ := u.UpdatePanel(sh, msg)
				return s, act
			}
			return s, core.Action{}
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

// Filtering keeps the router's global single-key shortcuts (tab, q, [ ]) from
// stealing keystrokes while any panel is capturing text (see capturingSlot).
func (s *ModularScreen) Filtering() bool { return s.capturingSlot() >= 0 }

func (s *ModularScreen) PopStop() bool { return s.popStop }

// SetFocused implements core.FocusableScreen: the router blurs the screen when
// the output pane takes the keys and refocuses it on return. The focus INDEX is
// untouched — only the focused panel's visual state flips (Blur/Focus are
// idempotent flag-setters; a ScreenPanel forwards to its child), so the pane
// that was active dims and relights in place.
func (s *ModularScreen) SetFocused(focused bool) {
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

// Receive relays a PropagateAll broadcast to the Refresh closure when one is
// configured; without one it's a no-op (the common case). The closure answers
// the payload out of the panels it owns (SetLines/SetItems), so the screen
// itself stays content-agnostic.
func (s *ModularScreen) Receive(sh *core.Shared, payload any) core.Action {
	if s.refresh != nil {
		s.refresh(sh, payload)
	}
	return core.Action{}
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
// Before joining, each column gets a measure-then-grow pass for its Expand
// slots: panels are rendered at their Weight allocation, and a panel whose
// content renders shorter (a form's box) would leave the slack pooling at the
// bottom of the terminal. The slack is split equally among the column's Expand
// slots (remainder to the last), which are re-sized and re-rendered — so an
// Expand slot fills whatever its siblings didn't use. Growth only, never a
// shrink; columns without Expand slots render exactly as allocated.
func (s *ModularScreen) View(*core.Shared) string {
	cols := make([]string, len(s.cols))
	fi := 0
	for c, col := range s.cols {
		rows := make([]string, len(col))
		total := 0
		for i, slot := range col {
			rows[i] = slot.Panel.View(fi == s.focus)
			total += lipgloss.Height(rows[i])
			fi++
		}
		if slack := s.bodyH - total; slack > 0 {
			var expand []int // flat indices of this column's Expand slots
			for i := range col {
				if col[i].Expand {
					expand = append(expand, fi-len(col)+i)
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
					if col[i].Expand {
						idx := fi - len(col) + i
						rows[i] = col[i].Panel.View(idx == s.focus)
					}
				}
			}
		}
		cols[c] = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}
	return core.WithTitle(s.title, lipgloss.JoinHorizontal(lipgloss.Top, cols...))
}

// HelpView composes the bar from the screen's own hints (pane cycle, back), the
// focused panel's PanelHelp bindings, and the caller's Help extras, rendered
// through the shared static-help style.
func (s *ModularScreen) HelpView(sh *core.Shared) string {
	var hints []key.Binding
	if s.focusableCount() > 1 {
		hints = append(hints, key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "panes")))
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

// capturingSlot is the flat index of the panel currently capturing text input (a
// filtering list, a typing form child), or -1. Only a panel that is also a
// PanelUpdater counts — capturing without input routing is meaningless, and only
// one panel can sensibly capture at a time.
func (s *ModularScreen) capturingSlot() int {
	for i, slot := range s.flat {
		c, ok := slot.Panel.(Capturing)
		if !ok || !c.Capturing() {
			continue
		}
		if _, ok := slot.Panel.(PanelUpdater); ok {
			return i
		}
	}
	return -1
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

// nextFocusable scans forward from `from`, wrapping, for the next Focusable
// slot; with a single focusable slot it returns `from` itself (focus stays put).
func (s *ModularScreen) nextFocusable(from int) int {
	n := len(s.flat)
	for i := 1; i <= n; i++ {
		if j := (from + i) % n; isFocusable(s.flat[j].Panel) {
			return j
		}
	}
	return from
}

// advanceFocus moves focus on tab per the slot's NextFocus rule (see
// Slot.NextFocus): default loop, explicit 1-based target, or FocusEnd back to
// the first Focusable slot.
func (s *ModularScreen) advanceFocus() {
	if s.focus < 0 {
		return
	}
	target := -1
	switch nf := s.flat[s.focus].NextFocus; {
	case nf == FocusEnd:
		target = s.firstFocusable()
	case nf > 0:
		// An explicit target names a flattened slot directly; an out-of-range or
		// non-focusable one is a config error, so focus stays put rather than
		// landing somewhere arbitrary.
		if i := nf - 1; i >= 0 && i < len(s.flat) && isFocusable(s.flat[i].Panel) {
			target = i
		}
	default:
		target = s.nextFocusable(s.focus)
	}
	if target >= 0 {
		s.focusSlot(target)
	}
}

// focusSlot moves focus to flat slot i, blurring the old panel and focusing the
// new. A no-op when i already holds focus or isn't Focusable.
func (s *ModularScreen) focusSlot(i int) {
	if i == s.focus || !isFocusable(s.flat[i].Panel) {
		return
	}
	if s.focus >= 0 {
		if f, ok := s.focusedPanel().(Focusable); ok {
			f.Blur()
		}
	}
	s.focus = i
	s.focusedPanel().(Focusable).Focus()
}

// slotAt is the flat index of the slot whose rect contains absolute terminal
// coordinates (x, y), or -1. Rects are body-relative; Shared.BodyY carries the
// chrome rows above the body.
func (s *ModularScreen) slotAt(sh *core.Shared, x, y int) int {
	rel := y - sh.BodyY()
	for i, r := range s.rects {
		if x >= r.x && x < r.x+r.w && rel >= r.y && rel < r.y+r.h {
			return i
		}
	}
	return -1
}
