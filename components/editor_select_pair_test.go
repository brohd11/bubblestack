package components

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestEditorWordBoundsAt(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		col      int
		from, to int
	}{
		{"word", "foo.bar(baz_qux)", 1, 0, 3},
		{"dot alone", "foo.bar(baz_qux)", 3, 3, 4},
		{"paren alone", "foo.bar(baz_qux)", 7, 7, 8},
		{"underscore joins", "foo.bar(baz_qux)", 10, 8, 15},
		{"closing paren", "foo.bar(baz_qux)", 15, 15, 16},
		{"space run", "ab  cd", 2, 2, 4},
		{"digits are word chars", "v2 x", 1, 0, 2},
		{"past end takes last run", "abc", 3, 0, 3},
		{"empty line", "", 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, to := wordBoundsAt([]rune(tc.line), tc.col)
			if from != tc.from || to != tc.to {
				t.Fatalf("wordBoundsAt(%q, %d) = %d,%d, want %d,%d", tc.line, tc.col, from, to, tc.from, tc.to)
			}
		})
	}
}

func TestEditorDoubleClickSelectsWord(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo.bar baz")
	y := s.titleH()
	now := time.Now()

	s.pressSelection(sh, 5, y, now)
	if s.selectionActive() {
		t.Fatal("a single press should not select")
	}
	s.pressSelection(sh, 5, y, now.Add(10*time.Millisecond))
	text := s.selectedText()
	if text != "bar" {
		t.Fatalf("double click selected %q, want %q", text, "bar")
	}
	if s.selStart != (textPos{0, 4}) || s.selEnd != (textPos{0, 7}) {
		t.Fatalf("selection = %v..%v, want {0 4}..{0 7}", s.selStart, s.selEnd)
	}
	if s.curX != 7 {
		t.Fatalf("caret at %d, want the selection end 7", s.curX)
	}
	if s.dragging {
		t.Fatal("a multi-click ends the gesture: dragging must be off so the release cannot clear it")
	}
}

func TestEditorSlowSecondClickStaysCaret(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar")
	y := s.titleH()
	now := time.Now()

	s.pressSelection(sh, 1, y, now)
	s.pressSelection(sh, 1, y, now.Add(2*time.Second))
	if s.selectionActive() {
		t.Fatal("two presses two seconds apart are two caret clicks, not a double click")
	}
}

func TestEditorSecondClickElsewhereStaysCaret(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar")
	y := s.titleH()
	now := time.Now()

	s.pressSelection(sh, 1, y, now)
	s.pressSelection(sh, 5, y, now.Add(10*time.Millisecond))
	if s.selectionActive() {
		t.Fatal("a fast press on a different cell is a fresh first click")
	}
}

func TestEditorLeftDoubleClickSurvivesReleaseWithoutCopy(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar")
	oldWrite := writeEditorClipboard
	defer func() { writeEditorClipboard = oldWrite }()
	var copied string
	writeEditorClipboard = func(text string) error { copied = text; return nil }
	y := s.titleH()

	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 1, Y: y}
	s.Update(sh, down)
	_, act := s.Update(sh, down)
	if act.Cmd != nil || copied != "" {
		t.Fatal("a left double click should select without copying")
	}
	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonNone, X: 1, Y: y})
	if !s.selectionActive() || s.selectedText() != "foo" {
		t.Fatalf("the release cleared the word selection: active=%v text=%q", s.selectionActive(), s.selectedText())
	}
}

// TestEditorRightPressNeverMultiSelects: right presses are one-shot menu gestures, so
// repeating one on the same cell must never promote to the word/line selection a repeated
// LEFT press does.
func TestEditorRightPressNeverMultiSelects(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("foo bar\nnext")
	y := s.titleH()
	down := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: 1, Y: y}

	for i := 0; i < 3; i++ {
		if _, act := s.Update(sh, down); act.Msg == nil {
			t.Fatalf("right press %d should open the menu", i+1)
		}
		if s.selectionActive() {
			t.Fatalf("right press %d selected %q; repeated right presses must not promote",
				i+1, s.selectedText())
		}
	}
}

