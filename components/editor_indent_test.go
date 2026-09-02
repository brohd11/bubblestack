package components

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Block indentation. The gesture that carries it is BARE tab: shift+tab is
// core.Keys.PaneNext and keeps its old meaning, which TestEditorShiftTabNeverIndentsBlock
// pins from this side.

var (
	tabKey        = keyMsg("tab")
	shiftTabKey   = keyMsg("shift+tab")
	dedentKey     = keyMsg("alt+,")
	indentKey     = keyMsg("alt+.")
	indentModeKey = keyMsg("alt+i")
)

func TestEditorTabIndentsSelectedLines(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\ntwo\nthree\nfour")
	selectRange(s, 0, 1, 2, 2)
	s.key(nil, tabKey)
	if got, want := buffer(s), "\tone\n\ttwo\n\tthree\nfour"; got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
	// The columns rode along with their own line, so the same characters stay selected
	// and the key repeats to nest.
	if got, want := s.selectedText(), "ne\n\ttwo\n\tth"; got != want {
		t.Fatalf("selection after indent = %q, want %q", got, want)
	}
	s.key(nil, tabKey)
	if got, want := buffer(s), "\t\tone\n\t\ttwo\n\t\tthree\nfour"; got != want {
		t.Fatalf("second indent = %q, want %q", got, want)
	}
	if s.curY != 2 || s.curX != 4 || s.wantX != 4 {
		t.Fatalf("caret = %d,%d (wantX %d); want 2,4", s.curY, s.curX, s.wantX)
	}
}

// A shift+down / triple-click selection ends at column 0 of the line AFTER the last one
// it covers: it took the newline, not the line. That trailing line must not be indented.
func TestEditorIndentStopsAtFullLineSelectionEnd(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\ntwo\nthree")
	selectRange(s, 0, 0, 2, 0)
	s.key(nil, tabKey)
	if got, want := buffer(s), "\tone\n\ttwo\nthree"; got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
	// The end sits on an untouched line, so it stayed exactly where it was.
	if s.selStart != (textPos{0, 0}) || s.selEnd != (textPos{2, 0}) {
		t.Fatalf("selection = %v..%v, want {0 0}..{2 0}", s.selStart, s.selEnd)
	}
}

// The pins for what tab did before: within one line it still REPLACES the selection,
// and with no selection it still inserts one literal tab.
func TestEditorTabStillTypesWithinOneLine(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha beta")
	selectRange(s, 0, 0, 0, 5)
	s.key(nil, tabKey)
	if got, want := buffer(s), "\t beta"; got != want {
		t.Fatalf("single-line selection + tab = %q, want %q", got, want)
	}
	if s.selectionActive() {
		t.Fatal("selection survived a replacing tab")
	}
}

func TestEditorShiftTabNeverIndentsBlock(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\ntwo\nthree")
	selectRange(s, 0, 0, 2, 5)
	s.key(nil, shiftTabKey)
	// The navigation key gained no editing meaning: standalone it is still the plain
	// insert-a-tab alias, so it replaced the selection rather than indenting it.
	if got, want := buffer(s), "\t"; got != want {
		t.Fatalf("shift+tab = %q, want %q", got, want)
	}
}

func TestEditorDedentTakesTabsThenSpaces(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("\tone\n    two\n  three\nfour")
	selectRange(s, 0, 0, 3, 4)
	s.key(nil, dedentKey)
	// A tab goes whole; spaces go up to one unit's worth (4, the tab-mode width) and a
	// line with fewer gives what it has; a flush-left line is left alone.
	if got, want := buffer(s), "one\ntwo\nthree\nfour"; got != want {
		t.Fatalf("dedent = %q, want %q", got, want)
	}
	s.key(nil, dedentKey)
	if got, want := buffer(s), "one\ntwo\nthree\nfour"; got != want {
		t.Fatalf("dedent at column 0 = %q, want it unchanged", got)
	}
}

