package components

import (
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// caretTo puts the caret on a cell and re-asserts visibility the way a keystroke does,
// without the 120 arrow presses the same walk would otherwise take.
func caretTo(s *EditorScreen, y, x int) {
	s.curY, s.curX, s.wantX = y, x, x
	s.clampScroll()
}

// TestEditorHCaretBand: the horizontal clamp parks the caret in a band measured from the
// RIGHT edge of the window, so the text behind it — the part already read — stays on screen
// and a caret coming back leftwards restores the start of the line long before it gets there.
func TestEditorHCaretBand(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(wideDoc()) // one 200-cell line, no scrollbar to narrow textW

	w := s.contentW()
	lo, hi := s.hCaretBand()
	if wantLo, wantHi := w-1-w*editorHCaretFarPct/100, w-1-w*editorHCaretNearPct/100; lo != wantLo || hi != wantHi {
		t.Fatalf("hCaretBand = (%d, %d), want (%d, %d) of a %d-cell window", lo, hi, wantLo, wantHi, w)
	}

	// Rightward the view holds until the caret passes the near edge of the band, and no
	// sooner: the caret standing ON hi is still inside it.
	caretTo(s, 1, hi)
	if s.scrX != 0 {
		t.Fatalf("a caret on the near edge of the band scrolled: scrX %d, want 0", s.scrX)
	}
	caretTo(s, 1, hi+1)
	if s.scrX != 1 {
		t.Fatalf("a caret one past the band → scrX %d, want 1", s.scrX)
	}

	// Mid-line the band is held open on the right: the line goes on past the caret.
	caretTo(s, 1, 150)
	if got := s.curX - s.scrX; got != hi {
		t.Fatalf("the caret sits %d cells into the window, want %d", got, hi)
	}

	// Leftward the view holds for the width of the band, then rolls left with the caret.
	held := s.scrX
	for s.curX-s.scrX > lo {
		caretTo(s, 1, s.curX-1)
	}
	if s.scrX != held {
		t.Fatalf("the view moved inside the band: scrX %d, want %d", s.scrX, held)
	}
	caretTo(s, 1, s.curX-1)
	if s.scrX != held-1 {
		t.Fatalf("a caret crossing the far edge → scrX %d, want %d", s.scrX, held-1)
	}

	// And it reaches column 0 with the caret still a band's width in, rather than only once
	// the caret sits on column 0 itself.
	for s.curX > lo {
		caretTo(s, 1, s.curX-1)
	}
	if s.scrX != 0 {
		t.Fatalf("scrX %d with the caret %d cells in, want the start of the line back on screen",
			s.scrX, s.curX)
	}

	// At the end of the line there is nothing further to reveal, so no gap is held open and
	// the caret takes the last text cell — exactly where the minimal clamp leaves it.
	caretTo(s, 1, len(s.lines[1]))
	if want := len(s.lines[1]) - w + 1; s.scrX != want {
		t.Fatalf("a caret at end of line → scrX %d, want %d (no blank past the line)", s.scrX, want)
	}

	// A window too narrow to spare the cells rounds the near margin away, and the overflow
	// marker's nudge is what keeps the caret visible there.
	s.SetSize(sh, 9, 10)
	if _, hi := s.hCaretBand(); hi != s.contentW()-1 {
		t.Fatalf("a %d-cell window claims a right margin (hi %d)", s.contentW(), hi)
	}
	caretTo(s, 1, 130)
	if cell := s.curX - s.scrX; cell != s.contentW()-2 {
		t.Fatalf("the caret sits in column %d of a %d-cell window, want it clear of the marker",
			cell, s.contentW())
	}
}

// TestEditorHCaretBandIsKeyboardOnly: a pointer gesture places the caret on text the user is
// already looking at, so it must not re-park the view and slide that text out from under them.
func TestEditorHCaretBandIsKeyboardOnly(t *testing.T) {
	press := func(mod tea.KeyMod) (*EditorScreen, *core.Shared, int) {
		s, sh := newEditor(EditorOpts{})
		s.setContent(wideDoc())
		caretTo(s, 1, 150) // scrolled right, the caret parked in the band
		if s.scrX == 0 {
			t.Fatal("the setup should have scrolled the view right")
		}
		was := s.scrX
		s.Update(sh, tea.MouseClickMsg{X: 5, Y: s.insetY() + 1, Button: tea.MouseLeft, Mod: mod})
		return s, sh, was
	}

	for name, mod := range map[string]tea.KeyMod{"click": 0, "shift+click": tea.ModShift} {
		t.Run(name, func(t *testing.T) {
			s, _, was := press(mod)
			if s.scrX != was {
				t.Fatalf("a %s moved the view: scrX %d, want %d", name, s.scrX, was)
			}
			if want := was + 5; s.curX != want {
				t.Fatalf("the caret landed on cell %d, want %d — the one under the pointer", s.curX, want)
			}
		})
	}

	// The press that opens a drag is the same story, and the drag that follows still selects
	// from the cell that was pointed at.
	s, sh, was := press(0)
	s.Update(sh, tea.MouseMotionMsg{X: 25, Y: s.insetY() + 1, Button: tea.MouseLeft})
	if s.scrX != was {
		t.Fatalf("a drag re-parked the view: scrX %d, want %d", s.scrX, was)
	}
	// [press, motion] inclusive of the cell the pointer is on, both counted in the buffer
	// rather than on screen — which is only right because the view never moved.
	if s.selStart.x != was+5 || s.selEnd.x != was+26 {
		t.Fatalf("the drag selected [%d, %d), want [%d, %d)", s.selStart.x, s.selEnd.x, was+5, was+26)
	}
}

// dragTick delivers one auto-scroll frame addressed to this editor's live gesture.
func dragTick(s *EditorScreen, sh *core.Shared) core.Action {
	_, act := s.Update(sh, editorDragScrollMsg{target: s, seq: s.dragSeq})
	return act
}

// TestEditorDragAutoScrollVertical: a drag held at the bottom edge keeps scrolling and
// keeps selecting, faster the further past the pane the pointer is, and stops on its own
// when the pointer comes back inside.
func TestEditorDragAutoScrollVertical(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc())
	top := s.insetY()
	bottom := top + s.h - 1

	s.Update(sh, tea.MouseClickMsg{X: 20, Y: top, Button: tea.MouseLeft})
	_, act := s.Update(sh, tea.MouseMotionMsg{X: 20, Y: bottom, Button: tea.MouseLeft})
	if act.Cmd == nil || !s.dragScrolling {
		t.Fatal("a drag onto the bottom edge row should arm the auto-scroll clock")
	}

	act = dragTick(s, sh)
	if s.scrY != editorDragScrollUnitY {
		t.Fatalf("one frame at the edge scrolled %d lines, want %d", s.scrY, editorDragScrollUnitY)
	}
	if act.Cmd == nil {
		t.Fatal("a frame that scrolled should schedule the next one")
	}
	if s.selEnd.y != s.scrY+s.h-1 {
		t.Fatalf("the selection ended at line %d, want the row under the pointer (%d)",
			s.selEnd.y, s.scrY+s.h-1)
	}

	// Thrown well past the pane, the same clock runs at the ceiling.
	s.Update(sh, tea.MouseMotionMsg{X: 20, Y: bottom + 4, Button: tea.MouseLeft})
	was := s.scrY
	dragTick(s, sh)
	if got, want := s.scrY-was, editorDragScrollUnitY*editorDragScrollMaxUnits; got != want {
		t.Fatalf("a pointer past the pane scrolled %d lines a frame, want %d", got, want)
	}

	// Back inside, the next frame is the last one.
	s.Update(sh, tea.MouseMotionMsg{X: 20, Y: top + s.h/2, Button: tea.MouseLeft})
	was = s.scrY
	act = dragTick(s, sh)
	if s.scrY != was || act.Cmd != nil || s.dragScrolling {
		t.Fatalf("the clock should stop with the pointer inside: scrY %d (was %d), rearmed %v",
			s.scrY, was, act.Cmd != nil || s.dragScrolling)
	}
}

