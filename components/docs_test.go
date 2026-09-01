package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// codeChip is how a rendered inline code span looks: the text with a cell of the
// tinted background on each side.
func codeChip(text string) string { return codeSpanStyle().Render(" " + text + " ") }

// render is RenderMarkdown with the styling stripped — what the page READS as, for
// the assertions that care about text and layout rather than color.
func render(body string, width int) string {
	return ansi.Strip(RenderMarkdown(body, width))
}

// TestRenderMarkdownHeadings: each level gets its own style and a blank line above,
// and the marker itself never reaches the page.
func TestRenderMarkdownHeadings(t *testing.T) {

	got := RenderMarkdown("intro\n\n# One\n\n## Two\n\n### Three\n", 40)
	for _, want := range []string{
		h1Style().Render("One"),
		headingStyle().Render("Two"),
		subheadingStyle().Render("Three"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing heading render %q in:\n%s", want, got)
		}
	}
	if strings.Contains(ansi.Strip(got), "#") {
		t.Error("heading markers must not reach the page")
	}
}

// TestRenderMarkdownDeepHeadings: "###" through "######" are all headings and all wear
// the same dimmed-accent style. They used to match no case at all below three hashes and
// fell through to the paragraph default, which printed the markers as prose.
func TestRenderMarkdownDeepHeadings(t *testing.T) {

	for _, marker := range []string{"###", "####", "#####", "######"} {
		got := RenderMarkdown("intro\n\n"+marker+" Deep\n", 40)
		if want := subheadingStyle().Render("Deep"); !strings.Contains(got, want) {
			t.Errorf("%q: missing heading render %q in:\n%s", marker, want, got)
		}
		if strings.Contains(ansi.Strip(got), "#") {
			t.Errorf("%q: heading marker reached the page:\n%s", marker, ansi.Strip(got))
		}
	}
}

// TestSubheadingRanksUnderHeading: a subheading is the SECTION's accent dimmed, not the
// muted grey body-secondary text wears — the distinction the level is there to draw.
func TestSubheadingRanksUnderHeading(t *testing.T) {

	sub := subheadingStyle().GetForeground()
	if sub == headingStyle().GetForeground() {
		t.Error("a subheading must not render identically to the heading above it")
	}
	if sub == codeStyle().GetForeground() {
		t.Error("a subheading must not fall back to the muted grey of body-secondary text")
	}
}

// Seven hashes is not a heading in markdown, and must stay prose here too.
func TestRenderMarkdownSevenHashesIsProse(t *testing.T) {
	if got := render("####### Deep\n", 40); !strings.Contains(got, "####### Deep") {
		t.Errorf("seven hashes should render as prose, got:\n%s", got)
	}
}

// TestRenderMarkdownInline: the four inline constructs render styled with their
// delimiters dropped.
func TestRenderMarkdownInline(t *testing.T) {

	got := RenderMarkdown("a **b** and *c* and `d` and [e](http://f) end", 80)
	for _, want := range []string{
		boldStyle().Render("b"),
		italicStyle().Render("c"),
		codeChip("d"),
		linkStyle().Render("e"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing inline render %q in %q", want, got)
		}
	}
	if plain := ansi.Strip(got); plain != "a b and c and  d  and e end" {
		t.Errorf("delimiters should be dropped, got %q", plain)
	}
}

// TestRenderMarkdownInlineIsolation pins the two ways a naive implementation breaks
// here, both of which it did before this test existed:
//
//   - running the patterns in sequence lets a later one match the ANSI an earlier one
//     emitted — the link pattern reads an escape's "\x1b[" as a label bracket and
//     swallows the whole line up to the next "](…)";
//   - markup inside a code span must stay literal.
func TestRenderMarkdownInlineIsolation(t *testing.T) {

	// Bold, em and code all preceding a link on one line: the link must come out as
	// just its label, and everything before it must survive intact.
	line := "**bold**, *em*, `code` and a [link](http://x) in it."
	if got := render(line, 80); got != "bold, em,  code  and a link in it." {
		t.Errorf("earlier spans leaked into the link match: %q", got)
	}

	// Markup inside a code span is literal, and the span keeps the code style.
	got := RenderMarkdown("`**not bold**` here", 80)
	if !strings.Contains(got, codeChip("**not bold**")) {
		t.Errorf("a code span's contents must stay literal, got %q", got)
	}
}

// TestRenderMarkdownHeadingInline: a heading is prose too — inline markup inside one
// renders instead of printing its delimiters. Headings were the one block type that
// skipped the inline pass (table headers and quotes always had it), so "## the `--flag`
// option" used to reach the page with its backticks on.
func TestRenderMarkdownHeadingInline(t *testing.T) {

	got := RenderMarkdown("# one **b**\n\n## two `c`\n\n### three *e*\n", 60)
	if plain := ansi.Strip(got); strings.ContainsAny(plain, "*`") {
		t.Errorf("heading delimiters must not reach the page, got %q", plain)
	}
	// The construct inherits the heading it sits in rather than dropping to the
	// terminal default mid-line: bold in an h1 keeps the accent and the underline.
	for _, want := range []string{
		boldStyle().Inherit(h1Style()).Render("b"),
		codeChip("c"),
		italicStyle().Inherit(subheadingStyle()).Render("e"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing heading inline render %q in %q", want, got)
		}
	}
}

