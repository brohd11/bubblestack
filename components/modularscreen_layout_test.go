package components

import (
	"math"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/brohd11/bubblestack/core"
)

type layoutProbe struct {
	focusPanel
	w, h                                      int
	initializations, updates, receives, sized int
	mouse                                     tea.Mouse
	ox, oy                                    int
}

func (p *layoutProbe) SetSize(w, h int) { p.w, p.h = w, h; p.sized++ }
func (p *layoutProbe) View(bool) string {
	return strings.Repeat(strings.Repeat("x", max(0, p.w))+"\n", max(0, p.h-1)) + strings.Repeat("x", max(0, p.w))
}
func (p *layoutProbe) Init(*core.Shared) tea.Cmd             { p.initializations++; return nil }
func (p *layoutProbe) Receive(*core.Shared, any) core.Action { p.receives++; return core.Action{} }
func (p *layoutProbe) SetPaneOrigin(x, y int)                { p.ox, p.oy = x, y }
func (p *layoutProbe) UpdatePanel(_ *core.Shared, msg tea.Msg) (core.Action, bool) {
	p.updates++
	if m, ok := msg.(tea.MouseMsg); ok {
		p.mouse = m.Mouse()
	}
	return core.Action{}, true
}

func layoutLeaf(p Panel) LayoutNode { return LayoutNode{Slot: &Slot{Panel: p}} }

func TestModularLayoutFixedBar(t *testing.T) {
	sh := core.NewShared(nil)
	bar, editor, bottom := NewTabBar(), &layoutProbe{}, &layoutProbe{}
	root := LayoutNode{ID: "workspace", Axis: LayoutVertical, Children: []LayoutNode{
		{ID: "document", Axis: LayoutVertical, Weight: 3, Children: []LayoutNode{
			{Slot: &Slot{Panel: bar}, Size: 1, FixedSize: true}, layoutLeaf(editor),
		}}, layoutLeaf(bottom),
	}}
	m := NewModularLayout(root, ModularOpts{Resize: &ResizeOpts{State: ResizeState{Splits: map[string]SplitState{
		"document": {Sizes: []int{9, 0}, Weights: []float64{1, 1}},
	}}}})
	m.Init(sh)
	m.SetSize(sh, 80, 24)
	m.View(sh)
	if bar.height != 1 || editor.oy != 1 {
		t.Fatalf("locked row not honored: height=%d editorY=%d", bar.height, editor.oy)
	}
	for _, edge := range m.edges {
		if edge.group.id == "document" {
			t.Fatal("locked row has resize handle")
		}
	}
	old := bottom.h
	m.nudgeLayout(0, 2)
	if bottom.h == old || bar.height != 1 {
		t.Fatal("keyboard resize did not skip locked boundary and resize workspace")
	}
	m.applySplitDelta(m.layoutGroups["document"], 0, 5)
	m.relayout()
	if bar.height != 1 {
		t.Fatal("direct resize changed locked row")
	}
	m.SetSize(sh, 4, 2)
	if bar.height != 1 {
		t.Fatal("tiny terminal lost locked row")
	}
	if got := lipgloss.Height(m.View(sh)); got != 2 {
		t.Fatalf("tiny layout height = %d", got)
	}
	if m.focus != 1 {
		t.Fatal("fixed bar captured focus")
	}
}

func twoRowLayout(a, b, c, d Panel) LayoutNode {
	second := []LayoutNode{layoutLeaf(c)}
	if d != nil {
		second = append(second, layoutLeaf(d))
	}
	return LayoutNode{ID: "rows", Axis: LayoutVertical, Children: []LayoutNode{
		{ID: "first", Axis: LayoutHorizontal, Weight: 3, Children: []LayoutNode{layoutLeaf(a), layoutLeaf(b)}},
		{ID: "second", Axis: LayoutHorizontal, Children: second},
	}}
}

