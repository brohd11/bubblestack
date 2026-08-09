package components

import (
	"io/fs"
	"regexp"
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// This file is the shared in-TUI manual engine: parse a set of markdown pages, render
// them with a deliberately partial markdown reader, and build a Docs index over them. It
// names no domain type — a consumer supplies its own embedded pages and the page
// title/description come out of the markdown itself — so every bubblestack app (gdaddon,
// repoview, …) shares one renderer and index instead of copying ~300 lines.
//
// The standard consumer layout: pages at doc/embedded/*.md in the app's repo (the
// discoverable doc folder), exposed by a tiny doc package's Pages() — go:embed can't
// reach a parent directory, so the embed lives at that level, and the app's internal
// docs flow imports Pages() from it.
//
// Adding a page is dropping a numbered .md into the consumer's pages dir: the filename
// orders it (fs.ReadDir returns sorted), the first "# " heading is its title, and the
// first line under that heading is its one-line index description.

// DocPage is one parsed manual page: its title + one-line description (both read out of
// the markdown) and the body the renderer folds to width.
type DocPage struct {
	Title string
	Desc  string
	Body  string // everything after the title line (the description is its first paragraph)
}

// ParseDocPages reads dir out of fsys (typically an embed.FS) in filename order and parses
// each entry into a DocPage. A read error yields no pages (an empty menu), never a panic.
func ParseDocPages(fsys fs.FS, dir string) []DocPage {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var pages []DocPage
	for _, e := range entries {
		data, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			continue
		}
		pages = append(pages, parseDocPage(string(data)))
	}
	return pages
}

// parseDocPage pulls the title (the first "# " heading) and description (the first
// non-empty line under it) out of a page, leaving the body to the renderer. A page missing
// its heading still reads — it just falls back to its first line as the title.
func parseDocPage(src string) DocPage {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	p := DocPage{Body: src}
	for i, line := range lines {
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		p.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		p.Body = strings.Join(lines[i+1:], "\n")
		for _, rest := range lines[i+1:] {
			if strings.TrimSpace(rest) != "" {
				p.Desc = plain(strings.TrimSpace(rest))
				break
			}
		}
		break
	}
	if p.Title == "" && len(lines) > 0 {
		p.Title = strings.TrimSpace(lines[0])
	}
	return p
}

// DocsIndex is the manual menu: one self-dispatching row per page, each pushing its own
// scrollable DocScreen (the body re-renders at whatever width the screen is given, so it
// re-wraps on resize). title/crumb name the index screen; an empty page set shows a
// placeholder row.
func DocsIndex(title, crumb string, pages []DocPage) *PickerScreen {
	var items []list.Item
	for _, p := range pages {
		p := p
		items = append(items, Item{
			Name: p.Title,
			Desc: p.Desc,
			Pick: func(*core.Shared) core.Action { return core.Push(newDocPage(p)) },
		})
	}
	items = EnsurePlaceholder(items, "(no pages)", "the docs pages didn't compile into this build")
	return NewPicker(items, PickerOpts{Title: title, Crumb: crumb})
}

func newDocPage(p DocPage) *DocScreen {
	return NewDocScreen(DocOpts{
		Title:  p.Title,
		Render: func(width int) string { return RenderMarkdown(p.Body, width) },
	})
}

// DocsItem is the standard Actions-menu docs row: "? Docs" with a desc derived
// from the page titles (docTopics), pushing the DocsIndex over pages. ok=false
// when pages is empty — no docs compiled into the build, no row.
func DocsItem(pages []DocPage) (item list.Item, ok bool) {
	if len(pages) == 0 {
		return nil, false
	}
	return Item{
		Name: "? Docs",
		Desc: docTopics(pages),
		Pick: func(sh *core.Shared) core.Action { return core.Push(DocsIndex("Docs", "Docs", pages)) },
	}, true
}

// docTopics builds the docs row's desc from the page titles — lowercased and
// joined, capped at four topics with a trailing ellipsis when there are more.
// Derived rather than fixed so adding a page updates the blurb on its own.
func docTopics(pages []DocPage) string {
	topics := make([]string, len(pages))
	for i, p := range pages {
		topics[i] = strings.ToLower(p.Title)
	}
	if len(topics) > 4 {
		return strings.Join(topics[:4], ", ") + ", …"
	}
	return strings.Join(topics, ", ")
}

