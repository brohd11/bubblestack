package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/lipgloss"
)

// ---------- helpers ----------

// mdRow parses doc through a fresh markdown highlighter and answers one row's
// spans — the two-call shape (Parse the document, ask by row) every highlighter
// test repeats.
func mdRow(doc string, row int) []Span {
	h := NewMarkdownHighlighter()
	h.Parse(doc)
	return h.HighlightLine(row)
}

// styleEq compares two styles by what they actually paint. lipgloss.Style holds a
// func field (transform), so == does not compile and DeepEqual would compare
// renderer pointers; rendering the same probe through both is the honest witness.
// It only means anything under withColor — with the Ascii profile every style
// renders to bare text.
func styleEq(a, b lipgloss.Style) bool {
	return a.Render("Xy") == b.Render("Xy")
}

// wantSpan is one expected (text, style) pair. Tests spell a row out in FULL, so
// the Span contract — the spans concatenated reconstruct the line — is asserted
// by construction rather than as an afterthought.
type wantSpan struct {
	text  string
	style lipgloss.Style
}

func assertSpans(t *testing.T, doc string, row int, want ...wantSpan) {
	t.Helper()
	got := mdRow(doc, row)
	if len(got) != len(want) {
		t.Fatalf("%q row %d: got %d spans %v, want %d", doc, row, len(got), spanTexts(got), len(want))
	}
	for i := range want {
		if got[i].Text != want[i].text {
			t.Errorf("%q row %d span %d: got text %q, want %q", doc, row, i, got[i].Text, want[i].text)
		}
		if !styleEq(got[i].Style, want[i].style) {
			t.Errorf("%q row %d span %d (%q): style mismatch", doc, row, i, got[i].Text)
		}
	}
}

func spanTexts(spans []Span) []string {
	out := make([]string, len(spans))
	for i, sp := range spans {
		out[i] = sp.Text
	}
	return out
}

// mdNone is the unstyled run's style — the filler between highlighted spans.
var mdNone = mdStyles[mdStyleNone]

// ---------- markdown highlighter ----------

// TestMarkdownInlineConstructs: each construct the palette covers produces its own
// span, delimiters excluded (they stay in the surrounding unstyled run) — that is
// what childRange's "first to last descendant Text segment" buys.
func TestMarkdownInlineConstructs(t *testing.T) {
	withColor(t) // styleEq compares rendered output; Ascii would flatten every style

	// A heading is a BLOCK: the style paints the whole row, "# " marker included.
	assertSpans(t, "# Heading\n", 0, wantSpan{"# Heading", mdHeadingStyle})

	assertSpans(t, "a *em* b\n", 0,
		wantSpan{"a *", mdNone}, wantSpan{"em", mdEmphasisStyle}, wantSpan{"* b", mdNone})
	assertSpans(t, "a **strong** b\n", 0,
		wantSpan{"a **", mdNone}, wantSpan{"strong", mdStrongStyle}, wantSpan{"** b", mdNone})
	assertSpans(t, "a `code` b\n", 0,
		wantSpan{"a `", mdNone}, wantSpan{"code", mdCodeStyle}, wantSpan{"` b", mdNone})
	// A link styles its visible text, not the (url) target.
	assertSpans(t, "a [text](http://x) b\n", 0,
		wantSpan{"a [", mdNone}, wantSpan{"text", mdLinkStyle}, wantSpan{"](http://x) b", mdNone})
	// An autolink has no separate text node: the span comes off Pos()+1.
	assertSpans(t, "a <https://x.com> b\n", 0,
		wantSpan{"a <", mdNone}, wantSpan{"https://x.com", mdLinkStyle}, wantSpan{"> b", mdNone})

	// A paragraph with nothing to style answers nil, not a full-line unstyled span.
	if got := mdRow("plain paragraph\n", 0); got != nil {
		t.Errorf("an unstyled line should answer nil, got %v", spanTexts(got))
	}
}

// TestMarkdownInlineBeatsBlock: mdInterval.prio — an inline construct paints OVER
// the block style on the cells it covers, so `code` inside a heading is code-colored
// while the rest of the row stays heading-styled.
func TestMarkdownInlineBeatsBlock(t *testing.T) {
	withColor(t)

	assertSpans(t, "# A `b` c\n", 0,
		wantSpan{"# A `", mdHeadingStyle}, wantSpan{"b", mdCodeStyle}, wantSpan{"` c", mdHeadingStyle})
}

