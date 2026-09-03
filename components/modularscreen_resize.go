package components

import (
	"math"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
)

// ResizeOpts turns on drag-to-resize and the keyboard resize mode. A nil
// *ResizeOpts leaves ModularScreen's layout and input behavior unchanged.
type ResizeOpts struct {
	MinW     int         // minimum column width in cells; zero means 8
	MinH     int         // minimum slot height in rows; zero means 3
	Key      key.Binding // enters resize mode; zero means ctrl+alt+n
	State    ResizeState // proportions restored from a previous screen
	OnChange func(ResizeState)
}

// ResizeState is a snapshot of one layout's adjusted proportions. Its slices
// are positional in the same column and slot order used to build the screen.
// Cols uses ModularOpts.ColWidths' encoding: a positive cell width is fixed and
// zero is flex. Flex entries are meaningful only for flex columns.
type ResizeState struct {
	Cols []int
	Flex []float64
	Rows [][]float64
}

type resizeEdge struct {
	vertical bool
	col, row int
	at       int
	lo, hi   int
}

const (
	defaultResizeMinW = 8
	defaultResizeMinH = 3
)

func (s *ModularScreen) initResize(cols []int, state ResizeState) {
	s.defaultCols = make([]int, len(s.cols))
	copy(s.defaultCols, cols)
	s.colWidths = append([]int(nil), s.defaultCols...)
	for c := range s.colWidths {
		if c < len(state.Cols) {
			s.colWidths[c] = max(state.Cols[c], 0)
		}
	}

	s.flexFrac = make([]float64, len(s.cols))
	s.seedFlex(s.flexFrac)
	for c := range s.flexFrac {
		if s.colWidths[c] == 0 && c < len(state.Flex) && validFrac(state.Flex[c]) {
			s.flexFrac[c] = state.Flex[c]
		}
	}
	s.normalizeFlex()

	s.rowFrac = make([][]float64, len(s.cols))
	for c, col := range s.cols {
		s.rowFrac[c] = weightFractions(col)
		if c >= len(state.Rows) || len(state.Rows[c]) != len(col) {
			continue
		}
		valid := true
		for _, f := range state.Rows[c] {
			if !validFrac(f) {
				valid = false
				break
			}
		}
		if valid {
			copy(s.rowFrac[c], state.Rows[c])
			normalize(s.rowFrac[c])
		}
	}
}

