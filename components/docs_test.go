package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

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
		codeSpanStyle().Render("d"),
		linkStyle().Render("e"),
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing inline render %q in %q", want, got)
		}
	}
	if plain := ansi.Strip(got); plain != "a b and c and d and e end" {
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
	if got := render(line, 80); got != "bold, em, code and a link in it." {
		t.Errorf("earlier spans leaked into the link match: %q", got)
	}

	// Markup inside a code span is literal, and the span keeps the code style.
	got := RenderMarkdown("`**not bold**` here", 80)
	if !strings.Contains(got, codeSpanStyle().Render("**not bold**")) {
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