func TestEditorDedentClampsSelectionStart(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("\tone\n\ttwo")
	selectRange(s, 0, 0, 1, 4)
	s.key(nil, dedentKey)
	if s.selStart.x != 0 {
		t.Fatalf("selStart.x = %d, want 0 (clamped, not negative)", s.selStart.x)
	}
	if s.selEnd != (textPos{1, 3}) {
		t.Fatalf("selEnd = %v, want {1 3}", s.selEnd)
	}
}

// With no selection both alt chords act on the caret's own line, which is what makes
// them useful outside a block gesture.
func TestEditorIndentChordsActOnCaretLine(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\ntwo")
	s.curY, s.curX, s.wantX = 1, 2, 2
	s.key(nil, indentKey)
	if got, want := buffer(s), "one\n\ttwo"; got != want {
		t.Fatalf("alt+. = %q, want %q", got, want)
	}
	if s.curX != 3 || s.wantX != 3 {
		t.Fatalf("caret x = %d (wantX %d), want 3", s.curX, s.wantX)
	}
	s.key(nil, dedentKey)
	if got, want := buffer(s), "one\ntwo"; got != want {
		t.Fatalf("alt+, = %q, want %q", got, want)
	}
}

func TestEditorIndentSkipsEmptyLines(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\n\ntwo")
	selectRange(s, 0, 0, 2, 3)
	s.key(nil, tabKey)
	// An indented blank line would be nothing but trailing whitespace.
	if got, want := buffer(s), "\tone\n\n\ttwo"; got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
	s.key(nil, dedentKey)
	if got, want := buffer(s), "one\n\ntwo"; got != want {
		t.Fatalf("dedent = %q, want %q", got, want)
	}
}

func TestEditorBlockIndentIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("one\ntwo\nthree")
	selectRange(s, 0, 1, 2, 2)
	s.key(nil, tabKey)
	if len(s.undoStack) != 1 {
		t.Fatalf("history = %d steps, want 1 for the whole block", len(s.undoStack))
	}
	undoEditor(s)
	if got, want := buffer(s), "one\ntwo\nthree"; got != want {
		t.Fatalf("after undo = %q, want %q", got, want)
	}
	// The history entry carries the selection, so the block is still selected to retry.
	if s.selStart != (textPos{0, 1}) || s.selEnd != (textPos{2, 2}) {
		t.Fatalf("selection after undo = %v..%v, want {0 1}..{2 2}", s.selStart, s.selEnd)
	}
}

func TestEditorAutoIndentFollowsExtension(t *testing.T) {
	resolver := func(path string) *EditorLanguageConfig {
		if strings.HasSuffix(path, ".soft") {
			return &EditorLanguageConfig{IndentSpaces: 2}
		}
		return nil
	}
	yaml, _ := newEditor(EditorOpts{Path: "conf.soft", ResolveLanguage: resolver})
	yaml.setContent("a\nb")
	selectRange(yaml, 0, 0, 1, 1)
	yaml.key(nil, tabKey)
	if got, want := buffer(yaml), "  a\n  b"; got != want {
		t.Fatalf("yaml indent = %q, want %q", got, want)
	}

	txt, _ := newEditor(EditorOpts{Path: "notes.txt"})
	txt.setContent("a\nb")
	selectRange(txt, 0, 0, 1, 1)
	txt.key(nil, tabKey)
	if got, want := buffer(txt), "\ta\n\tb"; got != want {
		t.Fatalf("txt indent = %q, want %q", got, want)
	}
}

// A save-as re-picks the unit from the new name's resolved language.
func TestEditorSaveAsRepicksIndentUnit(t *testing.T) {
	resolver := func(path string) *EditorLanguageConfig {
		if strings.HasSuffix(path, ".soft") {
			return &EditorLanguageConfig{IndentSpaces: 2}
		}
		return nil
	}
	s, _ := newEditor(EditorOpts{Path: "notes.txt", ResolveLanguage: resolver})
	s.applySaveName("conf.soft")
	s.setContent("a\nb")
	selectRange(s, 0, 0, 1, 1)
	s.key(nil, tabKey)
	if got, want := buffer(s), "  a\n  b"; got != want {
		t.Fatalf("indent after save-as = %q, want %q", got, want)
	}
}