// TestRenderMarkdownInlineNesting: a construct nested in another renders, because
// inlineOver recurses with the outer style as the new base. inlineAny only ever
// excluded the delimiter the same alternative would consume, so these all MATCHED
// before — they just came out with the inner delimiters printed.
func TestRenderMarkdownInlineNesting(t *testing.T) {

	got := RenderMarkdown("a **bold `--flag` opt** and [**b** l](http://x)\n", 80)
	if plain := ansi.Strip(got); plain != "a bold  --flag  opt and b l" {
		t.Errorf("nested delimiters should be dropped, got %q", plain)
	}
	for _, want := range []string{
		codeChip("--flag"),                           // the chip keeps its own colors
		boldStyle().Render("bold "),                  // the run around it stays bold
		boldStyle().Inherit(linkStyle()).Render("b"), // bold in a link is both
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing nested render %q in %q", want, got)
		}
	}

	// The floor under the recursion: a code span is literal all the way down.
	if lit := RenderMarkdown("`a **b** [c](d)`\n", 80); !strings.Contains(lit, codeChip("a **b** [c](d)")) {
		t.Errorf("a code span must not recurse, got %q", lit)
	}
}

// TestRenderMarkdownLists: bullets get the bullet mark, ordered items keep the
// author's own number, and continuation rows hang under the marker's width.
func TestRenderMarkdownLists(t *testing.T) {
	rows := strings.Split(render("- one\n- two\n", 40), "\n")
	if rows[0] != bulletMark+"one" || rows[1] != bulletMark+"two" {
		t.Errorf("bullets should carry %q, got %q", bulletMark, rows)
	}

	// The numbers are the author's, not a renumbering: 1. then 10.
	rows = strings.Split(render("1. one\n10) ten\n", 40), "\n")
	if rows[0] != "1. one" || rows[1] != "10) ten" {
		t.Errorf("ordered markers should be kept verbatim, got %q", rows)
	}

	// A wrapped "10. " item hangs its continuation under four columns, not two.
	rows = strings.Split(render("10. aaaa bbbb cccc dddd eeee ffff", 20), "\n")
	if len(rows) < 2 {
		t.Fatalf("expected the item to wrap, got %q", rows)
	}
	if !strings.HasPrefix(rows[1], "    ") || strings.HasPrefix(rows[1], "     ") {
		t.Errorf("continuation should hang under the marker's 4 columns, got %q", rows[1])
	}
}

// TestRenderMarkdownPassthrough: what the reader does not implement is shown as its
// source rather than swallowed — the user can still see it is there. The pipe row is
// now also the pin on tables needing a delimiter row: with no dashes under it, a line
// of pipes is prose (a shell pipeline, a page about markdown) and must stay literal.
func TestRenderMarkdownPassthrough(t *testing.T) {
	for _, src := range []string{
		"![img](x.png) after",
		"snake_case_name and another_one",
		"| a | b |",
		"<div>html</div>",
	} {
		if got := render(src, 80); !strings.Contains(got, ansi.Strip(src)) {
			t.Errorf("%q should pass through, got %q", src, got)
		}
	}
}

// TestRenderMarkdownWidth: everything is folded to the width the caller gave, styling
// included — the DocScreen and the preview pane both size their box from it.
func TestRenderMarkdownWidth(t *testing.T) {

	body := strings.Join([]string{
		"# A heading long enough that it would overflow a narrow pane on its own",
		"",
		"A paragraph with **bold** and `code` that definitely needs to wrap somewhere.",
		"",
		"- a bullet that also runs well past the width it has been given here",
		"12. an ordered item that runs past the width it has been given as well",
		"",
		"```",
		"a fenced line that is much too long to fit inside the given width at all",
		"```",
		"",
		"| Flag | What it does | Default |",
		"|------|--------------|---------|",
		"| `-v` | noisy logging that runs on for quite a while | off |",
	}, "\n")

	const width = 40
	for _, line := range strings.Split(RenderMarkdown(body, width), "\n") {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("line exceeds width %d (got %d): %q", width, w, ansi.Strip(line))
		}
	}
}

// TestPlain strips the same constructs inline styles, for the docs index's one-line
// descriptions. An image keeps its source: there is no sensible text to reduce it to.
func TestPlain(t *testing.T) {
	if got := plain("a **b** *c* `d` [e](f) ![g](h)"); got != "a b c d e ![g](h)" {
		t.Errorf("plain: got %q", got)
	}
}

// TestParseDocPageUnchanged: the index still reads its title and description out of a
// page, with the description stripped of markup by plain.
func TestParseDocPageUnchanged(t *testing.T) {
	p := parseDocPage("# Title\n\nA **bold** `intro` line.\n\nmore\n")
	if p.Title != "Title" {
		t.Errorf("title: got %q", p.Title)
	}
	if p.Desc != "A bold intro line." {
		t.Errorf("desc should be stripped of markup, got %q", p.Desc)
	}
	if strings.Contains(p.Body, "# Title") {
		t.Error("the title line should not be part of the body")
	}
}

