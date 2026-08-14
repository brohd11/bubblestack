package components

import (
	"errors"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

var errFakeClipboard = errors.New("no clipboard")

// rightPress is the gesture under test throughout this file.
func rightPress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonRight, X: x, Y: y}
}

// stubClipboard swaps both clipboard seams for the duration of one test and returns a
// pointer to whatever the editor wrote. read is what a paste will find.
func stubClipboard(t *testing.T, read string) *string {
	t.Helper()
	oldWrite, oldRead := writeEditorClipboard, readEditorClipboard
	t.Cleanup(func() { writeEditorClipboard, readEditorClipboard = oldWrite, oldRead })
	var wrote string
	writeEditorClipboard = func(text string) error { wrote = text; return nil }
	readEditorClipboard = func() (string, error) { return read, nil }
	return &wrote
}

// TestEditorRightPressWithoutOptIsInert: the menu is opt-in, and off it takes the right
// button back entirely — no menu, and none of the caret/selection side effects the opt-in
// path has.
func TestEditorRightPressWithoutOptIsInert(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	_, act := s.Update(sh, rightPress(5, s.titleH()))
	if act.Msg != nil || act.Cmd != nil {
		t.Fatalf("a right press without ContextMenu should do nothing, got %#v", act)
	}
	if got := s.selectedText(); got != "bcd" || s.curX != 4 {
		t.Fatalf("the inert press moved things: selection %q caret %d, want %q and 4", got, s.curX, "bcd")
	}
}

// TestEditorRightPressInsideSelectionKeepsIt is the "inside" half of the desktop
// convention: the menu is about to act on the highlighted text, so the press must not
// disturb it.
func TestEditorRightPressInsideSelectionKeepsIt(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	_, act := s.Update(sh, rightPress(2, s.titleH()))
	if act.Msg == nil {
		t.Fatal("a right press should open the context menu")
	}
	if got := s.selectedText(); got != "bcd" || s.curX != 4 {
		t.Fatalf("pressing inside the selection changed it: %q caret %d, want %q and 4", got, s.curX, "bcd")
	}
}

// TestEditorRightPressOffTextOpensNothing: a press that maps to no buffer position has no
// position for the menu to act on, and a menu raised there would silently act on wherever
// the caret happened to be.
func TestEditorRightPressOffTextOpensNothing(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	if s.titleH() == 0 {
		t.Skip("no title bar to press on in this configuration")
	}
	_, act := s.Update(sh, rightPress(2, 0)) // the title-bar row
	if act.Msg != nil || s.curX != 0 {
		t.Fatalf("a press on the title bar opened %#v with caret %d, want nothing and caret 0", act.Msg, s.curX)
	}

	// The scrollbar column is likewise not buffer text.
	bar, shBar := newEditor(EditorOpts{ContextMenu: true})
	bar.setContent(strings.Repeat("line\n", 200))
	if !bar.barVisible() {
		t.Fatal("200 lines in a 20-row editor should raise the scrollbar; fixture is wrong")
	}
	if _, act := bar.Update(shBar, rightPress(bar.textW(), bar.titleH())); act.Msg != nil {
		t.Fatalf("a press on the scrollbar opened %#v, want nothing", act.Msg)
	}
}

// TestEditorContextMenuRows pins the row set: what is disabled, and that a host's extras
// arrive below a rule with no dangling separator when there are none.
func TestEditorContextMenuRows(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")

	m := s.editMenu(sh, 0, 0)
	if len(m.items) != 3 {
		t.Fatalf("a menu with no host extras has %d rows, want 3", len(m.items))
	}
	if !m.items[0].Disabled || !m.items[1].Disabled {
		t.Error("Copy and Cut should be disabled without a selection")
	}
	if m.items[2].Disabled {
		t.Error("Paste is never disabled: the clipboard cannot be read on the render tick")
	}

	selectRange(s, 0, 1, 0, 4)
	m = s.editMenu(sh, 0, 0)
	if m.items[0].Disabled || m.items[1].Disabled {
		t.Error("Copy and Cut should be live with a selection")
	}

	// A nil return adds nothing at all — no trailing separator.
	empty, shEmpty := newEditor(EditorOpts{
		ContextMenu:  true,
		ContextItems: func(*core.Shared) []MenuItem { return nil },
	})
	if got := len(empty.editMenu(shEmpty, 0, 0).items); got != 3 {
		t.Errorf("nil extras produced %d rows, want 3 with no dangling separator", got)
	}

	extra, shExtra := newEditor(EditorOpts{
		ContextMenu:  true,
		ContextItems: func(*core.Shared) []MenuItem { return []MenuItem{{Label: "Toggle wrap"}} },
	})
	rows := extra.editMenu(shExtra, 0, 0).items
	if len(rows) != 5 || !rows[3].Separator || rows[4].Label != "Toggle wrap" {
		t.Errorf("host extras should land below a rule, got %+v", rows)
	}
}