// An explicit width is an override and outlives the rename, as an explicit Highlighter does.
func TestEditorExplicitIndentWidthSurvivesRename(t *testing.T) {
	resolver := func(string) *EditorLanguageConfig { return &EditorLanguageConfig{IndentSpaces: 2} }
	s, _ := newEditor(EditorOpts{Path: "notes.txt", ResolveLanguage: resolver, Indent: IndentSpaces, IndentWidth: 3})
	s.applySaveName("conf.soft")
	s.setContent("a\nb")
	selectRange(s, 0, 0, 1, 1)
	s.key(nil, tabKey)
	if got, want := buffer(s), "   a\n   b"; got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
}

func TestEditorCycleIndentMode(t *testing.T) {
	resolver := func(string) *EditorLanguageConfig { return &EditorLanguageConfig{IndentSpaces: 2} }
	s, _ := newEditor(EditorOpts{Path: "conf.soft", ResolveLanguage: resolver})
	s.setContent("a\nb")
	if got, want := s.indentLabel(), "auto (2 spaces)"; got != want {
		t.Fatalf("initial label = %q, want %q", got, want)
	}
	_, act := s.key(nil, indentModeKey)
	if act.Msg == nil {
		t.Fatal("alt+i returned no action; want the status line")
	}
	if got := fmt.Sprint(act.Msg); !strings.Contains(got, "indent: tab") {
		t.Fatalf("status = %q, want it to name the new mode", got)
	}
	if got, want := s.indentLabel(), "tab"; got != want {
		t.Fatalf("after one cycle = %q, want %q", got, want)
	}
	// Auto said two spaces, so the spaces step inherits that width.
	s.key(nil, indentModeKey)
	if got, want := s.indentLabel(), "2 spaces"; got != want {
		t.Fatalf("after two cycles = %q, want %q", got, want)
	}
	s.key(nil, indentModeKey)
	if got, want := s.indentLabel(), "auto (2 spaces)"; got != want {
		t.Fatalf("after three cycles = %q, want %q", got, want)
	}
	if s.dirty || len(s.undoStack) != 0 {
		t.Fatalf("cycling touched the buffer: dirty=%v history=%d", s.dirty, len(s.undoStack))
	}
}

// Under IndentTab the gesture types a tab even when the resolved profile asks for spaces.
func TestEditorIndentTabModeOverridesProfile(t *testing.T) {
	resolver := func(string) *EditorLanguageConfig { return &EditorLanguageConfig{IndentSpaces: 2} }
	s, _ := newEditor(EditorOpts{Path: "conf.soft", ResolveLanguage: resolver, Indent: IndentTab})
	s.setContent("a\nb")
	selectRange(s, 0, 0, 1, 1)
	s.key(nil, tabKey)
	if got, want := buffer(s), "\ta\n\tb"; got != want {
		t.Fatalf("indent = %q, want %q", got, want)
	}
}

// The help bar must not advertise the pane-navigation key as an indent chord.
func TestEditorHelpDropsShiftTabFromIndent(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	for _, b := range s.HelpBindings() {
		if strings.Contains(b.Help().Desc, "indent") {
			for _, k := range b.Keys() {
				if k == "shift+tab" {
					t.Fatal("shift+tab is listed as an indent chord")
				}
			}
		}
	}
}

// The chords are matched as raw strings, so pin the strings bubbletea produces for
// them, and that none of them is one of the three the framework cannot deliver:
// ctrl+tab has no keycode at all in bubbletea v1 (0x09 is "tab", which IS ctrl+i),
// ctrl+[ is the byte esc arrives as, and alt+[ is swallowed whole by Run's
// mouseSGRFragmentFilter.
func TestEditorIndentChordKeycodes(t *testing.T) {
	for _, tc := range []struct {
		msg  tea.KeyPressMsg
		want string
	}{
		{tabKey, "tab"},
		{dedentKey, "alt+,"},
		{indentKey, "alt+."},
		{indentModeKey, "alt+i"},
	} {
		if got := tc.msg.String(); got != tc.want {
			t.Fatalf("keycode = %q, want %q", got, tc.want)
		}
	}
}
