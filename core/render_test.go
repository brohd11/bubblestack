package core

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

func TestTruncLeft(t *testing.T) {
	if got := TruncLeft("abc", 5); got != "abc" {
		t.Errorf("short string should pass through, got %q", got)
	}
	// Keeps the right (most informative) end, prefixed with "…".
	if got := TruncLeft("abcdefghij", 5); got != "…ghij" {
		t.Errorf("TruncLeft = %q, want …ghij", got)
	}
	// max < 4 is clamped to 4; "abcd" fits.
	if got := TruncLeft("abcd", 2); got != "abcd" {
		t.Errorf("max clamp: got %q, want abcd", got)
	}
}

func TestHeaderInnerWidthAndConfirmWidth(t *testing.T) {
	if got := HeaderInnerWidth(100); got != 96 {
		t.Errorf("HeaderInnerWidth(100) = %d, want 96", got)
	}
	if got := HeaderInnerWidth(10); got != 20 {
		t.Errorf("HeaderInnerWidth floor = %d, want 20", got)
	}

	sh := NewShared(nil)
	sh.width = 100
	if got := sh.ConfirmWidth(); got != 90 {
		t.Errorf("ConfirmWidth(100) = %d, want 90", got)
	}
	sh.width = 5
	if got := sh.ConfirmWidth(); got != 24 {
		t.Errorf("ConfirmWidth floor = %d, want 24", got)
	}
}

func TestHardWrap(t *testing.T) {
	in := "aaaaaaaaaabbbbbbbbbb" // 20 runes
	out := HardWrap(in, 8)
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 8 {
			t.Errorf("line exceeds width: %q", line)
		}
	}
	if strings.ReplaceAll(out, "\n", "") != in {
		t.Errorf("HardWrap should preserve content, got %q", out)
	}
	// width < 8 is clamped to 8.
	if got := HardWrap("abcdefghij", 4); !strings.Contains(got, "\n") || len([]rune(strings.SplitN(got, "\n", 2)[0])) != 8 {
		t.Errorf("width clamp: got %q", got)
	}
	// No wrap when the string fits.
	if got := HardWrap("short", 8); got != "short" {
		t.Errorf("no-wrap case: got %q", got)
	}
}

func TestBlanks(t *testing.T) {
	if got := Blanks(0); got != "" {
		t.Errorf("Blanks(0) = %q, want empty", got)
	}
	if got := Blanks(1); got != "" {
		t.Errorf("Blanks(1) = %q, want empty (single line, no newline)", got)
	}
	if got := Blanks(3); strings.Count(got, "\n") != 2 {
		t.Errorf("Blanks(3) should have 2 newlines, got %q", got)
	}
}

func TestIndentLines(t *testing.T) {
	if got := IndentLines("a\nb", "> "); got != "> a\n> b" {
		t.Errorf("IndentLines = %q", got)
	}
}

