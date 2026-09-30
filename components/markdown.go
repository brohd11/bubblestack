package components

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/brohd11/bubblestack/core"
	"github.com/charmbracelet/x/ansi"
)

// CodeBlockRenderer, when set, renders a fenced code block's lines (with the fence's
// language tag, maybe "") to finished display lines of at most width, which are emitted
// indented. nil keeps the muted, hard-wrapped default. It lets gote add syntax
// highlighting. Set it at init; it is not for concurrent use.
var CodeBlockRenderer func(lang string, code []string, width int) []string

// inlineAny matches every inline construct in one alternation, so inline() can style in a
// single pass: sequential passes would match the ANSI earlier passes emitted. Alternation
// order is priority (leftmost-first): code spans win, "**" before "*", and images are
// matched whole so they are not half-read as links.
//
// Underscore emphasis must skip snake_case, which needs a boundary Go's regexp cannot
// express as a lookaround, so the boundary characters are matched and re-emitted by
// inlineParts. Each alternative is a named group so inlineParts can tell which fired.
var inlineAny = regexp.MustCompile(
	"(?P<code>`[^`]+`)" +
		`|(?P<strong>\*\*[^*]+\*\*)` +
		`|(?P<em>\*[^*]+\*)` +
		`|(?P<link>!?\[[^\]]+\]\([^)]*\))` +
		`|(?P<uem>(?:^|[^\p{L}\p{N}_])_[^_]+_(?:[^\p{L}\p{N}_]|$))`)

// orderedItem matches "1. " / "12) " at the start of a list line; the capture is the
// marker, which is kept verbatim rather than renumbered.
var orderedItem = regexp.MustCompile(`^(\d+[.)])\s+`)

// themeBreak matches a thematic break line: three or more of one of "-", "*", "_".
var themeBreak = regexp.MustCompile(`^(-{3,}|\*{3,}|_{3,})$`)

// subheading matches headings below "##". They share one style: a terminal has no type
// sizes to tell six levels apart.
var subheading = regexp.MustCompile(`^#{3,6} `)

const (
	bulletMark = MarkdownBullet + " "
	indent     = "  "
	// quoteBar prefixes every row of a blockquote (see quoteBlock).
	quoteBar = MarkdownQuoteBar + " "
	// wrapBreaks are extra wrap points beyond whitespace, so a long path or URL folds
	// at a separator rather than mid-name.
	wrapBreaks = "/_"
	// tableSep separates columns; tableCross is its equal-width counterpart on the header
	// rule, so the rule is exactly as wide as a row.
	tableSep   = " │ "
	tableCross = "─┼─"
	// tableMinCol is the narrowest column before a table falls back to prose.
	tableMinCol = 3
)

// RenderMarkdown folds a markdown body to width columns. It reads a subset, re-flowed
// with core's theme (styles are read per call, so a theme switch repaints):
//
//	# / ## / ###+   headings (accent; ### and deeper dimmed one step)
//	--- *** ___     a full-width rule
//	- item, 1. item bullets and numbered items with a hanging indent
//	```, ~~~        fenced code: muted, hard-wrapped, never re-flowed
//	> quote         muted paragraph with a bar down every row
//	| a | b |       GFM table; the delimiter row is required and sets alignment
//	`code` **b** *i* [text](url)   inline code, bold, italic, OSC 8 links
//
// Anything else is a paragraph. Images, HTML and backslash escapes pass through as
// literal text. A table too wide for the pane shrinks its widest columns and wraps
// cells, falling back to prose when even three cells per column will not fit.
func RenderMarkdown(body string, width int) string {
	out, _ := RenderMarkdownMapped(body, width)
	return out
}

