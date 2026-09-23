package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// FormField is one row of a FormScreen, rendering its own marker, label and content. Key
// identifies it for Value/SetValue/Focus; non-focusable rows (headings, notes) are
// skipped by navigation.
type FormField interface {
	Key() string
	Focusable() bool
	Focus() tea.Cmd
	Blur()
	// SetInnerWidth gives the field the box's inner width. The field subtracts its own marker
	// and label and keeps every line within it: the box would re-wrap an overlong line at
	// column 0, into the label column.
	SetInnerWidth(int)
	// View renders the row. It must be pure and cheap — SetSize measures it to budget
	// the form's rows.
	View(focused bool) string
}

// Toggler is a field that responds to Left/Right while focused (a multi-option
// switch). OnToggle moves the selection forward (right) or backward (left).
type Toggler interface{ OnToggle(forward bool) }

// Activator is a field that handles Enter itself (reporting whether it did) instead of
// submitting the form.
type Activator interface {
	OnSelect(*core.Shared) (core.Action, bool)
}

// editable is a focused text field that feeds keystrokes to its own bubbles model.
type editable interface{ UpdateInput(tea.Msg) tea.Cmd }

// valued is the capability Value/SetValue look up. Requiring the setter keeps ToggleField
// out.
type valued interface {
	Value() string
	SetValue(string)
}

// Growable is a multi-row field that takes a height ceiling from FormScreen.SetSize, so
// it cannot push the form out of its box.
type Growable interface{ SetMaxHeight(rows int) }

// markerWidth is fieldMarker's display width, the same in both of its states.
const markerWidth = 2

// fieldBase carries the key + label every concrete field shares.
type fieldBase struct {
	key, label string
}

func (b fieldBase) Key() string { return b.key }

// labelWidth is the label column's width, for anchoring a popup under the value.
func (b fieldBase) labelWidth() int { return lipgloss.Width(b.label) }

// contentWidth is the box's inner width minus this field's marker and label, floored at 1
// (ansi.Wrap treats a smaller limit as "don't wrap").
func (b fieldBase) contentWidth(inner int) int {
	if w := inner - markerWidth - lipgloss.Width(b.label); w > 1 {
		return w
	}
	return 1
}

// fieldRow lays content beside the marker and label, so folded content hangs under the
// content column. Single-line content renders exactly as prefix+content.
func fieldRow(focused bool, label, content string) string {
	prefix := fieldMarker(focused) + fieldLabel().Render(label)
	return lipgloss.JoinHorizontal(lipgloss.Top, prefix, content)
}

// fieldLabel is the muted style for a field's label. Built per call (not cached in
// a package var) so it tracks the active theme after a core.SetTheme switch.
func fieldLabel() lipgloss.Style { return core.MutedStyle() }

// fieldMarker is the focus arrow rendered to the left of a focusable row.
func fieldMarker(focused bool) string {
	if focused {
		return lipgloss.NewStyle().Foreground(core.FocusedColor).Render("▸ ")
	}
	return "  "
}

// ---------- TextField ----------

// TextField is a free-text row backed by a textinput.Model. It satisfies editable,
// so the form routes typed characters here via QueryUpdate.
type TextField struct {
	fieldBase
	input textinput.Model
}

func NewTextField(key, label, placeholder string) *TextField {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = "" // the label is rendered separately
	return &TextField{fieldBase: fieldBase{key, label}, input: ti}
}

func (t *TextField) Focusable() bool   { return true }
func (t *TextField) Focus() tea.Cmd    { return t.input.Focus() }
func (t *TextField) Blur()             { t.input.Blur() }
func (t *TextField) Value() string     { return t.input.Value() }
func (t *TextField) SetValue(v string) { t.input.SetValue(v) }

// SetInnerWidth resizes the textinput window. SetCursor re-seats the cursor to force
// textinput to recompute its visible window, which it otherwise does only on edits: a
// value set before the first resize would render whole and overrun the box.
func (t *TextField) SetInnerWidth(inner int) {
	t.input.SetWidth(t.contentWidth(inner))
	t.input.SetCursor(t.input.Position())
}

func (t *TextField) UpdateInput(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return cmd
}

func (t *TextField) View(focused bool) string {
	return fieldRow(focused, t.label, t.input.View())
}

// ---------- TextAreaField ----------

// growRows is how tall a TextAreaField grows before it scrolls.
const growRows = 6

// TextAreaField is a text row that wraps and grows downward (a commit message), otherwise
// like TextField. The value is kept to one logical line, which keeps the height math
// exact: LineInfo().Height covers only the cursor's logical line.
type TextAreaField struct {
	fieldBase
	input   textarea.Model
	maxRows int
}