func TestWithTitle(t *testing.T) {
	if got := WithTitle("", "body"); got != "body" {
		t.Errorf("empty title should return body unchanged, got %q", got)
	}
	got := WithTitle("Title", "body")
	if !strings.Contains(got, "body") {
		t.Errorf("WithTitle should keep the body, got %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Errorf("WithTitle should prepend a title bar (extra line), got %q", got)
	}
}

func TestGetOptional(t *testing.T) {
	if got := GetOptional(5); got != 5 {
		t.Errorf("default: got %d", got)
	}
	if got := GetOptional(5, 9); got != 9 {
		t.Errorf("supplied: got %d", got)
	}
	if got := GetOptionalIdx(false, 0, true); got != true {
		t.Errorf("idx 0 supplied: got %v", got)
	}
	if got := GetOptionalIdx("x", 1, "a"); got != "x" {
		t.Errorf("idx out of range should return default, got %q", got)
	}
	if got := GetOptionalIdx("x", 1, "a", "b"); got != "b" {
		t.Errorf("idx 1 supplied: got %q", got)
	}
}

func TestMatchKey(t *testing.T) {
	b := Hint("up", Keys.Up) // carries up/k/w
	if !MatchKey("k", b) {
		t.Error("MatchKey should match a key carried by the binding")
	}
	if MatchKey("z", b) {
		t.Error("MatchKey should reject a key the binding does not carry")
	}
}

func TestPopupBox(t *testing.T) {
	withTitle := PopupBox("Title", "Body", 0)
	if !strings.Contains(withTitle, "Title") || !strings.Contains(withTitle, "Body") {
		t.Errorf("PopupBox should contain title and body, got:\n%s", withTitle)
	}
	if !strings.Contains(withTitle, "│") {
		t.Errorf("PopupBox should draw a border, got:\n%s", withTitle)
	}
	// A title adds a head line + blank, so the titled box is taller than the untitled one.
	noTitle := PopupBox("", "Body", 0)
	if strings.Count(withTitle, "\n") <= strings.Count(noTitle, "\n") {
		t.Errorf("titled popup should be taller than untitled")
	}
}

// TestShortHelpFullColumns checks the full (?) help renders the central chrome column
// (output/refresh/mouse/quit) alongside a tab's own action keys, and that an action key
// duplicating a chrome key (a "clear log" a tab still lists) is shown once, not twice.
func TestShortHelpFullColumns(t *testing.T) {
	extra := []key.Binding{
		FullHint("terminal", Keys.Terminal),
		FullHint("open dir", Keys.OpenDir),
		FullHint("clear log", Keys.Clear), // duplicates the chrome column's clear-log key
	}
	l := NewSelectList(nil, "T", extra...)
	l.Help.ShowAll = true
	out := ShortHelp(l, HelpMinimal)

	for _, want := range []string{"focus log", "refresh", "mouse", "quit", "terminal", "open dir"} {
		if !strings.Contains(out, want) {
			t.Errorf("full help missing %q:\n%s", want, out)
		}
	}
	if n := strings.Count(out, "clear log"); n != 1 {
		t.Errorf("clear log should render once (deduped from actions), got %d:\n%s", n, out)
	}
}

// TestMarqueeSeg pins the window arithmetic that lets a compact row's two differently
// styled halves slide as one string: each half is cut against the SAME [offset,
// offset+width) window, addressed by where the half starts in the combined row.
func TestMarqueeSeg(t *testing.T) {
	const title, tail = "abcdefgh", "  xy" // combined: "abcdefgh  xy", 12 cells
	for _, tc := range []struct {
		name              string
		seg               string
		start, off, width int
		want              string
	}{
		{"whole segment inside window", title, 0, 0, 8, "abcdefgh"},
		{"window wider than the row", title, 0, 0, 99, "abcdefgh"},
		{"title clipped on the right", title, 0, 0, 6, "abcdef"},
		{"title clipped on both ends", title, 0, 2, 4, "cdef"},
		{"title fully scrolled past", title, 0, 8, 4, ""},
		{"tail not reached yet", tail, 8, 0, 6, ""},
		{"tail partly in view", tail, 8, 4, 6, "  "},
		{"tail flush at the right edge", tail, 8, 6, 6, "  xy"},
		{"tail clipped on the left", tail, 8, 10, 6, "xy"},
		{"negative-width window yields nothing", title, 0, 4, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := marqueeSeg(tc.seg, tc.start, tc.off, tc.width); got != tc.want {
				t.Errorf("marqueeSeg(%q, %d, %d, %d) = %q, want %q",
					tc.seg, tc.start, tc.off, tc.width, got, tc.want)
			}
		})
	}
}

// TestMarqueeSegWide: the cuts are cell-based, not byte-based, so a multi-byte path
// windows by what it occupies on screen.
func TestMarqueeSegWide(t *testing.T) {
	const s = "æøå/über/"
	if got := marqueeSeg(s, 0, 0, 4); got != "æøå/" {
		t.Errorf("multi-byte window = %q, want æøå/", got)
	}
	if got := marqueeSeg(s, 0, 4, 5); got != "über/" {
		t.Errorf("multi-byte offset window = %q, want über/", got)
	}
}

type marqueeItem struct{ title, suffix string }

func (i marqueeItem) Title() string       { return i.title }
func (i marqueeItem) SuffixText() string  { return i.suffix }
func (i marqueeItem) FilterValue() string { return i.title }

