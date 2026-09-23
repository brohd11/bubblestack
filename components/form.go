package components

import (
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// FormScreen is a column of self-rendering fields with one focused. It owns the generic
// key handling (focus cycling, the QueryUpdate typing split, Back/Left/Right/Select) and
// the titled box; each field carries its own behavior, and the caller supplies the fields
// and OnSubmit. Field types and optional capabilities are in form_fields.go.

type FormOpts struct {
	Title      string // optional in-body title bar (core.WithTitle); omitted ⇒ no bar
	Crumb      string // breadcrumb segment (CrumbLabel); omitted ⇒ contributes none
	CrumbShort string // optional short breadcrumb-bar segment; defaults to Crumb
	Fields     []FormField
	Help       []key.Binding
	Focus      string // initial focused field key; default first focusable
	OnSubmit   func(*core.Shared, *FormScreen) core.Action
	OnCancel   func(*core.Shared) core.Action // Back handler; defaults to a plain Pop
	// OnKey claims extra keys before the form's own handling ((act, true) to take one). Only
	// claim non-text keys, so editing still works.
	OnKey func(*core.Shared, string) (core.Action, bool)
	// Overlay draws the form as a centered modal over the screen below it. Width is
	// the popup's inner content width; zero uses a compact terminal-relative default.
	Overlay bool
	Width   int
}

type FormScreen struct {
	title       string
	crumb       string
	crumbShort  string
	fields      []FormField
	help        []key.Binding
	focus       int
	focused     bool // panel focus when nested (FocusableScreen); a standalone form is always focused
	onSubmit    func(*core.Shared, *FormScreen) core.Action
	onCancel    func(*core.Shared) core.Action
	onKey       func(*core.Shared, string) (core.Action, bool)
	overlay     bool
	width       int
	renderWidth int
}

var _ core.Screen = (*FormScreen)(nil)
var _ core.Filterer = (*FormScreen)(nil)
var _ core.Crumber = (*FormScreen)(nil)
var _ core.Overlayer = (*FormScreen)(nil)
var _ Typable = (*FormScreen)(nil)
var _ FocusableScreen = (*FormScreen)(nil)

// CrumbLabel contributes the form's breadcrumb segment, defaulting to "Form" when no
// Crumb is declared.
func (f *FormScreen) CrumbLabel(short bool) string {
	if f.overlay {
		return ""
	}
	return CrumbSegment(short, f.crumbShort, f.crumb, "Form")
}

// IsOverlay reports whether the router should composite this form over the screen
// below it. A non-overlay form keeps its existing full-body behavior.
func (f *FormScreen) IsOverlay() bool { return f.overlay }

func NewForm(opts FormOpts) *FormScreen {
	f := &FormScreen{title: opts.Title, crumb: opts.Crumb, crumbShort: opts.CrumbShort, fields: opts.Fields, help: opts.Help, focused: true, onSubmit: opts.OnSubmit, onCancel: opts.OnCancel, onKey: opts.OnKey}
	f.overlay, f.width = opts.Overlay, opts.Width
	f.focus = f.firstFocusable()
	if opts.Focus != "" {
		for i, fld := range f.fields {
			if fld.Key() == opts.Focus && fld.Focusable() {
				f.focus = i
				break
			}
		}
	}
	return f
}

func (f *FormScreen) firstFocusable() int {
	for i, fld := range f.fields {
		if fld.Focusable() {
			return i
		}
	}
	return 0
}

func (f *FormScreen) current() FormField { return f.fields[f.focus] }

// editable returns the focused field's text capability, or a true nil (the comma-ok form
// avoids returning a typed nil).
func (f *FormScreen) editable() editable {
	e, _ := f.current().(editable)
	return e
}

// Typable: a free-text field has focus iff the current field owns a text model.
func (f *FormScreen) Typing() bool { return f.editable() != nil }

func (f *FormScreen) UpdateInput(msg tea.Msg) tea.Cmd {
	if e := f.editable(); e != nil {
		return e.UpdateInput(msg)
	}
	return nil
}

func (f *FormScreen) Filtering() bool           { return true }
func (f *FormScreen) Init(*core.Shared) tea.Cmd { return f.syncFocus() }

func (f *FormScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	if cmd, ok := QueryUpdate(f, msg); ok {
		return f, core.Async(cmd)
	}
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return f, core.Action{}
	}
	k := key.String()
	if f.onKey != nil {
		if act, handled := f.onKey(sh, k); handled {
			return f, act
		}
	}
	switch {
	case core.MatchKey(k, core.Keys.Back):
		if f.onCancel != nil {
			return f, f.onCancel(sh)
		}
		return f, core.Pop()
	case core.MatchKey(k, core.Keys.PrevField):
		f.move(-1)
		return f, core.Async(f.syncFocus())
	case core.MatchKey(k, core.Keys.NextField):
		f.move(1)
		return f, core.Async(f.syncFocus())
	case core.MatchKey(k, core.Keys.Left), core.MatchKey(k, core.Keys.Right):
		// On a Toggler row these cycle the option; on a text row they fall through
		// to the input (cursor movement / literal characters).
		if t, ok := f.current().(Toggler); ok {
			t.OnToggle(core.MatchKey(k, core.Keys.Right))
			return f, core.Action{}
		}
	case core.MatchKey(k, core.Keys.Toggle):
		// Space flips a Toggler in place (steps a multi-option toggle forward). A focused text
		// field never gets here: QueryUpdate takes the space first.
		if t, ok := f.current().(Toggler); ok {
			t.OnToggle(true)
			return f, core.Action{}
		}
	case core.MatchKey(k, core.Keys.Select):
		if a, ok := f.current().(Activator); ok {
			if act, handled := a.OnSelect(sh); handled {
				return f, act
			}
		}
		if f.onSubmit != nil {
			return f, f.onSubmit(sh, f)
		}
		return f, core.Action{}
	}
	// Editing keys (backspace, cursor) fall through to the focused text field.
	if e := f.editable(); e != nil {
		return f, core.Async(e.UpdateInput(msg))
	}
	return f, core.Action{}
}

