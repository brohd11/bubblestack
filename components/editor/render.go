package editor

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Rendering for Screen: body, gutter, soft-wrap rows, syntax spans, selection and search
// painting, and help. Nothing here mutates the buffer.

// ---------- rendering ----------

// titleH is the title bar's height. Focused and muted bars are the same height, so focus
// never shifts the body.
func (s *Screen) titleH() int {
	if s.hideTitle {
		return 0
	}
	return lipgloss.Height(core.RenderTitleBar(s.titleText()))
}

// insetX and insetY are the body's offsets from the screen's top-left, shared by SetSize
// and clickAt so they cannot drift. Left: the frame border when bordered, plus one blank
// column when embedded.
func (s *Screen) insetX() int {
	x := 0
	if s.bordered {
		x++
	}
	if s.embedded {
		x++
	}
	return x
}

// insetY is the frame's top border row when bordered, else the title bar's height.
func (s *Screen) insetY() int {
	if s.bordered {
		return 1
	}
	return s.titleH()
}

func (s *Screen) baseTitleText() string {
	return s.title + s.ChangeMark()
}

func (s *Screen) titleText() string {
	if s.hideTitle {
		return ""
	}
	return s.baseTitleText()
}

// searchBarVisible reports whether the bottom rows belong to search: while its overlay is
// open, or while a retained query still highlights matches.
func (s *Screen) searchBarVisible() bool {
	return s.searchEnabled && (s.searchEditing || s.searchQuery != "")
}

// searchBar renders the unfocused search bar; clicking it opens the LineEditScreen over
// the same shell.
func (s *Screen) searchBar() string {
	w := s.paneW()
	contentW := max(w-4, 1) // two border cells and one padding cell on each side
	content := ansi.Truncate("find: "+s.searchQuery, contentW, "…")
	return retainedSearchBox().Width(contentW + 4).Render(content)
}

// retainedSearchBox is the unfocused counterpart to components.LineEditBox: same geometry, but
// the framework's ordinary border color rather than the active accent.
func retainedSearchBox() lipgloss.Style {
	return components.LineEditBox().BorderForeground(core.BorderColor)
}

// View renders the buffer under its title: in the frame legend when bordered, otherwise a
// title bar muted while unfocused.
func (s *Screen) View(*core.Shared) string {
	s.syncSearchBarHeight()
	var editor string
	if s.bordered {
		editor = components.Frame(s.titleText(), s.body(), s.w+s.gutter(), s.focused)
	} else {
		editor = core.WithTitleFocused(s.titleText(), s.body(), s.focused)
	}
	if !s.searchBarVisible() {
		return editor
	}
	return lipgloss.JoinVertical(lipgloss.Left, editor, s.searchBar())
}

// syncSearchBarHeight re-lays-out when the retained search bar appears or goes. The query
// is edited in a pushed overlay that writes straight to this screen, so no resize happens
// in between; View is the reliable place to catch it (as ListPanel's filter line does).
func (s *Screen) syncSearchBarHeight() {
	if s.lastSizeW > 0 && s.searchBarVisible() != s.lastSearchBar {
		s.sizeDirty = true
		s.SetSize(nil, s.lastSizeW, s.lastSizeH)
	}
}

// gutter is the embedded body's one-column left indent (0 standalone) — the part of
// insetX that lives INSIDE the frame, so View adds it back to the frame's inner run.
func (s *Screen) gutter() int {
	if s.embedded {
		return 1
	}
	return 0
}

