package components

import "strings"

// EditorPosition is a zero-based insertion point in an editor buffer. Column is a
// rune index, deliberately not an LSP byte or UTF-16 offset.
type EditorPosition struct {
	Line, Column int
}

// EditorRange is a half-open range [Start, End) in rune-based editor positions.
type EditorRange struct {
	Start, End EditorPosition
}

// CursorPosition returns the editor's current rune-based insertion point.
func (s *EditorScreen) CursorPosition() EditorPosition {
	return EditorPosition{Line: s.curY, Column: s.curX}
}

// LineText returns one live buffer line without its newline.
func (s *EditorScreen) LineText(line int) (string, bool) {
	if line < 0 || line >= len(s.lines) {
		return "", false
	}
	return string(s.lines[line]), true
}

// CursorAnchor returns the caret's absolute terminal cell when it is currently
// visible. An absolute origin is available for embedded editors after their parent has
// rendered at least once; standalone editors therefore report false.
func (s *EditorScreen) CursorAnchor() (x, y int, visible bool) {
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

// ReplaceRange applies one externally supplied edit through the editor's ordinary
// delta history. The replacement is one undo step, clears the selection, and leaves
// the caret immediately after the inserted text. Invalid ranges are rejected without
// changing the buffer.
func (s *EditorScreen) ReplaceRange(r EditorRange, text string) bool {
	start := textPos{y: r.Start.Line, x: r.Start.Column}
	end := textPos{y: r.End.Line, x: r.End.Column}
	if !s.validExternalRange(start, end) {
		return false
	}
	s.cancelCompletionSession()
	parts := splitPastedLines(text)
	var clean strings.Builder
	for i, part := range parts {
		if i > 0 {
			clean.WriteByte('\n')
		}
		clean.WriteString(string(part))
	}
	s.editAtomic(func() {
		s.clearSelection()
		at := s.replaceText(start, end, clean.String())
		s.curY, s.curX, s.wantX = at.y, at.x, at.x
	})
	return true
}

func (s *EditorScreen) validExternalRange(start, end textPos) bool {
	if start.y < 0 || end.y < 0 || start.y >= len(s.lines) || end.y >= len(s.lines) {
		return false
	}
	if start.x < 0 || end.x < 0 || start.x > len(s.lines[start.y]) || end.x > len(s.lines[end.y]) {
		return false
	}
	return start.y < end.y || (start.y == end.y && start.x <= end.x)
}
