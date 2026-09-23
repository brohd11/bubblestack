package core

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Chrome is the optional furniture around the active screen's body: a header box, a
// status line and an output pane. Each is optional (nil is not drawn) and toggleable at
// runtime, and a screen can hide any while on top (ChromeMasker). A nil Shared.Chrome
// renders just body and help bar. Header is a closure and Output an interface, so core
// names no domain type.
type Chrome struct {
	Header     *HeaderPane     // nil ⇒ no header box
	Breadcrumb *BreadcrumbPane // nil ⇒ breadcrumb still drawn (default, shown); set to toggle it
	Output     Output          // nil ⇒ no output pane (default impl: components.LogPane)
	Status     Status          // nil ⇒ no status line (default impl: components.StatusLine)

	// outputFocused routes input to the output pane instead of the screen. The router owns
	// it; the pane only renders the flag View is given.
	outputFocused bool
}

// visibility is the runtime hidden flag shared by the toggleable chrome panes.
type visibility struct{ hidden bool }

func (v *visibility) Hide()        { v.hidden = true }
func (v *visibility) Show()        { v.hidden = false }
func (v *visibility) Toggle()      { v.hidden = !v.hidden }
func (v *visibility) Hidden() bool { return v.hidden }

// HeaderPane wraps a consumer's header renderer with a runtime hidden flag.
type HeaderPane struct {
	Render func(*Shared) string
	// OnClick fires on a left click in the header box, in terminal cells (the header is
	// topmost, so y is also the header-local row). Nil ⇒ clicks fall through.
	OnClick func(sh *Shared, x, y int) Action
	visibility
}

// NewHeaderPane wraps a header renderer (the closure a consumer supplies). The
// facade's Run builds this from Config.Header.
func NewHeaderPane(render func(*Shared) string) *HeaderPane { return &HeaderPane{Render: render} }

// view renders the header, or "" when the pane is nil, hidden, or has no renderer —
// so the router measures/draws it uniformly. Nil-receiver safe.
func (h *HeaderPane) view(s *Shared) string {
	if h == nil || h.hidden || h.Render == nil {
		return ""
	}
	return h.Render(s)
}

// BreadcrumbPane carries the runtime hidden flag for the router-drawn breadcrumb bar.
type BreadcrumbPane struct {
	visibility
}

// NewBreadcrumbPane returns a shown breadcrumb pane. The facade's Run sets this on
// Chrome so a consumer can sh.Chrome.Breadcrumb.Hide() it.
func NewBreadcrumbPane() *BreadcrumbPane { return &BreadcrumbPane{} }

// view renders the breadcrumb bar and a rule under it, or "" when hidden or empty. A nil
// pane renders as shown.
func (b *BreadcrumbPane) view(crumbs []Crumb, width int) string {
	if b != nil && b.hidden {
		return ""
	}
	bar := RenderBreadcrumb(crumbs, width)
	if bar == "" || width <= 0 {
		return bar
	}
	rule := breadcrumbRuleStyle.Render(strings.Repeat("─", width))
	return lipgloss.JoinVertical(lipgloss.Left, bar, rule)
}

// Crumb is one segment of the router-drawn breadcrumb: a full label and an optional
// shorter form used when the trail is too wide. Short falls back to Full when empty.
type Crumb struct {
	Full  string
	Short string
}

func (c Crumb) pick(short bool) string {
	if short && c.Short != "" {
		return c.Short
	}
	return c.Full
}

// crumbSep separates breadcrumb segments.
const crumbSep = " › "