// body is the viewport: exactly s.h rows of exactly s.w cells (a wider row would wrap in
// the terminal and shift every later frame), plus the exit prompt as the last row when
// up. When the buffer overflows, the rightmost column is the scrollbar. Only rowCount and
// renderRow know what a row is (a line, or a wrapped chunk).
func (s *Screen) body() string {
	s.ensureLiveSnapshot()
	rows := s.h
	if s.confirmExit {
		rows-- // the prompt takes the last body row
	}
	bar := s.barVisible()
	total := s.rowCount()
	sb := s.syncBar()
	pad := strings.Repeat(" ", s.gutter())
	var b strings.Builder
	for i := 0; i < rows; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		if row := s.scrY + i; row < total {
			line := s.renderRow(row)
			b.WriteString(line)
			if bar {
				b.WriteString(strings.Repeat(" ", max(s.textW()-lipgloss.Width(line), 0)))
			}
		} else if bar {
			b.WriteString(strings.Repeat(" ", s.textW()))
		}
		if bar {
			b.WriteString(sb.Cell(i))
		}
	}
	if s.confirmExit {
		if rows > 0 {
			b.WriteByte('\n')
		}
		prompt := editorPromptStyle.Render("Save modified buffer? (y)es (n)o (c)ancel")
		b.WriteString(pad + prompt)
		if bar {
			b.WriteString(strings.Repeat(" ", max(s.textW()-lipgloss.Width(prompt), 0)))
			b.WriteString(sb.Cell(s.h - 1))
		}
	}
	return b.String()
}

// rowCount is how many rows the viewport scrolls through: wrapped rows, or buffer lines.
func (s *Screen) rowCount() int {
	if s.wrap {
		return s.wrapTotalRows()
	}
	return len(s.lines)
}

// renderRow renders row i of rowCount's sequence.
func (s *Screen) renderRow(i int) string {
	if s.wrap {
		return s.renderWrappedRow(i)
	}
	return s.renderLine(i)
}

// gutterOn reports whether line numbers are enabled, independently of wrapping.
func (s *Screen) gutterOn() bool { return s.lineNums }

// numGutterWidth is the line-number column's width, just wide enough for the last line
// number; zero when the viewport is too narrow for numbers and text. It must not consult
// textW, which (through barVisible and the wrap cache) depends on it.
func (s *Screen) numGutterWidth() int {
	if !s.gutterOn() {
		return 0
	}
	digits := 1
	for n := len(s.lines); n >= 10; n /= 10 {
		digits++
	}
	w := digits + 1 // a trailing space separates the number from the text
	if w > s.w-2 {
		return 0
	}
	return w
}

// visibleGutter returns the sign columns and number width that fit, which together are
// leftGutterWidth: everything left of the text. Every "where does text start" computation
// (contentW, the wrap rebuild, click mapping) must use it, or clicks shift by a sign
// column. Like numGutterWidth it must not consult textW. When narrow, numbers go first,
// then sign columns from the outside in.
func (s *Screen) visibleGutter() (signs []string, nums int) {
	capacity := max(s.w-2, 0)
	signs = s.shownSignColumns()
	nums = s.numGutterWidth()
	if len(signs)+nums > capacity {
		nums = 0
	}
	if len(signs) > capacity {
		// The order is outer-to-inner. On a tiny viewport retain the columns closest
		// to the text, where the most immediate annotation belongs.
		signs = signs[len(signs)-capacity:]
	}
	return signs, nums
}

func (s *Screen) leftGutterWidth() int {
	signs, nums := s.visibleGutter()
	return len(signs) + nums
}

// gutterText is the sign and number cells left of one display row, blank on
// continuations, always exactly leftGutterWidth wide.
func (s *Screen) gutterText(line int, first bool) string {
	signs, nums := s.visibleGutter()
	if len(signs)+nums == 0 {
		return ""
	}
	return s.signText(signs, line, first) + s.lineNumTextWidth(nums, line, first)
}

func (s *Screen) lineNumTextWidth(w, line int, first bool) string {
	if w == 0 {
		return ""
	}
	if !first {
		return strings.Repeat(" ", w)
	}
	return fmt.Sprintf("%*d ", w-1, line+1)
}