// TestRenderMarkdownUnderscoreEm: "_em_" italicizes prose but must leave snake_case
// identifiers alone — the distinction the manual pages depend on, and the reason the
// pattern matches its boundary characters instead of using a lookaround Go's regexp
// does not have.
func TestRenderMarkdownUnderscoreEm(t *testing.T) {

	for _, tc := range []struct{ src, want string }{
		{"a _em_ b", "a " + italicStyle().Render("em") + " b"},
		{"_leading_ word", italicStyle().Render("leading") + " word"},
		{"word _trailing_", "word " + italicStyle().Render("trailing")},
	} {
		if got := RenderMarkdown(tc.src, 80); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.src, got, tc.want)
		}
	}

	// Identifiers stay literal: their underscores always follow an alphanumeric.
	for _, src := range []string{
		"snake_case_name and another_one",
		"a_b_c and x_y_z_w",
		"call some_func_here(arg)",
	} {
		if got := render(src, 80); got != src {
			t.Errorf("%q should stay literal, got %q", src, got)
		}
	}

	// Inside a code span it is literal like everything else.
	if got := RenderMarkdown("`_not em_` here", 80); !strings.Contains(got, codeChip("_not em_")) {
		t.Errorf("underscores in a code span must stay literal, got %q", got)
	}

	// The documented edge: the consumed trailing boundary swallows a construct that
	// starts on that exact character. A space between them behaves normally.
	if got := render("_a_*b* edge", 80); got != "a*b* edge" {
		t.Errorf("adjacent construct edge: got %q", got)
	}
	if got := render("_a_ *b* fine", 80); got != "a b fine" {
		t.Errorf("a separated construct should still render, got %q", got)
	}
}

// TestRenderMarkdownCodeSpanBackground: an inline span is tinted, while a fenced block
// keeps foreground-only styling because a background would break on literal tabs.
func TestRenderMarkdownCodeSpanBackground(t *testing.T) {

	if codeSpanStyle().GetBackground() == codeStyle().GetBackground() {
		t.Error("an inline code span should be distinguishable from a block by its background")
	}
	got := RenderMarkdown("a `span` here\n\n```\nblock\n```\n", 40)
	if !strings.Contains(got, codeChip("span")) {
		t.Errorf("the inline span should carry the tinted style, got %q", got)
	}
	if !strings.Contains(got, codeStyle().Render(indent+"block")) {
		t.Errorf("the block should keep foreground-only styling, got %q", got)
	}
}

// TestRenderMarkdownCodeBlockSpacing: fenced blocks use indentation and blank-line
// separation, not the rules an older renderer drew around them. Closed and unclosed
// fences share that rule-free layout.
func TestRenderMarkdownCodeBlockSpacing(t *testing.T) {
	const width = 30
	rule := strings.Repeat("─", width)

	rows := strings.Split(render("intro\n\n```\ncode\n```\n\ntail\n", width), "\n")
	want := []string{"intro", "", indent + "code", "", "tail"}
	if !slices.Equal(rows, want) {
		t.Fatalf("closed fence rows = %q, want %q", rows, want)
	}
	if strings.Contains(strings.Join(rows, "\n"), rule) {
		t.Fatalf("a fenced block should not draw boundary rules, got %q", rows)
	}

	rows = strings.Split(render("```\ncode\n", width), "\n")
	want = []string{indent + "code", indent}
	if !slices.Equal(rows, want) {
		t.Fatalf("unclosed fence rows = %q, want %q", rows, want)
	}
	if strings.Contains(strings.Join(rows, "\n"), rule) {
		t.Fatalf("an unclosed fence should not draw a boundary rule, got %q", rows)
	}

	body := "intro\n```go\ncode\n```\ntail\n"
	defaultLayout := render(body, width)
	CodeBlockRenderer = func(_ string, code []string, _ int) []string {
		return append([]string(nil), code...)
	}
	t.Cleanup(func() { CodeBlockRenderer = nil })
	if injectedLayout := render(body, width); injectedLayout != defaultLayout {
		t.Fatalf("injecting a code renderer changed layout:\ndefault:  %q\ninjected: %q", defaultLayout, injectedLayout)
	}
}

// TestRenderMarkdownThemeBreak: a line of three or more of one of "-", "*", "_" is a
// thematic break — a dim rule across the full width. A line merely CONTAINING such a
// run is not one.
func TestRenderMarkdownThemeBreak(t *testing.T) {
	const width = 30
	rule := strings.Repeat("─", width)

	for _, marker := range []string{"---", "***", "___", "-----"} {
		rows := strings.Split(render("above\n\n"+marker+"\n\nbelow\n", width), "\n")
		found := false
		for _, r := range rows {
			if r == rule {
				found = true
			}
		}
		if !found {
			t.Errorf("%q should render the full-width rule, got %q", marker, rows)
		}
	}

	if got := render("**bold**\n", width); strings.Contains(got, rule) {
		t.Errorf("an emphasized line is not a thematic break, got %q", got)
	}
	if got := render("a --- b\n", width); strings.Contains(got, rule) {
		t.Errorf("a run mid-line is not a thematic break, got %q", got)
	}
}