func TestEditorTripleClickSelectsLine(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("alpha\nbeta\ngamma")
	now := time.Now()
	y := s.titleH() + 1 // "beta"

	s.pressSelection(sh, 1, y, now)
	s.pressSelection(sh, 1, y, now.Add(10*time.Millisecond))
	s.pressSelection(sh, 1, y, now.Add(20*time.Millisecond))
	text := s.selectedText()
	if text != "beta\n" {
		t.Fatalf("triple click selected %q, want the line and its newline", text)
	}
	if s.selStart != (textPos{1, 0}) || s.selEnd != (textPos{2, 0}) {
		t.Fatalf("selection = %v..%v, want {1 0}..{2 0}", s.selStart, s.selEnd)
	}

	// The last line has no newline to take.
	s.clickCount = 0
	last := s.titleH() + 2
	s.pressSelection(sh, 1, last, now)
	s.pressSelection(sh, 1, last, now.Add(10*time.Millisecond))
	s.pressSelection(sh, 1, last, now.Add(20*time.Millisecond))
	if text := s.selectedText(); text != "gamma" {
		t.Fatalf("triple click on the last line selected %q, want %q", text, "gamma")
	}
	if s.selEnd != (textPos{2, 5}) {
		t.Fatalf("last-line selection ends at %v, want {2 5}", s.selEnd)
	}
}

func TestEditorFourthClickStartsOver(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar")
	y := s.titleH()
	now := time.Now()

	for i := 0; i < 3; i++ {
		s.pressSelection(sh, 1, y, now.Add(time.Duration(i)*10*time.Millisecond))
	}
	s.pressSelection(sh, 1, y, now.Add(30*time.Millisecond))
	if s.selectionActive() || s.clickCount != 1 {
		t.Fatalf("fourth click: active=%v count=%d, want a fresh first click", s.selectionActive(), s.clickCount)
	}
}

func TestEditorTypingBreaksTheClickRun(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar")
	y := s.titleH()
	now := time.Now()

	s.pressSelection(sh, 1, y, now)
	typeRunes(s, 'x')
	s.pressSelection(sh, 1, y, now.Add(10*time.Millisecond))
	if s.selectionActive() {
		t.Fatal("typing between two clicks must break the multi-click run")
	}
}

func TestEditorSurroundSelectionNests(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	typeRunes(s, '*')
	if got := buffer(s); got != "a*bcd*ef" {
		t.Fatalf("buffer = %q, want %q", got, "a*bcd*ef")
	}
	if got := s.selectedText(); got != "bcd" {
		t.Fatalf("selection after wrapping = %q, want the original text %q", got, "bcd")
	}

	typeRunes(s, '*')
	if got := buffer(s); got != "a**bcd**ef" {
		t.Fatalf("second wrap = %q, want %q", got, "a**bcd**ef")
	}
	if got := s.selectedText(); got != "bcd" {
		t.Fatalf("selection after the second wrap = %q, want %q", got, "bcd")
	}
	if s.curX != s.selEnd.x || s.curY != s.selEnd.y {
		t.Fatalf("caret at %d,%d, want the selection end %v", s.curY, s.curX, s.selEnd)
	}
}

