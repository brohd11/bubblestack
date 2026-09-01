package components

import (
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// TestSpaceStillTogglesAField pins the key-string change v2 made underfoot: v1
// normalized a bare space to KeySpace, whose name was " "; v2's Key.String() answers
// "space" instead. core.Keys.Toggle moved with it, and this is the binding's one
// user-visible job.
func TestSpaceStillTogglesAField(t *testing.T) {
	f, toggle := sampleForm()
	sh := core.NewShared(nil)
	f.SetSize(sh, 80, 24)
	for f.current() != FormField(toggle) {
		f.move(1)
	}

	before := toggle.Value()
	f.Update(sh, keyMsg("space"))
	if toggle.Value() == before {
		t.Fatalf("space should step the toggle off %q; the Toggle binding is not matching", before)
	}
}

// TestPastedArrowTextInsertsRatherThanNavigating is the workaround v2 made structural.
// v1 delivered a bracketed paste as one rune-bearing key whose String() was the pasted
// text, so pasting the literal word "up" was indistinguishable from pressing ↑ — which
// is why the filtering branch used to match on the key TYPE. v2 routes a paste to its
// own message, so the text reaches the filter and the cursor stays put.
func TestPastedArrowTextInsertsRatherThanNavigating(t *testing.T) {
	l := newList(Item{Name: "A"}, Item{Name: "B"}, Item{Name: "C"})
	startFiltering(&l, "")
	l.CursorDown()
	at := l.Index()

	RootUpdate(core.NewShared(nil), &l, tea.PasteMsg{Content: "up"})
	if got := l.Index(); got != at {
		t.Fatalf("a pasted \"up\" moved the cursor from %d to %d; it is text, not the arrow key", at, got)
	}
	if got := l.FilterValue(); got != "up" {
		t.Fatalf("the pasted text should reach the filter, got %q", got)
	}
}

// TestPastedNewlineStaysOneLine: a TextAreaField holds a single logical line so its
// height math stays exact. v1 had to rewrite the rune payload of a paste-flagged key;
// v2's tea.PasteMsg is the only way a newline can arrive, so that is the one guard.
func TestPastedNewlineStaysOneLine(t *testing.T) {
	ta := NewTextAreaField("k", "L", "")
	ta.Focus() // textarea ignores input while blurred, as a form field does
	ta.UpdateInput(tea.PasteMsg{Content: "one\ntwo\r\nthree"})
	if got := ta.Value(); got != "one two three" {
		t.Fatalf("pasted value = %q, want the newlines collapsed to spaces", got)
	}
}
