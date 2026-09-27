package editor

import (
	"crypto/sha256"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"
)

type diskFingerprint struct {
	exists bool
	hash   [sha256.Size]byte
}

type diskState struct {
	enabled, ready, known, changed       bool
	baseline                             diskFingerprint
	generation, checkSerial, writeSerial uint64
	writing                              bool
	saveCheck                            bool
}

type diskCheckMsg struct {
	target             *Screen
	path               string
	generation, serial uint64
	content            string
	fingerprint        diskFingerprint
	err                error
	save               *diskSaveIntent
}

type diskSaveIntent struct {
	valid func() bool
	write func(int) core.Action // number of overlays to pop
}

// DiskStateChangedMsg tells hosts to refresh views and language-server content after
// a disk check has been applied. Results themselves are private, targeted broadcasts.
type DiskStateChangedMsg struct {
	Editor   *Screen
	Reloaded bool
}

// DiskChanged reports a difference from the last loaded or successfully saved file.
func (s *Screen) DiskChanged() bool { return s.disk.changed }

// ChangeMark is the shared title/list/tab suffix for local and external changes.
func (s *Screen) ChangeMark() string {
	switch {
	case s.disk.changed && s.dirty:
		return " (!*)"
	case s.disk.changed:
		return " (!)"
	case s.dirty:
		return " (*)"
	default:
		return ""
	}
}

func fingerprint(content string, exists bool) diskFingerprint {
	return diskFingerprint{exists: exists, hash: sha256.Sum256([]byte(content))}
}

func (s *Screen) recordDisk(content string, exists bool) {
	if !s.disk.enabled || s.path == "" {
		return
	}
	s.disk.baseline = fingerprint(content, exists)
	s.disk.ready, s.disk.known, s.disk.changed = true, true, false
	s.disk.generation++
}

// LoadFile synchronously seeds an editor used behind a preview, preserving the
// distinction between an empty file, a missing file, and a failed read.
func (s *Screen) LoadFile() error {
	content, err := os.ReadFile(s.path)
	s.loaded = true
	s.disk.ready = true
	if err == nil {
		s.SetText(string(content))
	} else if os.IsNotExist(err) {
		s.setContent("")
		s.recordDisk("", false)
	}
	return err
}

// CheckDiskChanges reads asynchronously. Unloaded/pathless editors and editors
// currently saving are skipped. Apply-time dirty state decides whether to reload.
func (s *Screen) CheckDiskChanges() tea.Cmd {
	if !s.disk.enabled || !s.disk.ready || s.path == "" || s.disk.writing || s.disk.saveCheck {
		return nil
	}
	return s.diskCheck(nil)
}

func (s *Screen) diskCheck(save *diskSaveIntent) tea.Cmd {
	s.disk.checkSerial++
	m := diskCheckMsg{target: s, path: s.path, generation: s.disk.generation, serial: s.disk.checkSerial, save: save}
	return func() tea.Msg {
		b, err := os.ReadFile(m.path)
		m.content, m.err = string(b), err
		m.fingerprint = fingerprint(m.content, err == nil)
		if os.IsNotExist(err) {
			m.err = nil
		}
		return core.PropagateAll(m)
	}
}

func (s *Screen) applyDiskCheck(m diskCheckMsg) core.Action {
	if m.target != s || m.serial != s.disk.checkSerial {
		return core.Action{}
	}
	s.disk.checkSerial++ // the active editor can receive a broadcast through two routes
	if m.save != nil {
		s.disk.saveCheck = false
		if !m.save.valid() {
			return core.Action{}
		}
	}
	if m.path != s.path || m.generation != s.disk.generation || s.disk.writing {
		if m.save != nil {
			return core.SetStatus("file changed during save check; retry saving")
		}
		return core.Action{}
	}
	if m.err != nil {
		return core.SetStatusAndLog("check file: " + m.path + ": " + m.err.Error())
	}
	// A failed initial load has no trustworthy baseline. Do not silently overwrite it.
	s.disk.changed = !s.disk.known || m.fingerprint != s.disk.baseline
	if m.save != nil {
		if !s.disk.changed {
			return m.save.write(1)
		}
		return core.Seq(core.PropagateAll(DiskStateChangedMsg{Editor: s}), core.Push(&components.DialogScreen{
			Title: "file changed on disk", Overlay: true, Help: components.DefaultHelpKeys,
			Render: func(*core.Shared) string { return "The source file has changed on disk. Save anyway?" },
			OnYes:  func(*core.Shared) core.Action { return m.save.write(2) },
		}))
	}
	reloaded := false
	var highlight tea.Cmd
	if s.disk.changed && !s.dirty && m.fingerprint.exists {
		caret, scrollY, scrollX := s.CursorPosition(), s.scrY, s.scrX
		s.setContent(m.content)
		s.recordDisk(m.content, true)
		s.curY = min(caret.Line, len(s.lines)-1)
		s.curX = min(caret.Column, len(s.lines[s.curY]))
		s.wantX = s.curX
		s.scrY, s.scrX = scrollY, scrollX
		s.clampScrollBounds()
		s.refreshHighlightPreview()
		highlight = s.startHighlightParse()
		reloaded = true
	}
	return core.Seq(core.Async(highlight), core.PropagateAll(DiskStateChangedMsg{Editor: s, Reloaded: reloaded}))
}
