package components

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Rendering for EditorScreen: the body and its gutter, soft-wrap row mapping, per-line
// styling with syntax-highlight spans, selection and search-match painting, and the help
// view. Nothing here mutates the buffer.

// ---------- rendering ----------

// titleH is the title bar's rendered height, subtracted from the body (and from mouse
// rows) the same way ModularScreen accounts for its own title. The focused and muted
// bars render at the same height, so focus never shifts the body.
func (s *EditorScreen) titleH() int {
	return lipgloss.Height(core.RenderTitleBar(s.titleText()))
}

// insetX and insetY are the body's offsets from the screen's own top-left: the chrome
// this editor draws above and left of the first buffer cell. They are the single
// definition SetSize (which subtracts them) and clickAt (which offsets by them) both
// read, so the two can't drift apart.
//
// Left: the frame's border column when bordered, plus the embedded gutter — one blank
// column keeping the text off a neighbouring pane's border.
func (s *EditorScreen) insetX() int {
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
func (s *EditorScreen) insetY() int {
	if s.bordered {
		return 1
	}
	return s.titleH()
}

func (s *EditorScreen) baseTitleText() string {
	if s.dirty {
		return s.title + " (*)"
	}
	return s.title
}

func (s *EditorScreen) titleText() string { return s.baseTitleText() }

// searchBarVisible reports whether the bottom rows belong to search: while the
// modal editor is open (including its initially empty state), or afterward while a
// retained query is still filtering the buffer.
func (s *EditorScreen) searchBarVisible() bool {
	return s.searchEnabled && (s.searchEditing || s.searchQuery != "")
}

// searchBar renders the unfocused version beneath the viewport. A left click replaces
// it with the focused LineEditScreen, composited directly over the same rounded shell
// and full pane width. Its text is cell-truncated on narrow panes.
func (s *EditorScreen) searchBar() string {
	w := s.paneW()
	contentW := max(w-4, 1) // two border cells and one padding cell on each side
	content := ansi.Truncate("find: "+s.searchQuery, contentW, "…")
	return retainedSearchBox().Width(contentW + 4).Render(content)
}

// retainedSearchBox is the unfocused counterpart to lineEditBox: same geometry, but
// the framework's ordinary border color rather than the active accent.
func retainedSearchBox() lipgloss.Style {
	return lineEditBox().BorderForeground(core.BorderColor)
}

// View renders the buffer window under its title, both tracking focus: bordered, the
// title (with its (*) modified marker) is the frame's top-border legend and the frame
// carries the tint; unbordered, it is the title bar above the body, muted while a
// sibling pane holds the keys.
func (s *EditorScreen) View(*core.Shared) string {
	var editor string
	if s.bordered {
		editor = frame(s.titleText(), s.body(), s.w+s.gutter(), s.focused)
	} else {
		editor = core.WithTitleFocused(s.titleText(), s.body(), s.focused)
	}
	if !s.searchBarVisible() {
		return editor
	}
	return lipgloss.JoinVertical(lipgloss.Left, editor, s.searchBar())
}

// gutter is the embedded body's one-column left indent (0 standalone) — the part of
// insetX that lives INSIDE the frame, so View adds it back to the frame's inner run.
func (s *EditorScreen) gutter() int {
	if s.embedded {
		return 1
	}
	return 0
}

// body is the viewport itself: s.h rows of the visible row window and — while the
// exit prompt is up — the prompt as the last row, each indented by the gutter. When
// the buffer overflows, the rightmost column is the scrollbar (rows padded up to it,
// so the bar reads as one solid column). Always exactly s.h lines tall AND exactly
// s.w cells wide, so the frame around it stays rectangular: a row one cell over
// wraps in the terminal and shifts every frame after it.
//
// What a "row" is depends on the mode — a buffer line, or one wrapped chunk of one —
// but only rowCount and renderRow know that, so the two modes cannot drift apart in
// how they pad or where they put the bar.
func (s *EditorScreen) body() string {
	rows := s.h
	if s.confirmExit {
		rows-- // the prompt takes the last body row
	}
	bar := s.barVisible()
	total := s.rowCount()
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
			b.WriteString(s.scrollbarCell(i))
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
			b.WriteString(s.scrollbarCell(s.h - 1))
		}
	}
	return b.String()
}

// rowCount is how many rows the viewport scrolls through — what scrY indexes and what
// the scrollbar measures itself against: wrapped display rows while wrap is on, buffer
// lines while it is off.
func (s *EditorScreen) rowCount() int {
	if s.wrap {
		return s.wrapTotalRows()
	}
	return len(s.lines)
}