// TestRenderMarkdownCodeBlockRenderer: with CodeBlockRenderer set, a fenced block's
// lines accumulate and go through it once — language tag and fold width included —
// and its output lands verbatim (indented), with the same blank-line separation as the
// default path. An unclosed fence still flushes through it at EOF. The
// renderer is a process-global seam, so the test restores nil on the way out.
func TestRenderMarkdownCodeBlockRenderer(t *testing.T) {
	type call struct {
		lang  string
		code  []string
		width int
	}
	var calls []call
	CodeBlockRenderer = func(lang string, code []string, width int) []string {
		calls = append(calls, call{lang, append([]string{}, code...), width})
		out := make([]string, len(code))
		for i, l := range code {
			out[i] = "R:" + l
		}
		return out
	}
	t.Cleanup(func() { CodeBlockRenderer = nil })

	const width = 30
	rule := strings.Repeat("─", width)

	rows := strings.Split(render("intro\n\n```go\nfmt.Println()\nx\n```\n\ntail\n", width), "\n")
	if len(calls) != 1 {
		t.Fatalf("the block should go through the renderer once, got %d calls", len(calls))
	}
	if c := calls[0]; c.lang != "go" || c.width != width-len(indent) ||
		len(c.code) != 2 || c.code[0] != "fmt.Println()" || c.code[1] != "x" {
		t.Errorf("the renderer should get lang, raw lines and fold width, got %+v", c)
	}
	want := []string{"intro", "", indent + "R:fmt.Println()", indent + "R:x", "", "tail"}
	if len(rows) != len(want) {
		t.Fatalf("got %q, want %q", rows, want)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("row %d = %q, want %q (all rows %q)", i, rows[i], want[i], rows)
		}
	}
	if strings.Contains(strings.Join(rows, "\n"), rule) {
		t.Error("the highlighted path draws no rules around the block")
	}

	// An unclosed fence flushes through the renderer at EOF (the source's trailing
	// newline is a blank content line inside the still-open block).
	calls = nil
	render("```py\nunclosed\n", width)
	if len(calls) != 1 || calls[0].lang != "py" ||
		len(calls[0].code) != 2 || calls[0].code[0] != "unclosed" || calls[0].code[1] != "" {
		t.Errorf("an unclosed fence should flush through the renderer, got %+v", calls)
	}
}

// TestRenderMarkdownBorrowedAccent: under mono the page must not flatten — headings,
// links and code spans render in the borrowed accent, not in mono's own near-white
// FocusedColor which is what body text already is.
func TestRenderMarkdownBorrowedAccent(t *testing.T) {
	prev := core.CurrentTheme()
	t.Cleanup(func() { core.SetTheme(prev) })

	core.SetTheme("mono")
	got := RenderMarkdown("## Section\n\nwith `code` and [link](http://x)\n", 60)
	flat := lipgloss.NewStyle().Bold(true).Foreground(core.FocusedColor).Render("Section")
	if strings.Contains(got, flat) {
		t.Error("mono should not render a heading in its own body-text accent")
	}
	if !strings.Contains(got, headingStyle().Render("Section")) {
		t.Errorf("the heading should use the borrowed accent, got %q", got)
	}
}

// TestRenderMarkdownTildeFence: "~~~" opens a code block like "```", and only the
// marker that opened one can close it — otherwise the other marker appearing inside a
// block (which is exactly what a block ABOUT markdown contains) would end it early.
func TestRenderMarkdownTildeFence(t *testing.T) {
	const width = 30

	rows := strings.Split(render("~~~\ncode\n~~~\n", width), "\n")
	if !slices.Equal(rows, []string{indent + "code"}) {
		t.Fatalf("a ~~~ block should render like a ``` one, got %q", rows)
	}

	// A ``` line inside a ~~~ block is content, not a terminator.
	rows = strings.Split(render("~~~\n```\nstill inside\n~~~\nafter\n", width), "\n")
	if rows[0] != indent+"```" || rows[1] != indent+"still inside" {
		t.Errorf("the other marker should stay block content, got %q", rows)
	}
	if rows[len(rows)-1] != "after" {
		t.Errorf("the matching marker should close the block, got %q", rows)
	}

	// And symmetrically: a ~~~ line inside a ``` block.
	rows = strings.Split(render("```\n~~~\nstill inside\n```\nafter\n", width), "\n")
	if rows[0] != indent+"~~~" || rows[1] != indent+"still inside" {
		t.Errorf("the other marker should stay block content, got %q", rows)
	}
	if rows[len(rows)-1] != "after" {
		t.Errorf("the matching marker should close the block, got %q", rows)
	}
}

// TestRenderMarkdownBlockquote: quotes re-flow like a paragraph but carry the bar on
// EVERY row — a marker only on the first would read as a list item.
func TestRenderMarkdownBlockquote(t *testing.T) {
	const width = 30

	// Consecutive "> " lines join into one block and re-wrap to the width.
	rows := strings.Split(render("> one two three four five six seven\n> eight nine\n", width), "\n")
	if len(rows) < 2 {
		t.Fatalf("the quote should have wrapped, got %q", rows)
	}
	for i, row := range rows {
		if !strings.HasPrefix(row, quoteBar) {
			t.Errorf("row %d should carry the bar, got %q", i, row)
		}
		if w := lipgloss.Width(row); w > width {
			t.Errorf("row %d exceeds the width: %q", i, row)
		}
	}
	if joined := strings.ReplaceAll(strings.Join(rows, " "), quoteBar, ""); !strings.Contains(joined, "seven eight nine") {
		t.Errorf("consecutive quote lines should join and re-flow, got %q", rows)
	}

	// A blank line ends the quote; the prose after it is not barred.
	rows = strings.Split(render("> quoted\n\nafter\n", width), "\n")
	if rows[len(rows)-1] != "after" {
		t.Errorf("a blank line should end the quote, got %q", rows)
	}

	// ">" with no trailing space still works, and a nested ">>" renders flat.
	if got := render(">tight\n", width); got != quoteBar+"tight" {
		t.Errorf("a bare > marker should still quote, got %q", got)
	}
	if got := render("> > nested\n", width); got != quoteBar+"nested" {
		t.Errorf("a nested quote should collapse to one level, got %q", got)
	}
}