// rebuildWrapRows recomputes the display rows and whether the scrollbar is needed. It
// cannot read textW or barVisible (they depend on what it builds), so it measures at full
// width and, on overflow, once more one column narrower for the bar; narrowing only adds
// rows, so the second pass is final. Edits, resizes and toggles set wrapDirty.
func (s *Screen) rebuildWrapRows() {
	// Live rendering: rows that flipped between source and rendered change height, so a
	// change in the active set rewraps, keeping the top line where it was.
	flipped := false
	if s.live {
		if k := s.currentLiveKey(); k != s.liveLast {
			s.liveLast = k
			flipped = true
			s.wrapDirty = true
		}
	}
	if !s.wrapDirty {
		return
	}
	topLine, topOff := -1, 0
	if flipped && s.scrY < len(s.wrapRows) {
		topLine = s.wrapRows[s.scrY].line
		for i := s.scrY; i > 0 && s.wrapRows[i-1].line == topLine; i-- {
			topOff++
		}
	}
	s.wrapDirty = false // cleared first: nothing below may re-enter the rebuild
	s.wrapBar = false
	s.buildWrapRows(s.w - s.leftGutterWidth())
	if len(s.wrapRows) > s.h {
		s.wrapBar = true
		s.buildWrapRows(s.w - 1 - s.leftGutterWidth())
	}
	if topLine >= 0 {
		for i, r := range s.wrapRows {
			if r.line == topLine {
				s.scrY = i
				for n := 0; n < topOff && s.scrY+1 < len(s.wrapRows) && s.wrapRows[s.scrY+1].line == topLine; n++ {
					s.scrY++
				}
				break
			}
		}
	}
}

// buildWrapRows prefers breaks after spaces and tabs (expanded to spaces). Other
// characters, including punctuation and nonbreaking spaces, stay with their word.
// Oversized words and whitespace runs still split at w cells; no text is discarded.
// A full final row needs an extra empty row for the end-of-line caret.
func (s *Screen) buildWrapRows(w int) {
	w = max(w, 1)
	s.wrapRows = s.wrapRows[:0]
	for i, line := range s.lines {
		if spans := s.liveSpans(i, w); spans != nil {
			disp, _ := liveDisplay(spans)
			s.appendWrapRows(i, disp, w, true)
			continue
		}
		s.appendWrapRows(i, expandLine(line), w, false)
	}
}

// appendWrapRows wraps one line's display runes at w cells.
func (s *Screen) appendWrapRows(line int, disp []rune, w int, rendered bool) {
	n, lastWidth := len(disp), 0
	for start := 0; start < n; {
		end := min(start+w, n)
		// An exact word boundary already fits. Otherwise retreat to the last
		// separator, moving the entire next word onto a fresh row.
		if end < n && disp[end] != ' ' && disp[end-1] != ' ' {
			for at := end - 1; at >= start; at-- {
				if disp[at] == ' ' {
					end = at + 1
					break
				}
			}
		}
		s.wrapRows = append(s.wrapRows, wrapRow{line, start, end, rendered, w})
		lastWidth, start = end-start, end
	}
	// A rendered row never holds the caret, so it needs no end-of-line row.
	if n == 0 || lastWidth == w && !rendered {
		s.wrapRows = append(s.wrapRows, wrapRow{line, n, n, rendered, w})
	}
}

// settleRows brings the wrap cache up to date before a caller reads scrY: under live
// rendering a rebuild can move it to keep the top line in place.
func (s *Screen) settleRows() {
	if s.wrap {
		s.rebuildWrapRows()
	}
}

// wrapTotalRows is the number of display rows the wrapped buffer occupies.
func (s *Screen) wrapTotalRows() int {
	s.rebuildWrapRows()
	return len(s.wrapRows)
}

// wrapRowForCursor finds the caret's row in the same cache the render uses, so the caret
// is always on the row it is drawn on.
func (s *Screen) wrapRowForCursor() int {
	s.rebuildWrapRows()
	cell := cellOfCol(s.lines[s.curY], s.curX)
	last := 0
	for i, r := range s.wrapRows {
		if r.line != s.curY {
			continue
		}
		if cell < r.end {
			return i
		}
		last = i // end of line: the line's last row owns the caret
	}
	return last
}

