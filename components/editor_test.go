package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// newEditor builds a sized screen over a shared with no chrome (BodyY 0, so mouse
// tests work in body-relative rows directly).
func newEditor(opts EditorOpts) (*EditorScreen, *core.Shared) {
	s := NewEditorScreen(opts)
	sh := core.NewShared(nil)
	s.SetSize(sh, 80, 20)
	return s, sh
}

func typeRunes(s *EditorScreen, rs ...rune) {
	s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: rs})
}

func buffer(s *EditorScreen) string {
	var b strings.Builder
	for i, l := range s.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	return b.String()
}

// TestEditorTyping: printable runes land in the buffer — including letters the router
// would otherwise treat as global shortcuts (q/j/k), which is why Filtering is always
// true — and edits mark the buffer dirty.
func TestEditorTyping(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, 'q', 'j', 'k')
	if got := buffer(s); got != "qjk" {
		t.Fatalf("buffer = %q, want %q", got, "qjk")
	}
	if !s.dirty {
		t.Fatal("typing must mark the buffer dirty")
	}
}

// TestEditorEnterAndShiftTab: enter splits the line at the cursor; shift+tab inserts
// a literal tab (tab itself stays reserved for navigation).
func TestEditorEnterAndShiftTab(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, 'a', 'b', 'c')
	s.key(nil, tea.KeyMsg{Type: tea.KeyEnter})
	typeRunes(s, 'd')
	if got := buffer(s); got != "abc\nd" {
		t.Fatalf("buffer = %q, want %q", got, "abc\nd")
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := buffer(s); got != "abc\nd\t" {
		t.Fatalf("buffer = %q, want tab inserted, got %q", "abc\nd\t", got)
	}
	// A bare tab inserts nothing.
	s.key(nil, tea.KeyMsg{Type: tea.KeyTab})
	if got := buffer(s); got != "abc\nd\t" {
		t.Fatalf("bare tab must not type, buffer = %q", got)
	}
}

// TestEditorTabsNeverRenderRaw is the screen-corruption regression: a buffer holding
// tabs must render with them expanded — a raw '\t' in the frame expands at the
// terminal while the renderer measures it zero-width, wrapping the padded line and
// shifting every later frame.
func TestEditorTabsNeverRenderRaw(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("a\tb\n\tc")
	if v := s.View(sh); strings.Contains(v, "\t") {
		t.Fatal("View must never emit a raw tab")
	}
	// The tab occupies editorTabWidth display cells in the render.
	if !strings.Contains(s.renderLine(0), "a"+strings.Repeat(" ", editorTabWidth)+"b") {
		t.Fatalf("renderLine should expand the tab to %d spaces, got %q", editorTabWidth, s.renderLine(0))
	}
}

// TestEditorCellMapping: cursor cell and click column account for tab expansion —
// curX counts runes, scrX/clicks count display cells.
func TestEditorCellMapping(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("\tab") // display: 4 spaces + 'a' + 'b'
	s.curX = 2           // after 'a': cell 4+1 = 5
	if got := cellOfCol(s.lines[0], s.curX); got != 5 {
		t.Fatalf("cellOfCol = %d, want 5", got)
	}
	if got := colAtCell(s.lines[0], 5); got != 2 { // cell 5 is 'b' (rune 2)
		t.Fatalf("colAtCell(5) = %d, want 2", got)
	}
	if got := colAtCell(s.lines[0], 2); got != 0 { // inside the tab's expansion: the tab itself
		t.Fatalf("colAtCell(2) = %d, want 0", got)
	}
	// A click 4 cells in lands the cursor on 'a'.
	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 4, Y: sh.BodyY() + s.titleH()})
	if s.curX != 1 {
		t.Fatalf("click past a tab: curX = %d, want 1", s.curX)
	}
}