// The pages are markdown, but only the constructs below are honored — this is a
// reader, not a general markdown implementation (a full renderer would drag in a
// dependency and bring its own theme, which would fight core's). Everything is
// re-flowed to the width the DocScreen hands us:
//
//	# heading       bold accent, blank line above
//	## heading      bold accent, blank line above
//	### heading     bold, muted
//	- item          bulleted, wrapped with a hanging indent (an indented line continues it)
//	1. item         same, keeping the author's number as the marker ("1)" too)
//	```fence```     indented, muted, hard-wrapped (never re-flowed), ruled off above and below
//	~~~fence~~~     the same; only the marker that opened a block can close it
//	> quote         re-flowed like a paragraph, muted, with a bar down every row
//	`code`          accent on a tinted background, inline, a cell of tint each side
//	**bold**        bold
//	*em* / _em_     italic
//	[text](url)     the text, underlined; the target is dropped
//	anything else   a paragraph: consecutive lines join, then wrap as one block
//
// Images (![alt](url)), tables and HTML pass through as their literal source text —
// the deliberate "skip what we don't know" behavior, so an unsupported construct is
// visible rather than silently swallowed. Known edges: a lone "*" in prose can pair
// with a later one and italicize what's between them; backslash escapes (\*) are not
// honored; "_em_" consumes the character on each side of it (see inlineAny), so a
// construct starting on that exact character is skipped — "_a_*b*" italicizes "a" and
// leaves "*b*" literal, while "_a_ *b*" behaves normally; a nested ">>" quote
// collapses to one level rather than nesting; and an inline construct that straddles a
// wrap break inside a quote renders literally (see quoteBlock).
//
// Styles are read per call (not cached) so a theme switch repaints the page — the same
// rule core.StyleList follows.

// inlineAny matches every inline construct in ONE alternation, which is what makes
// the single pass in inline() possible. Running the patterns in sequence instead
// would have each one matching the ANSI the previous one emitted — the link pattern
// happily reads an escape sequence's "\x1b[" as a label bracket — so the passes have
// to be mutually exclusive by construction, not by ordering.
//
// Alternation order IS priority (Go's regexp is leftmost-FIRST, like Perl): code
// spans win over everything inside them, and "**" is tried before "*".
// Images carry their "!" into the match so they are recognized and passed through
// whole, rather than half-matched as a link.
//
// The underscore emphasis is the awkward one. "_em_" has to italicize prose while
// leaving snake_case_name alone, and that distinction is a boundary condition Go's
// regexp cannot express as a lookaround — so the boundary characters are MATCHED and
// then re-emitted by inlineParts. Requiring a non-word character before the opening
// "_" is what rejects the identifiers: their underscores always follow a letter.
//
// Each alternative is a named group so inlineParts can classify a match by which one
// fired; the underscore form's match starts with an arbitrary boundary character, so
// sniffing the first byte (as the others allow) would not identify it.
var inlineAny = regexp.MustCompile(
	"(?P<code>`[^`]+`)" +
		`|(?P<strong>\*\*[^*]+\*\*)` +
		`|(?P<em>\*[^*]+\*)` +
		`|(?P<link>!?\[[^\]]+\]\([^)]*\))` +
		`|(?P<uem>(?:^|[^\p{L}\p{N}_])_[^_]+_(?:[^\p{L}\p{N}_]|$))`)

// orderedItem matches "1. " / "12) " at the start of a list line; the capture is the
// marker, which is kept verbatim rather than renumbered.
var orderedItem = regexp.MustCompile(`^(\d+[.)])\s+`)

const (
	bulletMark = "• "
	indent     = "  "
	// quoteBar prefixes every row of a blockquote (see quoteBlock).
	quoteBar = "│ "
	// wrapBreaks are extra wrap points beyond whitespace, so a long path or URL folds
	// at a separator rather than mid-name.
	wrapBreaks = "/_"
)

