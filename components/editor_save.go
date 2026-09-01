package components

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/goutil/strutil"

	tea "charm.land/bubbletea/v2"
)

// Saving for EditorScreen: the ctrl+x prompt's save path and the save-as line edit that
// seeds it with the current name.

// ---------- save ----------

// saveAsEdit builds the filename prompt both save keys push — the exit prompt's "y"
// and ctrl+s — a floating line edit seeded with the buffer's full path (an unchanged
// enter re-saves the same file), its input row covering the y/n/c prompt row — nano's
// "File Name to Write". Enter saves under the typed name (a different name is a
// save-as: the buffer takes the new path and title); a blank entry or esc pops back to
// whatever raised it, so from the exit prompt that prompt is still up and from ctrl+s
// nothing happened. A relative name resolves against the process CWD, nano's rule.
// saveExits, set by the caller before the push, is what decides where the write lands.
//
// A name that DIFFERS from the buffer's own goes through saveAsConfirm first. The box is
// seeded with the full path, so it is one stray keystroke away from silently moving the
// document — the buffer, its title, its crumb and the host's idea of which file it is
// all follow the new name. A first save (no path yet) is not that, and prompts for
// nothing.
//
// A leading "~" is resolved here, the one place the user's typed text enters, so the
// buffer takes the RESOLVED path: unexpanded it would reach os.WriteFile as a literal
// directory named "~" under the CWD, and the title, the prefill of the next ctrl+s and
// the path handed to OnSaved would all name a file that isn't where the user asked for
// it. A "~user" form is refused rather than guessed (strutil.ExpandHome's rule) and the
// write is abandoned — writing it literally is the very surprise this resolves.
//
// The anchor covers the bottom of just this editor: embedded, the host layout
// pushes the pane's absolute origin and the box spans the pane's width (SetPaneOrigin);
// standalone there is no pane, so the box spans the full terminal width at the
// body's bottom — the same look nano's full-width prompt has. y is one row above
// the prompt row either way (the LineEdit draws its input one row below the anchor).
func (s *EditorScreen) saveAsEdit(sh *core.Shared) *LineEditScreen {
	x, y, w, h := s.paneGeometry(sh)
	edit := NewLineEdit("file name to write", x, y+max(h-2, 0), w,
		func(_ *core.Shared, name string) core.Action {
			name = strings.TrimSpace(name)
			if name == "" {
				return core.Pop()
			}
			path, err := strutil.ExpandHome(name)
			if err != nil {
				// The same wording editorSavedMsg reports a failed write with.
				return core.Seq(core.Pop(), core.SetStatus("save failed: "+err.Error()))
			}
			if s.path != "" && path != s.path {
				return core.Push(s.saveAsConfirm(path))
			}
			s.applySaveName(path)
			return core.Seq(core.Pop(), core.Action{Cmd: s.saveCmd()})
		}, nil)
	if s.path != "" {
		edit.SetValue(s.path) // the full path: an unchanged enter re-saves the same file
	}
	return edit
}

// saveAsConfirm is the y/n step between a NEW name in the save-as box and the write.
// This is a save-as, not a rename: the write creates the new file and the BUFFER moves to
// it, while the old file stays on disk holding whatever was last saved to it. Both halves
// are worth stating before the y — the second is the one that surprises, since the
// document appears to have moved and a copy is left behind.
//
// It is pushed OVER the save-as box rather than replacing it so cancelling returns to the
// name still typed there: the reason to say no is usually a typo, and a confirm that
// threw the name away would make correcting one mean retyping the whole path. Both
// overlays composite (Router.overlayBase walks the whole chain), so the box stays visible
// under the modal. Yes pops both levels before the write.
func (s *EditorScreen) saveAsConfirm(path string) *DialogScreen {
	target := filepath.Base(path)
	if filepath.Dir(path) != filepath.Dir(s.path) {
		target = path // another folder is a move, and the folder is the whole point of it
	}
	body := "write the buffer to " + target + "?\n\n" +
		"the buffer follows it; " + filepath.Base(s.path) + " stays on disk"
	return &DialogScreen{
		Title:  "save as",
		Render: func(*core.Shared) string { return body },
		OnYes: func(*core.Shared) core.Action {
			s.applySaveName(path)
			// Two levels: this confirm and the save-as box under it.
			return core.Seq(core.Pop(2), core.Action{Cmd: s.saveCmd()})
		},
		Help:    DefaultHelpKeys,
		Overlay: true,
	}
}

// paneGeometry is the editor's assigned outer rectangle in absolute terminal cells.
// Unlike s.h it does not shrink when a search bar is visible, so every bottom-edge
// overlay stays pinned to the pane rather than drifting up with the text viewport.
func (s *EditorScreen) paneGeometry(sh *core.Shared) (x, y, w, h int) {
	x, y, w, h = 0, sh.BodyY(), sh.Width(), s.paneH
	if s.hasOrigin {
		x, y, w = s.originX, s.originY, s.paneW()
	}
	return x, y, w, h
}

// paneW is the full width the pane gave SetSize — the text window plus the chrome
// around it — so the save-as box covers exactly the editor's bottom.
func (s *EditorScreen) paneW() int {
	w := s.w + s.insetX()
	if s.bordered {
		w++ // the right border
	}
	return w
}

// applySaveName points the buffer at name: a save-as renames it, so the title bar,
// crumb and host-resolved editing behavior follow the new identity. An explicit
// highlighter or indent width passed through EditorOpts is a deliberate override and a
// rename must not undo it.
func (s *EditorScreen) applySaveName(name string) {
	s.path = name
	s.title = filepath.Base(name)
	s.crumb = s.title
	s.applyLanguage(name)
}

// SetPath points the buffer at path after the file moved underneath it — the host
// renamed it on disk, as opposed to the save-as applySaveName otherwise serves. The
// buffer, its dirty flag and its undo history are untouched: only the identity moves,
// which is exactly what keeps the next ctrl+s from re-creating the old file. The load
// flag is deliberately left set — a rename does not make the new path worth reading, and
// re-reading it on a later pane swap would undo everything this method promises.
func (s *EditorScreen) SetPath(path string) { s.applySaveName(path) }

// saveCmd snapshots the buffer and its revision and writes it to Path asynchronously
// (IO in the cmd lane); the result arrives as an editorSavedMsg. An empty path is an error — a
// scratch buffer has nowhere to save to. Parent directories are created: a save-as
// names a LOCATION, and refusing one because a folder in it doesn't exist yet would
// make the box ask for something it won't accept.
func (s *EditorScreen) saveCmd() tea.Cmd {
	path := s.path
	revision := s.revision
	var b strings.Builder
	for i, l := range s.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	content := b.String()
	return func() tea.Msg {
		if path == "" {
			return editorSavedMsg{err: errors.New("no file path"), revision: revision}
		}
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return editorSavedMsg{err: err, revision: revision}
			}
		}
		return editorSavedMsg{err: os.WriteFile(path, []byte(content), 0o644), revision: revision}
	}
}
