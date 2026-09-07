package components

import (
	"fmt"
	"image/color"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// TreeNode is one stable item in a TreePanel. ID uniquely identifies the logical node across
// SetNodes refreshes, which lets the panel retain folds while its backing data changes.
// A new node starts expanded unless Collapsed asks otherwise.
type TreeNode struct {
	ID        string
	Item      core.SuffixItem
	Children  []TreeNode
	Collapsed bool
}

// TreePanelOpts contains the host behavior that is not intrinsic to a tree. OnSelect
// receives the original node rather than the private rendered row. With no hook, a
// components.CompactItem dispatches its own Pick closure as it does in ListPanel.
type TreePanelOpts struct {
	OnSelect func(*core.Shared, TreeNode) core.Action
	Help     []key.Binding
	Border   bool
}

// TreePanel is a compact, filterable tree in a ModularScreen pane. Branch state belongs
// to the panel; filtering temporarily exposes every node so collapsed descendants remain
// searchable, then restores the visible fold shape when the filter is cleared.
type TreePanel struct {
	panel       *CompactListPanel
	opts        TreePanelOpts
	nodes       []TreeNode
	collapsed   map[string]bool
	initialized map[string]bool
	parents     map[string]string
}

var _ Panel = (*TreePanel)(nil)
var _ Focusable = (*TreePanel)(nil)
var _ PanelUpdater = (*TreePanel)(nil)
var _ Capturing = (*TreePanel)(nil)
var _ PanelHelper = (*TreePanel)(nil)
var _ panelInitializer = (*TreePanel)(nil)
var _ FocusNotifier = (*TreePanel)(nil)

// NewTreePanel builds an expanded-by-default tree. Nodes with the same stable ID retain
// their fold state when SetNodes replaces the data.
func NewTreePanel(nodes []TreeNode, title string, opts TreePanelOpts) *TreePanel {
	p := &TreePanel{
		opts: opts, collapsed: make(map[string]bool), initialized: make(map[string]bool),
		parents: make(map[string]string),
	}
	p.panel = NewCompactListPanel(nil, title, ListPanelOpts{
		Border: opts.Border,
		Help: append([]key.Binding{
			core.Hint("fold", core.Keys.Left, core.Keys.Right),
		}, opts.Help...),
		OnSelect: p.selectRow,
		OnKey:    p.keyRow,
	})
	p.SetNodes(nodes)
	return p
}

// treeRow is the list-facing projection. PrefixText keeps hierarchy chrome out of the
// filter value and pinned while a long selected title marquees.
type treeRow struct {
	id, parent string
	node       TreeNode
	depth      int
	branch     bool
	collapsed  bool
}

func (r treeRow) Title() string       { return r.node.Item.Title() }
func (r treeRow) SuffixText() string  { return r.node.Item.SuffixText() }
func (r treeRow) FilterValue() string { return r.node.Item.FilterValue() }
func (r treeRow) PrefixText() string {
	marker := "  "
	if r.branch {
		marker = "▾ "
		if r.collapsed {
			marker = "▸ "
		}
	}
	prefix := fmt.Sprintf("%*s%s", r.depth*2, "", marker)
	if item, ok := r.node.Item.(core.PrefixItem); ok {
		prefix += item.PrefixText()
	}
	return prefix
}
func (r treeRow) Mark() string {
	if item, ok := r.node.Item.(core.MarkItem); ok {
		return item.Mark()
	}
	return ""
}
func (r treeRow) TitleColor() color.Color {
	if item, ok := r.node.Item.(core.ColorItem); ok {
		return item.TitleColor()
	}
	return nil
}

// SetNodes replaces the data while retaining folds for IDs seen before. Selection follows
// the same ID when possible and otherwise lands on its deepest visible ancestor.
func (p *TreePanel) SetNodes(nodes []TreeNode) {
	selected := p.selectedID()
	p.nodes = append(p.nodes[:0], nodes...)
	p.parents = make(map[string]string)
	p.indexNodes(p.nodes, "", "")
	p.rebuild(p.filtering(), selected)
}

func (p *TreePanel) indexNodes(nodes []TreeNode, parent, path string) {
	for i := range nodes {
		id := nodes[i].ID
		if id == "" {
			id = fmt.Sprintf("%s/%d", path, i)
			nodes[i].ID = id
		}
		p.parents[id] = parent
		if !p.initialized[id] {
			p.initialized[id] = true
			p.collapsed[id] = nodes[i].Collapsed
		}
		p.indexNodes(nodes[i].Children, id, id)
	}
}

func (p *TreePanel) rows(all bool) []list.Item {
	var rows []list.Item
	var walk func([]TreeNode, string, int)
	walk = func(nodes []TreeNode, parent string, depth int) {
		for _, node := range nodes {
			collapsed := p.collapsed[node.ID]
			rows = append(rows, treeRow{
				id: node.ID, parent: parent, node: node, depth: depth,
				branch: len(node.Children) > 0, collapsed: collapsed,
			})
			if all || !collapsed {
				walk(node.Children, node.ID, depth+1)
			}
		}
	}
	walk(p.nodes, "", 0)
	return rows
}

func (p *TreePanel) rebuild(all bool, selected string) {
	p.panel.SetItems(p.rows(all))
	if selected != "" {
		p.Select(selected)
	}
}

func (p *TreePanel) filtering() bool { return p.panel.List().FilterState() != list.Unfiltered }

func (p *TreePanel) selectedRow() (treeRow, bool) {
	row, ok := p.panel.List().SelectedItem().(treeRow)
	return row, ok
}

func (p *TreePanel) selectedID() string {
	row, ok := p.selectedRow()
	if !ok {
		return ""
	}
	return row.id
}

func (p *TreePanel) selectRow(sh *core.Shared, item list.Item) core.Action {
	row, ok := item.(treeRow)
	if !ok {
		return core.Action{}
	}
	if p.opts.OnSelect != nil {
		return p.opts.OnSelect(sh, row.node)
	}
	if pick := itemPick(row.node.Item); pick != nil {
		return pick(sh)
	}
	return core.Action{}
}

func (p *TreePanel) keyRow(sh *core.Shared, k string, item list.Item) (core.Action, bool) {
	row, ok := item.(treeRow)
	if !ok {
		return core.Action{}, false
	}
	if !p.filtering() {
		switch {
		case core.MatchKey(k, core.Keys.Toggle):
			if row.branch {
				p.collapsed[row.id] = !p.collapsed[row.id]
				p.rebuild(false, row.id)
			}
			return core.Action{}, true
		case core.MatchKey(k, core.Keys.Right):
			if !row.branch {
				return core.Action{}, true
			}
			if row.collapsed {
				p.collapsed[row.id] = false
				p.rebuild(false, row.id)
			} else if len(row.node.Children) > 0 {
				p.Select(row.node.Children[0].ID)
			}
			return core.Action{}, true
		case core.MatchKey(k, core.Keys.Left):
			if row.branch && !row.collapsed {
				p.collapsed[row.id] = true
				p.rebuild(false, row.id)
			} else if row.parent != "" {
				p.Select(row.parent)
			}
			return core.Action{}, true
		}
	}
	if keys := itemKeys(row.node.Item); keys != nil {
		return keys(sh, k)
	}
	return core.Action{}, false
}

// Select moves to id without opening its ancestors. If it is hidden by a fold, the
// deepest visible ancestor is selected instead. It returns false only when neither the
// ID nor one of its ancestors is in the current view.
func (p *TreePanel) Select(id string) bool {
	for id != "" {
		for i, item := range p.panel.List().VisibleItems() {
			if row, ok := item.(treeRow); ok && row.id == id {
				p.panel.List().Select(i)
				return true
			}
		}
		id = p.parents[id]
	}
	return false
}

// Selected returns the original node under the cursor.
func (p *TreePanel) Selected() (TreeNode, bool) {
	row, ok := p.selectedRow()
	return row.node, ok
}

// UpdatePanel adds fold handling and swaps between the folded and all-node row sets as
// the embedded list enters or leaves filtering.
func (p *TreePanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	wasFiltering := p.filtering()
	selected := p.selectedID()
	act, handled := p.panel.UpdatePanel(sh, msg)
	isFiltering := p.filtering()
	if wasFiltering != isFiltering {
		p.rebuild(isFiltering, selected)
	}
	return act, handled
}

func (p *TreePanel) SetSize(width, height int)    { p.panel.SetSize(width, height) }
func (p *TreePanel) View(focused bool) string     { return p.panel.View(focused) }
func (p *TreePanel) Focus()                       { p.panel.Focus() }
func (p *TreePanel) Blur()                        { p.panel.Blur() }
func (p *TreePanel) Focused() bool                { return p.panel.Focused() }
func (p *TreePanel) Capturing() bool              { return p.panel.Capturing() }
func (p *TreePanel) PanelHelp() []key.Binding     { return p.panel.PanelHelp() }
func (p *TreePanel) Init(sh *core.Shared) tea.Cmd { return p.panel.Init(sh) }
func (p *TreePanel) OnFocus() tea.Cmd             { return p.panel.OnFocus() }
func (p *TreePanel) List() *list.Model            { return p.panel.List() }
