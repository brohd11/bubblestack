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

// newEditor builds a sized screen over a shared with no chrome (BodyY 0, so mouse
// tests work in body-relative rows directly).
func newEditor(opts EditorOpts) (*EditorScreen, *core.Shared) {
	s := NewEditorScreen(opts)
	sh := core.NewShared(nil)
	s.SetSize(sh, 80, 20)
	return s, sh
}

func typeRunes(s *EditorScreen, rs ...rune) {
	s.key(nil, keyMsg(string(rs)))
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

func selectRange(s *EditorScreen, y1, x1, y2, x2 int) {
	s.selStart, s.selEnd = textPos{y1, x1}, textPos{y2, x2}
	s.curY, s.curX, s.wantX = y2, x2, x2
}

func TestEditorSelectedText(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha\tbeta\ngamma\nlast")
	selectRange(s, 0, 2, 2, 3)
	if got, want := s.selectedText(), "pha\tbeta\ngamma\nlas"; got != want {
		t.Fatalf("selected text = %q, want %q", got, want)
	}

	selectRange(s, 1, 1, 1, 4)
	if got, want := s.selectedText(), "amm"; got != want {
		t.Fatalf("single-line selected text = %q, want %q", got, want)
	}
}

func TestEditorSelectionEditing(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want string
	}{
		{"typing replaces", keyMsg("X"), "aXef"},
		{"enter replaces", keyMsg("enter"), "a\nef"},
		{"tab replaces", keyMsg("tab"), "a\tef"},
		{"backspace deletes", keyMsg("backspace"), "aef"},
		{"delete deletes", keyMsg("delete"), "aef"},
		{"word delete deletes", keyMsg("ctrl+w"), "aef"},
		{"line delete deletes", keyMsg("ctrl+k"), "aef"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{})
			s.setContent("abcdef")
			selectRange(s, 0, 1, 0, 4)
			s.key(nil, tc.key)
			if got := buffer(s); got != tc.want {
				t.Fatalf("buffer = %q, want %q", got, tc.want)
			}
			if s.selectionActive() {
				t.Fatal("an edit should consume the selection")
			}
		})
	}
}

func TestEditorCaretMoveClearsSelection(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	s.key(nil, keyMsg("right"))
	if s.selectionActive() {
		t.Fatal("caret movement should clear the selection")
	}
	if s.curX != 5 {
		t.Fatalf("right should move from the active endpoint to column 5, got %d", s.curX)
	}
}

func TestEditorLeftMouseDragSelectsWithoutCopy(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdef")
	y := s.titleH()

	s.Update(sh, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	s.Update(sh, tea.MouseMotionMsg{X: 3, Y: y, Button: tea.MouseLeft})
	_, act := s.Update(sh, tea.MouseReleaseMsg{X: 3, Y: y, Button: tea.MouseNone})
	if act.Cmd != nil {
		t.Fatal("a left drag should select without issuing a clipboard command")
	}
	if got := s.selectedText(); got != "bcd" || s.curX != 4 {
		t.Fatalf("left drag selected %q with caret %d, want %q with caret 4", got, s.curX, "bcd")
	}
}

func TestEditorClickDoesNotCopyOrSelect(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abc")
	y := s.titleH()
	s.Update(sh, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	_, act := s.Update(sh, tea.MouseReleaseMsg{X: 1, Y: y, Button: tea.MouseNone})
	if act.Cmd != nil || s.selectionActive() {
		t.Fatal("a press/release in one cell should remain an ordinary caret click")
	}
}

// TestEditorRightClickOutsideSelectionBecomesCaretClick: the "outside" half of the
// context menu's desktop convention — the press drops the old selection and puts the
// caret where it landed, so a paste goes where the user pointed, and only then opens
// the menu.
func TestEditorRightClickOutsideSelectionBecomesCaretClick(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	y := s.titleH()

	_, act := s.Update(sh, tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseRight})
	if act.Msg == nil {
		t.Fatal("a right press should open the context menu")
	}
	if s.selectionActive() || s.curX != 5 {
		t.Fatalf("outside right click: selection=%v caret=%d, want no selection and caret 5", s.selectionActive(), s.curX)
	}
	// The release that follows must not disturb what the press set up.
	s.Update(sh, tea.MouseReleaseMsg{X: 5, Y: y, Button: tea.MouseNone})
	if s.selectionActive() || s.curX != 5 {
		t.Fatalf("the release after a right press changed state: selection=%v caret=%d", s.selectionActive(), s.curX)
	}
}

// TestEditorRightPressBreaksMultiClickChain replaces the old cross-button guard: right
// presses no longer reach pressSelection at all, so instead of being tracked as a
// different button's click they must simply end the run, leaving the next left press a
// fresh first click rather than the second of a double.
func TestEditorRightPressBreaksMultiClickChain(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("foo bar")
	y := s.titleH()

	s.Update(sh, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	s.Update(sh, tea.MouseReleaseMsg{X: 1, Y: y, Button: tea.MouseNone})
	s.Update(sh, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseRight})
	s.Update(sh, tea.MouseClickMsg{X: 1, Y: y, Button: tea.MouseLeft})
	if s.clickCount != 1 || s.selectionActive() {
		t.Fatalf("a right press between two left clicks formed a multi-click: count=%d selection=%v",
			s.clickCount, s.selectionActive())
	}
}

func TestEditorReverseAndMultilineDrag(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abc\ndef")
	y := s.titleH()
	s.startDrag(sh, 1, y+1) // e
	s.extendDrag(sh, 1, y)  // b
	if got, want := s.selectedText(), "bc\nde"; got != want {
		t.Fatalf("reverse multiline drag = %q, want %q", got, want)
	}
	if s.curY != 0 || s.curX != 1 {
		t.Fatalf("reverse drag caret = (%d,%d), want active endpoint (0,1)", s.curY, s.curX)
	}
}

func TestEditorSelectionRenderKeepsGeometry(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("a\tb\nnext")
	selectRange(s, 0, 1, 1, 2)
	row := s.renderLine(0)
	if strings.Contains(row, "\t") {
		t.Fatal("selection rendering must keep tabs expanded")
	}
	if lipgloss.Width(row) != 7 { // a + four tab cells + b + selected newline cell
		t.Fatalf("selected row width = %d, want 7", lipgloss.Width(row))
	}
	if !strings.Contains(row, "\x1b[") {
		t.Fatal("selected cells should carry an ANSI background style")
	}
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
// editor owning the key — pane navigation is on shift+arrows and shift+tab, never on
// bare tab. This is a standalone editor, which is the only place the shift+tab alias
// survives: a ModularScreen host claims it as PaneNext before the panel is asked.
func TestEditorEnterAndTab(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, 'a', 'b', 'c')
	s.key(nil, keyMsg("enter"))
	typeRunes(s, 'd')
	if got := buffer(s); got != "abc\nd" {
		t.Fatalf("buffer = %q, want %q", got, "abc\nd")
	}
	s.key(nil, keyMsg("shift+tab"))
	if got := buffer(s); got != "abc\nd\t" {
		t.Fatalf("buffer = %q, want tab inserted, got %q", "abc\nd\t", got)
	}
	// A bare tab types one too.
	s.key(nil, keyMsg("tab"))
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
	if !strings.Contains(ansi.Strip(s.renderLine(0)), "a"+strings.Repeat(" ", editorTabWidth)+"b") {
		t.Fatalf("renderLine should expand the tab to %d spaces, got %q", editorTabWidth, s.renderLine(0))
	}
}

// pasteText delivers text the way a bracketed paste arrives: one tea.PasteMsg carrying
// the whole payload, newlines and all.
func pasteText(s *EditorScreen, text string) {
	s.Update(nil, tea.PasteMsg{Content: text})
}

// TestSplitPastedLines pins the line-break normalization and the control-rune filter:
// tabs are the one control rune that survives, because expandLine gives it cells.
func TestSplitPastedLines(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "abc", []string{"abc"}},
		{"empty", "", []string{""}},
		{"lf", "a\nb", []string{"a", "b"}},
		{"crlf", "a\r\nb", []string{"a", "b"}},
		{"lone cr", "a\rb", []string{"a", "b"}},
		{"trailing newline", "a\n", []string{"a", ""}},
		{"blank line", "a\n\nb", []string{"a", "", "b"}},
		{"tab kept", "a\tb", []string{"a\tb"}},
		{"controls dropped", "a\x00b\x1bc\x7fd", []string{"abcd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := splitPastedLines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("split(%q) = %q, want %q", tc.in, got, tc.want)
			}
			for i := range got {
				if string(got[i]) != tc.want[i] {
					t.Fatalf("split(%q)[%d] = %q, want %q", tc.in, i, string(got[i]), tc.want[i])
				}
			}
		})
	}
}