func TestModularLayoutSpanningLeafAndRouting(t *testing.T) {
	sh := core.NewShared(nil)
	a, b, c := &layoutProbe{}, &layoutProbe{}, &layoutProbe{}
	m := NewModularLayout(twoRowLayout(a, b, c, nil), ModularOpts{Resize: &ResizeOpts{}})
	m.Init(sh)
	m.SetSize(sh, 80, 24)
	view := m.View(sh)
	if a.w != 40 || a.h != 18 || b.w != 40 || c.w != 80 || c.h != 6 {
		t.Fatalf("sizes: a=%dx%d b=%dx%d c=%dx%d", a.w, a.h, b.w, b.h, c.w, c.h)
	}
	if lipgloss.Width(view) != 80 || lipgloss.Height(view) != 24 {
		t.Fatal("layout does not fill allocation")
	}
	if c.ox != 0 || c.oy != sh.BodyY()+18 {
		t.Fatal("spanning leaf origin incorrect")
	}
	m.Update(sh, struct{}{})
	m.Receive(sh, struct{}{})
	for _, p := range []*layoutProbe{a, b, c} {
		if p.initializations != 1 || p.updates != 1 || p.receives != 1 {
			t.Fatalf("duplicate lifecycle delivery: %+v", p)
		}
	}
	m.SetSize(sh, 80, 24)
	if a.sized != 1 || c.sized != 1 {
		t.Fatal("unchanged size recalculated tree")
	}
	m.Update(sh, tea.MouseClickMsg{X: 57, Y: sh.BodyY() + 20, Button: tea.MouseLeft})
	if !c.Focused() || a.Focused() || b.Focused() || c.mouse.X != 57 || c.mouse.Y != 2 {
		t.Fatalf("click was not routed to spanning leaf: %+v", c)
	}
	m.Update(sh, tea.MouseMotionMsg{X: 120, Y: sh.BodyY() - 2, Button: tea.MouseLeft})
	if c.mouse.X != 120 || c.mouse.Y != -20 {
		t.Fatal("out-of-bounds drag lost owner")
	}
	m.Update(sh, tea.MouseReleaseMsg{Button: tea.MouseLeft})
	if m.mouseSlot != -1 {
		t.Fatal("release retained mouse gesture")
	}
	m.FocusSlot(1)
	m.cycleFocus(1)
	if !c.Focused() {
		t.Fatal("tree traversal missed leaf")
	}
	m.cycleFocus(1)
	if !a.Focused() {
		t.Fatal("tree traversal did not wrap")
	}
}

func TestModularLayoutIndependentSiblingSplits(t *testing.T) {
	sh := core.NewShared(nil)
	a, b, c, d := &layoutProbe{}, &layoutProbe{}, &layoutProbe{}, &layoutProbe{}
	root := twoRowLayout(a, b, c, d)
	var saved ResizeState
	m := NewModularLayout(root, ModularOpts{Resize: &ResizeOpts{OnChange: func(state ResizeState) { saved = state }}})
	m.SetSize(sh, 80, 24)
	m.Update(sh, tea.MouseClickMsg{X: 40, Y: sh.BodyY() + 20, Button: tea.MouseLeft})
	m.Update(sh, tea.MouseMotionMsg{X: 50, Y: sh.BodyY() + 20, Button: tea.MouseLeft})
	m.Update(sh, tea.MouseReleaseMsg{X: 50, Y: sh.BodyY() + 20, Button: tea.MouseLeft})
	if c.w != 50 || d.w != 30 || a.w != 40 || b.w != 40 {
		t.Fatal("resizing one row changed the other row")
	}
	m.FocusSlot(3)
	m.SetResizing(true)
	m.Nudge(0, -3)
	m.SetResizing(false)
	if a.h != 15 || c.h != 9 || d.w != 30 {
		t.Fatal("nearest vertical ancestor did not resize both rows")
	}
	if m.neighbor(3, 0, -1) != 1 || m.neighbor(1, 0, 1) != 3 || m.neighbor(2, 1, 0) != 3 {
		t.Fatal("nested directional focus chose the wrong leaf")
	}
	if m.neighbor(3, 1, 0) != -1 {
		t.Fatal("directional focus should clamp")
	}
	restored := NewModularLayout(root, ModularOpts{Resize: &ResizeOpts{State: saved}})
	restored.SetSize(sh, 80, 24)
	if !reflect.DeepEqual(m.rects, restored.rects) {
		t.Fatalf("restored rectangles differ: %v vs %v", m.rects, restored.rects)
	}
	snapshot := restored.ResizeState()
	snapshot.Splits["second"].Weights[0] = 999
	if restored.ResizeState().Splits["second"].Weights[0] == 999 {
		t.Fatal("snapshot aliases live state")
	}
	m.resetResize()
	if c.w != 40 || c.h != 6 {
		t.Fatal("reset did not restore declared split weights")
	}
}

