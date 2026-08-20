package core

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ---------- header ----------

// HeaderInnerWidth is the content width inside the persistent context box for a
// terminal of the given width, so a Header closure can size/truncate values to fit.
func HeaderInnerWidth(width int) int {
	inner := width - 4 // minus border (2) and padding (2)
	if inner < 20 {
		inner = 20
	}
	return inner
}

// HeaderBox renders body inside the persistent bordered context box, sized to the
// terminal width. A consumer's Header closure builds body (e.g. with Label +
// TruncLeft) and returns HeaderBox(sh.Width(), body).
func HeaderBox(width int, body string) string {
	return headerStyle.Width(HeaderInnerWidth(width)).Render(body)
}

// Label renders a context-box/field label in the muted label style.
func Label(s string) string { return labelStyle.Render(s) }

// Value renders a context-box field value in the log (near-white) style.
func Value(s string) string { return logStyle.Render(s) }

// TruncLeft keeps the right (most informative) end of a path, prefixing "…".
func TruncLeft(s string, max int) string {
	if max < 4 {
		max = 4
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return "…" + string(r[len(r)-(max-1):])
}

// ---------- breadcrumb / title bars ----------

// renderTitleBar renders text as a list-title-styled bar, so screens without
// their own list title keep a consistent header. It is TWO rows: bubbles' default
// TitleBar carries a bottom padding of 1 (listStyles is DefaultStyles with only
// Title recolored), and that blank row under the bar is what a real list's title
// section has too, so a bar rendered here lines up with one. Anything mapping rows
// to items must measure it rather than assume — see components.listHeaderHeight,
// where assuming one row put every titled list's clicks a row out.
func RenderTitleBar(text string) string {
	return listStyles.TitleBar.Render(listStyles.Title.Render(text))
}

// renderTitleBarMuted is RenderTitleBar for an unfocused element: the accent fill
// comes off and the text goes muted. Only the colors change — the bar keeps
// listStyles.Title's padding, so its height and left pad match the focused bar
// exactly and a focus flip can't shift the body under it.
func renderTitleBarMuted(text string) string {
	muted := listStyles.Title.UnsetBackground().Foreground(MutedColor)
	return listStyles.TitleBar.Render(muted.Render(text))
}

// WithTitle prepends a styled title bar to body, or returns body unchanged when
// title is empty — so any screen can make its in-body title optional by passing the
// raw (unrendered) title text straight through.
func WithTitle(title, body string) string {
	if title == "" {
		return body
	}
	return lipgloss.JoinVertical(lipgloss.Left, RenderTitleBar(title), body)
}

// WithTitleFocused is WithTitle with a focus-tinted bar: the accent bar when
// focused, a muted one when a sibling pane holds focus. Screens that can be nested
// in a ModularScreen render through it for the same reason they use BoxFocused —
// so the whole element, title included, reads as inactive. Standalone screens
// (always focused) keep using WithTitle.
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
	inner := s.width - 10
	if inner < 24 {
		inner = 24
	}
	return inner
}

// box renders body inside the shared bordered confirm/summary box.
func (s *Shared) Box(body string) string {
	return boxStyle.Width(s.ConfirmWidth()).Render(body)
}

// BoxFocused is Box with a focus-tinted border: FocusedColor when focused,
// BorderColor when not. Screens that can be nested in a ModularScreen (a form
// beside a detail pane) render through it so the box border carries the panel
// focus the way a ScrollContainer's legend border does. Standalone screens keep
// using Box.
func (s *Shared) BoxFocused(body string, focused bool) string {
	color := BorderColor
	if focused {
		color = FocusedColor
	}
	return boxStyle.BorderForeground(color).Width(s.ConfirmWidth()).Render(body)
}

// BoxInnerWidth is the widest a line of body text can be before Box word-wraps it:
// ConfirmWidth minus the padding lipgloss reserves out of it. Derived from boxStyle
// rather than written as a literal, so a padding change can't silently desync a caller
// that sizes its content to fit (a re-wrap by the box restarts the line at column 0,
// which is where a form field's continuation collides with its label column).
func (s *Shared) BoxInnerWidth() int {
	return s.ConfirmWidth() - boxStyle.GetHorizontalPadding()
}

// BoxOrigin is the cell offset of a Box's first content line from the box block's own
// top-left — what a caller anchoring an overlay to a row *inside* a box has to add to
// reach that row's real screen cell (FormScreen.FieldAnchor is the first such caller).
// Derived from boxStyle for the same reason BoxInnerWidth is: a change to the margin,
// border or padding has to move this, not silently mis-place every anchored popup.
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