// TestMarkdownCodeBlocks is the multi-line-state case the AST exists to solve: a
// fenced block styles its opening fence, every interior line and its closing fence,
// an unclosed fence runs to EOF, and an indented block styles its rows — none of it
// tracked by hand across lines.
func TestMarkdownCodeBlocks(t *testing.T) {
	withColor(t)

	closed := "before\n```go\nfmt.Println()\nx\n```\nafter\n"
	for _, row := range []int{1, 2, 3, 4} { // fence, body, body, closing fence
		assertSpans(t, closed, row, wantSpan{strings.Split(closed, "\n")[row], mdCodeStyle})
	}
	for _, row := range []int{0, 5} { // the prose either side is untouched
		if got := mdRow(closed, row); got != nil {
			t.Errorf("row %d outside the fence should be unstyled, got %v", row, spanTexts(got))
		}
	}

	unclosed := "before\n```\nunclosed\nmore\n"
	for _, row := range []int{1, 2, 3} {
		assertSpans(t, unclosed, row, wantSpan{strings.Split(unclosed, "\n")[row], mdCodeStyle})
	}

	indented := "para\n\n    indented\n    code\n\nafter\n"
	for _, row := range []int{2, 3} {
		assertSpans(t, indented, row, wantSpan{strings.Split(indented, "\n")[row], mdCodeStyle})
	}
}

// TestMarkdownBlockquote: a blockquote's own Lines() is empty, so its range comes
// from walking to the deepest descendant BLOCK. Inline children are skipped on that
// walk — ast.BaseInline panics on Lines() instead of answering empty — which is why
// this covers a quote carrying inline markup and a nested quote.
func TestMarkdownBlockquote(t *testing.T) {
	withColor(t)

	doc := "> quote *em*\n> second\n>\n> > nested\n\nafter\n"
	for _, row := range []int{0, 1, 2, 3} {
		if got := mdRow(doc, row); len(got) == 0 {
			t.Fatalf("row %d of the quote should be styled", row)
		}
	}
	// The em inside the quote still wins its own cells.
	assertSpans(t, doc, 0,
		wantSpan{"> quote *", mdQuoteStyle}, wantSpan{"em", mdEmphasisStyle}, wantSpan{"*", mdQuoteStyle})
	if got := mdRow(doc, 5); got != nil {
		t.Errorf("the prose after the quote should be unstyled, got %v", spanTexts(got))
	}

	// A fenced block inside a quote: the code block is walked after its container,
	// so it paints over the quote style.
	fenced := "> ```\n> code\n> ```\n"
	assertSpans(t, fenced, 1, wantSpan{"> code", mdCodeStyle})
}

// TestMarkdownSetextUnderline documents a known gap rather than an intent: a setext
// heading's Lines() cover the text row only, so the "=====" underline renders plain.
func TestMarkdownSetextUnderline(t *testing.T) {
	withColor(t)

	assertSpans(t, "Title\n=====\n\nbody\n", 0, wantSpan{"Title", mdHeadingStyle})
	if got := mdRow("Title\n=====\n\nbody\n", 1); got != nil {
		t.Errorf("the setext underline is not styled today, got %v", spanTexts(got))
	}
}

// TestMarkdownSpanInvariant is the contract the editor validates against before it
// styles anything: a row's spans, concatenated, must equal the row's text exactly.
// Break it and the highlighter silently loses its colors (see hlSpans), so it is
// checked over a document mixing every construct, tabs and multi-byte runes.
func TestMarkdownSpanInvariant(t *testing.T) {
	doc := strings.Join([]string{
		"# Title `code` here",
		"",
		"a *em* and **strong** and [link](x) and <https://y.z>",
		"a\tb `c\td` e",
		"ünïcödé *émphasis* 日本語",
		"> quote *em*",
		"> more",
		"",
		"```go",
		"x := 1 // `not a code span`",
		"```",
		"",
		"    indented code",
		"",
		"plain tail",
	}, "\n")

	h := NewMarkdownHighlighter()
	h.Parse(doc)
	for row, line := range strings.Split(doc, "\n") {
		spans := h.HighlightLine(row)
		if spans == nil {
			continue // an unstyled row: the editor renders it plain
		}
		if got := spansText(spans); got != line {
			t.Errorf("row %d: spans concatenate to %q, want %q", row, got, line)
		}
	}

	// Rows off either end are safe — the editor asks by row and clamping is the
	// highlighter's job, not the caller's.
	if h.HighlightLine(-1) != nil || h.HighlightLine(9999) != nil {
		t.Error("out-of-range rows must answer nil")
	}
}

// ---------- registry ----------