// TestEditorPasteSplitsLines is the core fix: a multi-line paste mid-line splits into
// real buffer lines, the tail rides the last one, and the caret sits between them.
func TestEditorPasteSplitsLines(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("head-tail")
	s.curX, s.wantX = 4, 4 // between "head" and "-tail"
	pasteText(s, "one\ntwo\r\nthree")
	if got := buffer(s); got != "headone\ntwo\nthree-tail" {
		t.Fatalf("buffer = %q, want %q", got, "headone\ntwo\nthree-tail")
	}
	if s.curY != 2 || s.curX != 5 { // end of "three", before the tail
		t.Fatalf("cursor = (%d,%d), want (2,5)", s.curY, s.curX)
	}
	if s.wantX != s.curX {
		t.Fatalf("wantX = %d, want %d", s.wantX, s.curX)
	}
	if !s.dirty {
		t.Fatal("paste must mark the buffer dirty")
	}
}

// TestEditorPasteSelectionAndUndo: a paste replaces the selection like typing does, and
// the whole block is a single undo step (editorEditKey snapshots any rune-bearing key).
func TestEditorPasteSelectionAndUndo(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abc\ndef")
	selectRange(s, 0, 1, 1, 2)
	pasteText(s, "X\nY")
	if got := buffer(s); got != "aX\nYf" {
		t.Fatalf("buffer = %q, want %q", got, "aX\nYf")
	}
	s.key(nil, keyMsg("ctrl+z"))
	if got := buffer(s); got != "abc\ndef" {
		t.Fatalf("one undo should take back the whole paste, buffer = %q", got)
	}
}

// TestEditorPasteKeepsGeometry is the rendering regression the user hit: newlines inside
// a buffer line made View emit extra physical rows and shifted every later frame. The
// view must keep its exact height and width after a paste, wrapped or not.
func TestEditorPasteKeepsGeometry(t *testing.T) {
	block := "short\n" + strings.Repeat("long ", 60) + "\n\ttabbed\nlast"
	for _, wrap := range []bool{false, true} {
		s, sh := newEditor(EditorOpts{})
		if wrap {
			s.ToggleWrap()
		}
		h := lipgloss.Height(s.View(sh))
		pasteText(s, block)
		after := s.View(sh)
		if strings.Contains(after, "\t") {
			t.Fatalf("wrap=%v: View must never emit a raw tab", wrap)
		}
		if got := lipgloss.Height(after); got != h {
			t.Fatalf("wrap=%v: height after paste = %d, want %d", wrap, got, h)
		}
		for i, row := range strings.Split(after, "\n") {
			if got := lipgloss.Width(row); got > s.w {
				t.Fatalf("wrap=%v: row %d width = %d, overflows the %d-cell body", wrap, i, got, s.w)
			}
		}
		if len(s.lines) != 4 {
			t.Fatalf("wrap=%v: paste should yield 4 buffer lines, got %d", wrap, len(s.lines))
		}
	}
}

// TestEditorPasteSkipsExtensionHandler: a paste's String() is the bracketed form, so the
// markdown list-continuation handler (which gates on "enter") leaves it alone — pasted
// list items must not sprout extra markers.
func TestEditorPasteSkipsExtensionHandler(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "notes.md"})
	if s.keyHandler == nil {
		t.Fatal("a .md buffer should carry the markdown key handler")
	}
	pasteText(s, "- a\n- b")
	if got := buffer(s); got != "- a\n- b" {
		t.Fatalf("buffer = %q, want %q", got, "- a\n- b")
	}
}

