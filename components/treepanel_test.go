package components

import (
	"image/color"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func treeTestNodes(picked *string) []TreeNode {
	return []TreeNode{{
		ID: "class", Item: CompactItem{Name: "Class", Pick: func(*core.Shared) core.Action {
			*picked = "class"
			return core.Action{}
		}},
		Children: []TreeNode{{
			ID: "method", Item: CompactItem{Name: "method", Suffix: "func()", Pick: func(*core.Shared) core.Action {
				*picked = "method"
				return core.Action{}
			}},
			Children: []TreeNode{{ID: "local", Item: CompactItem{Name: "needle"}}},
		}},
	}, {ID: "other", Item: CompactItem{Name: "Other"}}}
}

func TestTreePanelHostKeysAndRowPosition(t *testing.T) {
	var hostKeys, itemKeys []string
	nodes := []TreeNode{{ID: "folder", Item: CompactItem{Name: "folder"}, Children: []TreeNode{
		{ID: "file", Item: CompactItem{Name: "file.md", Keys: func(_ *core.Shared, k string) (core.Action, bool) {
			itemKeys = append(itemKeys, k)
			return core.Action{}, k == "ctrl+d"
		}}},
	}}}
	p := NewTreePanel(nodes, "Docs", TreePanelOpts{Border: true,
		OnKey: func(_ *core.Shared, k string, node TreeNode) (core.Action, bool) {
			hostKeys = append(hostKeys, node.ID+":"+k)
			return core.Action{}, k == "ctrl+r"
		},
	})
	p.SetSize(30, 12)
	p.Focus()
	sh := core.NewShared(nil)
	p.UpdatePanel(sh, keyMsg("right")) // built-in navigation precedes host keys
	if node, _ := p.Selected(); node.ID != "file" || len(hostKeys) != 0 {
		t.Fatal("host intercepted built-in folding/navigation")
	}
	if row, ok := p.RowY(p.List().Index()); !ok || row != 2 {
		t.Fatalf("file row = %d, %v; want row 2 inside the frame", row, ok)
	}
	p.UpdatePanel(sh, keyMsg("ctrl+r"))
	if len(hostKeys) != 1 || hostKeys[0] != "file:ctrl+r" || len(itemKeys) != 0 {
		t.Fatal("host key did not receive the original file node or suppress fallback")
	}
	p.UpdatePanel(sh, keyMsg("ctrl+d"))
	if len(itemKeys) != 1 || itemKeys[0] != "ctrl+d" {
		t.Fatal("unhandled host key did not fall back to the item's keys")
	}
	p.UpdatePanel(sh, keyMsg("/"))
	if p.List().FilterState() != list.Filtering {
		t.Fatal("filter did not open")
	}
	hostCount, itemCount := len(hostKeys), len(itemKeys)
	p.UpdatePanel(sh, keyMsg("x"))
	if len(hostKeys) != hostCount || len(itemKeys) != itemCount {
		t.Fatal("filter input dispatched a row action")
	}
}

type treeColorItem struct {
	CompactItem
	keep bool
}

func (i treeColorItem) TitleColor() color.Color { return lipgloss.Color("11") }
func (i treeColorItem) KeepColor() bool         { return i.keep }

func TestTreePanelForwardsKeepColor(t *testing.T) {
	for _, keep := range []bool{false, true} {
		p := NewTreePanel([]TreeNode{{ID: "file", Item: treeColorItem{CompactItem: CompactItem{Name: "file.md"}, keep: keep}}}, "Docs", TreePanelOpts{})
		row := p.List().SelectedItem()
		if row.(core.KeepColorItem).KeepColor() != keep || row.(core.ColorItem).TitleColor() != lipgloss.Color("11") {
			t.Fatal("tree row lost underlying color behavior")
		}
	}
	p := NewTreePanel([]TreeNode{{ID: "plain", Item: CompactItem{Name: "plain"}}}, "Outline", TreePanelOpts{})
	if p.List().SelectedItem().(core.KeepColorItem).KeepColor() {
		t.Fatal("plain outline row opted out of the selection accent")
	}
}

func TestTreePanelRendersAndFolds(t *testing.T) {
	picked := ""
	p := NewTreePanel(treeTestNodes(&picked), "Outline", TreePanelOpts{Border: true})
	p.SetSize(30, 12)
	p.Focus()
	sh := core.NewShared(nil)

	view := ansi.Strip(p.View(true))
	if !strings.Contains(view, "▾ Class") || !strings.Contains(view, "  ▾ method") ||
		!strings.Contains(view, "    needle") {
		t.Fatalf("expanded hierarchy was not rendered:\n%s", view)
	}

	if !p.Select("method") {
		t.Fatal("could not select child")
	}
	p.UpdatePanel(sh, keyMsg("left"))
	if node, _ := p.Selected(); node.ID != "method" {
		t.Fatalf("left on an expanded branch selected %q instead of collapsing it", node.ID)
	}
	if strings.Contains(ansi.Strip(p.View(true)), "needle") {
		t.Fatal("collapsed grandchild remained visible")
	}
	p.UpdatePanel(sh, keyMsg("left"))
	if node, _ := p.Selected(); node.ID != "class" {
		t.Fatalf("left on a collapsed branch selected %q, want its parent", node.ID)
	}
	p.UpdatePanel(sh, keyMsg("right"))
	if node, _ := p.Selected(); node.ID != "method" {
		t.Fatalf("right on an expanded branch selected %q, want its first child", node.ID)
	}
	p.UpdatePanel(sh, keyMsg("enter"))
	if picked != "method" {
		t.Fatalf("enter picked %q, want the underlying method item", picked)
	}

	// The method remains folded when refreshed under the same stable IDs.
	p.SetNodes(treeTestNodes(&picked))
	if strings.Contains(ansi.Strip(p.View(true)), "needle") {
		t.Fatal("SetNodes lost the retained fold state")
	}
}

func TestTreePanelFilterSearchesCollapsedDescendants(t *testing.T) {
	picked := ""
	p := NewTreePanel(treeTestNodes(&picked), "Outline", TreePanelOpts{Border: true})
	p.SetSize(30, 12)
	p.Focus()
	sh := core.NewShared(nil)

	p.Select("method")
	p.UpdatePanel(sh, keyMsg("space"))
	if strings.Contains(ansi.Strip(p.View(true)), "needle") {
		t.Fatal("test setup did not collapse the descendant")
	}
	p.UpdatePanel(sh, keyMsg("/"))
	for _, r := range "needle" {
		p.UpdatePanel(sh, keyMsg(string(r)))
	}
	if p.List().FilterState() != list.Filtering {
		t.Fatal("tree did not enter filtering")
	}
	if view := ansi.Strip(p.View(true)); !strings.Contains(view, "needle") {
		t.Fatalf("filter could not find a collapsed descendant:\n%s", view)
	}
	p.UpdatePanel(sh, keyMsg("enter")) // apply
	p.UpdatePanel(sh, keyMsg("esc"))   // clear
	if strings.Contains(ansi.Strip(p.View(true)), "needle") {
		t.Fatal("clearing the filter did not restore the prior fold")
	}
}

func TestTreePanelSelectUsesVisibleAncestor(t *testing.T) {
	picked := ""
	p := NewTreePanel(treeTestNodes(&picked), "Outline", TreePanelOpts{})
	p.SetSize(30, 12)
	p.Focus()
	sh := core.NewShared(nil)
	p.Select("class")
	p.UpdatePanel(sh, keyMsg("space"))
	if !p.Select("local") {
		t.Fatal("hidden descendant had no selectable ancestor")
	}
	if node, _ := p.Selected(); node.ID != "class" {
		t.Fatalf("hidden descendant selected %q, want deepest visible ancestor class", node.ID)
	}
}
