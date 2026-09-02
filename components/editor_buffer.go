package components

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// Buffer mutation for EditorScreen — inserting, deleting, splitting lines — and the undo
// history that wraps it. Every text change is a range replacement, and the current key
// event records only the replaced text rather than copying the whole buffer.

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
	s.activeEdit = nil
	s.revision, s.savedRevision, s.nextRevision = 0, 0, 1
	s.editSeq++ // the buffer changed even though the load is clean: reparse
	s.wrapDirty = true
}

// editorEditKey identifies keys that open a history transaction before dispatch. A no-op
// transaction records no changes and is discarded.
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

func (s *EditorScreen) historyState() editorState {
	return editorState{
		curY: s.curY, curX: s.curX, wantX: s.wantX,
		selStart: s.selStart, selEnd: s.selEnd, revision: s.revision,
	}
}

func (s *EditorScreen) restoreHistoryState(state editorState) {
	s.curY, s.curX, s.wantX = state.curY, state.curX, state.wantX
	s.selStart, s.selEnd = state.selStart, state.selEnd
	s.revision = state.revision
	s.dirty = s.revision != s.savedRevision
	s.resetMouseGesture()
	s.editSeq++
	s.wrapDirty = true
	s.clampScroll()
}

func pushEditorHistory(stack []editorHistoryEntry, entry editorHistoryEntry) []editorHistoryEntry {
	if len(stack) == editorHistoryLimit {
		copy(stack, stack[1:])
		stack[len(stack)-1] = entry
		return stack
	}
	return append(stack, entry)
}

func (s *EditorScreen) beginHistory() *editorHistoryEntry {
	if s.activeEdit != nil {
		return nil
	}
	entry := &editorHistoryEntry{before: s.historyState()}
	s.activeEdit = entry
	return entry
}

func (s *EditorScreen) finishHistory(entry *editorHistoryEntry) bool {
	if entry == nil {
		return false
	}
	s.activeEdit = nil
	if len(entry.changes) == 0 {
		return false
	}
	s.revision = s.nextRevision
	s.nextRevision++
	s.dirty = s.revision != s.savedRevision
	entry.after = s.historyState()
	s.undoStack = pushEditorHistory(s.undoStack, *entry)
	s.redoStack = nil
	return true
}

func (s *EditorScreen) undo() {
	if len(s.undoStack) == 0 {
		return
	}
	last := len(s.undoStack) - 1
	entry := s.undoStack[last]
	s.undoStack[last] = editorHistoryEntry{}
	s.undoStack = s.undoStack[:last]
	for i := len(entry.changes) - 1; i >= 0; i-- {
		change := entry.changes[i]
		s.applyTextReplacement(change.start, textEnd(change.start, change.inserted), change.deleted)
	}
	s.restoreHistoryState(entry.before)
	s.redoStack = pushEditorHistory(s.redoStack, entry)
}

func (s *EditorScreen) redo() {
	if len(s.redoStack) == 0 {
		return
	}
	last := len(s.redoStack) - 1
	entry := s.redoStack[last]
	s.redoStack[last] = editorHistoryEntry{}
	s.redoStack = s.redoStack[:last]
	for _, change := range entry.changes {
		s.applyTextReplacement(change.start, textEnd(change.start, change.deleted), change.inserted)
	}
	s.restoreHistoryState(entry.after)
	s.undoStack = pushEditorHistory(s.undoStack, entry)
}

// textBetween returns the exact buffer text in [start,end). Only the affected range is
// copied, which is also the complete payload an inverse replacement needs.
func (s *EditorScreen) textBetween(start, end textPos) string {
	if start == end {
		return ""
	}
	if start.y == end.y {
		return string(s.lines[start.y][start.x:end.x])
	}
	var b strings.Builder
	b.WriteString(string(s.lines[start.y][start.x:]))
	for y := start.y + 1; y < end.y; y++ {
		b.WriteByte('\n')
		b.WriteString(string(s.lines[y]))
	}
	b.WriteByte('\n')
	b.WriteString(string(s.lines[end.y][:end.x]))
	return b.String()
}

// textEnd advances start by text and returns the position immediately after it.
func textEnd(start textPos, text string) textPos {
	lastNewline := strings.LastIndexByte(text, '\n')
	if lastNewline < 0 {
		return textPos{start.y, start.x + utf8.RuneCountInString(text)}
	}
	return textPos{
		y: start.y + strings.Count(text, "\n"),
		x: utf8.RuneCountInString(text[lastNewline+1:]),
	}
}

