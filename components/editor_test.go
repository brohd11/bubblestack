package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// TestEditorEnterAndTab: enter splits the line at the cursor; both tab and its
// shift+tab alias insert a literal tab. A bare tab typing is the whole point of the
// editor owning the key — pane navigation is on shift+arrows, not tab.
func TestEditorEnterAndTab(t *testing.T) {
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
	// A bare tab types one too.
	s.key(nil, tea.KeyMsg{Type: tea.KeyTab})
	if got := buffer(s); got != "abc\nd\t\t" {
		t.Fatalf("bare tab must type, buffer = %q, want %q", got, "abc\nd\t\t")
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

// newPaneEditor builds the embedded shape: told it is a pane before sizing, exactly
// as ScreenPanel drives it. Border is the caller's (the instancer's) choice, carried
// in opts.
func newPaneEditor(opts EditorOpts) (*EditorScreen, *core.Shared) {
	s := NewEditorScreen(opts)
	s.SetEmbedded(true)
	sh := core.NewShared(nil)
	s.SetSize(sh, 40, 10)
	return s, sh
}

// TestEditorBorderedView: bordered, the title moves into the frame's top-edge legend
// (carrying the [+] dirty marker) and the separate title bar is gone; unbordered, the
// title bar stays and no frame is drawn.
func TestEditorBorderedView(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{Title: "notes.md", Border: true})
	v := s.View(sh)
	if !strings.HasPrefix(v, "┌─ notes.md ") {
		t.Fatalf("bordered View should open with the title legend, got %q", strings.SplitN(v, "\n", 2)[0])
	}
	if strings.Contains(v, core.RenderTitleBar("notes.md")) {
		t.Fatal("bordered View must not also draw the title bar")
	}
	if h := lipgloss.Height(v); h != 10 {
		t.Fatalf("bordered View height = %d, want the pane's full 10 rows", h)
	}

	typeRunes(s, 'x') // dirty
	if !strings.HasPrefix(s.View(sh), "┌─ notes.md [+] ") {
		t.Fatal("the dirty marker should ride the legend")
	}

	plain, sh2 := newEditor(EditorOpts{Title: "notes.md"})
	pv := plain.View(sh2)
	if !strings.Contains(pv, core.RenderTitleBar("notes.md")) {
		t.Fatal("unbordered View should keep its title bar")
	}
	if strings.Contains(pv, "┌") {
		t.Fatal("unbordered View must draw no frame")
	}
}

// TestEditorSizeInsets: the viewport is the assigned dims net of whatever chrome the
// editor draws — the frame on both axes when bordered, the title bar otherwise, plus
// the one-column gutter when embedded. Standalone stays what it always was.
func TestEditorSizeInsets(t *testing.T) {
	plain := NewEditorScreen(EditorOpts{})
	plain.SetSize(core.NewShared(nil), 40, 10)
	if plain.w != 40 || plain.h != 10-plain.titleH() {
		t.Fatalf("standalone 40x10 → %dx%d, want 40x%d", plain.w, plain.h, 10-plain.titleH())
	}

	pane, _ := newPaneEditor(EditorOpts{}) // embedded, unbordered: gutter + title bar
	if pane.w != 39 || pane.h != 10-pane.titleH() {
		t.Fatalf("pane 40x10 → %dx%d, want 39x%d", pane.w, pane.h, 10-pane.titleH())
	}

	framed, _ := newPaneEditor(EditorOpts{Border: true}) // frame + gutter
	if framed.w != 37 || framed.h != 8 {
		t.Fatalf("framed pane 40x10 → %dx%d, want 37x8", framed.w, framed.h)
	}
}

// TestEditorPaneClick is the embedded mouse contract (core.Embeddable): the host
// ModularScreen hands over pane-relative coordinates, so only the editor's own chrome
// comes off. Covers gote's real shape (unbordered: title bar + gutter) and the framed
// one. The BodyY half of the contract can't be exercised here — Shared.bodyY is
// router-owned and unexported, so it is 0 in every components test (as it is in the
// standalone TestEditorClickSetsCursor above); what this pins is the inset math.
func TestEditorPaneClick(t *testing.T) {
	press := func(s *EditorScreen, sh *core.Shared, x, y int) {
		s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	}

	t.Run("unbordered", func(t *testing.T) {
		s, sh := newPaneEditor(EditorOpts{})
		s.setContent("hello\nhi\nworld")
		press(s, sh, 1, s.titleH()) // first body row, first text column
		if s.curY != 0 || s.curX != 0 {
			t.Fatalf("click on the first text cell = (%d,%d), want (0,0)", s.curY, s.curX)
		}
		press(s, sh, 4, s.titleH()+2) // "world", fourth column
		if s.curY != 2 || s.curX != 3 {
			t.Fatalf("click = (%d,%d), want (2,3)", s.curY, s.curX)
		}
		press(s, sh, 0, s.titleH()) // the gutter reads as column 0
		if s.curY != 0 || s.curX != 0 {
			t.Fatalf("click in the gutter = (%d,%d), want (0,0)", s.curY, s.curX)
		}
	})

	t.Run("bordered", func(t *testing.T) {
		s, sh := newPaneEditor(EditorOpts{Border: true})
		s.setContent("hello\nhi\nworld")
		press(s, sh, 2, 1) // inside the frame, past the gutter
		if s.curY != 0 || s.curX != 0 {
			t.Fatalf("click on the first text cell = (%d,%d), want (0,0)", s.curY, s.curX)
		}
		press(s, sh, 5, 3) // "world", fourth column
		if s.curY != 2 || s.curX != 3 {
			t.Fatalf("click = (%d,%d), want (2,3)", s.curY, s.curX)
		}
		press(s, sh, 3, 0) // the top border is not a buffer row
		if s.curY != 2 || s.curX != 3 {
			t.Fatalf("click on the top border moved the cursor to (%d,%d)", s.curY, s.curX)
		}
	})
}

// TestEditorUnfocusedRender: an unfocused pane mutes its body and drops the cursor —
// a caret where the keys don't land reads as a lie — and mutes its title bar with it.
// A standalone editor is focused from birth, so none of this shows.
func TestEditorUnfocusedRender(t *testing.T) {
	withColor(t) // the tint and the reverse-video caret are ANSI; force a profile

	s, sh := newPaneEditor(EditorOpts{Title: "notes.md"})
	s.setContent("hello\nworld")
	if !s.focused {
		t.Fatal("an editor should be focused from birth (standalone always is)")
	}
	lit := s.View(sh)
	if !strings.Contains(lit, editorCursorStyle.Render("h")) {
		t.Fatal("the focused pane should draw its cursor cell")
	}

	s.SetFocused(false)
	dark := s.View(sh)
	if strings.Contains(dark, editorCursorStyle.Render("h")) {
		t.Fatal("an unfocused pane must not draw a cursor")
	}
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	if !strings.Contains(dark, muted.Render("hello")) {
		t.Fatal("an unfocused pane should mute its body text")
	}
	if strings.Contains(dark, core.RenderTitleBar("notes.md")) {
		t.Fatal("an unfocused pane should mute its title bar too")
	}
	if lipgloss.Height(lit) != lipgloss.Height(dark) {
		t.Fatal("focus must not change the pane's footprint")
	}

	s.SetFocused(true)
	if s.View(sh) != lit {
		t.Fatal("regaining focus should restore the lit render exactly")
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

	// y pushes the filename prompt (nano's "File Name to Write"), seeded with the
	// buffer's name; enter saves, and the async result pops the screen.
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	_, act := s.Update(sh, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if act.Msg == nil || act.Cmd != nil {
		t.Fatal("y should push the filename prompt (nav msg, no cmd)")
	}
	edit := s.saveAsEdit(sh)
	if got := edit.input.Value(); got != path {
		t.Fatalf("the filename prompt should seed the full path, got %q", got)
	}
	if act := edit.OnDone(sh, path); act.Msg == nil {
		t.Fatal("enter on the filename prompt should navigate (pop + save)")
	}
	_, act = s.Update(sh, s.saveCmd()())
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
	if s.confirmExit {
		t.Fatal("a completed save must clear the prompt")
	}
}

// TestEditorDiscardExit: n on the prompt pops without writing, prompt cleared.
func TestEditorDiscardExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "discarded.txt")
	s, _ := newEditor(EditorOpts{Path: path})
	typeRunes(s, 'x')
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	_, act := s.key(nil, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if act.Msg == nil {
		t.Fatal("n should pop (non-nil nav msg)")
	}
	if s.confirmExit {
		t.Fatal("n must clear the prompt")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("n must not write the file")
	}
}

// TestEditorSaveKey: ctrl+s writes and STAYS — no exit, no OnExit — and reports the
// path it landed at through OnSaved. A save-as through the same box renames the buffer
// under a live cursor, so the third case checks the caret and scroll survive it.
func TestEditorSaveKey(t *testing.T) {
	dir := t.TempDir()

	t.Run("saves without exiting", func(t *testing.T) {
		path := filepath.Join(dir, "stay.txt")
		exited, saved := 0, ""
		s, sh := newEditor(EditorOpts{
			Path:    path,
			OnExit:  func(*core.Shared) core.Action { exited++; return core.Action{} },
			OnSaved: func(_ *core.Shared, p string) core.Action { saved = p; return core.Action{} },
		})
		typeRunes(s, 'h', 'i')
		s.key(sh, tea.KeyMsg{Type: tea.KeyCtrlS})
		if s.confirmExit {
			t.Fatal("ctrl+s must not raise the exit prompt")
		}
		s.saveAsEdit(sh).OnDone(sh, path)
		s.Update(sh, s.saveCmd()())
		if exited != 0 {
			t.Fatalf("ctrl+s must not exit (hook fired %d)", exited)
		}
		if saved != path {
			t.Fatalf("OnSaved should report the written path, got %q", saved)
		}
		if s.dirty {
			t.Fatal("a completed save should clear the dirty marker")
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != "hi" {
			t.Fatalf("saved content = %q, err = %v", b, err)
		}
	})

	t.Run("save-as adopts the new path", func(t *testing.T) {
		saved := ""
		s, sh := newEditor(EditorOpts{
			Path:    filepath.Join(dir, "before.txt"),
			OnSaved: func(_ *core.Shared, p string) core.Action { saved = p; return core.Action{} },
		})
		typeRunes(s, 'x')
		s.key(sh, tea.KeyMsg{Type: tea.KeyCtrlS})
		after := filepath.Join(dir, "nested", "after.txt") // a dir that does not exist yet
		s.saveAsEdit(sh).OnDone(sh, after)
		s.Update(sh, s.saveCmd()())
		if s.path != after || s.title != "after.txt" {
			t.Fatalf("save-as should rename the buffer, got path %q title %q", s.path, s.title)
		}
		if saved != after {
			t.Fatalf("OnSaved should report the NEW path, got %q", saved)
		}
		if b, err := os.ReadFile(after); err != nil || string(b) != "x" {
			t.Fatalf("the new path should hold the buffer: %q, err = %v", b, err)
		}
	})

	t.Run("keeps the caret and the view", func(t *testing.T) {
		s, sh := newEditor(EditorOpts{Path: filepath.Join(dir, "caret.txt")})
		s.setContent(longDoc())
		s.curY, s.curX, s.scrY = 60, 1, 55
		s.key(sh, tea.KeyMsg{Type: tea.KeyCtrlS})
		s.saveAsEdit(sh).OnDone(sh, s.path)
		s.Update(sh, s.saveCmd()())
		if s.curY != 60 || s.curX != 1 || s.scrY != 55 {
			t.Fatalf("a save must not move the caret or the view, got %d/%d scroll %d", s.curY, s.curX, s.scrY)
		}
	})

	t.Run("renaming re-picks the highlighter", func(t *testing.T) {
		s, _ := newEditor(EditorOpts{Path: filepath.Join(dir, "plain.txt")})
		if s.hl != nil {
			t.Fatal(".txt has no registered highlighter")
		}
		s.applySaveName(filepath.Join(dir, "now.md"))
		if s.hl == nil {
			t.Fatal("a rename to .md should pick the markdown highlighter up")
		}
		explicit := NewMarkdownHighlighter()
		s2, _ := newEditor(EditorOpts{Path: filepath.Join(dir, "a.md"), Highlighter: explicit})
		s2.applySaveName(filepath.Join(dir, "b.txt"))
		if s2.hl != explicit {
			t.Fatal("an explicitly configured highlighter must survive a rename")
		}
	})
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
		// Through the real "y": it is what marks this save as the exit path's, and
		// so what makes the write end in the hook rather than in a plain save. It
		// needs the real Shared — the prompt it pushes anchors off the body.
		s.key(sh, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
		if act := s.saveAsEdit(sh).OnDone(sh, path); act.Msg == nil {
			t.Fatal("enter on the filename prompt should navigate (pop + save)")
		}
		_, act := s.Update(sh, s.saveCmd()())
		if fired != 1 || act.Msg != nil {
			t.Fatalf("a successful save should run the hook (fired %d) and not pop (msg %v)", fired, act.Msg)
		}
		if b, err := os.ReadFile(path); err != nil || string(b) != "hi" {
			t.Fatalf("saved content = %q, err = %v", b, err)
		}
	})
}

// TestEditorSaveAs: a different name at the filename prompt writes the NEW path and
// renames the buffer (path and title follow), leaving the old file untouched; a
// blank name cancels. A scratch buffer's prompt starts empty and saves to the typed
// name — the save a bare "y" used to refuse with "no file path".
func TestEditorSaveAs(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, sh := newEditor(EditorOpts{Path: old})
	s.Update(sh, s.Init(sh)())
	typeRunes(s, '!')

	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	s.key(sh, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	edit := s.saveAsEdit(sh)
	if got := edit.input.Value(); got != old {
		t.Fatalf("the prompt should seed the full path, got %q", got)
	}
	edit.OnDone(sh, "  ") // blank: a quiet cancel
	if s.path != old {
		t.Fatalf("a blank name must not touch the path, got %q", s.path)
	}
	renamed := filepath.Join(dir, "renamed.txt")
	edit.OnDone(sh, renamed)
	if s.path != renamed || s.title != "renamed.txt" {
		t.Fatalf("save-as should rename the buffer, got path %q title %q", s.path, renamed)
	}
	s.Update(sh, s.saveCmd()())
	if b, err := os.ReadFile(renamed); err != nil || string(b) != "!orig" {
		t.Fatalf("the new path should hold the buffer: %q, err = %v", b, err)
	}
	if b, err := os.ReadFile(old); err != nil || string(b) != "orig" {
		t.Fatalf("save-as must not touch the old file: %q, err = %v", b, err)
	}

	scratch, sh2 := newEditor(EditorOpts{})
	typeRunes(scratch, 'n', 'o')
	scratch.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	scratch.key(sh2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	edit2 := scratch.saveAsEdit(sh2)
	if got := edit2.input.Value(); got != "" {
		t.Fatalf("a scratch buffer's prompt starts empty, got %q", got)
	}
	fresh := filepath.Join(dir, "fresh.txt")
	edit2.OnDone(sh2, fresh)
	if _, act := scratch.Update(sh2, scratch.saveCmd()()); act.Msg == nil {
		t.Fatal("saving a named scratch buffer should pop")
	}
	if b, err := os.ReadFile(fresh); err != nil || string(b) != "no" {
		t.Fatalf("scratch saved content = %q, err = %v", b, err)
	}
}

// longDoc is a 100-line buffer, taller than any test viewport.
func longDoc() string {
	return strings.TrimSuffix(strings.Repeat("x\n", 100), "\n")
}

func wheel(s *EditorScreen, sh *core.Shared, btn tea.MouseButton) {
	s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: btn, X: 5, Y: 5})
}

// TestEditorWheelScroll: the wheel browses the buffer without moving the caret,
// clamped at both ends; a caret move afterwards snaps the view back to it
// (clampScroll runs after every keystroke).
func TestEditorWheelScroll(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc())

	wheel(s, sh, tea.MouseButtonWheelDown)
	if s.scrY != editorWheelStep {
		t.Fatalf("wheel down → scrY %d, want %d", s.scrY, editorWheelStep)
	}
	if s.curY != 0 || s.curX != 0 {
		t.Fatalf("the wheel must not move the caret, got (%d,%d)", s.curY, s.curX)
	}
	wheel(s, sh, tea.MouseButtonWheelUp)
	wheel(s, sh, tea.MouseButtonWheelUp) // past the top: clamps
	if s.scrY != 0 {
		t.Fatalf("wheel up past the top → scrY %d, want 0", s.scrY)
	}
	for i := 0; i < 40; i++ {
		wheel(s, sh, tea.MouseButtonWheelDown)
	}
	if want := len(s.lines) - s.h; s.scrY != want {
		t.Fatalf("wheel at the bottom → scrY %d, want %d (len-h)", s.scrY, want)
	}

	// The router re-lays out (SetSize) after EVERY message; the browse position must
	// survive it, or each wheel tick snaps straight back to the caret.
	s.SetSize(sh, 80, 20)
	if want := len(s.lines) - s.h; s.scrY != want {
		t.Fatalf("SetSize undid the wheel scroll: scrY %d, want %d", s.scrY, want)
	}

	// The caret sits far above the view now; one arrow snaps the view back to it.
	s.key(nil, tea.KeyMsg{Type: tea.KeyDown})
	if s.curY != 1 || s.scrY != 1 {
		t.Fatalf("down with the caret off-screen → (%d, scrY %d), want (1, 1)", s.curY, s.scrY)
	}
}

// TestEditorWheelFocus: mouse msgs are broadcast to every pane, so the wheel rolls
// only the focused editor; the exit prompt suspends it too.
func TestEditorWheelFocus(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{})
	s.setContent(longDoc())

	s.SetFocused(false)
	wheel(s, sh, tea.MouseButtonWheelDown)
	if s.scrY != 0 {
		t.Fatalf("an unfocused pane must not scroll, scrY = %d", s.scrY)
	}
	s.SetFocused(true)
	wheel(s, sh, tea.MouseButtonWheelDown)
	if s.scrY != editorWheelStep {
		t.Fatalf("focused wheel down → scrY %d, want %d", s.scrY, editorWheelStep)
	}

	s.dirty = true
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlX})
	if !s.confirmExit {
		t.Fatal("dirty ctrl+x should raise the exit prompt")
	}
	before := s.scrY // raising the prompt re-clamps the view to the caret
	wheel(s, sh, tea.MouseButtonWheelDown)
	if s.scrY != before {
		t.Fatalf("the prompt suspends the wheel: scrY %d, want %d", s.scrY, before)
	}
}

