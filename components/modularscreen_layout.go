package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"charm.land/lipgloss/v2"
	"github.com/brohd11/bubblestack/core"
)

// LayoutAxis describes the direction in which a group's children are arranged.
type LayoutAxis uint8

const (
	LayoutHorizontal LayoutAxis = iota // children side by side
	LayoutVertical                     // children above one another
)

// LayoutNode is either a Slot leaf or a group of Children. Size fixes this
// node's extent in cells along its parent's axis; zero uses Weight (default 1).
// A leaf's Slot.Weight supplies the default when its node Weight is unset.
// Groups fill their allocated rectangle, and leaves are padded/clipped to it.
// Slot.ExpandV/H remain useful for the original column-layout constructor;
// a composable layout allocates all space directly through its split weights.
//
// ID names a group in ResizeState.Splits. Use stable IDs when groups can be
// hidden or reordered; unnamed groups use their structural path. IDs must be
// unique. Mixing a Slot with Children is a programming error.
type LayoutNode struct {
	ID       string
	Slot     *Slot
	Axis     LayoutAxis
	Children []LayoutNode
	Size     int
	Weight   float64
}

// SplitState records a group's child allocations, in declaration order. Sizes
// uses the same fixed/weighted encoding as LayoutNode.Size; Weights controls
// only children whose Size is zero. A mismatched child count restores defaults.
type SplitState struct {
	Sizes   []int
	Weights []float64
}

type layoutBranch struct {
	id          string
	axis        LayoutAxis
	slot        *Slot
	parent      *layoutBranch
	children    []*layoutBranch
	index, leaf int
	first, last int // half-open range of descendant leaf indexes
	rect        panelRect
	minW, minH  int
	sizes       []int
	weights     []float64
	defaults    SplitState
}

// NewModularLayout builds one screen over a tree of horizontal/vertical splits.
// Only leaves are input targets; groups never introduce nested screen updates.
// ColWidths and the legacy positional resize fields belong to NewModularScreen;
// composable layouts declare widths in nodes and restore ResizeState.Splits.
func NewModularLayout(root LayoutNode, opts ModularOpts) *ModularScreen {
	s := NewModularScreen(nil, opts)
	s.layoutGroups = make(map[string]*layoutBranch)
	var build func(LayoutNode, *layoutBranch, int, string) *layoutBranch
	build = func(node LayoutNode, parent *layoutBranch, index int, path string) *layoutBranch {
		g := &layoutBranch{parent: parent, index: index, leaf: -1, first: len(s.flat), axis: node.Axis}
		if node.Slot != nil {
			if len(node.Children) > 0 || node.Slot.Panel == nil {
				panic("components: layout leaf requires a panel and no children")
			}
			slot := *node.Slot
			g.slot = &slot
			g.leaf = len(s.flat)
			s.flat = append(s.flat, g.slot)
			s.layoutLeaves = append(s.layoutLeaves, g)
			g.minW, g.minH = defaultResizeMinW, defaultResizeMinH
			if opts.Resize != nil {
				if opts.Resize.MinW > 0 {
					g.minW = opts.Resize.MinW
				}
				if opts.Resize.MinH > 0 {
					g.minH = opts.Resize.MinH
				}
			}
		} else {
			if node.Axis != LayoutHorizontal && node.Axis != LayoutVertical {
				panic("components: invalid layout axis")
			}
			g.id = node.ID
			if g.id == "" {
				g.id = path
			}
			if _, exists := s.layoutGroups[g.id]; exists {
				panic("components: duplicate layout group ID: " + g.id)
			}
			s.layoutGroups[g.id] = g
			for i, child := range node.Children {
				branch := build(child, g, i, fmt.Sprintf("%s/%d", path, i))
				g.children = append(g.children, branch)
				g.sizes = append(g.sizes, max(0, child.Size))
				weight := child.Weight
				if !validFrac(weight) {
					weight = 1
					if child.Slot != nil {
						weight = float64(weightOf(*child.Slot))
					}
				}
				g.weights = append(g.weights, weight)
				if g.axis == LayoutHorizontal {
					g.minW += branch.minW
					g.minH = max(g.minH, branch.minH)
				} else {
					g.minH += branch.minH
					g.minW = max(g.minW, branch.minW)
				}
			}
			g.defaults = SplitState{Sizes: append([]int(nil), g.sizes...), Weights: append([]float64(nil), g.weights...)}
			if opts.Resize != nil {
				if state, ok := opts.Resize.State.Splits[g.id]; ok && validSplitState(state, len(g.children)) {
					g.sizes = append([]int(nil), state.Sizes...)
					g.weights = append([]float64(nil), state.Weights...)
				}
			}
		}
		g.last = len(s.flat)
		return g
	}
	s.layout = build(root, nil, 0, "root")
	if f := s.firstFocusable(); f >= 0 {
		s.focus = f
		s.focusedPanel().(Focusable).Focus()
	}
	return s
}

