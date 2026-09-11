package editor

import (
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// highlightOverlayRange is the compact, already-validated form kept in the immutable
// overlay snapshot. Rows live in the outer slice, so only rune columns ride here.
type highlightOverlayRange struct {
	from, to int
	style    *lipgloss.Style
}

// SetHighlightOverlay replaces the host overlay for the current buffer generation.
// It returns false, without disturbing the live overlay, when seq is stale. A current
// empty set is meaningful: it clears the previous answer.
//
// Individual malformed ranges are dropped. That tolerance matches syntax highlighting's
// general failure mode — one bad range loses color, never text or the rest of the answer.
func (s *Screen) SetHighlightOverlay(seq int, ranges []HighlightRange) bool {
	if seq != s.editSeq {
		return false
	}
	lines := make([][]highlightOverlayRange, len(s.lines))
	for _, item := range ranges {
		r := item.Range
		if r.Start.Line != r.End.Line || r.Start.Line < 0 || r.Start.Line >= len(s.lines) ||
			r.Start.Column < 0 || r.End.Column <= r.Start.Column ||
			r.End.Column > len(s.lines[r.Start.Line]) {
			continue
		}
		row := r.Start.Line
		lines[row] = append(lines[row], highlightOverlayRange{
			from: r.Start.Column, to: r.End.Column, style: item.Style,
		})
	}
	// Preserve caller order: when ranges overlap, a later range is the more specific
	// correction and wins as applyHighlightOverlay walks this slice.
	s.hlOverlay = lines
	s.hlOverlayRows = make([]int, len(s.lines))
	for row := range s.hlOverlayRows {
		s.hlOverlayRows[row] = row
	}
	return true
}

// ClearHighlightOverlay removes host styling immediately. It is used when a buffer takes
// on a different path/language; ordinary edits retain and structurally rebase unaffected
// rows instead.
func (s *Screen) ClearHighlightOverlay() {
	s.hlOverlay = nil
	s.hlOverlayRows = nil
}

// rebaseHighlightOverlay mirrors a text replacement against the positional overlay.
// Every touched row becomes a hole while rows below a line splice keep pointing at the
// immutable answer that still describes their text.
func (s *Screen) rebaseHighlightOverlay(start, end textPos, inserted string) {
	if len(s.hlOverlayRows) != len(s.lines) || start.y < 0 || end.y >= len(s.hlOverlayRows) {
		if s.hlOverlayRows != nil {
			s.hlOverlayRows = nil
		}
		return
	}
	newRows := strings.Count(inserted, "\n") + 1
	if start.y == end.y && newRows == 1 {
		s.hlOverlayRows[start.y] = -1
		return
	}
	out := make([]int, 0, len(s.hlOverlayRows)-(end.y-start.y+1)+newRows)
	out = append(out, s.hlOverlayRows[:start.y]...)
	for range newRows {
		out = append(out, -1)
	}
	out = append(out, s.hlOverlayRows[end.y+1:]...)
	s.hlOverlayRows = out
}

// applyHighlightOverlay cuts lexical spans at the host ranges for row. The implementation
// uses a per-rune style key because a range may land inside any span and lipgloss.Style
// itself is not comparable.
func (s *Screen) applyHighlightOverlay(row int, spans []Span) []Span {
	if row < 0 || row >= len(s.hlOverlayRows) {
		return spans
	}
	overlayRow := s.hlOverlayRows[row]
	if overlayRow < 0 || overlayRow >= len(s.hlOverlay) || len(s.hlOverlay[overlayRow]) == 0 {
		return spans
	}
	line := s.lines[row]
	if !spansMatchLine(spans, line) {
		spans = []Span{{Text: string(line)}}
	}
	keys := make([]int, len(line))
	styles := make([]*lipgloss.Style, 0, len(spans)+len(s.hlOverlay[overlayRow]))
	at := 0
	for _, span := range spans {
		styles = append(styles, span.Style)
		key := len(styles) - 1
		for range utf8.RuneCountInString(span.Text) {
			if at >= len(keys) {
				return spans
			}
			keys[at] = key
			at++
		}
	}
	if at != len(keys) {
		return spans
	}
	for _, item := range s.hlOverlay[overlayRow] {
		styles = append(styles, item.style)
		key := len(styles) - 1
		for col := item.from; col < item.to; col++ {
			keys[col] = key
		}
	}
	out := make([]Span, 0, len(spans)+len(s.hlOverlay[overlayRow]))
	for from := 0; from < len(line); {
		to := from + 1
		for to < len(line) && keys[to] == keys[from] {
			to++
		}
		out = append(out, Span{Text: string(line[from:to]), Style: styles[keys[from]]})
		from = to
	}
	return out
}
