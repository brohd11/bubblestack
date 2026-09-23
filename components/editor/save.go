package editor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/goutil/strutil"

	tea "charm.land/bubbletea/v2"
)

// Saving for Screen: the ctrl+x prompt's save path and the save-as line edit that
// seeds it with the current name.

// ---------- save ----------

// saveAsEdit builds the filename prompt both save keys push (the exit prompt's "y" and
// ctrl+s): a floating line edit seeded with the full path, over the prompt row. Enter
// saves under the typed name (a new name is a save-as); blank or esc pops back to
// whatever raised it. saveExits, set by the caller, decides where the save leads.
//
// A name different from the buffer's goes through saveAsConfirm first, since the seeded
// path is one keystroke from moving the document. A leading "~" is expanded here so the
// buffer, title and OnSaved all get the real path; "~user" is refused. Relative names
// resolve via resolveSavePath.
//
// It spans the pane's bottom when embedded (SetPaneOrigin), or the full terminal width
// standalone, like nano's prompt.
func (s *Screen) saveAsEdit(sh *core.Shared) *components.LineEditScreen {
	x, y, w, h := s.paneGeometry(sh)
	edit := components.NewLineEdit("file name to write", x, y+max(h-2, 0), w,
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
			// Before the comparison below, so a relative spelling of the buffer's OWN file
			// re-saves in place rather than reading as a save-as onto a new name.
			path = s.resolveSavePath(path)
			if s.path != "" && path != s.path {
				return core.Push(s.saveAsConfirm(path))
			}
			highlight := s.applySaveName(path)
			return core.Seq(core.Pop(), core.Action{Cmd: tea.Batch(s.saveCmd(), highlight)})
		}, nil)
	if s.path != "" {
		edit.SetValue(s.path) // the full path: an unchanged enter re-saves the same file
	}
	return edit
}

// resolveSavePath makes a typed name absolute, resolving relative names against
// Opts.BaseDir (else the cwd): the path becomes the buffer's identity, and hosts key
// documents by path. ".." is allowed; the save box may write anywhere.
func (s *Screen) resolveSavePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	if s.baseDir != "" {
		return filepath.Clean(filepath.Join(s.baseDir, path))
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// saveAsConfirm is the y/n step before saving under a new name: the buffer moves to the
// new file and the old one keeps its last saved contents. It is pushed over the save-as
// box, so "no" returns to the typed name for correction. Yes pops both first.
func (s *Screen) saveAsConfirm(path string) *components.DialogScreen {
	target := filepath.Base(path)
	if filepath.Dir(path) != filepath.Dir(s.path) {
		target = path // another folder is a move, and the folder is the whole point of it
	}
	body := "write the buffer to " + target + "?\n\n" +
		"the buffer follows it; " + filepath.Base(s.path) + " stays on disk"
	return &components.DialogScreen{
		Title:  "save as",
		Render: func(*core.Shared) string { return body },
		OnYes: func(*core.Shared) core.Action {
			highlight := s.applySaveName(path)
			// Two levels: this confirm and the save-as box under it.
			return core.Seq(core.Pop(2), core.Action{Cmd: tea.Batch(s.saveCmd(), highlight)})
		},
		Help:    components.DefaultHelpKeys,
		Overlay: true,
	}
}

// paneGeometry is the editor's outer rectangle in absolute cells. It ignores the search
// bar, so bottom-edge overlays stay pinned to the pane.
func (s *Screen) paneGeometry(sh *core.Shared) (x, y, w, h int) {
	x, y, w, h = 0, sh.BodyY(), sh.Width(), s.paneH
	if s.hasOrigin {
		x, y, w = s.originX, s.originY, s.paneW()
	}
	return x, y, w, h
}

// paneW is the full width the pane gave SetSize — the text window plus the chrome
// around it — so the save-as box covers exactly the editor's bottom.
func (s *Screen) paneW() int {
	w := s.w + s.insetX()
	if s.bordered {
		w++ // the right border
	}
	return w
}

// applySaveName points the buffer at name after a save-as; title, crumb and language
// behavior follow. Explicit Opts overrides survive.
func (s *Screen) applySaveName(name string) tea.Cmd {
	if name == s.path {
		return nil
	}
	s.path = name
	s.title = filepath.Base(name)
	s.crumb = s.title
	s.applyLanguage(name)
	if !s.loaded {
		return nil
	}
	return s.startHighlightParse()
}

// SetPath follows a file the host renamed on disk: only the identity moves (buffer, dirty
// flag and history are kept), so the next ctrl+s does not recreate the old file. The
// loaded flag stays set, so nothing is re-read.
func (s *Screen) SetPath(path string) tea.Cmd { return s.applySaveName(path) }

// saveCmd writes a snapshot of the buffer to Path asynchronously, reporting an
// editorSavedMsg. An empty path is an error. Missing parent directories are created.
func (s *Screen) saveCmd() tea.Cmd {
	path := s.path
	revision := s.revision
	content := s.Text()
	if s.lineEnding == "\r\n" {
		content = strings.ReplaceAll(content, "\n", "\r\n")
	}
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