// newSelectList builds a list styled like the others (no status bar, help drawn
// separately, esc/enter hints) for the versions and submenu screens. It's sized
// to zero; the owning screen's SetSize gives it real dimensions.
func NewSelectList(items []list.Item, title string, extra ...key.Binding) list.Model {
	return newSelectList(items, title, NewDelegate(), extra...)
}

// SuffixItem is the row contract for a compact list. Title is the primary value;
// SuffixText is optional context rendered after it in the theme's muted color.
type SuffixItem interface {
	list.Item
	Title() string
	SuffixText() string
}

// NewCompactList builds the single-line counterpart of NewSelectList. It keeps
// the same title, filtering, keymap, help, and pagination behavior; only the row
// delegate changes to a one-cell-high title plus optional muted suffix.
func NewCompactList(items []list.Item, title string, extra ...key.Binding) list.Model {
	return newSelectList(items, title, CompactDelegate{}, extra...)
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
	l.AdditionalShortHelpKeys = keys
	l.AdditionalFullHelpKeys = keys
	return l
}

// CompactDelegate renders one item per terminal row with no inter-item spacing.
// The title is given width priority; any suffix is fitted into the cells left over.
//
// Offset opts the SELECTED row into a marquee: when it is non-nil and that row's
// title-plus-suffix is wider than the row, the two are treated as one string and
// windowed by *Offset, so a name AND the path after it both become readable in a
// column too narrow for either. The owner of the pointer owns the clock — nothing
// here advances it (Render must stay a pure function of state), and a nil Offset
// leaves every row statically truncated. ListPanel is the in-tree owner.
type CompactDelegate struct{ Offset *int }

