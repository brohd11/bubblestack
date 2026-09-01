package core

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// maskOf is the chrome suppression requested by screen s, or the zero mask (hide
// nothing) when it doesn't implement ChromeMasker. Parameterized by screen (rather
// than always reading r.Top()) so the overlay path can frame the screen below the
// popup with that screen's own mask.
func (r Router) maskOf(s Screen) ChromeMask {
	if m, ok := s.(ChromeMasker); ok {
		return m.ChromeMask()
	}
	return ChromeMask{}
}

// currentMask is the mask of the active (top) screen.
func (r Router) currentMask() ChromeMask { return r.maskOf(r.Top()) }

// outputVisible reports whether an output pane currently occupies layout space
// (present and shown). It does not account for the per-screen mask.
func (r Router) outputVisible() bool {
	return r.sh.Chrome != nil && r.sh.Chrome.Output != nil && r.sh.Chrome.Output.Shown()
}

// helpViewFor is screen s's help bar, suppressed (empty) when its mask hides it.
// helpHeightFor measures it the same way so the body sizing stays in sync.
func (r Router) helpViewFor(s Screen, mask ChromeMask) string {
	if mask.Help {
		return ""
	}
	return s.HelpView(r.sh)
}

func (r Router) helpHeightFor(s Screen, mask ChromeMask) int {
	return vheight(r.helpViewFor(s, mask))
}

// tabStripView renders the top-level tab strip (omitted when there's only one
// tab): the tab titles followed by a full-width rule that delimits it from the
// content below.
func (r Router) tabStripView() string {
	if len(r.tabs) < 2 {
		return ""
	}
	tabs := make([]string, len(r.tabs))
	for i, t := range r.tabs {
		if i == r.active {
			tabs[i] = activeTabStyle.Render(t.Title)
		} else {
			tabs[i] = tabStyle.Render(t.Title)
		}
	}
	row := tabStripStyle.Render(lipgloss.JoinHorizontal(lipgloss.Top, tabs...))
	if r.sh.width <= 0 {
		return row
	}
	rule := tabRuleStyle.Render(strings.Repeat("─", r.sh.width))
	return lipgloss.JoinVertical(lipgloss.Left, row, rule)
}

// tabSpans maps each tab to the cells it occupies in the rendered strip, so the
// click hit-test (tabClick) and the renderer can never disagree: tabStripStyle's
// 1-cell left padding, then each title plus its own 1-cell horizontal padding
// (tabStyle/activeTabStyle are identical but for color). The tabs are joined with
// no gap, so each span starts where the previous one ends. Computed from the
// titles rather than the rendered row, so a title containing spaces hit-tests
// exactly.
func (r Router) tabSpans() []crumbSpan {
	spans := make([]crumbSpan, len(r.tabs))
	x := 1 // tabStripStyle's left padding
	for i, t := range r.tabs {
		w := lipgloss.Width(t.Title) + 2 // the tab style's horizontal padding
		spans[i] = crumbSpan{x, x + w}
		x += w
	}
	return spans
}

// crumbTrail walks the live nav stack collecting breadcrumb segments (screens
// implementing Crumber with a non-empty full label), paired with each segment's
// stack index — the click hit-test (crumbClick) needs the index to know how far
// to Pop, since non-Crumber screens leave no segment to click.
func (r Router) crumbTrail() ([]Crumb, []int) {
	var crumbs []Crumb
	var idxs []int
	for i, s := range r.stack {
		c, ok := s.(Crumber)
		if !ok {
			continue
		}
		full := c.CrumbLabel(false)
		if full == "" {
			continue
		}
		crumbs = append(crumbs, Crumb{Full: full, Short: c.CrumbLabel(true)})
		idxs = append(idxs, i)
	}
	return crumbs, idxs
}

// breadcrumbView builds the breadcrumb bar from the live nav stack: it asks each
// screen implementing Crumber for its segment (root→top, the top screen full and the
// upstream ones short), skips empty ones, and hands the crumbs to Chrome.Breadcrumb
// to render (joined path + separator rule, gated by the pane's hidden flag). Built
// fresh each frame so it always reflects the current stack — pushing/popping needs no
// breadcrumb bookkeeping.
func (r Router) breadcrumbView() string {
	crumbs, _ := r.crumbTrail()
	var bc *BreadcrumbPane
	if r.sh.Chrome != nil {
		bc = r.sh.Chrome.Breadcrumb
	}
	return bc.view(crumbs, r.sh.width) // nil-safe: renders normally
}