func validSplitState(state SplitState, n int) bool {
	if len(state.Sizes) != n || len(state.Weights) != n {
		return false
	}
	for i, size := range state.Sizes {
		if size < 0 || !validFrac(state.Weights[i]) {
			return false
		}
	}
	return true
}

func (s *ModularScreen) sizeLayout(width, height int) {
	y := 0
	if s.title != "" {
		y = lipgloss.Height(core.RenderTitleBar(s.title))
	}
	s.bodyH = max(0, height-y)
	s.rects = make([]panelRect, len(s.flat))
	s.hitRects = nil
	s.edges = nil
	var size func(*layoutBranch, panelRect)
	size = func(g *layoutBranch, r panelRect) {
		g.rect = r
		if g.slot != nil {
			s.rects[g.leaf] = r
			g.slot.Panel.SetSize(r.w, r.h)
			return
		}
		extent := r.w
		mins := make([]int, len(g.children))
		for i, c := range g.children {
			mins[i] = c.minW
			if g.axis == LayoutVertical {
				mins[i] = c.minH
			}
		}
		if g.axis == LayoutVertical {
			extent = r.h
		}
		lengths := splitLengths(extent, g.sizes, g.weights, mins)
		at := 0
		// Parent boundaries precede descendant boundaries at intersections.
		if s.resize != nil {
			for i := 0; i+1 < len(g.children); i++ {
				at += lengths[i]
				edge := resizeEdge{group: g, row: i, vertical: g.axis == LayoutHorizontal, at: r.x + at, lo: r.y, hi: r.y + r.h}
				if g.axis == LayoutVertical {
					edge.at = r.y + at
					edge.lo = r.x
					edge.hi = r.x + r.w
				}
				s.edges = append(s.edges, edge)
			}
		}
		at = 0
		for i, c := range g.children {
			child := r
			if g.axis == LayoutHorizontal {
				child.x += at
				child.w = lengths[i]
			} else {
				child.y += at
				child.h = lengths[i]
			}
			size(c, child)
			at += lengths[i]
		}
	}
	size(s.layout, panelRect{y: y, w: max(0, width), h: s.bodyH})
}

