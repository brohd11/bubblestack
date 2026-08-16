package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
// do its own navigation (usually core.Pop). Every other key feeds the textinput;
// OnChange, when set, receives each resulting value change. SetCursorBlink(false)
// keeps the caret visible and static for overlays that should not flash.
//
// The box leaves the chrome of the screen it covers alone — no Crumber, so the
// breadcrumb keeps reading as the background screen while the edit is up. A modal
// popup is a thing on top of a place, not a place of its own.
type LineEditScreen struct {
	input textinput.Model
	x, y  int // box top-left anchor, absolute terminal cells
	width int // total box width — the width of the element being covered
	termW int // terminal width, from SetSize, for clamping

	OnDone   func(*core.Shared, string) core.Action // enter; nil ⇒ plain Pop
	OnCancel func(*core.Shared) core.Action         // esc; nil ⇒ plain Pop
	OnChange func(*core.Shared, string) core.Action // value changed; nil ⇒ nothing
	Help     []key.Binding                          // rendered inside the box; nil ⇒ default enter/esc hints, empty ⇒ none
}

var _ core.Overlayer = (*LineEditScreen)(nil)
var _ core.OverlayPositioner = (*LineEditScreen)(nil)
var _ core.Filterer = (*LineEditScreen)(nil)

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

func (s *LineEditScreen) Init(*core.Shared) tea.Cmd {
	if s.input.Cursor.Mode() == cursor.CursorBlink {
		return textinput.Blink
	}
	return nil
}

// SetValue seeds the input (the cursor lands at the end), for an edit that starts
// from an existing value — a save-as prefilled with the current file name, say.
func (s *LineEditScreen) SetValue(v string) {
	s.input.SetValue(v)
	s.input.SetCursor(len([]rune(v)))
}

// SetPrompt replaces textinput's default "> " prefix.
func (s *LineEditScreen) SetPrompt(prompt string) { s.input.Prompt = prompt }

// SetCursorBlink controls whether the input caret flashes. Static mode keeps a
// visible caret without scheduling blink messages, which is useful for small
// transient overlays where motion reads as noise.
func (s *LineEditScreen) SetCursorBlink(blink bool) {
	mode := cursor.CursorStatic
	if blink {
		mode = cursor.CursorBlink
	}
	s.input.Cursor.SetMode(mode)
}

// IsOverlay marks the screen for compositing over the screen below it.
func (s *LineEditScreen) IsOverlay() bool { return true }

// OverlayPos pins the box to the caller's anchor; the router clamps it on screen.
func (s *LineEditScreen) OverlayPos(int, int) (int, int) { return s.x, s.y }

// Filtering always reports text capture: every printable key is input, so the
// router's global single-key shortcuts (q/O/t/…) must not fire over this screen.
func (s *LineEditScreen) Filtering() bool { return true }

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
	before := s.input.Value()
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	act := core.Async(cmd)
	if s.OnChange != nil && s.input.Value() != before {
		changed := s.OnChange(sh, s.input.Value())
		act.Msg = changed.Msg
		if changed.Cmd != nil {
			act.Cmd = tea.Batch(cmd, changed.Cmd)
		}
	}
	return s, act
}

func (s *LineEditScreen) View(sh *core.Shared) string {
	w := s.width
	if s.termW > 0 && w > s.termW {
		w = s.termW
	}
	// Border and padding take 4 cells off the covered width; the rest splits
	// between the prompt, the text window and one cell held back for the caret.
	// lipgloss's Width counts padding but not the border, so the style width is
	// the covered width minus the border — the rendered box comes out exactly w
	// cells wide.
	//
	// The held-back cell is not cosmetic: textinput renders promptW + Width + 1
	// cells whenever the caret sits past the last character, the trailing cell
	// being the caret itself. Budget only promptW + Width and a value that fills
	// the window puts the line one cell over the wrap limit — and it wraps only
	// on the blink-on phase, since cellbuf.Wrap reads the caret's reversed space
	// as a word but drops a plain one at end of line, so the box would gain and
	// lose a row on every blink.
	promptW := lipgloss.Width(s.input.Prompt)
	contentW := max(w-4, 0) // a bordered, padded box cannot render under 4 cells
	inner := contentW - promptW - 1
	if inner < 1 {
		inner = 1
	}
	if s.input.Width != inner {
		s.input.Width = inner
		// textinput only reflows its scroll window on value/cursor movement, so
		// re-seat the cursor to force the recompute (see TextField.SetInnerWidth).
		s.input.SetCursor(s.input.Position())
	}
	// Cell-truncate rather than let a stray cell wrap (searchBar's precedent): on a
	// pane too narrow for prompt + caret, inner hits its floor and the input line
	// can still outrun the content width. A one-row box must stay one row.
	body := ansi.Truncate(s.input.View(), contentW, "…")
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
	return lineEditBox().Width(contentW + 2).Render(body)
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
