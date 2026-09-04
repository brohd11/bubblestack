package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// scan renders a page and reads its link spans back — the round trip every consumer
// makes, and the only way to test either half honestly: the spans are meaningless
// except against the rows the renderer actually produced.
func scan(body string, width int) (string, LinkMap) {
	out := RenderMarkdown(body, width)
	return out, ScanLinks(out)
}

// rowAt is one row of a render with the styling stripped — what the span's columns
// index into.
func rowAt(out string, row int) string {
	rows := strings.Split(ansi.Strip(out), "\n")
	if row >= len(rows) {
		return ""
	}
	return rows[row]
}

// cells is the text a span covers: ansi.Cut rather than a slice expression, because a
// span's Col and Width are display columns and a row holding a bullet ("•") or any
// other multi-byte rune does not index the same way by byte.
func cells(row string, col, width int) string { return ansi.Cut(row, col, col+width) }

// TestScanLinksSpan: a link's span lands on the label, at the column the label
// actually prints at — measured against the stripped row rather than asserted as a
// number, so a change in the renderer's spacing can't quietly pass.
func TestScanLinksSpan(t *testing.T) {
	out, links := scan("see the [manual](https://x.example/y?a=1;b=2) for more\n", 80)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d: %#v", len(links), links)
	}
	l := links[0]
	if l.Target != "https://x.example/y?a=1;b=2" {
		t.Errorf("target = %q — a target holding its own %q must survive the OSC split", l.Target, ";")
	}
	if l.Text != "manual" {
		t.Errorf("text = %q, want %q", l.Text, "manual")
	}
	row := rowAt(out, l.Row)
	if got := cells(row, l.Col, l.Width); got != "manual" {
		t.Errorf("span covers %q in %q, want the label", got, row)
	}
}

// TestScanLinksNone: the common page pays nothing. A nil map is also what a pane can
// hand around without a nil check.
func TestScanLinksNone(t *testing.T) {
	if links := ScanLinks(RenderMarkdown("plain prose, `code`, **bold**\n", 40)); links != nil {
		t.Errorf("a page with no links must scan to nil, got %#v", links)
	}
}

// TestScanLinksWrapped: a label the wrap splits lands one span per row, and the two
// halves together are the label. The opening escape rides with the first word only, so
// the second row is clickable ONLY because the scan carries the open state across rows.
func TestScanLinksWrapped(t *testing.T) {
	out, links := scan("[a label long enough to wrap](./x.md) tail\n", 20)
	if len(links) != 2 {
		t.Fatalf("want the label split across 2 rows, got %d: %#v", len(links), links)
	}
	if links[0].Row == links[1].Row {
		t.Errorf("both spans landed on row %d", links[0].Row)
	}
	var got string
	for _, l := range links {
		if l.Target != "./x.md" {
			t.Errorf("target = %q, want ./x.md on every row of a split label", l.Target)
		}
		if want := cells(rowAt(out, l.Row), l.Col, l.Width); want != l.Text {
			t.Errorf("row %d span covers %q but reports %q", l.Row, want, l.Text)
		}
		got += l.Text
	}
	if got != "a label long enoughto wrap" {
		t.Errorf("halves joined to %q — want the label minus the space the wrap ate", got)
	}
}

// TestScanLinksIndented: a link inside a bullet is offset by the marker the renderer
// hangs the item under, which is exactly the kind of shift no source-level guess would
// get right.
func TestScanLinksIndented(t *testing.T) {
	out, links := scan("- go to [there](there.md)\n", 40)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	l := links[0]
	if l.Col == 0 {
		t.Error("a bullet's link cannot start in column 0 — the marker precedes it")
	}
	if got := cells(rowAt(out, l.Row), l.Col, l.Width); got != "there" {
		t.Errorf("span covers %q, want the label", got)
	}
}