// RenderMarkdown folds a page's markdown body to width display columns.
func RenderMarkdown(body string, width int) string {
	if width < 20 {
		width = 20
	}
	r := &docRenderer{width: width}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		r.line(line)
	}
	r.flush()
	return strings.Trim(strings.Join(r.out, "\n"), "\n")
}

// docRenderer walks the page a line at a time, accumulating the current block (a paragraph
// or a bullet) until something ends it. Blocks — not source lines — are what get wrapped,
// so a paragraph the author hard-wrapped at 88 columns re-flows to the terminal instead
// of breaking wherever the .md file happened to break.
type docRenderer struct {
	width int
	out   []string

	pending []string // the lines of the block being accumulated
	marker  string   // the pending block is a list item hung under this marker; "" ⇒ paragraph
	quote   bool     // the pending block is a blockquote
	fence   string   // the marker that opened the current code fence; "" ⇒ not in one
}

func (r *docRenderer) line(line string) {
	trimmed := strings.TrimSpace(line)

	if m := fenceMarker(trimmed); m != "" && (r.fence == "" || r.fence == m) {
		r.flush()
		if r.fence == "" {
			r.fence = m
		} else {
			r.fence = ""
		}
		// A dim rule on each side of the block. Blocks are the one thing here that
		// isn't re-flowed, so without a boundary they read as ordinary indented prose
		// — and a background (what the inline spans use) would break on the tabs a
		// block renders literally.
		r.emit(ruleStyle().Render(strings.Repeat("─", r.width)))
		return
	}
	if r.fence != "" {
		r.emit(codeStyle().Render(indent + core.HardWrap(line, r.width-len(indent))))
		return
	}

	switch {
	case trimmed == "":
		r.flush()
		r.blank()
	case strings.HasPrefix(trimmed, ">"):
		// Consecutive "> " lines join into one re-flowed quote, like a paragraph.
		if !r.quote {
			r.flush()
			r.quote = true
		}
		r.pending = append(r.pending, quoteText(trimmed))
	case strings.HasPrefix(trimmed, "### "):
		r.heading(subheadingStyle().Render(strings.TrimPrefix(trimmed, "### ")))
	case strings.HasPrefix(trimmed, "## "):
		r.heading(headingStyle().Render(strings.TrimPrefix(trimmed, "## ")))
	case strings.HasPrefix(trimmed, "# "):
		r.heading(h1Style().Render(strings.TrimPrefix(trimmed, "# ")))
	case strings.HasPrefix(trimmed, "- "):
		r.item(bulletMark, strings.TrimPrefix(trimmed, "- "))
	case orderedItem.MatchString(trimmed):
		// The author's own number is kept rather than renumbered: the source is
		// what they'll compare the preview against.
		m := orderedItem.FindStringSubmatch(trimmed)
		r.item(m[1]+" ", trimmed[len(m[0]):])
	case r.marker != "" && line != trimmed:
		// An indented line under a list item continues it rather than starting a paragraph.
		r.pending = append(r.pending, trimmed)
	default:
		r.pending = append(r.pending, trimmed)
	}
}

// item starts a list block: marker is the hanging prefix (already spaced), text its
// first line.
func (r *docRenderer) item(marker, text string) {
	r.flush()
	r.marker = marker
	r.pending = []string{text}
}

// fenceMarker is the fence run opening or closing a code block on this line — "```"
// or "~~~" — or "" when the line is not a fence. The marker is returned rather than a
// bool so the renderer can require the SAME one to close: a "```" line inside a "~~~"
// block is block content, not the end of it.
func fenceMarker(trimmed string) string {
	for _, m := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, m) {
			return m
		}
	}
	return ""
}

// quoteText strips a quote line's markers down to its text. Nested ">>" collapses to
// one level — the markers come off together and the quote renders flat, which is as
// much as a reader for our own prose needs.
func quoteText(trimmed string) string {
	for strings.HasPrefix(trimmed, ">") {
		trimmed = strings.TrimPrefix(trimmed, ">")
		trimmed = strings.TrimPrefix(trimmed, " ")
	}
	return trimmed
}