// TestEditorDragAutoScrollHorizontal: the same at the right edge, sideways only — and
// wrapped there is nowhere to roll sideways to, so only the vertical half survives.
func TestEditorDragAutoScrollHorizontal(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(wideDoc())
	row := s.insetY() + 1 // the 200-cell line
	edge := s.insetX() + s.leftGutterWidth() + s.contentW() - 1

	s.Update(sh, tea.MouseClickMsg{X: 20, Y: row, Button: tea.MouseLeft})
	_, act := s.Update(sh, tea.MouseMotionMsg{X: edge, Y: row, Button: tea.MouseLeft})
	if act.Cmd == nil {
		t.Fatal("a drag onto the right edge column should arm the auto-scroll clock")
	}
	dragTick(s, sh)
	if s.scrX != editorDragScrollUnitX {
		t.Fatalf("one frame at the right edge scrolled %d cells, want %d", s.scrX, editorDragScrollUnitX)
	}
	if s.scrY != 0 {
		t.Fatalf("a sideways drag scrolled vertically, scrY = %d", s.scrY)
	}

	s.Update(sh, tea.MouseMotionMsg{X: edge + 12, Y: row, Button: tea.MouseLeft})
	was := s.scrX
	dragTick(s, sh)
	if got, want := s.scrX-was, editorDragScrollUnitX*editorDragScrollMaxUnits; got != want {
		t.Fatalf("a pointer past the right edge scrolled %d cells a frame, want %d", got, want)
	}

	// Wrapped, every cell of the line is on screen already: only the vertical half is left.
	s.ToggleWrap()
	if dx, _ := s.dragEdgeScroll(sh, edge+12, row); dx != 0 {
		t.Fatalf("wrapped, a drag past the right edge asked for %d cells of scroll, want 0", dx)
	}
	if _, dy := s.dragEdgeScroll(sh, edge+12, s.insetY()+s.h+2); dy <= 0 {
		t.Fatalf("wrapped, a drag below the pane should still scroll down, got %d", dy)
	}
}