func validFrac(f float64) bool {
	return f > 0 && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func weightFractions(col []Slot) []float64 {
	frac := make([]float64, len(col))
	total := 0
	for _, slot := range col {
		total += weightOf(slot)
	}
	if total == 0 {
		return frac
	}
	for i, slot := range col {
		frac[i] = float64(weightOf(slot)) / float64(total)
	}
	return frac
}

func normalize(frac []float64) {
	total := 0.0
	for _, f := range frac {
		total += f
	}
	if total <= 0 {
		return
	}
	for i := range frac {
		frac[i] /= total
	}
}

func (s *ModularScreen) seedFlex(dst []float64) {
	n := 0
	for c := range s.cols {
		if c >= len(s.colWidths) || s.colWidths[c] == 0 {
			n++
		}
	}
	if n == 0 {
		return
	}
	share := 1 / float64(n)
	for c := range s.cols {
		if c >= len(s.colWidths) || s.colWidths[c] == 0 {
			dst[c] = share
		}
	}
}

func (s *ModularScreen) normalizeFlex() {
	total := 0.0
	for c := range s.flexFrac {
		if s.colWidths[c] == 0 && validFrac(s.flexFrac[c]) {
			total += s.flexFrac[c]
		} else {
			s.flexFrac[c] = 0
		}
	}
	if total == 0 {
		s.seedFlex(s.flexFrac)
		return
	}
	for c := range s.flexFrac {
		if s.colWidths[c] == 0 {
			s.flexFrac[c] /= total
		}
	}
}

func (s *ModularScreen) minW() int {
	if s.resize.MinW > 0 {
		return s.resize.MinW
	}
	return defaultResizeMinW
}

func (s *ModularScreen) minH() int {
	if s.resize.MinH > 0 {
		return s.resize.MinH
	}
	return defaultResizeMinH
}

func (s *ModularScreen) setResizeSize(width, bodyHeight int) {
	y0 := 0
	if s.title != "" {
		y0 = lipgloss.Height(core.RenderTitleBar(s.title))
		bodyHeight -= y0
	}
	s.bodyH = bodyHeight
	widths := s.resizeWidths(width)
	s.rects = make([]panelRect, 0, len(s.flat))
	s.hitRects = nil

	x := 0
	for c, col := range s.cols {
		used := 0
		for i, slot := range col {
			h := bodyHeight - used
			if i < len(col)-1 {
				h = int(float64(bodyHeight) * s.rowFrac[c][i])
			}
			h = max(h, s.minH())
			s.rects = append(s.rects, panelRect{x: x, y: y0 + used, w: widths[c], h: h})
			used += h
			slot.Panel.SetSize(widths[c], h)
		}
		x += widths[c]
	}
	s.buildResizeEdges(y0, bodyHeight)
}

func (s *ModularScreen) resizeWidths(width int) []int {
	widths := make([]int, len(s.cols))
	fixed, lastFlex := 0, -1
	for c := range s.cols {
		if s.colWidths[c] > 0 {
			widths[c] = max(s.colWidths[c], s.minW())
			fixed += widths[c]
		} else {
			lastFlex = c
		}
	}
	if lastFlex < 0 {
		return widths
	}
	space, used := width-fixed, 0
	for c := range s.cols {
		if s.colWidths[c] != 0 || c == lastFlex {
			continue
		}
		widths[c] = max(int(float64(space)*s.flexFrac[c]), s.minW())
		used += widths[c]
	}
	widths[lastFlex] = max(space-used, s.minW())
	return widths
}

func (s *ModularScreen) buildResizeEdges(y0, bodyHeight int) {
	s.edges = s.edges[:0]
	for c := 0; c+1 < len(s.cols); c++ {
		at := 0
		if len(s.cols[c+1]) > 0 {
			at = s.rects[s.starts[c+1]].x
		} else if len(s.cols[c]) > 0 {
			r := s.rects[s.starts[c]]
			at = r.x + r.w
		}
		s.edges = append(s.edges, resizeEdge{vertical: true, col: c, at: at, lo: y0, hi: y0 + bodyHeight})
	}
	for c, col := range s.cols {
		for row := 0; row+1 < len(col); row++ {
			lower := s.rects[s.starts[c]+row+1]
			s.edges = append(s.edges, resizeEdge{col: c, row: row, at: lower.y, lo: lower.x, hi: lower.x + lower.w})
		}
	}
}

func (s *ModularScreen) edgeAt(sh *core.Shared, x, y int) int {
	relY := y - sh.BodyY()
	for i, edge := range s.edges {
		if edge.vertical {
			if (x == edge.at-1 || x == edge.at) && relY >= edge.lo && relY < edge.hi {
				return i
			}
			continue
		}
		if (relY == edge.at-1 || relY == edge.at) && x >= edge.lo && x < edge.hi {
			return i
		}
	}
	return -1
}

func (s *ModularScreen) applyDelta(edgeIndex, delta int) {
	if delta == 0 || edgeIndex < 0 || edgeIndex >= len(s.edges) {
		return
	}
	edge := s.edges[edgeIndex]
	if edge.vertical {
		s.applyColumnDelta(edge.col, delta)
		return
	}
	s.applyRowDelta(edge.col, edge.row, delta)
}

func (s *ModularScreen) applyColumnDelta(col, delta int) {
	if col < 0 || col+1 >= len(s.cols) {
		return
	}
	left, right := s.columnWidth(col), s.columnWidth(col+1)
	leftFixed, rightFixed := s.colWidths[col] > 0, s.colWidths[col+1] > 0

	switch {
	case leftFixed:
		delta = max(delta, s.minW()-left)
		if !rightFixed {
			delta = min(delta, right-s.minW())
		}
		s.colWidths[col] = left + delta
	case rightFixed:
		delta = max(delta, s.minW()-left)
		delta = min(delta, right-s.minW())
		s.colWidths[col+1] = right - delta
	default:
		delta = max(delta, s.minW()-left)
		delta = min(delta, right-s.minW())
		space := s.flexSpace()
		if space <= 0 {
			return
		}
		shift := float64(delta) / float64(space)
		s.flexFrac[col] += shift
		s.flexFrac[col+1] -= shift
		s.normalizeFlex()
	}
}

func (s *ModularScreen) applyRowDelta(col, row, delta int) {
	if col < 0 || col >= len(s.cols) || row < 0 || row+1 >= len(s.cols[col]) || s.bodyH <= 0 {
		return
	}
	upper := s.rects[s.starts[col]+row].h
	lower := s.rects[s.starts[col]+row+1].h
	delta = max(delta, s.minH()-upper)
	delta = min(delta, lower-s.minH())
	shift := float64(delta) / float64(s.bodyH)
	s.rowFrac[col][row] += shift
	s.rowFrac[col][row+1] -= shift
	normalize(s.rowFrac[col])
}

func (s *ModularScreen) columnWidth(col int) int {
	if col < 0 || col >= len(s.cols) {
		return 0
	}
	if len(s.cols[col]) > 0 && len(s.rects) == len(s.flat) {
		return s.rects[s.starts[col]].w
	}
	if s.lastW > 0 {
		return s.resizeWidths(s.lastW)[col]
	}
	return 0
}

func (s *ModularScreen) flexSpace() int {
	fixed := 0
	for c := range s.cols {
		if s.colWidths[c] > 0 {
			fixed += max(s.colWidths[c], s.minW())
		}
	}
	return s.lastW - fixed
}

func (s *ModularScreen) relayout() {
	if s.lastW > 0 && s.lastH > 0 {
		s.SetSize(nil, s.lastW, s.lastH)
	}
}

func (s *ModularScreen) resizeKeyMatches(k string) bool {
	keys := s.resize.Key.Keys()
	if len(keys) == 0 {
		return k == "ctrl+alt+n"
	}
	return core.MatchKey(k, s.resize.Key)
}

// SetResizing enters or leaves keyboard resize mode. Leaving an active mode
// publishes the final snapshot through ResizeOpts.OnChange.
func (s *ModularScreen) SetResizing(resizing bool) {
	if s.resize == nil || s.resizing == resizing {
		return
	}
	s.resizing = resizing
	if !resizing {
		s.notifyResize()
	}
}

// Resizing reports whether the keyboard resize mode is active.
func (s *ModularScreen) Resizing() bool { return s.resizing }

// Nudge moves the focused pane's trailing edge, or its leading edge when the
// pane is the last sibling on that axis. dw and dh are physical edge movement
// in terminal cells, so moving the leading edge right narrows the final pane.
func (s *ModularScreen) Nudge(dw, dh int) {
	if s.resize == nil || s.focus < 0 {
		return
	}
	p := s.pos[s.focus]
	if dw != 0 && len(s.cols) > 1 {
		col := p.col
		if col == len(s.cols)-1 {
			col--
		}
		if edge := s.findResizeEdge(true, col, 0); edge >= 0 {
			s.applyDelta(edge, dw)
		}
	}
	if dh != 0 && len(s.cols[p.col]) > 1 {
		row := p.row
		if row == len(s.cols[p.col])-1 {
			row--
		}
		if edge := s.findResizeEdge(false, p.col, row); edge >= 0 {
			s.applyDelta(edge, dh)
		}
	}
	s.relayout()
}

func (s *ModularScreen) findResizeEdge(vertical bool, col, row int) int {
	for i, edge := range s.edges {
		if edge.vertical == vertical && edge.col == col && (vertical || edge.row == row) {
			return i
		}
	}
	return -1
}

func (s *ModularScreen) resetResize() {
	copy(s.colWidths, s.defaultCols)
	for i := range s.flexFrac {
		s.flexFrac[i] = 0
	}
	s.seedFlex(s.flexFrac)
	for c, col := range s.cols {
		s.rowFrac[c] = weightFractions(col)
	}
	s.relayout()
}

// ResizeState returns a deep snapshot suitable for restoring into ResizeOpts.State.
func (s *ModularScreen) ResizeState() ResizeState {
	state := ResizeState{
		Cols: append([]int(nil), s.colWidths...),
		Flex: append([]float64(nil), s.flexFrac...),
		Rows: make([][]float64, len(s.rowFrac)),
	}
	for c := range s.rowFrac {
		state.Rows[c] = append([]float64(nil), s.rowFrac[c]...)
	}
	return state
}

func (s *ModularScreen) notifyResize() {
	if s.resize != nil && s.resize.OnChange != nil {
		s.resize.OnChange(s.ResizeState())
	}
}