func TestEditorSurroundIsOneUndoStepEach(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	typeRunes(s, '(')
	typeRunes(s, '[')
	if got := buffer(s); got != "a([bcd])ef" {
		t.Fatalf("buffer = %q, want %q", got, "a([bcd])ef")
	}
	if len(s.undoStack) != 2 {
		t.Fatalf("undo stack has %d entries, want one per wrap", len(s.undoStack))
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := buffer(s); got != "a(bcd)ef" {
		t.Fatalf("after undo = %q, want one wrap removed: %q", got, "a(bcd)ef")
	}
}

func TestEditorSurroundMultilineSelection(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abc\ndef")
	selectRange(s, 0, 1, 1, 2)

	typeRunes(s, '(')
	if got := buffer(s); got != "a(bc\nde)f" {
		t.Fatalf("buffer = %q, want %q", got, "a(bc\nde)f")
	}
	if got := s.selectedText(); got != "bc\nde" {
		t.Fatalf("selection = %q, want the original text %q", got, "bc\nde")
	}
}

func TestEditorSurroundWrapsTextNotTheLineBreak(t *testing.T) {
	// A triple click selects the line's newline too; the closer belongs at the end of
	// the text, not at the head of the following line.
	s, _ := newEditor(EditorOpts{})
	s.setContent("abc\ndef")
	selectRange(s, 0, 0, 1, 0)

	typeRunes(s, '(')
	if got := buffer(s); got != "(abc)\ndef" {
		t.Fatalf("buffer = %q, want %q", got, "(abc)\ndef")
	}
	if got := s.selectedText(); got != "abc" {
		t.Fatalf("selection = %q, want the wrapped text %q", got, "abc")
	}
	typeRunes(s, '(')
	if got := buffer(s); got != "((abc))\ndef" {
		t.Fatalf("second wrap = %q, want %q", got, "((abc))\ndef")
	}
}

func TestEditorSurroundWorksInEveryFileType(t *testing.T) {
	// '*' does not auto-close in a .go buffer, but wrapping a selection in it is a
	// deliberate gesture and stays available.
	s, _ := newEditor(EditorOpts{Path: "x.go"})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	typeRunes(s, '*')
	if got := buffer(s); got != "a*bcd*ef" {
		t.Fatalf("buffer = %q, want %q", got, "a*bcd*ef")
	}
}

func TestEditorAutoPair(t *testing.T) {
	tests := []struct {
		name string
		path string
		r    rune
		want string
		curX int
	}{
		{"paren in code", "x.go", '(', "()", 1},
		{"bracket in code", "x.go", '[', "[]", 1},
		{"brace in code", "x.go", '{', "{}", 1},
		{"star in code stays bare", "x.go", '*', "*", 1},
		{"underscore in code stays bare", "x.go", '_', "_", 1},
		{"star in markdown", "x.md", '*', "**", 1},
		{"underscore in markdown", "x.md", '_', "__", 1},
		{"paren in markdown", "x.md", '(', "()", 1},
		{"star in yaml stays bare", "x.yaml", '*', "*", 1},
		{"unpaired rune", "x.go", 'a', "a", 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{Path: tc.path})
			typeRunes(s, tc.r)
			if got := buffer(s); got != tc.want {
				t.Fatalf("typing %q into %s gave %q, want %q", tc.r, tc.path, got, tc.want)
			}
			if s.curX != tc.curX {
				t.Fatalf("caret at %d, want %d (between the pair)", s.curX, tc.curX)
			}
			if s.wantX != s.curX {
				t.Fatalf("wantX %d out of sync with curX %d", s.wantX, s.curX)
			}
		})
	}
}

func TestEditorAutoPairLeavesPasteAlone(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "x.go"})
	s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("f(a, b)")})
	if got := buffer(s); got != "f(a, b)" {
		t.Fatalf("pasted %q, want it verbatim with no added closers", got)
	}
}

func TestEditorAutoPairIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "x.go"})
	typeRunes(s, '(')
	if len(s.undoStack) != 1 {
		t.Fatalf("undo stack has %d entries, want 1 for the pair", len(s.undoStack))
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlZ})
	if got := buffer(s); got != "" {
		t.Fatalf("undo left %q, want the pair fully removed", got)
	}
}

func TestEditorSaveAsPicksUpEmphasisPairs(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "notes.txt"})
	if s.emphasisPairs {
		t.Fatal(".txt should not auto-close emphasis")
	}
	s.applySaveName("notes.md")
	if !s.emphasisPairs {
		t.Fatal("a save-as to .md should start auto-closing emphasis")
	}
}
