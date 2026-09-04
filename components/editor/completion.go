package editor

import (
	"sort"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// CompletionStop describes one snippet tab stop using rune offsets into
// CompletionEdit.Text. Index zero is the final caret. Positive indices are
// visited in ascending order; Start and End are a half-open placeholder range.
type CompletionStop struct {
	Index      int
	Start, End int
}

// CompletionEdit is an atomic, externally supplied completion replacement.
// PairTrailingOpener asks the editor to complete one final configured, asymmetric
// delimiter for plain-text completions. Stops make the insertion a snippet and disable
// pair synthesis because the producer already owns its exact text and caret positions.
type CompletionEdit struct {
	Range              Range
	Text               string
	Stops              []CompletionStop
	PairTrailingOpener bool
}

type editorCompletionStop struct {
	index      int
	start, end textPos
}

type editorCompletionSession struct {
	stops  []editorCompletionStop
	active int
	final  textPos
}

// ApplyCompletion replaces a range as one undo step and installs any supplied snippet
// stops. Completion text is sanitized by the same rules as paste; stop offsets must
// refer to that sanitized text, so an offset-changing control character rejects a
// structured snippet instead of silently moving its placeholders.
func (s *Screen) ApplyCompletion(edit CompletionEdit) bool {
	start := textPos{y: edit.Range.Start.Line, x: edit.Range.Start.Column}
	end := textPos{y: edit.Range.End.Line, x: edit.Range.End.Column}
	if !s.validExternalRange(start, end) {
		return false
	}
	clean := sanitizedEditorText(edit.Text)
	if len(edit.Stops) > 0 && utf8.RuneCountInString(clean) != utf8.RuneCountInString(edit.Text) {
		return false
	}
	positions, stops, final, ok := completionPositions(start, clean, edit.Stops)
	if !ok {
		return false
	}

	s.cancelCompletionSession()
	caretOffset := len(positions) - 1
	if len(edit.Stops) == 0 && edit.PairTrailingOpener {
		runes := []rune(clean)
		if len(runes) > 0 {
			open := runes[len(runes)-1]
			if close, paired := s.autoPairs[open]; paired && close != open {
				caretOffset = len(runes)
				following := end.x < len(s.lines[end.y]) && s.lines[end.y][end.x] == close
				if !following {
					clean += string(close)
					positions, _, _, _ = completionPositions(start, clean, nil)
				}
			}
		}
	}
	if final != nil {
		caretOffset = final.Start
	}
	if caretOffset < 0 || caretOffset >= len(positions) {
		return false
	}
	finalCaret := positions[caretOffset]

	s.editAtomic(func() {
		s.clearSelection()
		s.replaceText(start, end, clean)
		s.curY, s.curX, s.wantX = finalCaret.y, finalCaret.x, finalCaret.x
	})
	if len(stops) > 0 {
		s.completion = &editorCompletionSession{stops: stops, active: 0, final: finalCaret}
		s.selectCompletionStop(0)
	}
	s.clampScroll()
	return true
}

func sanitizedEditorText(value string) string {
	parts := splitPastedLines(value)
	var out []rune
	for i, part := range parts {
		if i > 0 {
			out = append(out, '\n')
		}
		out = append(out, part...)
	}
	return string(out)
}

func completionPositions(start textPos, value string, supplied []CompletionStop) (
	[]textPos, []editorCompletionStop, *CompletionStop, bool,
) {
	runes := []rune(value)
	positions := make([]textPos, len(runes)+1)
	positions[0] = start
	for i, r := range runes {
		positions[i+1] = positions[i]
		if r == '\n' {
			positions[i+1].y++
			positions[i+1].x = 0
		} else {
			positions[i+1].x++
		}
	}
	seen := make(map[int]bool, len(supplied))
	positive := make([]CompletionStop, 0, len(supplied))
	var final *CompletionStop
	for _, stop := range supplied {
		if stop.Index < 0 || stop.Start < 0 || stop.Start > stop.End || stop.End > len(runes) || seen[stop.Index] {
			return nil, nil, nil, false
		}
		seen[stop.Index] = true
		copy := stop
		if stop.Index == 0 {
			if stop.Start != stop.End {
				return nil, nil, nil, false
			}
			final = &copy
		} else {
			positive = append(positive, copy)
		}
	}
	for i := range positive {
		for j := i + 1; j < len(positive); j++ {
			if positive[i].Start < positive[j].End && positive[j].Start < positive[i].End ||
				positive[i].Start == positive[i].End && positive[i].Start == positive[j].Start ||
				positive[j].Start == positive[j].End && positive[j].Start == positive[i].Start {
				return nil, nil, nil, false
			}
		}
	}
	sort.Slice(positive, func(i, j int) bool { return positive[i].Index < positive[j].Index })
	stops := make([]editorCompletionStop, len(positive))
	for i, stop := range positive {
		stops[i] = editorCompletionStop{
			index: stop.Index, start: positions[stop.Start], end: positions[stop.End],
		}
	}
	return positions, stops, final, true
}

func (s *Screen) cancelCompletionSession() { s.completion = nil }

func (s *Screen) selectCompletionStop(index int) {
	if s.completion == nil || index < 0 || index >= len(s.completion.stops) {
		return
	}
	s.completion.active = index
	stop := s.completion.stops[index]
	s.selStart, s.selEnd = stop.start, stop.end
	s.curY, s.curX, s.wantX = stop.end.y, stop.end.x, stop.end.x
	if stop.start == stop.end {
		s.clearSelection()
	}
	s.clampScroll()
}

func (s *Screen) advanceCompletionStop() {
	if s.completion == nil {
		return
	}
	next := s.completion.active + 1
	if next < len(s.completion.stops) {
		s.selectCompletionStop(next)
		return
	}
	final := s.completion.final
	s.cancelCompletionSession()
	s.clearSelection()
	s.curY, s.curX, s.wantX = final.y, final.x, final.x
	s.clampScroll()
}

func completionEditingKey(k string, m tea.KeyPressMsg) bool {
	if m.Text != "" {
		return true
	}
	switch k {
	case "enter", "backspace", "ctrl+h", "delete", "ctrl+d", "alt+backspace", "ctrl+w",
		"alt+delete", "alt+d", "ctrl+u", "ctrl+k", "shift+tab":
		return true
	}
	return false
}

// handleCompletionKey owns only the temporary snippet gestures. Ordinary edits continue
// through key so they retain language hooks and history; non-editing commands first end
// the session and then keep their normal meaning.
func (s *Screen) handleCompletionKey(k string, m tea.KeyPressMsg) bool {
	if s.completion == nil {
		return false
	}
	switch k {
	case "tab":
		s.advanceCompletionStop()
		return true
	case "esc":
		s.cancelCompletionSession()
		s.clearSelection()
		return true
	}
	if !completionEditingKey(k, m) {
		s.cancelCompletionSession()
	}
	return false
}

func (s *Screen) rebaseCompletionEdit(start, end textPos, inserted string) {
	if s.completion == nil {
		return
	}
	active := &s.completion.stops[s.completion.active]
	if posLess(start, active.start) || posLess(active.end, end) {
		s.cancelCompletionSession()
		return
	}
	newEnd := textEnd(start, inserted)
	active.end = rebaseCompletionPos(active.end, end, newEnd)
	for i := range s.completion.stops {
		if i == s.completion.active {
			continue
		}
		stop := &s.completion.stops[i]
		switch {
		case !posLess(start, stop.end):
			// The stop is wholly before the edit.
		case !posLess(stop.start, end):
			stop.start = rebaseCompletionPos(stop.start, end, newEnd)
			stop.end = rebaseCompletionPos(stop.end, end, newEnd)
		default:
			s.cancelCompletionSession()
			return
		}
	}
	if !posLess(s.completion.final, end) {
		s.completion.final = rebaseCompletionPos(s.completion.final, end, newEnd)
	}
}

func rebaseCompletionPos(p, oldEnd, newEnd textPos) textPos {
	if p.y == oldEnd.y {
		return textPos{y: newEnd.y, x: newEnd.x + p.x - oldEnd.x}
	}
	return textPos{y: p.y + newEnd.y - oldEnd.y, x: p.x}
}
