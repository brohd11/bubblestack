package editor

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/brohd11/bubblestack/core"
)

func TestSignClickPreservesSelectionAndUsesRenderedColumn(t *testing.T) {
	var hits []SignClick
	s, sh := newEditor(Opts{LineNumbers: true, OnSignClick: func(_ *core.Shared, hit SignClick) (core.Action, bool) {
		hits = append(hits, hit)
		return core.Action{}, hit.Column == "git"
	}})
	s.SetEmbedded(true)
	s.SetPaneOrigin(30, 4)
	s.SetSize(sh, 40, 15)
	s.SetText("one\ntwo\nthree\nfour")
	s.SetSignColumnOrder("diagnostics", "git")
	s.ShowSignColumn("diagnostics", true)
	s.ShowSignColumn("git", true)
	s.SetSignColumn("diagnostics", map[int]Sign{1: {Text: "E"}})
	s.SetSignColumn("git", map[int]Sign{1: {Text: "┃"}})
	s.curY, s.curX = 2, 2
	s.selStart, s.selEnd = textPos{1, 0}, textPos{2, 2}
	before := s.selectedText()
	s.scrY = 1
	x, y := s.insetX()+1, s.insetY()
	s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if len(hits) != 1 || hits[0] != (SignClick{Column: "git", Line: 1, X: x + 30, Y: y + 4}) {
		t.Fatalf("click = %+v", hits)
	}
	s.Update(sh, tea.MouseMotionMsg{X: x + 8, Y: y + 1, Button: tea.MouseLeft})
	s.Update(sh, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
	if s.curY != 2 || s.curX != 2 || s.selectedText() != before || s.dragging {
		t.Fatalf("sign click or its tail changed caret/selection: %+v %q", s.CursorPosition(), s.selectedText())
	}
	// A host declining a different sign leaves the ordinary gutter caret click intact.
	s.Update(sh, tea.MouseClickMsg{X: s.insetX(), Y: y, Button: tea.MouseLeft})
	if len(hits) != 2 || hits[1].Column != "diagnostics" || s.curY != 1 || s.curX != 0 {
		t.Fatalf("declined sign did not fall through: hits=%+v cursor=%+v", hits, s.CursorPosition())
	}
}

func TestSignHitRejectsBlankContinuationAndClippedCells(t *testing.T) {
	s, sh := newEditor(Opts{Wrap: true, LineNumbers: true, HideTitle: true})
	s.SetEmbedded(true)
	s.SetPaneOrigin(10, 3)
	s.SetSize(sh, 20, 12)
	s.SetText(strings.Repeat("x", 60) + "\nblank\nlast")
	s.SetSignColumnOrder("git", "diagnostics")
	s.ShowSignColumn("git", true)
	s.ShowSignColumn("diagnostics", true)
	s.SetSignColumn("git", map[int]Sign{0: {Text: "┃"}, 2: {Text: " "}})
	s.SetSignColumn("diagnostics", map[int]Sign{0: {Text: "E"}})
	s.rebuildWrapRows()
	for _, cell := range [][2]int{
		{s.insetX() - 1, s.insetY()},               // padding
		{s.insetX() + 2, s.insetY()},               // numbers
		{s.insetX(), s.insetY() + 1},               // wrapped continuation
		{s.insetX(), s.insetY() + len(s.wrapRows)}, // below EOF
		{s.insetX(), s.insetY() - 1},               // above body
		{s.insetX(), s.insetY() + s.h},             // below body
	} {
		if hit, ok := s.signAt(sh, cell[0], cell[1]); ok {
			t.Errorf("blank/outside cell %v hit %+v", cell, hit)
		}
	}
	for i, row := range s.wrapRows {
		if row.line > 0 {
			if hit, ok := s.signAt(sh, s.insetX(), s.insetY()+i); ok {
				t.Errorf("missing/space sign hit %+v", hit)
			}
		}
	}
	s.scrY = 1 // the first visible row is itself a continuation
	if _, ok := s.signAt(sh, s.insetX(), s.insetY()); ok {
		t.Fatal("scrolled continuation claimed a sign")
	}
	s.scrY = 0
	s.SetSize(sh, 4, 12) // inset leaves three cells: only the inner sign fits
	hit, ok := s.signAt(sh, s.insetX(), s.insetY())
	if !ok || hit.Column != "diagnostics" {
		t.Fatalf("narrow viewport did not follow visibleGutter: %+v %v", hit, ok)
	}
	s.ShowSignColumn("diagnostics", false)
	hit, ok = s.signAt(sh, s.insetX(), s.insetY())
	if !ok || hit.Column != "git" {
		t.Fatalf("hidden column still occupied a cell: %+v %v", hit, ok)
	}
}

func TestSignClickStandaloneChromeAndModifiers(t *testing.T) {
	hits := 0
	s, sh := newEditor(Opts{Border: true, OnSignClick: func(_ *core.Shared, _ SignClick) (core.Action, bool) {
		hits++
		return core.Action{}, true
	}})
	s.SetText("alpha\nbeta")
	s.ShowSigns(true)
	s.SetSigns(map[int]Sign{0: {Text: "+"}})
	x, y := s.insetX(), sh.BodyY()+s.insetY()
	hit, ok := s.signAt(sh, x, y)
	if !ok || hit.X != x || hit.Y != y || hit.Line != 0 {
		t.Fatalf("standalone anchor: %+v %v", hit, ok)
	}
	for _, mod := range []tea.KeyMod{tea.ModShift, tea.ModAlt, tea.ModCtrl} {
		s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft, Mod: mod})
	}
	if hits != 0 {
		t.Fatal("modified selection/definition gestures were intercepted")
	}
	s.Update(sh, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	if hits != 1 {
		t.Fatal("unmodified press did not reach the hook")
	}
}