// renderWrappedRow renders display row idx: gutter (numbered on the line's first row) and
// its chunk, with the caret when present. The chunk start is the window origin, as scrX
// is unwrapped.
func (s *Screen) renderWrappedRow(idx int) string {
	r := s.wrapRows[idx]
	if r.rendered {
		if spans := s.liveSpans(r.line, r.width); spans != nil {
			return s.gutterText(r.line, r.start == 0) + renderLiveWindow(spans, r.start, r.end)
		}
	}
	line := s.lines[r.line]
	disp := expandLine(line)
	start, end := min(r.start, len(disp)), min(r.end, len(disp))
	num := s.gutterText(r.line, r.start == 0)
	// A caret one cell past this chunk is only THIS row's to draw when the line ends
	// here. Mid-line it belongs to the next row, at its column 0.
	eol := s.lastRowOfLine(idx)

	if s.hl != nil || len(s.hlOverlayRows) > 0 {
		if styled, ok := s.renderLineStyled(r.line, start, end, eol); ok {
			return num + styled
		}
	}
	return num + s.renderLinePlain(r.line, start, end, eol)
}

// lastRowOfLine reports whether display row idx is the final one of its buffer line.
func (s *Screen) lastRowOfLine(idx int) bool {
	return idx == len(s.wrapRows)-1 || s.wrapRows[idx+1].line != s.wrapRows[idx].line
}

// renderLine renders one buffer row's window in display cells behind the gutter, with the
// caret as a reverse-video cell (a blank at end of line). With a highlighter the window
// renders through its spans; the caret still wins. Styles never change widths.
// Unfocused, text keeps its styling and the caret is hidden.
func (s *Screen) renderLine(row int) string {
	if spans := s.liveSpans(row, s.contentW()); spans != nil {
		return s.renderLiveLine(row, spans)
	}
	disp := expandLine(s.lines[row])
	w := s.contentW()
	start := s.scrX
	if start > len(disp) {
		start = len(disp)
	}
	end := s.scrX + w
	// over: the line continues past the window, so the last column shows the marker, and eol
	// is false so no end-of-line blank lands in that cell.
	over := w >= 2 && len(disp) > end
	if over {
		end--
	}
	if end > len(disp) {
		end = len(disp)
	}
	num := s.gutterText(row, true)
	var body string
	done := false
	if s.hl != nil || len(s.hlOverlayRows) > 0 {
		body, done = s.renderLineStyled(row, start, end, !over)
	}
	if !done {
		body = s.renderLinePlain(row, start, end, !over)
	}
	if over {
		body += core.MutedStyle().Render(string(editorOverflowMark))
	}
	return num + body
}

// renderLinePlain layers selection and caret over a cell window. Selection is
// converted to cells so a tab's whole expansion is highlighted.
func (s *Screen) renderLinePlain(row, start, end int, eol bool) string {
	line := s.lines[row]
	disp := expandLine(line)
	guides := s.indentGuideCells(line)
	vis := disp[start:end]
	c := -1
	if s.focused && row == s.curY {
		c = cellOfCol(s.lines[row], s.curX) - start
	}
	selected := lipgloss.NewStyle().Background(core.MutedColor).Foreground(core.OnFocusedColor)
	selFrom, selTo, hasSel := s.selectedCells(row)
	inSel := func(cell int) bool { return hasSel && cell >= selFrom && cell < selTo }
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := inSel(start + i)
		match := s.cellMatched(row, start+i)
		guide := guideCell(guides, start+i)
		j := i + 1
		for j < len(vis) && j != c && inSel(start+j) == sel &&
			s.cellMatched(row, start+j) == match && guideCell(guides, start+j) == guide {
			j++
		}
		style := lipgloss.NewStyle()
		styled := false
		if guide {
			style = style.Foreground(core.MutedColor)
			styled = true
		}
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
			styled = true
		} else if match {
			style = s.editorSearchStyle()
			styled = true
		}
		text := string(vis[i:j])
		if guide {
			text = strings.Repeat(string(editorIndentGuide), j-i)
		}
		if styled {
			b.WriteString(style.Render(text))
		} else {
			b.WriteString(text)
		}
		i = j
	}
	if eol && end-start < s.contentW() {
		switch {
		case c == len(vis):
			b.WriteString(editorCursorStyle.Render(" "))
		case s.newlineSelected(row):
			b.WriteString(selected.Render(" "))
		}
	}
	return b.String()
}

