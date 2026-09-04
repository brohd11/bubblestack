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

// CursorAnchor returns the caret's absolute terminal cell when it is currently
// visible. An absolute origin is available for embedded editors after their parent has
// rendered at least once; standalone editors therefore report false.
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

// Reveal moves the caret to p and brings it on screen, centering the row when it was
// not already visible. It clears the selection but changes no text, so it records no
// undo step. A position naming a line the buffer does not have is rejected and changes
// nothing — which is what lets a caller aim at a buffer whose file is still loading and
// simply try again on the next update.
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

// centerIfOffscreen puts the caret's row in the middle of the viewport when it is not
// already on it. clampScroll alone scrolls the minimum distance, which lands a jump
// target on the very first or last row with no context around it; a jump the user did
// not scroll to themselves should arrive somewhere they can read. A caret already on
// screen is left exactly where it sits, so this never disturbs ordinary editing.
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

// SelectRange reveals r's start and highlights r. The caret lands on the START, not the
// end: the range is something the caller found for the user (a definition's name, the
// span a hover describes), so the reading position is its beginning. An empty or
// inverted range reveals without highlighting, since a selection needs start < end.
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

// ReplaceRange applies one externally supplied edit through the editor's ordinary
// delta history. The replacement is one undo step, clears the selection, and leaves
// the caret immediately after the inserted text. Invalid ranges are rejected without
// changing the buffer.
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

// ApplyEdits applies a whole set of externally supplied edits as ONE undo step. The set
// is applied in reverse document order, so every range still names the text it was
// computed against — the edits are stated in terms of the buffer as it stands, and
// working backwards means an earlier edit's length change cannot move a later one. A
// formatter returning three hundred edits must cost one ctrl+z, not three hundred.
//
// The whole set is validated first: any invalid or overlapping range rejects everything
// and leaves the buffer untouched, because a half-applied format is worse than none.
//
// The caret is preserved by line and column rather than followed to an edit's end —
// these edits are a reformat of text the user is sitting in, not an insertion they
// asked for at a point — and clamped to wherever that lands in the new buffer.
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
		// Inside the mutation, not after it: editAtomic ends with clampScroll, which
		// reads the caret's line — and a set that deleted lines can leave the caret
		// pointing past the buffer it was measured against.
		s.curY = min(max(caret.Line, 0), len(s.lines)-1)
		s.curX = min(max(caret.Column, 0), len(s.lines[s.curY]))
		s.wantX = s.curX
	})
	return true
}

// cleanExternalText turns arbitrary supplied text into buffer-legal lines, the same
// normalization a bracketed paste gets: control runes with no display cell are dropped
// and every line ending becomes a plain newline.
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