// TestLinkMapAt: the cells inside a span hit and the ones on either side miss.
func TestLinkMapAt(t *testing.T) {
	m := LinkMap{{Target: "a", Row: 2, Col: 4, Width: 3}}
	for _, tc := range []struct {
		row, col int
		want     bool
	}{
		{2, 4, true}, {2, 6, true},
		{2, 3, false}, {2, 7, false}, {1, 5, false}, {3, 5, false},
	} {
		if _, ok := m.At(tc.row, tc.col); ok != tc.want {
			t.Errorf("At(%d,%d) = %v, want %v", tc.row, tc.col, ok, tc.want)
		}
	}
}

// TestLinkHooksDo: every destination reaches its own hook, and only its own.
func TestLinkHooksDo(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(text, []byte("# notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(binary, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01}, 0o644); err != nil {
		t.Fatal(err)
	}

	var fired string
	var got Link
	hook := func(kind string) func(*core.Shared, Link) core.Action {
		return func(_ *core.Shared, l Link) core.Action {
			fired, got = kind, l
			return core.Action{}
		}
	}
	h := LinkHooks{Base: dir, URL: hook("url"), Text: hook("text"), File: hook("file")}

	for _, tc := range []struct {
		target, want string
	}{
		{"https://example.com/a", "url"},
		{"mailto:someone@example.com", "url"},
		{"www.example.com", "url"},
		{"notes.md", "text"},
		{"./notes.md#a-heading", "text"}, // the fragment is not part of the path
		{"shot.png", "file"},
		{".", "file"},          // a directory is opened, not read
		{"missing.md", "text"}, // unresolved goes to the consumer that knows
		{"#local", ""},         // an in-page anchor does nothing
		{"", ""},
	} {
		fired = ""
		h.Do(nil, Link{Target: tc.target})
		if fired != tc.want {
			t.Errorf("%q fired %q, want %q", tc.target, fired, tc.want)
		}
	}

	// The resolved path and its existence reach the hook: a consumer that opens a buffer
	// must be able to refuse a target that isn't there.
	fired = ""
	h.Do(nil, Link{Target: "notes.md"})
	if got.Path != text || !got.Exists {
		t.Errorf("hook got path %q exists=%v, want %q true", got.Path, got.Exists, text)
	}
	h.Do(nil, Link{Target: "missing.md"})
	if got.Exists {
		t.Error("a missing target must reach the hook with Exists false")
	}

	// A Windows drive letter is a path, not a scheme — the reason linkScheme wants two
	// characters before the colon.
	if linkScheme.MatchString(`C:\work\notes.md`) {
		t.Error(`C:\ must not read as a URL scheme`)
	}
}

// TestLinkHooksNil: an unwired kind is silently inert, which is what lets a component
// render links whether or not anyone handles them.
func TestLinkHooksNil(t *testing.T) {
	var h LinkHooks
	for _, target := range []string{"https://example.com", "notes.md", "/etc"} {
		if act := h.Do(nil, Link{Target: target}); act.Msg != nil || act.Cmd != nil {
			t.Errorf("%q on nil hooks returned %#v, want the zero action", target, act)
		}
	}
}

// leftClick is a plain left press at a cell — what both panes hit-test.
func leftClick(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

// TestDocScreenClickFollowsLink walks the whole geometry a page click crosses: the
// gutter on the left, the title bar above the body, and the scroll offset. Each of
// those is a silent off-by-N — a click lands on prose and nothing happens — so the
// coordinates are computed from the span the render actually produced.
func TestDocScreenClickFollowsLink(t *testing.T) {
	body := "top [first](one.md) line\n\n" +
		strings.Repeat("some filler prose\n\n", 30) +
		"end [second](two.md) line\n"
	var got string
	d := NewDocScreen(DocOpts{
		Title:  "Manual",
		Render: func(w int) string { return RenderMarkdown(body, w) },
		Links: LinkHooks{Text: func(_ *core.Shared, l Link) core.Action {
			got = l.Target
			return core.Action{}
		}},
	})
	sh := core.NewShared(nil)
	d.SetSize(sh, 40, 12)
	if len(d.links) != 2 {
		t.Fatalf("want the render's 2 links cached at SetSize, got %d", len(d.links))
	}
	titleH := lipgloss.Height(core.RenderTitleBar("Manual"))
	click := func(l Link, dx int) {
		got = ""
		d.Update(sh, leftClick(l.Col+len(gutter)+dx, l.Row+titleH-d.vp.YOffset()))
	}

	click(d.links[0], 0)
	if got != "one.md" {
		t.Errorf("clicking the first link fired %q, want one.md", got)
	}

	// Scrolled: the same span now sits at a different terminal row.
	d.vp.SetYOffset(d.links[1].Row)
	click(d.links[1], 0)
	if got != "two.md" {
		t.Errorf("clicking a scrolled link fired %q, want two.md", got)
	}

	click(d.links[1], -1)
	if got != "" {
		t.Errorf("the cell before a span fired %q, want nothing", got)
	}
}

// TestScrollContainerClickFollowsLink: the pane's own chrome (a border and a column of
// padding on the left, the hand-drawn edge on top) is what a click has to be measured
// against, and an unfocused pane ignores the gesture entirely — mouse messages are
// broadcast, so acting while blurred would fire from a click in another pane.
func TestScrollContainerClickFollowsLink(t *testing.T) {
	p := NewScrollContainer("preview")
	p.SetSize(40, 10)
	out := RenderMarkdown("see the [manual](x.md) now\n", p.TextWidth())
	links := ScanLinks(out)
	if len(links) != 1 {
		t.Fatalf("want 1 link, got %d", len(links))
	}
	p.SetLines(strings.Split(out, "\n"))
	p.SetLinks(links)

	var got string
	p.OnLink = func(_ *core.Shared, l Link) core.Action {
		got = l.Target
		return core.Action{}
	}
	sh := core.NewShared(nil)
	msg := leftClick(links[0].Col+2, links[0].Row+1)

	if _, handled := p.UpdatePanel(sh, msg); handled || got != "" {
		t.Errorf("an unfocused pane must ignore a click, handled=%v fired %q", handled, got)
	}
	p.Focus()
	if _, handled := p.UpdatePanel(sh, msg); !handled {
		t.Error("a click on a link must be handled")
	}
	if got != "x.md" {
		t.Errorf("fired %q, want x.md", got)
	}

	got = ""
	if _, handled := p.UpdatePanel(sh, leftClick(0, 0)); handled || got != "" {
		t.Errorf("a click on the border fired %q (handled=%v), want nothing", got, handled)
	}
}

// TestFindDocPage: a link between manual pages resolves by filename or title, ignores
// any path and fragment around it, and resolves to nothing when the set has no such
// page — which is what keeps a compiled-in manual inside its own embedded FS.
func TestFindDocPage(t *testing.T) {
	pages := []DocPage{
		{Title: "Getting started", File: "01-start.md"},
		{Title: "Config", File: "02-config.md"},
	}
	for _, tc := range []struct {
		target, want string
	}{
		{"02-config.md", "Config"},
		{"./02-config.md", "Config"},
		{"../doc/embedded/02-config.md#keys", "Config"},
		{"Config", "Config"},
		{"getting started", "Getting started"},
		{"/etc/passwd", ""},
		{"03-missing.md", ""},
		{"", ""},
	} {
		p, ok := findDocPage(pages, tc.target)
		if got := p.Title; (got != tc.want) || (ok != (tc.want != "")) {
			t.Errorf("findDocPage(%q) = %q,%v — want %q", tc.target, got, ok, tc.want)
		}
	}
}

// TestDocPageLinksToSibling: the manual's own hook pushes a sibling page and does
// nothing for a target outside the set.
func TestDocPageLinksToSibling(t *testing.T) {
	pages := []DocPage{{Title: "One", File: "01.md"}, {Title: "Two", File: "02.md", Body: "body\n"}}
	d := newDocPage(pages[0], pages)
	if act := d.Links.Text(nil, Link{Target: "02.md"}); act.Msg == nil {
		t.Error("a link to a sibling page should push it")
	}
	if act := d.Links.Text(nil, Link{Target: "../../secrets.md"}); act.Msg != nil {
		t.Error("a target outside the page set must do nothing")
	}
}