// TestEditorBackspace: backspace deletes the rune before the cursor, and at column 0
// joins the line onto the previous one.
func TestEditorBackspace(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("ab\ncd")
	s.curY, s.curX = 1, 0
	s.key(nil, tea.KeyMsg{Type: tea.KeyBackspace}) // join
	if got := buffer(s); got != "abcd" {
		t.Fatalf("join: buffer = %q, want %q", got, "abcd")
	}
	if s.curY != 0 || s.curX != 2 {
		t.Fatalf("cursor after join = (%d,%d), want (0,2)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyBackspace}) // delete 'b'
	if got := buffer(s); got != "acd" {
		t.Fatalf("delete: buffer = %q, want %q", got, "acd")
	}
}

// TestEditorArrows: vertical moves clamp to each line's length but return to the
// target column on longer lines (wantX), and horizontal moves wrap across line ends.
func TestEditorArrows(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcd\nx\nabc")
	s.curY, s.curX, s.wantX = 0, 3, 3

	s.key(nil, tea.KeyMsg{Type: tea.KeyDown}) // onto "x": clamp 3 → 1
	if s.curY != 1 || s.curX != 1 {
		t.Fatalf("down onto short line = (%d,%d), want (1,1)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyDown}) // onto "abc": back to target column 3
	if s.curY != 2 || s.curX != 3 {
		t.Fatalf("down restores target column = (%d,%d), want (2,3)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyRight}) // end of "abc": wraps to next line? no next — stays
	if s.curY != 2 || s.curX != 3 {
		t.Fatalf("right at buffer end = (%d,%d), want (2,3)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyUp})
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft}) // column 1 → 0 of "x"
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft}) // column 0: wraps to end of "abcd"
	if s.curY != 0 || s.curX != 4 {
		t.Fatalf("left wraps to previous line end = (%d,%d), want (0,4)", s.curY, s.curX)
	}
}

// TestEditorClickSetsCursor: a left press maps terminal coordinates through BodyY and
// the title bar into a buffer position, clamped to the clicked line's length.
func TestEditorClickSetsCursor(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("hello\nhi\nworld")
	s.dirty = false

	click := func(x, y int) {
		s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	}
	click(2, sh.BodyY()+s.titleH()+1) // second visible row ("hi"), past its end → clamp
	if s.curY != 1 || s.curX != 2 {
		t.Fatalf("click = (%d,%d), want (1,2) clamped to line end", s.curY, s.curX)
	}
	click(3, sh.BodyY()+s.titleH()+2) // "world"
	if s.curY != 2 || s.curX != 3 {
		t.Fatalf("click = (%d,%d), want (2,3)", s.curY, s.curX)
	}
	click(0, sh.BodyY()+s.titleH()+9) // below the buffer → last line
	if s.curY != 2 || s.curX != 0 {
		t.Fatalf("click below buffer = (%d,%d), want (2,0)", s.curY, s.curX)
	}
}

// TestEditorWordDelete mirrors bubbles/textinput's word/line deletes: alt+backspace
// (and ctrl+w) delete the word before the cursor, joining at column 0; alt+delete
// deletes the word ahead, pulling the next line up at end of line; ctrl+u/ctrl+k
// delete to the line start/end.
func TestEditorWordDelete(t *testing.T) {
	s, _ := newEditor(EditorOpts{})

	// Mid-word: alt+backspace deletes back to the word start.
	s.setContent("foo bar baz")
	s.curY, s.curX = 0, 6 // inside "bar", after the 'a'
	s.key(nil, tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got := buffer(s); got != "foo r baz" {
		t.Fatalf("alt+backspace mid-word: buffer = %q, want %q", got, "foo r baz")
	}

	// At a word start (after spaces): deletes the PREVIOUS word and the spaces.
	s.setContent("foo   bar")
	s.curY, s.curX = 0, 6
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlW})
	if got := buffer(s); got != "bar" {
		t.Fatalf("ctrl+w past spaces: buffer = %q, want %q", got, "bar")
	}

	// Column 0 joins the line, exactly like backspace (one press per segment).
	s.setContent("one\ntwo")
	s.curY, s.curX = 1, 0
	s.key(nil, tea.KeyMsg{Type: tea.KeyBackspace, Alt: true})
	if got := buffer(s); got != "onetwo" {
		t.Fatalf("alt+backspace at col 0: buffer = %q, want %q", got, "onetwo")
	}

	// alt+delete removes the word under/ahead of the cursor…
	s.setContent("foo bar")
	s.curY, s.curX = 0, 1
	s.key(nil, tea.KeyMsg{Type: tea.KeyDelete, Alt: true})
	if got := buffer(s); got != "fbar" {
		t.Fatalf("alt+delete mid-word: buffer = %q, want %q", got, "fbar")
	}
	// …and at end of line pulls the next line up.
	s.setContent("ab\ncd")
	s.curY, s.curX = 0, 2
	s.key(nil, tea.KeyMsg{Type: tea.KeyDelete, Alt: true})
	if got := buffer(s); got != "abcd" {
		t.Fatalf("alt+delete at EOL: buffer = %q, want %q", got, "abcd")
	}

	// ctrl+u / ctrl+k delete to the line start / end.
	s.setContent("hello world")
	s.curY, s.curX = 0, 5
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlU})
	if got := buffer(s); got != " world" {
		t.Fatalf("ctrl+u: buffer = %q, want %q", got, " world")
	}
	s.setContent("hello world")
	s.curY, s.curX = 0, 5
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlK})
	if got := buffer(s); got != "hello" {
		t.Fatalf("ctrl+k: buffer = %q, want %q", got, "hello")
	}
}

