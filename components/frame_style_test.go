package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTitledFrameRender(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame TitledFrame
		want  string
	}{
		{"open top and bottom", TitledFrame{}, "│ Docs │\n├──────┤\n│ab    │"},
		{"box top", TitledFrame{Top: TopBox}, "┌──────┐\n│ Docs │\n├──────┤\n│ab    │"},
		{"tee top, closed", TitledFrame{Top: TopTee, Bottom: true}, "├──────┤\n│ Docs │\n├──────┤\n│ab    │\n└──────┘"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ansi.Strip(tc.frame.Render("Docs", "ab", 6, false))
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
			in := tc.frame.Insets()
			if h := lipgloss.Height(got); h != in.Top+1+in.Bottom {
				t.Fatalf("height %d disagrees with insets %+v", h, in)
			}
		})
	}
	// An over-long legend is clipped to the run rather than pushing the side out.
	row := strings.Split(ansi.Strip(TitledFrame{}.Render("Documents", "ab", 6, false)), "\n")[0]
	if row != "│ Docu…│" {
		t.Fatalf("clipped legend row = %q", row)
	}
}

// TestBoxFrameMatchesBorder: Border is shorthand for BoxFrame, byte for byte.
func TestBoxFrameMatchesBorder(t *testing.T) {
	items := []list.Item{Item{Name: "one"}, Item{Name: "two"}}
	a := NewListPanel(items, "Docs", ListPanelOpts{Border: true})
	b := NewListPanel(items, "Docs", ListPanelOpts{Frame: BoxFrame{}})
	a.SetSize(30, 10)
	b.SetSize(30, 10)
	if a.View(true) != b.View(true) {
		t.Fatal("Frame: BoxFrame{} renders differently from Border: true")
	}
}

// TestTitledFrameListGeometry: the panel sizes, places and hit-tests its rows from the
// frame's insets, so a taller top still fills the allocation and clicks land on rows.
func TestTitledFrameListGeometry(t *testing.T) {
	sh := core.NewShared(nil)
	items := []list.Item{
		compactTestItem{title: "zero"}, compactTestItem{title: "one"}, compactTestItem{title: "two"},
	}
	picked := ""
	p := NewCompactListPanel(items, "Docs", ListPanelOpts{
		Frame: TitledFrame{Top: TopTee, Bottom: true},
		OnSelect: func(_ *core.Shared, it list.Item) core.Action {
			picked = it.(compactTestItem).title
			return core.Action{}
		},
	})
	p.SetSize(20, 9)
	p.Focus()
	v := p.View(true)
	if w, h := lipgloss.Width(v), lipgloss.Height(v); w != 20 || h != 9 {
		t.Fatalf("View is %dx%d, want the allocated 20x9", w, h)
	}
	if row, _ := p.RowY(0); row != 3 {
		t.Fatalf("RowY(0) = %d, want 3 (tee, legend, rule)", row)
	}
	if line := ansi.Strip(strings.Split(v, "\n")[3]); !strings.Contains(line, "zero") {
		t.Fatalf("row 3 = %q, want the first item", line)
	}
	for y := 0; y < 3; y++ {
		picked = ""
		p.UpdatePanel(sh, tea.MouseClickMsg{X: 5, Y: y, Button: tea.MouseLeft})
		if picked != "" {
			t.Fatalf("frame row %d picked %q", y, picked)
		}
	}
	p.UpdatePanel(sh, tea.MouseClickMsg{X: 5, Y: 4, Button: tea.MouseLeft})
	if picked != "one" {
		t.Fatalf("row 4 picked %q, want one", picked)
	}

	// Swapping the frame re-fits: dropping the top edge and bottom gives the list two rows.
	before := p.List().Height()
	p.SetFrame(TitledFrame{})
	if got := p.List().Height(); got != before+2 {
		t.Fatalf("list height %d after SetFrame, want %d", got, before+2)
	}
	if h := lipgloss.Height(p.View(true)); h != 9 {
		t.Fatalf("height %d after SetFrame, want 9", h)
	}
}

// TestFrameFocusModes: FocusEdges tints the whole frame, as the box always has; under
// FocusLegend the lines keep the border color and only the legend text reacts.
func TestFrameFocusModes(t *testing.T) {
	rows := func(f FrameStyle, focused bool) []string {
		return strings.Split(f.Render("Docs", "ab", 6, focused), "\n")
	}
	for _, f := range []FrameStyle{TitledFrame{Focus: FocusLegend, Bottom: true}, BoxFrame{Focus: FocusLegend}} {
		on, off := rows(f, true), rows(f, false)
		const legendRow = 0 // the box's top edge; the titled frame, with no top, opens on it
		if !strings.Contains(on[legendRow], core.AccentStyle().Render("Docs")) ||
			!strings.Contains(off[legendRow], core.MutedStyle().Render("Docs")) {
			t.Fatalf("%T: the legend should be muted, and accented while focused:\n%q\n%q", f, off[legendRow], on[legendRow])
		}
		for i := range on {
			if i != legendRow && on[i] != off[i] {
				t.Fatalf("%T: edge row %d changed with focus:\n%q\n%q", f, i, on[i], off[i])
			}
		}
	}
	for _, f := range []FrameStyle{TitledFrame{Bottom: true}, BoxFrame{}} {
		on, off := rows(f, true), rows(f, false)
		if last := len(on) - 1; on[last] == off[last] {
			t.Fatalf("%T: under FocusEdges the bottom edge should take the accent", f)
		}
	}
}
