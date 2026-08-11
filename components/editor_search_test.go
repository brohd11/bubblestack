package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func editorSearchKey(s *EditorScreen, sh *core.Shared, typ tea.KeyType) core.Action {
	_, act := s.key(sh, tea.KeyMsg{Type: typ})
	return act
}

func lineEditKey(s *LineEditScreen, sh *core.Shared, msg tea.KeyMsg) core.Action {
	_, act := s.Update(sh, msg)
	return act
}

func setSearchQuery(s *EditorScreen, query string) {
	s.searchQuery = query
	s.searchSeq = -1
}

func TestEditorSearchInteraction(t *testing.T) {
	s, sh := newEditor(EditorOpts{Title: "notes.md", Search: true})
	s.setContent("Alpha alpha ALPHA\nalphabet\nbeta")

	if act := editorSearchKey(s, sh, tea.KeyCtrlF); act.Msg == nil {
		t.Fatal("ctrl+f should push a floating line edit")
	}
	edit := s.searchEdit(sh)
	if cmd := edit.Init(sh); cmd != nil || edit.input.Cursor.Mode() != cursor.CursorStatic {
		t.Fatal("search line edit should use a visible, non-blinking cursor")
	}
	lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alpha")})
	if got := s.searchQuery; got != "alpha" {
		t.Fatalf("live query = %q, want alpha", got)
	}
	s.rebuildSearchMatches()
	if got := len(s.searchMatches[0]); got != 3 {
		t.Fatalf("case-insensitive matches on row 0 = %d, want 3", got)
	}
	if got := len(s.searchMatches[1]); got != 1 {
		t.Fatalf("prefix match on row 1 = %d, want 1", got)
	}
	if got := ansi.Strip(s.titleText()); !strings.Contains(got, "notes.md · find: alpha") {
		t.Fatalf("editing title = %q, want file and live query", got)
	}

	if act := lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyEnter}); act.Msg == nil || s.searchQuery != "alpha" {
		t.Fatalf("enter should pop the overlay and retain alpha, query=%q", s.searchQuery)
	}
	if got := ansi.Strip(s.titleText()); !strings.Contains(got, "find: alpha") {
		t.Fatalf("retained title = %q, want active query", got)
	}

	edit = s.searchEdit(sh)
	lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyCtrlU})
	if s.searchQuery != "" {
		t.Fatalf("clearing the input should clear live matches, query=%q", s.searchQuery)
	}
	lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyEsc})
	if s.searchQuery != "alpha" {
		t.Fatalf("escape should restore alpha, query=%q", s.searchQuery)
	}

	edit = s.searchEdit(sh)
	lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyCtrlU})
	lineEditKey(edit, sh, tea.KeyMsg{Type: tea.KeyEnter})
	if s.searchQuery != "" {
		t.Fatalf("submitting empty should stop search, query=%q", s.searchQuery)
	}
	if got := ansi.Strip(s.titleText()); got != "notes.md" {
		t.Fatalf("cleared title = %q, want ordinary file title", got)
	}
}

func TestEditorSearchIsOptIn(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	if act := editorSearchKey(s, sh, tea.KeyCtrlF); act.Msg != nil || act.Cmd != nil {
		t.Fatal("ctrl+f must remain inert when search is disabled")
	}
	for _, binding := range s.HelpBindings() {
		if strings.Contains(strings.Join(binding.Keys(), " "), "ctrl+f") {
			t.Fatal("disabled search must not advertise ctrl+f")
		}
	}

	enabled, _ := newEditor(EditorOpts{Search: true})
	var advertised bool
	for _, binding := range enabled.HelpBindings() {
		advertised = advertised || strings.Contains(strings.Join(binding.Keys(), " "), "ctrl+f")
	}
	if !advertised {
		t.Fatal("enabled search should advertise ctrl+f")
	}
}

func TestEditorSearchUnicodeTabsAndCacheRefresh(t *testing.T) {
	s, _ := newEditor(EditorOpts{Search: true})
	s.setContent("x\tFÖÖ föö")
	setSearchQuery(s, "\tföö")
	s.rebuildSearchMatches()
	if got := s.searchMatches[0]; len(got) != 1 || got[0] != (textRange{from: 1, to: 8}) {
		t.Fatalf("unicode/tab match = %#v, want display-cell range [1,8)", got)
	}
	for cell := 1; cell < 8; cell++ { // four tab cells + three letters
		if !s.cellMatched(0, cell) {
			t.Fatalf("display cell %d should be matched", cell)
		}
	}
	if s.cellMatched(0, 0) || s.cellMatched(0, 8) {
		t.Fatal("cells outside the literal match should not be highlighted")
	}

	// A buffer edit invalidates the same query's cached rune ranges.
	s.curX = 0
	typeRunes(s, 'z')
	s.rebuildSearchMatches()
	if got := s.searchMatches[0][0]; got != (textRange{from: 2, to: 9}) {
		t.Fatalf("match after edit = %#v, want shifted display range [2,9)", got)
	}
}

func TestEditorSearchRenderingComposesWithEditorLayers(t *testing.T) {
	withColor(t)
	s, _ := newEditor(EditorOpts{Search: true, Highlighter: NewMarkdownHighlighter()})
	s.setContent("# Foo foo\n" + strings.Repeat("a", 120))
	setSearchQuery(s, "foo")

	row := s.renderLine(0)
	searchStyle := s.editorSearchStyle()
	if !strings.Contains(row, searchStyle.Render("Foo")) || !strings.Contains(row, searchStyle.Render("foo")) {
		t.Fatalf("syntax-highlighted row should contain both search-painted matches: %q", row)
	}
	if lipgloss.Width(row) != lipgloss.Width("# Foo foo") {
		t.Fatalf("search styling changed row width to %d", lipgloss.Width(row))
	}

	// Selection wins over the search background, while the caret wins over both.
	selectRange(s, 0, 2, 0, 5)
	row = s.renderLine(0)
	selected := mdHeadingStyle.Background(core.MutedColor).Foreground(core.OnFocusedColor).Render("Foo")
	if !strings.Contains(row, selected) {
		t.Fatalf("selection should paint over a search result: %q", row)
	}
	s.clearSelection()
	s.curX = 2
	row = s.renderLine(0)
	if !strings.Contains(row, editorCursorStyle.Render("F")) {
		t.Fatalf("caret should paint over a search result: %q", row)
	}

	// Search ranges are cell-based at render time, so wrap and horizontal clipping
	// retain rectangular rows even when a match crosses the visible boundary.
	setSearchQuery(s, "aaaa")
	s.SetSize(nil, 12, 6)
	s.ToggleWrap()
	assertRowGeometry(t, s, "search-highlighted wrap")
	s.ToggleWrap()
	s.scrX = 3
	assertRowGeometry(t, s, "search-highlighted horizontal scroll")
}

func TestEditorSearchTitleStaysWithinNarrowPane(t *testing.T) {
	s, _ := newEditor(EditorOpts{Title: "a-very-long-document-name.md", Search: true})
	s.SetSize(nil, 24, 6)
	setSearchQuery(s, "needle-that-is-also-long")
	if got := lipgloss.Width(s.titleText()); got > s.w-2 {
		t.Fatalf("retained search title width = %d, available %d", got, s.w-2)
	}
	s.SetPaneOrigin(0, 0)
	sh := core.NewShared(nil)
	edit := s.searchEdit(sh)
	edit.SetSize(nil, 24, 6)
	if got := lipgloss.Width(edit.View(sh)); got > 24 {
		t.Fatalf("search overlay width = %d, pane width 24", got)
	}
}