// openContextMenu right-presses on text row `row` (0 = the first visible line) and returns
// the menu that came up. It goes through a real router because core.Push's payload is
// unexported: the stack is the only place the pushed screen can be observed from here.
func openContextMenu(t *testing.T, termH int, content string, setup func(*EditorScreen),
	x, row int) (*MenuScreen, *EditorScreen, int) {
	t.Helper()
	s := NewEditorScreen(EditorOpts{ContextMenu: true})
	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{
		Title: "T", New: func(*core.Shared) core.Screen { return s },
	}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: termH})
	s.setContent(content)
	if setup != nil {
		setup(s)
	}
	textTop := sh.BodyY() + s.titleH() // the row the first buffer line renders on
	if row < 0 {
		row += s.h // negative counts back from the viewport's last row
	}
	tm, _ = tm.Update(rightPress(x, textTop+row))
	menu, ok := tm.(core.Router).Top().(*MenuScreen)
	if !ok {
		t.Fatalf("a right press on text row %d opened no menu, top is %T", row, tm.(core.Router).Top())
	}
	return menu, s, textTop
}

// TestEditorContextMenuAnchorAbsolute pins absCell. An overlay anchor is stated in
// absolute terminal cells, but an embedded editor receives pane-relative mouse
// coordinates — so the pane origin has to go back on, and only there.
func TestEditorContextMenuAnchorAbsolute(t *testing.T) {
	pane, shPane := newPaneEditor(EditorOpts{ContextMenu: true})
	pane.setContent("abcdef")
	pane.SetPaneOrigin(12, 5) // what ModularScreen pushes every View
	if x, y := pane.absCell(shPane, 3, 2); x != 15 || y != 7 {
		t.Errorf("embedded absCell(3,2) = (%d,%d), want (15,7)", x, y)
	}

	// Standalone the router already hands over absolute cells (BodyY included); adding an
	// origin would double-count the chrome.
	free, shFree := newEditor(EditorOpts{ContextMenu: true})
	if x, y := free.absCell(shFree, 3, 2); x != 3 || y != 2 {
		t.Errorf("standalone absCell(3,2) = (%d,%d), want it unchanged", x, y)
	}
}

// TestEditorContextMenuClearsTheText is the placement contract: the box opens one row
// BELOW the pressed cell, left edge on the pressed column, so the clicked line stays
// readable. The selection deliberately has no say in it — the second case is the one that
// matters, since anchoring off the selection's far edge would put the box three rows down
// here, and a whole selection's length down on a real one.
func TestEditorContextMenuClearsTheText(t *testing.T) {
	menu, _, textTop := openContextMenu(t, 24, "alpha\nbravo\ncharlie\ndelta", nil, 3, 1)
	if x, y, _, _ := menu.place(); x != 3 || y != textTop+2 {
		t.Errorf("unselected press placed the menu at (%d,%d), want (3,%d) — one row below it",
			x, y, textTop+2)
	}

	// Pressing row 1 of a selection spanning rows 0..2 still opens on row 2, not row 3.
	menu, s, textTop := openContextMenu(t, 24, "alpha\nbravo\ncharlie\ndelta",
		func(s *EditorScreen) { selectRange(s, 0, 1, 2, 3) }, 2, 1)
	if got := s.selectedText(); got == "" {
		t.Fatal("the press should have landed inside the selection and kept it")
	}
	if x, y, _, _ := menu.place(); x != 2 || y != textTop+2 {
		t.Errorf("menu placed at (%d,%d), want (2,%d) — one below the PRESS, not the selection",
			x, y, textTop+2)
	}
}

