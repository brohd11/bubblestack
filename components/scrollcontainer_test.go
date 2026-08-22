package components

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"
)

// TestScrollContainerLegendTracksKeymap is the guard the stale legend got past: the
// focused pane's border once advertised "⇧←→ panes" for months after the pane keys
// became shift+tab alone, while the host's help bar one row below it said ⇧tab. Two
// things pin it now — the legend carries no pane keys at all (they are the screen's,
// so the bar owns them alone), and the scroll hint that remains is derived from
// core.Keys rather than spelled out, so rebinding moves it too.
func TestScrollContainerLegendTracksKeymap(t *testing.T) {
	p := NewScrollContainer("preview")
	p.SetSize(40, 6)
	p.SetLines([]string{"body"})

	unfocused := p.View(false)
	if strings.Contains(unfocused, "scroll") {
		t.Fatalf("an unfocused pane advertises no keys; got:\n%s", unfocused)
	}

	v := p.View(true)
	for _, want := range []string{
		"preview",
		core.Legend(core.Hint("scroll", core.Keys.Up, core.Keys.Down)),
	} {
		if !strings.Contains(v, want) {
			t.Errorf("focused legend is missing %q; got:\n%s", want, v)
		}
	}
	// Pane navigation is the screen's key, not this pane's: the host's help bar
	// advertises it and the border does not repeat it.
	if strings.Contains(v, "panes") {
		t.Errorf("the border legend must not carry the pane hint; got:\n%s", v)
	}
	// The specific rot: keys no binding carries must not appear.
	for _, stale := range []string{"⇧←", "⇧→"} {
		if strings.Contains(v, stale) {
			t.Errorf("legend advertises %q, which core.Keys binds to nothing; got:\n%s", stale, v)
		}
	}
}

// TestScrollContainerKeyHintsOptOut: SetKeyHints(false) leaves the title alone on the
// border — the bordered ListPanel's look, which is the point of the switch — while the
// panel still contributes its keys to the host's help bar through PanelHelp.
func TestScrollContainerKeyHintsOptOut(t *testing.T) {
	p := NewScrollContainer("preview")
	p.SetKeyHints(false)
	p.SetSize(40, 6)
	p.SetLines([]string{"body"})

	v := p.View(true)
	if !strings.Contains(v, "preview") {
		t.Fatalf("the title stays on the border; got:\n%s", v)
	}
	for _, hint := range []string{"scroll", "panes"} {
		if strings.Contains(v, hint) {
			t.Errorf("key hints are opted out, but the legend still shows %q; got:\n%s", hint, v)
		}
	}
	if len(p.PanelHelp()) == 0 {
		t.Error("the help bar must still get the pane's keys")
	}
	p.SetKeyHints(true)
	if v := p.View(true); !strings.Contains(v, "scroll") {
		t.Error("SetKeyHints(true) should put the legend back")
	}
}

// TestScrollContainerSetTitle: the legend is a construction-time field, so a pane whose
// content changes shape (a count, a warning) needs the setter to say so on its own edge
// rather than spending a content row on it.
func TestScrollContainerSetTitle(t *testing.T) {
	p := NewScrollContainer("Files to Commit")
	p.SetSize(40, 6)
	p.SetLines([]string{"body"})

	p.SetTitle("Files to Commit (2 new)")
	v := p.View(false)
	if !strings.Contains(v, "Files to Commit (2 new)") {
		t.Errorf("the border should carry the new title; got:\n%s", v)
	}
}