// RenderBreadcrumb renders the bar: upstream segments muted, the current one accented.
// When too wide it retries with short forms for all but the last, then left-truncates,
// keeping the current segment visible.
func RenderBreadcrumb(crumbs []Crumb, width int) string {
	if len(crumbs) == 0 {
		return ""
	}
	chosen, truncated := crumbLabels(crumbs, width)
	if truncated {
		// Last resort: truncate the whole trail, keeping the tail (current segment).
		return breadcrumbBarStyle.Render(crumbMutedStyle.Render(TruncLeft(strings.Join(chosen, crumbSep), width-2)))
	}
	last := len(chosen) - 1
	parts := make([]string, len(chosen))
	for i, l := range chosen {
		if i == last {
			parts[i] = crumbCurStyle.Render(l)
		} else {
			parts[i] = crumbMutedStyle.Render(l)
		}
	}
	return breadcrumbBarStyle.Render(strings.Join(parts, crumbMutedStyle.Render(crumbSep)))
}

// crumbLabels picks full or short labels to fit width, shared by the renderer and
// crumbSpans. truncated reports the left-truncated fallback, where no spans exist.
func crumbLabels(crumbs []Crumb, width int) (chosen []string, truncated bool) {
	last := len(crumbs) - 1
	labels := func(short bool) []string {
		out := make([]string, len(crumbs))
		for i, c := range crumbs {
			out[i] = c.pick(short && i != last)
		}
		return out
	}
	avail := width - 2 // breadcrumbBarStyle's horizontal padding
	chosen = labels(false)
	if width > 0 && lipgloss.Width(strings.Join(chosen, crumbSep)) > avail {
		chosen = labels(true)
	}
	if width > 0 && lipgloss.Width(strings.Join(chosen, crumbSep)) > avail {
		return chosen, true
	}
	return chosen, false
}

// crumbSpan is one segment's clickable x range in terminal cells, [start, end).
type crumbSpan struct{ start, end int }

// crumbSpans maps each crumb to its cells in the rendered bar; ok is false when the bar
// is truncated.
func crumbSpans(crumbs []Crumb, width int) (spans []crumbSpan, ok bool) {
	chosen, truncated := crumbLabels(crumbs, width)
	if truncated || len(chosen) == 0 {
		return nil, false
	}
	spans = make([]crumbSpan, len(chosen))
	x := 1 // breadcrumbBarStyle's left padding
	for i, l := range chosen {
		w := lipgloss.Width(l)
		spans[i] = crumbSpan{x, x + w}
		x += w + lipgloss.Width(crumbSep)
	}
	return spans, true
}

// Output is the pluggable pane below the body, which the router renders, sizes and feeds
// scroll keys while focused. The default is components.NewLogPane. Logging (Log) and
// wrapping (Wrapper) are optional capabilities found by type assertion.
type Output interface {
	Shown() bool                       // occupies layout space when true
	Toggle()                           // show/hide (the Output key, `o`)
	Hide()                             // collapse (e.g. focus returning to the body)
	Clear()                            // drop contents (the Clear key)
	SetSize(termWidth, termHeight int) // lay out to the terminal; the pane picks its own height
	Height() int                       // rows occupied when shown (0 when hidden)
	View(focused bool) string          // render (focused ⇒ scroll affordance)
	Update(msg tea.Msg) tea.Cmd        // handle a key while focused (scrolling)
	GotoBottom()                       // pin to the newest content
	GotoTop()                          // pin to the oldest content
	Log(line string, forceShow bool)
}

// Wrapper is an Output's optional wrap mode for lines wider than the box, reached by type
// assertion on Keys.Wrap.
type Wrapper interface {
	ToggleWrap()
	Wrapped() bool
}

// Status is the pluggable one-line status below the body (default
// components.NewStatusLine). The router clears it on the Clear key or an auto-clear timer
// keyed on Gen, so an old timer never clears a newer message.
type Status interface {
	Set(line string) // replace the message and bump the generation
	Clear()          // drop the message (does NOT bump the generation)
	Shown() bool     // occupies layout space (non-empty message)
	Height() int     // rows occupied when shown (0 when empty)
	View() string    // render the themed line
	Gen() int        // current generation; the auto-clear timer compares against this
}