// TestEditorContextMenuFlipsAboveTheText: pressed on the last visible row there is no room
// below, so the box goes entirely above rather than sliding up over the text.
func TestEditorContextMenuFlipsAboveTheText(t *testing.T) {
	menu, s, textTop := openContextMenu(t, 24, strings.Repeat("line\n", 40), nil, 4, -1)

	pressed := textTop + s.h - 1
	x, y, _, h := menu.place()
	if bottom := y + h - 1; bottom != pressed-1 {
		t.Errorf("flipped menu's bottom row is %d, want %d — one above the pressed row",
			bottom, pressed-1)
	}
	if x != 4 {
		t.Errorf("flipping moved the column to %d, want the pressed column 4", x)
	}
}

// TestEditorContextCut: the deletion is synchronous, the write is not, and the whole
// thing is one undo step.
func TestEditorContextCut(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	wrote := stubClipboard(t, "")

	// Pick returns core.Seq(Pop, write): both lanes travel in Msg, and only the router
	// unpacks them — TestEditorContextMenuThroughRouter is where the write is observed
	// actually running. Here the synchronous half is what matters.
	act := s.editMenu(sh, 0, 0).items[1].Pick(sh)
	if act.Msg == nil {
		t.Fatal("Cut must pop the menu itself")
	}
	if got := buffer(s); got != "aef" {
		t.Fatalf("after cut the buffer is %q, want %q", got, "aef")
	}
	if s.selectionActive() || !s.dirty {
		t.Errorf("after cut: selection=%v dirty=%v, want cleared and dirty", s.selectionActive(), s.dirty)
	}
	if len(s.undoStack) != 1 {
		t.Fatalf("a cut should be one undo step, got %d", len(s.undoStack))
	}
	if *wrote != "" {
		t.Errorf("the write should not have run yet, clipboard holds %q", *wrote)
	}

	s.undo()
	if got := buffer(s); got != "abcdef" || s.selectedText() != "bcd" {
		t.Errorf("undo restored %q selecting %q, want the buffer and selection back", got, s.selectedText())
	}
}

// TestEditorContextCopyLeavesBuffer: Copy is the same path minus the mutation.
func TestEditorContextCopyLeavesBuffer(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	act := s.editMenu(sh, 0, 0).items[0].Pick(sh)
	if act.Msg == nil {
		t.Fatal("Copy must pop the menu itself")
	}
	if buffer(s) != "abcdef" || s.selectedText() != "bcd" {
		t.Errorf("copy left buffer %q selecting %q — both must be untouched", buffer(s), s.selectedText())
	}
	if len(s.undoStack) != 0 {
		t.Errorf("a copy is not an edit, got %d undo entries", len(s.undoStack))
	}
}

// TestEditorCopyCmdVerb: the write itself, and the cut flag that only picks the status
// line's verb — the deletion has already happened by the time this runs.
func TestEditorCopyCmdVerb(t *testing.T) {
	wrote := stubClipboard(t, "")
	if msg := copySelectionCmd("abc", false).Cmd(); msg != (editorCopiedMsg{n: 3}) {
		t.Errorf("copy cmd returned %#v, want a three-character copy", msg)
	}
	if *wrote != "abc" {
		t.Errorf("copy wrote %q, want %q", *wrote, "abc")
	}
	if msg := copySelectionCmd("abc", true).Cmd(); msg != (editorCopiedMsg{n: 3, cut: true}) {
		t.Errorf("cut cmd returned %#v, want the same count flagged as a cut", msg)
	}
}

// TestEditorContextPaste covers the arrival side: the read comes back as a message, and
// replacing a selection with it is one undo step.
func TestEditorContextPaste(t *testing.T) {
	s, sh := newEditor(EditorOpts{ContextMenu: true})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	s.Update(sh, editorPastedMsg{target: s, text: "X\nY"})
	if got := buffer(s); got != "aX\nYef" {
		t.Fatalf("paste over a selection gave %q, want %q", got, "aX\nYef")
	}
	if len(s.undoStack) != 1 {
		t.Fatalf("a paste should be one undo step, got %d", len(s.undoStack))
	}
	s.undo()
	if got := buffer(s); got != "abcdef" || s.selectedText() != "bcd" {
		t.Errorf("undo gave %q selecting %q, want the original back in one step", got, s.selectedText())
	}

	// An empty clipboard is a silent no-op, not an empty edit.
	before := len(s.undoStack)
	if _, act := s.Update(sh, editorPastedMsg{target: s, text: ""}); act.Msg != nil {
		t.Errorf("an empty paste reported %#v, want silence", act.Msg)
	}
	if len(s.undoStack) != before {
		t.Error("an empty paste pushed an undo entry")
	}

	// A failed read reports and leaves the buffer alone.
	snapshot := buffer(s)
	if _, act := s.Update(sh, editorPastedMsg{target: s, err: errFakeClipboard}); act.Msg == nil {
		t.Error("a failed paste should report on the status line")
	}
	if buffer(s) != snapshot {
		t.Error("a failed paste changed the buffer")
	}
}