// splitLengths honors fixed cells and weighted space, freezing children at their
// minimum before distributing the rest. If even minima cannot fit, scale them
// down rather than allocating outside the terminal. The final child takes rounding.
func splitLengths(total int, sizes []int, weights []float64, mins []int) []int {
	n := len(sizes)
	out := make([]int, n)
	if n == 0 {
		return out
	}
	total = max(0, total)
	minimum := 0
	for _, v := range mins {
		minimum += v
	}
	if minimum > total {
		used := 0
		for i, v := range mins {
			out[i] = total * v / minimum
			used += out[i]
		}
		out[n-1] += total - used
		return out
	}
	fixed, flexMin, extra := 0, 0, 0
	var flex []int
	for i, size := range sizes {
		if size > 0 {
			out[i] = max(size, mins[i])
			fixed += out[i]
			extra += out[i] - mins[i]
		} else {
			flex = append(flex, i)
			flexMin += mins[i]
		}
	}
	if fixed+flexMin > total {
		budget := total - minimum
		fixed = 0
		for i, size := range sizes {
			if size > 0 {
				out[i] = mins[i] + (out[i]-mins[i])*budget/extra
				fixed += out[i]
			}
		}
	}
	remaining := total - fixed
	for len(flex) > 0 {
		sum := 0.0
		for _, i := range flex {
			sum += weights[i]
		}
		frozen := false
		for j, i := range flex {
			if float64(remaining)*weights[i]/sum < float64(mins[i]) {
				out[i] = mins[i]
				remaining -= out[i]
				flex = append(flex[:j], flex[j+1:]...)
				frozen = true
				break
			}
		}
		if frozen {
			continue
		}
		used := 0
		for j, i := range flex {
			out[i] = int(math.Floor(float64(remaining)*weights[i]/sum + 1e-9))
			if j == len(flex)-1 {
				out[i] = remaining - used
			}
			used += out[i]
		}
		remaining = 0
		break
	}
	// A group of fixed children still fills its allocation.
	if remaining > 0 {
		out[n-1] += remaining
	}
	return out
}

// Fit only rows that need padding or clipping. Rendering the entire pane
// through another lipgloss style would segment and allocate for every cell again.
func fitLayoutBody(body string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.SplitN(body, "\n", height+1)
	changed := len(lines) != height
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		w := ansi.StringWidth(line)
		if w > width {
			lines[i] = ansi.Truncate(line, width, "")
			changed = true
		} else if w < width {
			lines[i] = line + strings.Repeat(" ", width-w)
			changed = true
		}
	}
	if !changed {
		return body
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func (s *ModularScreen) viewLayout(sh *core.Shared) string {
	var render func(*layoutBranch) string
	render = func(g *layoutBranch) string {
		if g.rect.w <= 0 || g.rect.h <= 0 {
			return ""
		}
		if g.slot != nil {
			return fitLayoutBody(g.slot.Panel.View(g.leaf == s.focus && s.hostFocused), g.rect.w, g.rect.h)
		}
		parts := make([]string, 0, len(g.children))
		for _, child := range g.children {
			if child.rect.w > 0 && child.rect.h > 0 {
				parts = append(parts, render(child))
			}
		}
		if len(parts) == 0 {
			return fitLayoutBody("", g.rect.w, g.rect.h)
		}
		if len(parts) == 1 {
			return parts[0]
		}
		if g.axis == LayoutHorizontal {
			return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
		}
		return lipgloss.JoinVertical(lipgloss.Left, parts...)
	}
	s.hitRects = append(s.hitRects[:0], s.rects...)
	for _, leaf := range s.layoutLeaves {
		if po, ok := leaf.slot.Panel.(PaneOriginer); ok {
			po.SetPaneOrigin(leaf.rect.x, sh.BodyY()+leaf.rect.y)
		}
	}
	return core.WithTitle(s.title, render(s.layout))
}

func (s *ModularScreen) layoutNeighbor(from, dx, dy int) int {
	axis, step := LayoutHorizontal, dx
	if dy != 0 {
		axis, step = LayoutVertical, dy
	}
	if step == 0 {
		return -1
	}
	origin := s.layoutLeaves[from].rect
	for child := s.layoutLeaves[from]; child.parent != nil; child = child.parent {
		g := child.parent
		if g.axis != axis {
			continue
		}
		for j := child.index + step; j >= 0 && j < len(g.children); j += step {
			branch := g.children[j]
			best, bestDistance := -1, math.Inf(1)
			for i := branch.first; i < branch.last; i++ {
				if !isFocusable(s.flat[i].Panel) {
					continue
				}
				r := s.layoutLeaves[i].rect
				// The closest center in the adjacent subtree resolves unequal
				// row/column splits; declaration order breaks exact ties.
				x := float64(2*r.x + r.w - 2*origin.x - origin.w)
				y := float64(2*r.y + r.h - 2*origin.y - origin.h)
				d := x*x + y*y
				if d < bestDistance {
					best, bestDistance = i, d
				}
			}
			if best >= 0 {
				return best
			}
		}
	}
	return -1
}