// TestRenderMarkdownBlockquoteStyling: the bar is chrome (border color) and the prose
// is muted from UNDERNEATH the inline spans — styling a finished row instead would
// have each span's reset drop the tint for everything after it.
func TestRenderMarkdownBlockquoteStyling(t *testing.T) {

	got := RenderMarkdown("> quoted `code` and **bold** and tail\n", 80)
	if !strings.HasPrefix(got, ruleStyle().Render(quoteBar)) {
		t.Errorf("the bar should be in the border color, got %q", got)
	}
	if !strings.Contains(got, codeChip("code")) {
		t.Errorf("inline spans inside a quote should still render, got %q", got)
	}
	// The run AFTER the last inline span must still be muted — the regression the
	// base-style pass exists to prevent.
	if !strings.Contains(got, quoteTextStyle().Render(" and tail")) {
		t.Errorf("the prose after an inline span should stay muted, got %q", got)
	}
}

// TestRenderMarkdownBlockquoteWrappedRowsStayMuted is the regression behind
// quoteBlock's wrap-then-style order. ansi.Wrap does not emit self-contained rows — a
// continuation row can rely on a color opened on the row above — so prefixing the
// bar (which carries a reset) to a pre-styled row left every row after the first
// untinted. Each row must now carry its own muted run.
func TestRenderMarkdownBlockquoteWrappedRowsStayMuted(t *testing.T) {

	const width = 30
	got := RenderMarkdown("> one two three four five six seven eight nine ten eleven twelve\n", width)
	rows := strings.Split(got, "\n")
	if len(rows) < 3 {
		t.Fatalf("expected the quote to wrap over several rows, got %d: %q", len(rows), rows)
	}
	bar := ruleStyle().Render(quoteBar)
	for i, row := range rows {
		if !strings.HasPrefix(row, bar) {
			t.Errorf("row %d should open with the styled bar, got %q", i, row)
		}
		text := strings.TrimPrefix(row, bar)
		if text != quoteTextStyle().Render(ansi.Strip(text)) {
			t.Errorf("row %d should be muted in full, got %q", i, row)
		}
	}
}

// TestRenderMarkdownMapped: the mapped render is the same text plus, per source line,
// the output row that line's block starts at. The marks are what a preview pane
// scrolls to, so the two properties that matter are that they never go backwards and
// that a heading's mark actually lands on the row carrying that heading.
func TestRenderMarkdownMapped(t *testing.T) {
	const width = 40
	body := "# Title\n" +
		"\n" +
		"a paragraph hard-wrapped by its author\n" +
		"across three source lines that the render\n" +
		"re-flows into fewer rows than it was given\n" +
		"\n" +
		"## Section\n" +
		"\n" +
		"- one\n" +
		"- two\n" +
		"\n" +
		"```go\n" +
		"code()\n" +
		"```\n" +
		"\n" +
		"### Tail\n"

	got, marks := RenderMarkdownMapped(body, width)
	if want := RenderMarkdown(body, width); got != want {
		t.Fatalf("the mapped render must match RenderMarkdown's, got:\n%s\nwant:\n%s", got, want)
	}

	src := strings.Split(body, "\n")
	if len(marks) != len(src) {
		t.Fatalf("got %d marks for %d source lines", len(marks), len(src))
	}

	rows := strings.Split(got, "\n")
	for i, m := range marks {
		if i > 0 && m < marks[i-1] {
			t.Fatalf("mark %d (%d) goes backwards from %d", i, m, marks[i-1])
		}
		if m > len(rows) {
			t.Fatalf("mark %d is row %d, past the render's %d rows", i, m, len(rows))
		}
	}

	// Headings and list items are the anchors worth pinning: they are exactly where a
	// proportional estimate drifts, since the render adds rows (the blank above every
	// heading) that the source does not have.
	for i, line := range src {
		text := ""
		switch {
		case strings.HasPrefix(line, "#"):
			text = strings.TrimLeft(line, "# ")
		case strings.HasPrefix(line, "- "):
			text = strings.TrimPrefix(line, "- ")
		default:
			continue
		}
		if row := ansi.Strip(rows[marks[i]]); !strings.Contains(row, text) {
			t.Errorf("source line %d (%q) marks row %d = %q, which does not carry it",
				i, line, marks[i], row)
		}
	}

	// A re-flowed paragraph has no row per source line, so its three lines share out the
	// block's rows: each mark lands inside the block (line 5 is the blank after it) and
	// they advance through it rather than all pinning to its first row.
	for i := 2; i <= 4; i++ {
		if marks[i] < marks[2] || marks[i] >= marks[5] {
			t.Errorf("paragraph line %d marks row %d, outside the block's rows [%d,%d)",
				i, marks[i], marks[2], marks[5])
		}
	}
	if marks[4] == marks[2] {
		t.Errorf("the paragraph's lines should advance through its rows, got %v", marks[2:5])
	}
	// The fence's own line and its content: the streaming path emits a row per code
	// line, so the code line marks the row carrying it rather than the block's start.
	if row := ansi.Strip(rows[marks[12]]); !strings.Contains(row, "code()") {
		t.Errorf("the fenced line marks row %d = %q, which does not carry it", marks[12], row)
	}
}