// TestExpandLineControlPlaceholder: control runes that reach the buffer another way
// (setContent only normalizes \r\n, so a lone \r or a NUL survives a file load) render as
// a one-cell placeholder rather than escaping into the frame.
func TestExpandLineControlPlaceholder(t *testing.T) {
	p := string(editorControlPlaceholder)
	got := string(expandLine([]rune("a\rb\x00c\n")))
	want := "a" + p + "b" + p + "c" + p
	if got != want {
		t.Fatalf("expandLine = %q, want %q", got, want)
	}
	// The backstop that keeps the frame rectangular no matter what reaches a line: an
	// embedded newline is one cell, not a second physical row.
	if n := lipgloss.Height(string(expandLine([]rune("a\nb")))); n != 1 {
		t.Fatalf("expanded line spans %d rows, want 1", n)
	}
	s, sh := newEditor(EditorOpts{})
	s.setContent("a\rb")
	if v := s.View(sh); strings.Contains(v, "\r") {
		t.Fatal("View must never emit a raw carriage return")
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
	s.Update(sh, tea.MouseClickMsg{X: 4, Y: sh.BodyY() + s.titleH(), Button: tea.MouseLeft})
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
	s.key(nil, keyMsg("backspace")) // join
	if got := buffer(s); got != "abcd" {
		t.Fatalf("join: buffer = %q, want %q", got, "abcd")
	}
	if s.curY != 0 || s.curX != 2 {
		t.Fatalf("cursor after join = (%d,%d), want (0,2)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("backspace")) // delete 'b'
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

	s.key(nil, keyMsg("down")) // onto "x": clamp 3 → 1
	if s.curY != 1 || s.curX != 1 {
		t.Fatalf("down onto short line = (%d,%d), want (1,1)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("down")) // onto "abc": back to target column 3
	if s.curY != 2 || s.curX != 3 {
		t.Fatalf("down restores target column = (%d,%d), want (2,3)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("right")) // end of "abc": wraps to next line? no next — stays
	if s.curY != 2 || s.curX != 3 {
		t.Fatalf("right at buffer end = (%d,%d), want (2,3)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("up"))
	s.key(nil, keyMsg("left")) // column 1 → 0 of "x"
	s.key(nil, keyMsg("left")) // column 0: wraps to end of "abcd"
	if s.curY != 0 || s.curX != 4 {
		t.Fatalf("left wraps to previous line end = (%d,%d), want (0,4)", s.curY, s.curX)
	}
}

// TestEditorArrowsAtBufferEnds: off either end of the buffer a vertical move becomes a
// horizontal one — down on the last line lands at end of line, up on the first at
// column zero — rather than the no-op that used to make a held arrow stall mid-line.
// Both reset wantX, so a subsequent vertical run aims at where the caret actually is.
func TestEditorArrowsAtBufferEnds(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcd\nx\nabcdef")
	s.curY, s.curX, s.wantX = 2, 2, 2

	s.key(nil, keyMsg("down"))
	if s.curY != 2 || s.curX != 6 || s.wantX != 6 {
		t.Fatalf("down on the last line = (%d,%d) wantX %d, want (2,6) wantX 6", s.curY, s.curX, s.wantX)
	}
	// wantX moved with it: the next up aims at column 6, not the original 2.
	s.key(nil, keyMsg("up"))
	if s.curY != 1 || s.curX != 1 {
		t.Fatalf("up after the end move = (%d,%d), want (1,1)", s.curY, s.curX)
	}

	s.curY, s.curX, s.wantX = 0, 2, 2
	s.key(nil, keyMsg("up"))
	if s.curY != 0 || s.curX != 0 || s.wantX != 0 {
		t.Fatalf("up on the first line = (%d,%d) wantX %d, want (0,0) wantX 0", s.curY, s.curX, s.wantX)
	}

	// A one-line buffer is both ends at once.
	s.setContent("hello")
	s.curX, s.wantX = 2, 2
	s.key(nil, keyMsg("down"))
	if s.curY != 0 || s.curX != 5 {
		t.Fatalf("down in a one-line buffer = (%d,%d), want (0,5)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("up"))
	if s.curY != 0 || s.curX != 0 {
		t.Fatalf("up in a one-line buffer = (%d,%d), want (0,0)", s.curY, s.curX)
	}
}

// TestEditorClickSetsCursor: a left press maps terminal coordinates through BodyY and
// the title bar into a buffer position, clamped to the clicked line's length.
func TestEditorClickSetsCursor(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("hello\nhi\nworld")
	s.dirty = false

	click := func(x, y int) {
		s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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
// (carrying the (*) dirty marker) and the separate title bar is gone; unbordered, the
// title bar stays and no frame is drawn.
func TestEditorBorderedView(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{Title: "notes.md", Border: true})
	v := s.View(sh)
	if !strings.HasPrefix(ansi.Strip(v), "┌─ notes.md ") {
		t.Fatalf("bordered View should open with the title legend, got %q", strings.SplitN(v, "\n", 2)[0])
	}
	if strings.Contains(v, core.RenderTitleBar("notes.md")) {
		t.Fatal("bordered View must not also draw the title bar")
	}
	if h := lipgloss.Height(v); h != 10 {
		t.Fatalf("bordered View height = %d, want the pane's full 10 rows", h)
	}

	typeRunes(s, 'x') // dirty
	if !strings.HasPrefix(ansi.Strip(s.View(sh)), "┌─ notes.md (*) ") {
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

// TestEditorInitLoadsOnce: reading the file is a one-time load, not something a host can
// re-trigger. A pane that swaps this editor out and back calls Init again
// (ScreenPanel.SetChild does it on every swap), and re-reading there would hand
// setContent the file — discarding the unsaved edits, undo history and cursor of the very
// buffer the swap-back exists to return to.
func TestEditorInitLoadsOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.md")
	if err := os.WriteFile(path, []byte("on disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, sh := newEditor(EditorOpts{Path: path})

	cmd := s.Init(sh)
	if cmd == nil {
		t.Fatal("the first Init should return the file read")
	}
	s.Update(sh, cmd())
	if got := buffer(s); got != "on disk" {
		t.Fatalf("buffer after the load = %q, want %q", got, "on disk")
	}

	typeRunes(s, '!') // an unsaved edit, which also moves the cursor and fills undo

	if cmd := s.Init(sh); cmd != nil {
		t.Fatal("a re-Init must not read the file a second time")
	}
	if got := buffer(s); got != "!on disk" {
		t.Fatalf("the unsaved edit should have survived the re-Init, buffer = %q", got)
	}
	if !s.Dirty() {
		t.Fatal("the dirty flag should have survived the re-Init")
	}
	if s.curX == 0 {
		t.Fatal("the cursor should have survived the re-Init")
	}
	if len(s.undoStack) == 0 {
		t.Fatal("the undo history should have survived the re-Init")
	}

	// A rename moves the identity, not the content (SetPath), so the new path is no more
	// worth reading than the old one was.
	s.SetPath(filepath.Join(dir, "renamed.md"))
	if cmd := s.Init(sh); cmd != nil {
		t.Fatal("a renamed buffer must not be read back off disk")
	}

	// The scratch buffer's leg: it has nothing to load, but a save-as gives it a path
	// afterwards — and that file is what the buffer just wrote, never a source to reload
	// from. This is what setting the flag before the empty-path return buys.
	scratch, ssh := newEditor(EditorOpts{})
	if cmd := scratch.Init(ssh); cmd != nil {
		t.Fatal("a path-less editor has nothing to read")
	}
	typeRunes(scratch, 'x')
	scratch.applySaveName(path) // as a save-as names it
	if cmd := scratch.Init(ssh); cmd != nil {
		t.Fatal("a saved-as scratch buffer must not be read back off disk")
	}
	if got := buffer(scratch); got != "x" {
		t.Fatalf("the scratch buffer should be untouched, got %q", got)
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
		s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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
	s.key(nil, keyMsg("alt+backspace"))
	if got := buffer(s); got != "foo r baz" {
		t.Fatalf("alt+backspace mid-word: buffer = %q, want %q", got, "foo r baz")
	}

	// At a word start (after spaces): deletes the PREVIOUS word and the spaces.
	s.setContent("foo   bar")
	s.curY, s.curX = 0, 6
	s.key(nil, keyMsg("ctrl+w"))
	if got := buffer(s); got != "bar" {
		t.Fatalf("ctrl+w past spaces: buffer = %q, want %q", got, "bar")
	}

	// Column 0 joins the line, exactly like backspace (one press per segment).
	s.setContent("one\ntwo")
	s.curY, s.curX = 1, 0
	s.key(nil, keyMsg("alt+backspace"))
	if got := buffer(s); got != "onetwo" {
		t.Fatalf("alt+backspace at col 0: buffer = %q, want %q", got, "onetwo")
	}

	// alt+delete removes the word under/ahead of the cursor…
	s.setContent("foo bar")
	s.curY, s.curX = 0, 1
	s.key(nil, keyMsg("alt+delete"))
	if got := buffer(s); got != "fbar" {
		t.Fatalf("alt+delete mid-word: buffer = %q, want %q", got, "fbar")
	}
	// …and at end of line pulls the next line up.
	s.setContent("ab\ncd")
	s.curY, s.curX = 0, 2
	s.key(nil, keyMsg("alt+delete"))
	if got := buffer(s); got != "abcd" {
		t.Fatalf("alt+delete at EOL: buffer = %q, want %q", got, "abcd")
	}

	// ctrl+u / ctrl+k delete to the line start / end.
	s.setContent("hello world")
	s.curY, s.curX = 0, 5
	s.key(nil, keyMsg("ctrl+u"))
	if got := buffer(s); got != " world" {
		t.Fatalf("ctrl+u: buffer = %q, want %q", got, " world")
	}
	s.setContent("hello world")
	s.curY, s.curX = 0, 5
	s.key(nil, keyMsg("ctrl+k"))
	if got := buffer(s); got != "hello" {
		t.Fatalf("ctrl+k: buffer = %q, want %q", got, "hello")
	}
}

func TestEditorBackwardWordDeleteSymbolBoundaries(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	altBackspace := keyMsg("alt+backspace")

	// Ordinary text stops at the nearest configured symbol; a contiguous symbol run
	// is its own token, so repeated presses peel an expression apart predictably.
	s.setContent("root/foo()[]{}.,|/bar")
	s.curY, s.curX = 0, len(s.lines[0])
	for _, want := range []string{
		"root/foo()[]{}.,|/",
		"root/foo",
		"root/",
		"root",
	} {
		s.key(nil, altBackspace)
		if got := buffer(s); got != want {
			t.Fatalf("successive symbol-aware alt+backspace = %q, want %q", got, want)
		}
	}

	// Trailing whitespace keeps the old behavior: it goes with the token before it,
	// while text after the cursor remains untouched.
	s.setContent("left/foo.bar   tail")
	s.curY, s.curX = 0, strings.Index(buffer(s), "tail")
	s.key(nil, altBackspace)
	if got, want := buffer(s), "left/foo.tail"; got != want {
		t.Fatalf("symbol boundary with spaces/suffix = %q, want %q", got, want)
	}

	// The requested list is exact: '-' remains part of an ordinary word.
	s.setContent("foo-bar")
	s.curY, s.curX = 0, len(s.lines[0])
	s.key(nil, altBackspace)
	if got := buffer(s); got != "" {
		t.Fatalf("unlisted hyphen should remain within the word, got %q", got)
	}
}

// TestEditorWordNav mirrors textinput's word jumps: alt/ctrl+left goes to the previous
// word start (wrapping to the previous line's end at column 0), alt/ctrl+right to the
// next word start (wrapping at end of line).
func TestEditorWordNav(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("foo bar\n  baz quux")

	s.curY, s.curX = 0, 5 // inside "bar"
	s.key(nil, keyMsg("alt+left"))
	if s.curX != 4 {
		t.Fatalf("alt+left to word start: curX = %d, want 4", s.curX)
	}
	s.key(nil, keyMsg("alt+left")) // previous word
	if s.curX != 0 {
		t.Fatalf("alt+left again: curX = %d, want 0", s.curX)
	}
	s.curY, s.curX = 1, 0
	s.key(nil, keyMsg("alt+left")) // col 0 → prev line end
	if s.curY != 0 || s.curX != 7 {
		t.Fatalf("alt+left wraps to prev line end: (%d,%d), want (0,7)", s.curY, s.curX)
	}

	s.curY, s.curX = 0, 0
	s.key(nil, keyMsg("alt+right")) // past "foo" and the space
	if s.curX != 4 {
		t.Fatalf("alt+right to next word: curX = %d, want 4", s.curX)
	}
	s.key(nil, keyMsg("ctrl+right")) // no more words on line 1 → its end
	if s.curY != 0 || s.curX != 7 {
		t.Fatalf("ctrl+right to line end: (%d,%d), want (0,7)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("ctrl+right")) // EOL → next line start
	if s.curY != 1 || s.curX != 0 {
		t.Fatalf("ctrl+right wraps to next line: (%d,%d), want (1,0)", s.curY, s.curX)
	}
	s.key(nil, keyMsg("alt+right")) // leading spaces → "baz"
	if s.curX != 2 {
		t.Fatalf("alt+right over leading spaces: curX = %d, want 2", s.curX)
	}
	s.key(nil, keyMsg("alt+right")) // past "baz" and the space → "quux"
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

	s.key(nil, keyMsg("ctrl+h"))
	if got := buffer(s); got != "ac" {
		t.Fatalf("ctrl+h: buffer = %q, want %q", got, "ac")
	}
	s.key(nil, keyMsg("ctrl+d"))
	if got := buffer(s); got != "a" {
		t.Fatalf("ctrl+d: buffer = %q, want %q", got, "a")
	}
	s.key(nil, keyMsg("ctrl+e"))
	if s.curX != 1 {
		t.Fatalf("ctrl+e: curX = %d, want 1", s.curX)
	}
	s.key(nil, keyMsg("ctrl+a"))
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

// TestEditorSetTextSuppressesTheInitRead: SetText is a load a host performs itself, so
// Init must not then read the file back over it. The suppression is the whole point —
// Init's read returns a message, and a message reaches this editor only while it is the
// top screen, so a host that is about to push something over it seeds the buffer instead
// of racing a read whose result would be dropped.
func TestEditorSetTextSuppressesTheInitRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, sh := newEditor(EditorOpts{Path: path})
	s.SetText("seeded")
	if cmd := s.Init(sh); cmd != nil {
		t.Fatal("Init must dispatch no read once the buffer has been seeded")
	}
	if got := buffer(s); got != "seeded" {
		t.Fatalf("buffer = %q, want the seeded text", got)
	}

	// A load, not an edit: clean, with nothing behind it to undo back to.
	if s.Dirty() {
		t.Error("a seeded buffer must be clean — nothing has been typed into it")
	}
	if len(s.undoStack) != 0 {
		t.Errorf("a seeded buffer must carry no undo history, got %d entries", len(s.undoStack))
	}

	// Called AFTER Init it is an ordinary buffer swap — the flag is already set, so
	// there is nothing left for it to suppress.
	s.SetText("replaced")
	if got := buffer(s); got != "replaced" {
		t.Fatalf("buffer = %q, want the replacement", got)
	}
}

// TestEditorExitCleanPops: ctrl+x on an unmodified buffer pops without a prompt.
func TestEditorExitCleanPops(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	_, act := s.key(nil, keyMsg("ctrl+x"))
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
	s.key(nil, keyMsg("enter"))
	typeRunes(s, 'y', 'o')

	s.key(nil, keyMsg("ctrl+x"))
	if !s.confirmExit {
		t.Fatal("ctrl+x on a dirty buffer should show the save prompt")
	}

	// While the prompt is up, other keys are swallowed.
	typeRunes(s, 'z')
	if got := buffer(s); got != "hi\nyo" {
		t.Fatalf("prompt mode must swallow typing, buffer = %q", got)
	}

	// c cancels back to editing.
	s.key(nil, keyMsg("c"))
	if s.confirmExit {
		t.Fatal("c should cancel the prompt")
	}

	// y pushes the filename prompt (nano's "File Name to Write"), seeded with the
	// buffer's name; enter saves, and the async result pops the screen.
	s.key(nil, keyMsg("ctrl+x"))
	_, act := s.Update(sh, keyMsg("y"))
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
	s.key(nil, keyMsg("ctrl+x"))
	_, act := s.key(nil, keyMsg("n"))
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
		s.key(sh, keyMsg("ctrl+s"))
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
		s.key(sh, keyMsg("ctrl+s"))
		after := filepath.Join(dir, "nested", "after.txt") // a dir that does not exist yet
		s.saveAsEdit(sh).OnDone(sh, after)                 // raises the confirm; nothing has moved yet
		if s.path == after {
			t.Fatal("the buffer must not adopt the new path before the confirm is answered")
		}
		s.saveAsConfirm(after).OnYes(sh) // the y the router would deliver
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
		s.key(sh, keyMsg("ctrl+s"))
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
		_, act := s.key(nil, keyMsg("ctrl+x"))
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
		s.key(nil, keyMsg("ctrl+x"))
		_, act := s.key(nil, keyMsg("n"))
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
		s.key(nil, keyMsg("ctrl+x"))
		// Through the real "y": it is what marks this save as the exit path's, and
		// so what makes the write end in the hook rather than in a plain save. It
		// needs the real Shared — the prompt it pushes anchors off the body.
		s.key(sh, keyMsg("y"))
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

	s.key(nil, keyMsg("ctrl+x"))
	s.key(sh, keyMsg("y"))
	edit := s.saveAsEdit(sh)
	if got := edit.input.Value(); got != old {
		t.Fatalf("the prompt should seed the full path, got %q", got)
	}
	edit.OnDone(sh, "  ") // blank: a quiet cancel
	if s.path != old {
		t.Fatalf("a blank name must not touch the path, got %q", s.path)
	}
	renamed := filepath.Join(dir, "renamed.txt")
	edit.OnDone(sh, renamed)           // the confirm, not the write
	s.saveAsConfirm(renamed).OnYes(sh) // answered yes
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
	scratch.key(nil, keyMsg("ctrl+x"))
	scratch.key(sh2, keyMsg("y"))
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

// TestEditorSaveAsConfirm: the save-as box is seeded with the buffer's own path, so a
// changed name is one stray keystroke away from moving the document — the write goes
// through a y/n confirm now. An UNCHANGED name is a plain save and must still be silent,
// and so must a first save of a scratch buffer, which has no old file to leave behind.
func TestEditorSaveAsConfirm(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(old, []byte("orig"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, sh := newEditor(EditorOpts{Path: old})
	s.Update(sh, s.Init(sh)())
	typeRunes(s, '!')

	// Unchanged: no confirm stands between the enter and the write.
	s.saveAsEdit(sh).OnDone(sh, old)
	s.Update(sh, s.saveCmd()())
	if b, _ := os.ReadFile(old); string(b) != "!orig" {
		t.Fatalf("an unchanged name should save straight through, got %q", b)
	}

	// Changed: nothing moves and nothing is written until the confirm is answered.
	moved := filepath.Join(dir, "moved.txt")
	s.saveAsEdit(sh).OnDone(sh, moved)
	if s.path != old || s.title != "old.txt" {
		t.Fatalf("the buffer must not move before the y, got path %q title %q", s.path, s.title)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Fatalf("nothing should be written before the y, stat err = %v", err)
	}

	// The confirm names both halves: where the buffer is going, and what stays behind.
	dlg := s.saveAsConfirm(moved)
	body := ansi.Strip(dlg.Render(sh))
	for _, want := range []string{"moved.txt", "old.txt", "stays on disk"} {
		if !strings.Contains(body, want) {
			t.Errorf("the confirm should say %q; got:\n%s", want, body)
		}
	}
	// Another folder is a move, so the whole target path is shown, not just the name.
	elsewhere := filepath.Join(dir, "sub", "moved.txt")
	if b := ansi.Strip(s.saveAsConfirm(elsewhere).Render(sh)); !strings.Contains(b, elsewhere) {
		t.Errorf("a move to another folder should name the folder; got:\n%s", b)
	}

	// No: the dialog pops back to the still-filled box, leaving the buffer alone.
	if _, act := dlg.Update(sh, keyMsg("esc")); act.Msg == nil {
		t.Error("esc should pop the confirm")
	}
	if s.path != old {
		t.Fatalf("a cancelled confirm must not move the buffer, got %q", s.path)
	}

	// Yes: the buffer takes the new path, and the old file keeps what was last saved
	// to it — this is a save-as, not an os.Rename.
	dlg.OnYes(sh)
	s.Update(sh, s.saveCmd()())
	if s.path != moved || s.title != "moved.txt" {
		t.Fatalf("yes should move the buffer, got path %q title %q", s.path, s.title)
	}
	if b, err := os.ReadFile(moved); err != nil || string(b) != "!orig" {
		t.Fatalf("the new path should hold the buffer: %q, err = %v", b, err)
	}
	if b, err := os.ReadFile(old); err != nil || string(b) != "!orig" {
		t.Fatalf("the old file stays as last saved: %q, err = %v", b, err)
	}

	// A scratch buffer's first save has no old file to warn about.
	scratch, sh2 := newEditor(EditorOpts{})
	typeRunes(scratch, 'n')
	fresh := filepath.Join(dir, "fresh.txt")
	scratch.saveAsEdit(sh2).OnDone(sh2, fresh)
	if scratch.path != fresh {
		t.Fatalf("a first save should not prompt, got %q", scratch.path)
	}
}

// TestEditorSaveAsExpandsHome: a "~/..." name at the filename prompt resolves to the
// home directory rather than to a directory literally named "~" under the CWD — the
// buffer takes the RESOLVED path, so the write, the title and the path reported to
// OnSaved all name the same file. A "~user" form is refused: nothing is written, the
// buffer keeps the path it had, and no "~user" directory is left behind.
func TestEditorSaveAsExpandsHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var saved string
	s, sh := newEditor(EditorOpts{OnSaved: func(_ *core.Shared, p string) core.Action {
		saved = p
		return core.Action{}
	}})
	typeRunes(s, 'h', 'i')

	s.saveAsEdit(sh).OnDone(sh, "~/notes.md")
	want := filepath.Join(home, "notes.md")
	if s.path != want || s.title != "notes.md" {
		t.Fatalf("~/notes.md should resolve to %q, got path %q title %q", want, s.path, s.title)
	}
	s.Update(sh, s.saveCmd()())
	if b, err := os.ReadFile(want); err != nil || string(b) != "hi" {
		t.Fatalf("the expanded path should hold the buffer: %q, err = %v", b, err)
	}
	if saved != want {
		t.Fatalf("OnSaved should report the expanded path, got %q", saved)
	}

	s.saveAsEdit(sh).OnDone(sh, "~someone/notes.md")
	if s.path != want {
		t.Fatalf("a refused ~user name must not touch the path, got %q", s.path)
	}
	if _, err := os.Lstat("~someone"); err == nil {
		t.Fatal(`a refused ~user name must not create a "~someone" directory`)
	}
}

// longDoc is a 100-line buffer, taller than any test viewport.
func longDoc() string {
	return strings.TrimSuffix(strings.Repeat("x\n", 100), "\n")
}

func wheel(s *EditorScreen, sh *core.Shared, btn tea.MouseButton) {
	s.Update(sh, tea.MouseWheelMsg{X: 5, Y: 5, Button: btn})
}

// TestEditorWheelScroll: the wheel browses the buffer without moving the caret,
// clamped at both ends; a caret move afterwards snaps the view back to it
// (clampScroll runs after every keystroke).
func TestEditorWheelScroll(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc())

	wheel(s, sh, tea.MouseWheelDown)
	if s.scrY != editorWheelStep {
		t.Fatalf("wheel down → scrY %d, want %d", s.scrY, editorWheelStep)
	}
	if s.curY != 0 || s.curX != 0 {
		t.Fatalf("the wheel must not move the caret, got (%d,%d)", s.curY, s.curX)
	}
	wheel(s, sh, tea.MouseWheelUp)
	wheel(s, sh, tea.MouseWheelUp) // past the top: clamps
	if s.scrY != 0 {
		t.Fatalf("wheel up past the top → scrY %d, want 0", s.scrY)
	}
	for i := 0; i < 40; i++ {
		wheel(s, sh, tea.MouseWheelDown)
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
	s.key(nil, keyMsg("down"))
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
	wheel(s, sh, tea.MouseWheelDown)
	if s.scrY != 0 {
		t.Fatalf("an unfocused pane must not scroll, scrY = %d", s.scrY)
	}
	s.SetFocused(true)
	wheel(s, sh, tea.MouseWheelDown)
	if s.scrY != editorWheelStep {
		t.Fatalf("focused wheel down → scrY %d, want %d", s.scrY, editorWheelStep)
	}

	s.dirty = true
	s.key(nil, keyMsg("ctrl+x"))
	if !s.confirmExit {
		t.Fatal("dirty ctrl+x should raise the exit prompt")
	}
	before := s.scrY // raising the prompt re-clamps the view to the caret
	wheel(s, sh, tea.MouseWheelDown)
	if s.scrY != before {
		t.Fatalf("the prompt suspends the wheel: scrY %d, want %d", s.scrY, before)
	}
}

func altWheel(s *EditorScreen, sh *core.Shared, btn tea.MouseButton) {
	s.Update(sh, tea.MouseWheelMsg{X: 5, Y: 5, Button: btn, Mod: tea.ModAlt})
}

// wideDoc is one 200-cell line between two short ones: a buffer that overflows the
// 80-column viewport horizontally but not vertically, so no scrollbar narrows textW.
func wideDoc() string {
	return "short\n" + strings.Repeat("x", 200) + "\nalso short"
}

// TestEditorHorizontalWheel: the horizontal wheel buttons a trackpad swipe emits browse
// sideways without moving the caret, floored at 0 and bounded by the widest line.
func TestEditorHorizontalWheel(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(wideDoc())

	wheel(s, sh, tea.MouseWheelRight)
	if s.scrX != editorHWheelStep {
		t.Fatalf("wheel right → scrX %d, want %d", s.scrX, editorHWheelStep)
	}
	if s.curY != 0 || s.curX != 0 {
		t.Fatalf("the horizontal wheel must not move the caret, got (%d,%d)", s.curY, s.curX)
	}
	if s.scrY != 0 {
		t.Fatalf("the horizontal wheel must not scroll vertically, scrY = %d", s.scrY)
	}

	wheel(s, sh, tea.MouseWheelLeft)
	wheel(s, sh, tea.MouseWheelLeft) // past the left edge: clamps
	if s.scrX != 0 {
		t.Fatalf("wheel left past the edge → scrX %d, want 0", s.scrX)
	}

	// Right past the end of the widest line: scrX stops one column past its last cell,
	// where clampScroll would park it for a caret at end-of-line.
	want := 200 - s.contentW() + 1
	if got := s.maxScrollX(); got != want {
		t.Fatalf("maxScrollX = %d, want %d (widest - contentW + 1)", got, want)
	}
	for i := 0; i < 60; i++ {
		wheel(s, sh, tea.MouseWheelRight)
	}
	if s.scrX != want {
		t.Fatalf("wheel right at the end → scrX %d, want %d", s.scrX, want)
	}
	// Bounded, not blank: the window still lands on text rather than past the line.
	if row := strings.Split(s.body(), "\n")[1]; !strings.Contains(row, "x") {
		t.Fatalf("the fully-scrolled window shows no text: %q", row)
	}
	assertRowGeometry(t, s, "scrolled fully right")

	// The router re-lays out after every message; the browse position must survive it.
	s.SetSize(sh, 80, 20)
	if s.scrX != want {
		t.Fatalf("SetSize undid the horizontal scroll: scrX %d, want %d", s.scrX, want)
	}

	// The caret sits far left of the view; one arrow snaps the view back to it. Landing
	// ON the caret's cell rather than at column 0 is clampScroll's "just enough" rule.
	s.key(nil, keyMsg("right"))
	if s.curX != 1 || s.scrX != 1 {
		t.Fatalf("right with the caret off-screen → (curX %d, scrX %d), want (1, 1)", s.curX, s.scrX)
	}
}

// TestEditorAltWheelHorizontal: ⌥ turns the vertical wheel sideways, for the terminals
// and mice that never emit the horizontal buttons. Without ⌥ it stays vertical.
func TestEditorAltWheelHorizontal(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(wideDoc())

	altWheel(s, sh, tea.MouseWheelDown)
	if s.scrX != editorHWheelStep || s.scrY != 0 {
		t.Fatalf("alt+wheel down → (scrX %d, scrY %d), want (%d, 0)", s.scrX, s.scrY, editorHWheelStep)
	}
	altWheel(s, sh, tea.MouseWheelUp)
	if s.scrX != 0 {
		t.Fatalf("alt+wheel up → scrX %d, want 0", s.scrX)
	}

	// The plain wheel is untouched: vertical only, scrX left alone.
	s.setContent(longDoc())
	wheel(s, sh, tea.MouseWheelDown)
	if s.scrY != editorWheelStep || s.scrX != 0 {
		t.Fatalf("plain wheel down → (scrY %d, scrX %d), want (%d, 0)", s.scrY, s.scrX, editorWheelStep)
	}
}

// TestEditorHorizontalWheelSuppressed: the horizontal wheel obeys the same guards as the
// vertical one — an unfocused pane must not roll — and wrap leaves nothing to roll to.
func TestEditorHorizontalWheelSuppressed(t *testing.T) {
	s, sh := newPaneEditor(EditorOpts{})
	s.setContent(wideDoc())

	s.SetFocused(false)
	wheel(s, sh, tea.MouseWheelRight)
	altWheel(s, sh, tea.MouseWheelDown)
	if s.scrX != 0 {
		t.Fatalf("an unfocused pane must not scroll sideways, scrX = %d", s.scrX)
	}

	s.SetFocused(true)
	wheel(s, sh, tea.MouseWheelRight)
	if s.scrX != editorHWheelStep {
		t.Fatalf("focused wheel right → scrX %d, want %d", s.scrX, editorHWheelStep)
	}

	// Wrapped, every cell of a line is on screen already and renderWrappedRow windows on
	// the chunk start, so scrX is inert — the gesture is a no-op and the position it had
	// unwrapped is carried across untouched.
	before := s.scrX
	s.ToggleWrap()
	wheel(s, sh, tea.MouseWheelRight)
	altWheel(s, sh, tea.MouseWheelDown)
	if s.scrX != before {
		t.Fatalf("wrapped, the horizontal wheel must be a no-op: scrX %d, want %d", s.scrX, before)
	}
	// No assertRowGeometry here: an embedded pane already renders gutter() cells wider
	// than s.w with scrX at 0 (body prepends the inset pad that textW never subtracts),
	// so the invariant is broken before the wheel ever runs. TestEditorHorizontalWheel
	// asserts it on the unembedded editor instead.
	s.ToggleWrap()
	if s.scrX != before {
		t.Fatalf("unwrapping lost the horizontal position: scrX %d, want %d", s.scrX, before)
	}
}

// TestEditorScrollbar: a buffer taller than the viewport draws a proportional
// scrollbar in the rightmost column — a thumb sized to the viewport's share of the
// buffer, placed by scrY — and the text window narrows one column so the caret
// never hides under the bar. Track and thumb share the one "│" glyph; the color
// splits them (thumb in the focus color, track dimmed). A short buffer keeps the
// full width and no bar.
func TestEditorScrollbar(t *testing.T) {
	// The thumb/track split is color-only now: the styling has to be observable.
	s, _ := newEditor(EditorOpts{}) // standalone: no gutter, the bar is the last cell
	s.setContent("a\nb\nc")
	if s.barVisible() || s.textW() != s.w {
		t.Fatalf("a buffer that fits: barVisible=%v textW=%d, want no bar and full %d", s.barVisible(), s.textW(), s.w)
	}
	if strings.ContainsRune(s.body(), '│') {
		t.Fatal("no scrollbar cells without overflow")
	}

	s.setContent(longDoc())
	if !s.barVisible() || s.textW() != s.w-1 {
		t.Fatalf("overflow: barVisible=%v textW=%d, want the bar and w-1 (%d)", s.barVisible(), s.textW(), s.w-1)
	}
	thumb := s.h * s.h / len(s.lines)
	trackCell := lipgloss.NewStyle().Foreground(core.MutedColor).Render("│")
	thumbCell := lipgloss.NewStyle().Foreground(core.FocusedColor).Render("│")
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
			want := trackCell
			if i >= top && i < top+thumb {
				want = thumbCell
			}
			if !strings.HasSuffix(row, want) {
				t.Fatalf("row %d bar cell = %q, want suffix %q (scrY %d)", i, row, want, s.scrY)
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
	s.key(nil, keyMsg("ctrl+e"))
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
		s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
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
	if want := sh.BodyY() + s.paneH - 2; edit.y != want {
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
	if want := 4 + pane.paneH - 2; edit.y != want {
		t.Fatalf("embedded y = %d, want %d", edit.y, want)
	}

	// A retained search shrinks only the text viewport; save-as remains anchored to
	// the pane's actual bottom and covers the search bar rather than drifting above it.
	setSearchQuery(pane, "needle")
	pane.SetSize(psh, 40, 10)
	edit = pane.saveAsEdit(psh)
	if want := 4 + pane.paneH - 2; edit.y != want {
		t.Fatalf("embedded y with search = %d, want pane-bottom anchor %d", edit.y, want)
	}
}

// ---------- soft wrap and line numbers ----------

// assertRowGeometry is the invariant every render owes the frame around it: exactly
// s.h rows, none wider than s.w, and — once the scrollbar is up — every one of them
// exactly s.w so the bar reads as a solid column. A row one cell over wraps in the
// terminal and shifts every frame after it.
func assertRowGeometry(t *testing.T, s *EditorScreen, ctx string) {
	t.Helper()
	rows := strings.Split(s.body(), "\n")
	if len(rows) != s.h {
		t.Fatalf("%s: body = %d rows, want %d", ctx, len(rows), s.h)
	}
	bar := s.barVisible()
	for i, row := range rows {
		w := lipgloss.Width(row)
		if w > s.w {
			t.Fatalf("%s: row %d overflows the frame: %d cells, want at most %d", ctx, i, w, s.w)
		}
		if bar && w != s.w {
			t.Fatalf("%s: row %d = %d cells, want %d padded out to the bar", ctx, i, w, s.w)
		}
	}
}

// caretRow is the body row carrying the reverse-video caret (-1 for none). Only
// meaningful because v2 renders styles verbatim: downsampling moved to the output layer.
func caretRow(s *EditorScreen) int {
	for i, row := range strings.Split(s.body(), "\n") {
		if strings.Contains(row, "\x1b[7m") {
			return i
		}
	}
	return -1
}

// TestEditorWrapGeometry is the regression wrap shipped without: nothing about the
// wrapped render may recurse, panic, or overrun the frame. The original cycle ran
// barVisible → wrapTotalRows → the rebuild → textW → barVisible and never terminated
// (a stack overflow, which no recover can catch — the terminal is left raw); the row
// pad then double-counted the number gutter and the scrollbar cell was written whether
// or not the bar was up.
func TestEditorWrapGeometry(t *testing.T) {
	for _, tc := range []struct{ name, doc string }{
		{"empty", ""},
		{"one wrapped line", strings.Repeat("z", 300)},
		{"more lines than rows", longDoc()},
		{"wrapped past the viewport", strings.Repeat(strings.Repeat("q", 250)+"\n", 30)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{})
			s.setContent(tc.doc)
			s.ToggleWrap()
			assertRowGeometry(t, s, "wrapped")
			s.key(nil, keyMsg("ctrl+e")) // caret to end of line
			assertRowGeometry(t, s, "wrapped, caret at EOL")
			s.ToggleLineNums()
			assertRowGeometry(t, s, "wrapped, preference flipped")
			s.ToggleWrap()
			assertRowGeometry(t, s, "unwrapped with numbers")
		})
	}
}

// TestEditorWrapCaret: the caret is drawn on the row the scroll math scrolls to, for a
// caret mid-line and at end of line — including at the end of a line that fills its
// last row exactly, where the row the caret needs has to be made or the caret cell
// lands one column past the frame.
func TestEditorWrapCaret(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent(strings.Repeat("a", 500))
	s.ToggleWrap()

	for _, at := range []string{"home", "end"} {
		if at == "home" {
			s.key(nil, keyMsg("ctrl+a"))
		} else {
			s.key(nil, keyMsg("ctrl+e"))
		}
		if got, want := caretRow(s), s.wrapRowForCursor()-s.scrY; got != want {
			t.Fatalf("caret at %s drawn on row %d, want %d", at, got, want)
		}
		if n := strings.Count(s.body(), "\x1b[7m"); n != 1 {
			t.Fatalf("caret at %s: %d caret cells drawn, want exactly 1", at, n)
		}
	}

	// A line filling its last row exactly: the trailing row is the caret's.
	w := s.contentW()
	s.setContent(strings.Repeat("b", w*2))
	s.key(nil, keyMsg("ctrl+e"))
	if got := s.wrapTotalRows(); got != 3 {
		t.Fatalf("a line of exactly %d cells = %d rows, want 3 (two full, one for the caret)", w*2, got)
	}
	if got, want := caretRow(s), 2; got != want {
		t.Fatalf("caret at the end of a full line drawn on row %d, want %d", got, want)
	}
	assertRowGeometry(t, s, "caret at the end of an exactly-full line")
}

// TestEditorWrapCaretHighlighted: the styled render places the caret from its window's
// origin, which wrapped is the CHUNK's start — reading scrX (which wrap never moves)
// pinned the caret to the line's first row no matter which row it was really on.
func TestEditorWrapCaretHighlighted(t *testing.T) {
	s, _ := newEditor(EditorOpts{Highlighter: NewMarkdownHighlighter()})
	s.setContent(strings.Repeat("a", 300))
	s.ToggleWrap()
	if s.hl == nil {
		t.Fatal("the highlighter should be in place: this test is about the styled path")
	}
	s.key(nil, keyMsg("ctrl+e"))
	if got, want := caretRow(s), s.wrapRowForCursor()-s.scrY; got != want {
		t.Fatalf("highlighted caret drawn on row %d, want %d", got, want)
	}
	if n := strings.Count(s.body(), "\x1b[7m"); n != 1 {
		t.Fatalf("%d caret cells drawn, want exactly 1", n)
	}
	assertRowGeometry(t, s, "highlighted and wrapped")

	// The same for a line ending exactly on the margin, where the row before the
	// caret's also sees a caret one cell past its own chunk.
	s.setContent(strings.Repeat("b", s.contentW()*2))
	s.key(nil, keyMsg("ctrl+e"))
	if n := strings.Count(s.body(), "\x1b[7m"); n != 1 {
		t.Fatalf("exactly-full line: %d caret cells drawn, want exactly 1", n)
	}
	if got, want := caretRow(s), 2; got != want {
		t.Fatalf("exactly-full line: caret on row %d, want %d", got, want)
	}
}

// TestEditorWrapScroll: scrY counts display rows while wrapped, so the wheel's clamp
// has to as well — clamping against the LINE count stopped the view a long way short
// of the end of a wrapped document.
func TestEditorWrapScroll(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(strings.Repeat(strings.Repeat("m", 200)+"\n", 12))
	s.ToggleWrap()
	total := s.wrapTotalRows()
	if total <= len(s.lines) {
		t.Fatalf("this doc should wrap to more rows than lines: %d rows, %d lines", total, len(s.lines))
	}
	for i := 0; i < total; i++ {
		wheel(s, sh, tea.MouseWheelDown)
	}
	if want := total - s.h; s.scrY != want {
		t.Fatalf("wheel to the bottom → scrY %d, want %d (the last full screen of rows)", s.scrY, want)
	}
	assertRowGeometry(t, s, "scrolled to the bottom")
}

// TestEditorWrapClick: a click on a continuation row lands in that chunk of its line,
// not on the buffer line that happens to share the row's index.
func TestEditorWrapClick(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("short\n" + strings.Repeat("c", 300))
	s.ToggleWrap()
	r := s.wrapRows[2] // row 0 is "short", row 1 the long line's first chunk
	if r.line != 1 || r.start == 0 {
		t.Fatalf("row 2 = %+v, want a continuation of line 1", r)
	}
	s.Update(sh, tea.MouseClickMsg{X: s.leftGutterWidth() + 4, Y: s.titleH() + 2, Button: tea.MouseLeft})
	if s.curY != r.line || s.curX != r.start+4 {
		t.Fatalf("click on a wrapped row = (%d,%d), want (%d,%d)", s.curY, s.curX, r.line, r.start+4)
	}
}

// TestEditorLineNumbers: ctrl+l sets a sticky preference that decides the gutter while
// wrap is off. Wrap forces the gutter on — soft breaks are indistinguishable from real
// ones without it — without disturbing the preference, so unwrapping goes back to
// whatever the user last chose.
func TestEditorLineNumbers(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("alpha\nbeta")
	first := func() string { return ansi.Strip(strings.Split(s.body(), "\n")[0]) }

	if got := first(); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("no gutter by default, got %q", got)
	}
	s.ToggleLineNums()
	if got := first(); !strings.HasPrefix(got, "1 alpha") {
		t.Fatalf("ctrl+l unwrapped should number the rows, got %q", got)
	}
	s.ToggleLineNums()
	if got := first(); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("ctrl+l again should take the gutter away, got %q", got)
	}

	s.ToggleWrap()
	if got := first(); !strings.HasPrefix(got, "1 alpha") {
		t.Fatalf("wrap should number the rows whatever the preference, got %q", got)
	}
	if s.LineNumMode() {
		t.Fatal("wrap must not change the preference itself")
	}
	s.ToggleWrap()
	if got := first(); !strings.HasPrefix(got, "alpha") {
		t.Fatalf("unwrapping should go back to the preference, got %q", got)
	}
}

// TestEditorWrapToggleKeepsView: scrY means display rows one side of the toggle and
// buffer lines the other, so it is translated across it. Reinterpreted, unwrapping
// from deep in a document lands past the last line and the viewport goes blank.
func TestEditorWrapToggleKeepsView(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent(strings.Repeat(strings.Repeat("k", 200)+"\n", 60))
	s.ToggleWrap()
	s.scrY = 40
	top := s.wrapRows[40].line

	s.ToggleWrap()
	if s.scrY != top {
		t.Fatalf("unwrapped top line = %d, want %d", s.scrY, top)
	}
	if s.scrY >= len(s.lines) {
		t.Fatalf("scrY %d is past the last line (%d): the viewport would be blank", s.scrY, len(s.lines))
	}
	s.ToggleWrap()
	if got := s.wrapRows[s.scrY].line; got != top {
		t.Fatalf("re-wrapped top line = %d, want %d", got, top)
	}
}

// TestEditorScrollAnchors: what a host syncing its own scroll to the editor reads —
// the line at the top, the line at the middle, and the scroll extent in display ROWS.
// Wrapped, one line is several rows, so the two answers have to come off the wrap
// cache rather than off the buffer index; unwrapped they are the same number.
func TestEditorScrollAnchors(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc()) // 100 one-char lines, 20 rows of viewport

	off, maxOff, height := s.ScrollSpan()
	if off != 0 || maxOff != len(s.lines)-s.h || height != s.h {
		t.Fatalf("at rest: ScrollSpan = (%d, %d, %d), want (0, %d, %d)",
			off, maxOff, height, len(s.lines)-s.h, s.h)
	}
	if s.TopLine() != 0 || s.CenterLine() != s.h/2 {
		t.Fatalf("at rest: top %d center %d, want 0 and %d", s.TopLine(), s.CenterLine(), s.h/2)
	}

	for i := 0; i < 10; i++ {
		wheel(s, sh, tea.MouseWheelDown)
	}
	off, _, _ = s.ScrollSpan()
	if off == 0 {
		t.Fatal("the wheel should have scrolled the view")
	}
	if got, want := s.CenterLine(), off+s.h/2; got != want {
		t.Fatalf("unwrapped center line = %d, want %d (offset %d)", got, want, off)
	}

	// Bottomed out, both anchors clamp to the buffer rather than running past it.
	for i := 0; i < 200; i++ {
		wheel(s, sh, tea.MouseWheelDown)
	}
	if off, maxOff, _ = s.ScrollSpan(); off != maxOff {
		t.Fatalf("the wheel should bottom out at %d, got %d", maxOff, off)
	}
	if got := s.CenterLine(); got >= len(s.lines) {
		t.Fatalf("center line %d is past the buffer's %d lines", got, len(s.lines))
	}

	// Wrapped: the span is measured in wrapped rows, and the anchors answer buffer
	// lines — the same line for every row it wraps over.
	s.setContent(strings.Repeat(strings.Repeat("q", 250)+"\n", 30))
	s.ToggleWrap()
	if _, maxOff, _ = s.ScrollSpan(); maxOff != s.wrapTotalRows()-s.h {
		t.Fatalf("wrapped: max offset %d, want %d rows", maxOff, s.wrapTotalRows()-s.h)
	}
	for i := 0; i < 5; i++ {
		wheel(s, sh, tea.MouseWheelDown)
	}
	off, _, _ = s.ScrollSpan()
	if got, want := s.TopLine(), s.wrapRows[off].line; got != want {
		t.Fatalf("wrapped: top line = %d, want %d (row %d)", got, want, off)
	}
	if got, want := s.CenterLine(), s.wrapRows[off+s.h/2].line; got != want {
		t.Fatalf("wrapped: center line = %d, want %d (row %d)", got, want, off+s.h/2)
	}
}

// ---------- horizontal overflow marker ----------

// TestEditorOverflowMark: a line the window cuts off ends in the marker, one that fits
// does not, and either way the row measures the same — the marker replaces a text cell
// rather than adding one, which is what keeps the frame (and the scrollbar column) put.
func TestEditorOverflowMark(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent(strings.Repeat("y", 200) + "\nshort")
	rows := strings.Split(s.body(), "\n")
	if !strings.HasSuffix(ansi.Strip(rows[0]), string(editorOverflowMark)) {
		t.Errorf("a clipped line should end in %q, got %q", editorOverflowMark, rows[0])
	}
	if lipgloss.Width(rows[0]) != s.contentW() {
		t.Errorf("marked row = %d cells, want %d", lipgloss.Width(rows[0]), s.contentW())
	}
	if strings.ContainsRune(rows[1], editorOverflowMark) {
		t.Errorf("a line that fits should carry no marker: %q", rows[1])
	}
	assertRowGeometry(t, s, "overflow marker")

	// Scrolled to the line's end there is nothing left to mark.
	s.key(nil, keyMsg("ctrl+e"))
	if row := strings.Split(s.body(), "\n")[0]; strings.HasSuffix(row, string(editorOverflowMark)) {
		t.Errorf("the end of a line should carry no marker: %q", row)
	}

	// Wrapped, every cell is on screen already.
	s.ToggleWrap()
	if strings.ContainsRune(s.body(), editorOverflowMark) {
		t.Error("wrapped rows must not draw the overflow marker")
	}
	s.ToggleWrap()

	// The gutter and the scrollbar both narrow the window the marker sits at the end
	// of; the rows still owe the frame their exact width.
	s.setContent(strings.Repeat(strings.Repeat("y", 200)+"\n", 40))
	s.ToggleLineNums()
	row := strings.Split(s.body(), "\n")[0]
	if !s.barVisible() {
		t.Fatal("40 lines over 20 rows should raise the scrollbar")
	}
	if cells := []rune(ansi.Strip(row)); cells[len(cells)-2] != editorOverflowMark {
		t.Errorf("clipped row with a gutter and a bar = %q, want the marker in the cell before the bar", row)
	}
	assertRowGeometry(t, s, "overflow marker with gutter and bar")
}

// TestEditorOverflowMarkKeepsCaret: walking right along a long line must never park the
// caret in the column the marker claims — a caret painted over is a lie about where
// typing lands.
func TestEditorOverflowMarkKeepsCaret(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent(strings.Repeat("y", 200))
	for i := 0; i < 120; i++ {
		s.key(nil, keyMsg("right"))
		row := strings.Split(s.body(), "\n")[0]
		if !strings.Contains(row, "\x1b[7m") {
			t.Fatalf("step %d: the caret left the row: %q", i, row)
		}
		if cell := cellOfCol(s.lines[0], s.curX); cell == s.scrX+s.contentW()-1 && len(s.lines[0]) > s.scrX+s.contentW() {
			t.Fatalf("step %d: caret sits in the marker column (scrX %d)", i, s.scrX)
		}
	}
	assertRowGeometry(t, s, "caret walk")
}