// renderRow renders row i of rowCount's sequence.
func (s *EditorScreen) renderRow(i int) string {
	if s.wrap {
		return s.renderWrappedRow(i)
	}
	return s.renderLine(i)
}

// gutterOn reports whether the line-number column is drawn: the sticky ctrl+l
// preference, or unconditionally while wrapped — in wrapped text the numbers are the
// only thing separating a soft break from a real line, so wrap turns them on without
// disturbing the preference the toggle goes back to.
func (s *EditorScreen) gutterOn() bool { return s.lineNums || s.wrap }

// numGutterWidth returns the fixed width of the line-number prefix when it is drawn.
// It is just wide enough for the highest line number in the buffer, so short docs do
// not waste columns and tall docs stay aligned. A viewport too narrow to hold the
// numbers and any text gets none: the text wins.
//
// It must not consult textW — that reads barVisible, which under wrap reads the row
// cache, which is measured against this width.
func (s *EditorScreen) numGutterWidth() int {
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

// leftGutterWidth is everything drawn left of the text: the host's sign columns
// (editor_signs.go) plus the line-number column. It is the width the body is measured
// against, and every consumer of a "how far in does text start" answer must use THIS,
// not numGutterWidth — contentW, the wrap rebuild and the click-to-cursor math all
// derive from it, and a site left on the narrower one would misplace clicks by exactly
// the sign column.
//
// It carries numGutterWidth's constraint unchanged: nothing here may consult textW,
// which reads barVisible, which under wrap reads the row cache, which is measured
// against this width. Neither part does.
//
// In a too-narrow viewport the numbers go first, followed by sign columns from the
// outside in. The innermost annotation therefore survives nearest the text.
func (s *EditorScreen) visibleGutter() (signs []string, nums int) {
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

func (s *EditorScreen) leftGutterWidth() int {
	signs, nums := s.visibleGutter()
	return len(signs) + nums
}

// gutterText is everything left of the text for one display row: the visible sign cells
// then the line-number cell. All are blank on a line's wrapped continuations, and the
// string this returns always measures exactly leftGutterWidth cells.
func (s *EditorScreen) gutterText(line int, first bool) string {
	signs, nums := s.visibleGutter()
	if len(signs)+nums == 0 {
		return ""
	}
	return s.signText(signs, line, first) + s.lineNumTextWidth(nums, line, first)
}

// lineNumText is the number cell for one display row: the 1-based line number on a
// line's first row, blanks on its wrapped continuations.
func (s *EditorScreen) lineNumText(line int, first bool) string {
	_, w := s.visibleGutter()
	return s.lineNumTextWidth(w, line, first)
}

func (s *EditorScreen) lineNumTextWidth(w, line int, first bool) string {
	if w == 0 {
		return ""
	}
	if !first {
		return strings.Repeat(" ", w)
	}
	return fmt.Sprintf("%*d ", w-1, line+1)
}

// rebuildWrapRows recomputes the display rows, and with them whether the scrollbar
// column is needed. Nothing it calls may consult textW or barVisible: the text width
// derives from the bar, the bar derives from the row count, and the row count is what
// this builds — reading either here recurses until the stack gives out. So the width is
// settled directly instead: measure at the full width, and if the document overflows,
// measure again one column narrower to make room for the bar. Narrowing can only add
// rows, never remove them, so an overflow at the full width is still an overflow at the
// narrower one and the second pass is final.
//
// The cache is invalidated (wrapDirty) by every edit, resize and toggle.
func (s *EditorScreen) rebuildWrapRows() {
	if !s.wrapDirty {
		return
	}
	s.wrapDirty = false // cleared first: nothing below may re-enter the rebuild
	s.wrapBar = false
	s.buildWrapRows(s.w - s.leftGutterWidth())
	if len(s.wrapRows) > s.h {
		s.wrapBar = true
		s.buildWrapRows(s.w - 1 - s.leftGutterWidth())
	}
}

// buildWrapRows fills the row cache, breaking each buffer line into chunks of at most w
// display cells. A line whose width is an exact multiple of w (an empty line included)
// gets a trailing empty row: without it the caret at end of line would have to sit one
// column past the last chunk, which is off the frame.
func (s *EditorScreen) buildWrapRows(w int) {
	if w < 1 {
		w = 1
	}
	s.wrapRows = s.wrapRows[:0]
	for i, line := range s.lines {
		n := len(expandLine(line))
		for start := 0; start < n; start += w {
			s.wrapRows = append(s.wrapRows, wrapRow{i, start, min(start+w, n)})
		}
		if n%w == 0 {
			s.wrapRows = append(s.wrapRows, wrapRow{i, n, n})
		}
	}
}

// wrapTotalRows is the number of display rows the wrapped buffer occupies.
func (s *EditorScreen) wrapTotalRows() int {
	s.rebuildWrapRows()
	return len(s.wrapRows)
}

// wrapRowForCursor is the display row holding the caret, FOUND in the same cache the
// render reads rather than recomputed from the wrap width — the two agreeing is what
// keeps the caret on the row it is drawn on.
func (s *EditorScreen) wrapRowForCursor() int {
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

// renderWrappedRow renders display row idx: its line-number gutter (numbered on the
// line's first row, blank on its continuations) and its chunk of the line, with the
// caret when this is the row the caret is on. The chunk's start is the window origin
// here, exactly as scrX is in the unwrapped render.
func (s *EditorScreen) renderWrappedRow(idx int) string {
	r := s.wrapRows[idx]
	line := s.lines[r.line]
	disp := expandLine(line)
	start, end := min(r.start, len(disp)), min(r.end, len(disp))
	num := s.gutterText(r.line, r.start == 0)
	// A caret one cell past this chunk is only THIS row's to draw when the line ends
	// here. Mid-line it belongs to the next row, at its column 0.
	eol := s.lastRowOfLine(idx)

	if s.focused && s.hl != nil {
		if styled, ok := s.renderLineStyled(r.line, start, end, eol); ok {
			return num + styled
		}
	}
	return num + s.renderLinePlain(r.line, start, end, eol)
}

// lastRowOfLine reports whether display row idx is the final one of its buffer line.
func (s *EditorScreen) lastRowOfLine(idx int) bool {
	return idx == len(s.wrapRows)-1 || s.wrapRows[idx+1].line != s.wrapRows[idx].line
}

// renderLine renders one buffer row's horizontal window in display cells (tabs
// expanded via expandLine — the raw '\t' never reaches the frame), behind the line
// number gutter when it is on and narrowed by it (contentW), with the cursor
// cell (a reverse-video rune, or a blank at end of line) when the row holds the
// cursor. A cursor sitting on a tab reverses the expansion's first cell.
//
// With a Highlighter set (and focused), the window renders through the spans
// instead: contiguous same-style runs, tabs carrying their span's style through
// the expansion, the cursor cell still reverse-video — the cursor wins over the
// syntax style, exactly as it wins over plain text. Styles never change cell
// widths, so the styled render measures the same as the plain one.
//
// Unfocused the whole window goes muted and the cursor is dropped: a caret in a pane
// the keys don't reach reads as a lie about where typing lands, and one caret per
// pane would leave nothing marking the live one. The muted style is built per call so
// a theme switch repaints it, as styleHelp and StyleList do.
func (s *EditorScreen) renderLine(row int) string {
	disp := expandLine(s.lines[row])
	w := s.contentW()
	start := s.scrX
	if start > len(disp) {
		start = len(disp)
	}
	end := s.scrX + w
	// over: the line runs past the window, so the last column goes to the marker instead
	// of to text. eol is then false — the line does not end in this window, and the tail
	// blank renderLinePlain draws for a caret or a selected newline would land in the
	// marker's cell. Below two columns there is nothing left to mark with.
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
	if s.focused && s.hl != nil {
		body, done = s.renderLineStyled(row, start, end, !over)
	}
	if !done {
		body = s.renderLinePlain(row, start, end, !over)
	}
	if over {
		body += lipgloss.NewStyle().Foreground(core.MutedColor).Render(string(editorOverflowMark))
	}
	return num + body
}

// renderLinePlain applies the muted/unfocused, selection, and caret layers to a
// display-cell window. Selection is measured in rune columns but converted to cells,
// so every cell of an expanded tab receives the same background.
func (s *EditorScreen) renderLinePlain(row, start, end int, eol bool) string {
	disp := expandLine(s.lines[row])
	vis := disp[start:end]
	c := -1
	if s.focused && row == s.curY {
		c = cellOfCol(s.lines[row], s.curX) - start
	}
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	selected := lipgloss.NewStyle().Background(core.MutedColor).Foreground(core.OnFocusedColor)
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := s.cellSelected(row, start+i)
		match := s.cellMatched(row, start+i)
		j := i + 1
		for j < len(vis) && j != c && s.cellSelected(row, start+j) == sel && s.cellMatched(row, start+j) == match {
			j++
		}
		style := lipgloss.NewStyle()
		styled := false
		if !s.focused {
			style = muted
			styled = true
		}
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
			styled = true
		} else if match {
			style = s.editorSearchStyle()
			styled = true
		}
		if styled {
			b.WriteString(style.Render(string(vis[i:j])))
		} else {
			b.WriteString(string(vis[i:j]))
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

func (s *EditorScreen) cellSelected(row, cell int) bool {
	if !s.selectionActive() || row < s.selStart.y || row > s.selEnd.y {
		return false
	}
	from, to := 0, len(s.lines[row])
	if row == s.selStart.y {
		from = s.selStart.x
	}
	if row == s.selEnd.y {
		to = s.selEnd.x
	}
	return cell >= cellOfCol(s.lines[row], from) && cell < cellOfCol(s.lines[row], to)
}

// newlineSelected reports whether the half-open range crosses the newline following
// row. Rendering one dim blank makes multiline selections and selected empty lines
// visible without putting a newline rune into the terminal output.
func (s *EditorScreen) newlineSelected(row int) bool {
	return s.selectionActive() && row >= s.selStart.y && row < s.selEnd.y
}

// rebuildSearchMatches refreshes the per-line match cache when either the query or
// buffer changes. Search is literal, case-insensitive and line-local because the
// input itself is single-line. Advancing by the query width makes results
// non-overlapping, matching conventional find behavior.
func (s *EditorScreen) rebuildSearchMatches() {
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

// cellMatched reports whether one display cell belongs to a search match. Cached
// ranges are sorted, so the lookup narrows to the first range ending after cell
// instead of scanning every match on a common-character search.
func (s *EditorScreen) cellMatched(row, cell int) bool {
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
	// Search yellow is semantic but not thematic: it stays recognizable while themes
	// and pane focus change. The darker light-terminal shade keeps the block visible
	// against white, while dark terminals get the bright form.
	editorSearchYellow = core.Color{Light: 136, Dark: 226}
	editorSearchText   = lipgloss.Color("232")
)

// editorSearchStyle is deliberately distinct from ordinary selection and independent
// of both the active theme and pane focus. Selection and the caret still win in the
// render layer ordering above it.
func (s *EditorScreen) editorSearchStyle() lipgloss.Style {
	return lipgloss.NewStyle().Background(core.Resolve(editorSearchYellow)).Foreground(editorSearchText)
}

// hlSpans answers the row's validated spans, reparsing the buffer first when it
// changed since the last parse — lazy and once per edit sequence, never per
// frame or per row. nil means "render plain": the row is unstyled, or the spans
// failed validation (their concatenated text must reconstruct the buffer line
// exactly — the check that keeps a buggy highlighter from corrupting the frame).
func (s *EditorScreen) hlSpans(row int) []Span {
	if s.hlSeq != s.editSeq {
		s.hl.Parse(s.Text())
		s.hlSeq = s.editSeq
	}
	spans := s.hl.HighlightLine(row)
	if spansText(spans) != string(s.lines[row]) {
		return nil
	}
	return spans
}

// renderLineStyled renders the row's window [start, end) through the
// highlighter's spans — start being the window's origin in display cells, scrX
// unwrapped and the chunk's start wrapped, so the caret lands in the right window
// either way. Per-rune span indexes ride through the tab expansion
// (a tab's cells take its span's style), contiguous same-span runs render in
// one style.Render, and the cursor cell splices in reverse-video — at end of
// line, as the appended styled blank, which only a window the line actually ends
// in (eol) may draw. ok=false falls back to the plain render.
func (s *EditorScreen) renderLineStyled(row, start, end int, eol bool) (string, bool) {
	spans := s.hlSpans(row)
	if spans == nil {
		return "", false
	}
	line := s.lines[row]
	// Cells sharing a span share its style, so the run grouping compares span
	// indexes — never lipgloss.Style values (they carry a func field, so == does
	// not even compile).
	idx := make([]int, len(line))
	pos := 0
	for i, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		for c := pos; c < pos+n && c < len(idx); c++ {
			idx[c] = i
		}
		pos += n
	}
	var drunes []rune
	var didx []int
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
	vis, vidx := drunes[start:end], didx[start:end]
	c := -1 // no cursor splice off the cursor row
	if row == s.curY {
		c = cellOfCol(line, s.curX) - start // start is the window origin in BOTH modes
	}
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := s.cellSelected(row, start+i)
		match := s.cellMatched(row, start+i)
		j := i + 1
		for j < len(vis) && j != c && vidx[j] == vidx[i] && s.cellSelected(row, start+j) == sel && s.cellMatched(row, start+j) == match {
			j++
		}
		style := spans[vidx[i]].Style
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
		} else if match {
			style = s.editorSearchStyle()
		}
		b.WriteString(style.Render(string(vis[i:j])))
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
