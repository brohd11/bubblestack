package components

import (
	"strings"
	"time"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// Highlighting has two layers. hl is the last exact, full-document snapshot and
// hlRows keeps its unaffected rows attached to the live buffer through line splices.
// hlPreview is a small synchronous fragment parse over the part of the viewport whose
// lexical state may have changed. A factory-backed exact parse later replaces both.

func (s *EditorScreen) resetHighlightRows() {
	s.hlRows = nil
	s.hlPreview = nil
	s.hlPrevSeq = -1
	s.hlPrevFrom, s.hlPrevTo = -1, -1
	s.hlDirty, s.hlAnchor, s.hlFar = 0, 0, false
}

func (s *EditorScreen) acceptHighlight(h Highlighter, seq int) {
	s.hl = h
	s.hlSeq = seq
	s.hlRows = make([]int, len(s.lines))
	for i := range s.hlRows {
		s.hlRows[i] = i
	}
	s.hlPreview = nil
	s.hlPrevSeq = -1
	s.hlPrevFrom, s.hlPrevTo = -1, -1
	s.hlDirty, s.hlAnchor, s.hlFar = -1, -1, false
}

// highlightAnchor translates the active snapshot's restart hint back into the current
// buffer. The search is deliberately bounded: walking an enormous multiline construct
// on every key would merely move the old parse stall into cache maintenance.
func (s *EditorScreen) highlightAnchor(row int) (int, bool) {
	if row < 0 || row >= len(s.hlRows) || s.hl == nil {
		return row, false
	}
	oldRow := s.hlRows[row]
	if oldRow < 0 {
		return row, s.hlFar
	}
	restart := oldRow
	if p, ok := s.hl.(HighlightRestartProvider); ok {
		restart = p.HighlightRestartLine(oldRow)
		if restart < 0 || restart > oldRow {
			restart = oldRow
		}
	}
	for current, n := row, 0; current >= 0 && n < editorHighlightPreviewLines; current, n = current-1, n+1 {
		if s.hlRows[current] == restart {
			return current, false
		}
	}
	return row, restart != oldRow
}

// rebaseHighlightRows mirrors a raw text replacement against the exact snapshot. Rows
// outside the replacement retain their old snapshot row; replacement rows are holes
// until the provisional or exact parser fills them.
func (s *EditorScreen) rebaseHighlightRows(start, end textPos, inserted string) {
	if s.hl == nil {
		return
	}
	anchor, far := s.highlightAnchor(start.y)
	if s.hlDirty < 0 || start.y < s.hlDirty {
		s.hlDirty = start.y
	}
	if s.hlAnchor < 0 || anchor < s.hlAnchor {
		s.hlAnchor = anchor
	}
	s.hlFar = s.hlFar || far
	s.hlPreview = nil
	s.hlPrevSeq = -1
	s.hlPrevFrom, s.hlPrevTo = -1, -1

	if len(s.hlRows) != len(s.lines) || start.y < 0 || end.y >= len(s.hlRows) {
		s.hlRows = nil
		return
	}
	newRows := strings.Count(inserted, "\n") + 1
	if start.y == end.y && newRows == 1 {
		s.hlRows[start.y] = -1
		return
	}
	out := make([]int, 0, len(s.hlRows)-(end.y-start.y+1)+newRows)
	out = append(out, s.hlRows[:start.y]...)
	for range newRows {
		out = append(out, -1)
	}
	out = append(out, s.hlRows[end.y+1:]...)
	s.hlRows = out
}

func (s *EditorScreen) visibleBufferLines() (int, int) {
	if len(s.lines) == 0 {
		return 0, -1
	}
	if s.h <= 0 {
		return s.curY, s.curY
	}
	if !s.wrap {
		first := min(max(s.scrY, 0), len(s.lines)-1)
		return first, min(first+s.h-1, len(s.lines)-1)
	}
	s.rebuildWrapRows()
	if len(s.wrapRows) == 0 {
		return s.curY, s.curY
	}
	firstRow := min(max(s.scrY, 0), len(s.wrapRows)-1)
	lastRow := min(firstRow+s.h-1, len(s.wrapRows)-1)
	return s.wrapRows[firstRow].line, s.wrapRows[lastRow].line
}

func (s *EditorScreen) previewCovers(row int) bool {
	return s.hlPrevSeq == s.editSeq && row >= s.hlPrevFrom && row <= s.hlPrevTo
}

// refreshHighlightPreview performs the only parse allowed in the keystroke path. It
// starts at a cached lexical boundary when one is nearby, stops at the viewport bottom,
// and never crosses the fixed physical-line budget.
func (s *EditorScreen) refreshHighlightPreview() {
	if s.hlFactory == nil || s.hlDirty < 0 || len(s.lines) == 0 {
		return
	}
	visibleFrom, visibleTo := s.visibleBufferLines()
	if visibleTo < s.hlDirty {
		return
	}
	from := max(s.hlDirty, visibleFrom)
	anchor := min(max(s.hlAnchor, 0), s.hlDirty)
	s.hlPreview = make(map[int][]Span, max(visibleTo-from+1, 0))
	s.hlPrevSeq, s.hlPrevFrom, s.hlPrevTo = s.editSeq, from, visibleTo
	if s.hlFar || visibleTo-anchor+1 > editorHighlightPreviewLines {
		return // the covered rows deliberately render plain until the exact result
	}

	var b strings.Builder
	for row := anchor; row <= visibleTo; row++ {
		if row > anchor {
			b.WriteByte('\n')
		}
		b.WriteString(string(s.lines[row]))
	}
	preview := s.hlFactory()
	if preview == nil {
		return
	}
	preview.Parse(b.String())
	for row := from; row <= visibleTo; row++ {
		spans := preview.HighlightLine(row - anchor)
		if spansText(spans) == string(s.lines[row]) {
			s.hlPreview[row] = spans
		}
	}
}

func (s *EditorScreen) highlightRefreshCmd(seq int, delay time.Duration) tea.Cmd {
	return tea.Tick(delay, func(time.Time) tea.Msg {
		return core.PropagateAll(editorHighlightMsg{target: s, seq: seq})
	})
}

// startHighlightParse starts at most one full parse. A fresh highlighter instance is
// both the worker and the immutable result, so rendering never reads the object a
// background goroutine is mutating.
func (s *EditorScreen) startHighlightParse() tea.Cmd {
	if s.hlFactory == nil || s.hlParsing || s.hlSeq == s.editSeq {
		return nil
	}
	s.hlParsing = true
	s.hlJob++
	job, epoch, seq := s.hlJob, s.hlEpoch, s.editSeq
	text := s.Text()
	factory := s.hlFactory
	return func() tea.Msg {
		h := factory()
		if h != nil {
			h.Parse(text)
		}
		return core.PropagateAll(editorHighlightReadyMsg{
			target: s, job: job, epoch: epoch, seq: seq, hl: h,
		})
	}
}

func (s *EditorScreen) handleHighlightWake(m editorHighlightMsg) core.Action {
	if m.target != s || m.seq != s.editSeq || s.hl == nil {
		return core.Action{}
	}
	if wait := s.highlightWait(time.Now()); wait > 0 {
		return core.Async(s.highlightRefreshCmd(m.seq, wait))
	}
	if s.hlFactory == nil {
		s.parseHighlight()
		return core.Action{}
	}
	return core.Async(s.startHighlightParse())
}

func (s *EditorScreen) handleHighlightReady(m editorHighlightReadyMsg) core.Action {
	if m.target != s || !s.hlParsing || m.job != s.hlJob {
		return core.Action{}
	}
	s.hlParsing = false
	if m.epoch == s.hlEpoch && m.seq == s.editSeq {
		if m.hl == nil {
			s.hl, s.hlFactory, s.hlSeq = nil, nil, m.seq
			s.resetHighlightRows()
			s.hlDirty = -1
			return core.Action{}
		}
		s.acceptHighlight(m.hl, m.seq)
		return core.Action{}
	}
	if s.hlFactory == nil || s.hlSeq == s.editSeq {
		return core.Action{}
	}
	if wait := s.highlightWait(time.Now()); wait > 0 {
		return core.Async(s.highlightRefreshCmd(s.editSeq, wait))
	}
	return core.Async(s.startHighlightParse())
}

// Receive lets versioned highlight work — and the drag auto-scroll clock, which is
// addressed the same way — arrive through PropagateAll even while this editor is embedded
// beneath a menu or dialog.
func (s *EditorScreen) Receive(sh *core.Shared, payload any) core.Action {
	switch m := payload.(type) {
	case editorDragScrollMsg:
		return s.handleDragScroll(sh, m)
	case editorHighlightMsg:
		return s.handleHighlightWake(m)
	case editorHighlightReadyMsg:
		return s.handleHighlightReady(m)
	default:
		return core.Action{}
	}
}