// TestEditorWordNav mirrors textinput's word jumps: alt/ctrl+left goes to the previous
// word start (wrapping to the previous line's end at column 0), alt/ctrl+right to the
// next word start (wrapping at end of line).
func TestEditorWordNav(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("foo bar\n  baz quux")

	s.curY, s.curX = 0, 5 // inside "bar"
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft, Alt: true})
	if s.curX != 4 {
		t.Fatalf("alt+left to word start: curX = %d, want 4", s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft, Alt: true}) // previous word
	if s.curX != 0 {
		t.Fatalf("alt+left again: curX = %d, want 0", s.curX)
	}
	s.curY, s.curX = 1, 0
	s.key(nil, tea.KeyMsg{Type: tea.KeyLeft, Alt: true}) // col 0 → prev line end
	if s.curY != 0 || s.curX != 7 {
		t.Fatalf("alt+left wraps to prev line end: (%d,%d), want (0,7)", s.curY, s.curX)
	}

	s.curY, s.curX = 0, 0
	s.key(nil, tea.KeyMsg{Type: tea.KeyRight, Alt: true}) // past "foo" and the space
	if s.curX != 4 {
		t.Fatalf("alt+right to next word: curX = %d, want 4", s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlRight}) // no more words on line 1 → its end
	if s.curY != 0 || s.curX != 7 {
		t.Fatalf("ctrl+right to line end: (%d,%d), want (0,7)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlRight}) // EOL → next line start
	if s.curY != 1 || s.curX != 0 {
		t.Fatalf("ctrl+right wraps to next line: (%d,%d), want (1,0)", s.curY, s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyRight, Alt: true}) // leading spaces → "baz"
	if s.curX != 2 {
		t.Fatalf("alt+right over leading spaces: curX = %d, want 2", s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyRight, Alt: true}) // past "baz" and the space → "quux"
	if s.curX != 6 {
		t.Fatalf("alt+right to next word: curX = %d, want 6", s.curX)
	}
}

// TestEditorCtrlAliases: the readline-style aliases textinput also honors — ctrl+h
// backspaces (0x08-sending terminals), ctrl+d forward-deletes, ctrl+a/ctrl+e are
// home/end.
func TestEditorCtrlAliases(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abc")
	s.curY, s.curX = 0, 2

	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlH})
	if got := buffer(s); got != "ac" {
		t.Fatalf("ctrl+h: buffer = %q, want %q", got, "ac")
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := buffer(s); got != "a" {
		t.Fatalf("ctrl+d: buffer = %q, want %q", got, "a")
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlE})
	if s.curX != 1 {
		t.Fatalf("ctrl+e: curX = %d, want 1", s.curX)
	}
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlA})
	if s.curX != 0 {
		t.Fatalf("ctrl+a: curX = %d, want 0", s.curX)
	}
}

// TestEditorLoadAndDirty: Init reads the file asynchronously; a successful load fills
// the buffer clean, a missing file leaves an empty clean buffer (created on save).
func TestEditorLoadAndDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("one\ntwo"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, sh := newEditor(EditorOpts{Path: path})
	msg := s.Init(sh)() // run the async read inline
	s.Update(sh, msg)
	if got := buffer(s); got != "one\ntwo" {
		t.Fatalf("loaded buffer = %q, want %q", got, "one\ntwo")
	}
	if s.dirty {
		t.Fatal("a fresh load must be clean")
	}

	missing, sh2 := newEditor(EditorOpts{Path: filepath.Join(dir, "nope.txt")})
	missing.Update(sh2, missing.Init(sh2)())
	if got := buffer(missing); got != "" || missing.dirty {
		t.Fatalf("missing file should give an empty clean buffer, got %q dirty=%v", got, missing.dirty)
	}
}

