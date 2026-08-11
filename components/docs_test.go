package components

import (
	"slices"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/lipgloss"
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
	withColor(t) // the styles are ANSI; Ascii would make every level identical

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

// TestRenderMarkdownInline: the four inline constructs render styled with their
// delimiters dropped.
func TestRenderMarkdownInline(t *testing.T) {
	withColor(t)

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
	withColor(t)

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
// source rather than swallowed — the user can still see it is there.
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
	withColor(t)

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
	withColor(t)

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
	withColor(t)

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
	withColor(t)
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
	withColor(t)

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
	withColor(t)

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