// TestRenderMarkdownMappedCodeBlock: the highlighted path accumulates a fenced block
// and emits it in one go at the closing marker, so without emitCode's per-line credit
// every line of a long block would anchor at the block's first row. It only holds when
// the injected renderer answers a row per line, which is the contract it advertises.
func TestRenderMarkdownMappedCodeBlock(t *testing.T) {
	CodeBlockRenderer = func(_ string, code []string, _ int) []string {
		out := make([]string, len(code))
		for i, l := range code {
			out[i] = "R:" + l
		}
		return out
	}
	t.Cleanup(func() { CodeBlockRenderer = nil })

	body := "intro\n\n```go\none()\ntwo()\nthree()\n```\n\ntail\n"
	got, marks := RenderMarkdownMapped(body, 30)
	rows := strings.Split(ansi.Strip(got), "\n")

	// Source lines 3..5 are the code; each should mark the row carrying it.
	for i, want := range map[int]string{3: "one()", 4: "two()", 5: "three()"} {
		if row := rows[marks[i]]; !strings.Contains(row, want) {
			t.Errorf("code line %d marks row %d = %q, want it to carry %q", i, marks[i], row, want)
		}
	}
	// The fence markers themselves have no row of their own: the opener falls back to
	// the blank before the block, the closer to the block's last row — behind, not ahead.
	if marks[2] > marks[3] || marks[6] < marks[5] {
		t.Errorf("the fence markers should bracket their content, got %v", marks[2:7])
	}
	if row := rows[marks[8]]; !strings.Contains(row, "tail") {
		t.Errorf("the line after the block marks row %d = %q, want the tail", marks[8], row)
	}
}

// TestRenderMarkdownTable: a table renders as its cells over a ─┼─ rule, with none of
// the pipes or dashes that described it reaching the page.
func TestRenderMarkdownTable(t *testing.T) {
	rows := strings.Split(render("| Flag | Does |\n|------|------|\n| -v | noisy |\n| -q | quiet |\n", 40), "\n")
	if len(rows) != 4 {
		t.Fatalf("expected a header, a rule and two body rows, got %d: %q", len(rows), rows)
	}
	for i, want := range []string{"Flag", "─┼─", "-v", "-q"} {
		if !strings.Contains(rows[i], want) {
			t.Errorf("row %d should carry %q, got %q", i, want, rows[i])
		}
	}
	if strings.Contains(rows[0], "|") || strings.Contains(rows[2], "|") {
		t.Errorf("the source's pipes must not reach the page, got %q", rows)
	}
	// The rule is the table's own, not a thematic break: it spans the columns rather than
	// the full width the renderer was given. (It is a cell or two wider than the header
	// row, which stops at its last cell's text rather than padding out to the column.)
	if w := lipgloss.Width(rows[1]); w >= 40 || w < lipgloss.Width(rows[0]) {
		t.Errorf("the rule should span the columns, got %d cells for a 40-cell width", w)
	}
}

// TestRenderMarkdownTableNeedsDelimiter: the delimiter row is the whole signal. Without
// one — or with one whose cell count disagrees — a line of pipes is prose, which is what
// keeps a shell pipeline and a page about markdown intact. A bare "---" under a pipe row
// stays a thematic break, since the table pattern deliberately matches it and tableStart
// is what requires the "|".
func TestRenderMarkdownTableNeedsDelimiter(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"no delimiter", "| a | b |\n| c | d |\n"},
		{"cell counts disagree", "| a | b |\n|---|\n"},
		{"prose above a rule", "| a | b |\n---\n"},
	} {
		got := render(tc.src, 40)
		if !strings.Contains(got, "| a | b |") {
			t.Errorf("%s: the pipe row should stay prose, got %q", tc.name, got)
		}
		if strings.Contains(got, "┼") {
			t.Errorf("%s: no table rule should be drawn, got %q", tc.name, got)
		}
	}
}

// TestRenderMarkdownTableAlignment: the delimiter's colons place each cell in its column.
func TestRenderMarkdownTableAlignment(t *testing.T) {
	rows := strings.Split(render("| left | mid | right |\n|:-----|:---:|------:|\n| a | b | c |\n", 40), "\n")
	if len(rows) != 3 {
		t.Fatalf("expected three rows, got %q", rows)
	}
	// The header cells are four, three and five wide, so a one-cell body entry has a
	// known number of blanks on each side of it.
	for _, tc := range []struct{ cell, want string }{
		{"a", "a    "}, // left: flush, three of its own pad plus the separator's
		{"b", "  b  "}, // center: one blank each side inside a three-wide column
		{"c", "    c"}, // right: flush to the column's end
	} {
		if !strings.Contains(rows[2], tc.want) {
			t.Errorf("cell %q should sit as %q in the row, got %q", tc.cell, tc.want, rows[2])
		}
	}
}