// topChrome is the persistent chrome above the body: the header box, the tab strip
// (if any), and the breadcrumb bar below it, each gated by the active screen's mask.
// Its height is measured (not a constant) so adding/removing a part automatically
// reflows the body.
func (r Router) topChrome(mask ChromeMask) string {
	var parts []string
	if !mask.Header && r.sh.Chrome != nil {
		if header := r.sh.Chrome.Header.view(r.sh); header != "" { // nil-receiver safe
			parts = append(parts, header)
		}
	}
	if !mask.TabStrip {
		if strip := r.tabStripView(); strip != "" {
			parts = append(parts, strip)
		}
	}
	if !mask.Breadcrumb {
		if crumb := r.breadcrumbView(); crumb != "" {
			parts = append(parts, crumb)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// belowChrome is the chrome rendered between the active screen's body and the help
// bar: the status line (if any) then the output box (when shown), each gated by the
// screen's mask. Drawn by the router around every screen, so output/status persist
// across tab switches and screen pushes. Empty when there's neither.
func (r Router) belowChrome(mask ChromeMask) string {
	ch := r.sh.Chrome
	if ch == nil {
		return ""
	}
	var parts []string
	if !mask.Status && ch.Status != nil && ch.Status.Shown() {
		parts = append(parts, ch.Status.View())
	}
	if !mask.Output && r.outputVisible() {
		parts = append(parts, ch.Output.View(ch.outputFocused))
	}
	if len(parts) == 0 {
		return ""
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// vheight is lipgloss.Height but reports 0 for an empty string (lipgloss.Height("")
// is 1), so optional chrome contributes no rows when absent.
func vheight(s string) int {
	if s == "" {
		return 0
	}
	return lipgloss.Height(s)
}

// bodyHeightFor is the rows available to screen s's body: the space between the top
// chrome and the help bar, minus the status/output chrome below the body.
func (r Router) bodyHeightFor(s Screen) int {
	mask := r.maskOf(s)
	h := r.sh.height - vheight(r.topChrome(mask)) - vheight(r.belowChrome(mask)) - r.helpHeightFor(s, mask)
	if h < 1 {
		h = 1
	}
	return h
}

func (r Router) resize() {
	if r.sh.width == 0 {
		return
	}
	// Publish the body's absolute row so screens that hit-test mouse coordinates
	// (a ModularScreen focusing the pane under the cursor) can translate them.
	r.sh.bodyY = vheight(r.topChrome(r.currentMask()))
	// The output pane is router-owned chrome, so the router sizes it and keeps it
	// pinned to the newest line unless the user is scrolling it.
	if r.outputVisible() {
		r.sh.Chrome.Output.SetSize(r.sh.width, r.sh.height)
		if !r.sh.Chrome.outputFocused {
			r.sh.Chrome.Output.GotoBottom()
		}
	}
	// While overlays are up, the base screen below them is still drawn as the
	// background, so it must be kept sized too — otherwise it goes stale on resize.
	if base, bi := r.overlayBase(); bi != len(r.stack)-1 {
		base.SetSize(r.sh, r.sh.width, r.bodyHeightFor(base))
	}
	r.Top().SetSize(r.sh, r.sh.width, r.bodyHeightFor(r.Top()))
}

// frame composes the persistent chrome (header/tab strip above, status/output and
// help below) around screen s's body — the full-screen layout the router shows for
// the active screen, and the background it draws a popup over (see View).
func (r Router) frame(s Screen) string {
	sh := r.sh
	mask := r.maskOf(s)
	chrome := r.topChrome(mask)
	body := s.View(sh)
	below := r.belowChrome(mask)
	help := r.helpViewFor(s, mask)
	// Pad the body so the status/output chrome and the always-visible help bar sit
	// at the very bottom. Clamp an overflowing body to the same allocation so the
	// terminal renderer never has to recover by dropping rows from the frame's top.
	avail := sh.height - vheight(chrome) - vheight(below) - vheight(help)
	if pad := avail - lipgloss.Height(body); pad > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, body, Blanks(pad))
	} else if pad < 0 {
		body = lipgloss.NewStyle().MaxHeight(avail).Render(body)
	}
	var parts []string
	if chrome != "" {
		parts = append(parts, chrome)
	}
	parts = append(parts, body)
	if below != "" {
		parts = append(parts, below)
	}
	if help != "" {
		parts = append(parts, help)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (r Router) View() tea.View {
	// Overlays (popups, floating line edits) STACK: the base is the deepest screen
	// below the top that isn't an overlay, framed whole, and each overlay above it
	// is composited on bottom-first — so a popup pushed over a floating line edit
	// lands over both, with the base's chrome and panes intact underneath. Only
	// the top screen receives input, so the chain stays modal. A box is centered
	// unless its overlay implements OverlayPositioner (a floating edit anchored
	// over the element it covers); either way the position clamps into the frame
	// so a box near an edge stays fully on screen.
	base, bi := r.overlayBase()
	out := r.frame(base)
	for i := bi + 1; i < len(r.stack); i++ {
		box := r.stack[i].View(r.sh)
		bw, bh := lipgloss.Width(box), lipgloss.Height(box)
		var x, y int
		if p, ok := r.stack[i].(OverlayPositioner); ok {
			x, y = p.OverlayPos(bw, bh)
		} else {
			x = (r.sh.width - bw) / 2
			y = (r.sh.height - bh) / 2
		}
		x = max(0, min(x, r.sh.width-bw))
		y = max(0, min(y, r.sh.height-bh))
		out = Composite(out, box, x, y)
	}

	// Alt screen and mouse reporting are view state in v2, not program options: what
	// the last View asked for is what the terminal is put into. mouseOn is the ctrl+g
	// toggle (see globalKey) — cell motion reports the wheel and clicks but only
	// streams motion while a button is held, so there is no hover traffic through
	// Update. It costs the terminal's own drag-select, which is why the key exists.
	v := tea.NewView(out)
	v.AltScreen = true
	if r.mouseOn {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

// overlayBase returns the deepest screen below the top that is NOT an overlay —
// the screen the overlay chain is composited over — and its stack index. It is
// the top screen itself when the top isn't an overlay (the common case: no popup
// up), so single-screen and single-overlay stacks render exactly as before.
func (r Router) overlayBase() (Screen, int) {
	i := len(r.stack) - 1
	for i > 0 {
		o, ok := r.stack[i].(Overlayer)
		if !ok || !o.IsOverlay() {
			break
		}
		i--
	}
	return r.stack[i], i
}