// heading emits an already-styled heading under a separator. It is wrapped like any
// other block: a heading longer than the pane is narrow — a preview beside an editor,
// say — would otherwise be the one thing that overflows the box.
func (r *docRenderer) heading(rendered string) {
	r.flush()
	r.blank()
	r.emit(ansi.Wrap(rendered, r.width, wrapBreaks))
}

// flush wraps the accumulated block and empties it.
func (r *docRenderer) flush() {
	if len(r.pending) == 0 {
		return
	}
	joined := strings.Join(r.pending, " ")
	switch {
	case r.quote:
		r.emit(quoteBlock(joined, r.width))
	case r.marker != "":
		r.emit(hang(r.marker, inline(joined), r.width))
	default:
		r.emit(ansi.Wrap(inline(joined), r.width, wrapBreaks))
	}
	r.pending, r.marker, r.quote = nil, "", false
}

// quoteBlock wraps a quote and bars EVERY row — unlike hang, which marks only the
// first: a quote's rows are all equally inside it, and a bar that stopped after the
// first would read as a list item.
//
// It takes the quote's RAW text and styles each row after wrapping, which is the
// opposite of every other block here and is forced by the bar. ansi.Wrap does not
// emit self-contained rows — a row can rely on a color opened on the row above — and
// the bar carries a reset, so prefixing it to a pre-styled row wipes the tint for the
// rest of that row. Wrapping first is safe because styling adds no display cells, so
// the break positions are identical either way; the cost is that an inline construct
// straddling a break renders literally.
func quoteBlock(text string, width int) string {
	w := width - lipgloss.Width(quoteBar)
	if w < 1 {
		w = 1
	}
	bar := ruleStyle().Render(quoteBar)
	rows := strings.Split(ansi.Wrap(text, w, wrapBreaks), "\n")
	for i, row := range rows {
		rows[i] = bar + inlineOver(row, quoteTextStyle())
	}
	return strings.Join(rows, "\n")
}

func (r *docRenderer) emit(s string) { r.out = append(r.out, s) }

// blank appends a separator line, collapsing runs (the source's blank line before a
// heading and the one the heading adds itself would otherwise double up).
func (r *docRenderer) blank() {
	if len(r.out) == 0 || r.out[len(r.out)-1] == "" {
		return
	}
	r.emit("")
}

// hang wraps text under a marker, indenting the continuation rows to sit under the first
// row's text so the entry still reads as one unit (the idiom LogPane's wrapped mode uses).
// The continuation indent is the marker's own width, so a wide "10. " lines up as
// readably as a bullet.
func hang(marker, text string, width int) string {
	mw := lipgloss.Width(marker)
	w := width - mw
	if w < 1 {
		w = 1
	}
	rows := strings.Split(ansi.Wrap(text, w, wrapBreaks), "\n")
	for i, row := range rows {
		if i == 0 {
			rows[i] = marker + row
			continue
		}
		rows[i] = strings.Repeat(" ", mw) + row
	}
	return strings.Join(rows, "\n")
}

// inline styles the spans inside a block of prose. Styling before wrapping is safe:
// ansi.Wrap measures display cells, not bytes.
func inline(s string) string { return inlineOver(s, lipgloss.Style{}) }

// inlineOver is inline with a base style under it: the runs BETWEEN the constructs
// render through base, each construct through its own. A blockquote needs this. The
// obvious alternative — style the finished row — does not work, because every inline
// span ends in a reset and everything after it would lose the tint.
//
// The zero base (what inline passes) renders each run unchanged, so the plain path
// is byte-identical to a single ReplaceAllStringFunc pass.
func inlineOver(s string, base lipgloss.Style) string {
	var b strings.Builder
	write := func(run string) {
		if run != "" {
			b.WriteString(base.Render(run))
		}
	}
	last := 0
	for _, loc := range inlineAny.FindAllStringIndex(s, -1) {
		write(s[last:loc[0]])
		p := inlineParts(s[loc[0]:loc[1]])
		write(p.prefix) // the boundary characters underscore emphasis had to match
		switch p.kind {
		case inlineKindCode:
			// A cell of the tint on each side: the background reads as a chip around
			// the code rather than as a smear ending flush against the next word.
			b.WriteString(codeSpanStyle().Render(" " + p.text + " "))
		case inlineKindBold:
			b.WriteString(boldStyle().Render(p.text))
		case inlineKindEm:
			b.WriteString(italicStyle().Render(p.text))
		case inlineKindLink:
			b.WriteString(linkStyle().Render(p.text))
		default:
			write(p.text) // an image: its literal source, in the base style
		}
		write(p.suffix)
		last = loc[1]
	}
	write(s[last:])
	return b.String()
}