// TestEditorPasteIsAddressed: async messages are broadcast to every panel, and a paste
// mutates the buffer — so a sibling editor pane must ignore one it did not ask for.
func TestEditorPasteIsAddressed(t *testing.T) {
	mine, sh := newEditor(EditorOpts{ContextMenu: true})
	mine.setContent("abc")
	other, _ := newEditor(EditorOpts{ContextMenu: true})

	mine.Update(sh, editorPastedMsg{target: other, text: "XYZ"})
	if got := buffer(mine); got != "abc" {
		t.Fatalf("an editor applied another's paste: %q", got)
	}
}

// TestEditorPasteReadSeam: the read runs in the cmd lane (it shells out to pbpaste), and
// addresses its result back to the editor that asked.
func TestEditorPasteReadSeam(t *testing.T) {
	s, _ := newEditor(EditorOpts{ContextMenu: true})
	stubClipboard(t, "hello")

	msg := pasteClipboardCmd(s).Cmd()
	pasted, ok := msg.(editorPastedMsg)
	if !ok {
		t.Fatalf("paste cmd returned %#v, want an editorPastedMsg", msg)
	}
	if pasted.target != s || pasted.text != "hello" || pasted.err != nil {
		t.Errorf("paste cmd returned %+v, want the text addressed to this editor", pasted)
	}
}

// TestEditorContextMenuThroughRouter is the end-to-end: a real router, a real push, a real
// pick. It is the only place the "Pick owns the dismissal" convention is checked against
// the stack rather than against a returned Action.
func TestEditorContextMenuThroughRouter(t *testing.T) {
	wrote := stubClipboard(t, "")
	s := NewEditorScreen(EditorOpts{ContextMenu: true})
	sh := core.NewShared(nil)
	r := core.NewRouter(sh, []core.TabEntry{{
		Title: "T", New: func(*core.Shared) core.Screen { return s },
	}})
	var tm tea.Model = r
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	tm, _ = tm.Update(rightPress(2, sh.BodyY()+s.titleH()))
	menu, ok := tm.(core.Router).Top().(*MenuScreen)
	if !ok {
		t.Fatalf("a right press should push a menu, top is %T", tm.(core.Router).Top())
	}
	if got := s.selectedText(); got != "bcd" {
		t.Fatalf("opening the menu disturbed the selection: %q", got)
	}

	// Enter on Copy: the menu pops itself and the write lands.
	tm, cmd := tm.Update(keyMsg("enter"))
	if _, still := tm.(core.Router).Top().(*MenuScreen); still {
		t.Fatal("picking a row must pop the menu")
	}
	if cmd == nil {
		t.Fatal("Copy should have produced a clipboard command")
	}
	if msg := cmd(); msg != (editorCopiedMsg{n: 3}) {
		t.Fatalf("router ran %#v, want a three-character copy", msg)
	}
	if *wrote != "bcd" {
		t.Errorf("clipboard got %q, want %q", *wrote, "bcd")
	}

	// A right press while the menu is up dismisses it (MenuScreen's non-left branch).
	tm, _ = tm.Update(rightPress(2, sh.BodyY()+s.titleH()))
	if _, ok := tm.(core.Router).Top().(*MenuScreen); !ok {
		t.Fatal("the menu should have reopened")
	}
	x, y, _, _ := menu.place()
	tm, _ = tm.Update(rightPress(x+2, y+1))
	if _, still := tm.(core.Router).Top().(*MenuScreen); still {
		t.Error("a right click over the open menu should dismiss it")
	}
}
