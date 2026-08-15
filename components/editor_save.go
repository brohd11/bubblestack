package components

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/brohd11/bubblestack/core"

	tea "github.com/charmbracelet/bubbletea"
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
// The anchor covers the bottom of just this editor: embedded, the host layout
// pushes the pane's absolute origin and the box spans the pane's width (SetPaneOrigin);
// standalone there is no pane, so the box spans the full terminal width at the
// body's bottom — the same look nano's full-width prompt has. y is one row above
// the prompt row either way (the LineEdit draws its input one row below the anchor).
func (s *EditorScreen) saveAsEdit(sh *core.Shared) *LineEditScreen {
	x, y, w, h := s.paneGeometry(sh)
	edit := NewLineEdit("file name to write", x, y+max(h-2, 0), w,
		func(_ *core.Shared, name string) core.Action {
			if strings.TrimSpace(name) == "" {
				return core.Pop()
			}
			s.applySaveName(name)
			return core.Seq(core.Pop(), core.Action{Cmd: s.saveCmd()})
		}, nil)
	if s.path != "" {
		edit.SetValue(s.path) // the full path: an unchanged enter re-saves the same file
	}
	edit.Crumb = "save as"
	return edit
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
// crumb, file-type key handler, emphasis pairing and syntax coloring follow the new
// extension. Only a registry-chosen highlighter is re-picked: one passed through
// EditorOpts was a deliberate override and a rename must not undo it. hlSeq is reset
// rather than bumped because the new highlighter has parsed nothing.
func (s *EditorScreen) applySaveName(name string) {
	s.path = name
	s.title = filepath.Base(name)
	s.crumb = s.title
	ext := strings.ToLower(filepath.Ext(name))
	s.keyHandler = lookupEditorKeyHandler(ext)
	s.emphasisPairs = emphasisPairExt(ext)
	if !s.hlExplicit {
		s.hl = lookupHighlighter(ext)
		s.hlSeq = -1
	}
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