func (CompactDelegate) Height() int                         { return 1 }
func (CompactDelegate) Spacing() int                        { return 0 }
func (CompactDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

// CompactRow is one row's two raw, untruncated pieces: the title, and the muted tail
// ("  " + suffix, empty when the item has no suffix).
type CompactRow struct{ Title, Tail string }

// Width is the row's full untruncated cell width. Subtracting the text width from it gives
// the marquee's last offset — the point at which the tail sits flush with the right edge.
func (r CompactRow) Width() int { return lipgloss.Width(r.Title) + lipgloss.Width(r.Tail) }

// CompactTextWidth is the cells a compact row's text actually gets out of a list of the
// given width — the delegate's padding taken off. Exported so the panel that drives a
// marquee measures the row against the same number Render fits it into.
func CompactTextWidth(listWidth int) int {
	s := list.NewDefaultItemStyles().NormalTitle
	if w := listWidth - s.GetPaddingLeft() - s.GetPaddingRight(); w > 1 {
		return w
	}
	return 1
}

// CompactMarquee reports a row's raw pieces and whether they overflow textWidth. It is the
// single place "does this row need to scroll?" is answered, so the panel that owns the
// offset and the delegate that consumes it cannot disagree about which rows are moving.
func CompactMarquee(i SuffixItem, textWidth int) (CompactRow, bool) {
	r := CompactRow{Title: i.Title()}
	if s := i.SuffixText(); s != "" {
		r.Tail = "  " + s
	}
	return r, r.Width() > textWidth
}

func (d CompactDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	i, ok := item.(SuffixItem)
	if !ok || m.Width() <= 0 {
		return
	}

	styles := list.NewDefaultItemStyles()
	styles.SelectedTitle = styles.SelectedTitle.Foreground(FocusedColor).BorderForeground(FocusedColor)
	styles.NormalDesc = styles.NormalDesc.Foreground(MutedColor)
	styles.DimmedDesc = styles.DimmedDesc.Foreground(MutedColor)

	textWidth := m.Width() - styles.NormalTitle.GetPaddingLeft() - styles.NormalTitle.GetPaddingRight()
	if textWidth < 1 {
		textWidth = 1
	}

	emptyFilter := m.FilterState() == list.Filtering && m.FilterValue() == ""
	isFiltered := m.FilterState() == list.Filtering || m.FilterState() == list.FilterApplied
	isSelected := index == m.Index() && m.FilterState() != list.Filtering

	title, suffix := "", ""
	// The marquee owns the fit when the selected row overflows: the two pieces slide as one
	// string, so the tail of a long name and the path behind it both come into view. Never
	// while a filter is live — the match highlighting below styles the title by rune index,
	// and those indices stop addressing the string once it has been windowed.
	if raw, over := CompactMarquee(i, textWidth); over && d.Offset != nil && isSelected && !isFiltered {
		off := min(max(*d.Offset, 0), raw.Width()-textWidth)
		title = marqueeSeg(raw.Title, 0, off, textWidth)
		suffix = marqueeSeg(raw.Tail, lipgloss.Width(raw.Title), off, textWidth)
	} else {
		title = ansi.Truncate(i.Title(), textWidth, "…")
		if s := i.SuffixText(); s != "" {
			if remaining := textWidth - lipgloss.Width(title) - 2; remaining > 0 {
				suffix = "  " + ansi.Truncate(s, remaining, "…")
			}
		}
	}

	titleStyle := styles.NormalTitle
	if emptyFilter {
		titleStyle = styles.DimmedTitle
	} else if isSelected {
		titleStyle = styles.SelectedTitle
	}
	if isFiltered && !emptyFilter && index < len(m.VisibleItems()) {
		matched := titleStyle.Inline(true).Inherit(styles.FilterMatch)
		title = lipgloss.StyleRunes(title, m.MatchesForItem(index), matched, titleStyle.Inline(true))
	}

	muted := lipgloss.NewStyle().Foreground(MutedColor)
	if emptyFilter {
		muted = styles.DimmedDesc.Inline(true)
	}
	fmt.Fprint(w, titleStyle.Render(title)+muted.Render(suffix)) //nolint:errcheck
}

// newDelegate is the shared list delegate with brightened description text and the
// selected row recolored to the theme accent (bubbles' default selected styles are
// a hardcoded pink). The left-border layout from the default delegate is kept; only
// the colors change.
func NewDelegate() list.DefaultDelegate {
	d := list.NewDefaultDelegate()
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(MutedColor)
	d.Styles.DimmedDesc = d.Styles.DimmedDesc.Foreground(MutedColor)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.Foreground(FocusedColor).BorderForeground(FocusedColor)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.Foreground(FocusedColor).BorderForeground(FocusedColor)
	return d
}

// styleList applies the shared list config: hide the built-in status bar and
// help (help is drawn manually at the bottom), and brighten the help colors.
func StyleList(l *list.Model) {
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	// Theme the list's own title bar to match the breadcrumb (RenderTitleBar)
	// instead of bubbles' default purple.
	l.Styles.Title = listStyles.Title
	// l.Styles.TitleBar = l.Styles.TitleBar.Margin(0) // how to set themes
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

// ShortHelp renders a tab root's decluttered short help for the given preset. The full
// (?) help is laid out here as four purpose-built columns — nav · actions · filter ·
// chrome — rather than bubbles' default, which crams the list-level filter keys and every
// AdditionalFullHelpKey into a single column that grows unreadably tall as a tab adds keys.
// The chrome column (output/refresh/mouse/quit — all router-owned, so absent from the list's
// own FullHelp) is built centrally and shown uniformly on every list/picker; a tab's own key
// list needn't enumerate it, and any entry that duplicates a chrome key is dropped from the
// actions column (excludeKeys) so it appears once. Tab roots use this instead of helpView so
// secondary keys stay out of the short bar.
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
		Hint("up", Keys.Up),
		Hint("down", Keys.Down),
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

// excludeKeys returns binds with any entry dropped whose keycodes overlap one of exclude's —
// used by the full-help layout so a tab that still lists a chrome key (e.g. "clear log") in
// its AdditionalFullHelpKeys doesn't render it twice once the chrome column adds it centrally.
func excludeKeys(binds, exclude []key.Binding) []key.Binding {
	skip := map[string]bool{}
	for _, b := range exclude {
		for _, k := range b.Keys() {
			skip[k] = true
		}
	}
	out := make([]key.Binding, 0, len(binds))
	for _, b := range binds {
		drop := false
		for _, k := range b.Keys() {
			if skip[k] {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, b)
		}
	}
	return out
}

// styleHelp re-styles the static help model from the live MutedColor so static help
// bars track the active theme after a SetTheme switch. Built per call (not baked in
// at NewShared) for the same reason StyleList / fieldLabel restyle per call rather
// than caching a color that goes stale on the next theme change.
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

// marqueeSeg returns the part of seg that falls inside the window [offset, offset+width),
// where seg begins at cell `start` of a longer logical row. That extra `start` is the whole
// point: a compact row is two differently styled pieces (title, then muted suffix) that must
// slide as ONE string, and windowing each piece separately against a shared offset is what
// lets them keep their own styles while staying tiled to the cell. A segment entirely
// outside the window yields "".
//
// Both cuts use an empty tail — no ellipsis on a sliding row. The motion is what says there
// is more text, and a "…" pinned to a moving edge reads as part of the path.
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

// hardWrap breaks s into chunks of at most width runes (URLs have no spaces to
// word-wrap on, so we break unconditionally).
func HardWrap(s string, width int) string {
	if width < 8 {
		width = 8
	}
	r := []rune(s)
	var b strings.Builder
	for len(r) > width {
		b.WriteString(string(r[:width]))
		b.WriteByte('\n')
		r = r[width:]
	}
	b.WriteString(string(r))
	return b.String()
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
