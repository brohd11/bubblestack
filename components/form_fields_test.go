package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// newArea builds a focused TextAreaField whose *content* is width columns wide. Focus
// matters: a blurred textarea ignores Update entirely. SetInnerWidth takes the box's inner
// width, so add back what the field subtracts for its own marker and label.
func newArea(label string, width int) *TextAreaField {
	ta := NewTextAreaField("msg", label, "placeholder")
	ta.SetInnerWidth(width + markerWidth + lipgloss.Width(label))
	ta.Focus()
	return ta
}

// typeInto feeds s a rune at a time, the way a user's keystrokes actually arrive.
func typeInto(ta *TextAreaField, s string) {
	for _, r := range s {
		ta.UpdateInput(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// TestTextAreaFieldWrapsInsteadOfScrolling is the regression this field exists for. A
// TextField would scroll its textinput's viewport rightward here and the first word would
// be gone from the render; the textarea wraps and keeps the whole message on screen.
func TestTextAreaFieldWrapsInsteadOfScrolling(t *testing.T) {
	ta := newArea("Message: ", 10)
	typeInto(ta, "alpha bravo charlie delta echo")

	view := ansi.Strip(ta.View(true))
	if !strings.Contains(view, "alpha") {
		t.Errorf("a message past the field width must still show its start, got:\n%s", view)
	}
	if !strings.Contains(view, "echo") {
		t.Errorf("a message past the field width must still show its end, got:\n%s", view)
	}
	if lipgloss.Height(view) < 2 {
		t.Errorf("a message past the field width should wrap onto extra rows, got:\n%s", view)
	}
}

func TestTextAreaFieldGrowsWithContentThenCaps(t *testing.T) {
	ta := newArea("M: ", 10)
	if got := lipgloss.Height(ta.View(true)); got != 1 {
		t.Errorf("an empty field should be one row (the placeholder), got %d", got)
	}

	typeInto(ta, "alpha bravo")
	if got := lipgloss.Height(ta.View(true)); got != 2 {
		t.Errorf("a value needing two rows should render two, got %d", got)
	}

	// Well past the cap: the field stops growing and scrolls internally instead.
	typeInto(ta, strings.Repeat(" xxxxxxxx", 20))
	if got := lipgloss.Height(ta.View(true)); got != growRows {
		t.Errorf("growth should stop at growRows (%d), got %d", growRows, got)
	}
}

// TestTextAreaFieldSetMaxHeightClamps covers the Growable path the form drives on a short
// terminal: the offered ceiling wins when it's below growRows, and can never be zero.
func TestTextAreaFieldSetMaxHeightClamps(t *testing.T) {
	ta := newArea("M: ", 10)
	typeInto(ta, strings.Repeat("alpha ", 20))

	ta.SetMaxHeight(2)
	if got := lipgloss.Height(ta.View(true)); got != 2 {
		t.Errorf("SetMaxHeight(2) should cap the row at 2, got %d", got)
	}
	ta.SetMaxHeight(0)
	if got := lipgloss.Height(ta.View(true)); got != 1 {
		t.Errorf("SetMaxHeight(0) should clamp up to 1 row, got %d", got)
	}
	ta.SetMaxHeight(99)
	if got := lipgloss.Height(ta.View(true)); got != growRows {
		t.Errorf("SetMaxHeight above growRows should clamp to it, got %d", got)
	}
}

// TestTextAreaFieldRowsHangUnderTheText pins the hanging indent: without it, wrapped rows
// restart at column 0 and collide with the label column.
func TestTextAreaFieldRowsHangUnderTheText(t *testing.T) {
	label := "Message: "
	ta := newArea(label, 10)
	typeInto(ta, "alpha bravo charlie")

	lines := strings.Split(ansi.Strip(ta.View(true)), "\n")
	if len(lines) < 2 {
		t.Fatalf("need a wrapped render to check the indent, got:\n%s", strings.Join(lines, "\n"))
	}
	indent := strings.Repeat(" ", 2+len(label)) // 2 = the focus marker
	for i, line := range lines[1:] {
		if !strings.HasPrefix(line, indent) {
			t.Errorf("wrapped row %d should hang under the text (%d spaces), got %q", i+1, len(indent), line)
		}
	}
}

// TestTextAreaFieldPasteStaysOneLine guards the invariant the height math rests on:
// LineInfo().Height covers the cursor's logical line only, so a second line would make
// View's height silently wrong. A bracketed paste is the only way one can arrive.
func TestTextAreaFieldPasteStaysOneLine(t *testing.T) {
	ta := newArea("M: ", 40)
	ta.UpdateInput(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("first\nsecond")})

	if got := ta.Value(); got != "first second" {
		t.Errorf("a pasted newline should collapse to a space, got %q", got)
	}
	if got := ta.input.LineCount(); got != 1 {
		t.Errorf("the value must stay one logical line, got %d lines", got)
	}
}

func TestTextAreaFieldSetValueCollapsesNewlines(t *testing.T) {
	ta := newArea("M: ", 40)
	ta.SetValue("first\r\nsecond\nthird")

	if got := ta.Value(); got != "first second third" {
		t.Errorf("SetValue should collapse newlines like textinput does, got %q", got)
	}
	if got := ta.input.LineCount(); got != 1 {
		t.Errorf("SetValue must leave one logical line, got %d lines", got)
	}
}

// ---------- row width invariant ----------

// stageLabel/stageOpts are the real commit form's Stage row (gitstack/repoui/repomenu.go),
// the widest toggle in the workspace at 59 cells and the row that motivated width-aware
// fields: it overran the box below a 73-column terminal, and the box re-wrapped it at
// column 0 into the label column.
const stageLabel = "Stage:   "

var stageOpts = []string{"tracked changes (-a)", "all, incl. new files (-A)"}

// allFields builds one of each field type at its widest realistic content, so the invariant
// below is checked against every implementer rather than just the one that broke.
func allFields() []FormField {
	tf := NewTextField("t", stageLabel, "")
	tf.SetValue(strings.Repeat("x", 120))
	ta := NewTextAreaField("a", stageLabel, "")
	ta.SetValue(strings.Repeat("alpha ", 40))
	pick := NewPickField("p", stageLabel, func() string { return strings.Repeat("y", 90) }, nil)
	return []FormField{
		tf, ta,
		NewToggleField("g", stageLabel, stageOpts, "|"),
		pick,
		NewNote("addon_manifest.yml is created in this directory (blank ⇒ project root)."),
	}
}

// TestFieldRowsNeverExceedTheInnerWidth is the invariant every field owes the form: fold
// your own content, because the box folds an overrun at column 0 — into the label column,
// and onto a row the form never budgeted. This is the test that fails against a no-op
// SetWidth, and the reason a toggle needs a last-resort fold rather than only packing
// between its options: at inner 20 the content column is 9 cells and "all, incl. new files
// (-A)" is 25, so no amount of packing fits it.
// Trailing spaces are measured out, not tolerated by accident: lipgloss's wrap drops a
// run of trailing spaces rather than breaking a line for it, so they cannot cause the
// column-0 re-wrap this invariant is about. TextField relies on that — textinput renders
// Width cells of text plus a trailing cursor cell, which is a space whenever the cursor
// sits at the end of the value. (The distinction is real, not a fudge: the same test
// catches a row that ends in real characters — that's what failed when the width
// arithmetic was off by 3.)
func TestFieldRowsNeverExceedTheInnerWidth(t *testing.T) {
	for _, inner := range []int{20, 40, 50, 66, 80} {
		for _, fld := range allFields() {
			fld.SetInnerWidth(inner)
			for i, line := range strings.Split(ansi.Strip(fld.View(true)), "\n") {
				line = strings.TrimRight(line, " ")
				if got := lipgloss.Width(line); got > inner {
					t.Errorf("%T at inner %d: line %d is %d cells, over by %d:\n%q",
						fld, inner, i, got, got-inner, line)
				}
			}
		}
	}
}

// TestTextFieldSeededBeforeResizeScrolls guards the order every seeded field in gdaddon
// uses — SetValue at construction, SetInnerWidth on the first resize. textinput recomputes
// its scroll window only on a value/cursor change, and the window it computes while Width
// is 0 is the entire value, so without SetInnerWidth re-running that the row renders the
// whole seeded URL and blows out the box until the first keystroke.
func TestTextFieldSeededBeforeResizeScrolls(t *testing.T) {
	tf := NewTextField("url", "URL: ", "")
	tf.SetValue("https://github.com/some-org/some-really-long-repository-name")
	tf.SetInnerWidth(30) // content column: 30 - 2 - 5 = 23

	got := ansi.Strip(tf.View(false))
	if w := lipgloss.Width(strings.TrimRight(got, " ")); w > 30 {
		t.Errorf("a seeded field should scroll to its width once sized, got %d cells:\n%q", w, got)
	}
}

// ---------- ToggleField ----------

func TestToggleFieldPacksBetweenOptions(t *testing.T) {
	// inner 50 ⇒ content 39, which fits either option but not both (48 + separator).
	g := NewToggleField("g", stageLabel, stageOpts, "|")
	g.SetInnerWidth(50)

	lines := strings.Split(ansi.Strip(g.View(false)), "\n")
	if len(lines) != 2 {
		t.Fatalf("a toggle too wide for one row should fold onto two, got %d:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if want := "  " + stageLabel + "tracked changes (-a) |"; strings.TrimRight(lines[0], " ") != want {
		t.Errorf("the delimiter should trail the folded line so it reads as continuing:\n got %q\nwant %q", lines[0], want)
	}
	if indent := strings.Repeat(" ", markerWidth+len(stageLabel)); !strings.HasPrefix(lines[1], indent) {
		t.Errorf("the folded row should hang under the content column, got %q", lines[1])
	}
	// The point of packing between options: ansi.Wrap would have split "(-a)" at the
	// hyphen, since it treats "-" as an unconditional breakpoint.
	for _, opt := range stageOpts {
		if !strings.Contains(ansi.Strip(g.View(false)), opt) {
			t.Errorf("each option should survive whole, %q did not:\n%s", opt, ansi.Strip(g.View(false)))
		}
	}
}

func TestToggleFieldStaysOneLineWhenItFits(t *testing.T) {
	g := NewToggleField("g", stageLabel, stageOpts, "|")
	g.SetInnerWidth(66) // content 55 > the 48-cell toggle

	got := g.View(true)
	if strings.Contains(got, "\n") {
		t.Errorf("a toggle that fits should stay on one row, got:\n%s", got)
	}
	// A one-row field must render exactly as the concatenation it used to be — this pins
	// that routing every field through fieldRow/JoinHorizontal changed no bytes.
	want := fieldMarker(true) + fieldLabel().Render(stageLabel) + RenderToggle(stageOpts, 0, "|")
	if got != want {
		t.Errorf("an unfolded row should be byte-identical to the old concat:\n got %q\nwant %q", got, want)
	}
}

// TestRenderToggleIsUnfolded pins the contract the two New Plugin confirm bodies depend on
// (gdaddon .../newplugin/newplugin.go and store.go): they call RenderToggle directly,
// bypassing ToggleField, and embed the result inline in a dialog body that does its own
// layout. A newline in there would break it.
func TestRenderToggleIsUnfolded(t *testing.T) {
	for _, delim := range []string{"|", ""} {
		if got := RenderToggle(stageOpts, 0, delim); strings.Contains(got, "\n") {
			t.Errorf("RenderToggle(delim=%q) must stay on one line, got:\n%s", delim, got)
		}
	}
	if got, want := ansi.Strip(RenderToggle(stageOpts, 0, "|")), strings.Join(stageOpts, " | "); got != want {
		t.Errorf("RenderToggle should join the options unchanged:\n got %q\nwant %q", got, want)
	}
}

// ---------- StaticField ----------

func TestStaticFieldWrapsToTheInnerWidth(t *testing.T) {
	// The real gdaddon note (71 cells), which already folds on a standard 80-col terminal.
	note := NewNote("addon_manifest.yml is created in this directory (blank ⇒ project root).")

	note.SetInnerWidth(66)
	if got := lipgloss.Height(note.View(false)); got != 2 {
		t.Errorf("a 71-cell note at inner 66 should fold onto 2 rows, got %d", got)
	}
	// Before the first resize there's no width to fold to, and ansi.Wrap passes it through.
	fresh := NewNote("addon_manifest.yml is created in this directory (blank ⇒ project root).")
	if got := lipgloss.Height(fresh.View(false)); got != 1 {
		t.Errorf("an unsized note should render on one row, got %d", got)
	}
}

// Deliberately untested: that the field renders with textarea's decorations stripped
// (the constructor's plain FocusedStyle/BlurredStyle and the Blur() that makes them
// take). lipgloss detects no TTY under `go test` and drops to an ASCII profile, so every
// render here is colorless whether the styles took or not — an assertion on them would
// pass unconditionally. Verified in the real TUI instead.
