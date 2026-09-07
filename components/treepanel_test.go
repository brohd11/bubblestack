package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
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
