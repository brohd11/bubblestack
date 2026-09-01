package components

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Buffer mutation for EditorScreen — inserting, deleting, splitting lines — and the undo
// history that wraps it. Every mutation goes through editAtomic (editor_clipboard.go) or
// records its own snapshot, which is what makes one keystroke one undo step.

// ---------- buffer editing ----------

// setContent replaces the buffer with loaded file content, marking it clean.
func (s *EditorScreen) setContent(content string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	raw := strings.Split(content, "\n")
	s.lines = make([][]rune, len(raw))
	for i, l := range raw {
		s.lines[i] = []rune(l)
	}
	s.curY, s.curX, s.wantX = 0, 0, 0
	s.scrY, s.scrX = 0, 0
	s.resetMouseGesture()
	s.clickCount = 0
	s.clearSelection()
	s.dirty = false
	s.undoStack, s.redoStack = nil, nil
	s.revision, s.savedRevision, s.nextRevision = 0, 0, 1
	s.editSeq++ // the buffer changed even though the load is clean: reparse
	s.wrapDirty = true
}

// editorEditKey identifies keys worth snapshotting before dispatch. A no-op still
// allocates a temporary snapshot, but recordEdit only retains it when editSeq changed.
func editorEditKey(k string, m tea.KeyPressMsg) bool {
	if m.Text != "" {
		return true
	}
	switch k {
	case "tab", "shift+tab", "enter", "backspace", "ctrl+h", "delete", "ctrl+d",
		"alt+backspace", "ctrl+w", "alt+delete", "alt+d", "ctrl+u", "ctrl+k":
		return true
	}
	return false
}

func cloneEditorLines(lines [][]rune) [][]rune {
	cloned := make([][]rune, len(lines))
	for i := range lines {
		cloned[i] = append([]rune(nil), lines[i]...)
	}
	return cloned
}

func (s *EditorScreen) snapshot() editorSnapshot {
	return editorSnapshot{
		lines: cloneEditorLines(s.lines), curY: s.curY, curX: s.curX, wantX: s.wantX,
		selStart: s.selStart, selEnd: s.selEnd, revision: s.revision,
	}
}

func pushEditorSnapshot(stack []editorSnapshot, snap editorSnapshot) []editorSnapshot {
	if len(stack) == editorHistoryLimit {
		copy(stack, stack[1:])
		stack[len(stack)-1] = snap
		return stack
	}
	return append(stack, snap)
}

func (s *EditorScreen) recordEdit(before editorSnapshot) {
	s.undoStack = pushEditorSnapshot(s.undoStack, before)
	s.redoStack = nil
	s.revision = s.nextRevision
	s.nextRevision++
	s.dirty = s.revision != s.savedRevision
}

func (s *EditorScreen) restoreSnapshot(snap editorSnapshot) {
	s.lines = cloneEditorLines(snap.lines)
	s.curY, s.curX, s.wantX = snap.curY, snap.curX, snap.wantX
	s.selStart, s.selEnd = snap.selStart, snap.selEnd
	s.revision = snap.revision
	s.dirty = s.revision != s.savedRevision
	s.resetMouseGesture()
	s.editSeq++
	s.wrapDirty = true
	s.clampScroll()
}

func (s *EditorScreen) undo() {
	if len(s.undoStack) == 0 {
		return
	}
	last := len(s.undoStack) - 1
	target := s.undoStack[last]
	s.undoStack = s.undoStack[:last]
	s.redoStack = pushEditorSnapshot(s.redoStack, s.snapshot())
	s.restoreSnapshot(target)
}

func (s *EditorScreen) redo() {
	if len(s.redoStack) == 0 {
		return
	}
	last := len(s.redoStack) - 1
	target := s.redoStack[last]
	s.redoStack = s.redoStack[:last]
	s.undoStack = pushEditorSnapshot(s.undoStack, s.snapshot())
	s.restoreSnapshot(target)
}

