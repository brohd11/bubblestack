package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"
)

// headingHL is a minimal LineRenderer: "# " prefixes are hidden on rendered rows, "---"
// fills the width it is given, "- " items keep a bullet glyph while raw, and every other
// line renders as its source.
type headingHL struct {
	countingHL
	widths []int // every LiveContext.Width RenderLine was asked for
}

var testGlyphStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))

func (h *headingHL) RenderLine(row int, ctx LiveContext) []Span {
	h.widths = append(h.widths, ctx.Width)
	if row < 0 || row >= len(h.lines) {
		return nil
	}
	if h.lines[row] == "---" {
		return []Span{{Text: strings.Repeat("=", ctx.Width)}}
	}
	if !strings.HasPrefix(h.lines[row], "# ") {
		return nil
	}
	return []Span{{Text: strings.TrimPrefix(h.lines[row], "# "), Style: &testHighlightStyle}}
}

var testSourceStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

// SourceSpans colors "code" rows distinctly: the live-only source highlighting.
func (h *headingHL) SourceSpans(row int) []Span {
	if row < 0 || row >= len(h.lines) || !strings.HasPrefix(h.lines[row], "code") {
		return nil
	}
	return []Span{{Text: h.lines[row], Style: &testSourceStyle}}
}

func (h *headingHL) ActiveGlyphs(row int) []Glyph {
	if row < 0 || row >= len(h.lines) || !strings.HasPrefix(h.lines[row], "- ") {
		return nil
	}
	return []Glyph{{Col: 0, From: '-', Text: '•', Style: &testGlyphStyle}}
}

func (h *headingHL) SourceCol(row, cell int) int {
	if row < len(h.lines) && strings.HasPrefix(h.lines[row], "# ") {
		return cell + 2
	}
	return cell
}

// liveEditor builds a live editor over content and draws a frame, which runs the
// explicit highlighter's first parse.
func liveEditor(t *testing.T, content string, opts Opts) (*Screen, *core.Shared) {
	t.Helper()
	opts.Highlighter = &headingHL{}
	s, sh := newEditor(opts)
	s.setContent(content)
	s.SetLiveRender(true)
	_ = s.View(sh)
	return s, sh
}

func plainRow(s *Screen, row int) string {
	return strings.TrimRight(ansi.Strip(s.renderRow(row)), " ")
}

func TestLiveRenderSwapsInactiveRows(t *testing.T) {
	s, _ := liveEditor(t, "# One\n# Two\nplain", Opts{})
	if got := plainRow(s, 0); got != "# One" {
		t.Fatalf("caret row = %q, want the source", got)
	}
	if got := plainRow(s, 1); got != "Two" {
		t.Fatalf("inactive row = %q, want the rendered form", got)
	}
	s.curY = 1
	if got := plainRow(s, 0); got != "One" {
		t.Fatalf("row the caret left = %q, want it rendered", got)
	}
	if got := plainRow(s, 1); got != "# Two" {
		t.Fatalf("row the caret entered = %q, want the source", got)
	}
	s.SetLiveRender(false)
	if got := plainRow(s, 0); got != "# One" {
		t.Fatalf("live off = %q, want the source", got)
	}
}

func TestLiveRenderSelectionAndSearchKeepRowsRaw(t *testing.T) {
	s, _ := liveEditor(t, "x\n# A\n# B\n# C", Opts{Search: true})
	selectRange(s, 1, 0, 2, 1)
	if plainRow(s, 1) != "# A" || plainRow(s, 2) != "# B" || plainRow(s, 3) != "C" {
		t.Fatalf("selected rows must be raw, others rendered: %q %q %q",
			plainRow(s, 1), plainRow(s, 2), plainRow(s, 3))
	}
	s.clearSelection()
	s.searchQuery = "c"
	if plainRow(s, 3) != "# C" || plainRow(s, 1) != "A" {
		t.Fatalf("a search hit must keep its row raw: %q %q", plainRow(s, 3), plainRow(s, 1))
	}
}

func TestLiveRenderWrapsRenderedWidth(t *testing.T) {
	s, sh := liveEditor(t, "x\n# abcdefgh", Opts{Wrap: true})
	s.SetSize(sh, 8, 20) // body width 8: the source wraps, the rendered form fits
	if n := s.wrapTotalRows(); n != 2 {
		t.Fatalf("rows = %d, want 2 (the rendered heading fits one row)", n)
	}
	if r := s.wrapRows[1]; !r.rendered || r.line != 1 {
		t.Fatalf("row 1 = %+v, want the rendered heading", r)
	}
	s.curY = 1
	if n := s.wrapTotalRows(); n != 4 {
		t.Fatalf("rows = %d after the caret entered, want 4 (the source breaks after the marker)", n)
	}
}

func TestLiveRenderClickMapsThroughRenderer(t *testing.T) {
	for _, wrap := range []bool{false, true} {
		s, sh := liveEditor(t, "x\n# Heading", Opts{Wrap: wrap})
		s.clickAt(sh, s.insetX()+s.leftGutterWidth()+3, s.insetY()+1)
		if s.curY != 1 || s.curX != 5 {
			t.Fatalf("wrap=%v: click at rendered cell 3 = (%d,%d), want (1,5)", wrap, s.curY, s.curX)
		}
	}
}

func TestLiveRenderVerticalMoveMapsThroughRenderer(t *testing.T) {
	s, _ := liveEditor(t, "abcd\n# Heading", Opts{Wrap: true})
	s.curX, s.wantX = 3, 3
	s.moveVertical(1)
	if s.curY != 1 || s.curX != 5 {
		t.Fatalf("down into a rendered row = (%d,%d), want (1,5)", s.curY, s.curX)
	}
}

