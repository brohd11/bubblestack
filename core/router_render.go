package core

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// chromeCache memoizes the top chrome (header, tab strip, breadcrumb) for one message:
// it was rendered three times per keystroke and dominated the router's cost. resize()
// resets it after the message's Actions apply, so View reuses exactly the strings the
// body was sized from. Only the top chrome is cached; the status line and output pane are
// cheap and stay live. Two slots cover the top screen's mask and an overlay base's; a
// third renders uncached.
type chromeCache struct {
	valid bool
	n     int
	slots [2]chromeSlot
}

type chromeSlot struct {
	mask ChromeMask
	top  string
	set  bool
}

// reset opens a new message's cache. Called from resize(), which every Update path
// reaches after applying whatever the message changed.
func (c *chromeCache) reset() {
	c.valid, c.n = true, 0
	c.slots = [2]chromeSlot{}
}

// slot is the entry for mask, or nil when there is nothing to cache into — before the
// first resize, or past the two masks a frame can hold.
func (c *chromeCache) slot(mask ChromeMask) *chromeSlot {
	if !c.valid {
		return nil
	}
	for i := range c.n {
		if c.slots[i].mask == mask {
			return &c.slots[i]
		}
	}
	if c.n == len(c.slots) {
		return nil
	}
	c.slots[c.n] = chromeSlot{mask: mask}
	c.n++
	return &c.slots[c.n-1]
}

// maskOf is s's ChromeMask, or the zero mask. It takes the screen so an overlay's base can
// be framed with its own mask.
func (r Router) maskOf(s Screen) ChromeMask {
	if m, ok := s.(ChromeMasker); ok {
		return m.ChromeMask()
	}
	return ChromeMask{}
}

// currentMask is the mask of the frame on screen: the overlay base's while overlays are
// up, since View frames that screen and only composites the overlays over it. Geometry
// (BodyY, overlay sizing, chrome hit-tests) must follow what is drawn, not the top.
func (r Router) currentMask() ChromeMask {
	base, _ := r.overlayBase()
	return r.maskOf(base)
}

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

// tabStripView renders the tab titles and a full-width rule (nothing with one tab).
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

// tabSpans maps each tab to its cells in the strip, computed from the titles so the
// renderer and tabClick agree exactly.
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

// crumbTrail collects breadcrumb segments with their stack indexes, which crumbClick
// needs to know how far to pop.
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

// breadcrumbView builds the breadcrumb bar from the live stack each frame (top screen
// full, upstream short), so push and pop need no bookkeeping.
func (r Router) breadcrumbView() string {
	crumbs, _ := r.crumbTrail()
	var bc *BreadcrumbPane
	if r.sh.Chrome != nil {
		bc = r.sh.Chrome.Breadcrumb
	}
	return bc.view(crumbs, r.sh.width) // nil-safe: renders normally
}

// topChrome is the header, tab strip and breadcrumb, each gated by mask. Its height is
// measured, so the body reflows when a part comes or goes.
func (r Router) topChrome(mask ChromeMask) string {
	slot := r.sh.chrome.slot(mask)
	if slot == nil {
		return r.renderTopChrome(mask)
	}
	if !slot.set {
		slot.top, slot.set = r.renderTopChrome(mask), true
	}
	return slot.top
}

func (r Router) renderTopChrome(mask ChromeMask) string {
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

// belowChrome is the status line and output pane between the body and the help bar, each
// gated by mask. The router draws it around every screen, so it persists across tabs and
// pushes.
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
	h := max(r.sh.height-vheight(r.topChrome(mask))-vheight(r.belowChrome(mask))-r.helpHeightFor(s, mask), 1)
	return h
}

func (r Router) resize() {
	// Open this message's chrome cache first: every render below, and every one View
	// makes after it, is then one message's worth of truth (see chromeCache).
	r.sh.chrome.reset()
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
	// background, so it must be kept sized too — otherwise it goes stale on resize. The
	// overlay gets the base's body height too: it is composited into that frame.
	base, bi := r.overlayBase()
	if bi != len(r.stack)-1 {
		base.SetSize(r.sh, r.sh.width, r.bodyHeightFor(base))
	}
	r.Top().SetSize(r.sh, r.sh.width, r.bodyHeightFor(base))
}

// frame composes the chrome around screen s's body: the full layout, and the background
// an overlay is drawn over.
func (r Router) frame(s Screen) string {
	sh := r.sh
	mask := r.maskOf(s)
	chrome := r.topChrome(mask)
	body := s.View(sh)
	below := r.belowChrome(mask)
	help := r.helpViewFor(s, mask)
	// Pad the body so the status, output and help sit at the bottom, and clamp an overflowing
	// body so the terminal never drops rows from the top.
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
	// Overlays stack: the deepest non-overlay screen is framed whole and each overlay above
	// is composited bottom-first, centered unless it is an OverlayPositioner, clamped into
	// the frame.
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

	// Alt screen and mouse reporting are View state in v2. mouseOn is the mouse toggle; cell
	// motion reports wheel and clicks, and motion only while a button is held.
	v := tea.NewView(out)
	v.AltScreen = true
	v.ReportFocus = true
	if r.mouseOn {
		v.MouseMode = tea.MouseModeCellMotion
		if mw, ok := r.Top().(MotionWanter); ok && mw.WantsAllMotion() {
			v.MouseMode = tea.MouseModeAllMotion
		}
	}
	return v
}

// overlayBase returns the deepest non-overlay screen below the top and its index (the top
// itself when no overlay is up).
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
