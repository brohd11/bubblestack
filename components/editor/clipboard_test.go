package editor

import (
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// altKey builds the alt-modified rune the clipboard chords arrive as: bubbletea reports
// KeyRunes with Alt set, whose String() is "alt+<rune>".
func altKey(r rune) tea.KeyMsg {
	return keyMsg("alt+" + string(r))
}

// chord presses one clipboard chord and runs the command it returns — the write travels
// in the cmd lane (it shells out to pbcopy), so nothing reaches the stubbed clipboard
// until the returned Action is executed the way the router would.
func chord(t *testing.T, s *Screen, sh *core.Shared, r rune) tea.Msg {
	t.Helper()
	_, act := s.Update(sh, altKey(r))
	if act.Cmd == nil {
		t.Fatalf("alt+%c returned no command", r)
	}
	return act.Cmd()
}

// TestEditorCopyChordSelection: alt+c writes the selection and touches nothing else. The
// selection surviving is the point — the chord arrives as a rune, and key()'s selection
// pre-switch would take an unrecognized rune for typing and delete it.
func TestEditorCopyChordSelection(t *testing.T) {
	wrote := stubClipboard(t, "")
	s, sh := newEditor(Opts{})
	s.setContent("abcdef\nsecond")
	selectRange(s, 0, 1, 0, 4)

	if msg := chord(t, s, sh, 'c'); msg != (editorCopiedMsg{n: 3}) {
		t.Fatalf("alt+c ran %#v, want a three-character copy", msg)
	}
	if *wrote != "bcd" {
		t.Errorf("clipboard got %q, want %q", *wrote, "bcd")
	}
	if buffer(s) != "abcdef\nsecond" || s.selectedText() != "bcd" {
		t.Errorf("copy left buffer %q selecting %q — both must be untouched", buffer(s), s.selectedText())
	}
	if len(s.undoStack) != 0 {
		t.Errorf("a copy is not an edit, got %d undo entries", len(s.undoStack))
	}
}

// TestEditorCutChordSelection: alt+x writes the same text and deletes it, as one undo step.
func TestEditorCutChordSelection(t *testing.T) {
	wrote := stubClipboard(t, "")
	s, sh := newEditor(Opts{})
	s.setContent("abcdef\nsecond")
	selectRange(s, 0, 1, 0, 4)

	if msg := chord(t, s, sh, 'x'); msg != (editorCopiedMsg{n: 3, cut: true}) {
		t.Fatalf("alt+x ran %#v, want a three-character cut", msg)
	}
	if *wrote != "bcd" {
		t.Errorf("clipboard got %q, want %q", *wrote, "bcd")
	}
	if got := buffer(s); got != "aef\nsecond" {
		t.Fatalf("cut gave %q, want %q", got, "aef\nsecond")
	}
	if len(s.undoStack) != 1 {
		t.Fatalf("a cut should be one undo step, got %d", len(s.undoStack))
	}
	s.undo()
	if got := buffer(s); got != "abcdef\nsecond" {
		t.Errorf("undo gave %q, want the original back in one step", got)
	}
}

// TestEditorCopyChordLineFallback: with no selection the chords take the whole line, its
// newline included, so a cut+paste is a line move.
func TestEditorCopyChordLineFallback(t *testing.T) {
	wrote := stubClipboard(t, "")
	s, sh := newEditor(Opts{})
	s.setContent("first\nsecond\nthird")
	s.curY, s.curX = 1, 3

	chord(t, s, sh, 'c')
	if *wrote != "second\n" {
		t.Errorf("copy with no selection wrote %q, want the whole line", *wrote)
	}
	if buffer(s) != "first\nsecond\nthird" || len(s.undoStack) != 0 {
		t.Error("a line copy must not touch the buffer")
	}
}

// TestEditorCutChordLine covers the three shapes of a line cut: one that pulls the next
// line up, the last line (which takes the newline BEFORE it), and the only line, which
// has neither and is emptied in place — the buffer may never hold zero lines.
func TestEditorCutChordLine(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		curY, curX int
		wrote      string
		want       string
		wantY      int
		wantX      int
	}{
		{
			name: "middle line slides up", content: "first\nsecond\nthird", curY: 1, curX: 3,
			wrote: "second\n", want: "first\nthird", wantY: 1, wantX: 0,
		},
		{
			name: "first line", content: "first\nsecond", curY: 0, curX: 2,
			wrote: "first\n", want: "second", wantY: 0, wantX: 0,
		},
		{
			name: "last line lands at the end of the previous", content: "first\nsecond", curY: 1, curX: 4,
			wrote: "second\n", want: "first", wantY: 0, wantX: 5,
		},
		{
			name: "only line empties in place", content: "solo", curY: 0, curX: 2,
			wrote: "solo\n", want: "", wantY: 0, wantX: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wrote := stubClipboard(t, "")
			s, sh := newEditor(Opts{})
			s.setContent(tc.content)
			s.curY, s.curX = tc.curY, tc.curX

			chord(t, s, sh, 'x')
			if *wrote != tc.wrote {
				t.Errorf("clipboard got %q, want %q", *wrote, tc.wrote)
			}
			if got := buffer(s); got != tc.want {
				t.Fatalf("cut gave %q, want %q", got, tc.want)
			}
			if s.curY != tc.wantY || s.curX != tc.wantX {
				t.Errorf("caret at (%d,%d), want (%d,%d)", s.curY, s.curX, tc.wantY, tc.wantX)
			}
			if len(s.undoStack) != 1 {
				t.Fatalf("a line cut should be one undo step, got %d", len(s.undoStack))
			}
			s.undo()
			if got := buffer(s); got != tc.content {
				t.Errorf("undo gave %q, want %q back in one step", got, tc.content)
			}
		})
	}
}

