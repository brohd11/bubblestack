package components

import (
	"strings"
	"testing"
	"time"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

var testHighlightStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))

// countingHL records the generic highlighter lifecycle without tying the editor tests
// to any language implementation.
type countingHL struct {
	parses int
	last   string
	lines  []string
}

type restartHL struct {
	countingHL
	restart []int
}

func (h *restartHL) HighlightRestartLine(row int) int {
	if row < 0 || row >= len(h.restart) {
		return row
	}
	return h.restart[row]
}

func trackedHighlighterLanguage(created *[]*restartHL, restart []int) EditorLanguageResolver {
	return func(string) *EditorLanguageConfig {
		return &EditorLanguageConfig{NewHighlighter: func() Highlighter {
			h := &restartHL{restart: append([]int(nil), restart...)}
			*created = append(*created, h)
			return h
		}}
	}
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
	return []Span{{Text: c.lines[row], Style: testHighlightStyle}}
}

type brokenHL struct{}

func (brokenHL) Parse(string) {}
func (brokenHL) HighlightLine(int) []Span {
	return []Span{{Text: "nope", Style: testHighlightStyle}}
}

func highlighterLanguage(path string) *EditorLanguageConfig {
	if strings.HasSuffix(strings.ToLower(path), ".lit") {
		return &EditorLanguageConfig{NewHighlighter: func() Highlighter { return &countingHL{} }}
	}
	return nil
}

func TestEditorLanguageHighlighterResolution(t *testing.T) {
	plain := NewEditorScreen(EditorOpts{Path: "notes.txt", ResolveLanguage: highlighterLanguage})
	if plain.hl != nil {
		t.Fatal("a nil language profile should leave the editor plain")
	}

	a := NewEditorScreen(EditorOpts{Path: "one.LIT", ResolveLanguage: highlighterLanguage})
	b := NewEditorScreen(EditorOpts{Path: "two.lit", ResolveLanguage: highlighterLanguage})
	if a.hl == nil || b.hl == nil || a.hl == b.hl {
		t.Fatal("each resolved language application should create a fresh highlighter")
	}

	explicit := &countingHL{}
	s := NewEditorScreen(EditorOpts{
		Path: "notes.txt", ResolveLanguage: highlighterLanguage, Highlighter: explicit,
	})
	s.applySaveName("notes.lit")
	if s.hl != explicit {
		t.Fatal("an explicit highlighter must win over a profile and survive a rename")
	}
}

func TestEditorConfiguredHighlightRender(t *testing.T) {
	styled, _ := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: highlighterLanguage})
	styled.setContent("alpha\na\tb")
	plain, _ := newEditor(EditorOpts{})
	plain.setContent("alpha\na\tb")
	styled.curY, plain.curY = 1, 1
	if got := styled.renderLine(0); !strings.Contains(got, testHighlightStyle.Render("alpha")) {
		t.Fatalf("configured row did not render through its highlighter: %q", got)
	}
	for row := range styled.lines {
		styled.curY, styled.curX = row, 0
		plain.curY, plain.curX = row, 0
		got, want := styled.renderLine(row), plain.renderLine(row)
		if lipgloss.Width(got) != lipgloss.Width(want) || strings.Contains(got, "\t") {
			t.Fatalf("row %d broke render geometry: styled %q plain %q", row, got, want)
		}
	}
	if strings.Contains(styled.View(core.NewShared(nil)), "\t") {
		t.Fatal("a highlighted View must never emit a raw tab")
	}
}

func TestEditorHighlighterMismatchFallsBack(t *testing.T) {
	s, _ := newEditor(EditorOpts{Highlighter: brokenHL{}})
	s.setContent("hello world")
	plain, _ := newEditor(EditorOpts{})
	plain.setContent("hello world")
	for _, cur := range []int{0, 5, len("hello world")} {
		s.curX, plain.curX = cur, cur
		if got, want := s.renderLine(0), plain.renderLine(0); got != want {
			t.Fatalf("cursor %d: broken spans rendered %q, want plain %q", cur, got, want)
		}
	}
}

func TestEditorHighlighterReparseCache(t *testing.T) {
	h := &countingHL{}
	s, sh := newEditor(EditorOpts{Highlighter: h})
	s.setContent("alpha\nbeta")
	s.View(sh)
	s.View(sh)
	if h.parses != 1 {
		t.Fatalf("unchanged frames parsed %d times, want 1", h.parses)
	}
	typeRunes(s, 'x')
	s.View(sh)
	if h.parses != 1 {
		t.Fatalf("an active edit parsed immediately: %d parses", h.parses)
	}
	s.hlChanged = time.Now().Add(-s.highlightDelay())
	s.View(sh)
	if h.parses != 2 || h.last != buffer(s) {
		t.Fatalf("edit parse count/content = %d/%q, want 2/%q", h.parses, h.last, buffer(s))
	}
}

