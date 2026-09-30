package editor

import (
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
)

// Live rendering: with SetLiveRender on and a highlighter that implements LineRenderer,
// every inactive row shows the renderer's display text instead of its source. A row is
// active, and so shown raw, when it holds the caret, is touched by the selection, or
// holds a search match. The caret and selection therefore only ever sit on raw rows, and
// every cursor, selection and edit path keeps working on source text; rendered rows only
// need wrapping, drawing and click mapping.

// LineRenderer optionally replaces an inactive row's display. Unlike Highlighter spans,
// the returned spans need not concatenate to the row's source text: they are what shows.
// Rows are in the highlighter's own parse, like HighlightLine's.
type LineRenderer interface {
	// RenderLine returns the display spans for row, or nil to show the source.
	RenderLine(row int, ctx LiveContext) []Span
	// SourceCol maps a rune index into RenderLine's text (its length for the end) back
	// to a rune column of the source row, for clicks and vertical moves.
	SourceCol(row, i int) int
	// ActiveGlyphs are one-cell swaps a row keeps while it shows its source (a list
	// bullet for its marker). They never change widths, so the caret and selection are
	// unaffected; the editor drops any the caret or selection touches.
	ActiveGlyphs(row int) []Glyph
	// SourceSpans are richer spans for a row shown as its source while live rendering is
	// on (a fenced block's language colors), so plain editing stays light. nil keeps
	// HighlightLine's; like those, they must concatenate to the row's text.
	SourceSpans(row int) []Span
}

// LiveContext is what the editor knows about the row RenderLine is drawing.
type LiveContext struct {
	// Width is the display cells the row has: the wrap width, or the text window's width
	// unwrapped. Content wider than this wraps (or scrolls) like any other.
	Width int
}

// Glyph swaps the rune at source column Col, which must be From, for Text (one rune, one
// cell) in Style. A From mismatch, from a stale parse, drops the glyph.
type Glyph struct {
	Col   int
	From  rune
	Text  rune
	Style *lipgloss.Style
}

// liveKey is the part of the active set the wrap cache was built for. Edits already
// dirty the cache, so the search matches only change through the query.
type liveKey struct {
	curY, selFrom, selTo int
	query                string
}

// SetLiveRender toggles live rendering, keeping the top line in place. It does nothing
// visible unless the highlighter implements LineRenderer.
func (s *Screen) SetLiveRender(on bool) {
	if s.live == on {
		return
	}
	top := s.TopLine()
	s.live = on
	s.wrapGoalValid = false
	s.wrapDirty = true
	s.SetTopLine(top)
}

// LiveRender reports whether live rendering is on.
func (s *Screen) LiveRender() bool { return s.live }

func (s *Screen) currentLiveKey() liveKey {
	k := liveKey{curY: s.curY, selFrom: -1, selTo: -1}
	if s.selectionActive() {
		k.selFrom, k.selTo = s.selStart.y, s.selEnd.y
	}
	if s.searchEnabled {
		k.query = s.searchQuery
	}
	return k
}

// rowActive reports whether row shows its source under live rendering.
func (s *Screen) rowActive(row int) bool {
	if row == s.curY {
		return true
	}
	if s.selectionActive() && row >= s.selStart.y && row <= s.selEnd.y {
		return true
	}
	if s.searchEnabled && s.searchQuery != "" {
		s.rebuildSearchMatches()
		return row < len(s.searchMatches) && len(s.searchMatches[row]) > 0
	}
	return false
}

// liveRenderer returns the highlighter's LineRenderer while live rendering is on.
func (s *Screen) liveRenderer() (LineRenderer, bool) {
	if !s.live || s.hl == nil {
		return nil, false
	}
	lr, ok := s.hl.(LineRenderer)
	return lr, ok
}

// snapshotRow maps a buffer row to its row in the highlighter's parse. Rows an edit
// replaced have none until the reparse; everything else stays attached, so a keystroke
// does not flash the rows around it back to source.
func (s *Screen) snapshotRow(row int) (int, bool) {
	if len(s.hlRows) == len(s.lines) {
		if r := s.hlRows[row]; r >= 0 {
			return r, true
		}
		return 0, false
	}
	if s.hlSeq == s.editSeq {
		return row, true
	}
	return 0, false
}

// liveSpans returns row's rendered spans at width, or nil when the row shows its source.
func (s *Screen) liveSpans(row, width int) []Span {
	lr, ok := s.liveRenderer()
	if !ok || row < 0 || row >= len(s.lines) || s.rowActive(row) {
		return nil
	}
	snap, ok := s.snapshotRow(row)
	if !ok {
		return nil
	}
	return lr.RenderLine(snap, LiveContext{Width: max(width, 1)})
}

