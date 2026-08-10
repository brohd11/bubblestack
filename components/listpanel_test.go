package components

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type compactTestItem struct{ title, suffix string }

func (i compactTestItem) Title() string       { return i.title }
func (i compactTestItem) SuffixText() string  { return i.suffix }
func (i compactTestItem) FilterValue() string { return i.title }

// TestListPanelBorder: with ListPanelOpts.Border the title moves out of the list's
// own title bar and into the frame's legend, and the list is sized to the inner run
// so the framed panel still fills its allocation exactly.
func TestListPanelBorder(t *testing.T) {
	items := []list.Item{Item{Name: "one"}, Item{Name: "two"}}
	p := NewListPanel(items, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 10)

	if p.List().ShowTitle() {
		t.Fatal("a bordered panel should hide the list's own title bar")
	}
	v := p.View(false)
	if !strings.HasPrefix(v, "┌─ Docs ") {
		t.Fatalf("bordered View should open with the title legend, got %q", strings.SplitN(v, "\n", 2)[0])
	}
	if w := lipgloss.Width(v); w != 30 {
		t.Fatalf("bordered View width = %d, want the allocated 30", w)
	}
	if h := lipgloss.Height(v); h != 10 {
		t.Fatalf("bordered View height = %d, want the allocated 10", h)
	}
	if !strings.Contains(v, "one") {
		t.Fatal("the rows should still render inside the frame")
	}
}

// TestListPanelBorderlessByDefault is the no-regression guard for existing sidebars
// (gitstack's tags panel): without the opt-in the panel renders the bare list, title
// bar and all.
func TestListPanelBorderlessByDefault(t *testing.T) {
	p := NewListPanel([]list.Item{Item{Name: "one"}}, "Tags", ListPanelOpts{})
	p.SetSize(30, 10)

	if !p.List().ShowTitle() {
		t.Fatal("an unbordered panel keeps the list's own title bar")
	}
	v := p.View(false)
	if strings.Contains(v, "┌") {
		t.Fatal("an unbordered panel must draw no frame")
	}
	if !strings.Contains(v, "Tags") {
		t.Fatal("the title should still show in the list's title bar")
	}
	if v != p.List().View() {
		t.Fatal("an unbordered panel should render the bare list")
	}
}

// TestListPanelFocusTint: the View arg selects the frame's color. The tint itself
// can't be asserted headless (lipgloss strips color without a TTY — see
// TestFrameColorTracksFocus), so this pins the geometry that must not change with it.
func TestListPanelFocusTint(t *testing.T) {
	p := NewListPanel([]list.Item{Item{Name: "one"}}, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 10)
	focused, blurred := p.View(true), p.View(false)
	if lipgloss.Width(focused) != lipgloss.Width(blurred) ||
		lipgloss.Height(focused) != lipgloss.Height(blurred) {
		t.Fatal("focus must not change the framed panel's footprint")
	}
	if !strings.HasPrefix(focused, "┌─ Docs ") {
		t.Fatal("the legend should survive the focused tint")
	}
}

func TestCompactListPanelRowsAndPagination(t *testing.T) {
	items := make([]list.Item, 10)
	for i := range items {
		items[i] = compactTestItem{title: fmt.Sprintf("doc%d.md", i), suffix: "notes/"}
	}
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{Border: true})
	p.SetSize(30, 8)

	if got := p.List().Paginator.PerPage; got != 3 {
		t.Fatalf("compact per-page count = %d, want 3", got)
	}
	if lines := strings.Split(p.View(false), "\n"); len(lines) != 8 {
		t.Fatalf("compact bordered panel height = %d, want 8", len(lines))
	}
	if v := p.View(false); !strings.Contains(v, "doc0.md  notes/") {
		t.Fatalf("compact row should place suffix on the title line, got:\n%s", v)
	}
}

func TestBorderedListPanelMouseRows(t *testing.T) {
	sh := core.NewShared(nil)
	click := func(y int) tea.MouseMsg {
		return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 5, Y: y}
	}

	t.Run("compact", func(t *testing.T) {
		items := []list.Item{
			compactTestItem{title: "zero"},
			compactTestItem{title: "one"},
			compactTestItem{title: "two"},
			compactTestItem{title: "three"},
		}
		picked := ""
		p := NewCompactListPanel(items, "Docs", ListPanelOpts{
			Border: true,
			OnSelect: func(_ *core.Shared, item list.Item) core.Action {
				picked = item.(compactTestItem).title
				return core.Action{}
			},
		})
		p.SetSize(30, 8)
		p.Focus()

		for _, y := range []int{0, 1} { // frame, then the list's filter/title row
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != "" {
				t.Fatalf("panel row %d should not pick an item, picked %q", y, picked)
			}
		}
		for y, want := range map[int]string{2: "zero", 3: "one", 4: "two"} {
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != want {
				t.Fatalf("panel row %d picked %q, want %q", y, picked, want)
			}
		}

		// Select moves the paginator to page two. Its first item occupies the
		// same rendered row as page one's first item.
		p.List().Select(p.List().Paginator.PerPage)
		picked = ""
		p.UpdatePanel(sh, click(2))
		if picked != "three" {
			t.Fatalf("page-two first row picked %q, want three", picked)
		}
	})

	t.Run("standard", func(t *testing.T) {
		items := []list.Item{Item{Name: "zero"}, Item{Name: "one"}}
		picked := ""
		p := NewListPanel(items, "Docs", ListPanelOpts{
			Border: true,
			OnSelect: func(_ *core.Shared, item list.Item) core.Action {
				picked = item.(Item).Name
				return core.Action{}
			},
		})
		p.SetSize(30, 12)
		p.Focus()
		for y, want := range map[int]string{2: "zero", 5: "one"} {
			picked = ""
			p.UpdatePanel(sh, click(y))
			if picked != want {
				t.Fatalf("panel row %d picked %q, want %q", y, picked, want)
			}
		}
	})
}