// indentGuideCells marks complete leading indent levels in display cells, so guides align
// through tabs, mixed indent, scrolling and wrap.
func (s *Screen) indentGuideCells(line []rune) []bool {
	if !s.indentGuides {
		return nil
	}
	leading := len(leadingWhitespace(line))
	leadingCells := cellOfCol(line, leading)
	unit := s.indentUnit()
	unitCells := len(unit)
	if len(unit) == 1 && unit[0] == '\t' {
		unitCells = editorTabWidth
	}
	if unitCells <= 0 || leadingCells < unitCells {
		return nil
	}
	guides := make([]bool, leadingCells)
	for cell := 0; cell+unitCells <= leadingCells; cell += unitCells {
		guides[cell] = true
	}
	return guides
}

func guideCell(guides []bool, cell int) bool {
	return cell >= 0 && cell < len(guides) && guides[cell]
}

// selectedCells is the row's selected window in display cells (ok=false for none),
// computed once per row: per-cell cellOfCol calls made highlighted rows quadratic.
func (s *Screen) selectedCells(row int) (from, to int, ok bool) {
	if !s.selectionActive() || row < s.selStart.y || row > s.selEnd.y {
		return 0, 0, false
	}
	line := s.lines[row]
	fromCol, toCol := 0, len(line)
	if row == s.selStart.y {
		fromCol = s.selStart.x
	}
	if row == s.selEnd.y {
		toCol = s.selEnd.x
	}
	return cellOfCol(line, fromCol), cellOfCol(line, toCol), true
}

// newlineSelected reports whether the selection crosses the newline after row, which
// renders as one dim blank.
func (s *Screen) newlineSelected(row int) bool {
	return s.selectionActive() && row >= s.selStart.y && row < s.selEnd.y
}

// rebuildSearchMatches refreshes the per-line match cache after a query or buffer change.
// Search is literal, case-insensitive, line-local and non-overlapping.
func (s *Screen) rebuildSearchMatches() {
	if s.searchSeq == s.editSeq && s.searchCached == s.searchQuery {
		return
	}
	s.searchSeq, s.searchCached = s.editSeq, s.searchQuery
	s.searchMatches = make([][]textRange, len(s.lines))
	query := []rune(s.searchQuery)
	if !s.searchEnabled || len(query) == 0 {
		return
	}
	for row, line := range s.lines {
		for from := 0; from+len(query) <= len(line); {
			to := from + len(query)
			if strings.EqualFold(string(line[from:to]), s.searchQuery) {
				s.searchMatches[row] = append(s.searchMatches[row], textRange{
					from: cellOfCol(line, from),
					to:   cellOfCol(line, to),
				})
				from = to
				continue
			}
			from++
		}
	}
}

// cellMatched reports whether a cell is in a search match, starting from the first sorted
// range ending after it.
func (s *Screen) cellMatched(row, cell int) bool {
	if s.searchQuery == "" || row < 0 || row >= len(s.lines) {
		return false
	}
	s.rebuildSearchMatches()
	matches := s.searchMatches[row]
	lo, hi := 0, len(matches)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if matches[mid].to <= cell {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(matches) && cell >= matches[lo].from
}

var (
	// Search yellow ignores the theme and focus so matches stay recognizable; the light
	// terminal shade is darker to show on white.
	editorSearchYellow = core.Color{Light: 136, Dark: 226}
	editorSearchText   = lipgloss.Color("232")
)

// editorSearchStyle is distinct from selection and independent of theme and focus;
// selection and caret still paint over it.
func (s *Screen) editorSearchStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(core.Resolve(editorSearchYellow)).Foreground(editorSearchText)
}