func TestLiveRenderKeepsTopLineAcrossFlips(t *testing.T) {
	var b strings.Builder
	b.WriteString("# " + strings.Repeat("a", 10)) // wraps raw at width 8, fits rendered
	for i := 0; i < 30; i++ {
		b.WriteString("\nline")
	}
	s, sh := liveEditor(t, b.String(), Opts{Wrap: true, Search: true})
	s.SetSize(sh, 8, 5)
	s.curY = 20
	s.SetTopLine(10)
	if s.TopLine() != 10 {
		t.Fatalf("top line = %d, want 10", s.TopLine())
	}
	s.searchQuery = "aaaa" // a hit above the viewport flips row 0 raw, adding a row
	if top := s.TopLine(); top != 10 {
		t.Fatalf("top line = %d after a flip above the viewport, want 10", top)
	}
}

func TestLiveRenderEditedRowStaysRawUntilReparse(t *testing.T) {
	s, _ := liveEditor(t, "# One\n# Two", Opts{})
	s.curY, s.curX = 1, len(s.lines[1])
	typeRunes(s, '!')
	s.curY = 0
	s.hlChanged = time.Now()
	if got := plainRow(s, 1); got != "# Two!" {
		t.Fatalf("edited row before reparse = %q, want its source", got)
	}
	s.parseHighlight()
	if got := plainRow(s, 1); got != "Two!" {
		t.Fatalf("edited row after reparse = %q, want it rendered", got)
	}
}

func TestLiveRenderPassesRowWidth(t *testing.T) {
	s, sh := liveEditor(t, "x\n---", Opts{Wrap: true})
	s.SetSize(sh, 12, 20)
	if n := s.wrapTotalRows(); n != 2 {
		t.Fatalf("rows = %d, want 2 (the rule fills exactly one row)", n)
	}
	if got, want := plainRow(s, 1), strings.Repeat("=", s.contentW()); got != want {
		t.Fatalf("wrapped rule = %q, want %q", got, want)
	}
	s.ToggleWrap()
	if got, want := plainRow(s, 1), strings.Repeat("=", s.contentW()); got != want {
		t.Fatalf("unwrapped rule = %q, want %q", got, want)
	}
}

func TestLiveRenderGlyphsOnRawRows(t *testing.T) {
	s, _ := liveEditor(t, "- item\nx", Opts{})
	s.curY, s.curX = 0, 4
	if got := plainRow(s, 0); !strings.HasPrefix(got, "• item") {
		t.Fatalf("caret away from the marker: %q, want the bullet", got)
	}
	for _, x := range []int{0, 1} {
		s.curX = x
		if got := ansi.Strip(s.renderRow(0)); strings.Contains(got, "•") {
			t.Fatalf("caret at %d touches the marker: %q, want the source", x, got)
		}
	}
	s.curX = 4
	selectRange(s, 0, 0, 0, 3)
	if got := plainRow(s, 0); strings.Contains(got, "•") {
		t.Fatalf("a selection over the marker must show it: %q", got)
	}
	s.clearSelection()
	s.lines[0][0] = '*' // the parse no longer matches the text
	if got := plainRow(s, 0); strings.Contains(got, "•") {
		t.Fatalf("a glyph whose From no longer matches must drop: %q", got)
	}
}

func TestLiveRenderGlyphsSurviveTypingOnTheRow(t *testing.T) {
	var created []*headingHL
	s, sh := newEditor(Opts{ResolveLanguage: func(string) *LanguageConfig {
		return &LanguageConfig{NewHighlighter: func() Highlighter {
			h := &headingHL{}
			created = append(created, h)
			return h
		}}
	}, Path: "x.md"})
	s.setContent("- item\nx")
	s.acceptHighlight(s.hlFactory(), s.editSeq)
	s.SetLiveRender(true)
	s.curY, s.curX = 0, len(s.lines[0])
	s.Update(sh, keyMsg("s"))
	if s.hlRows[0] >= 0 {
		t.Fatal("the typed row should have lost its snapshot row")
	}
	if got := plainRow(s, 0); !strings.HasPrefix(got, "• items") {
		t.Fatalf("typed row = %q, want the bullet kept from the preview parse", got)
	}
}

func TestLiveRenderSourceSpansOnlyWhileLive(t *testing.T) {
	s, sh := liveEditor(t, "code one\ncode two\nx", Opts{})
	s.curY = 0
	// The caret row (its first cell is the caret), and a source row with no rendered form.
	for row, want := range []string{testSourceStyle.Render("ode one"), testSourceStyle.Render("code two")} {
		if got := s.renderRow(row); !strings.Contains(got, want) {
			t.Fatalf("live row %d = %q, want the SourceSpans style", row, got)
		}
	}
	s.SetLiveRender(false)
	if got := s.renderRow(1); strings.Contains(got, testSourceStyle.Render("code two")) {
		t.Fatalf("live off, row 1 = %q, want HighlightLine's spans", got)
	}
	s.SetLiveRender(true)

	// A typed row has no snapshot row until the reparse; the preview parse supplies them.
	var ed *Screen
	ed, sh = newEditor(Opts{Path: "x.md", ResolveLanguage: func(string) *LanguageConfig {
		return &LanguageConfig{NewHighlighter: func() Highlighter { return &headingHL{} }}
	}})
	ed.setContent("code\nx")
	ed.acceptHighlight(ed.hlFactory(), ed.editSeq)
	ed.SetLiveRender(true)
	ed.curY, ed.curX = 0, 4
	ed.Update(sh, keyMsg("s"))
	ed.curY = 1 // off the row, so no caret cell splits it
	if got := ed.renderRow(0); !strings.Contains(got, testSourceStyle.Render("codes")) {
		t.Fatalf("typed row = %q, want SourceSpans from the preview parse", got)
	}
}