func TestEditorHighlighterDebounceUsesLatestEdit(t *testing.T) {
	h := &countingHL{}
	s, sh := newEditor(EditorOpts{Highlighter: h})
	s.setContent("alpha\nbeta")
	s.View(sh)
	s.hlDebounce = time.Hour

	_, act := s.Update(sh, keyMsg("x"))
	if act.Cmd == nil {
		t.Fatal("an edit should schedule a highlighter refresh")
	}
	s.View(sh)
	if h.parses != 1 {
		t.Fatalf("highlighting parsed inside the quiet window: %d", h.parses)
	}
	seq := s.editSeq
	s.Update(sh, editorHighlightMsg{target: s, seq: seq - 1})
	if h.parses != 1 {
		t.Fatal("a stale highlight message reparsed the buffer")
	}

	s.hlChanged = time.Now().Add(-s.highlightDelay())
	s.Update(sh, editorHighlightMsg{target: s, seq: seq})
	if h.parses != 2 || h.last != "xalpha\nbeta" || s.hlSeq != seq {
		t.Fatalf("latest parse = count %d text %q seq %d", h.parses, h.last, s.hlSeq)
	}
	s.Update(sh, editorHighlightMsg{target: s, seq: seq})
	if h.parses != 2 {
		t.Fatal("an already-current highlight message parsed twice")
	}
}

func TestEditorHighlighterFactoryPreviewsEditedLine(t *testing.T) {
	var created []*restartHL
	s, sh := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: trackedHighlighterLanguage(&created, nil)})
	s.setContent("alpha\nbeta\ngamma")
	exact := s.hl.(*restartHL)
	exact.Parse(s.Text())
	s.acceptHighlight(exact, s.editSeq)

	_, act := s.Update(sh, keyMsg("x"))
	if act.Cmd == nil {
		t.Fatal("an edit should retain the exact-parse debounce command")
	}
	if exact.parses != 1 {
		t.Fatalf("the active exact snapshot was mutated in the input path: parses=%d", exact.parses)
	}
	if !s.previewCovers(0) || spansText(s.hlSpans(0)) != "xalpha" {
		t.Fatalf("edited row was not provisionally highlighted: %#v", s.hlSpans(0))
	}
	if got := s.hlRows; len(got) != 3 || got[0] != -1 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("single-line edit row map = %v, want [-1 1 2]", got)
	}
}

func TestEditorHighlighterRebasesInsertedAndDeletedLines(t *testing.T) {
	var created []*restartHL
	s, sh := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: trackedHighlighterLanguage(&created, nil)})
	s.setContent("one\ntwo\nthree")
	exact := s.hl.(*restartHL)
	exact.Parse(s.Text())
	s.acceptHighlight(exact, s.editSeq)
	s.curX = len(s.lines[0])

	s.Update(sh, keyMsg("enter"))
	if got := s.hlRows; len(got) != 4 || got[0] != -1 || got[1] != -1 || got[2] != 1 || got[3] != 2 {
		t.Fatalf("row map after Enter = %v, want [-1 -1 1 2]", got)
	}
	if spansText(s.hlSpans(2)) != "two" || spansText(s.hlSpans(3)) != "three" {
		t.Fatal("unchanged rows below Enter lost their rebased exact highlighting")
	}

	s.curY, s.curX = 1, 0
	s.Update(sh, keyMsg("backspace"))
	if got := s.hlRows; len(got) != 3 || got[0] != -1 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("row map after joining lines = %v, want [-1 1 2]", got)
	}

	joined := &restartHL{}
	joined.Parse(s.Text())
	s.acceptHighlight(joined, s.editSeq)
	s.Update(sh, keyMsg("ctrl+z"))
	if got := s.hlRows; len(got) != 4 || got[0] != -1 || got[1] != -1 || got[2] != 1 || got[3] != 2 {
		t.Fatalf("row map after undoing the join = %v, want [-1 -1 1 2]", got)
	}
	s.Update(sh, keyMsg("ctrl+y"))
	if got := s.hlRows; len(got) != 3 || got[0] != -1 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("row map after redoing the join = %v, want [-1 1 2]", got)
	}
}

