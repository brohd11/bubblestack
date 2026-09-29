package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// LineEditScreen is a floating single-line edit composited at a caller-supplied anchor
// (core.OverlayPositioner), so it can sit over the element it edits, such as a list row
// naming a new file. It is modal because only the top screen gets input.
//
// The caller computes the anchor (the box's top-left in absolute cells) and the covered
// width; ListItemRow does the list math. The input renders one row below the anchor (the
// top border), so anchor one row above a covered row. The router clamps the box into the
// frame.
//
// Enter runs OnDone and esc OnCancel (nil means a plain Pop); a callback that continues
// the flow does its own navigation. OnChange sees each value change. It adds no breadcrumb
// segment: a popup is not a place.
type LineEditScreen struct {
	input textinput.Model
	x, y  int // box top-left anchor, absolute terminal cells
	width int // total box width — the width of the element being covered
	termW int // terminal width, from SetSize, for clamping

	OnDone   func(*core.Shared, string) core.Action // enter; nil ⇒ plain Pop
	OnCancel func(*core.Shared) core.Action         // esc; nil ⇒ plain Pop
	OnChange func(*core.Shared, string) core.Action // value changed; nil ⇒ nothing
	Help     []key.Binding                          // rendered inside the box; nil/empty ⇒ none (NewLineEdit's help seeds the enter/esc hints)
}

var _ core.Overlayer = (*LineEditScreen)(nil)
var _ core.OverlayPositioner = (*LineEditScreen)(nil)
var _ core.Filterer = (*LineEditScreen)(nil)

// defaultLineEditHelp is the hint row inside the box. Ad-hoc bindings, because the
// typable letters in Keys.Yes/No must stay text here.
var defaultLineEditHelp = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "done")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

// NewLineEdit builds a line edit with its box's top-left at absolute cell (x, y),
// covering width cells. help adds the enter/esc hint row inside the box; without it the
// box is a single input row.
func NewLineEdit(placeholder string, x, y, width int, help bool, onDone func(*core.Shared, string) core.Action, onCancel func(*core.Shared) core.Action) *LineEditScreen {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	s := &LineEditScreen{
		input:    ti,
		x:        x,
		y:        y,
		width:    width,
		OnDone:   onDone,
		OnCancel: onCancel,
	}
	if help {
		s.Help = defaultLineEditHelp
	}
	return s
}

func (s *LineEditScreen) Init(*core.Shared) tea.Cmd {
	if s.input.Styles().Cursor.Blink {
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

// Value is the current text — the read half of SetValue, for a caller holding the
// screen while it is up.
func (s *LineEditScreen) Value() string { return s.input.Value() }

// Anchor returns the constructor's geometry, for a caller checking where its box landed.
func (s *LineEditScreen) Anchor() (x, y, width int) { return s.x, s.y, s.width }

// SetPrompt replaces textinput's default "> " prefix.
func (s *LineEditScreen) SetPrompt(prompt string) { s.input.Prompt = prompt }

// SetCursorBlink(false) keeps the caret visible without blinking, for small transient
// overlays.
func (s *LineEditScreen) SetCursorBlink(blink bool) {
	st := s.input.Styles()
	st.Cursor.Blink = blink
	s.input.SetStyles(st)
}

// CursorBlink reports the input cursor's blink state — the read half of SetCursorBlink.
func (s *LineEditScreen) CursorBlink() bool { return s.input.Styles().Cursor.Blink }

// IsOverlay marks the screen for compositing over the screen below it.
func (s *LineEditScreen) IsOverlay() bool { return true }

// OverlayPos pins the box to the caller's anchor; the router clamps it on screen.
func (s *LineEditScreen) OverlayPos(int, int) (int, int) { return s.x, s.y }

// Filtering always reports text capture: every printable key is input, so the
// router's global single-key shortcuts (q/O/t/…) must not fire over this screen.
func (s *LineEditScreen) Filtering() bool { return true }

func (s *LineEditScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if km, ok := msg.(tea.KeyPressMsg); ok {
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
	// Border and padding take 4 cells; the rest goes to the prompt, the text window and one
	// cell for a caret past the end. Without that cell, a full value overflows by one only
	// while the caret blinks on, and the box would gain and lose a row each blink.
	promptW := lipgloss.Width(s.input.Prompt)
	contentW := max(w-4, 0) // a bordered, padded box cannot render under 4 cells
	inner := max(contentW-promptW-1, 1)
	if s.input.Width() != inner {
		s.input.SetWidth(inner)
		// textinput only reflows its scroll window on value/cursor movement, so
		// re-seat the cursor to force the recompute (see TextField.SetInnerWidth).
		s.input.SetCursor(s.input.Position())
	}
	// Cell-truncate so a narrow pane cannot wrap the input: the box must stay one row.
	body := ansi.Truncate(s.input.View(), contentW, "…")
	if len(s.Help) > 0 {
		// BindingHelp renders for the chrome help bar and leads with a blank
		// row; the slim box wants just the entries line.
		if hint := strings.TrimLeft(sh.BindingHelp(s.Help), " \n"); hint != "" {
			body = body + "\n" + hint
		}
	}
	return LineEditBox().Width(contentW + 4).Render(body)
}

// HelpView is empty: the hints render inside the box (the popup precedent — the
// background screen's help bar stays visible in the chrome).
func (s *LineEditScreen) HelpView(*core.Shared) string { return "" }

func (s *LineEditScreen) SetSize(_ *core.Shared, width, _ int) { s.termW = width }

// LineEditBox is the slim popup box, one input row tall, built per call to track the
// theme.
func LineEditBox() lipgloss.Style {
	return lipgloss.NewStyle().
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(core.FocusedColor)
}