// TestEditorDragScrollStops: the generation on the gesture is what ends the clock, so a
// frame that was already in flight when the drag ended lands as a no-op — for a release
// and for the keystroke that abandons a drag alike.
func TestEditorDragScrollStops(t *testing.T) {
	for name, end := range map[string]func(*EditorScreen, *core.Shared){
		"release": func(s *EditorScreen, sh *core.Shared) {
			s.Update(sh, tea.MouseReleaseMsg{X: 20, Y: s.insetY() + s.h - 1, Button: tea.MouseNone})
		},
		"keystroke": func(s *EditorScreen, sh *core.Shared) { s.key(sh, keyMsg("left")) },
	} {
		t.Run(name, func(t *testing.T) {
			s, sh := newEditor(EditorOpts{})
			s.setContent(longDoc())
			top := s.insetY()

			s.Update(sh, tea.MouseClickMsg{X: 20, Y: top, Button: tea.MouseLeft})
			s.Update(sh, tea.MouseMotionMsg{X: 20, Y: top + s.h - 1, Button: tea.MouseLeft})
			stale := editorDragScrollMsg{target: s, seq: s.dragSeq}
			end(s, sh)

			was := s.scrY
			_, act := s.Update(sh, stale)
			if s.scrY != was || act.Cmd != nil {
				t.Fatalf("a frame from an ended gesture scrolled to %d (was %d) or re-armed (%v)",
					s.scrY, was, act.Cmd != nil)
			}
		})
	}
}

// TestEditorWheelDuringDrag: the wheel stays usable mid-gesture, and what it reveals joins
// the selection — the view rolled under a pointer that is still held down.
func TestEditorWheelDuringDrag(t *testing.T) {
	s, sh := newEditor(EditorOpts{})
	s.setContent(longDoc())
	mid := s.insetY() + s.h/2

	s.Update(sh, tea.MouseClickMsg{X: 20, Y: s.insetY(), Button: tea.MouseLeft})
	s.Update(sh, tea.MouseMotionMsg{X: 20, Y: mid, Button: tea.MouseLeft})
	before := s.selEnd.y

	s.Update(sh, tea.MouseWheelMsg{X: 20, Y: mid, Button: tea.MouseWheelDown})
	if s.scrY != editorWheelStep {
		t.Fatalf("the wheel should still browse mid-drag, scrY = %d", s.scrY)
	}
	if got, want := s.selEnd.y, before+editorWheelStep; got != want {
		t.Fatalf("the selection ended at line %d after a wheel notch, want %d", got, want)
	}
}