func TestEditorHighlighterUsesBoundedRestartHint(t *testing.T) {
	lines := make([]string, editorHighlightPreviewLines+3)
	restarts := make([]int, len(lines))
	for i := range lines {
		lines[i] = "line"
		restarts[i] = 0
	}
	var created []*restartHL
	s, sh := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: trackedHighlighterLanguage(&created, restarts)})
	s.setContent(strings.Join(lines, "\n"))
	exact := s.hl.(*restartHL)
	exact.Parse(s.Text())
	s.acceptHighlight(exact, s.editSeq)
	s.curY = editorHighlightPreviewLines + 1
	s.curX = len(s.lines[s.curY])
	s.scrY = s.curY
	before := len(created)

	s.Update(sh, keyMsg("x"))
	if len(created) != before {
		t.Fatal("a restart beyond the preview budget launched a synchronous parser")
	}
	if !s.previewCovers(s.curY) || s.hlSpans(s.curY) != nil {
		t.Fatal("the affected row should render plain until the exact async result")
	}
}

func TestEditorHighlighterPreviewStartsAtMultilineHint(t *testing.T) {
	var created []*restartHL
	restarts := []int{0, 0, 0, 3}
	s, sh := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: trackedHighlighterLanguage(&created, restarts)})
	s.setContent("open\ninside\nclose\nafter")
	exact := s.hl.(*restartHL)
	exact.Parse(s.Text())
	s.acceptHighlight(exact, s.editSeq)
	s.curY, s.curX = 2, len(s.lines[2])

	s.Update(sh, keyMsg("x"))
	preview := created[len(created)-1]
	if preview.last != "open\ninside\nclosex\nafter" {
		t.Fatalf("preview fragment = %q, want parse from multiline opener", preview.last)
	}
	for row := 2; row <= 3; row++ {
		if !s.previewCovers(row) {
			t.Fatalf("visible row %d after the edit was not provisionally refreshed", row)
		}
	}
}

func TestEditorHighlighterExactParseIsSingleFlightAndVersioned(t *testing.T) {
	var created []*restartHL
	s, sh := newEditor(EditorOpts{Path: "x.lit", ResolveLanguage: trackedHighlighterLanguage(&created, nil)})
	s.setContent("old")
	firstSeq := s.editSeq
	cmd := s.startHighlightParse()
	if cmd == nil || !s.hlParsing {
		t.Fatal("the first exact parse should start asynchronously")
	}
	if s.startHighlightParse() != nil {
		t.Fatal("a second exact parse started while one was in flight")
	}
	job, epoch := s.hlJob, s.hlEpoch
	s.Update(sh, keyMsg("x"))
	s.hlChanged = time.Now().Add(-s.highlightDelay())
	stale := &restartHL{}
	stale.Parse("old")
	act := s.handleHighlightReady(editorHighlightReadyMsg{
		target: s, job: job, epoch: epoch, seq: firstSeq, hl: stale,
	})
	if act.Cmd == nil || !s.hlParsing || s.hlSeq == firstSeq {
		t.Fatal("a stale completion should be discarded and immediately start the latest parse")
	}

	latest := &restartHL{}
	latest.Parse(s.Text())
	s.handleHighlightReady(editorHighlightReadyMsg{
		target: s, job: s.hlJob, epoch: s.hlEpoch, seq: s.editSeq, hl: latest,
	})
	if s.hl != latest || s.hlSeq != s.editSeq || s.hlParsing {
		t.Fatal("the latest exact snapshot was not installed")
	}
}

func TestEditorHighlighterRejectsPreviousLanguageSnapshot(t *testing.T) {
	var created []*restartHL
	resolve := trackedHighlighterLanguage(&created, nil)
	s, _ := newEditor(EditorOpts{Path: "old.lit", ResolveLanguage: resolve})
	s.setContent("content")
	if s.startHighlightParse() == nil {
		t.Fatal("the old language parse should start")
	}
	job, epoch, seq := s.hlJob, s.hlEpoch, s.editSeq
	s.applyLanguage("new.lit")
	old := &restartHL{}
	old.Parse(s.Text())
	act := s.handleHighlightReady(editorHighlightReadyMsg{
		target: s, job: job, epoch: epoch, seq: seq, hl: old,
	})
	if s.hl == old || act.Cmd == nil || !s.hlParsing {
		t.Fatal("a completion from the previous language should be rejected and replaced")
	}
}

func TestEditorUnfocusedSuppressesHighlight(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{Highlighter: &countingHL{}})
	s.setContent("alpha")
	s.SetFocused(false)
	dark := s.View(sh)
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	if !strings.Contains(dark, muted.Render("alpha")) || strings.Contains(dark, testHighlightStyle.Render("alpha")) {
		t.Fatalf("unfocused render did not replace syntax styling with muted text: %q", dark)
	}
}