// TestRenderMarkdownTableWidth: a table folds to the width it is given like every other
// block — the invariant the DocScreen and both consumers' page tests rely on. Below the
// width a three-cell column each would need, the rows fall back to the prose they read
// as, which is guaranteed to fit.
func TestRenderMarkdownTableWidth(t *testing.T) {

	body := "| Flag | What it does | Default |\n" +
		"|------|--------------|---------|\n" +
		"| `-v` | noisy logging that runs on for quite a while | off |\n" +
		"| -q | quiet, errors only | on |\n"
	for _, width := range []int{20, 30, 40, 80} {
		for _, line := range strings.Split(RenderMarkdown(body, width), "\n") {
			if w := lipgloss.Width(line); w > width {
				t.Errorf("width %d: line exceeds it (got %d): %q", width, w, ansi.Strip(line))
			}
		}
	}

	// Five columns of real words cannot be laid out in twenty cells: the source comes
	// back as prose, still inside the width.
	const narrow = 20
	wide := "| alpha | bravo | charlie | delta | echo |\n|--|--|--|--|--|\n| 1 | 2 | 3 | 4 | 5 |\n"
	got := RenderMarkdown(wide, narrow)
	if strings.Contains(ansi.Strip(got), "┼") {
		t.Errorf("an unlayable table should fall back to prose, got:\n%s", ansi.Strip(got))
	}
	if !strings.Contains(ansi.Strip(got), "alpha") {
		t.Errorf("the fallback must still show the source, got:\n%s", ansi.Strip(got))
	}
	for _, line := range strings.Split(got, "\n") {
		if w := lipgloss.Width(line); w > narrow {
			t.Errorf("the fallback exceeds the width (got %d): %q", w, ansi.Strip(line))
		}
	}
}

// TestRenderMarkdownTableWraps: a cell too wide for its column takes extra rows and the
// other columns pad out beside it, so the separators stay in one column all the way down.
// That is the invariant a naive pad breaks, and it is what makes a wrapped row still read
// as one row of the table.
func TestRenderMarkdownTableWraps(t *testing.T) {
	rows := strings.Split(render("| A | B |\n|---|---|\n| one | a phrase that has to wrap |\n", 24), "\n")
	if len(rows) < 4 {
		t.Fatalf("expected the long cell to wrap over extra rows, got %q", rows)
	}
	want := strings.Index(rows[0], "│")
	if want < 0 {
		t.Fatalf("no separator in the header row %q", rows[0])
	}
	for i, row := range rows {
		if i == 1 {
			continue // the rule carries ┼ at that column, not │
		}
		if got := strings.Index(row, "│"); got != want {
			t.Errorf("row %d puts its separator at column %d, want %d: %q", i, got, want, row)
		}
	}
	// The wrapped row's continuation has nothing in its first column but still holds it.
	if !strings.HasPrefix(rows[3], strings.Repeat(" ", want)) {
		t.Errorf("the continuation row should pad its empty first column, got %q", rows[3])
	}
}

// TestRenderMarkdownTableCodeSpanWidth pins that a column is sized on what is PRINTED,
// not on the source: inline renders `q` as a chip two cells wider than the word, so a
// naive len(cell) sizes the column short and the chip pushes the row past its width.
func TestRenderMarkdownTableCodeSpanWidth(t *testing.T) {

	const width = 40
	got := RenderMarkdown("| A | B |\n|---|---|\n| `q` | x |\n", width)
	if !strings.Contains(got, codeChip("q")) {
		t.Errorf("the cell should render its code span as a chip, got %q", got)
	}
	rows := strings.Split(got, "\n")
	if len(rows) != 3 {
		t.Fatalf("expected three rows, got %q", rows)
	}
	// The chip is three cells, so the column is three wide and every row matches it.
	for i, row := range rows {
		if w := lipgloss.Width(row); w != lipgloss.Width(rows[0]) {
			t.Errorf("row %d is %d cells, want the header's %d: %q",
				i, w, lipgloss.Width(rows[0]), ansi.Strip(row))
		}
	}
}

// TestRenderMarkdownTableNoColorBleed is the regression behind wrapCell's terminator.
// ansi.Wrap does not emit self-contained rows, so a code span broken across a wrap left
// its background OPEN at end of row — tinting the padding, the separator and the whole of
// the next column. Every cell must end its own color.
func TestRenderMarkdownTableNoColorBleed(t *testing.T) {

	const width = 26
	got := RenderMarkdown("| A | B |\n|---|---|\n| `a long tinted span` | plain |\n", width)
	rows := strings.Split(got, "\n")
	if len(rows) < 4 {
		t.Fatalf("expected the chip to wrap, got %q", rows)
	}
	sep := ruleStyle().Render(tableSep)
	for i, row := range rows[2:] {
		// Whatever a cell opened is closed before the separator, so the separator survives
		// intact and what follows it starts from the terminal's own colors.
		if !strings.Contains(row, sep) && !strings.Contains(row, ruleStyle().Render(strings.TrimRight(tableSep, " "))) {
			t.Errorf("body row %d lost its separator to a bleed: %q", i, row)
		}
		if _, after, ok := strings.Cut(row, sep); ok && strings.HasPrefix(after, "\x1b") {
			t.Errorf("body row %d opens an escape right after the separator: %q", i, row)
		}
	}
	if !strings.Contains(rows[2], "plain") {
		t.Errorf("the plain cell should render as bare text, got %q", rows[2])
	}
}

