package components

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Keyboard selection: the shifted motions and the shift+click extend. What every test
// here really exercises is selectionAnchor, which DERIVES the fixed end from the
// selection rather than storing it — see editor_cursor.go.

func shiftKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

var (
	shiftLeftKey  = shiftKey(tea.KeyShiftLeft)
	shiftRightKey = shiftKey(tea.KeyShiftRight)
	shiftDownKey  = shiftKey(tea.KeyShiftDown)
	shiftHomeKey  = shiftKey(tea.KeyShiftHome)
	shiftEndKey   = shiftKey(tea.KeyShiftEnd)
)

// pressKeyN sends the same key n times.
func pressKeyN(s *EditorScreen, m tea.KeyMsg, n int) {
	for range n {
		s.key(nil, m)
	}
}

func TestEditorShiftRightSelectsForward(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")

	pressKeyN(s, shiftRightKey, 3)
	if got := s.selectedText(); got != "abc" {
		t.Fatalf("three shift+→ selected %q, want %q", got, "abc")
	}
	if s.selStart != (textPos{0, 0}) || s.selEnd != (textPos{0, 3}) {
		t.Fatalf("selection = %v..%v, want {0 0}..{0 3}", s.selStart, s.selEnd)
	}
	if s.curX != 3 {
		t.Fatalf("caret at %d, want the moving end 3", s.curX)
	}
}

// TestEditorShiftSelectionCollapsesOnItsAnchor: walking back onto the anchor leaves
// NOTHING selected, and the next shifted key re-anchors there — the empty-selection
// branch of selectionAnchor is what makes the second half work.
func TestEditorShiftSelectionCollapsesOnItsAnchor(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	s.curX, s.wantX = 2, 2

	pressKeyN(s, shiftRightKey, 2)
	if got := s.selectedText(); got != "cd" {
		t.Fatalf("selected %q, want %q", got, "cd")
	}
	pressKeyN(s, shiftLeftKey, 2)
	if s.selectionActive() {
		t.Fatalf("back on the anchor should select nothing, got %q", s.selectedText())
	}
	if s.curX != 2 {
		t.Fatalf("caret at %d, want back at the anchor 2", s.curX)
	}
	// Re-anchored at 2, not at 0 or wherever the cleared range pointed.
	s.key(nil, shiftRightKey)
	if got := s.selectedText(); got != "c" {
		t.Fatalf("the next shift+→ selected %q, want %q", got, "c")
	}
}

// TestEditorShiftSelectionCrossesItsAnchor: the anchor is a pivot, not a start. Going
// left then back past the starting point selects FORWARD from it, which only holds if
// the anchor stayed put while the caret sat on the range's low end.
func TestEditorShiftSelectionCrossesItsAnchor(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	s.curX, s.wantX = 3, 3

	s.key(nil, shiftLeftKey)
	if got := s.selectedText(); got != "c" {
		t.Fatalf("shift+← selected %q, want %q", got, "c")
	}
	pressKeyN(s, shiftRightKey, 3)
	if got := s.selectedText(); got != "de" {
		t.Fatalf("crossing the anchor selected %q, want %q", got, "de")
	}
	if s.selStart != (textPos{0, 3}) {
		t.Fatalf("selection starts at %v, want the untouched anchor {0 3}", s.selStart)
	}
}

// TestEditorShiftMotionExtendsMouseSelection is the payoff of deriving the anchor: a
// selection the MOUSE made has no keyboard anchor recorded anywhere, and a shifted key
// must still extend it rather than collapse it to a single character.
func TestEditorShiftMotionExtendsMouseSelection(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("foo bar baz")
	y := s.titleH()
	now := time.Now()

	s.pressSelection(sh, 5, y, now)
	s.pressSelection(sh, 5, y, now.Add(10*time.Millisecond))
	if got := s.selectedText(); got != "bar" {
		t.Fatalf("double click selected %q, want %q", got, "bar")
	}
	pressKeyN(s, shiftRightKey, 2)
	if got := s.selectedText(); got != "bar b" {
		t.Fatalf("shift+→ after a word select gave %q, want %q", got, "bar b")
	}
}

// TestEditorShiftMotionExtendsBackwardMouseSelection is the other half: after a drag
// that ran RIGHT TO LEFT the caret sits on selStart, so the anchor has to be selEnd.
func TestEditorShiftMotionExtendsBackwardMouseSelection(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdefgh")
	y := s.titleH()

	s.startDrag(sh, 6, y)
	s.extendDrag(sh, 2, y)
	if got := s.selectedText(); got != "cdefg" {
		t.Fatalf("backward drag selected %q, want %q", got, "cdefg")
	}
	s.key(nil, shiftLeftKey)
	if got := s.selectedText(); got != "bcdefg" {
		t.Fatalf("shift+← after a backward drag gave %q, want %q", got, "bcdefg")
	}
}

