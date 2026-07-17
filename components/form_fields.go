package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// FormField is one row of a FormScreen. It renders its own row (marker + label +
// content) so the form just stacks them. Key is a stable identifier used by the
// form's Value/SetValue/Focus lookups; non-focusable rows (headings/notes/spacers)
// return false from Focusable and are skipped by field navigation.
type FormField interface {
	Key() string
	Focusable() bool
	Focus() tea.Cmd
	Blur()
	// SetInnerWidth hands the field the *box's* inner text width. The field subtracts
	// its own marker and label (fieldBase.contentWidth) and sizes or folds its content
	// so that no line it renders exceeds inner. That contract is the point: the box
	// re-wraps an overrunning line at column 0, where it collides with the label column
	// and costs the form a row it never budgeted for.
	SetInnerWidth(int)
	// View renders the row. It must be pure and cheap — SetSize measures it to budget
	// the form's rows.
	View(focused bool) string
}

// Toggler is a field that responds to Left/Right while focused (a multi-option
// switch). OnToggle moves the selection forward (right) or backward (left).
type Toggler interface{ OnToggle(forward bool) }

// Activator is a field that handles Enter itself instead of submitting the form
// (e.g. the search Source row, whose Enter opens a sub-picker). It returns an Action
// and whether it consumed the Enter; when not consumed the form runs its OnSubmit.
type Activator interface {
	OnSelect(*core.Shared) (core.Action, bool)
}

// editable is the (unexported) field capability QueryUpdate needs: a focused text
// field that owns a text model and feeds it itself. TextField (textinput) and
// TextAreaField (textarea) both satisfy it — the field, not the form, knows which
// bubbles model it holds, so the form routes a keystroke without naming either type.
type editable interface{ UpdateInput(tea.Msg) tea.Cmd }

// valued is the (unexported) field capability the form's Value/SetValue look up.
// Requiring the setter too is deliberate: it keeps ToggleField (which has a Value but
// no SetValue) out, so f.Value("stage") stays "" exactly as it does today.
type valued interface {
	Value() string
	SetValue(string)
}

// Growable is a field that renders more than one row and wants a ceiling on how tall it
// may get. FormScreen.SetSize hands it the rows the body can spare, so a field that
// grows with its content can't push the form out of its box on a short terminal. Same
// optional-capability shape as Toggler/Activator: always-one-row fields just don't
// implement it.
type Growable interface{ SetMaxHeight(rows int) }

// markerWidth is fieldMarker's display width, the same in both of its states.
const markerWidth = 2

// fieldBase carries the key + label every concrete field shares.
type fieldBase struct {
	key, label string
}

func (b fieldBase) Key() string { return b.key }

// contentWidth is what's left of the box's inner width once the field has drawn its own
// marker and label — each field subtracting its own label is what keeps the form from
// needing a constant that knows the widest label in it.
//
// Floored at 1, not 0: ansi.Wrap reads a limit below 1 as "don't wrap", so a zero floor
// would quietly reinstate the overrun this arithmetic exists to prevent.
func (b fieldBase) contentWidth(inner int) int {
	if w := inner - markerWidth - lipgloss.Width(b.label); w > 1 {
		return w
	}
	return 1
}

// fieldRow lays content beside the marker+label prefix, so a content block that folds
// hangs under the content column instead of restarting at column 0. For single-line
// content this is exactly prefix+content: JoinHorizontal pads each block to its own
// widest line, which for a one-line block is that line itself, so a one-row field renders
// byte-for-byte as it did when this was a concatenation.
func fieldRow(focused bool, label, content string) string {
	prefix := fieldMarker(focused) + fieldLabel().Render(label)
	return lipgloss.JoinHorizontal(lipgloss.Top, prefix, content)
}

// fieldLabel is the muted style for a field's label. Built per call (not cached in
// a package var) so it tracks the active theme after a core.SetTheme switch.
func fieldLabel() lipgloss.Style { return lipgloss.NewStyle().Foreground(core.MutedColor) }

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