// splitPastedLines turns arbitrary incoming text into the buffer lines it should become.
// Bracketed paste arrives as one KeyMsg carrying the payload verbatim, so this is where
// the line breaks and the runes that have no display cell are dealt with: '\n' ends a
// line, '\r' ends a line and swallows a following '\n' (CRLF), tabs stay raw the way the
// tab key inserts them (expandLine owns their width), and every other control rune is
// dropped — leaving one in the buffer breaks the same row geometry the editorTabWidth
// note describes. Always returns at least one line.
func splitPastedLines(text string) [][]rune {
	out := [][]rune{{}}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			out = append(out, []rune{})
		case r == '\n':
			out = append(out, []rune{})
		case r == '\t':
			out[len(out)-1] = append(out[len(out)-1], r)
		case r < 0x20 || r == 0x7f:
			// no cell to render it in; drop it
		default:
			out[len(out)-1] = append(out[len(out)-1], r)
		}
	}
	return out
}

// insertText inserts text at the cursor, splitting it across buffer lines. This is the
// path every rune-bearing key takes: ordinary typing is the single-line case and lands in
// insertRunes, while a paste splices its lines in so that one buffer line stays one
// physical row. Counts as a single edit, so undo takes the whole paste back.
func (s *EditorScreen) insertText(text string) {
	parts := splitPastedLines(text)
	if len(parts) == 1 {
		if len(parts[0]) > 0 {
			s.insertRunes(parts[0]...)
		}
		return
	}
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	s.lines[s.curY] = append(line[:s.curX], parts[0]...)
	added := parts[1:]
	last := len(added) - 1
	endX := len(added[last]) // where the caret lands: after the paste, before the tail
	added[last] = append(added[last], tail...)
	// A fresh copy of the lines below: appending added in place would overwrite them.
	rest := append([][]rune{}, s.lines[s.curY+1:]...)
	s.lines = append(append(s.lines[:s.curY+1], added...), rest...)
	s.curY += len(added)
	s.curX = endX
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// insertRunes inserts rs at the cursor and advances past them.
func (s *EditorScreen) insertRunes(rs ...rune) {
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	line = append(line[:s.curX], rs...)
	s.lines[s.curY] = append(line, tail...)
	s.curX += len(rs)
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// newline splits the current line at the cursor; the tail moves to a new line below.
func (s *EditorScreen) newline() {
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	s.lines[s.curY] = line[:s.curX]
	s.lines = append(s.lines, nil)
	copy(s.lines[s.curY+2:], s.lines[s.curY+1:])
	s.lines[s.curY+1] = tail
	s.curY++
	s.curX, s.wantX = 0, 0
	s.dirty = true
	s.editSeq++
}

// backspace deletes the rune before the cursor, or joins the line onto the previous
// one at column 0.
func (s *EditorScreen) backspace() {
	if s.curX > 0 {
		line := s.lines[s.curY]
		s.lines[s.curY] = append(line[:s.curX-1], line[s.curX:]...)
		s.curX--
	} else if s.curY > 0 {
		prev := s.lines[s.curY-1]
		s.curX = len(prev)
		s.lines[s.curY-1] = append(prev, s.lines[s.curY]...)
		s.lines = append(s.lines[:s.curY], s.lines[s.curY+1:]...)
		s.curY--
	} else {
		return
	}
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// forwardDelete deletes the rune under the cursor (delete key), or pulls the next line
// up at end of line.
func (s *EditorScreen) forwardDelete() {
	line := s.lines[s.curY]
	if s.curX < len(line) {
		s.lines[s.curY] = append(line[:s.curX], line[s.curX+1:]...)
	} else if s.curY < len(s.lines)-1 {
		s.lines[s.curY] = append(line, s.lines[s.curY+1]...)
		s.lines = append(s.lines[:s.curY+1], s.lines[s.curY+2:]...)
	} else {
		return
	}
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}
