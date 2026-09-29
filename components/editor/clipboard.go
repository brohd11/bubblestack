package editor

import (
	"unicode/utf8"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
)

// Clipboard verbs for Screen: the ctrl+c/x/v chords and the optional right-click menu.
// With no selection they act on the caret's line.

// copySelectionCmd writes the clipboard off the UI thread: atotto shells out to
// pbcopy/xclip, which must not run inside Update.
func copySelectionCmd(text string, cut bool) core.Action {
	return core.Async(func() tea.Msg {
		return editorCopiedMsg{n: utf8.RuneCountInString(text), err: writeEditorClipboard(text), cut: cut}
	})
}

// pasteClipboardCmd is the reading half, addressed to the editor that asked for it (see
// editorPastedMsg). The platform clipboard read stays off the update/render path.
func pasteClipboardCmd(target *Screen) core.Action {
	return core.Async(func() tea.Msg {
		text, err := readEditorClipboard()
		return editorPastedMsg{target: target, text: text, err: err}
	})
}

// editMenu builds the right-click menu: the clipboard verbs, then Opts.ContextItems below
// a rule. x, y are absolute cells. Copy and Cut are disabled without a selection; Paste is
// always enabled, since checking the clipboard needs IO.
func (s *Screen) editMenu(sh *core.Shared, x, y int) *components.MenuScreen {
	sel := s.selectionActive()
	items := []components.MenuItem{
		{Label: "Copy", Disabled: !sel, Pick: func(*core.Shared) core.Action { return s.copySelection(false) }},
		{Label: "Cut", Disabled: !sel, Pick: func(*core.Shared) core.Action { return s.copySelection(true) }},
		{Label: "Paste", Pick: func(*core.Shared) core.Action {
			return core.Seq(core.Pop(), pasteClipboardCmd(s))
		}},
	}
	if s.contextItems != nil {
		if extra := s.contextItems(sh); len(extra) > 0 {
			items = append(items, components.MenuItem{Separator: true})
			items = append(items, extra...)
		}
	}
	return components.NewMenu(components.MenuOpts{Items: items, Anchor: components.AnchorBelow(x, y)})
}

// copySelection is the menu's Copy and Cut: copyOrCut after popping the menu.
func (s *Screen) copySelection(cut bool) core.Action {
	return core.Seq(core.Pop(), s.copyOrCut(cut))
}

// copyOrCut backs both verbs from chords and menu. A cut deletes before the write
// completes (undo covers a failed write). Without a selection it takes the whole line,
// newline included, so cut and paste moves a line.
func (s *Screen) copyOrCut(cut bool) core.Action {
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

// deleteLine removes the caret's line and newline (the preceding newline for the last
// line; the only line is emptied, since the buffer is never empty).
func (s *Screen) deleteLine() {
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

// editAtomic runs one mutation as a single undo step from outside key() (a menu Pick
// never passes through it). A no-op leaves the undo stack alone.
func (s *Screen) editAtomic(mutate func()) {
	entry := s.beginHistory()
	mutate()
	if !s.finishHistory(entry) {
		return
	}
	s.wrapDirty = true
	s.clampScroll()
}
