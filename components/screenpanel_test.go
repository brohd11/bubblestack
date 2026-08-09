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

// paneStub is a child that answers both embed-time capabilities, recording what it
// was told and the dims it was sized with — the shape of an embedded EditorScreen.
// It starts focused, as a real screen does (standalone always is).
type paneStub struct {
	stubScreen
	embedded bool
	focused  bool
}

func newPaneStub() *paneStub { return &paneStub{focused: true} }

func (s *paneStub) SetEmbedded(on bool) { s.embedded = on }
func (s *paneStub) SetFocused(on bool)  { s.focused = on }

// TestScreenPanelSyncsChild: embedding hands the child the two facts only the panel
// knows — that it is a pane (core.Embeddable, which fixes its mouse geometry) and
// whether this pane holds focus — from Init and from a later SetChild alike, and
// always before the child is sized. A child that implements neither is left alone.
func TestScreenPanelSyncsChild(t *testing.T) {
	sh := core.NewShared(nil)

	t.Run("from init", func(t *testing.T) {
		child := newPaneStub()
		p := NewScreenPanel(child)
		if child.embedded {
			t.Fatal("wrapping alone must not embed the child")
		}
		p.SetSize(40, 10)
		p.Init(sh)
		if !child.embedded {
			t.Fatal("Init should tell an Embeddable child it is a pane")
		}
		if child.w != 40 || child.h != 10 {
			t.Fatalf("child sized %dx%d, want the outer 40x10", child.w, child.h)
		}
	})

	// An unfocused pane must not leave its child rendering as focused: nothing else
	// ever blurs it, since ModularScreen focuses one slot at construction and never
	// touches the rest.
	t.Run("unfocused pane blurs its child", func(t *testing.T) {
		child := newPaneStub()
		p := NewScreenPanel(child)
		p.Init(sh)
		if child.focused {
			t.Fatal("a child of an unfocused pane should render unfocused")
		}
	})

	// The mirror case, and the one gote hits: picking a second doc swaps a child into
	// a pane that ALREADY holds focus, so FocusSlot is a no-op and the swap is the
	// only chance to tell the new child it is live.
	t.Run("focused pane lights its new child", func(t *testing.T) {
		p := NewScreenPanel(&stubScreen{name: "plain"}) // implements neither: untouched
		p.SetSize(40, 10)
		p.Init(sh)
		p.Focus()

		child := newPaneStub()
		child.focused = false
		p.SetChild(child)
		if !child.embedded || !child.focused {
			t.Fatalf("SetChild should embed (%v) and focus (%v) the new child", child.embedded, child.focused)
		}
	})

	t.Run("before init", func(t *testing.T) {
		child := newPaneStub()
		p := NewScreenPanel(&stubScreen{name: "plain"})
		p.SetChild(child) // swapped in before the panel is initialized
		if !child.embedded {
			t.Fatal("a child swapped in before Init should still be embedded")
		}
	})
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

// swapStub is a child whose Update swaps the panel's child mid-flight — the gote
// editor pane's shape: its OnExit hook calls SetChild while the router is still
// inside the child's Update.
type swapStub struct {
	stubScreen
	panel *ScreenPanel
	repl  core.Screen
}

func (s *swapStub) Update(*core.Shared, tea.Msg) (core.Screen, core.Action) {
	s.panel.SetChild(s.repl)
	return s, core.Action{}
}

// TestScreenPanelSwapSurvivesUpdate: a child whose update swaps the pane's child
// (an OnExit hook closing the pane's doc and switching to the next one) must not
// have the swap clobbered by UpdatePanel's returned-screen bookkeeping — before the
// fix, the pane kept rendering the closed doc's editor, title and all.
func TestScreenPanelSwapSurvivesUpdate(t *testing.T) {
	sh := core.NewShared(nil)
	p := NewScreenPanel(&stubScreen{name: "original"})
	p.SetSize(40, 10)
	p.Init(sh)
	swap := &swapStub{panel: p, repl: &stubScreen{name: "replacement"}}
	swap.name = "swapper"
	p.SetChild(swap)

	p.UpdatePanel(sh, tea.KeyMsg{Type: tea.KeyCtrlX})
	if got := p.View(false); got != "replacement" {
		t.Fatalf("the mid-update swap should survive, pane renders %q", got)
	}
}

// TestScreenPanelForwardsPaneOrigin: the host-pushed origin reaches a PaneOriginer
// child — immediately, and for a child swapped in after the first push.
func TestScreenPanelForwardsPaneOrigin(t *testing.T) {
	sh := core.NewShared(nil)
	child := &originStubScreen{}
	p := NewScreenPanel(child)
	p.Init(sh)
	p.SetPaneOrigin(31, 4)
	if !child.has || child.x != 31 || child.y != 4 {
		t.Fatalf("child got (%d,%d) has=%v, want (31,4)", child.x, child.y, child.has)
	}

	later := &originStubScreen{}
	p.SetChild(later)
	if !later.has || later.x != 31 || later.y != 4 {
		t.Fatalf("a swapped-in child should inherit the origin, got (%d,%d) has=%v", later.x, later.y, later.has)
	}
}

// originStubScreen records the pushed pane origin (EditorScreen's shape).
type originStubScreen struct {
	stubScreen
	x, y int
	has  bool
}

func (s *originStubScreen) SetPaneOrigin(x, y int) { s.x, s.y, s.has = x, y, true }