// TestHighlighterRegistry: registration is keyed by lowercase extension WITH the dot,
// each lookup mints a fresh instance (screens must never share parser state), and an
// unregistered extension answers nil — the plain-render case.
func TestHighlighterRegistry(t *testing.T) {
	RegisterHighlighter(".HLTEST", func() Highlighter { return &countingHL{} }) // keys lowercase on the way in

	a, b := lookupHighlighter(".hltest"), lookupHighlighter(".hltest")
	if a == nil || b == nil {
		t.Fatal("a registered extension should resolve")
	}
	if a == b {
		t.Error("each lookup must mint a fresh highlighter, not share one")
	}
	if lookupHighlighter(".nobody-registered-this") != nil {
		t.Error("an unregistered extension must answer nil")
	}
	if lookupHighlighter(".md") == nil {
		t.Error("markdown should self-register from init()")
	}
}

// ---------- editor wiring ----------

// TestEditorHighlighterAutoPick: left nil, EditorOpts resolves a highlighter from the
// path's extension (case-insensitively, since the constructor lowercases); an
// unregistered extension or a path-less buffer stays plain; an explicit highlighter
// wins over the registry either way.
func TestEditorHighlighterAutoPick(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"notes.md", true},
		{"NOTES.MD", true},
		{"readme.markdown", true},
		{"notes.txt", false},
		{"", false},
		{"noext", false},
	} {
		s := NewEditorScreen(EditorOpts{Path: tc.path})
		if (s.hl != nil) != tc.want {
			t.Errorf("Path %q: highlighter present = %v, want %v", tc.path, s.hl != nil, tc.want)
		}
	}

	explicit := &countingHL{}
	if s := NewEditorScreen(EditorOpts{Path: "notes.md", Highlighter: explicit}); s.hl != explicit {
		t.Error("an explicit Highlighter must win over the registry")
	}
	if s := NewEditorScreen(EditorOpts{Path: "notes.txt", Highlighter: explicit}); s.hl != explicit {
		t.Error("an explicit Highlighter must apply to an unregistered extension too")
	}
}

// TestEditorStyledRender: highlighting is render-only. The styled row carries ANSI a
// plain one does not, but measures exactly the same and still expands tabs — the two
// invariants body()'s padding and the frame depend on.
func TestEditorStyledRender(t *testing.T) {
	withColor(t)

	const doc = "# Title\n\nbody `code` here\n\na\tb `c\td` e\n"
	lit, _ := newEditor(EditorOpts{Path: "x.md"})
	lit.setContent(doc)
	plain, _ := newEditor(EditorOpts{Path: "x.txt"})
	plain.setContent(doc)
	if lit.hl == nil || plain.hl != nil {
		t.Fatal("expected exactly the .md screen to be highlighted")
	}

	lit.curY, plain.curY = 1, 1 // park the caret off row 0 so no cell is spliced
	if got := lit.renderLine(0); !strings.Contains(got, mdHeadingStyle.Render("# Title")) {
		t.Errorf("the heading row should render through the heading style, got %q", got)
	}
	if lit.renderLine(2) == plain.renderLine(2) {
		t.Error("a row with an inline code span should not render identically to plain")
	}

	for row := range lit.lines {
		lit.curY, lit.curX = row, 0
		plain.curY, plain.curX = row, 0
		got, want := lit.renderLine(row), plain.renderLine(row)
		if lipgloss.Width(got) != lipgloss.Width(want) {
			t.Errorf("row %d: styled width %d != plain width %d", row, lipgloss.Width(got), lipgloss.Width(want))
		}
		if strings.Contains(got, "\t") {
			t.Errorf("row %d: the styled render must never emit a raw tab", row)
		}
	}
	if strings.Contains(lit.View(core.NewShared(nil)), "\t") {
		t.Error("View must never emit a raw tab, highlighted or not")
	}
}

// TestEditorStyledCursor: the cursor wins over the syntax style exactly as it wins
// over plain text — mid-line, at end of line (the appended blank) and sitting on a
// tab inside a styled run.
func TestEditorStyledCursor(t *testing.T) {
	withColor(t)

	s, _ := newEditor(EditorOpts{Path: "x.md"})
	s.setContent("# Title\na\tb `c\td` e")

	// Mid-line, inside the heading's block style.
	s.curY, s.curX = 0, 2
	if got := s.renderLine(0); !strings.Contains(got, editorCursorStyle.Render("T")) {
		t.Errorf("the cursor cell should be reverse-video on a highlighted row, got %q", got)
	}
	// End of line: the blank is appended in the cursor style.
	s.curX = len(s.lines[0])
	if got := s.renderLine(0); !strings.HasSuffix(got, editorCursorStyle.Render(" ")) {
		t.Errorf("an end-of-line cursor should append a styled blank, got %q", got)
	}
	// On the tab at column 1: the expansion's FIRST cell reverses, and the row keeps
	// the plain row's width.
	s.curY, s.curX = 1, 1
	got := s.renderLine(1)
	if !strings.Contains(got, editorCursorStyle.Render(" ")) {
		t.Errorf("a cursor on a tab should reverse the expansion's first cell, got %q", got)
	}
	plain, _ := newEditor(EditorOpts{Path: "x.txt"})
	plain.setContent("# Title\na\tb `c\td` e")
	plain.curY, plain.curX = 1, 1
	if lipgloss.Width(got) != lipgloss.Width(plain.renderLine(1)) {
		t.Error("a cursor on a tab must measure the same styled as plain")
	}
}