func NewTextAreaField(key, label, placeholder string) *TextAreaField {
	ta := textarea.New()
	ta.Placeholder = placeholder
	ta.Prompt = ""             // the label is rendered separately, as in TextField
	ta.ShowLineNumbers = false // a one-line value has nothing to number
	// Strip textarea's cursor-line and blurred styling to match TextField; keep the
	// placeholder grey.
	plain := textarea.StyleState{Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("240"))}
	st := ta.Styles()
	st.Focused, st.Blurred = plain, plain
	ta.SetStyles(st)
	// Blur now so the first frame uses the plain styles (New points at a private copy).
	ta.Blur()
	// Re-apply the width now that the prompt and line numbers are ours; SetSize sets the real
	// one.
	ta.SetWidth(40)
	// The height stays pinned at the cap: resizing after an Update would leave the viewport
	// scrolled for the old height and hide the first row. View slices the render instead.
	ta.SetHeight(growRows)
	return &TextAreaField{fieldBase: fieldBase{key, label}, input: ta, maxRows: growRows}
}

func (t *TextAreaField) Focusable() bool   { return true }
func (t *TextAreaField) Focus() tea.Cmd    { return t.input.Focus() }
func (t *TextAreaField) Blur()             { t.input.Blur() }
func (t *TextAreaField) Value() string     { return t.input.Value() }
func (t *TextAreaField) SetValue(v string) { t.input.SetValue(oneLine(v)) }

// SetInnerWidth sets the wrap width to what the row leaves after marker and label.
func (t *TextAreaField) SetInnerWidth(inner int) { t.input.SetWidth(t.contentWidth(inner)) }

// SetMaxHeight (Growable) takes the form's offer of body rows and caps it at growRows.
func (t *TextAreaField) SetMaxHeight(rows int) {
	rows = min(max(rows, 1), growRows)
	if rows == t.maxRows {
		return
	}
	t.maxRows = rows
	t.input.SetHeight(rows)
}

// UpdateInput collapses newlines to spaces before feeding the textarea, keeping the value
// one logical line. Only a bracketed paste can carry one (the form takes Enter).
func (t *TextAreaField) UpdateInput(msg tea.Msg) tea.Cmd {
	if pm, ok := msg.(tea.PasteMsg); ok && strings.ContainsAny(pm.Content, "\r\n") {
		msg = tea.PasteMsg{Content: oneLine(pm.Content)}
	}
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return cmd
}

// oneLine collapses CR/LF to spaces, matching what textinput's sanitizer does for free.
var newlineRepl = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

func oneLine(s string) string { return newlineRepl.Replace(s) }