// liveCol maps a display cell of row's rendered form (at width) to a source rune column.
// A cell inside an expanded tab lands on the tab, as colAtCell does for source.
func (s *Screen) liveCol(row, cell, width int) int {
	lr, ok := s.liveRenderer()
	snap, mapped := s.snapshotRow(row)
	spans := s.liveSpans(row, width)
	if !ok || !mapped || spans == nil {
		return colAtCell(s.lines[row], cell)
	}
	i, c := 0, 0
	for _, sp := range spans {
		for _, r := range sp.Text {
			w := 1
			if r == '\t' {
				w = editorTabWidth
			}
			if c+w > cell {
				return min(max(lr.SourceCol(snap, i), 0), len(s.lines[row]))
			}
			c += w
			i++
		}
	}
	return min(max(lr.SourceCol(snap, i), 0), len(s.lines[row]))
}

// liveSourceSpans are the renderer's SourceSpans for a row showing its source, from the
// preview parse on an edited row (so a code line being typed keeps its colors), or nil.
func (s *Screen) liveSourceSpans(row int) []Span {
	lr, ok := s.liveRenderer()
	if !ok {
		return nil
	}
	if s.previewCovers(row) {
		return s.hlPreviewSource[row]
	}
	if snap, ok := s.snapshotRow(row); ok {
		return lr.SourceSpans(snap)
	}
	return nil
}

// activeGlyphs are row's glyphs while it shows its source, minus any the caret or the
// selection touches. An edited row takes them from the viewport preview parse, so a
// bullet does not flicker back to its marker while its item is typed.
func (s *Screen) activeGlyphs(row int) []Glyph {
	lr, ok := s.liveRenderer()
	if !ok || row < 0 || row >= len(s.lines) {
		return nil
	}
	var gs []Glyph
	if s.previewCovers(row) {
		gs = s.hlPreviewGlyphs[row]
	} else if snap, ok := s.snapshotRow(row); ok {
		gs = lr.ActiveGlyphs(snap)
	}
	line := s.lines[row]
	out := gs[:0:0]
	for _, g := range gs {
		if g.Col < 0 || g.Col >= len(line) || line[g.Col] != g.From || line[g.Col] == '\t' {
			continue
		}
		if row == s.curY && (s.curX == g.Col || s.curX == g.Col+1) {
			continue
		}
		if s.selectionActive() && posLess(s.selStart, textPos{row, g.Col + 1}) &&
			posLess(textPos{row, g.Col}, s.selEnd) {
			continue
		}
		out = append(out, g)
	}
	return out
}

// ensureLiveSnapshot runs a due synchronous parse before a frame, so a snapshot swap
// (which rewraps rendered rows) never lands halfway through drawing one.
func (s *Screen) ensureLiveSnapshot() {
	if !s.live || s.hl == nil || s.hlFactory != nil || s.hlSeq == s.editSeq {
		return
	}
	if _, ok := s.hl.(LineRenderer); !ok {
		return
	}
	if s.hlSeq < 0 || s.highlightWait(time.Now()) == 0 {
		s.parseHighlight()
	}
}

// liveDisplay expands rendered spans like expandLine does source, with each cell's span
// index alongside.
func liveDisplay(spans []Span) (disp []rune, idx []int) {
	for i, sp := range spans {
		for _, r := range sp.Text {
			switch {
			case r == '\t':
				for range editorTabWidth {
					disp = append(disp, ' ')
					idx = append(idx, i)
				}
			case r < 0x20 || r == 0x7f:
				disp = append(disp, editorControlPlaceholder)
				idx = append(idx, i)
			default:
				disp = append(disp, r)
				idx = append(idx, i)
			}
		}
	}
	return disp, idx
}

// liveWidth is the display width of rendered spans.
func liveWidth(spans []Span) int {
	n := 0
	for _, sp := range spans {
		n += utf8.RuneCountInString(sp.Text) + (editorTabWidth-1)*strings.Count(sp.Text, "\t")
	}
	return n
}

// renderLiveWindow draws cells [start, end) of rendered spans: styles only, as no caret,
// selection, match or guide can fall on an inactive row.
func renderLiveWindow(spans []Span, start, end int) string {
	disp, idx := liveDisplay(spans)
	start, end = min(start, len(disp)), min(end, len(disp))
	var b strings.Builder
	for i := start; i < end; {
		j := i + 1
		for j < end && idx[j] == idx[i] {
			j++
		}
		text := string(disp[i:j])
		if style, ok := spans[idx[i]].SpanStyle(); ok {
			b.WriteString(style.Render(text))
		} else {
			b.WriteString(text)
		}
		i = j
	}
	return b.String()
}

// renderLiveLine is renderLine for a rendered row: the horizontal window at scrX, with the
// overflow marker when the row continues past it.
func (s *Screen) renderLiveLine(row int, spans []Span) string {
	n := liveWidth(spans)
	w := s.contentW()
	start := min(s.scrX, n)
	end := s.scrX + w
	over := w >= 2 && n > end
	if over {
		end--
	}
	body := renderLiveWindow(spans, start, min(end, n))
	if over {
		body += core.MutedStyle().Render(string(editorOverflowMark))
	}
	return s.gutterText(row, true) + body
}