// TestEditorScrollbar: a buffer taller than the viewport draws a proportional
// scrollbar in the rightmost column — a thumb sized to the viewport's share of the
// buffer, placed by scrY — and the text window narrows one column so the caret
// never hides under the bar. A short buffer keeps the full width and no bar.
func TestEditorScrollbar(t *testing.T) {
	s, _ := newEditor(EditorOpts{}) // standalone: no gutter, the bar is the last cell
	s.setContent("a\nb\nc")
	if s.barVisible() || s.textW() != s.w {
		t.Fatalf("a buffer that fits: barVisible=%v textW=%d, want no bar and full %d", s.barVisible(), s.textW(), s.w)
	}
	if strings.ContainsRune(s.body(), '█') || strings.ContainsRune(s.body(), '│') {
		t.Fatal("no scrollbar cells without overflow")
	}

	s.setContent(longDoc())
	if !s.barVisible() || s.textW() != s.w-1 {
		t.Fatalf("overflow: barVisible=%v textW=%d, want the bar and w-1 (%d)", s.barVisible(), s.textW(), s.w-1)
	}
	thumb := s.h * s.h / len(s.lines)
	lastCell := func(row string) rune { rs := []rune(row); return rs[len(rs)-1] }
	assertBar := func(top int) {
		t.Helper()
		rows := strings.Split(s.body(), "\n")
		if len(rows) != s.h {
			t.Fatalf("body = %d rows, want %d", len(rows), s.h)
		}
		for i, row := range rows {
			if w := lipgloss.Width(row); w != s.w {
				t.Fatalf("row %d width = %d, want %d (bar included)", i, w, s.w)
			}
			want := '│'
			if i >= top && i < top+thumb {
				want = '█'
			}
			if got := lastCell(row); got != want {
				t.Fatalf("row %d bar cell = %q, want %q (scrY %d)", i, got, want, s.scrY)
			}
		}
	}
	assertBar(0) // unscrolled: the thumb hugs the top

	s.scrollLines(len(s.lines)) // to the very bottom
	if top := s.scrY * (s.h - thumb) / (len(s.lines) - s.h); top != s.h-thumb {
		t.Fatalf("at the bottom the thumb should hug the last rows: top %d, want %d", top, s.h-thumb)
	}
	assertBar(s.h - thumb)

	// The caret never hides under the bar: horizontal scrolling keeps it within textW.
	s.setContent(strings.Repeat("y", 200) + "\n" + longDoc())
	s.key(nil, tea.KeyMsg{Type: tea.KeyCtrlE})
	if cell := cellOfCol(s.lines[0], s.curX); s.scrX+s.textW() != cell+1 {
		t.Fatalf("caret at EOL should sit in the last text cell: scrX=%d textW=%d cell=%d", s.scrX, s.textW(), cell)
	}
}

