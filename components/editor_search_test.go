package components

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func editorSearchKey(s *EditorScreen, sh *core.Shared, k string) core.Action {
	_, act := s.key(sh, keyMsg(k))
	return act
}

func lineEditKey(s *LineEditScreen, sh *core.Shared, msg tea.KeyPressMsg) core.Action {
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
	fullH := s.h

	if act := editorSearchKey(s, sh, "ctrl+f"); act.Msg == nil {
		t.Fatal("ctrl+f should push a floating line edit")
	}
	edit := s.searchEdit(sh)
	s.SetSize(sh, 80, 20)
	if !s.searchEditing || s.h != fullH-editorSearchBarH {
		t.Fatalf("open search should reserve %d bottom rows: editing=%v viewport=%d, want %d", editorSearchBarH, s.searchEditing, s.h, fullH-editorSearchBarH)
	}
	if cmd := edit.Init(sh); cmd != nil || edit.input.Styles().Cursor.Blink {
		t.Fatal("search line edit should use a visible, non-blinking cursor")
	}
	lineEditKey(edit, sh, keyMsg("alpha"))
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
	if got := ansi.Strip(s.titleText()); got != "notes.md" {
		t.Fatalf("search should not alter title, got %q", got)
	}
	if got := ansi.Strip(s.searchBar()); !strings.Contains(got, "find: alpha") {
		t.Fatalf("editing bottom bar = %q, want live query", got)
	}

	if act := lineEditKey(edit, sh, keyMsg("enter")); act.Msg == nil || s.searchQuery != "alpha" {
		t.Fatalf("enter should pop the overlay and retain alpha, query=%q", s.searchQuery)
	}
	s.SetSize(sh, 80, 20)
	if s.searchEditing || !s.searchBarVisible() || s.h != fullH-editorSearchBarH {
		t.Fatalf("retained search should leave an inactive reserved bar: editing=%v visible=%v viewport=%d", s.searchEditing, s.searchBarVisible(), s.h)
	}
	if got := ansi.Strip(s.searchBar()); !strings.Contains(got, "find: alpha") {
		t.Fatalf("retained bottom bar = %q, want active query", got)
	}

	edit = s.searchEdit(sh)
	lineEditKey(edit, sh, keyMsg("ctrl+u"))
	lineEditKey(edit, sh, keyMsg("beta"))
	lineEditKey(edit, sh, keyMsg("esc"))
	if s.searchQuery != "beta" || s.searchEditing {
		t.Fatalf("escape should retain beta and unfocus the bar, query=%q editing=%v", s.searchQuery, s.searchEditing)
	}

	edit = s.searchEdit(sh)
	lineEditKey(edit, sh, keyMsg("ctrl+u"))
	lineEditKey(edit, sh, keyMsg("esc"))
	if s.searchQuery != "" {
		t.Fatalf("escaping an empty search should stop it, query=%q", s.searchQuery)
	}
	s.SetSize(sh, 80, 20)
	if s.searchBarVisible() || s.h != fullH {
		t.Fatalf("escaping empty should remove the bar and restore viewport %d, visible=%v viewport=%d", fullH, s.searchBarVisible(), s.h)
	}
}

func TestEditorSearchSeedsSingleLineSelection(t *testing.T) {
	s, sh := newEditor(EditorOpts{Search: true})
	s.setContent("alpha beta alpha")
	setSearchQuery(s, "before")
	selectRange(s, 0, 6, 0, 10)

	edit := s.searchEdit(sh)
	if got := edit.input.Value(); got != "beta" {
		t.Fatalf("seeded field = %q, want selected text beta", got)
	}
	if s.searchQuery != "beta" {
		t.Fatalf("live query = %q, want beta", s.searchQuery)
	}
	s.rebuildSearchMatches()
	if got := len(s.searchMatches[0]); got != 1 {
		t.Fatalf("seeded selection found %d matches, want 1", got)
	}
	lineEditKey(edit, sh, keyMsg("esc"))
	if s.searchQuery != "beta" {
		t.Fatalf("escape retained %q, want seeded query beta", s.searchQuery)
	}

	edit = s.searchEdit(sh)
	lineEditKey(edit, sh, keyMsg("enter"))
	if s.searchQuery != "beta" {
		t.Fatalf("enter retained %q, want seeded query beta", s.searchQuery)
	}
}