// TestEditorCutEmptyOnlyLine: nothing to delete, so nothing is recorded — the clipboard
// still takes the bare newline, which is what an empty line IS.
func TestEditorCutEmptyOnlyLine(t *testing.T) {
	wrote := stubClipboard(t, "")
	s, sh := newEditor(Opts{})

	chord(t, s, sh, 'x')
	if *wrote != "\n" {
		t.Errorf("clipboard got %q, want a bare newline", *wrote)
	}
	if len(s.undoStack) != 0 {
		t.Errorf("cutting an empty buffer pushed %d undo entries", len(s.undoStack))
	}
}

// TestEditorPasteChord: alt+v is the read half, addressed back to this editor; the splice
// itself is the editorPastedMsg case (TestEditorContextPaste covers it).
func TestEditorPasteChord(t *testing.T) {
	s, sh := newEditor(Opts{})
	stubClipboard(t, "hello")
	s.setContent("ab")
	s.curX = 1

	_, act := s.Update(sh, altKey('v'))
	if act.Cmd == nil {
		t.Fatal("alt+v should return the clipboard read command")
	}
	pasted, ok := act.Cmd().(editorPastedMsg)
	if !ok {
		t.Fatalf("alt+v ran %#v, want an editorPastedMsg", act.Cmd())
	}
	if pasted.target != s || pasted.text != "hello" {
		t.Fatalf("alt+v read %+v, want the text addressed to this editor", pasted)
	}
	s.Update(sh, pasted)
	if got := buffer(s); got != "ahellob" {
		t.Errorf("paste gave %q, want %q", got, "ahellob")
	}
}

// TestEditorChordsDoNotType: the chords are keystrokes carrying runes, and the plain
// rune paths (auto-pair, selection-replace, insert) must not see them.
func TestEditorChordsDoNotType(t *testing.T) {
	stubClipboard(t, "")
	for _, r := range []rune{'c', 'x', 'v'} {
		s, sh := newEditor(Opts{})
		s.setContent("abcdef")
		selectRange(s, 0, 1, 0, 4)

		s.Update(sh, altKey(r))
		if r != 'x' && buffer(s) != "abcdef" {
			t.Errorf("alt+%c typed into the buffer: %q", r, buffer(s))
		}
		if r == 'c' && s.selectedText() != "bcd" {
			t.Errorf("alt+c dropped the selection: %q", s.selectedText())
		}
	}
}

// TestEditorUnknownAltRunesDoNotType is the editor-side backstop for malformed
// terminal escape input. The program filter drops the known SGR fragment pair, but an
// unrecognized Alt rune must never become text or delete the current selection even
// when an Screen is driven without bubblestack.Run.
func TestEditorUnknownAltRunesDoNotType(t *testing.T) {
	s, sh := newEditor(Opts{})
	s.setContent("abcdef")
	selectRange(s, 0, 1, 0, 4)

	s.Update(sh, keyMsg("alt+["))
	if got := buffer(s); got != "abcdef" {
		t.Fatalf("unknown Alt rune changed the buffer to %q", got)
	}
	if got := s.selectedText(); got != "bcd" {
		t.Fatalf("unknown Alt rune changed the selection to %q", got)
	}
	if s.dirty || len(s.undoStack) != 0 {
		t.Fatal("ignored Alt input must not create an edit or undo step")
	}
}