// View renders the label prefix beside the textarea, sliced to the rows the value uses
// (the textarea always renders maxRows).
func (t *TextAreaField) View(focused bool) string {
	rows := min(max(t.input.LineInfo().Height, 1), t.maxRows)
	lines := strings.Split(t.input.View(), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return fieldRow(focused, t.label, strings.Join(lines, "\n"))
}

// ---------- ToggleField ----------

// ToggleField is a multi-option switch; OnToggle cycles the index. delim joins the options
// (empty: ◄ ►).
type ToggleField struct {
	fieldBase
	options []string
	index   int
	delim   string
	width   int
}

// NewToggleField builds a multi-option switch. delim is optional: omit it for the
// default ◄ ► arrows, or pass one (e.g. "|") to join the options differently.
func NewToggleField(key, label string, options []string, delim ...string) *ToggleField {
	t := &ToggleField{fieldBase: fieldBase{key, label}, options: options}
	if len(delim) > 0 {
		t.delim = delim[0]
	}
	return t
}

func (t *ToggleField) Focusable() bool         { return true }
func (t *ToggleField) Focus() tea.Cmd          { return nil }
func (t *ToggleField) Blur()                   {}
func (t *ToggleField) SetInnerWidth(inner int) { t.width = t.contentWidth(inner) }
func (t *ToggleField) Index() int              { return t.index }
func (t *ToggleField) Value() string           { return t.options[t.index] }

// SetIndex pre-selects an option (e.g. to seed a toggle from detected state). Out-of-
// range values are ignored, so it's safe to call before options are known to match.
func (t *ToggleField) SetIndex(i int) {
	if i >= 0 && i < len(t.options) {
		t.index = i
	}
}

func (t *ToggleField) OnToggle(forward bool) {
	n := len(t.options)
	if forward {
		t.index = (t.index + 1) % n
	} else {
		t.index = (t.index - 1 + n) % n
	}
}

func (t *ToggleField) View(focused bool) string {
	return fieldRow(focused, t.label, packToggle(t.options, t.index, t.delim, t.width))
}

// RenderToggle renders a multi-option switch on one line, the active option highlighted
// and the options joined by delim (empty: ◄ ►). Rendering only; the caller cycles.
func RenderToggle(options []string, index int, delim string) string {
	return packToggle(options, index, delim, 0)
}

// packToggle folds the toggle to width, breaking only between options (ansi.Wrap would
// split "(-a)" at the hyphen). Folded lines keep the trailing delimiter. width <= 0 does
// not fold.
func packToggle(options []string, index int, delim string, width int) string {
	sep := "  ◄ ►  "
	if delim != "" {
		sep = " " + delim + " "
	}
	sepW := lipgloss.Width(sep)
	trail := strings.TrimRight(sep, " ")

	// Style each option after packing and folding the raw text, so every folded row is
	// colored and no SGR stays open across a break.
	render := func(i int) string {
		s := options[i]
		// An option wider than the column is folded rather than left to overrun into the label.
		if width > 0 && lipgloss.Width(s) > width {
			s = ansi.Wrap(s, width, "")
		}
		if i == index {
			return core.AccentStyle().Render(s)
		}
		return core.MutedStyle().Render(s)
	}

	if width <= 0 {
		parts := make([]string, len(options))
		for i := range options {
			parts[i] = render(i)
		}
		return strings.Join(parts, sep)
	}

	var lines []string
	line, lineW := "", 0
	for i := range options {
		// Raw width on purpose: an over-wide option never packs beside a neighbour.
		w := lipgloss.Width(options[i])
		switch {
		case i == 0:
			line, lineW = render(i), w
		case lineW+sepW+w <= width:
			line, lineW = line+sep+render(i), lineW+sepW+w
		default:
			if lineW+lipgloss.Width(trail) <= width {
				line += trail // the continuation marker, only when it fits
			}
			lines, line, lineW = append(lines, line), render(i), w
		}
	}
	if len(options) > 0 {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// ---------- CheckField ----------

// checkWidth is the display width of the "[X] " box, the same in both of its states.
const checkWidth = 4

// CheckField is a boolean checkbox row ("[X] Include dirs"). It is a Toggler (space and ◄ ►
// flip it) but not an Activator, so Enter still submits; read it with Checked().
type CheckField struct {
	fieldBase
	checked bool
	width   int
}

// NewCheckField builds a checkbox row seeded with checked.
func NewCheckField(key, label string, checked bool) *CheckField {
	return &CheckField{fieldBase: fieldBase{key, label}, checked: checked}
}

func (c *CheckField) Focusable() bool   { return true }
func (c *CheckField) Focus() tea.Cmd    { return nil }
func (c *CheckField) Blur()             {}
func (c *CheckField) Checked() bool     { return c.checked }
func (c *CheckField) SetChecked(v bool) { c.checked = v }

// OnToggle flips the box. The direction is ignored: with two states, forward and backward
// land in the same place.
func (c *CheckField) OnToggle(bool) { c.checked = !c.checked }

// SetInnerWidth subtracts the marker and box (the label is the content here), floored at
// 1 like contentWidth.
func (c *CheckField) SetInnerWidth(inner int) {
	if w := inner - markerWidth - checkWidth; w > 1 {
		c.width = w
		return
	}
	c.width = 1
}

// View renders "▸ [X] Label", the box in the prefix and the label as content, so a long
// label folds under itself. Checked uses the focused color, unchecked muted; styles are
// built per call to track the theme.
func (c *CheckField) View(focused bool) string {
	box, style := "[ ] ", core.MutedStyle()
	if c.checked {
		box, style = "[X] ", core.AccentStyle()
	}
	prefix := fieldMarker(focused) + style.Render(box)
	return lipgloss.JoinHorizontal(lipgloss.Top, prefix, style.Render(ansi.Wrap(c.label, c.width, "")))
}

// ---------- PickField ----------

// PickField is a focusable row whose Enter runs onSel (an Activator), such as a Source row
// choosing from a dropdown anchored by FieldAnchor. value supplies the display text.
type PickField struct {
	fieldBase
	value func() string
	onSel func(*core.Shared) (core.Action, bool)
	width int
}

func NewPickField(key, label string, value func() string, onSel func(*core.Shared) (core.Action, bool)) *PickField {
	return &PickField{fieldBase: fieldBase{key, label}, value: value, onSel: onSel}
}

func (p *PickField) Focusable() bool                              { return true }
func (p *PickField) Focus() tea.Cmd                               { return nil }
func (p *PickField) Blur()                                        {}
func (p *PickField) SetInnerWidth(inner int)                      { p.width = p.contentWidth(inner) }
func (p *PickField) OnSelect(sh *core.Shared) (core.Action, bool) { return p.onSel(sh) }

// View wraps the value to the content column (width 0 before the first resize passes it
// through).
func (p *PickField) View(focused bool) string {
	return fieldRow(focused, p.label, ansi.Wrap(p.value(), p.width, ""))
}

// ---------- StaticField ----------

// StaticField is a non-focusable display row: a heading, a muted note, or a blank
// spacer. Field navigation skips it.
type StaticField struct {
	text  string
	style lipgloss.Style
	width int
}

func NewHeading(text string) *StaticField { return &StaticField{text: text} }
func NewNote(text string) *StaticField    { return &StaticField{text: text, style: fieldLabel()} }
func NewSpacer() *StaticField             { return &StaticField{} }

func (s *StaticField) Key() string     { return "" }
func (s *StaticField) Focusable() bool { return false }
func (s *StaticField) Focus() tea.Cmd  { return nil }
func (s *StaticField) Blur()           {}

// SetInnerWidth takes the box's inner width whole: a static row draws no marker and no
// label, so it starts at column 0 and has the full width to itself.
func (s *StaticField) SetInnerWidth(inner int) { s.width = inner }

// View folds the text itself so the rendered height is honest for SetSize's budgeting.
// Wrap raw, then render, so no SGR spans a break.
func (s *StaticField) View(bool) string { return s.style.Render(ansi.Wrap(s.text, s.width, "")) }
