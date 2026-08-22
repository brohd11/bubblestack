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
//	# heading       bold accent, underlined, blank line above
//	## heading      bold accent, blank line above
//	### … ######    bold, the accent dimmed one step; all four levels render alike
//	---             a full-width rule (also *** and ___, three or more of one char)
//	- item          bulleted, wrapped with a hanging indent (an indented line continues it)
//	1. item         same, keeping the author's number as the marker ("1)" too)
//	```fence```     indented, muted, hard-wrapped (never re-flowed)
//	~~~fence~~~     the same; only the marker that opened a block can close it
//	> quote         re-flowed like a paragraph, muted, with a bar down every row
//	| a | b |       a table: the header in the accent, a ─┼─ rule under it, columns
//	|:--|---:|      separated by │. The delimiter row is required and its ":" align.
//	`code`          accent on a tinted background, inline, a cell of tint each side
//	**bold**        bold
//	*em* / _em_     italic
//	[text](url)     the text, underlined; the target is dropped
//	anything else   a paragraph: consecutive lines join, then wrap as one block
//
// Images (![alt](url)) and HTML pass through as their literal source text — the
// deliberate "skip what we don't know" behavior, so an unsupported construct is
// visible rather than silently swallowed. Known edges: a lone "*" in prose can pair
// with a later one and italicize what's between them; backslash escapes (\*) are not
// honored; "_em_" consumes the character on each side of it (see inlineAny), so a
// construct starting on that exact character is skipped — "_a_*b*" italicizes "a" and
// leaves "*b*" literal, while "_a_ *b*" behaves normally; a nested ">>" quote
// collapses to one level rather than nesting; and an inline construct that straddles a
// wrap break inside a quote renders literally (see quoteBlock).
//
// A table's own edges: a row of pipes with no delimiter row under it (or one whose cell
// count disagrees) is not a table and reads as prose, which is what leaves a shell
// pipeline and a page about markdown intact; a cell cannot hold a literal "|", since
// escapes are unhonored and GFM splits inside a code span too, so `a|b` is two cells
// there as well; cells past the header's count are dropped and missing ones come out
// empty. A table too wide for the pane shrinks its widest columns first and wraps the
// cells that still do not fit (see tableWidths), and when not even a three-cell column
// each will fit, the rows fall back to prose. A construct straddling a wrap break inside
// a cell keeps its text but loses its styling on the continuation row — the mirror of the
// quote's edge, and the price of wrapping a styled cell so columns can be measured on
// what is actually printed (see wrapCell).
//
// Styles are read per call (not cached) so a theme switch repaints the page — the same
// rule core.StyleList follows.

// CodeBlockRenderer, when non-nil, renders the content of a fenced code block: it gets
// the fence's language tag ("" when the fence carries none), the raw content lines, and
// the width to fold to, and returns the finished display lines, which are emitted
// verbatim and indented. nil keeps the default muted, hard-wrapped rendering. The
// surrounding layout is the same either way; this seam only lets a consumer (gote) add
// syntax highlighting without the framework knowing any language — set it at init, it
// is not for concurrent use.
var CodeBlockRenderer func(lang string, code []string, width int) []string

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

// themeBreak matches a thematic break: three or more of one of "-", "*", "_" and
// nothing else. Standalone lines only — "***bold***" inline never reaches here. An
// alternation rather than a backreference, which Go's regexp does not have.
var themeBreak = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

// subheading matches every heading level below "##" — three hashes through six, the
// deepest markdown has. They share one style rather than fading level by level: a
// terminal has no type sizes, so six distinguishable heading weights do not exist, and
// the levels below "##" are in practice one bucket of "smaller than a section".
var subheading = regexp.MustCompile(`^#{3,6} `)

// tableDelim matches a GFM delimiter row — the line of dashes under a table's header,
// which is the ONLY thing that makes a row of pipes a table. Each cell is one or more
// "-" with an optional ":" on either end (the alignment marker); the outer pipes are
// optional, as GFM allows.
//
// It deliberately also matches a bare "---": requiring a pipe here would need a second
// alternation for the leading- and trailing-pipe forms, so tableStart requires the "|"
// separately — which is what keeps a thematic break under a line of pipes a thematic
// break rather than a one-column table.
var tableDelim = regexp.MustCompile(`^\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?$`)