// TestEditorUnshiftedMoveClearsSelection pins the pre-pass the shifted motions had to be
// matched above (editor.go): an ordinary arrow still drops the selection.
func TestEditorUnshiftedMoveClearsSelection(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")

	pressKeyN(s, shiftRightKey, 3)
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft})
	if s.selectionActive() {
		t.Fatalf("a bare arrow must clear the selection, still holding %q", s.selectedText())
	}
}

// TestEditorShiftSelectionFeedsTheEditVerbs: a keyboard selection is the same value the
// mouse produces, so everything already built on it works unchanged — typing replaces it
// in ONE undo step, and a cut takes it rather than the caret's line.
func TestEditorShiftSelectionFeedsTheEditVerbs(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")

	pressKeyN(s, shiftRightKey, 3)
	typeRunes(s, 'X')
	if got := buffer(s); got != "Xdef" {
		t.Fatalf("typing over a shift-selection gave %q, want %q", got, "Xdef")
	}
	undoEditor(s)
	if got := buffer(s); got != "abcdef" {
		t.Fatalf("one undo should restore the whole replacement, got %q", got)
	}

	s.setContent("abcdef")
	pressKeyN(s, shiftRightKey, 3)
	// alt+x's clipboard write travels in the cmd lane; not running the returned Action
	// keeps the test off pbcopy while still exercising the buffer half of the cut.
	s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true})
	if got := buffer(s); got != "def" {
		t.Fatalf("alt+x over a shift-selection gave %q, want %q — the LINE was cut", got, "def")
	}
}

// TestEditorShiftSelectionSpansLines: nothing about the anchor is per-line, and the
// range carries the newline the way a dragged one does.
func TestEditorShiftSelectionSpansLines(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha\nbeta")
	s.curX, s.wantX = 3, 3

	s.key(nil, shiftDownKey)
	s.key(nil, shiftEndKey)
	if got := s.selectedText(); got != "ha\nbeta" {
		t.Fatalf("multiline selection = %q, want %q", got, "ha\nbeta")
	}
	s.key(nil, shiftHomeKey)
	if got := s.selectedText(); got != "ha\n" {
		t.Fatalf("shift+home pulled back to %q, want %q", got, "ha\n")
	}
}

// TestEditorShiftWordSelection: the word chords select over the SAME moves alt+←/→
// already make, trailing spaces and all (wordForwardPos), rather than a second notion of
// where a word ends.
func TestEditorShiftWordSelection(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("foo bar baz")

	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if got := s.selectedText(); got != "foo " {
		t.Fatalf("ctrl+shift+→ selected %q, want %q", got, "foo ")
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlShiftRight})
	if got := s.selectedText(); got != "foo bar " {
		t.Fatalf("a second ctrl+shift+→ selected %q, want %q", got, "foo bar ")
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlShiftLeft})
	if got := s.selectedText(); got != "foo " {
		t.Fatalf("ctrl+shift+← pulled back to %q, want %q", got, "foo ")
	}
	// All the way back onto the anchor at column 0 — the word chords share the one
	// anchor with the arrows, so this collapses rather than selecting backwards.
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlShiftLeft})
	if s.selectionActive() {
		t.Fatalf("back on the anchor should select nothing, got %q", s.selectedText())
	}
}

// TestEditorShiftClickExtends: the press carries the shift bit, so it extends from the
// caret instead of placing a new one. The bare press beside it is the control — that is
// the behavior the modifier has to leave alone.
func TestEditorShiftClickExtends(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdefgh")
	y := s.titleH()

	shiftPress := tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Shift: true, X: 5, Y: y}
	s.Update(sh, shiftPress)
	if got := s.selectedText(); got != "abcde" {
		t.Fatalf("shift+click from column 0 selected %q, want %q", got, "abcde")
	}
	// A second one re-aims the same anchor rather than starting over.
	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Shift: true, X: 2, Y: y})
	if got := s.selectedText(); got != "ab" {
		t.Fatalf("the second shift+click selected %q, want %q", got, "ab")
	}

	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 4, Y: y})
	if s.selectionActive() {
		t.Fatalf("an unmodified press is still a bare caret, got %q", s.selectedText())
	}
	if s.curX != 4 {
		t.Fatalf("the bare press put the caret at %d, want 4", s.curX)
	}
}

// TestEditorShiftClickThenDrag: the extend leaves a gesture running from the SAME
// anchor, so holding and moving keeps widening the range the click opened.
func TestEditorShiftClickThenDrag(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdefgh")
	y := s.titleH()
	s.curX, s.wantX = 2, 2

	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Shift: true, X: 4, Y: y})
	if got := s.selectedText(); got != "cd" {
		t.Fatalf("shift+click selected %q, want %q", got, "cd")
	}
	if !s.dragging {
		t.Fatal("the extend should leave a drag running so the pointer can keep widening it")
	}
	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 6, Y: y})
	if got := s.selectedText(); got != "cdefg" {
		t.Fatalf("dragging on from the extend gave %q, want %q", got, "cdefg")
	}
}