// move shifts focus by delta, skipping non-focusable fields and wrapping around.
func (f *FormScreen) move(delta int) {
	n := len(f.fields)
	for i := 1; i <= n; i++ {
		j := ((f.focus+delta*i)%n + n) % n
		if f.fields[j].Focusable() {
			f.focus = j
			return
		}
	}
}

// syncFocus focuses the current field and blurs the rest, returning the focused
// field's command (the cursor blink for a text field).
func (f *FormScreen) syncFocus() tea.Cmd {
	var cmd tea.Cmd
	for i, fld := range f.fields {
		if i == f.focus {
			cmd = fld.Focus()
		} else {
			fld.Blur()
		}
	}
	return cmd
}

// field looks up a field by key (nil if none).
func (f *FormScreen) field(key string) FormField {
	for _, fld := range f.fields {
		if fld.Key() == key {
			return fld
		}
	}
	return nil
}

// Value reads a text field's value by key ("" if the key is absent or not text).
func (f *FormScreen) Value(key string) string {
	if t, ok := f.field(key).(valued); ok {
		return t.Value()
	}
	return ""
}

// SetValue sets a text field's value by key (no-op if absent or not text).
func (f *FormScreen) SetValue(key, v string) {
	if t, ok := f.field(key).(valued); ok {
		t.SetValue(v)
	}
}

// Focus moves focus to the field with the given key, returning its focus command.
func (f *FormScreen) Focus(key string) tea.Cmd {
	for i, fld := range f.fields {
		if fld.Key() == key {
			f.focus = i
			return f.syncFocus()
		}
	}
	return nil
}

// FocusedKey is the key of the currently focused field.
func (f *FormScreen) FocusedKey() string { return f.current().Key() }