const (
	bulletMark = "• "
	indent     = "  "
	// quoteBar prefixes every row of a blockquote (see quoteBlock).
	quoteBar = "│ "
	// wrapBreaks are extra wrap points beyond whitespace, so a long path or URL folds
	// at a separator rather than mid-name.
	wrapBreaks = "/_"
	// tableSep separates two of a table's columns; tableCross is the same three cells on
	// the header rule, so the crossing lands under the bar. Both are chrome (ruleStyle),
	// and their equal widths are what make the rule exactly as wide as a row.
	tableSep   = " │ "
	tableCross = "─┼─"
	// tableMinCol is the narrowest a column may be squeezed to before the table is
	// declared unlayable and its rows fall back to prose: three cells still hold a
	// readable fragment and keep the rule visible.
	tableMinCol = 3
)

// RenderMarkdown folds a page's markdown body to width display columns.
func RenderMarkdown(body string, width int) string {
	out, _ := RenderMarkdownMapped(body, width)
	return out
}

// RenderMarkdownMapped renders body like RenderMarkdown and also answers, per source
// line, the output row that line's block starts at — what a preview pane needs to
// follow an editor's scroll exactly rather than proportionally (the render re-flows,
// so no proportional estimate can land on the right row).
//
// A line that only accumulates into a pending block — a hard-wrapped paragraph's
// second line, a bullet's continuation — has no row of its own, since the block is
// re-flowed as one: those lines share out the block's rows in proportion. Every mark
// therefore lands inside the block its line belongs to, and the marks are monotonic
// non-decreasing, so a scroll anchored on one never runs ahead of the source.
func RenderMarkdownMapped(body string, width int) (string, []int) {
	if width < 20 {
		width = 20
	}
	src := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	marks := make([]int, len(src))
	for i := range marks {
		marks[i] = -1
	}
	r := &docRenderer{width: width, marks: marks, pendAt: -1}
	for i, line := range src {
		r.at, r.mark = i, i
		next := ""
		if i+1 < len(src) {
			next = src[i+1]
		}
		r.line(line, next)
		// Whatever is pending after the line was fed in belongs to this line too: the
		// block's rows are credited to the whole run when it flushes.
		if len(r.pending) > 0 {
			if r.pendAt < 0 {
				r.pendAt = i
			}
			r.pendTo = i
		}
	}
	r.flush()

	// Backfill the lines that emitted nothing of their own — a collapsed blank, a fence
	// marker, code the renderer folded — onto the last row that did come from the
	// source above them. Behind, never ahead: a preview anchored here shows the line
	// the editor is on rather than something after it.
	last := 0
	for i, m := range marks {
		if m < 0 {
			marks[i] = last
			continue
		}
		last = m
	}

	// The leading rows the trim takes off shift every mark with them. blank refuses to
	// emit a leading separator, so this is normally a no-op — done anyway rather than
	// leave the marks able to disagree with the text they index.
	joined := strings.Join(r.out, "\n")
	out := strings.TrimLeft(joined, "\n")
	lead := len(joined) - len(out)
	for i := range marks {
		marks[i] = max(marks[i]-lead, 0)
	}
	return strings.TrimRight(out, "\n"), marks
}

// docRenderer walks the page a line at a time, accumulating the current block (a paragraph
// or a bullet) until something ends it. Blocks — not source lines — are what get wrapped,
// so a paragraph the author hard-wrapped at 88 columns re-flows to the terminal instead
// of breaking wherever the .md file happened to break.
type docRenderer struct {
	width int
	out   []string
	rows  int // display rows emitted so far: one out entry can be a whole wrapped block

	pending []string // the lines of the block being accumulated
	marker  string   // the pending block is a list item hung under this marker; "" ⇒ paragraph
	quote   bool     // the pending block is a blockquote
	table   bool     // the pending block is a pipe table: header, delimiter row, then rows
	fence   string   // the marker that opened the current code fence; "" ⇒ not in one
	lang    string   // the current fence's language tag ("go" in "```go"); "" ⇒ none
	code    []string // the fenced lines accumulated for CodeBlockRenderer (nil ⇒ streamed)

	// The source→row map (see RenderMarkdownMapped, the one place a docRenderer is
	// built). marks is indexed by source line, -1 until that line's row is known.
	marks   []int
	at      int // the source line being fed to line()
	mark    int // the line emit credits a row to: at, or -1 while a block is flushing
	pendAt  int // the source line the pending block opened at; -1 ⇒ nothing pending
	pendTo  int // the last source line to join it
	fenceAt int // the source line the open fence's marker is on
}

