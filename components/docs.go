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
//	```fence```     indented, muted, hard-wrapped (never re-flowed)
//	`code`          accent, inline
//	**bold**        bold
//	*em*            italic ("_em_" is NOT honored: it would hit every snake_case name)
//	[text](url)     the text, underlined; the target is dropped
//	anything else   a paragraph: consecutive lines join, then wrap as one block
//
// Images (![alt](url)), tables, blockquotes and HTML pass through as their literal
// source text — the deliberate "skip what we don't know" behavior, so an unsupported
// construct is visible rather than silently swallowed. Two more known edges: a lone
// "*" in prose can pair with a later one and italicize what's between them, and
// backslash escapes (\*) are not honored.
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
// Only the "*em*" spelling is honored; "_em_" would italicize the middle of every
// snake_case identifier the manual pages are full of.
var inlineAny = regexp.MustCompile("`[^`]+`" + `|\*\*[^*]+\*\*|\*[^*]+\*|!?\[[^\]]+\]\([^)]*\)`)

// orderedItem matches "1. " / "12) " at the start of a list line; the capture is the
// marker, which is kept verbatim rather than renumbered.
var orderedItem = regexp.MustCompile(`^(\d+[.)])\s+`)

const (
	bulletMark = "• "
	indent     = "  "
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
	fence   bool     // inside a ``` code fence
}

func (r *docRenderer) line(line string) {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "```") {
		r.flush()
		r.fence = !r.fence
		return
	}
	if r.fence {
		r.emit(codeStyle().Render(indent + core.HardWrap(line, r.width-len(indent))))
		return
	}

	switch {
	case trimmed == "":
		r.flush()
		r.blank()
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
	text := inline(strings.Join(r.pending, " "))
	if r.marker != "" {
		r.emit(hang(r.marker, text, r.width))
	} else {
		r.emit(ansi.Wrap(text, r.width, wrapBreaks))
	}
	r.pending, r.marker = nil, ""
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
func inline(s string) string {
	return inlineAny.ReplaceAllStringFunc(s, func(m string) string {
		kind, text := inlineParts(m)
		switch kind {
		case inlineKindCode:
			return codeSpanStyle().Render(text)
		case inlineKindBold:
			return boldStyle().Render(text)
		case inlineKindEm:
			return italicStyle().Render(text)
		case inlineKindLink:
			return linkStyle().Render(text)
		}
		return text // an image: its literal source
	})
}

// The inline construct kinds inlineParts classifies a match into.
const (
	inlineKindCode = iota
	inlineKindBold
	inlineKindEm
	inlineKindLink
	inlineKindImage
)

// inlineParts splits one inlineAny match into its kind and the text that survives
// into the output — the delimiters dropped, an image kept whole.
func inlineParts(m string) (kind int, text string) {
	switch {
	case strings.HasPrefix(m, "`"):
		return inlineKindCode, strings.Trim(m, "`")
	case strings.HasPrefix(m, "**"):
		return inlineKindBold, strings.Trim(m, "*")
	case strings.HasPrefix(m, "*"):
		return inlineKindEm, strings.Trim(m, "*")
	case strings.HasPrefix(m, "!"):
		return inlineKindImage, m
	}
	return inlineKindLink, m[1:strings.Index(m, "]")]
}

// plain strips the inline markup from a line, for places that show it unstyled (the
// index's one-line descriptions) — inline's pass without the styling.
func plain(s string) string {
	return inlineAny.ReplaceAllStringFunc(s, func(m string) string {
		_, text := inlineParts(m)
		return text
	})
}

// h1Style is the top-level heading: the accent heading underlined, so a page's "#"
// still outranks the "##" sections under it in a terminal with no type sizes.
func h1Style() lipgloss.Style {
	return headingStyle().Underline(true)
}

func headingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.FocusedColor)
}

func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true)
}

func linkStyle() lipgloss.Style {
	return lipgloss.NewStyle().Underline(true).Foreground(core.FocusedColor)
}

func subheadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.MutedColor)
}

func codeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.MutedColor)
}

func codeSpanStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.FocusedColor)
}