// TestEditorHighlighterMismatchFallback: a highlighter whose spans do not reconstruct
// the line is ignored outright. The validation is what lets the editor accept an
// arbitrary third-party Highlighter without letting a buggy one corrupt the frame.
func TestEditorHighlighterMismatchFallback(t *testing.T) {
	withColor(t)

	s, _ := newEditor(EditorOpts{Path: "x.md", Highlighter: brokenHL{}})
	s.setContent("hello world")
	plain, _ := newEditor(EditorOpts{Path: "x.txt"})
	plain.setContent("hello world")

	for _, cur := range []int{0, 5, len("hello world")} {
		s.curX, plain.curX = cur, cur
		if got, want := s.renderLine(0), plain.renderLine(0); got != want {
			t.Errorf("cursor %d: broken spans should render plain, got %q want %q", cur, got, want)
		}
	}
}

// TestEditorHighlightReparse: the reparse cache. Parse runs once per EDIT — not per
// frame and not per rendered row — and every buffer mutation invalidates it, so what
// the screen shows can never lag the buffer.
func TestEditorHighlightReparse(t *testing.T) {
	withColor(t)

	c := &countingHL{}
	s, sh := newEditor(EditorOpts{Path: "x.md", Highlighter: c})
	s.setContent("alpha\nbeta\ngamma")

	s.View(sh)
	if c.parses != 1 {
		t.Fatalf("a full frame should parse once, got %d", c.parses)
	}
	s.View(sh)
	s.View(sh)
	if c.parses != 1 {
		t.Errorf("re-rendering an unchanged buffer must not reparse, got %d parses", c.parses)
	}

	typeRunes(s, 'x')
	s.View(sh)
	if c.parses != 2 {
		t.Errorf("an edit should force exactly one reparse, got %d parses", c.parses)
	}
	if c.last != buffer(s) {
		t.Errorf("Parse got %q, want the live buffer %q", c.last, buffer(s))
	}

	// The reparse is observable: adding a marker rune restyles the row. The caret
	// parks on row 1 for both reads so no spliced cursor cell muddies them.
	md, _ := newEditor(EditorOpts{Path: "x.md"})
	md.setContent("Title\ntail")
	md.curY = 1
	if got := md.renderLine(0); got != "Title" {
		t.Fatalf("a bare paragraph should render plain, got %q", got)
	}
	md.curY, md.curX = 0, 0
	typeRunes(md, '#', ' ')
	md.curY, md.curX = 1, 0
	if got := md.renderLine(0); !strings.Contains(got, mdHeadingStyle.Render("# Title")) {
		t.Errorf("typing the heading marker should restyle the row, got %q", got)
	}
}

// TestEditorUnfocusedNoHighlight: an unfocused pane mutes its whole body — the
// !focused branch returns before the highlighter, so no syntax color leaks into a
// pane the keys don't reach.
func TestEditorUnfocusedNoHighlight(t *testing.T) {
	withColor(t)

	s, sh := newPaneEditor(EditorOpts{Path: "notes.md"})
	s.setContent("# Title\nbody")
	s.SetFocused(false)

	dark := s.View(sh)
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	if !strings.Contains(dark, muted.Render("# Title")) {
		t.Error("an unfocused pane should mute its heading row like any other text")
	}
	if strings.Contains(dark, mdHeadingStyle.Render("# Title")) {
		t.Error("syntax styling must not survive losing focus")
	}
}

// ---------- test doubles ----------

// countingHL records how often Parse ran and what it last saw, so the editor's
// reparse cache can be asserted directly. It styles every non-empty line whole.
type countingHL struct {
	parses int
	last   string
	lines  []string
}

func (c *countingHL) Parse(doc string) {
	c.parses++
	c.last = doc
	c.lines = strings.Split(doc, "\n")
}

func (c *countingHL) HighlightLine(row int) []Span {
	if row < 0 || row >= len(c.lines) || c.lines[row] == "" {
		return nil
	}
	return []Span{{Text: c.lines[row], Style: mdHeadingStyle}}
}

// brokenHL violates the Span contract: its spans never reconstruct the line.
type brokenHL struct{}

func (brokenHL) Parse(string) {}

func (brokenHL) HighlightLine(int) []Span {
	return []Span{{Text: "nope", Style: mdHeadingStyle}}
}