// SetInnerWidth resizes the scrolling window textinput renders through.
//
// The SetCursor is not a no-op. textinput recomputes that window (its unexported
// handleOverflow) only when the value or cursor moves — never when Width changes — and
// while Width is 0 the window it computed is the *whole* value. A field seeded with
// SetValue before the first resize has exactly that, so without re-running it here the
// row renders the entire value and overruns the box until the next keystroke; a resize
// narrower leaves the same staleness. SetCursor is the exported door to that recompute,
// and re-seating the cursor where it already is moves nothing.
func (t *TextField) SetInnerWidth(inner int) {
	t.input.Width = t.contentWidth(inner)
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

// growRows is how far a TextAreaField may grow before it scrolls instead. Six rows is a
// comfortable commit message; past that the field crowds the box even on a tall
// terminal, and SetSize's bodyHeight clamp cuts it down further on a short one.
const growRows = 6

// TextAreaField is a free-text row that wraps and grows downward instead of scrolling
// sideways — the shape a commit message wants, where TextField would slide the start of
// the line out of view. Otherwise a drop-in for TextField: same constructor signature,
// same Value/SetValue, same marker+label row.
//
// The value is held to a single logical line (see UpdateInput and SetValue). That's what
// makes the height math exact: textarea's LineInfo().Height reports the wrapped height of
// the *cursor's* logical line, which is the whole block only while there's just one.
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
	// textarea's defaults dress the widget up: a background-highlighted cursor line when
	// focused, greyed text when blurred. TextField has neither, so strip both style sets
	// back to plain and keep only the placeholder grey (textinput's own default).
	plain := textarea.Style{Placeholder: lipgloss.NewStyle().Foreground(lipgloss.Color("240"))}
	ta.FocusedStyle, ta.BlurredStyle = plain, plain
	// New aims the live style pointer at a *local* copy of the default blurred style
	// rather than at the BlurredStyle field, so the two assignments above stay invisible
	// until a Focus/Blur repoints it. Blur now so the first frame is already plain.
	ta.Blur()
	// New already called SetWidth — but with its own "┃ " prompt still in place, leaving
	// the inner width 2 short. Redo it now that Prompt and ShowLineNumbers are ours; the
	// form's SetSize supplies the real width on the first resize.
	ta.SetWidth(40)
	// The height is pinned at the cap and never tracks the content: growing it after an
	// Update would leave the viewport scrolled to a taller layout's cursor row
	// (repositionView runs inside Update, against the old height) and the first row of the
	// message would vanish. View slices the render down to the rows actually needed.
	ta.SetHeight(growRows)
	return &TextAreaField{fieldBase: fieldBase{key, label}, input: ta, maxRows: growRows}
}

func (t *TextAreaField) Focusable() bool   { return true }
func (t *TextAreaField) Focus() tea.Cmd    { return t.input.Focus() }
func (t *TextAreaField) Blur()             { t.input.Blur() }
func (t *TextAreaField) Value() string     { return t.input.Value() }
func (t *TextAreaField) SetValue(v string) { t.input.SetValue(oneLine(v)) }

// SetInnerWidth sets the textarea's wrap width from what the row has left after the
// marker and label. With no prompt and no line numbers the textarea's inner width is
// exactly what it's given. (textarea.Model has its own SetWidth; this is the FormField
// hook, which means something different — hence the name.)
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

// UpdateInput feeds the keystroke to the textarea, first collapsing any newline to a
// space. textarea's sanitizer keeps newlines where textinput's replaces them, and its
// sanitizer is unexported, so the message is the only place to do this. A second logical
// line would break View's height math and smuggle a multi-line message past a form whose
// Enter submits. A bracketed paste is the only way one can arrive: the form intercepts
// Keys.Select before the fall-through, so KeyEnter never reaches the textarea, and
// QueryUpdate never diverts it.
func (t *TextAreaField) UpdateInput(msg tea.Msg) tea.Cmd {
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyRunes {
		if s := string(km.Runes); strings.ContainsAny(s, "\r\n") {
			km.Runes = []rune(oneLine(s)) // a fresh slice; km.Runes is shared with the sender
			msg = km
		}
	}
	var cmd tea.Cmd
	t.input, cmd = t.input.Update(msg)
	return cmd
}

// oneLine collapses CR/LF to spaces, matching what textinput's sanitizer does for free.
var newlineRepl = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")

func oneLine(s string) string { return newlineRepl.Replace(s) }

