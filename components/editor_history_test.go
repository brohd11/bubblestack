package components

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func undoEditor(s *EditorScreen) { s.key(nil, keyMsg("ctrl+z")) }
func redoEditor(s *EditorScreen) { s.key(nil, keyMsg("ctrl+y")) }

func TestEditorUndoRedoPerKeyEvent(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, []rune("paste")...)
	typeRunes(s, '!')
	if got := buffer(s); got != "paste!" || len(s.undoStack) != 2 {
		t.Fatalf("two rune events = buffer %q, history %d; want paste! and 2", got, len(s.undoStack))
	}

	undoEditor(s)
	if got := buffer(s); got != "paste" {
		t.Fatalf("first undo = %q, want paste", got)
	}
	undoEditor(s)
	if got := buffer(s); got != "" {
		t.Fatalf("multi-rune event should undo atomically, got %q", got)
	}
	redoEditor(s)
	redoEditor(s)
	if got := buffer(s); got != "paste!" {
		t.Fatalf("two redos = %q, want paste!", got)
	}
}

func TestEditorUndoRestoresSelectionReplacement(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)
	typeRunes(s, 'X')
	if got := buffer(s); got != "aXef" {
		t.Fatalf("replacement = %q, want aXef", got)
	}

	undoEditor(s)
	if got := buffer(s); got != "abcdef" {
		t.Fatalf("undo replacement = %q, want abcdef", got)
	}
	if s.selStart != (textPos{0, 1}) || s.selEnd != (textPos{0, 4}) || s.curX != 4 || s.wantX != 4 {
		t.Fatalf("undo did not restore selection/caret: %v..%v caret %d want %d",
			s.selStart, s.selEnd, s.curX, s.wantX)
	}

	redoEditor(s)
	if got := buffer(s); got != "aXef" || s.selectionActive() || s.curX != 2 {
		t.Fatalf("redo replacement = %q selection=%v caret=%d", got, s.selectionActive(), s.curX)
	}
}

func TestEditorStructuredEnterIsOneUndoStep(t *testing.T) {
	for _, tc := range []struct {
		name, path, content, edited string
	}{
		{"markdown", "notes.md", "- item", "- item\n- "},
		{"yaml", "config.yaml", "  key: value", "  key: value\n  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{Path: tc.path})
			s.setContent(tc.content)
			s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
			s.key(nil, keyMsg("enter"))
			if got := buffer(s); got != tc.edited || len(s.undoStack) != 1 {
				t.Fatalf("structured enter = %q history=%d, want %q/1", got, len(s.undoStack), tc.edited)
			}
			undoEditor(s)
			if got := buffer(s); got != tc.content {
				t.Fatalf("undo structured enter = %q, want %q", got, tc.content)
			}
			redoEditor(s)
			if got := buffer(s); got != tc.edited {
				t.Fatalf("redo structured enter = %q, want %q", got, tc.edited)
			}
		})
	}
}

func TestEditorUndoCoversDeletionFamilies(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		keyMsg("backspace"),
		keyMsg("delete"),
		keyMsg("ctrl+w"),
		keyMsg("ctrl+k"),
	} {
		s, _ := newEditor(EditorOpts{})
		s.setContent("alpha beta")
		s.curX, s.wantX = 6, 6
		s.key(nil, key)
		if buffer(s) == "alpha beta" {
			t.Fatalf("%s should mutate the prepared buffer", key.String())
		}
		undoEditor(s)
		if got := buffer(s); got != "alpha beta" {
			t.Fatalf("undo %s = %q, want original", key.String(), got)
		}
	}
}

func TestEditorRedoInvalidatedByNewEdit(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, 'a')
	typeRunes(s, 'b')
	undoEditor(s)
	typeRunes(s, 'c')
	redoEditor(s)
	if got := buffer(s); got != "ac" {
		t.Fatalf("redo after branching edit = %q, want ac", got)
	}
	if len(s.redoStack) != 0 {
		t.Fatal("a new edit after undo must discard redo history")
	}
}

func TestEditorHistoryLimit(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	for i := 0; i < editorHistoryLimit+1; i++ {
		typeRunes(s, 'x')
	}
	if len(s.undoStack) != editorHistoryLimit {
		t.Fatalf("undo history = %d, want cap %d", len(s.undoStack), editorHistoryLimit)
	}
	for i := 0; i < editorHistoryLimit+5; i++ {
		undoEditor(s)
	}
	if got := buffer(s); got != "x" {
		t.Fatalf("oldest retained history should leave the evicted first edit, got %q", got)
	}
}

func TestEditorDirtyTracksSavedRevision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.txt")
	s, sh := newEditor(EditorOpts{Path: path})
	typeRunes(s, 'a')
	s.Update(sh, s.saveCmd()())
	if s.dirty {
		t.Fatal("saved revision should be clean")
	}
	undoEditor(s)
	if !s.dirty {
		t.Fatal("undoing past the save point should be dirty")
	}
	redoEditor(s)
	if s.dirty {
		t.Fatal("redoing to the saved revision should be clean")
	}
}

func TestEditorSaveResultKeepsLaterEditDirty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inflight.txt")
	s, sh := newEditor(EditorOpts{Path: path})
	typeRunes(s, 'a')
	cmd := s.saveCmd()
	typeRunes(s, 'b')
	s.Update(sh, cmd())
	if !s.dirty {
		t.Fatal("an edit made after the saved snapshot must remain dirty")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "a" {
		t.Fatalf("saved snapshot = %q, err=%v; want a", data, err)
	}
	undoEditor(s)
	if got := buffer(s); got != "a" || s.dirty {
		t.Fatalf("undo to saved revision = %q dirty=%v, want a/false", got, s.dirty)
	}
}

func TestEditorLoadAndNoOpsManageHistory(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	typeRunes(s, 'x')
	s.setContent("loaded")
	if len(s.undoStack) != 0 || len(s.redoStack) != 0 {
		t.Fatal("loading content should reset both history stacks")
	}
	s.curX = 0
	s.key(nil, keyMsg("backspace"))
	if len(s.undoStack) != 0 {
		t.Fatal("a no-op editing key should not create history")
	}
	undoEditor(s)
	redoEditor(s)
	if got := buffer(s); got != "loaded" {
		t.Fatalf("empty-stack undo/redo changed buffer to %q", got)
	}
}

func TestEditorUndoRedoHelpBindings(t *testing.T) {
	var all []string
	for _, binding := range NewEditorScreen(EditorOpts{}).HelpBindings() {
		all = append(all, binding.Keys()...)
	}
	joined := strings.Join(all, " ")
	if !strings.Contains(joined, "ctrl+z") || !strings.Contains(joined, "ctrl+y") {
		t.Fatalf("editor help keys %q should advertise undo and redo", joined)
	}
}
