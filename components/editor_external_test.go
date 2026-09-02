package components

import "testing"

func TestEditorReplaceRangeIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
	s.SetText("héllo\nworld")
	if !s.ReplaceRange(EditorRange{
		Start: EditorPosition{Line: 0, Column: 1},
		End:   EditorPosition{Line: 0, Column: 5},
	}, "i\nthere") {
		t.Fatal("valid replacement was rejected")
	}
	if got := s.Text(); got != "hi\nthere\nworld" {
		t.Fatalf("replacement = %q", got)
	}
	if got := s.CursorPosition(); got != (EditorPosition{Line: 1, Column: 5}) {
		t.Fatalf("cursor after replacement = %+v", got)
	}
	s.undo()
	if got := s.Text(); got != "héllo\nworld" {
		t.Fatalf("single undo = %q", got)
	}
	s.redo()
	if got := s.Text(); got != "hi\nthere\nworld" {
		t.Fatalf("single redo = %q", got)
	}
}

func TestEditorExternalRangeValidationAndCursorAnchor(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.SetText("abc")
	before := s.Text()
	if s.ReplaceRange(EditorRange{Start: EditorPosition{Line: 0, Column: 4}}, "x") {
		t.Fatal("out-of-bounds replacement was accepted")
	}
	if s.Text() != before {
		t.Fatal("invalid replacement changed the buffer")
	}

	s.SetEmbedded(true)
	s.SetPaneOrigin(10, 5)
	s.SetSize(sh, 80, 20)
	s.SetFocused(true)
	s.curX, s.wantX = 1, 1
	x, y, visible := s.CursorAnchor()
	if !visible || x != 12 || y != 7 {
		t.Fatalf("cursor anchor = (%d,%d,%v), want (12,7,true)", x, y, visible)
	}
	if line, ok := s.LineText(0); !ok || line != "abc" {
		t.Fatalf("LineText = %q,%v", line, ok)
	}
}