// View renders the row as the label prefix beside the textarea block, so wrapped rows
// hang under the text instead of restarting at column 0. The textarea always renders
// exactly maxRows rows (padding the tail with end-of-buffer blanks), so slice it back to
// the rows the value occupies — that, rather than a SetHeight, is what grows the row.
func (t *TextAreaField) View(focused bool) string {
	rows := min(max(t.input.LineInfo().Height, 1), t.maxRows)
	lines := strings.Split(t.input.View(), "\n")
	if len(lines) > rows {
		lines = lines[:rows]
	}
	return fieldRow(focused, t.label, strings.Join(lines, "\n"))
}

// ---------- ToggleField ----------

// ToggleField is a multi-option switch (e.g. Project/Global). OnToggle cycles the
// index, so it works for any number of options; delim controls how RenderToggle
// joins them (empty → the ◄ ► arrows).
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

// RenderToggle renders a multi-option switch on one line, with the active option
// highlighted and the options joined by delim (empty → the "◄ ►" arrows). Pure rendering —
// the cycling lives in the caller (ToggleField.OnToggle), so it works for any option
// count. Reused by the New Plugin confirm screens, which embed it in a dialog body that
// does its own layout; width 0 is what keeps it on one line for them.
func RenderToggle(options []string, index int, delim string) string {
	return packToggle(options, index, delim, 0)
}

// packToggle renders the toggle folded to width, breaking only *between* options — never
// inside one, which is what a general-purpose wrap would do to them: ansi.Wrap and
// ansi.Wordwrap both treat "-" as an unconditional breakpoint, so "tracked changes (-a)"
// would come apart as "tracked changes (-" / "a)". A folded line keeps the delimiter
// trailing so the row reads as continuing.
//
// width <= 0 means don't fold — RenderToggle's contract, and the state of a field the
// form hasn't sized yet.
func packToggle(options []string, index int, delim string, width int) string {
	sep := "  ◄ ►  "
	if delim != "" {
		sep = " " + delim + " "
	}
	sepW := lipgloss.Width(sep)
	trail := strings.TrimRight(sep, " ")

	// Style at emit time, from the raw option, because both the packing and the fold below
	// have to measure and cut raw text: lipgloss re-opens the style on each line it
	// renders, so folding first keeps every row colored, where folding an already-rendered
	// run would leave one SGR open across the break and bleed it into the padding
	// JoinHorizontal writes alongside.
	render := func(i int) string {
		s := options[i]
		// An option wider than the whole content column can't be packed anywhere, so fold
		// it rather than let it overrun. The hyphen split is ugly, but it isn't
		// destructive: fieldRow still hangs the remainder under the content column, where
		// an overrun would instead collide with the label.
		if width > 0 && lipgloss.Width(s) > width {
			s = ansi.Wrap(s, width, "")
		}
		if i == index {
			return lipgloss.NewStyle().Foreground(core.FocusedColor).Bold(true).Render(s)
		}
		return lipgloss.NewStyle().Foreground(core.MutedColor).Render(s)
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
		// Raw width, deliberately: an over-wide option measures past width no matter what,
		// so it never packs beside a neighbour and its folded block lands as its own entry
		// in lines — which the final Join flattens correctly.
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

// ---------- PickField ----------

// PickField is a focusable row whose Enter runs a custom action (an Activator) — used
// for the search Source row, whose value is chosen in a pushed sub-picker. value
// supplies the current display text; onSel runs on Enter.
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

// View folds the value to the content column. Unlike a toggle's options the value is one
// atom with nothing to pack between, so a plain wrap is all there is; it's unstyled, so
// ansi.Wrap's unconditional hyphen breakpoint costs nothing here. Before the first resize
// width is 0 and ansi.Wrap passes the value through, which is what it did before.
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

// View folds the text itself rather than leaving it to the box. The box would fold it to
// the same place — a static row starts where a re-wrap would restart it, so this was never
// visibly broken — but folding here is what makes the rendered height honest, and SetSize
// budgets the form's rows off that height.
//
// Wrap raw, then Render: lipgloss re-opens the style per line, where folding an already
// rendered run would leave an SGR open across the break.
func (s *StaticField) View(bool) string { return s.style.Render(ansi.Wrap(s.text, s.width, "")) }