// TestEditorExitCleanPops: ctrl+x on an unmodified buffer pops without a prompt.
func TestEditorExitCleanPops(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	if act.Msg == nil {
		t.Fatal("ctrl+x on a clean buffer should pop (non-nil nav msg)")
	}
	if s.confirmExit {
		t.Fatal("no prompt for a clean buffer")
	}
}

// TestEditorExitPrompt covers the dirty ctrl+x flow: the prompt shows, c cancels,
// n discards and pops, y saves and pops after the async write lands.
func TestEditorExitPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "saved.txt")
	s, sh := newEditor(EditorOpts{Path: path})
	typeRunes(s, 'h', 'i')
	s.key(nil, tea.KeyMsg{Type: tea.KeyEnter})
	typeRunes(s, 'y', 'o')

	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	if !s.confirmExit {
		t.Fatal("ctrl+x on a dirty buffer should show the save prompt")
	}

	// While the prompt is up, other keys are swallowed.
	typeRunes(s, 'z')
	if got := buffer(s); got != "hi\nyo" {
		t.Fatalf("prompt mode must swallow typing, buffer = %q", got)
	}

	// c cancels back to editing.
	s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if s.confirmExit {
		t.Fatal("c should cancel the prompt")
	}

	// y saves: the cmd runs the write, its result msg pops the screen.
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if act.Cmd == nil {
		t.Fatal("y should return the async save cmd")
	}
	_, act = s.Update(sh, act.Cmd())
	if act.Msg == nil {
		t.Fatal("a successful save should pop (non-nil nav msg)")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hi\nyo" {
		t.Fatalf("saved content = %q, want %q", b, "hi\nyo")
	}
	if s.dirty {
		t.Fatal("buffer should be clean after a successful save")
	}
}

// TestEditorDiscardExit: n on the prompt pops without writing.
func TestEditorDiscardExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discarded.txt")
	s, _ := newEditor(EditorOpts{Path: path})
	typeRunes(s, 'x')
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if act.Msg == nil {
		t.Fatal("n should pop (non-nil nav msg)")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("n must not write the file")
	}
}

// TestEditorOnExitHook: with EditorOpts.OnExit set (embedded use), every exit path —
// clean ctrl+x, discard, and save — runs the hook instead of popping.
func TestEditorOnExitHook(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		fired := 0
		hook := func(*core.Shared) core.Action { fired++; return core.Action{} }
		s, _ := newEditor(EditorOpts{OnExit: hook})
		_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
		if fired != 1 || act.Msg != nil {
			t.Fatalf("clean ctrl+x should run the hook (fired %d) and not pop (msg %v)", fired, act.Msg)
		}
	})

	t.Run("discard", func(t *testing.T) {
		fired := 0
		hook := func(*core.Shared) core.Action { fired++; return core.Action{} }
		path := filepath.Join(t.TempDir(), "discarded.txt")
		s, _ := newEditor(EditorOpts{Path: path, OnExit: hook})
		typeRunes(s, 'x')
		s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
		_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
		if fired != 1 || act.Msg != nil {
			t.Fatalf("n should run the hook (fired %d) and not pop (msg %v)", fired, act.Msg)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("n must not write the file")
		}
	})

	t.Run("save", func(t *testing.T) {
		fired := 0
		hook := func(*core.Shared) core.Action { fired++; return core.Action{} }
		path := filepath.Join(t.TempDir(), "saved.txt")
		s, sh := newEditor(EditorOpts{Path: path, OnExit: hook})
		typeRunes(s, 'h', 'i')
		s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
		_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		if act.Cmd == nil {
			t.Fatal("y should return the async save cmd")
		}
		_, act = s.Update(sh, act.Cmd())
		if fired != 1 || act.Msg != nil {
			t.Fatalf("a successful save should run the hook (fired %d) and not pop (msg %v)", fired, act.Msg)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != "hi" {
			t.Fatalf("saved content = %q, err = %v", b, err)
		}
	})
}
