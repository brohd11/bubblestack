package editor

import "strings"

// The comment toggle: walk the lines a selection spans, splice each, and carry the
// selection and caret over the columns that moved. The direction is decided once for the
// whole span.

// commentSpace follows the delimiter when commenting. Uncommenting does not require it.
const commentSpace = " "

// toggleComment comments or uncomments the selection's lines (or the caret's line). A
// profile with no delimiter makes it a no-op.
func (s *Screen) toggleComment() {
	if s.lineComment == "" && s.blockComment[0] == "" {
		return
	}
	first, last := s.indentSpan()
	col, ok := s.commentColumn(first, last)
	if !ok {
		return // nothing but blank lines
	}

	// Uncomment only when every non-blank line is already commented, so a mixed block ends up
	// fully commented.
	remove := true
	for y := first; y <= last && remove; y++ {
		if line := strings.TrimSpace(string(s.lines[y])); line != "" {
			remove = s.commentedAt(y, col)
		}
	}

	selected := s.selectionActive()
	caretAtEnd := selected && s.curY == s.selEnd.y && s.curX == s.selEnd.x
	moved := false
	var dStart, dEnd, dCaret int
	for y := first; y <= last; y++ {
		// Blank lines are skipped both ways: a delimiter alone on an empty line is
		// trailing junk, and it would survive an uncomment that has nothing to match.
		if strings.TrimSpace(string(s.lines[y])) == "" {
			continue
		}
		d := s.shiftLineComment(y, col, remove)
		if d == 0 {
			continue
		}
		moved = true
		if y == s.selStart.y {
			dStart = d
		}
		if y == s.selEnd.y {
			dEnd = d
		}
		if y == s.curY {
			dCaret = d
		}
	}
	if !moved {
		return
	}
	if !selected {
		s.curX = shiftCommentCol(s.curX, col, dCaret)
		s.wantX = s.curX
		return
	}
	s.selStart.x = shiftCommentCol(s.selStart.x, col, dStart)
	s.selEnd.x = shiftCommentCol(s.selEnd.x, col, dEnd)
	s.curX = shiftCommentCol(s.curX, col, dCaret)
	if caretAtEnd {
		s.curY, s.curX = s.selEnd.y, s.selEnd.x
	}
	s.wantX = s.curX
}

// commentColumn is the shallowest indent among the span's non-blank lines, keeping the
// block's shape and round-tripping. false when the span has nothing to comment.
func (s *Screen) commentColumn(first, last int) (int, bool) {
	col, found := 0, false
	for y := first; y <= last; y++ {
		line := s.lines[y]
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		indent := len(leadingWhitespace(line))
		if !found || indent < col {
			col, found = indent, true
		}
	}
	return col, found
}

// commentedAt reports whether line y already carries the delimiter at col.
func (s *Screen) commentedAt(y, col int) bool {
	line := s.lines[y]
	if col > len(line) {
		return false
	}
	rest := string(line[col:])
	if s.lineComment != "" {
		return strings.HasPrefix(rest, s.lineComment)
	}
	return strings.HasPrefix(rest, s.blockComment[0]) &&
		strings.HasSuffix(strings.TrimRight(rest, " \t"), s.blockComment[1])
}

// shiftLineComment adds or removes one line's delimiter at col and answers how many columns
// the text after it moved.
func (s *Screen) shiftLineComment(y, col int, remove bool) int {
	line := s.lines[y]
	if col > len(line) {
		return 0
	}
	if !remove {
		return s.insertComment(y, col)
	}
	return s.removeComment(y, col)
}

func (s *Screen) insertComment(y, col int) int {
	if s.lineComment != "" {
		open := s.lineComment + commentSpace
		s.replaceText(textPos{y, col}, textPos{y, col}, open)
		return len([]rune(open))
	}
	// Each line is wrapped on its own rather than the span once, so every line stays
	// independently reversible and the direction test above works per line.
	open := s.blockComment[0] + commentSpace
	closing := commentSpace + s.blockComment[1]
	end := len(s.lines[y])
	s.replaceText(textPos{y, end}, textPos{y, end}, closing)
	s.replaceText(textPos{y, col}, textPos{y, col}, open)
	return len([]rune(open))
}

func (s *Screen) removeComment(y, col int) int {
	line := s.lines[y]
	rest := string(line[col:])
	if s.lineComment != "" {
		open := s.lineComment
		if strings.HasPrefix(rest, open+commentSpace) {
			open += commentSpace
		}
		if !strings.HasPrefix(rest, open) {
			return 0
		}
		width := len([]rune(open))
		s.replaceText(textPos{y, col}, textPos{y, col + width}, "")
		return -width
	}
	open, closing := s.blockComment[0], s.blockComment[1]
	if !strings.HasPrefix(rest, open) {
		return 0
	}
	// The closer comes off first: removing the opener would shift the columns the second
	// splice was measured against, the ordering surroundSelection documents.
	trimmed := strings.TrimRight(rest, " \t")
	if !strings.HasSuffix(trimmed, closing) {
		return 0
	}
	tail := col + len([]rune(trimmed))
	head := tail - len([]rune(closing))
	if strings.HasSuffix(trimmed[:len(trimmed)-len(closing)], commentSpace) {
		head--
	}
	s.replaceText(textPos{y, head}, textPos{y, tail}, "")
	if strings.HasPrefix(rest, open+commentSpace) {
		open += commentSpace
	}
	width := len([]rune(open))
	s.replaceText(textPos{y, col}, textPos{y, col + width}, "")
	return -width
}

// shiftCommentCol moves a column across a splice at col; columns before it stay put.
func shiftCommentCol(x, col, delta int) int {
	if x < col {
		return x
	}
	return max(x+delta, col)
}