// renderCompact renders row 0 of a compact list `width` cells wide, with the marquee
// pointed at off (nil ⇒ the static truncation every list had before).
func renderCompact(t *testing.T, off *int, width int, items ...list.Item) string {
	t.Helper()
	l := NewCompactList(items, "")
	l.SetSize(width, 10)
	var b strings.Builder
	CompactDelegate{Offset: off}.Render(&b, l, 0, items[0])
	return b.String()
}

// TestCompactMarqueeRender: an overflowing SELECTED row slides as one string — the two
// windows tile the row exactly at every offset, the left edge advances a cell at a time,
// and the last offset brings the whole suffix into view. Width is the load-bearing
// assertion: a row that changed width mid-slide would shove the frame around it.
func TestCompactMarqueeRender(t *testing.T) {
	const width = 28 // textWidth 26 once the delegate's padding comes off
	item := marqueeItem{title: "architecture-notes.md", suffix: "design/deep/"}
	row, over := CompactMarquee(item, CompactTextWidth(width))
	if !over {
		t.Fatal("test fixture must overflow to exercise the marquee")
	}
	maxOff := row.Width() - CompactTextWidth(width)

	var prev string
	for off := 0; off <= maxOff; off++ {
		out := renderCompact(t, &off, width, item)
		if w := lipgloss.Width(out); w != width {
			t.Fatalf("offset %d: rendered width = %d, want a constant %d\n%q", off, w, width, out)
		}
		if out == prev {
			t.Errorf("offset %d rendered identically to offset %d — the row did not move", off, off-1)
		}
		prev = out
	}

	// Offset 0 shows the name from its start; the last offset has scrolled far enough
	// that the whole suffix — the thing the static fit used to drop — is on screen.
	zero := 0
	if first := renderCompact(t, &zero, width, item); !strings.Contains(first, "architecture-notes.md") {
		t.Errorf("offset 0 should show the name from its left edge, got %q", first)
	}
	if last := renderCompact(t, &maxOff, width, item); !strings.HasSuffix(last, "design/deep/") {
		t.Errorf("last offset should end on the full suffix, got %q", last)
	}
}

// TestCompactMarqueeOffsetClamped: a stale offset (the panel resized under it, or the
// cursor moved to a shorter row) is clamped rather than windowing past the end into blanks.
func TestCompactMarqueeOffsetClamped(t *testing.T) {
	item := marqueeItem{title: "architecture-notes.md", suffix: "design/deep/"}
	huge, negative := 9999, -5
	row, _ := CompactMarquee(item, CompactTextWidth(28))
	atEnd := renderCompact(t, ptr(row.Width()-CompactTextWidth(28)), 28, item)

	if got := renderCompact(t, &huge, 28, item); got != atEnd {
		t.Errorf("an over-large offset should clamp to the last one:\n got %q\nwant %q", got, atEnd)
	}
	if got, want := renderCompact(t, &negative, 28, item), renderCompact(t, ptr(0), 28, item); got != want {
		t.Errorf("a negative offset should clamp to 0:\n got %q\nwant %q", got, want)
	}
}

// TestCompactMarqueeInert: the marquee only ever touches an overflowing selected row.
// Everything else — a row that fits, an unselected row, a list with no offset at all —
// must render byte-for-byte what it did before the marquee existed.
func TestCompactMarqueeInert(t *testing.T) {
	off := 4

	fits := marqueeItem{title: "doc0.md", suffix: "notes/"}
	if got, want := renderCompact(t, &off, 28, fits), renderCompact(t, nil, 28, fits); got != want {
		t.Errorf("a row that fits must not move:\n got %q\nwant %q", got, want)
	}

	long := marqueeItem{title: "architecture-notes.md", suffix: "design/deep/"}
	l := NewCompactList([]list.Item{fits, long}, "")
	l.SetSize(28, 10)
	var moving, static strings.Builder
	CompactDelegate{Offset: &off}.Render(&moving, l, 1, long) // index 1, cursor is on 0
	CompactDelegate{}.Render(&static, l, 1, long)
	if moving.String() != static.String() {
		t.Errorf("an unselected row must not move:\n got %q\nwant %q", moving.String(), static.String())
	}

	// The static path keeps its ellipsis; the sliding one deliberately has none.
	if s := renderCompact(t, nil, 28, long); !strings.Contains(s, "…") {
		t.Errorf("the static fit should still mark truncation with an ellipsis, got %q", s)
	}
}

func ptr(i int) *int { return &i }