// TestRenderMarkdownTableChrome: the separators and the rule are chrome in the border
// color, the header carries the accent, and body cells carry neither.
func TestRenderMarkdownTableChrome(t *testing.T) {

	got := RenderMarkdown("| Head |\n|------|\n| body |\n", 40)
	if !strings.Contains(got, tableHeadStyle().Render("Head")) {
		t.Errorf("the header cell should carry the heading accent, got %q", got)
	}
	if !strings.Contains(got, ruleStyle().Render(strings.Repeat("─", 4))) {
		t.Errorf("the rule should be drawn in the border color, got %q", got)
	}
	if !strings.Contains(got, "body") || strings.Contains(got, tableHeadStyle().Render("body")) {
		t.Errorf("a body cell should be unstyled, got %q", got)
	}

	// Two columns: the separator is chrome too.
	got = RenderMarkdown("| a | b |\n|---|---|\n| c | d |\n", 40)
	if !strings.Contains(got, ruleStyle().Render(tableSep)) {
		t.Errorf("the column separator should be drawn in the border color, got %q", got)
	}
}

// TestRenderMarkdownTableRagged: rows are squared off to the header — a short one is
// padded out and a long one's extra cells are dropped, since there is no column to hold
// them. A "\|" splits like any other pipe: escapes are not honored anywhere in this
// reader, so a cell cannot contain one.
func TestRenderMarkdownTableRagged(t *testing.T) {
	rows := strings.Split(render("| a | b | c |\n|---|---|---|\n| 1 |\n| 1 | 2 | 3 | 4 |\n", 40), "\n")
	if len(rows) != 4 {
		t.Fatalf("expected four rows, got %q", rows)
	}
	if strings.Contains(rows[3], "4") {
		t.Errorf("the fourth cell has no column and should be dropped, got %q", rows[3])
	}
	if !strings.HasPrefix(rows[2], "1 │") {
		t.Errorf("a short row should pad out to the header's columns, got %q", rows[2])
	}
	if cells := tableCells(`a \| b | c`); len(cells) != 3 {
		t.Errorf(`"\|" should split like any pipe, got %q`, cells)
	}
}

// TestRenderMarkdownTableMapped: a table is one pending block, so flush's proportional
// spread maps its source rows onto its output rows — and since header + delimiter + rows
// comes to the same count as header + rule + rows, each row lands on its own.
//
// The half-typed cases are what gote's preview renders on every keystroke: they must not
// panic, whatever state the author has left the table in.
func TestRenderMarkdownTableMapped(t *testing.T) {
	body := "intro\n\n| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |\n\ntail\n"
	got, marks := RenderMarkdownMapped(body, 40)
	rows := strings.Split(ansi.Strip(got), "\n")
	for i, m := range marks {
		if i > 0 && m < marks[i-1] {
			t.Fatalf("mark %d (%d) goes backwards from %d", i, m, marks[i-1])
		}
		// One past the last row is the trailing blank's backfill, as elsewhere.
		if m > len(rows) {
			t.Fatalf("mark %d is row %d, past the render's %d rows", i, m, len(rows))
		}
	}
	// Source line 2 is the header, 4 and 5 the body rows.
	for i, want := range map[int]string{2: "a", 4: "1", 5: "3"} {
		if row := rows[marks[i]]; !strings.HasPrefix(row, want) {
			t.Errorf("table line %d marks row %d = %q, want it to open with %q", i, marks[i], row, want)
		}
	}

	for _, src := range []string{"| a | b |\n|--", "| a |\n", "|\n|-|\n", "| a | b |\n|---|---|\n"} {
		if _, m := RenderMarkdownMapped(src, 40); len(m) != len(strings.Split(src, "\n")) {
			t.Errorf("%q: got %d marks for %d source lines", src, len(m), len(strings.Split(src, "\n")))
		}
	}
}

// TestRenderMarkdownWrapsStyledSpansToWidth is the bug lipgloss v1 hid: ansi.Wrap keeps
// the space before a break when the next token opens with a style sequence, so a
// paragraph whose wrap point lands just before an inline code span rendered one cell over
// the width it was given — clipped at the pane edge with nowhere to scroll to it. Under
// v1's TTY-less Ascii profile the spans carried no escapes, so the tests never saw it
// while every real terminal did.
func TestRenderMarkdownWrapsStyledSpansToWidth(t *testing.T) {
	const width = 40
	src := "Run `gdaddon` in your project (or `gdaddon /path/to/project`). It finds the " +
		"project root from git, then looks for an `addon_manifest.yml` underneath it."

	styled := false
	for i, row := range strings.Split(RenderMarkdown(src, width), "\n") {
		if w := ansi.StringWidth(row); w > width {
			t.Errorf("row %d is %d cells wide, want at most %d: %q", i+1, w, width, row)
		}
		if strings.Contains(row, "\x1b") {
			styled = true
		}
	}
	if !styled {
		t.Fatal("no row carried a style: the test cannot see the bug it exists for")
	}
}
