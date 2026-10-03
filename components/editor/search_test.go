package editor

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func editorSearchKey(s *Screen, sh *core.Shared, k string) core.Action {
	_, act := s.key(sh, keyMsg(k))
	return act
}

func lineEditKey(s *components.LineEditScreen, sh *core.Shared, msg tea.KeyPressMsg) core.Action {
	_, act := s.Update(sh, msg)
	return act
}

func setSearchQuery(s *Screen, query string) {
	s.searchQuery = query
	s.searchSeq = -1
}

func TestEditorSearchInteraction(t *testing.T) {
	s, sh := newEditor(Opts{Title: "notes.md", Search: true})
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
	if cmd := edit.Init(sh); cmd != nil || edit.CursorBlink() {
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
	s, sh := newEditor(Opts{Search: true})
	s.setContent("alpha beta alpha")
	setSearchQuery(s, "before")
	selectRange(s, 0, 6, 0, 10)

	edit := s.searchEdit(sh)
	if got := edit.Value(); got != "beta" {
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
	s, sh := newEditor(Opts{Search: true})
	s.setContent("alpha\nbeta")
	setSearchQuery(s, "prior")
	selectRange(s, 0, 2, 1, 2)

	edit := s.searchEdit(sh)
	if got := edit.Value(); got != "prior" {
		t.Fatalf("multiline seed = %q, want prior query unchanged", got)
	}
	if s.searchQuery != "prior" {
		t.Fatalf("live query = %q, want prior", s.searchQuery)
	}
}

func TestEditorSearchIsOptIn(t *testing.T) {
	s, sh := newEditor(Opts{})
	if act := editorSearchKey(s, sh, "ctrl+f"); act.Msg != nil || act.Cmd != nil {
		t.Fatal("ctrl+f must remain inert when search is disabled")
	}
	for _, binding := range s.HelpBindings() {
		if strings.Contains(strings.Join(binding.Keys(), " "), "ctrl+f") {
			t.Fatal("disabled search must not advertise ctrl+f")
		}
	}

	// alt+f is the terminal's escape-prefixed alt+right: it belongs to word-forward, and
	// enabling search must not take it back. The cursor moving is the whole assertion.
	enabled, _ := newEditor(Opts{Search: true})
	enabled.setContent("alpha beta")
	if act := editorSearchKey(enabled, sh, "alt+f"); act.Msg != nil || act.Cmd != nil {
		t.Fatal("alt+f must not invoke search; it is the editor's word-forward motion")
	}
	if enabled.curX == 0 {
		t.Fatal("alt+f should have moved the cursor forward by a word")
	}
	var advertised bool
	for _, binding := range enabled.HelpBindings() {
		advertised = advertised || strings.Contains(strings.Join(binding.Keys(), " "), "ctrl+f")
	}
	if !advertised {
		t.Fatal("enabled search should advertise ctrl+f")
	}
}

func TestEditorSearchUnicodeTabsAndCacheRefresh(t *testing.T) {
	s, _ := newEditor(Opts{Search: true})
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
	s, _ := newEditor(Opts{Search: true, Highlighter: &countingHL{}})
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
			s, _ := newEditor(Opts{Search: true})
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
	if got := components.LineEditBox().GetBorderTopForeground(); !reflect.DeepEqual(got, core.FocusedColor) {
		t.Fatalf("focused line edit border = %v, want theme accent %v", got, core.FocusedColor)
	}
	if got := retainedSearchBox().GetBorderTopForeground(); !reflect.DeepEqual(got, core.BorderColor) {
		t.Fatalf("retained search border = %v, want standard border %v", got, core.BorderColor)
	}
}

func TestEditorSearchBarStaysWithinNarrowPaneAndAnchorsBottom(t *testing.T) {
	s, _ := newEditor(Opts{Title: "a-very-long-document-name.md", Search: true})
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
	if x, y, _ := edit.Anchor(); x != 7 || y != 4+6-editorSearchBarH {
		t.Fatalf("search overlay anchor = (%d,%d), want (7,%d)", x, y, 4+6-editorSearchBarH)
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
			var s *Screen
			var sh *core.Shared
			if tc.pane {
				s, sh = newPaneEditor(Opts{Title: "notes.md", Search: true, Border: tc.border})
			} else {
				s, sh = newEditor(Opts{Title: "notes.md", Search: true, Border: tc.border})
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
			s, sh := newEditor(Opts{Search: true, Border: tc.border})
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
		s, sh := newEditor(Opts{Search: true})
		setSearchQuery(s, "needle")
		s.SetSize(sh, 80, 20)
		_, act := s.Update(sh, msg)
		if s.searchEditing || act.Msg != nil {
			t.Errorf("%s unexpectedly focused retained search", msg.String())
		}
	}
}

// TestEditorSearchStepAndCount: up/down in the open field select the previous/next match
// from the caret, wrapping both ways, without touching the query; the trailer counts.
func TestEditorSearchStepAndCount(t *testing.T) {
	s, sh := newEditor(Opts{Search: true})
	s.setContent("one foo\nfoo foo\nbar")
	s.SetSize(sh, 60, 12)
	edit := s.searchEdit(sh)
	lineEditKey(edit, sh, keyMsg("foo"))
	s.curY, s.curX = 0, 0
	if got := ansi.Strip(s.searchTrailer()); got != "3 matches  ↑ ↓" {
		t.Fatalf("trailer = %q", got)
	}

	want := []struct{ row, col int }{{0, 4}, {1, 0}, {1, 4}, {0, 4}}
	for i, w := range want {
		lineEditKey(edit, sh, keyMsg("down"))
		if s.curY != w.row || s.curX != w.col || s.selectedText() != "foo" {
			t.Fatalf("down #%d: caret (%d,%d) selection %q, want (%d,%d) foo", i+1, s.curY, s.curX, s.selectedText(), w.row, w.col)
		}
	}
	if got := ansi.Strip(s.searchTrailer()); got != "1/3  ↑ ↓" {
		t.Fatalf("trailer on first match = %q", got)
	}
	lineEditKey(edit, sh, keyMsg("up"))
	if s.curY != 1 || s.curX != 4 {
		t.Fatalf("up from the first match should wrap to the last, caret (%d,%d)", s.curY, s.curX)
	}
	if edit.Value() != "foo" || s.searchQuery != "foo" {
		t.Fatalf("arrows changed the query: field %q, query %q", edit.Value(), s.searchQuery)
	}

	setSearchQuery(s, "zzz")
	if got := ansi.Strip(s.searchTrailer()); got != "no matches  ↑ ↓" {
		t.Fatalf("trailer = %q", got)
	}
	s.searchStep(1) // nothing to select
}

// TestEditorRetainedSearchBarButtons: ↓/↑ on the retained bar step through matches and
// keep the bar unfocused; the query side still reopens the field.
func TestEditorRetainedSearchBarButtons(t *testing.T) {
	s, sh := newEditor(Opts{Search: true})
	s.setContent("foo\nfoo")
	setSearchQuery(s, "foo")
	s.SetPaneOrigin(0, 0)
	s.SetSize(sh, 40, 10)
	if got := ansi.Strip(strings.Split(s.searchBar(), "\n")[1]); !strings.HasSuffix(strings.TrimRight(got, " │"), "2 matches  ↑ ↓") {
		t.Fatalf("retained bar row %q", got)
	}
	y := 10 - editorSearchBarH + 1
	right := 2 + 40 - 4 // content's right edge
	click := func(x int) core.Action {
		_, act := s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
		return act
	}
	if act := click(right - 1); act.Msg != nil || s.searchEditing {
		t.Fatal("↓ should not open the search field")
	}
	if s.curY != 1 || s.selectedText() != "foo" {
		t.Fatalf("↓ from caret (0,0) should select row 1, caret (%d,%d)", s.curY, s.curX)
	}
	click(right - 3)
	if s.curY != 0 || s.selectedText() != "foo" {
		t.Fatalf("↑ should select row 0, caret (%d,%d)", s.curY, s.curX)
	}
	if act := click(4); act.Msg == nil || !s.searchEditing {
		t.Fatal("a click on the query should reopen the field")
	}
}

// TestEditorSearchOverlayTrailerClick drives the focused field's ↓ button.
func TestEditorSearchOverlayTrailerClick(t *testing.T) {
	s, sh := newEditor(Opts{Search: true})
	s.setContent("a foo")
	s.SetPaneOrigin(0, 0)
	s.SetSize(sh, 40, 10)
	edit := s.searchEdit(sh)
	edit.SetSize(sh, 80, 24)
	lineEditKey(edit, sh, keyMsg("foo"))
	edit.View(sh)
	x, y, w := edit.Anchor()
	edit.Update(sh, tea.MouseClickMsg{X: x + w - 3, Y: y + 1, Button: tea.MouseLeft})
	if s.selectedText() != "foo" {
		t.Fatalf("overlay ↓ click selected %q", s.selectedText())
	}
}