func TestModularLayoutNestedFixedAndWeightedSizing(t *testing.T) {
	sh := core.NewShared(nil)
	a, b, c, d := &layoutProbe{}, &layoutProbe{}, &layoutProbe{}, &layoutProbe{}
	root := LayoutNode{ID: "columns", Axis: LayoutHorizontal, Children: []LayoutNode{
		{ID: "stack", Axis: LayoutVertical, Size: 24, Children: []LayoutNode{layoutLeaf(a), layoutLeaf(b)}},
		layoutLeaf(c), layoutLeaf(d),
	}}
	m := NewModularLayout(root, ModularOpts{Resize: &ResizeOpts{}})
	m.SetSize(sh, 100, 30)
	m.FocusSlot(0)
	m.Nudge(5, 4)
	if a.w != 29 || a.h != 19 || b.h != 11 || c.w != 33 || d.w != 38 {
		t.Fatalf("nested resize affected wrong extents: a=%dx%d b=%dx%d c=%d d=%d", a.w, a.h, b.w, b.h, c.w, d.w)
	}
	m.Nudge(1000, 1000)
	if c.w != 8 || b.h != 3 {
		t.Fatalf("minimum sizes were not enforced: c=%d b=%d", c.w, b.h)
	}
	state := m.ResizeState()
	// Stable group IDs survive adding an ancestor and changing group position.
	outer := LayoutNode{ID: "outer", Axis: LayoutVertical, Children: []LayoutNode{root}}
	restored := NewModularLayout(outer, ModularOpts{Resize: &ResizeOpts{State: state}})
	restored.SetSize(sh, 100, 30)
	if !reflect.DeepEqual(m.rects, restored.rects) {
		t.Fatal("stable split IDs failed after structural move")
	}
}

func TestModularLayoutSmallTerminalsAndStateValidation(t *testing.T) {
	sh := core.NewShared(nil)
	for _, resize := range []*ResizeOpts{nil, {State: ResizeState{Splits: map[string]SplitState{
		"first":  {Sizes: []int{0, 0}, Weights: []float64{math.NaN(), 1}},
		"second": {Sizes: []int{0}, Weights: []float64{1}},
	}}}} {
		root := twoRowLayout(NewScrollContainer("a"), NewScrollContainer("b"), NewScrollContainer("c"), NewScrollContainer("d"))
		m := NewModularLayout(root, ModularOpts{Resize: resize})
		for h := 1; h <= 24; h++ {
			for _, w := range []int{1, 5, 20, 80} {
				m.SetSize(sh, w, h)
				v := m.View(sh)
				if lipgloss.Width(v) > w || lipgloss.Height(v) > h {
					t.Fatalf("size %dx%d overflowed: %dx%d", w, h, lipgloss.Width(v), lipgloss.Height(v))
				}
				for _, r := range m.rects {
					if r.x < 0 || r.y < 0 || r.x+r.w > w || r.y+r.h > h {
						t.Fatalf("rect outside screen: %+v", r)
					}
				}
			}
		}
		m.SetSize(sh, 80, 24)
		if m.rects[0].w != 40 || m.rects[2].w != 40 {
			t.Fatal("invalid or mismatched split state did not restore defaults")
		}
	}
}

func TestSplitLengthsConserveSpace(t *testing.T) {
	for _, sizes := range [][]int{{0, 0, 0}, {25, 0, 0}, {80, 50, 0}, {4, 4, 4}} {
		for total := 0; total < 160; total++ {
			got := splitLengths(total, sizes, []float64{3, 1, 2}, []int{8, 8, 8})
			sum := 0
			for _, v := range got {
				if v < 0 {
					t.Fatal("negative allocation")
				}
				sum += v
			}
			if sum != total {
				t.Fatalf("sizes=%v total=%d allocations=%v", sizes, total, got)
			}
			if total >= 24 {
				for _, v := range got {
					if v < 8 {
						t.Fatal("allocation below feasible minimum")
					}
				}
			}
		}
	}
}