// The inline construct kinds inlineParts classifies a match into.
const (
	inlineKindCode = iota
	inlineKindBold
	inlineKindEm
	inlineKindLink
	inlineKindImage
)

// inlinePart is one classified inlineAny match: the text that survives into the
// output, plus the boundary characters the underscore-emphasis alternative had to
// match to prove it wasn't inside an identifier. Every other kind leaves prefix and
// suffix empty; they are re-emitted verbatim around the styled text.
type inlinePart struct {
	kind           int
	prefix, suffix string
	text           string
}

// inlineParts classifies one inlineAny match by which named group fired, and splits
// it into its parts — the delimiters dropped, an image kept whole.
func inlineParts(m string) inlinePart {
	switch {
	case strings.HasPrefix(m, "`"):
		return inlinePart{kind: inlineKindCode, text: strings.Trim(m, "`")}
	case strings.HasPrefix(m, "**"):
		return inlinePart{kind: inlineKindBold, text: strings.Trim(m, "*")}
	case strings.HasPrefix(m, "*"):
		return inlinePart{kind: inlineKindEm, text: strings.Trim(m, "*")}
	case strings.HasPrefix(m, "!"):
		return inlinePart{kind: inlineKindImage, text: m}
	case strings.HasPrefix(m, "["):
		return inlinePart{kind: inlineKindLink, text: m[1:strings.Index(m, "]")]}
	}
	// The underscore form: everything outside the outermost pair of "_" is boundary.
	lo, hi := strings.Index(m, "_"), strings.LastIndex(m, "_")
	return inlinePart{
		kind:   inlineKindEm,
		prefix: m[:lo],
		text:   m[lo+1 : hi],
		suffix: m[hi+1:],
	}
}

// plain strips the inline markup from a line, for places that show it unstyled (the
// index's one-line descriptions) — inline's pass without the styling.
func plain(s string) string {
	return inlineAny.ReplaceAllStringFunc(s, func(m string) string {
		p := inlineParts(m)
		return p.prefix + p.text + p.suffix
	})
}

// h1Style is the top-level heading: the accent heading underlined, so a page's "#"
// still outranks the "##" sections under it in a terminal with no type sizes.
func h1Style() lipgloss.Style {
	return headingStyle().Underline(true)
}

// The accent-carrying styles read core.MarkdownAccent rather than core.FocusedColor:
// a page leans on the accent for headings, code spans and links at once, and a theme
// whose accent is the terminal's own extreme (mono) would flatten all three into body
// text. MarkdownAccent is the active theme's accent unless it borrows one.
func headingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.MarkdownAccent())
}

func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true)
}

func linkStyle() lipgloss.Style {
	return lipgloss.NewStyle().Underline(true).Foreground(core.MarkdownAccent())
}

// subheadingStyle and codeStyle stay on the theme's own muted grey — it reads fine
// under every preset including mono, so there is nothing to borrow.
func subheadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.MutedColor)
}

func codeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.MutedColor)
}

// codeSpanStyle tints an inline span's background as well as its text, which is what
// separates `code` from a fenced block now that both would otherwise be one accent.
// The background is one step off the terminal's own ground in whichever direction the
// terminal is not — the adaptive pattern core's neutral palette uses.
func codeSpanStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(core.MarkdownAccent()).
		Background(lipgloss.AdaptiveColor{Light: "254", Dark: "236"})
}

// ruleStyle draws the thin separators around a code block and the bar down a
// blockquote, in the theme's border color so they read as chrome rather than content.
func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.BorderColor)
}

// quoteTextStyle mutes a blockquote's prose, so a quote reads as set apart from the
// body text around it and not merely indented behind a bar.
func quoteTextStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.MutedColor)
}