// applyTextReplacement performs the raw buffer splice without touching history or edit
// generations. Single-line replacements keep the outer line slice intact, making an
// ordinary keystroke proportional to its line rather than to the document.
func (s *EditorScreen) applyTextReplacement(start, end textPos, inserted string) {
	if start.y == end.y && !strings.ContainsRune(inserted, '\n') {
		line, replacement := s.lines[start.y], []rune(inserted)
		out := make([]rune, 0, len(line)-(end.x-start.x)+len(replacement))
		out = append(out, line[:start.x]...)
		out = append(out, replacement...)
		s.lines[start.y] = append(out, line[end.x:]...)
		return
	}

	parts := strings.Split(inserted, "\n")
	replacement := make([][]rune, len(parts))
	prefix := s.lines[start.y][:start.x]
	suffix := s.lines[end.y][end.x:]
	if len(parts) == 1 {
		line := make([]rune, 0, len(prefix)+utf8.RuneCountInString(parts[0])+len(suffix))
		line = append(line, prefix...)
		line = append(line, []rune(parts[0])...)
		replacement[0] = append(line, suffix...)
	} else {
		first := make([]rune, 0, len(prefix)+utf8.RuneCountInString(parts[0]))
		first = append(first, prefix...)
		replacement[0] = append(first, []rune(parts[0])...)
		for i := 1; i < len(parts)-1; i++ {
			replacement[i] = []rune(parts[i])
		}
		last := len(parts) - 1
		tail := make([]rune, 0, utf8.RuneCountInString(parts[last])+len(suffix))
		tail = append(tail, []rune(parts[last])...)
		replacement[last] = append(tail, suffix...)
	}
	lines := make([][]rune, 0, len(s.lines)-(end.y-start.y)+len(replacement)-1)
	lines = append(lines, s.lines[:start.y]...)
	lines = append(lines, replacement...)
	s.lines = append(lines, s.lines[end.y+1:]...)
}

// replaceText is the only recorded text mutation. Multiple calls inside one key event
// become ordered changes in the same history entry.
func (s *EditorScreen) replaceText(start, end textPos, inserted string) textPos {
	if start == end && inserted == "" {
		return start
	}
	deleted := s.textBetween(start, end)
	s.applyTextReplacement(start, end, inserted)
	if s.activeEdit != nil {
		s.activeEdit.changes = append(s.activeEdit.changes, editorChange{
			start: start, deleted: deleted, inserted: inserted,
		})
	}
	s.dirty = true
	s.editSeq++
	return textEnd(start, inserted)
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

// insertText sanitizes and inserts text at the cursor, splitting it across buffer lines.
// This is the path every rune-bearing key and paste takes. The surrounding history
// transaction makes the whole payload one undo step.
func (s *EditorScreen) insertText(text string) {
	parts := splitPastedLines(text)
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(part))
	}
	start := textPos{s.curY, s.curX}
	end := s.replaceText(start, start, b.String())
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

// insertRunes inserts rs at the cursor and advances past them.
func (s *EditorScreen) insertRunes(rs ...rune) {
	if len(rs) == 0 {
		return
	}
	start := textPos{s.curY, s.curX}
	end := s.replaceText(start, start, string(rs))
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

// newline splits the current line at the cursor; the tail moves to a new line below.
func (s *EditorScreen) newline() {
	start := textPos{s.curY, s.curX}
	end := s.replaceText(start, start, "\n")
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

// backspace deletes the rune before the cursor, or joins the line onto the previous
// one at column 0.
func (s *EditorScreen) backspace() {
	end := textPos{s.curY, s.curX}
	start := end
	if s.curX > 0 {
		start.x--
	} else if s.curY > 0 {
		start = textPos{s.curY - 1, len(s.lines[s.curY-1])}
	} else {
		return
	}
	s.replaceText(start, end, "")
	s.curY, s.curX, s.wantX = start.y, start.x, start.x
}

// forwardDelete deletes the rune under the cursor (delete key), or pulls the next line
// up at end of line.
func (s *EditorScreen) forwardDelete() {
	start := textPos{s.curY, s.curX}
	end := start
	if s.curX < len(s.lines[s.curY]) {
		end.x++
	} else if s.curY < len(s.lines)-1 {
		end = textPos{s.curY + 1, 0}
	} else {
		return
	}
	s.replaceText(start, end, "")
	s.wantX = s.curX
}
