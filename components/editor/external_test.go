package editor

import (
	"fmt"
	"strings"
	"testing"
)

func TestEditorReplaceRangeIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.SetText("héllo\nworld")
	if !s.ReplaceRange(Range{
		Start: Position{Line: 0, Column: 1},
		End:   Position{Line: 0, Column: 5},
	}, "i\nthere") {
		t.Fatal("valid replacement was rejected")
	}
	if got := s.Text(); got != "hi\nthere\nworld" {
		t.Fatalf("replacement = %q", got)
	}
	if got := s.CursorPosition(); got != (Position{Line: 1, Column: 5}) {
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
	s, sh := newEditor(Opts{})
	s.SetText("abc")
	before := s.Text()
	if s.ReplaceRange(Range{Start: Position{Line: 0, Column: 4}}, "x") {
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

func longBuffer(lines int) string {
	var b strings.Builder
	for i := range lines {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func TestEditorRevealCentersOffscreenTargets(t *testing.T) {
	s, _ := newEditor(Opts{}) // 80x20
	s.SetText(longBuffer(200))
	if !s.Reveal(Position{Line: 100, Column: 3}) {
		t.Fatal("valid reveal was rejected")
	}
	if got := s.CursorPosition(); got != (Position{Line: 100, Column: 3}) {
		t.Fatalf("cursor after reveal = %+v", got)
	}
	if want := 100 - s.h/2; s.scrY != want {
		t.Fatalf("scroll after centering = %d, want %d", s.scrY, want)
	}
	// A target already on screen must not move the viewport at all.
	before := s.scrY
	if !s.Reveal(Position{Line: 100 + s.h/4, Column: 0}) {
		t.Fatal("on-screen reveal was rejected")
	}
	if s.scrY != before {
		t.Fatalf("on-screen reveal scrolled to %d, want %d", s.scrY, before)
	}
}

func TestEditorRevealRejectsUnloadedBuffer(t *testing.T) {
	s, _ := newEditor(Opts{})
	if s.Reveal(Position{Line: 40, Column: 0}) {
		t.Fatal("reveal into an empty buffer was accepted")
	}
	if got := s.CursorPosition(); got != (Position{}) {
		t.Fatalf("rejected reveal moved the cursor to %+v", got)
	}
	// The column clamps rather than rejecting: a real line is a real target.
	s.SetText("abc")
	if !s.Reveal(Position{Line: 0, Column: 99}) {
		t.Fatal("reveal past end of line was rejected")
	}
	if got := s.CursorPosition(); got != (Position{Line: 0, Column: 3}) {
		t.Fatalf("clamped cursor = %+v", got)
	}
}

func TestEditorSelectRangeHighlightsAndAnchorsAtStart(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.SetText("alpha\nbeta\ngamma")
	if !s.SelectRange(Range{
		Start: Position{Line: 1, Column: 0},
		End:   Position{Line: 1, Column: 4},
	}) {
		t.Fatal("valid range selection was rejected")
	}
	if !s.selectionActive() {
		t.Fatal("no selection after SelectRange")
	}
	if got := s.selectedText(); got != "beta" {
		t.Fatalf("selected text = %q", got)
	}
	if got := s.CursorPosition(); got != (Position{Line: 1, Column: 0}) {
		t.Fatalf("cursor = %+v, want the range start", got)
	}
}

func TestEditorApplyEditsIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.SetText("aaa\nbbb\nccc")
	// Stated in ascending order and against the ORIGINAL buffer; ApplyEdits reverses
	// them so the first edit's length change cannot move the second.
	ok := s.ApplyEdits([]Edit{
		{Range: Range{
			Start: Position{Line: 0, Column: 0},
			End:   Position{Line: 0, Column: 3}}, Text: "onelong"},
		{Range: Range{
			Start: Position{Line: 2, Column: 0},
			End:   Position{Line: 2, Column: 3}}, Text: "z"},
	})
	if !ok {
		t.Fatal("valid edit set was rejected")
	}
	if got := s.Text(); got != "onelong\nbbb\nz" {
		t.Fatalf("edited buffer = %q", got)
	}
	s.undo()
	if got := s.Text(); got != "aaa\nbbb\nccc" {
		t.Fatalf("single undo = %q, want the whole set reverted", got)
	}
	s.redo()
	if got := s.Text(); got != "onelong\nbbb\nz" {
		t.Fatalf("single redo = %q", got)
	}
}

func TestEditorApplyEditsRejectsWholeSetOnBadRange(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.SetText("aaa\nbbb")
	before := s.Text()
	for name, edits := range map[string][]Edit{
		"out of range": {
			{Range: Range{Start: Position{Line: 0}, End: Position{Line: 0, Column: 3}}, Text: "x"},
			{Range: Range{Start: Position{Line: 9}, End: Position{Line: 9}}, Text: "y"},
		},
		"overlapping": {
			{Range: Range{Start: Position{Line: 0}, End: Position{Line: 0, Column: 3}}, Text: "x"},
			{Range: Range{Start: Position{Line: 0, Column: 1}, End: Position{Line: 1, Column: 1}}, Text: "y"},
		},
	} {
		if s.ApplyEdits(edits) {
			t.Fatalf("%s: edit set was accepted", name)
		}
		if s.Text() != before {
			t.Fatalf("%s: rejected set changed the buffer to %q", name, s.Text())
		}
	}
	if s.ApplyEdits(nil) {
		t.Fatal("empty edit set was accepted")
	}
}

func TestEditorApplyEditsKeepsCaretLineAndColumn(t *testing.T) {
	s, _ := newEditor(Opts{})
	s.SetText("aaa\nbbbbbb\nccc")
	s.Reveal(Position{Line: 1, Column: 4})
	// Reformat a line the caret is not on; the caret must not follow the edit.
	if !s.ApplyEdits([]Edit{{
		Range: Range{Start: Position{Line: 0}, End: Position{Line: 0, Column: 3}},
		Text:  "a",
	}}) {
		t.Fatal("valid edit was rejected")
	}
	if got := s.CursorPosition(); got != (Position{Line: 1, Column: 4}) {
		t.Fatalf("cursor = %+v, want it left where it was", got)
	}
	// A shrinking edit under the caret clamps rather than dangling past the line end.
	s.Reveal(Position{Line: 1, Column: 6})
	if !s.ApplyEdits([]Edit{{
		Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Column: 6}},
		Text:  "bb",
	}}) {
		t.Fatal("valid shrinking edit was rejected")
	}
	if got := s.CursorPosition(); got != (Position{Line: 1, Column: 2}) {
		t.Fatalf("clamped cursor = %+v", got)
	}
}