func TestEditorSearchDoesNotSeedMultilineSelection(t *testing.T) {
	s, sh := newEditor(EditorOpts{Search: true})
	s.setContent("alpha\nbeta")
	setSearchQuery(s, "prior")
	selectRange(s, 0, 2, 1, 2)

	edit := s.searchEdit(sh)
	if got := edit.input.Value(); got != "prior" {
		t.Fatalf("multiline seed = %q, want prior query unchanged", got)
	}
	if s.searchQuery != "prior" {
		t.Fatalf("live query = %q, want prior", s.searchQuery)
	}
}

func TestEditorSearchIsOptIn(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	if act := editorSearchKey(s, sh, "ctrl+f"); act.Msg != nil || act.Cmd != nil {
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
	s, _ := newEditor(EditorOpts{Search: true, Highlighter: &countingHL{}})
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
	selected := testHighlightStyle.Background(core.MutedColor).Foreground(core.OnFocusedColor).Render("Foo")
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

func TestEditorSearchUsesThemeIndependentYellow(t *testing.T) {
	previous := core.CurrentTheme()
	t.Cleanup(func() { core.SetTheme(previous) })

	for _, theme := range core.ThemeNames() {
		if !core.SetTheme(theme) {
			t.Fatalf("could not set registered theme %q", theme)
		}
		for _, focused := range []bool{true, false} {
			s, _ := newEditor(EditorOpts{Search: true})
			s.SetFocused(focused)
			style := s.editorSearchStyle()
			if got := style.GetBackground(); !reflect.DeepEqual(got, core.Resolve(editorSearchYellow)) {
				t.Errorf("theme=%s focused=%v background=%v, want adaptive yellow %v", theme, focused, got, editorSearchYellow)
			}
			if got := style.GetForeground(); !reflect.DeepEqual(got, editorSearchText) {
				t.Errorf("theme=%s focused=%v foreground=%v, want %v", theme, focused, got, editorSearchText)
			}
		}
	}
}

func TestEditorSearchFocusedAndRetainedBorders(t *testing.T) {
	if got := lineEditBox().GetBorderTopForeground(); !reflect.DeepEqual(got, core.FocusedColor) {
		t.Fatalf("focused line edit border = %v, want theme accent %v", got, core.FocusedColor)
	}
	if got := retainedSearchBox().GetBorderTopForeground(); !reflect.DeepEqual(got, core.BorderColor) {
		t.Fatalf("retained search border = %v, want standard border %v", got, core.BorderColor)
	}
}

func TestEditorSearchBarStaysWithinNarrowPaneAndAnchorsBottom(t *testing.T) {
	s, _ := newEditor(EditorOpts{Title: "a-very-long-document-name.md", Search: true})
	s.SetSize(nil, 24, 6)
	setSearchQuery(s, "needle-that-is-also-long")
	s.SetSize(nil, 24, 6)
	if got := lipgloss.Width(s.searchBar()); got != 24 {
		t.Fatalf("retained search bar width = %d, pane width 24", got)
	}
	if got := lipgloss.Height(s.searchBar()); got != editorSearchBarH {
		t.Fatalf("retained search bar height = %d, want %d", got, editorSearchBarH)
	}
	s.SetPaneOrigin(7, 4)
	sh := core.NewShared(nil)
	edit := s.searchEdit(sh)
	edit.SetSize(nil, 24, 6)
	if got := lipgloss.Width(edit.View(sh)); got > 24 {
		t.Fatalf("search overlay width = %d, pane width 24", got)
	}
	if edit.x != 7 || edit.y != 4+6-editorSearchBarH {
		t.Fatalf("search overlay anchor = (%d,%d), want (7,%d)", edit.x, edit.y, 4+6-editorSearchBarH)
	}
	if got, want := lipgloss.Width(edit.View(sh)), lipgloss.Width(s.searchBar()); got != want {
		t.Fatalf("focused width = %d, retained width %d", got, want)
	}
}

func TestEditorSearchBarReservesRowsWithoutChangingOuterGeometry(t *testing.T) {
	tests := []struct {
		name   string
		border bool
		pane   bool
		width  int
		height int
	}{
		{name: "standalone", width: 32, height: 10},
		{name: "embedded", pane: true, width: 32, height: 10},
		{name: "bordered pane", border: true, pane: true, width: 32, height: 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var s *EditorScreen
			var sh *core.Shared
			if tc.pane {
				s, sh = newPaneEditor(EditorOpts{Title: "notes.md", Search: true, Border: tc.border})
			} else {
				s, sh = newEditor(EditorOpts{Title: "notes.md", Search: true, Border: tc.border})
			}
			s.SetSize(sh, tc.width, tc.height)
			fullH := s.h
			setSearchQuery(s, "needle")
			s.SetSize(sh, tc.width, tc.height)
			if s.h != fullH-editorSearchBarH {
				t.Fatalf("viewport height = %d, want %d", s.h, fullH-editorSearchBarH)
			}
			if got := lipgloss.Height(s.View(sh)); got != tc.height {
				t.Fatalf("editor render height = %d, assigned height %d", got, tc.height)
			}
			assertRowGeometry(t, s, "reserved search rows")
		})
	}

}

func TestEditorRetainedSearchBarLeftClickRefocuses(t *testing.T) {
	tests := []struct {
		name     string
		embedded bool
		border   bool
	}{
		{name: "standalone"},
		{name: "embedded", embedded: true},
		{name: "bordered pane", embedded: true, border: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, sh := newEditor(EditorOpts{Search: true, Border: tc.border})
			s.SetEmbedded(tc.embedded)
			s.SetPaneOrigin(7, 4)
			s.setContent("selected text\nsecond")
			selectRange(s, 0, 0, 0, 8)
			setSearchQuery(s, "second")
			s.SetSize(sh, 32, 10)
			beforeY, beforeX, beforeSelection := s.curY, s.curX, s.selectedText()

			x, y := 2, 10-editorSearchBarH+1 // pane-relative middle of the retained box
			if !tc.embedded {
				x += 7
				y += 4
			}
			_, act := s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
			if act.Msg == nil || !s.searchEditing {
				t.Fatal("left click on retained search should push the focused line edit")
			}
			if s.searchQuery != "second" {
				t.Fatalf("click replaced retained query with %q", s.searchQuery)
			}
			if s.curY != beforeY || s.curX != beforeX || s.selectedText() != beforeSelection {
				t.Fatalf("click changed document cursor/selection: cursor=(%d,%d) selection=%q", s.curY, s.curX, s.selectedText())
			}
		})
	}
}

func TestEditorRetainedSearchBarNonLeftEventsDoNotFocus(t *testing.T) {
	for _, msg := range []tea.MouseMsg{
		tea.MouseClickMsg{X: 2, Y: 18, Button: tea.MouseRight},
		tea.MouseWheelMsg{X: 2, Y: 18, Button: tea.MouseWheelDown},
		tea.MouseMotionMsg{X: 2, Y: 18, Button: tea.MouseLeft},
	} {
		s, sh := newEditor(EditorOpts{Search: true})
		setSearchQuery(s, "needle")
		s.SetSize(sh, 80, 20)
		_, act := s.Update(sh, msg)
		if s.searchEditing || act.Msg != nil {
			t.Errorf("%s unexpectedly focused retained search", msg.String())
		}
	}
}
