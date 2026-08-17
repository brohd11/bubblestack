package components

import (
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
)

// Clipboard verbs for EditorScreen: the alt+c/alt+x/alt+v chords and the optional
// right-click menu behind them. With no selection they act on the cursor's line — the
// whole-line shorthand, kept for when nothing is highlighted; a selection (shifted
// motions or the mouse) is what they act on otherwise.

func copySelectionCmd(text string, cut bool) core.Action {
	return core.Async(func() tea.Msg {
		return editorCopiedMsg{n: utf8.RuneCountInString(text), err: writeEditorClipboard(text), cut: cut}
	})
}

// pasteClipboardCmd is the reading half, addressed to the editor that asked for it (see
// editorPastedMsg). clipboard.ReadAll shells out the same way the write does.
func pasteClipboardCmd(target *EditorScreen) core.Action {
	return core.Async(func() tea.Msg {
		text, err := readEditorClipboard()
		return editorPastedMsg{target: target, text: text, err: err}
	})
}

// editMenu builds the right-click menu: the three clipboard verbs, then whatever the host
// hung off EditorOpts.ContextItems below a rule. x and y are the pressed cell in ABSOLUTE
// terminal cells (absCell converts); AnchorBelow is what keeps the box off that cell.
//
// Copy and Cut are disabled without a selection, because that state is free to know. Paste
// never is: finding out whether the clipboard holds anything means READING it, which shells
// out to pbpaste/xclip and so cannot happen on the render tick. An empty clipboard is
// therefore a live row that pastes nothing.
func (s *EditorScreen) editMenu(sh *core.Shared, x, y int) *MenuScreen {
	sel := s.selectionActive()
	items := []MenuItem{
		{Label: "Copy", Disabled: !sel, Pick: func(*core.Shared) core.Action { return s.copySelection(false) }},
		{Label: "Cut", Disabled: !sel, Pick: func(*core.Shared) core.Action { return s.copySelection(true) }},
		{Label: "Paste", Pick: func(*core.Shared) core.Action {
			return core.Seq(core.Pop(), pasteClipboardCmd(s))
		}},
	}
	if s.contextItems != nil {
		if extra := s.contextItems(sh); len(extra) > 0 {
			items = append(items, MenuItem{Separator: true})
			items = append(items, extra...)
		}
	}
	return NewMenu(MenuOpts{Items: items, Anchor: AnchorBelow(x, y)})
}

// copySelection is the MENU's Copy and Cut: copyOrCut with the Pop that closes the menu
// in front of it. The rows are disabled without a selection, so the line fallback below
// never fires from here.
func (s *EditorScreen) copySelection(cut bool) core.Action {
	return core.Seq(core.Pop(), s.copyOrCut(cut))
}

// copyOrCut is both verbs for both entry points (the alt+c/alt+x chords and the menu
// rows). A cut deletes before the write completes, on purpose: undo covers a failed
// write, whereas holding the deletion until the clipboard round trip returns would race
// the buffer the user can go on editing.
//
// Without a selection the target is the whole current line, its newline included, rather
// than an inert chord. It is what an editor with the same chords does, and it keeps a
// cut+paste a line move — worth keeping now that shifted motions mean the caret is no
// longer the only thing the keyboard can offer.
func (s *EditorScreen) copyOrCut(cut bool) core.Action {
	if s.selectionActive() {
		text := s.selectedText()
		if cut {
			s.editAtomic(func() { s.deleteSelection() })
		}
		return copySelectionCmd(text, cut)
	}
	text := string(s.lines[s.curY]) + "\n"
	if cut {
		s.editAtomic(func() { s.deleteLine() })
	}
	return copySelectionCmd(text, cut)
}

// deleteLine removes the cursor's whole line, newline included, leaving the caret at the
// start of whatever slid up into its place. The last line has no newline after it to
// remove, so it takes the one BEFORE it and the caret lands at the end of the previous
// line; the only line has neither and is emptied in place, since the buffer may never
// hold zero lines.
func (s *EditorScreen) deleteLine() {
	switch {
	case s.curY+1 < len(s.lines):
		s.deleteRange(s.curY, 0, s.curY+1, 0)
		s.curX, s.wantX = 0, 0
	case s.curY > 0:
		prev := len(s.lines[s.curY-1])
		s.deleteRange(s.curY-1, prev, s.curY, len(s.lines[s.curY]))
		s.curY, s.curX, s.wantX = s.curY-1, prev, prev
	default:
		s.deleteRange(0, 0, 0, len(s.lines[0]))
		s.curX, s.wantX = 0, 0
	}
}

// editAtomic runs one buffer mutation as a single undo step from outside key(). key()'s
// own gate can't be reused — it is a deferred snapshot wrapped around a keystroke, and a
// menu's Pick never passes through it — but the rule is the same one: record the snapshot
// only if the mutation actually moved editSeq, so a no-op leaves the undo stack alone.
func (s *EditorScreen) editAtomic(mutate func()) {
	before, seq := s.snapshot(), s.editSeq
	mutate()
	if s.editSeq == seq {
		return
	}
	s.recordEdit(before)
	s.wrapDirty = true
	s.clampScroll()
}

// key routes one keystroke. After the exit prompt, a handler registered for the
// buffer's file extension gets first refusal; returning false preserves every default
// below. Editor-local keys are matched as raw strings: ctrl+x / tab / enter are this
// screen's own keys with no core.Keys binding, and the arrows
// match only the raw keycodes (not the k/j/h/l alternates core.Keys.Up et al. carry
// — those letters must stay typable). The word/line editing combos mirror
// bubbles/textinput's KeyMap verbatim (alt+←→ word jumps, alt+⌫ word delete,
// ctrl+u/k line deletes, ctrl+a/e line ends, ctrl+h/d char-delete aliases) so the
// editor behaves like the form field. shift+tab is kept as an alias for tab: it is
// what a form binds to PrevField, so the finger that reaches for it in a field
// shouldn't do nothing here. The alias only reaches this switch on a STANDALONE
// editor — in a ModularScreen pane shift+tab is a PaneNext keycode and the host
// consumes it before the panel is offered anything (see core.Keys.PaneNext).