// line feeds one source line to the renderer. next is the line after it, or "" at EOF:
// the reader's only lookahead, and it exists for exactly one construct — a row of pipes
// is a table only when a delimiter row follows it (see tableStart).
func (r *docRenderer) line(line, next string) {
	trimmed := strings.TrimSpace(line)

	if m := fenceMarker(trimmed); m != "" && (r.fence == "" || r.fence == m) {
		if r.fence != "" && r.code != nil {
			// Closing a highlighted block: the accumulated lines go through the
			// injected renderer. (Pending is always empty while a fence is open, so
			// there is nothing to flush first.)
			lang, code := r.lang, r.code
			r.fence, r.lang, r.code = "", "", nil
			r.emitCode(lang, code)
			r.blank()
			return
		}
		r.flush()
		if r.fence == "" {
			r.fence = m
			r.fenceAt = r.at
			r.lang = fenceLang(trimmed[len(m):])
			if CodeBlockRenderer != nil {
				r.code = []string{}
			}
		} else {
			r.fence = ""
		}
		// A fenced block is separated from prose by blank lines in both rendering
		// paths. CodeBlockRenderer changes only the block's styling, not its layout.
		r.blank()
		return
	}
	if r.fence != "" {
		if r.code != nil {
			r.code = append(r.code, line)
			return
		}
		r.emit(codeStyle().Render(indent + core.HardWrap(line, r.width-len(indent))))
		return
	}

	switch {
	case trimmed == "":
		r.flush()
		r.blank()
	case themeBreak.MatchString(trimmed):
		r.flush()
		r.emit(ruleStyle().Render(strings.Repeat("─", r.width)))
	case strings.HasPrefix(trimmed, ">"):
		// Consecutive "> " lines join into one re-flowed quote, like a paragraph.
		if !r.quote {
			r.flush()
			r.quote = true
		}
		r.pending = append(r.pending, quoteText(trimmed))
	case subheading.MatchString(trimmed):
		// Above the "##"/"#" cases so the deepest marker wins, the order every other
		// nested construct here is written in. Before this, "####" and deeper matched
		// nothing and fell through to the paragraph default, printing their own hashes.
		r.heading(inlineOver(subheading.ReplaceAllString(trimmed, ""), subheadingStyle()))
	case strings.HasPrefix(trimmed, "## "):
		r.heading(inlineOver(strings.TrimPrefix(trimmed, "## "), headingStyle()))
	case strings.HasPrefix(trimmed, "# "):
		r.heading(inlineOver(strings.TrimPrefix(trimmed, "# "), h1Style()))
	case r.table && tableOpen(trimmed):
		r.pending = append(r.pending, trimmed)
	case tableStart(trimmed, next):
		// The table cases sit below every other block opener and above the list ones, so
		// anything that starts a block of its own wins: "> a | b" is a quote and "## a | b"
		// a heading, delimiter row or not. A table can only begin where a PARAGRAPH would.
		r.flush()
		r.table = true
		r.pending = append(r.pending, trimmed)
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

// fenceLang reads the language tag off an opening fence's info string — the first word
// after the marker ("```go" → "go"); "" when the fence carries none.
func fenceLang(info string) string {
	fields := strings.Fields(info)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
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
//
// The callers style it through inlineOver, not Style.Render, so a heading carrying an
// inline construct renders it instead of printing its delimiters — the one block type
// that used to skip the inline pass, while table headers and quotes always had it.
func (r *docRenderer) heading(rendered string) {
	r.flush()
	r.blank()
	r.emit(ansi.Wrap(rendered, r.width, wrapBreaks))
}

// flush wraps the accumulated block and empties it. An unclosed fence is also ended
// here (EOF): its accumulated lines render through CodeBlockRenderer exactly as if the
// closing marker had arrived — the default path streams, so it has nothing pending.
func (r *docRenderer) flush() {
	if r.fence != "" && r.code != nil {
		lang, code := r.lang, r.code
		r.fence, r.code, r.lang = "", nil, ""
		r.emitCode(lang, code)
	}
	if len(r.pending) == 0 {
		return
	}
	// The block's rows belong to the lines that accumulated it — never to r.at, which
	// is usually the line that ENDED it (the blank, the next heading) and has its own
	// rows still to come.
	start, mark := r.rows, r.mark
	r.mark = -1
	r.emit(r.block())
	r.mark = mark
	if r.pendAt >= 0 {
		// Spread the block's rows across the lines that fed it rather than pinning them
		// all to its first row. A hard-wrapped paragraph is the common case in these
		// pages — three source lines re-flowing to two or three rows — and pinning left
		// a synced view several rows behind for the whole of every paragraph. The result
		// stays inside the block, so it can never run ahead of the source.
		n, span := r.pendTo-r.pendAt+1, r.rows-start
		for i := r.pendAt; i <= r.pendTo; i++ {
			r.marks[i] = start + (i-r.pendAt)*span/n
		}
		r.pendAt = -1
	}
	r.pending, r.marker, r.quote, r.table = nil, "", false, false
}

// block renders the pending lines to the finished rows of whatever block they are. It is
// split out of flush so the table case can see the lines INDIVIDUALLY: every other block
// joins them into one string first, because re-flowing the author's hard wraps is the
// whole point — a table is the one place where the line breaks are data.
func (r *docRenderer) block() string {
	if r.table {
		if out, ok := tableBlock(r.pending, r.width); ok {
			return out
		}
		// Too many columns to lay out at this width, or a table still being typed. Fall
		// through to the paragraph path — how a table rendered before it was honored at
		// all: the source stays visible and, unlike a squeezed grid, certainly fits.
	}
	joined := strings.Join(r.pending, " ")
	switch {
	case r.quote:
		return quoteBlock(joined, r.width)
	case r.marker != "":
		return hang(r.marker, inline(joined), r.width)
	}
	return ansi.Wrap(inline(joined), r.width, wrapBreaks)
}

// emitCode renders an accumulated fenced block through CodeBlockRenderer. The rows come
// out one per source line unless the renderer folds or expands them, so that is the
// condition for crediting each row to its own line rather than leaving the whole block
// to the backfill.
func (r *docRenderer) emitCode(lang string, code []string) {
	rows := CodeBlockRenderer(lang, code, r.width-len(indent))
	perLine := len(rows) == len(code)
	mark := r.mark
	for i, l := range rows {
		if perLine {
			r.mark = r.fenceAt + 1 + i // the fence marker itself is fenceAt
		}
		r.emit(indent + l)
	}
	r.mark = mark
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

// tableStart reports whether trimmed opens a GFM table: a row of pipe-separated cells
// with a delimiter row directly under it. The delimiter is the whole signal — without one
// a line of pipes is prose the reader must leave alone (a shell pipeline, an ASCII
// diagram, "| a | b |" in a page about markdown), which is why the reader grew its one
// line of lookahead rather than guessing from the pipes.
//
// The two rows' cell counts must agree, as GFM requires: a mismatch is a typo, and
// showing the source is how the author gets to see it.
func tableStart(trimmed, next string) bool {
	if !strings.Contains(trimmed, "|") {
		return false
	}
	next = strings.TrimSpace(next)
	if !strings.Contains(next, "|") || !tableDelim.MatchString(next) {
		return false
	}
	return len(tableCells(trimmed)) == len(tableCells(next))
}

// tableOpen reports whether trimmed continues an open table rather than starting a new
// block. A table runs to the first blank line or the next block-level construct, which is
// GFM's own rule. Anything else is a row, pipes or not — GFM reads a pipeless line inside
// a table as a one-cell row, and so do we.
//
// The list markers are why this exists: every other block opener has its case ABOVE the
// table's and so flushes it before this is ever called, but "- " and the ordered items sit
// below, and without rejecting them here an open table silently swallows the list under
// it. Headings and quotes are checked anyway, so the rule reads whole from one place.
func tableOpen(trimmed string) bool {
	return !strings.HasPrefix(trimmed, "#") &&
		!strings.HasPrefix(trimmed, ">") &&
		!strings.HasPrefix(trimmed, "- ") &&
		!orderedItem.MatchString(trimmed)
}

// tableCells splits one row into its cells. GFM makes the outer pipes optional, so one
// leading and one trailing pipe come off before the split: "| a | b |" and "a | b" are
// both two cells.
//
// Backslash escapes are not honored here, as nowhere else in this reader, so a cell
// cannot hold a literal pipe: one rule for every escape beats one construct quietly
// having its own. A pipe inside a code span splits the cell too — that is GFM's behavior
// as well, since the split happens before any inline parsing there as here.
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	cells := strings.Split(row, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// The horizontal alignments a delimiter row's colons can ask for.
type tableAlign int

const (
	tableLeft tableAlign = iota
	tableCenter
	tableRight
)

// tableSpec is one parsed pipe table: the header cells, the body rows — every one squared
// off to the header's cell count — and the per-column alignment read off the delimiter.
type tableSpec struct {
	head  []string
	rows  [][]string
	align []tableAlign
}

// tableAligns reads the per-column alignment off a delimiter row: ":-" left, ":-:" center,
// "-:" right, and a bare "-" left, which is GFM's default.
func tableAligns(delim string) []tableAlign {
	cells := tableCells(delim)
	out := make([]tableAlign, len(cells))
	for i, c := range cells {
		lead, trail := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":")
		switch {
		case lead && trail:
			out[i] = tableCenter
		case trail:
			out[i] = tableRight
		}
	}
	return out
}

// parseTable turns an accumulated table block — the header, its delimiter row, then the
// body rows — into a spec with every row squared off to the header's cell count: a short
// row is padded with empty cells, and cells past the last column are dropped, since there
// is no column to hold them and no width budget to give them.
//
// ok is false when the block is not (yet) a table. It has to stay total for any []string:
// gote's preview renders half-typed source on every keystroke, so it sees a header with
// nothing under it and a delimiter row still being written.
func parseTable(lines []string) (tableSpec, bool) {
	if len(lines) < 2 || !tableDelim.MatchString(strings.TrimSpace(lines[1])) {
		return tableSpec{}, false
	}
	t := tableSpec{head: tableCells(lines[0]), align: tableAligns(lines[1])}
	if len(t.align) != len(t.head) {
		return tableSpec{}, false
	}
	for _, l := range lines[2:] {
		row := make([]string, len(t.head))
		copy(row, tableCells(l)) // copy pads the short rows and drops the long ones' tails
		t.rows = append(t.rows, row)
	}
	return t, true
}

// tableCellWidth is a cell's width in display cells ONCE RENDERED, which is not its
// source's: inline draws `code` as a chip with a cell of tint on each side and drops a
// link's target entirely. Sizing a column from its source leaves a link column several
// times too wide and a chip two cells short of fitting.
func tableCellWidth(text string) int { return lipgloss.Width(inline(text)) }

// tableWidths chooses a display width for every column. A column's natural width is its
// widest rendered cell, header included; when the row fits, that is the answer — the table
// stays as narrow as its content rather than being stretched to fill the pane.
//
// When it does not fit, the widest columns give up cells first and equally (max-min
// water-filling): find the largest cap c with Σ min(nat[i], c) <= avail, which leaves
// every column narrower than c untouched and levels the rest at c. That is stable —
// nothing depends on column order — and testable, since one number decides the layout.
//
// nil ⇒ the table cannot be laid out here at all: not even tableMinCol per column fits in
// what the separators leave. At the renderer's 20-column floor that is four columns or
// more (4*3 + 3*3 = 21), and the caller shows the source instead.
func tableWidths(t tableSpec, width int) []int {
	n := len(t.head)
	if n == 0 {
		return nil
	}
	nat, widest := make([]int, n), 0
	for i, c := range t.head {
		nat[i] = max(1, tableCellWidth(c))
	}
	for _, row := range t.rows {
		for i, c := range row {
			nat[i] = max(nat[i], tableCellWidth(c))
		}
	}
	// Measured from the constant, never a hard-coded 3, so widening the separator cannot
	// desync this budget from the rule tableRule draws to it.
	total := (n - 1) * lipgloss.Width(tableSep)
	avail := width - total
	for _, w := range nat {
		total += w
		widest = max(widest, w)
	}
	if total <= width {
		return nat
	}

	// capped is the width the columns take at cap c — the water-filling level's own sum,
	// so the feasibility test and the fill are the same expression.
	capped := func(c int) int {
		sum := 0
		for _, w := range nat {
			sum += min(w, c)
		}
		return sum
	}
	level := 0
	for c := 1; c <= widest && capped(c) <= avail; c++ {
		level = c
	}
	if level < tableMinCol {
		return nil
	}
	w := make([]int, n)
	for i := range nat {
		w[i] = min(nat[i], level)
	}
	// Spread what the level left over one cell at a time, left to right, over the columns
	// it squeezed. One pass always suffices: capped(level+1)-capped(level) is exactly their
	// count and level+1 did not fit, so no column can take a second cell.
	for i, left := 0, avail-capped(level); i < n && left > 0; i++ {
		if nat[i] > level {
			w[i]++
			left--
		}
	}
	return w
}

// wrapCell folds one cell to its column, styling BEFORE wrapping — the opposite of what
// quoteBlock does, and for the opposite reason. A quote owns its whole width, so it can
// wrap raw text and style each row; a column's width is measured on the RENDERED cells, so
// wrapping the source would break in the wrong display column and split a "**bold**" into
// two literal halves.
//
// Wrapping the styled string breaks where the text actually prints, which is what makes
// every row fit its column by construction. The cost is that ansi.Wrap does not emit
// self-contained rows: a run straddling a break leaves its color OPEN at end of row, which
// would tint the padding, the separator and the whole of the next column. The terminator
// below closes it, so every fragment tableRowBlock concatenates is self-terminating and no
// cell's color can reach the one beside it. What a reset cannot do is reopen the style on
// the continuation row, so a construct straddling a break keeps its text and loses its tint
// — the mirror of the quote's edge, and milder, since no delimiters leak.
//
// The terminator is conditional so the uncolored path stays byte-clean: under the Ascii
// profile inline emits no escapes at all, and an unconditional reset would put one on the
// end of every plain row.
func wrapCell(text string, w int, base lipgloss.Style) []string {
	rows := strings.Split(ansi.Wrap(inlineOver(text, base), w, wrapBreaks), "\n")
	for i, row := range rows {
		if strings.Contains(row, "\x1b") {
			rows[i] = row + ansi.ResetStyle
		}
	}
	return rows
}

// tablePad places one wrapped cell row in its column, blanks on whichever side the
// alignment asks for. The gap is measured on the RENDERED row: its escape sequences carry
// no display cells, and a chip's are exactly what would push the column out if counted.
func tablePad(row string, w int, a tableAlign) string {
	gap := w - lipgloss.Width(row)
	if gap <= 0 {
		return row
	}
	switch a {
	case tableRight:
		return strings.Repeat(" ", gap) + row
	case tableCenter:
		return strings.Repeat(" ", gap/2) + row + strings.Repeat(" ", gap-gap/2)
	}
	return row + strings.Repeat(" ", gap)
}

// tableRowBlock lays one table row out: every cell wrapped to its column, the row as tall
// as its tallest cell, and the shorter ones padded with blank rows so the separators stay
// in column all the way down.
func tableRowBlock(cells []string, w []int, align []tableAlign, base lipgloss.Style) string {
	cols, height := make([][]string, len(w)), 1
	for i := range w {
		cols[i] = wrapCell(cells[i], w[i], base)
		height = max(height, len(cols[i]))
	}
	sep := ruleStyle().Render(tableSep)
	// A separator that ENDS a row drops its trailing space: the last column had nothing on
	// this line, which is the common shape of a wrapped row. Rendering the shorter
	// separator rather than trimming the finished row keeps this identical under every
	// color profile — styled, that space sits inside the escape sequence where a
	// trailing-space trim would never reach it. Only the separators before a padded cell
	// are affected, so the bars still line up all the way down.
	sepEnd := ruleStyle().Render(strings.TrimRight(tableSep, " "))
	rows := make([]string, height)
	for y := range rows {
		var b strings.Builder
		for i, col := range cols {
			cell := ""
			if y < len(col) {
				cell = col[y]
			}
			cell = tablePad(cell, w[i], align[i])
			if i == len(cols)-1 {
				// The last column's own padding is width the row does not need — and only
				// it can come out empty here, since every earlier column keeps its blanks.
				cell = strings.TrimRight(cell, " ")
			}
			if i > 0 {
				if cell == "" {
					b.WriteString(sepEnd)
				} else {
					b.WriteString(sep)
				}
			}
			b.WriteString(cell)
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

// tableRule is the rule under the header: a run of "─" as wide as each column, joined by
// "─┼─" so the crossing sits under the separator's bar. One Render call for the whole rule,
// so it carries one color and one reset rather than a seam per column.
func tableRule(w []int) string {
	segs := make([]string, len(w))
	for i, n := range w {
		segs[i] = strings.Repeat("─", n)
	}
	return ruleStyle().Render(strings.Join(segs, tableCross))
}

// tableBlock renders an accumulated table block: the header in the heading accent, the ─┼─
// rule under it, then the body. ok is false when the block does not parse as a table or
// will not fit at this width, and the caller falls back to the prose its source reads as.
func tableBlock(lines []string, width int) (string, bool) {
	t, ok := parseTable(lines)
	if !ok {
		return "", false
	}
	w := tableWidths(t, width)
	if w == nil {
		return "", false
	}
	out := []string{tableRowBlock(t.head, w, t.align, tableHeadStyle()), tableRule(w)}
	for _, row := range t.rows {
		out = append(out, tableRowBlock(row, w, t.align, lipgloss.Style{}))
	}
	return strings.Join(out, "\n"), true
}

// emit appends one entry — which may itself be a wrapped block of several rows, hence
// the running row count — and credits the first row to the source line that produced it.
func (r *docRenderer) emit(s string) {
	if r.mark >= 0 && r.mark < len(r.marks) && r.marks[r.mark] < 0 {
		r.marks[r.mark] = r.rows
	}
	r.rows += 1 + strings.Count(s, "\n")
	r.out = append(r.out, s)
}

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
// A construct INHERITS base for whatever it does not set itself, so bold inside an
// accent heading is bold AND accent rather than dropping to the terminal's default
// mid-line — the emphasis styles set only their attribute, which is what makes them
// compose. The code span is deliberately left out of that: its chip carries its own
// foreground and background and is meant to read the same wherever it lands.
//
// Emphasis and links RECURSE with that composed style as the new base, which is what
// renders a construct nested in another one (**the `--flag` option**, a [**bold**
// link](x)) instead of printing the inner delimiters. Only the delimiters the same
// alternative would consume are excluded by inlineAny, so a span may already contain
// a different construct — recursing is all that was missing. A code span does NOT
// recurse: its contents are literal, which is both correct markdown and pinned by
// TestRenderMarkdownInlineIsolation. Each step strips a delimiter pair, so the
// recursion is over strictly shorter text and terminates.
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
			b.WriteString(inlineOver(p.text, boldStyle().Inherit(base)))
		case inlineKindEm:
			b.WriteString(inlineOver(p.text, italicStyle().Inherit(base)))
		case inlineKindLink:
			b.WriteString(inlineOver(p.text, linkStyle().Inherit(base)))
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

// subheadingStyle is every heading below "##": the same accent, dimmed one step (see
// core.Dim, which recedes toward the terminal's ground rather than simply darkening, so
// the dimmed heading is the quieter one under a light background too). It used to be the
// theme's muted grey, which is what body-secondary text wears — a "###" read as
// de-emphasized prose rather than as a heading ranking under its section.
func subheadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.Dim(core.MarkdownAccent(), subheadingDim))
}

// subheadingDim is how far a subheading recedes: far enough that every preset accent
// lands on a different ANSI index (core.Dim steps up until it does), close enough that
// the hue still reads as the section color rather than a new one.
const subheadingDim = 0.3

// codeStyle stays on the theme's own muted grey — it reads fine under every preset
// including mono, so there is nothing to borrow.
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

// tableHeadStyle is a table's header row — the accent heading the "##" sections use, not a
// plain bold: the rule under the header is deliberately thin chrome in the border color,
// and bold alone leaves the header reading as just another body row on a low-contrast
// theme. A column's header is a heading of its column.
func tableHeadStyle() lipgloss.Style {
	return headingStyle()
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