// FieldAnchor is the dropdown anchor for field key: below the row (above when the body is
// short), left edge under the value column. The rows above are measured, so folded rows
// are accounted for. false when the key is absent. Only correct for a form filling the
// body; a nested form would need its pane origin.
func (f *FormScreen) FieldAnchor(sh *core.Shared, key string) (MenuAnchor, bool) {
	bx, by := core.BoxOrigin()
	// Measured, matching chromeRows: WithTitle on an empty body is the title bar plus
	// the one line JoinVertical keeps, so its height less one is what the bar costs.
	titleRows := lipgloss.Height(core.WithTitle(f.title, "")) - 1
	y := sh.BodyY() + by + titleRows
	for _, fld := range f.fields {
		if fld.Key() == key {
			x := bx + markerWidth
			if lw, ok := fld.(interface{ labelWidth() int }); ok {
				x += lw.labelWidth()
			}
			return AnchorBelow(x, y), true
		}
		y += lipgloss.Height(fld.View(false))
	}
	return MenuAnchor{}, false
}

// SetFocused implements FocusableScreen: the box border is accented while the form's
// panel holds focus.
func (f *FormScreen) SetFocused(focused bool) { f.focused = focused }

func (f *FormScreen) View(sh *core.Shared) string {
	rows := make([]string, len(f.fields))
	for i, fld := range f.fields {
		rows[i] = fld.View(i == f.focus)
	}
	body := strings.Join(rows, "\n")
	if f.overlay {
		if hint := strings.Trim(sh.BindingHelp(f.help), " \n"); hint != "" {
			body += "\n\n" + hint
		}
		return core.PopupBox(f.title, body, f.renderWidth)
	}
	return core.WithTitle(f.title, sh.BoxFocused(body, f.focused))
}

func (f *FormScreen) HelpView(sh *core.Shared) string {
	if f.overlay {
		return ""
	}
	return sh.BindingHelp(f.help)
}

func (f *FormScreen) SetSize(sh *core.Shared, width, bodyHeight int) {
	// Give every field the box's inner width (from sh, the same source View renders with)
	// before measuring any height, since heights depend on widths.
	inner := sh.BoxInnerWidth()
	if f.overlay {
		available := max(width-6, 1) // PopupBox border plus two-column padding.
		inner = f.width
		if inner <= 0 {
			inner = min(64, available)
		}
		inner = min(inner, available)
		f.renderWidth = inner
	}
	var growers []Growable
	for _, fld := range f.fields {
		fld.SetInnerWidth(inner)
		if g, ok := fld.(Growable); ok {
			growers = append(growers, g)
		}
	}
	if len(growers) == 0 {
		return
	}
	// Budget growers against everything else's measured height (the frame plus each field,
	// growers counted at one row), since toggles and notes may fold.
	fixed := f.chromeRows(sh)
	if f.overlay {
		// Measure the popup's own border/padding/title against one placeholder body
		// row, then remove that row just as chromeRows does for a full-body form.
		fixed = lipgloss.Height(core.PopupBox(f.title, "", f.renderWidth)) - 1
		if hint := strings.Trim(sh.BindingHelp(f.help), " \n"); hint != "" {
			fixed += 2 + lipgloss.Height(hint) // the blank separator plus the help itself
		}
	}
	for _, fld := range f.fields {
		if _, ok := fld.(Growable); ok {
			fixed++ // counted, never rendered: its height is the answer we're computing
			continue
		}
		fixed += lipgloss.Height(fld.View(false)) // exact: fieldMarker is 2 cells either way
	}
	spare := (bodyHeight - fixed) / len(growers)
	for _, g := range growers {
		g.SetMaxHeight(1 + spare)
	}
}

// chromeRows is the frame's cost (box border, padding, margin and title bar), measured so
// style changes cannot un-clamp growers. Box("") carries one content line, subtracted.
func (f *FormScreen) chromeRows(sh *core.Shared) int {
	return lipgloss.Height(core.WithTitle(f.title, sh.Box(""))) - 1
}
