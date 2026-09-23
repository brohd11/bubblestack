package core

import (
	"fmt"
	"image/color"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---------- header ----------

// borderCells is the cells a left+right border costs; lipgloss v2 counts them inside
// Style.Width.
const borderCells = 2

// HeaderInnerWidth is the content width inside the persistent context box for a
// terminal of the given width, so a Header closure can size/truncate values to fit.
func HeaderInnerWidth(width int) int {
	return max(width-4, 20) // minus border (2) and padding (2)
}

// headerPadding is headerStyle's horizontal padding, which lipgloss counts inside the
// width HeaderBox passes.
const headerPadding = 2

// HeaderValueWidth is the room a Header body has for the value on a line beginning with
// label. Pass the label itself so wide runes and styling are measured correctly.
func HeaderValueWidth(width int, label string) int {
	v := HeaderInnerWidth(width) - headerPadding - lipgloss.Width(label)
	if v < 4 {
		v = 4 // TruncLeft's own floor; below this nothing legible survives anyway
	}
	return v
}

// HeaderBox renders body inside the bordered context box sized to the terminal. A Header
// closure builds body (Label + TruncLeft) and returns HeaderBox(sh.Width(), body).
func HeaderBox(width int, body string) string {
	return headerStyle.Width(HeaderInnerWidth(width) + borderCells).Render(body)
}

// Label renders a context-box/field label in the muted label style.
func Label(s string) string { return labelStyle.Render(s) }

// Value renders a context-box field value in the log (near-white) style.
func Value(s string) string { return logStyle.Render(s) }

// TruncLeft keeps the right (most informative) end of a path, prefixing "…".
func TruncLeft(s string, n int) string {
	n = max(n, 4)
	w := ansi.StringWidth(s)
	if w <= n {
		return s
	}
	return ansi.TruncateLeft(s, w-(n-1), "…")
}

// ---------- breadcrumb / title bars ----------

// RenderTitleBar renders text as a list-style title bar. It is two rows (bubbles'
// TitleBar has a bottom padding of 1), like a real list's title section, so anything
// mapping rows to items must measure it (see components.listHeaderHeight).
func RenderTitleBar(text string) string {
	return listStyles.TitleBar.Render(listStyles.Title.Render(text))
}

// renderTitleBarMuted is RenderTitleBar for an unfocused element. Only colors change, so
// a focus flip cannot shift the body.
func renderTitleBarMuted(text string) string {
	muted := listStyles.Title.UnsetBackground().Foreground(MutedColor)
	return listStyles.TitleBar.Render(muted.Render(text))
}

// WithTitle prepends a title bar to body, or returns body unchanged for an empty title.
func WithTitle(title, body string) string {
	if title == "" {
		return body
	}
	return lipgloss.JoinVertical(lipgloss.Left, RenderTitleBar(title), body)
}

// WithTitleFocused is WithTitle with a muted bar when unfocused, for screens nested in a
// ModularScreen.
func WithTitleFocused(title, body string, focused bool) string {
	if title == "" {
		return body
	}
	if focused {
		return WithTitle(title, body)
	}
	return lipgloss.JoinVertical(lipgloss.Left, renderTitleBarMuted(title), body)
}

// ---------- confirm/summary box ----------

// confirmWidth is the inner width of the boxed confirm/input screens, sized to
// the terminal with a sane floor.
func (s *Shared) ConfirmWidth() int {
	inner := max(s.width-10, 24)
	return inner
}

// box renders body inside the shared bordered confirm/summary box.
func (s *Shared) Box(body string) string {
	return boxStyle.Width(s.ConfirmWidth() + borderCells).Render(body)
}

// BoxFocused is Box with the border in FocusedColor when focused, for screens nested in a
// ModularScreen.
func (s *Shared) BoxFocused(body string, focused bool) string {
	color := BorderColor
	if focused {
		color = FocusedColor
	}
	return boxStyle.BorderForeground(color).Width(s.ConfirmWidth() + borderCells).Render(body)
}

// BoxInnerWidth is the widest body line before Box wraps it, derived from boxStyle so a
// caller sizing content to fit stays in step with the padding.
func (s *Shared) BoxInnerWidth() int {
	return s.ConfirmWidth() - boxStyle.GetHorizontalPadding()
}

// BoxOrigin is the offset of a Box's first content cell from the box's top-left, for
// anchoring an overlay to a row inside a box. Derived from boxStyle.
func BoxOrigin() (x, y int) {
	return boxStyle.GetMarginLeft() + boxStyle.GetBorderLeftSize() + boxStyle.GetPaddingLeft(),
		boxStyle.GetMarginTop() + boxStyle.GetBorderTopSize() + boxStyle.GetPaddingTop()
}

// ---------- help bars ----------

// helpView renders a list's own help bar on its own, so it can be placed below
// the status and output panes.
func HelpView(l list.Model) string {
	return l.Styles.HelpStyle.Render(l.Help.View(l))
}

// NewSelectList builds a list styled like the others (no status bar, help drawn
// separately). It is zero-sized until the owner's SetSize.
func NewSelectList(items []list.Item, title string, extra ...key.Binding) list.Model {
	return newSelectList(items, title, NewDelegate(), extra...)
}

// SuffixItem is the row contract for a compact list: Title, then SuffixText in the muted
// color. It is optional: a list.DefaultItem row renders compactly with its description as
// the suffix, which lets one list flip density at runtime.
type SuffixItem interface {
	list.Item
	Title() string
	SuffixText() string
}

// compactText resolves a compact row's title and suffix from SuffixItem or, failing that,
// list.DefaultItem. ok is false exactly when the default delegate could not render it
// either.
func compactText(item list.Item) (title, suffix string, ok bool) {
	switch i := item.(type) {
	case SuffixItem:
		return i.Title(), i.SuffixText(), true
	case list.DefaultItem:
		return i.Title(), i.Description(), true
	}
	return "", "", false
}

// MarkItem is an optional compact-row flag (gote's "(*)" for unsaved changes) pinned at
// the right. Its cells are reserved before the title is truncated, so it survives narrow
// columns and stays put during a marquee. Keep it short: every row pays its width.
type MarkItem interface{ Mark() string }

// PrefixItem is optional compact-row text pinned at the left edge (TreePanel's indent and
// disclosure markers). It is not filtered on and does not marquee.
type PrefixItem interface{ PrefixText() string }

// ColorItem is an optional row contract naming the row's own foreground (e.g. directories
// vs files). The selection accent outranks it unless the row is a KeepColorItem; itemColor
// holds the precedence for both delegates.
type ColorItem interface{ TitleColor() color.Color }

// KeepColorItem opts a colored row out of the selection accent, for colors the reader
// wants under the cursor (gote's git state). The accent border still marks the selection.
// An opted-out row without a color uses the normal title color. Dimming still applies.
type KeepColorItem interface{ KeepColor() bool }

// itemColor is the foreground a delegate should apply to a row, and false to leave its
// style alone. normal is an unselected row's foreground.
func itemColor(item list.Item, isSelected, dimmed bool, normal color.Color) (color.Color, bool) {
	if dimmed {
		return nil, false
	}
	keep := false
	if k, ok := item.(KeepColorItem); ok {
		keep = k.KeepColor()
	}
	if isSelected && !keep {
		return nil, false
	}
	if ci, ok := item.(ColorItem); ok {
		if c := ci.TitleColor(); c != nil {
			return c, true
		}
	}
	if isSelected {
		return normal, true
	}
	return nil, false
}

// NewCompactList is NewSelectList with a one-row delegate: title plus optional muted
// suffix.
func NewCompactList(items []list.Item, title string, extra ...key.Binding) list.Model {
	return newSelectList(items, title, CompactDelegate{}, extra...)
}

// RenderFilter is the filter heading for lists that keep an applied filter visible: the
// live input while editing, then the prompt style with the value unstyled.
func RenderFilter(l *list.Model) string {
	switch l.FilterState() {
	case list.Filtering:
		return l.FilterInput.View()
	case list.FilterApplied:
		in := l.FilterInput
		// The input is blurred once the filter is accepted, so the blurred prompt is
		// the one bubbles would have drawn had it kept rendering the input itself.
		return in.Styles().Blurred.Prompt.Render(in.Prompt) + in.Value()
	default:
		return ""
	}
}

// RenderList renders a full-screen list keeping an applied filter in its title bar.
// bubbles restores l.Title once a filter is accepted, so a copy is rendered instead, with
// its Title style cleared so it does not paint over RenderFilter.
func RenderList(l list.Model) string {
	if l.ShowTitle() && l.FilterState() == list.FilterApplied {
		l.Title = RenderFilter(&l)
		l.Styles.Title = lipgloss.NewStyle()
	}
	return l.View()
}

func newSelectList(items []list.Item, title string, delegate list.ItemDelegate, extra ...key.Binding) list.Model {
	l := list.New(items, delegate, 0, 0)
	if title != "" {
		l.Title = title
	} else {
		l.SetShowTitle(false)
	}
	StyleList(&l)
	keys := func() []key.Binding {
		return append([]key.Binding{
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
			key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		}, extra...)
	}
	// Only the full help matters to screens using ShortHelp, which builds its own short
	// entries; the short keys serve screens that render bubbles' help directly.
	l.AdditionalShortHelpKeys = keys
	l.AdditionalFullHelpKeys = keys
	return l
}

// CompactDelegate renders one item per row with no spacing; the title gets width priority
// over the suffix. A non-nil Offset marquees the selected row when it overflows, windowing
// title and suffix as one string. The owner (ListPanel) advances the offset; Render stays
// pure.
type CompactDelegate struct{ Offset *int }

func (CompactDelegate) Height() int                         { return 1 }
func (CompactDelegate) Spacing() int                        { return 0 }
func (CompactDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// CompactRow is one row's raw, untruncated pieces. Prefix and Mark stay pinned at the
// left and right edges; Title and Tail are the portion that slides under a marquee.
type CompactRow struct{ Prefix, Title, Tail, Mark string }

// Width is the row's full untruncated width including the mark; minus the text width it
// is the marquee's last offset.
func (r CompactRow) Width() int {
	return lipgloss.Width(r.Prefix) + lipgloss.Width(r.Title) + lipgloss.Width(r.Tail) + lipgloss.Width(r.Mark)
}

func (r CompactRow) movingWidth() int { return lipgloss.Width(r.Title) + lipgloss.Width(r.Tail) }

// MarqueeLimit is the last useful offset once prefix and mark take their cells, shared by
// CompactDelegate and ListPanel so the clock never runs into clamped frames.
func (r CompactRow) MarqueeLimit(textWidth int) int {
	prefixWidth := min(lipgloss.Width(r.Prefix), max(textWidth-lipgloss.Width(r.Mark)-1, 0))
	available := max(textWidth-prefixWidth-lipgloss.Width(r.Mark), 1)
	return max(r.movingWidth()-available, 0)
}

// CompactTextWidth is the text width of a compact row in a list of listWidth, exported so
// the marquee driver measures against the same number Render uses.
func CompactTextWidth(listWidth int) int {
	s := list.NewDefaultItemStyles(isDark).NormalTitle
	if w := listWidth - s.GetPaddingLeft() - s.GetPaddingRight(); w > 1 {
		return w
	}
	return 1
}

// CompactMarquee returns a row's pieces and whether they overflow textWidth: the one place
// that decides whether a row scrolls, shared by the offset owner and the delegate. ok is
// false for a row neither delegate can render.
func CompactMarquee(item list.Item, textWidth int) (CompactRow, bool) {
	title, suffix, ok := compactText(item)
	if !ok {
		return CompactRow{}, false
	}
	r := CompactRow{Title: title}
	if p, ok := item.(PrefixItem); ok {
		r.Prefix = p.PrefixText()
	}
	if suffix != "" {
		r.Tail = "  " + suffix
	}
	// The mark is read here, not in Render, so the marquee driver and the delegate agree on
	// the width left to scroll.
	if m, ok := item.(MarkItem); ok {
		r.Mark = m.Mark()
	}
	return r, r.MarqueeLimit(textWidth) > 0
}

func (d CompactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if _, _, ok := compactText(item); !ok || m.Width() <= 0 {
		return
	}

	styles := list.NewDefaultItemStyles(isDark)
	styles.SelectedTitle = styles.SelectedTitle.Foreground(FocusedColor).BorderForeground(FocusedColor)
	styles.NormalDesc = styles.NormalDesc.Foreground(MutedColor)
	styles.DimmedDesc = styles.DimmedDesc.Foreground(MutedColor)

	textWidth := max(m.Width()-styles.NormalTitle.GetPaddingLeft()-styles.NormalTitle.GetPaddingRight(), 1)

	// Structural prefixes and marks keep their edges while the title and suffix share the
	// cells between them.
	raw, over := CompactMarquee(item, textWidth)
	prefix := ansi.Truncate(raw.Prefix, max(textWidth-lipgloss.Width(raw.Mark)-1, 0), "")
	fitWidth := max(textWidth-lipgloss.Width(prefix)-lipgloss.Width(raw.Mark), 1)

	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""
	isFiltered := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied
	isSelected := index == m.Index() && m.FilterState() != list.Filtering

	title, suffix := "", ""
	// Marquee the overflowing selected row, but never while filtering: match highlighting
	// addresses the title by rune index, which windowing would break.
	if over && d.Offset != nil && isSelected && !isFiltered {
		off := min(max(*d.Offset, 0), raw.MarqueeLimit(textWidth))
		title = marqueeSeg(raw.Title, 0, off, fitWidth)
		suffix = marqueeSeg(raw.Tail, lipgloss.Width(raw.Title), off, fitWidth)
	} else {
		title = ansi.Truncate(raw.Title, fitWidth, "…")
		if raw.Tail != "" {
			if remaining := fitWidth - lipgloss.Width(title) - 2; remaining > 0 {
				suffix = "  " + ansi.Truncate(strings.TrimPrefix(raw.Tail, "  "), remaining, "…")
			}
		}
	}

	titleStyle := styles.NormalTitle
	if emptyFilter {
		titleStyle = styles.DimmedTitle
	} else if isSelected {
		titleStyle = styles.SelectedTitle
	}
	// Recolor before the highlight pass so matched runes inherit the row's color. It is a
	// style foreground on already-truncated text, so no width computation changes.
	if c, ok := itemColor(item, isSelected, emptyFilter, styles.NormalTitle.GetForeground()); ok {
		titleStyle = titleStyle.Foreground(c)
	}
	if isFiltered && !emptyFilter && index < len(m.VisibleItems()) {
		matched := titleStyle.Inline(true).Inherit(styles.FilterMatch)
		title = lipgloss.StyleRunes(title, m.MatchesForItem(index), matched, titleStyle.Inline(true))
	}

	muted := MutedStyle()
	if emptyFilter {
		muted = styles.DimmedDesc.Inline(true)
	}
	// The mark is appended after the highlight pass (it is not part of the matched name) and
	// inline, because titleStyle would draw a second border.
	mark := ""
	if raw.Mark != "" {
		mark = titleStyle.Inline(true).Render(raw.Mark)
	}
	fmt.Fprint(w, titleStyle.Render(prefix+title)+muted.Render(suffix)+mark) //nolint:errcheck
}

// ColorDelegate is the three-row delegate with per-row colors (ColorItem). A
// KeepColorItem keeps its color under the cursor; the description line still takes the
// accent. It wraps DefaultDelegate.Render, whose value receiver makes the style changes
// local to one row, so row geometry and layout stay upstream's.
type ColorDelegate struct{ list.DefaultDelegate }

func (d ColorDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	isSelected := index == m.Index() && m.FilterState() != list.Filtering
	dimmed := m.FilterState() == list.Filtering && m.FilterValue() == ""
	if c, ok := itemColor(item, isSelected, dimmed, d.Styles.NormalTitle.GetForeground()); ok {
		// Recolor the selected row too, so KeepColorItem can shed the accent; the tinted border
		// still marks the cursor.
		if isSelected {
			d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(c)
		} else {
			d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(c)
		}
	}
	d.DefaultDelegate.Render(w, m, index, item)
}

// NewDelegate is the shared three-row delegate: muted descriptions and the selected row
// in the theme accent instead of bubbles' hardcoded pink.
func NewDelegate() ColorDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(MutedColor)
	d.Styles.DimmedDesc = d.Styles.DimmedDesc.Foreground(MutedColor)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(FocusedColor).BorderForeground(FocusedColor)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(FocusedColor).BorderForeground(FocusedColor)
	return ColorDelegate{d}
}

// styleList applies the shared list config: hide the built-in status bar and
// help (help is drawn manually at the bottom), and brighten the help colors.
func StyleList(l *list.Model) {
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	// Theme the list's own title bar to match the breadcrumb (RenderTitleBar)
	// instead of bubbles' default purple.
	l.Styles.Title = listStyles.Title
	// The filter prompt keeps bubbles' adaptive yellow. Re-apply it from the live styles so a
	// theme change reaches FilterInput too.
	l.FilterInput.SetStyles(listStyles.Filter)
	// Drive list scrolling from the central keymap so an added scheme (e.g. wasd)
	// reaches lists too; FullHint keeps the list's own full (?) help reading well.
	l.KeyMap.CursorUp = FullHint("up", Keys.Up)
	l.KeyMap.CursorDown = FullHint("down", Keys.Down)
	l.KeyMap.PrevPage = FullHint("prev page", Keys.Left)
	l.KeyMap.NextPage = FullHint("next page", Keys.Right)
	// Quitting is owned by the router's global q handler; drop the list's built-in
	// q/esc quit so esc at a tab root is a no-op (back is spam-safe to the root).
	l.KeyMap.Quit = key.NewBinding()
	l.Help.Styles.ShortKey = l.Help.Styles.ShortKey.Foreground(MutedColor)
	l.Help.Styles.ShortDesc = l.Help.Styles.ShortDesc.Foreground(MutedColor)
	l.Help.Styles.ShortSeparator = l.Help.Styles.ShortSeparator.Foreground(MutedColor)
	l.Help.Styles.FullKey = l.Help.Styles.FullKey.Foreground(MutedColor)
	l.Help.Styles.FullDesc = l.Help.Styles.FullDesc.Foreground(MutedColor)
	l.Help.Styles.FullSeparator = l.Help.Styles.FullSeparator.Foreground(MutedColor)
}

// helpMode selects a tab root's help-bar preset. The zero value is the decluttered
// minimal bar (nav · select · quit · more); helpTabbed adds the [ ] tab-switch hint.
type HelpMode int

const (
	HelpMinimal HelpMode = iota
	HelpTabbed
)

// ShortHelp renders a tab root's short help for the given preset. The full (?) help is
// laid out in four columns (nav, actions, filter, chrome); the router-owned chrome column
// is added centrally, and actions that duplicate it are dropped (excludeKeys).
//
// The short bar is deliberately fixed: move, select, back or tabs, and "? more". New
// commands go in the (?) menu (via NewSelectList's extra keys), never on the bar. The same
// rule applies to panel help merged into a ModularScreen's bar.
func ShortHelp(l list.Model, mode HelpMode) string {
	if l.Help.ShowAll {
		nav := []key.Binding{
			l.KeyMap.CursorUp, l.KeyMap.CursorDown,
			l.KeyMap.NextPage, l.KeyMap.PrevPage,
			l.KeyMap.GoToStart, l.KeyMap.GoToEnd,
		}
		filter := []key.Binding{
			l.KeyMap.Filter, l.KeyMap.ClearFilter,
			l.KeyMap.AcceptWhileFiltering, l.KeyMap.CancelWhileFiltering,
		}
		chrome := []key.Binding{
			FullHint("focus log", Keys.ToggleOutput),
			FullHint("toggle log", Keys.Output),
			FullHint("wrap", Keys.Wrap),
			FullHint("clear log", Keys.Clear),
			FullHint("refresh", Keys.Refresh),
			FullHint("mouse", Keys.Mouse),
			FullHint("quit", Keys.Quit),
			l.KeyMap.CloseFullHelp,
		}
		var actions []key.Binding
		if l.AdditionalFullHelpKeys != nil {
			actions = excludeKeys(l.AdditionalFullHelpKeys(), chrome)
		}
		cols := [][]key.Binding{nav, actions, filter, chrome}
		return l.Styles.HelpStyle.Render(l.Help.FullHelpView(cols))
	}
	short := []key.Binding{
		// One entry for the arrows: "move" on a list, "scroll" on a viewport.
		Hint("move", Keys.Up, Keys.Down),
		Hint("select", Keys.Select),
	}
	switch mode {
	case HelpTabbed:
		short = append(short, tabHint())
	case HelpMinimal:
		short = append(short, Hint("back", Keys.Back))
	}
	short = append(short, l.KeyMap.ShowFullHelp)
	return l.Styles.HelpStyle.Render(l.Help.ShortHelpView(short))
}

// excludeKeys drops the binds whose keycodes overlap any in exclude.
func excludeKeys(binds, exclude []key.Binding) []key.Binding {
	skip := map[string]bool{}
	for _, b := range exclude {
		for _, k := range b.Keys() {
			skip[k] = true
		}
	}
	return slices.DeleteFunc(slices.Clone(binds), func(b key.Binding) bool {
		return slices.ContainsFunc(b.Keys(), func(k string) bool { return skip[k] })
	})
}

// styleHelp restyles the static help model from the live MutedColor, per call, so it
// tracks theme changes.
func (s *Shared) styleHelp() {
	s.help.Styles.ShortKey = s.help.Styles.ShortKey.Foreground(MutedColor)
	s.help.Styles.ShortDesc = s.help.Styles.ShortDesc.Foreground(MutedColor)
	s.help.Styles.ShortSeparator = s.help.Styles.ShortSeparator.Foreground(MutedColor)
}

// bindingHelp renders a set of key bindings as a static help bar aligned with
// the real list help bars (used by confirm / form / task screens).
func (s *Shared) BindingHelp(bindings []key.Binding) string {
	s.styleHelp()
	return listStyles.HelpStyle.Render(s.help.ShortHelpView(bindings))
}

// noteHelp renders a plain (non-interactive) note in the help bar position.
func (s *Shared) NoteHelp(text string) string {
	s.styleHelp()
	return listStyles.HelpStyle.Render(s.help.Styles.ShortDesc.Render(text))
}

// ---------- text helpers ----------

// marqueeSeg returns the part of seg inside the window [offset, offset+width), where seg
// starts at cell start of the whole row. Windowing each styled piece against one offset
// lets title and suffix slide as one string. No ellipsis: the motion shows there is more.
func marqueeSeg(seg string, start, offset, width int) string {
	lo, hi := offset, offset+width
	if start > lo {
		lo = start
	}
	if end := start + lipgloss.Width(seg); end < hi {
		hi = end
	}
	if hi <= lo {
		return ""
	}
	return ansi.Truncate(ansi.TruncateLeft(seg, lo-start, ""), hi-lo, "")
}

// HardWrap breaks s every width cells (minimum 8), for text with no spaces to wrap on.
func HardWrap(s string, width int) string {
	return ansi.Hardwrap(s, max(width, 8), false)
}

// blanks returns an n-line block of empty lines (height n) for use as a flexible
// filler/spacer in JoinVertical stacks.
func Blanks(n int) string {
	if n < 1 {
		return ""
	}
	return strings.Repeat("\n", n-1)
}

func IndentLines(s, prefix string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
