package components

import (
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// stubScreen is a minimal core.Screen recording its Init count, size, and render
// text, so ScreenPanel tests can see which child is live.
type stubScreen struct {
	name    string
	inits   int
	w, h    int
	initCmd tea.Cmd
}

func (s *stubScreen) Init(*core.Shared) tea.Cmd { s.inits++; return s.initCmd }
func (s *stubScreen) Update(*core.Shared, tea.Msg) (core.Screen, core.Action) {
	return s, core.Action{}
}
func (s *stubScreen) View(*core.Shared) string     { return s.name }
func (s *stubScreen) HelpView(*core.Shared) string { return "" }
func (s *stubScreen) SetSize(_ *core.Shared, w, h int) {
	s.w, s.h = w, h
}

// TestScreenPanelSetChild covers the swap in both orders: before the panel's Init
// (silent — the host's Init starts the new child, the cmd is nil) and after (the
// new child is sized and initialized immediately, its Init cmd handed back).
func TestScreenPanelSetChild(t *testing.T) {
	sh := core.NewShared(nil)

	t.Run("before init", func(t *testing.T) {
		first := &stubScreen{name: "first"}
		second := &stubScreen{name: "second"}
		p := NewScreenPanel(first)
		if cmd := p.SetChild(second); cmd != nil {
			t.Fatal("SetChild before Init should return a nil cmd")
		}
		if second.inits != 0 {
			t.Fatal("SetChild before Init must not init the new child — the host will")
		}
		p.Init(sh)
		if first.inits != 0 || second.inits != 1 {
			t.Fatalf("host Init should start the swapped child only (first %d, second %d)", first.inits, second.inits)
		}
	})

	t.Run("after init", func(t *testing.T) {
		first := &stubScreen{name: "first"}
		second := &stubScreen{name: "second", initCmd: func() tea.Msg { return nil }}
		p := NewScreenPanel(first)
		p.SetSize(40, 10)
		p.Init(sh)
		if first.w != 40 || first.h != 10 {
			t.Fatalf("Init should apply the stashed size, got %dx%d", first.w, first.h)
		}
		cmd := p.SetChild(second)
		if cmd == nil {
			t.Fatal("SetChild should hand back the new child's Init cmd")
		}
		if second.inits != 1 || second.w != 40 || second.h != 10 {
			t.Fatalf("the new child should be initialized and sized (inits %d, %dx%d)", second.inits, second.w, second.h)
		}
		if got := p.View(true); got != "second" {
			t.Fatalf("View should render the swapped child, got %q", got)
		}
	})
}