// hlSpans answers from the provisional viewport parse, then the rebased exact snapshot.
// Direct highlighters without a factory keep the lazy synchronous path.
func (s *Screen) hlSpans(row int) []Span {
	if s.hlFactory == nil && s.hlSeq != s.editSeq && (s.hlSeq < 0 || s.highlightWait(time.Now()) == 0) {
		s.parseHighlight()
	}
	if s.hlFactory != nil && s.hlSeq != s.editSeq && row >= s.hlDirty && !s.previewCovers(row) {
		s.refreshHighlightPreview()
	}
	var spans []Span
	if s.previewCovers(row) {
		spans = s.hlPreview[row]
	} else if row >= 0 && row < len(s.hlRows) && s.hlRows[row] >= 0 {
		spans = s.hl.HighlightLine(s.hlRows[row])
	} else if s.hlFactory == nil && s.hl != nil {
		spans = s.hl.HighlightLine(row)
	}
	if src := s.liveSourceSpans(row); src != nil && spansMatchLine(src, s.lines[row]) {
		spans = src
	}
	spans = s.applyHighlightOverlay(row, spans)
	if !spansMatchLine(spans, s.lines[row]) {
		return nil
	}
	return spans
}

// renderLineStyled renders the window [start, end) through the highlighter's spans
// (start is scrX unwrapped, the chunk start wrapped). Tabs take their span's style,
// same-span runs render together, and the caret is spliced in reverse-video (at end of
// line only when eol). ok=false falls back to the plain render.
func (s *Screen) renderLineStyled(row, start, end int, eol bool) (string, bool) {
	spans := s.hlSpans(row)
	if spans == nil {
		return "", false
	}
	line := s.lines[row]
	guides := s.indentGuideCells(line)
	// Group runs by span index: lipgloss.Style is not comparable.
	idx := make([]int, len(line))
	pos := 0
	for i, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		for c := pos; c < pos+n && c < len(idx); c++ {
			idx[c] = i
		}
		pos += n
	}
	drunes := make([]rune, 0, len(line)+8)
	didx := make([]int, 0, len(line)+8)
	for i, r := range line {
		if r == '\t' {
			for k := 0; k < editorTabWidth; k++ {
				drunes = append(drunes, ' ')
				didx = append(didx, idx[i])
			}
		} else {
			drunes = append(drunes, r)
			didx = append(didx, idx[i])
		}
	}
	// Live glyphs swap single cells; each gets a style slot past the highlighter's spans.
	if gs := s.activeGlyphs(row); len(gs) > 0 {
		spans = spans[:len(spans):len(spans)]
		for _, g := range gs {
			cell := cellOfCol(line, g.Col)
			drunes[cell] = g.Text
			didx[cell] = len(spans)
			spans = append(spans, Span{Style: g.Style})
		}
	}
	vis, vidx := drunes[start:end], didx[start:end]
	c := -1 // no cursor splice off the cursor row
	if s.focused && row == s.curY {
		c = cellOfCol(line, s.curX) - start // start is the window origin in BOTH modes
	}
	selFrom, selTo, hasSel := s.selectedCells(row)
	inSel := func(cell int) bool { return hasSel && cell >= selFrom && cell < selTo }
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := inSel(start + i)
		match := s.cellMatched(row, start+i)
		guide := guideCell(guides, start+i)
		j := i + 1
		for j < len(vis) && j != c && vidx[j] == vidx[i] && inSel(start+j) == sel &&
			s.cellMatched(row, start+j) == match && guideCell(guides, start+j) == guide {
			j++
		}
		style, styled := spans[vidx[i]].SpanStyle()
		if guide {
			style = style.Foreground(core.MutedColor)
			styled = true
		}
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
			styled = true
		} else if match {
			style = s.editorSearchStyle()
			styled = true
		}
		text := string(vis[i:j])
		if guide {
			text = strings.Repeat(string(editorIndentGuide), j-i)
		}
		// Unstyled runs skip Style.Render, which walks the whole box model and dominated frame
		// time; a pointer style makes the absence cheap to test.
		if styled {
			b.WriteString(style.Render(text))
		} else {
			b.WriteString(text)
		}
		i = j
	}
	if eol && end-start < s.contentW() {
		switch {
		case row == s.curY && c == len(vis): // only the window the line ends in
			b.WriteString(editorCursorStyle.Render(" "))
		case s.newlineSelected(row):
			b.WriteString(lipgloss.NewStyle().Background(core.MutedColor).Foreground(core.OnFocusedColor).Render(" "))
		}
	}
	return b.String(), true
}
