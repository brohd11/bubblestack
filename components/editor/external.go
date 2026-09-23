package editor

import (
	"sort"
	"strings"
)

// Position is a zero-based insertion point in an editor buffer. Column is a
// rune index, deliberately not an LSP byte or UTF-16 offset.
type Position struct {
	Line, Column int
}

// Range is a half-open range [Start, End) in rune-based editor positions.
type Range struct {
	Start, End Position
}

// CursorPosition returns the editor's current rune-based insertion point.
func (s *Screen) CursorPosition() Position {
	return Position{Line: s.curY, Column: s.curX}
}

// LineText returns one live buffer line without its newline.
func (s *Screen) LineText(line int) (string, bool) {
	if line < 0 || line >= len(s.lines) {
		return "", false
	}
	return string(s.lines[line]), true
}

// CursorAnchor returns the caret's absolute cell when visible. Only embedded editors
// know their origin (after a first render); standalone ones report false.
func (s *Screen) CursorAnchor() (x, y int, visible bool) {
	if !s.focused || !s.hasOrigin || s.h < 1 || s.w < 1 {
		return 0, 0, false
	}
	row := s.curY
	start := s.scrX
	if s.wrap {
		row = s.wrapRowForCursor()
		if row >= 0 && row < len(s.wrapRows) {
			start = s.wrapRows[row].start
		}
	}
	viewRow := row - s.scrY
	viewCol := cellOfCol(s.lines[s.curY], s.curX) - start
	if viewRow < 0 || viewRow >= s.h || viewCol < 0 || viewCol >= s.contentW() {
		return 0, 0, false
	}
	return s.originX + s.insetX() + s.leftGutterWidth() + viewCol,
		s.originY + s.insetY() + viewRow, true
}

// Reveal moves the caret to p and brings it on screen (centered if it was off), clearing
// the selection without an undo step. A line beyond the buffer is rejected, so a caller
// can retry once a file finishes loading.
func (s *Screen) Reveal(p Position) bool {
	if p.Line < 0 || p.Line >= len(s.lines) {
		return false
	}
	col := min(max(p.Column, 0), len(s.lines[p.Line]))
	s.cancelCompletionSession()
	s.clearSelection()
	s.resetMouseGesture()
	s.clickCount = 0
	s.curY, s.curX, s.wantX = p.Line, col, col
	s.centerIfOffscreen()
	s.clampScroll()
	return true
}

// centerIfOffscreen centers the caret's row when it is off screen, so a jump lands with
// context around it; an on-screen caret is left alone.
func (s *Screen) centerIfOffscreen() {
	if s.w < 1 || s.h < 1 {
		return
	}
	row := s.curY
	if s.wrap {
		row = s.wrapRowForCursor()
	}
	if row >= s.scrY && row < s.scrY+s.h {
		return
	}
	s.scrY = row - s.h/2
	s.clampScrollBounds()
}

// SelectRange reveals r's start and selects r, caret at the start. An empty or inverted
// range reveals without selecting.
func (s *Screen) SelectRange(r Range) bool {
	start := textPos{y: r.Start.Line, x: r.Start.Column}
	end := textPos{y: r.End.Line, x: r.End.Column}
	if !s.validExternalRange(start, end) {
		return false
	}
	if !s.Reveal(r.Start) {
		return false
	}
	s.selStart, s.selEnd = start, end
	return true
}

// ReplaceRange applies one external edit as one undo step, clearing the selection and
// leaving the caret after the inserted text. Invalid ranges change nothing.
func (s *Screen) ReplaceRange(r Range, text string) bool {
	start := textPos{y: r.Start.Line, x: r.Start.Column}
	end := textPos{y: r.End.Line, x: r.End.Column}
	if !s.validExternalRange(start, end) {
		return false
	}
	s.cancelCompletionSession()
	s.editAtomic(func() {
		s.clearSelection()
		at := s.replaceText(start, end, cleanExternalText(text))
		s.curY, s.curX, s.wantX = at.y, at.x, at.x
	})
	return true
}

// Edit is one range replacement in a set applied together.
type Edit struct {
	Range Range
	Text  string
}

// ApplyEdits applies a set of external edits as one undo step, in reverse document order
// so each range still refers to the original text. Any invalid or overlapping range
// rejects the whole set. The caret keeps its line and column (clamped), since these are
// reformats around it.
func (s *Screen) ApplyEdits(edits []Edit) bool {
	if len(edits) == 0 {
		return false
	}
	ordered := make([]Edit, len(edits))
	copy(ordered, edits)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i].Range.Start, ordered[j].Range.Start
		return a.Line > b.Line || a.Line == b.Line && a.Column > b.Column
	})
	for i, edit := range ordered {
		start := textPos{y: edit.Range.Start.Line, x: edit.Range.Start.Column}
		end := textPos{y: edit.Range.End.Line, x: edit.Range.End.Column}
		if !s.validExternalRange(start, end) {
			return false
		}
		// ordered runs last-to-first, so the previous entry's start is the earliest
		// position already claimed: this edit must end at or before it.
		if i > 0 {
			prev := ordered[i-1].Range.Start
			if posLess(textPos{y: prev.Line, x: prev.Column}, end) {
				return false
			}
		}
	}
	caret := Position{Line: s.curY, Column: s.curX}
	s.cancelCompletionSession()
	s.editAtomic(func() {
		s.clearSelection()
		for _, edit := range ordered {
			s.replaceText(
				textPos{y: edit.Range.Start.Line, x: edit.Range.Start.Column},
				textPos{y: edit.Range.End.Line, x: edit.Range.End.Column},
				cleanExternalText(edit.Text))
		}
		// Clamp inside the mutation: editAtomic's clampScroll reads the caret's line, which may
		// now be past the end.
		s.curY = min(max(caret.Line, 0), len(s.lines)-1)
		s.curX = min(max(caret.Column, 0), len(s.lines[s.curY]))
		s.wantX = s.curX
	})
	return true
}

// cleanExternalText normalizes supplied text like a paste: control runes dropped, line
// endings made \n.
func cleanExternalText(text string) string {
	parts := splitPastedLines(text)
	var clean strings.Builder
	for i, part := range parts {
		if i > 0 {
			clean.WriteByte('\n')
		}
		clean.WriteString(string(part))
	}
	return clean.String()
}

func (s *Screen) validExternalRange(start, end textPos) bool {
	if start.y < 0 || end.y < 0 || start.y >= len(s.lines) || end.y >= len(s.lines) {
		return false
	}
	if start.x < 0 || end.x < 0 || start.x > len(s.lines[start.y]) || end.x > len(s.lines[end.y]) {
		return false
	}
	return start.y < end.y || (start.y == end.y && start.x <= end.x)
}