// RenderMarkdownMapped is RenderMarkdown that also returns, per source line, the output
// row its block starts at, so a preview can follow an editor's scroll exactly. Lines
// folded into one re-flowed block share its rows proportionally. Marks never decrease
// and never run ahead of the source.
func RenderMarkdownMapped(body string, width int) (string, []int) {
	width = max(width, 20)
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

	// Lines that emitted nothing of their own (collapsed blanks, fence markers) map to the
	// last row from above them: behind, never ahead.
	last := 0
	for i, m := range marks {
		if m < 0 {
			marks[i] = last
			continue
		}
		last = m
	}

	// Keep the marks in step with any leading rows the trim removes (normally none).
	joined := strings.Join(r.out, "\n")
	out := strings.TrimLeft(joined, "\n")
	lead := len(joined) - len(out)
	for i := range marks {
		marks[i] = max(marks[i]-lead, 0)
	}
	return strings.TrimRight(out, "\n"), marks
}

// docRenderer walks the page a line at a time, accumulating a block (paragraph, bullet)
// until something ends it. Blocks are what get wrapped, so the author's hard wraps
// re-flow to the terminal.
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

// line feeds one source line to the renderer. next is the following line ("" at EOF),
// the one line of lookahead a table needs to see its delimiter row.
func (r *docRenderer) line(line, next string) {
	trimmed := strings.TrimSpace(line)

	if m := fenceMarker(trimmed); m != "" && (r.fence == "" || r.fence == m) {
		if r.fence != "" && r.code != nil {
			// Closing a highlighted block: the lines go through the injected renderer (nothing is
			// pending while a fence is open).
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
		// Before the "##"/"#" cases so the deepest marker wins.
		r.heading(inlineOver(subheading.ReplaceAllString(trimmed, ""), subheadingStyle()))
	case strings.HasPrefix(trimmed, "## "):
		r.heading(inlineOver(strings.TrimPrefix(trimmed, "## "), headingStyle()))
	case strings.HasPrefix(trimmed, "# "):
		r.heading(inlineOver(strings.TrimPrefix(trimmed, "# "), h1Style()))
	case r.table && tableOpen(trimmed):
		r.pending = append(r.pending, trimmed)
	case tableStart(trimmed, next):
		// Tables come after every other block opener and before lists, so "> a | b" is a quote
		// and "## a | b" a heading: a table can only start where a paragraph would.
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

// fenceMarker is the fence run ("```" or "~~~") opening or closing a code block on this
// line, or "". Returning the marker lets the renderer require the same one to close.
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

// quoteText strips a quote line's markers; nested ">>" collapses to one level.
func quoteText(trimmed string) string {
	for strings.HasPrefix(trimmed, ">") {
		trimmed = strings.TrimPrefix(trimmed, ">")
		trimmed = strings.TrimPrefix(trimmed, " ")
	}
	return trimmed
}

// heading emits an already-styled heading under a separator, wrapped like any block so a
// long heading fits a narrow pane. Callers style it through inlineOver so inline
// constructs inside it render.
func (r *docRenderer) heading(rendered string) {
	r.flush()
	r.blank()
	r.emit(wrapText(rendered, r.width))
}

// flush wraps the pending block and empties it. An unclosed fence at EOF is rendered as
// if it had closed.
func (r *docRenderer) flush() {
	if r.fence != "" && r.code != nil {
		lang, code := r.lang, r.code
		r.fence, r.code, r.lang = "", nil, ""
		r.emitCode(lang, code)
	}
	if len(r.pending) == 0 {
		return
	}
	// The block's rows belong to the lines that accumulated it, not r.at (usually the line
	// that ended it).
	start, mark := r.rows, r.mark
	r.mark = -1
	r.emit(r.block())
	r.mark = mark
	if r.pendAt >= 0 {
		// Spread the rows across the lines that fed the block, so a synced view does not lag a
		// whole paragraph behind. The result stays inside the block.
		n, span := r.pendTo-r.pendAt+1, r.rows-start
		for i := r.pendAt; i <= r.pendTo; i++ {
			r.marks[i] = start + (i-r.pendAt)*span/n
		}
		r.pendAt = -1
	}
	r.pending, r.marker, r.quote, r.table = nil, "", false, false
}

// block renders the pending lines. Split from flush because a table needs its lines
// individually; every other block joins them to re-flow the author's wraps.
func (r *docRenderer) block() string {
	if r.table {
		if out, ok := tableBlock(r.pending, r.width); ok {
			return out
		}
		// Too many columns for this width, or a table still being typed: render as a paragraph,
		// which keeps the source visible and always fits.
	}
	joined := strings.Join(r.pending, " ")
	switch {
	case r.quote:
		return quoteBlock(joined, r.width)
	case r.marker != "":
		return hang(r.marker, inline(joined), r.width)
	}
	return wrapText(inline(joined), r.width)
}

// emitCode renders a fenced block through CodeBlockRenderer. Rows are credited line by
// line when the renderer kept one row per source line; otherwise the backfill spreads
// them.
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

// wrapText is ansi.Wrap plus one fix: when the token after a break opens with a style,
// ansi.Wrap leaves the preceding space at the end of the row, making it one cell too
// wide (and clipped). Every wrap in this file goes through here.
func wrapText(text string, w int) string {
	rows := strings.Split(ansi.Wrap(text, w, wrapBreaks), "\n")
	for i, row := range rows {
		if ansi.StringWidth(row) > w {
			rows[i] = trimWrappedRow(row)
		}
	}
	return strings.Join(rows, "\n")
}

// trailingSGR matches the style sequences ansi.Wrap can leave stranded at the end of a
// wrapped row. lipgloss emits only SGR on this path.
var trailingSGR = regexp.MustCompile(`(?:\x1b\[[0-9;]*m)+$`)

// trimWrappedRow drops the trailing spaces sitting between a row's last visible
// character and any style sequence stranded after it.
func trimWrappedRow(row string) string {
	tail := trailingSGR.FindString(row)
	return strings.TrimRight(strings.TrimSuffix(row, tail), " ") + tail
}

// quoteBlock wraps a quote and bars every row. It wraps the raw text and styles each row
// afterwards: ansi.Wrap rows can depend on a color opened on the row above, and the
// bar's reset would wipe it. A construct straddling a break therefore renders literally.
func quoteBlock(text string, width int) string {
	w := max(width-lipgloss.Width(quoteBar), 1)
	bar := ruleStyle().Render(quoteBar)
	rows := strings.Split(wrapText(text, w), "\n")
	for i, row := range rows {
		rows[i] = bar + inlineOver(row, quoteTextStyle())
	}
	return strings.Join(rows, "\n")
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

// hang wraps text under a marker with continuation rows indented by the marker's width,
// so the entry reads as one unit.
func hang(marker, text string, width int) string {
	mw := lipgloss.Width(marker)
	return hangRows(wrapText(text, max(width-mw, 1)), marker, strings.Repeat(" ", mw))
}

// hangRows prefixes the first line of wrapped with first and every other line with rest.
func hangRows(wrapped, first, rest string) string {
	rows := strings.Split(wrapped, "\n")
	for i := range rows {
		if i == 0 {
			rows[i] = first + rows[i]
		} else {
			rows[i] = rest + rows[i]
		}
	}
	return strings.Join(rows, "\n")
}

// inline styles the spans inside a block of prose. Styling before wrapping is safe:
// ansi.Wrap measures display cells, not bytes.
func inline(s string) string { return inlineOver(s, lipgloss.Style{}) }

// inlineOver is inline with a base style for the runs between constructs (a blockquote
// needs this: styling the finished row fails because each span ends in a reset).
// Constructs inherit base for what they do not set, so bold inside an accent heading
// stays accent. Code spans keep their own colors.
//
// Emphasis and links recurse with the composed style as the new base, so nested
// constructs render; code spans do not (their contents are literal, pinned by
// TestRenderMarkdownInlineIsolation). Each step strips a delimiter pair, so it
// terminates. The zero base leaves the plain runs unchanged.
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
			// The target is carried as an OSC 8 hyperlink around the finished label. It costs no
			// cells, so wrapping and table widths are unaffected, and ScanLinks can recover each
			// link's row and column after wrapping. It is applied outside the recursion because
			// lipgloss does not inherit the link property.
			styled := inlineOver(p.text, linkStyle().Inherit(base))
			if p.dest != "" {
				styled = ansi.SetHyperlink(p.dest) + styled + ansi.ResetHyperlink()
			}
			b.WriteString(styled)
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

// inlinePart is one classified inlineAny match: the styled text, plus the boundary
// characters the underscore form had to match (re-emitted verbatim).
type inlinePart struct {
	kind           int
	prefix, suffix string
	text           string
	dest           string // a link's target, the one construct whose payload outlives its delimiters
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
		// The regexp matched "[label](target)" whole, so the split is arithmetic: the
		// label ends at the first "]", and the target is what the parens hold after it.
		close := strings.Index(m, "]")
		return inlinePart{
			kind: inlineKindLink,
			text: m[1:close],
			dest: strings.TrimSuffix(m[close+2:], ")"),
		}
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

// MarkdownStyles is the previewer's palette, resolved from the current theme. Other
// markdown views (gote's live preview) read it so the two stay alike; it is a snapshot,
// so take a fresh one after a theme or background change.
type MarkdownStyles struct {
	Heading, H1, Subheading lipgloss.Style
	Bold, Italic, Link      lipgloss.Style
	Code, CodeSpan          lipgloss.Style // a fenced block's text; an inline span's chip
	Rule, QuoteText         lipgloss.Style // rules, bars and table lines; a quote's prose
}

// CurrentMarkdownStyles is RenderMarkdown's palette as it stands now.
func CurrentMarkdownStyles() MarkdownStyles {
	return MarkdownStyles{
		Heading: headingStyle(), H1: h1Style(), Subheading: subheadingStyle(),
		Bold: boldStyle(), Italic: italicStyle(), Link: linkStyle(),
		Code: codeStyle(), CodeSpan: codeSpanStyle(),
		Rule: ruleStyle(), QuoteText: quoteTextStyle(),
	}
}

// The previewer's glyphs, for views that draw the same constructs.
const (
	MarkdownBullet   = "•"
	MarkdownQuoteBar = "│"
)

// h1Style is the top-level heading: the accent heading underlined, so a page's "#"
// still outranks the "##" sections under it in a terminal with no type sizes.
func h1Style() lipgloss.Style {
	return headingStyle().Underline(true)
}

// headingStyle and the other accent styles use core.MarkdownAccent, not FocusedColor, so
// a theme whose accent is the terminal's own extreme (mono) can borrow a visible one.
func headingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.Resolve(core.MarkdownAccent()))
}

func boldStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true)
}

func italicStyle() lipgloss.Style {
	return lipgloss.NewStyle().Italic(true)
}

func linkStyle() lipgloss.Style {
	return lipgloss.NewStyle().Underline(true).Foreground(core.Resolve(core.MarkdownAccent()))
}

// subheadingStyle is headings below "##": the accent dimmed one step (core.Dim recedes
// toward the ground, so it is quieter on light backgrounds too).
func subheadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.Resolve(core.Dim(core.MarkdownAccent(), subheadingDim)))
}

// subheadingDim is how far a subheading recedes: enough to change ANSI index, little
// enough to keep the section's hue.
const subheadingDim = 0.3

// codeStyle stays on the theme's own muted grey — it reads fine under every preset
// including mono, so there is nothing to borrow.
func codeStyle() lipgloss.Style {
	return core.MutedStyle()
}

// codeSpanStyle tints the background too, to set `code` apart from accent text. The tint
// is one step off the terminal's ground.
func codeSpanStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(core.Resolve(core.MarkdownAccent())).
		Background(core.Resolve(core.Color{Light: 254, Dark: 236}))
}

// ruleStyle draws the thin separators around a code block and the bar down a
// blockquote, in the theme's border color so they read as chrome rather than content.
func ruleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.BorderColor)
}

// quoteTextStyle mutes a blockquote's prose, so a quote reads as set apart from the
// body text around it and not merely indented behind a bar.
func quoteTextStyle() lipgloss.Style {
	return core.MutedStyle()
}
