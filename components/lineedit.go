package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LineEditScreen is a floating single-line text edit: an overlay (core.Overlayer)
// the router composites over the screen below it at a caller-supplied anchor
// (core.OverlayPositioner) instead of centered, so it can sit on top of the very
// element it edits — a list row naming a new file, say. The rest of the frame is
// the untouched background screen, and only this screen receives input while it is
// on top, so the edit is modal for free (the popup precedent).
//
// The screen is deliberately position-agnostic: it knows nothing about lists or
// any other host. The caller computes the anchor — the box's top-left cell in
// absolute terminal coordinates — and the width of the element being covered, and
// hands both to NewLineEdit (ListItemRow does the list math). The input line
// renders one row below the anchor (the box's top border row), so to align the
// input with a covered row, anchor one row above it. The router clamps the box
// into the visible frame, so an anchor near an edge is safe.
//
// Enter runs OnDone with the value, esc runs OnCancel; either nil callback is a
// plain Pop. Like DialogScreen's OnYes, a callback that keeps the flow going must
// do its own navigation (usually core.Pop). Every other key feeds the textinput.
type LineEditScreen struct {
	input textinput.Model
	x, y  int // box top-left anchor, absolute terminal cells
	width int // total box width — the width of the element being covered
	termW int // terminal width, from SetSize, for clamping

	OnDone   func(*core.Shared, string) core.Action // enter; nil ⇒ plain Pop
	OnCancel func(*core.Shared) core.Action          // esc; nil ⇒ plain Pop
	Help     []key.Binding // rendered inside the box; nil ⇒ default enter/esc hints, empty ⇒ none
	Crumb    string        // breadcrumb segment; default "edit"
}

var _ core.Overlayer = (*LineEditScreen)(nil)
var _ core.OverlayPositioner = (*LineEditScreen)(nil)
var _ core.Filterer = (*LineEditScreen)(nil)
var _ core.Crumber = (*LineEditScreen)(nil)

// defaultLineEditHelp is the hint row rendered inside the box; ad-hoc bindings
// rather than core.Hint over Keys.Yes/No, whose typable letters (y/e/n/c) must
// stay text here.
var defaultLineEditHelp = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// NewLineEdit builds a floating line edit anchored at (x, y) — the box's top-left
// corner in absolute terminal cells — covering width cells. placeholder shows in
// the empty input.
func NewLineEdit(placeholder string, x, y, width int, onDone func(*core.Shared, string) core.Action, onCancel func(*core.Shared) core.Action) *LineEditScreen {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	return &LineEditScreen{
		input:    ti,
		x:        x,
		y:        y,
		width:    width,
		OnDone:   onDone,
		OnCancel: onCancel,
	}
}

func (s *LineEditScreen) Init(*core.Shared) tea.Cmd { return textinput.Blink }

// IsOverlay marks the screen for compositing over the screen below it.
func (s *LineEditScreen) IsOverlay() bool { return true }

// OverlayPos pins the box to the caller's anchor; the router clamps it on screen.
func (s *LineEditScreen) OverlayPos(int, int) (int, int) { return s.x, s.y }

// Filtering always reports text capture: every printable key is input, so the
// router's global single-key shortcuts (q/O/t/…) must not fire over this screen.
func (s *LineEditScreen) Filtering() bool { return true }

// CrumbLabel contributes the breadcrumb segment (Crumb, default "edit").
func (s *LineEditScreen) CrumbLabel(short bool) string {
	return crumbSeg(short, "", s.Crumb, "edit")
}

func (s *LineEditScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if km, ok := msg.(tea.KeyMsg); ok {
		// enter/esc match as raw keycodes on purpose: the central Yes/No
		// bindings carry typable letters, which must stay text here.
		switch km.String() {
		case "enter":
			if s.OnDone != nil {
				return s, s.OnDone(sh, s.input.Value())
			}
			return s, core.Pop()
		case "esc":
			if s.OnCancel != nil {
				return s, s.OnCancel(sh)
			}
			return s, core.Pop()
		}
	}
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, core.Async(cmd)
}

func (s *LineEditScreen) View(sh *core.Shared) string {
	w := s.width
	if s.termW > 0 && w > s.termW {
		w = s.termW
	}
	// Border and padding take 4 cells off the covered width; the rest splits
	// between the prompt and the text window. lipgloss's Width counts padding
	// but not the border, so the style width is the covered width minus the
	// border — the rendered box comes out exactly w cells wide.
	promptW := lipgloss.Width(s.input.Prompt)
	inner := w - 4 - promptW
	if inner < 1 {
		inner = 1
	}
	if s.input.Width != inner {
		s.input.Width = inner
		// textinput only reflows its scroll window on value/cursor movement, so
		// re-seat the cursor to force the recompute (see TextField.SetInnerWidth).
		s.input.SetCursor(s.input.Position())
	}
	body := s.input.View()
	help := s.Help
	if help == nil {
		help = defaultLineEditHelp
	}
	if len(help) > 0 {
		// BindingHelp renders for the chrome help bar and leads with a blank
		// row; the slim box wants just the entries line.
		if hint := strings.TrimLeft(sh.BindingHelp(help), " \n"); hint != "" {
			body = body + "\n" + hint
		}
	}
	return lineEditBox().Width(inner + promptW + 2).Render(body)
}

// HelpView is empty: the hints render inside the box (the popup precedent — the
// background screen's help bar stays visible in the chrome).
func (s *LineEditScreen) HelpView(*core.Shared) string { return "" }

func (s *LineEditScreen) SetSize(_ *core.Shared, width, _ int) { s.termW = width }

// lineEditBox is the slim variant of core's popup box: one input row tall, so it
// hugs the covered element instead of reading as a dialog. Built per call (like
// popupStyle) so it tracks the active theme.
func lineEditBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(core.FocusedColor)
}
