package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
)

// barBg is the SGR prefix of a row drawn on the selection bar.
func barBg() string {
	seq, _, _ := strings.Cut(lipgloss.NewStyle().Background(core.SelectionColor).Render("x"), "x")
	return seq
}

func selectionRows() []list.Item {
	items := make([]list.Item, 5)
	for i := range items {
		items[i] = Item{Name: fmt.Sprintf("doc%d.md", i)}
	}
	return items
}

// TestListPanelHideUnfocused: HideUnfocused drops the selection while the host draws the
// panel unfocused and brings it back on focus; the zero value always shows it. The panels
// are unbordered so the only "│" is the selection border.
func TestListPanelHideUnfocused(t *testing.T) {
	always := NewCompactListPanel(selectionRows(), "", ListPanelOpts{})
	always.SetSize(30, 8)
	if !strings.Contains(always.View(false), "│") {
		t.Fatal("the zero SelectionOpts must keep the selection while unfocused")
	}

	for _, compact := range []bool{true, false} {
		var p *ListPanel
		opts := ListPanelOpts{Selection: core.SelectionOpts{HideUnfocused: true}}
		if compact {
			p = NewCompactListPanel(selectionRows(), "", opts).ListPanel
		} else {
			p = NewListPanel(selectionRows(), "", opts)
		}
		p.SetSize(30, 16)
		if v := p.View(false); strings.Contains(v, "│") {
			t.Fatalf("compact=%v: an unfocused panel should hide its selection:\n%s", compact, v)
		}
		if v := p.View(true); !strings.Contains(v, "│") {
			t.Fatalf("compact=%v: the focused panel should show its selection:\n%s", compact, v)
		}
	}
}

// TestListPanelMoveUpLeavesOneMarker: the stray "tick" seen when moving up is not in the
// panel's output — after a move up exactly one row carries the border marker, the new one.
func TestListPanelMoveUpLeavesOneMarker(t *testing.T) {
	p := NewCompactListPanel(selectionRows(), "", ListPanelOpts{})
	p.SetSize(30, 8)
	p.List().Select(3)
	p.View(true)
	p.List().CursorUp()
	var marked []string
	for _, line := range strings.Split(p.View(true), "\n") {
		if strings.Contains(line, "│") {
			marked = append(marked, line)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "doc2.md") {
		t.Fatalf("want one marker on doc2.md, got %q", marked)
	}
}

// TestPanelsForwardSelection: TreePanel and FilePanel hand Selection to their inner list,
// and FilePanel keeps it across a density rebuild.
func TestPanelsForwardSelection(t *testing.T) {
	sel := core.SelectionOpts{Style: core.SelectBackground, HideUnfocused: true}

	tree := NewTreePanel([]TreeNode{{ID: "a", Item: CompactItem{Name: "a.md"}}}, "Docs", TreePanelOpts{Selection: sel})
	tree.SetSize(30, 6)
	if !strings.Contains(tree.View(true), barBg()) {
		t.Fatal("TreePanel should draw the background selection")
	}
	if strings.Contains(tree.View(false), barBg()) {
		t.Fatal("TreePanel should hide the selection while unfocused")
	}

	root := fileTree(t)
	files := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true, Selection: sel})
	files.SetSize(30, 12)
	for _, compact := range []bool{true, false} {
		files.SetCompact(compact)
		files.SetSize(30, 12)
		if !strings.Contains(files.View(true), barBg()) {
			t.Fatalf("compact=%v: FilePanel should draw the background selection", compact)
		}
		if strings.Contains(files.View(false), barBg()) {
			t.Fatalf("compact=%v: FilePanel should hide the selection while unfocused", compact)
		}
	}
}