// TestEditorScrollbarClick: the bar column carries no buffer position — a click
// there is ignored rather than yanking the caret to a line's end.
func TestEditorScrollbarClick(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc())
	click := func(x, y int) {
		s.Update(sh, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	}
	click(s.w-1, s.titleH()+5) // the bar column
	if s.curY != 0 || s.curX != 0 {
		t.Fatalf("click on the scrollbar moved the caret to (%d,%d)", s.curY, s.curX)
	}
	click(3, s.titleH()+5) // inside the text: still lands (the 1-rune line clamps the column)
	if s.curY != 5 || s.curX != 1 {
		t.Fatalf("click in the text = (%d,%d), want (5,1)", s.curY, s.curX)
	}
}

// TestEditorSaveAsAnchor: the save-as box covers the bottom of just this editor —
// embedded, the host-pushed pane origin and the pane's own width; standalone (no
// origin was ever pushed) it spans the terminal width at the body's bottom.
func TestEditorSaveAsAnchor(t *testing.T) {
	s, sh := newEditor(EditorOpts{}) // standalone 80x20
	edit := s.saveAsEdit(sh)
	if edit.x != 0 || edit.width != sh.Width() {
		t.Fatalf("standalone anchor = (%d, w %d), want (0, %d)", edit.x, edit.width, sh.Width())
	}
	if want := sh.BodyY() + s.insetY() + s.h - 2; edit.y != want {
		t.Fatalf("standalone y = %d, want %d (one row above the prompt row)", edit.y, want)
	}

	pane, psh := newPaneEditor(EditorOpts{}) // embedded 40x10: gutter + title bar
	pane.SetPaneOrigin(31, 4)
	edit = pane.saveAsEdit(psh)
	if pane.paneW() != 40 {
		t.Fatalf("paneW = %d, want the pane's full 40", pane.paneW())
	}
	if edit.x != 31 || edit.width != 40 {
		t.Fatalf("embedded anchor = (%d, w %d), want (31, 40)", edit.x, edit.width)
	}
	if want := 4 + pane.insetY() + pane.h - 2; edit.y != want {
		t.Fatalf("embedded y = %d, want %d", edit.y, want)
	}
}
