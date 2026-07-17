package components

import (
	"reflect"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// navKey builds a real (non-rune) key message, so navigation keys reach the form's
// keybind switch instead of being diverted into a focused text field by QueryUpdate
// (which only swallows rune/space/backspace).
func navKey(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// sampleForm builds a form whose focusable rows (name, scope, url) are separated by a
// non-focusable heading, so navigation has something to skip. It returns the form plus
// the toggle field for index assertions.
func sampleForm(opts ...func(*FormOpts)) (*FormScreen, *ToggleField) {
	scope := NewToggleField("scope", "Scope", []string{"A", "B", "C"})
	o := FormOpts{Fields: []FormField{
		NewTextField("name", "Name", ""),
		NewHeading("— section —"),
		scope,
		NewTextField("url", "URL", ""),
	}}
	for _, f := range opts {
		f(&o)
	}
	return NewForm(o), scope
}

func TestFormFocusCyclingSkipsAndWraps(t *testing.T) {
	f, _ := sampleForm()
	sh := core.NewShared(nil)
	if got := f.FocusedKey(); got != "name" {
		t.Fatalf("initial focus should be the first focusable field, got %q", got)
	}

	// down skips the heading and lands on the toggle, then the last text field.
	f.Update(sh, navKey(tea.KeyDown))
	if got := f.FocusedKey(); got != "scope" {
		t.Fatalf("NextField should skip the heading to scope, got %q", got)
	}
	f.Update(sh, navKey(tea.KeyDown))
	if got := f.FocusedKey(); got != "url" {
		t.Fatalf("NextField should advance to url, got %q", got)
	}
	// wrap forward back to the first focusable.
	f.Update(sh, navKey(tea.KeyDown))
	if got := f.FocusedKey(); got != "name" {
		t.Fatalf("NextField should wrap to name, got %q", got)
	}
	// up wraps backward to the last focusable, skipping the heading.
	f.Update(sh, navKey(tea.KeyUp))
	if got := f.FocusedKey(); got != "url" {
		t.Fatalf("PrevField should wrap backward to url, got %q", got)
	}
}

func TestFormInitialFocusOpt(t *testing.T) {
	f, _ := sampleForm(func(o *FormOpts) { o.Focus = "url" })
	if got := f.FocusedKey(); got != "url" {
		t.Fatalf("Focus opt should honor a focusable key, got %q", got)
	}

	// An unknown Focus key falls back to the first focusable field.
	g, _ := sampleForm(func(o *FormOpts) { o.Focus = "nope" })
	if got := g.FocusedKey(); got != "name" {
		t.Fatalf("unknown Focus key should fall back to first focusable, got %q", got)
	}
}

func TestFormToggleLeftRight(t *testing.T) {
	f, scope := sampleForm(func(o *FormOpts) { o.Focus = "scope" })
	sh := core.NewShared(nil)

	f.Update(sh, navKey(tea.KeyRight))
	if scope.Index() != 1 {
		t.Fatalf("Right on the focused toggle should advance the index, got %d", scope.Index())
	}
	f.Update(sh, navKey(tea.KeyLeft))
	if scope.Index() != 0 {
		t.Fatalf("Left on the focused toggle should retreat the index, got %d", scope.Index())
	}
}

func TestFormSelectRunsOnSubmit(t *testing.T) {
	submitted := false
	f, _ := sampleForm(func(o *FormOpts) {
		o.OnSubmit = func(*core.Shared, *FormScreen) core.Action { submitted = true; return core.Action{} }
	})
	f.Update(core.NewShared(nil), navKey(tea.KeyEnter))
	if !submitted {
		t.Error("Select on a plain field should run OnSubmit")
	}
}

func TestFormActivatorConsumesEnter(t *testing.T) {
	submitted := false
	pick := NewPickField("src", "Source", func() string { return "" },
		func(*core.Shared) (core.Action, bool) { return core.Pop(), true })
	f := NewForm(FormOpts{
		Fields:   []FormField{pick},
		OnSubmit: func(*core.Shared, *FormScreen) core.Action { submitted = true; return core.Action{} },
	})
	_, act := f.Update(core.NewShared(nil), navKey(tea.KeyEnter))
	if !reflect.DeepEqual(act, core.Pop()) {
		t.Errorf("an Activator consuming Enter should return its own Action, got %+v", act)
	}
	if submitted {
		t.Error("a consuming Activator should short-circuit OnSubmit")
	}
}

func TestFormBack(t *testing.T) {
	// Default Back pops.
	f, _ := sampleForm()
	_, act := f.Update(core.NewShared(nil), keyMsg("esc"))
	if !reflect.DeepEqual(act, core.Pop()) {
		t.Errorf("Back with no OnCancel should pop, got %+v", act)
	}

	// OnCancel wins when supplied.
	cancelled := false
	g, _ := sampleForm(func(o *FormOpts) {
		o.OnCancel = func(*core.Shared) core.Action { cancelled = true; return core.ResetToRoot() }
	})
	_, act = g.Update(core.NewShared(nil), keyMsg("esc"))
	if !cancelled || !reflect.DeepEqual(act, core.ResetToRoot()) {
		t.Errorf("Back should run OnCancel, got cancelled=%v act=%+v", cancelled, act)
	}
}

func TestFormTypingAndValueRoundTrip(t *testing.T) {
	f, _ := sampleForm()
	sh := core.NewShared(nil)
	f.Init(sh) // focus the first field so its input is focused

	// Typing feeds the focused text field.
	f.Update(sh, keyMsg("h"))
	f.Update(sh, keyMsg("i"))
	if got := f.Value("name"); got != "hi" {
		t.Fatalf("typed keys should reach the focused text field, Value = %q", got)
	}

	// SetValue / Value by key round-trip, and Focus moves focus by key.
	f.SetValue("url", "https://x")
	if got := f.Value("url"); got != "https://x" {
		t.Fatalf("SetValue/Value should round-trip, got %q", got)
	}
	f.Focus("url")
	if got := f.FocusedKey(); got != "url" {
		t.Fatalf("Focus(key) should move focus, got %q", got)
	}
	// A missing / non-text key is inert.
	if got := f.Value("missing"); got != "" {
		t.Errorf("Value of an absent key should be empty, got %q", got)
	}
}

// areaForm is a form whose text row is a TextAreaField, to pin that the growable field
// flows through the same form plumbing a TextField does.
func areaForm(opts ...func(*FormOpts)) (*FormScreen, *TextAreaField) {
	msg := NewTextAreaField("msg", "Message: ", "what changed?")
	o := FormOpts{Fields: []FormField{msg, NewToggleField("stage", "Stage", []string{"A", "B"})}}
	for _, f := range opts {
		f(&o)
	}
	return NewForm(o), msg
}

// TestFormRoutesTypingToATextArea covers the interface inversion end to end: the keystroke
// goes QueryUpdate -> Typable.UpdateInput -> editable.UpdateInput and lands in the
// textarea, and Value reads it back through the widened `valued` assertion. Value used to
// assert on *TextField concretely, which would return "" here.
func TestFormRoutesTypingToATextArea(t *testing.T) {
	f, _ := areaForm()
	sh := core.NewShared(nil)
	f.Init(sh)

	f.Update(sh, keyMsg("h"))
	f.Update(sh, keyMsg("i"))
	if got := f.Value("msg"); got != "hi" {
		t.Fatalf("typed keys should reach a focused TextAreaField, Value = %q", got)
	}

	f.SetValue("msg", "rewritten")
	if got := f.Value("msg"); got != "rewritten" {
		t.Fatalf("SetValue/Value should round-trip through a TextAreaField, got %q", got)
	}
}

// TestFormEnterSubmitsOverATextArea pins the invariant the field's height math rests on:
// the form claims Enter before the fall-through, so it submits rather than inserting a
// newline. If that ever changes, the value stops being one logical line and View's height
// goes silently wrong.
func TestFormEnterSubmitsOverATextArea(t *testing.T) {
	submitted := false
	f, _ := areaForm(func(o *FormOpts) {
		o.OnSubmit = func(*core.Shared, *FormScreen) core.Action { submitted = true; return core.Action{} }
	})
	sh := core.NewShared(nil)
	f.Init(sh)
	f.Update(sh, keyMsg("h"))
	f.Update(sh, navKey(tea.KeyEnter))

	if !submitted {
		t.Error("Enter on a TextAreaField should submit the form")
	}
	if got := f.Value("msg"); strings.Contains(got, "\n") {
		t.Errorf("Enter must not insert a newline into the field, got %q", got)
	}
}

// TestFormRowsFitTheBox pins SetSize's width arithmetic. The box word-wraps anything past
// ConfirmWidth-4; a row that overruns gets re-wrapped and the form grows a phantom row.
// This is what the -12 -> -15 fix is for, and it fails at -12.
func TestFormRowsFitTheBox(t *testing.T) {
	long := strings.Repeat("x", 200)
	for _, tc := range []struct {
		name  string
		field FormField
	}{
		{"TextField", NewTextField("f", "Message: ", "")},
		{"TextAreaField", NewTextAreaField("f", "Message: ", "")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := NewForm(FormOpts{Fields: []FormField{tc.field}})
			sh := core.NewShared(nil)
			// Fill the row to its full width, then let SetSize clamp a grower to one row so
			// both field types are measured against the same one-row expectation.
			f.SetSize(sh, 80, 1+f.chromeRows(sh))
			f.SetValue("f", long)

			want := 1 + f.chromeRows(sh)
			if got := lipgloss.Height(f.View(sh)); got != want {
				t.Errorf("a full-width row should not overrun the box and re-wrap: height %d, want %d\n%s",
					got, want, f.View(sh))
			}
		})
	}
}

// TestFormSetSizeClampsGrowthToBody pins the other half of SetSize: on a short body the
// grower gets fewer rows than growRows, so the form can't push itself out of its box.
func TestFormSetSizeClampsGrowthToBody(t *testing.T) {
	f, msg := areaForm()
	sh := core.NewShared(nil)
	msg.SetValue(strings.Repeat("alpha ", 60))

	// A body with room for the chrome, the toggle row, and 2 rows of message.
	f.SetSize(sh, 80, f.chromeRows(sh)+len(f.fields)+1)
	if got := lipgloss.Height(msg.View(true)); got != 2 {
		t.Errorf("a short body should clamp the grower to the rows it can spare, got %d", got)
	}

	// A tall body lets it reach the cap, but no further.
	f.SetSize(sh, 80, 100)
	if got := lipgloss.Height(msg.View(true)); got != growRows {
		t.Errorf("a tall body should let the grower reach growRows (%d), got %d", growRows, got)
	}
}

// TestFormSetSizeCountsFoldedRows pins the row accounting: a field that folds onto a
// second row spends a row the growers can't also have. SetSize used to assume one row per
// field, which a folded toggle silently violates — and the grower then grows into rows the
// box doesn't have.
//
// Built to bite at the 24-column floor, which is where every components test runs (SetSize
// reads sh.ConfirmWidth(), and core.NewShared(nil) has no width): inner is 20, so the
// toggle's content column is 20-2-5 = 13 and "aaaaaaaa | bbbbbbbb" (19) folds onto 2 rows.
func TestFormSetSizeCountsFoldedRows(t *testing.T) {
	msg := NewTextAreaField("msg", "M: ", "")
	msg.SetValue(strings.Repeat("alpha ", 40))
	f := NewForm(FormOpts{Fields: []FormField{
		msg,
		NewToggleField("stage", "Stage", []string{"aaaaaaaa", "bbbbbbbb"}, "|"),
	}})
	sh := core.NewShared(nil)

	// Room for the frame, the grower's first row, the toggle's two, and one row to spare.
	f.SetSize(sh, 80, f.chromeRows(sh)+4)

	if got := lipgloss.Height(f.fields[1].View(false)); got != 2 {
		t.Fatalf("fixture is wrong: the toggle should fold onto 2 rows, got %d", got)
	}
	// Counting the toggle as one row would leave 2 spare and grow the message to 3, one
	// row past what the body holds.
	if got := lipgloss.Height(msg.View(true)); got != 2 {
		t.Errorf("the grower should get only the rows the folded toggle leaves, got %d", got)
	}
}

func TestFormCrumbLabel(t *testing.T) {
	if got := NewForm(FormOpts{}).CrumbLabel(false); got != "Form" {
		t.Errorf("CrumbLabel default should be Form, got %q", got)
	}
	if got := NewForm(FormOpts{Crumb: "New"}).CrumbLabel(false); got != "New" {
		t.Errorf("CrumbLabel should use Crumb, got %q", got)
	}
	if got := NewForm(FormOpts{Crumb: "New", CrumbShort: "N"}).CrumbLabel(true); got != "N" {
		t.Errorf("CrumbLabel(short) should use CrumbShort, got %q", got)
	}
}
