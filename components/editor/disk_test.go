package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"
)

func diskWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diskPump(model tea.Model, cmd tea.Cmd) tea.Model {
	if cmd == nil {
		return model
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = diskPump(model, child)
		}
		return model
	}
	if msg == nil {
		return model
	}
	model, cmd = model.Update(msg)
	return diskPump(model, cmd)
}

func diskFixture(t *testing.T, content string) (*Screen, *core.Shared, tea.Model) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.txt")
	diskWrite(t, path, content)
	ed, sh := newEditor(Opts{Path: path, TrackDiskChanges: true})
	router := core.NewRouter(sh, []core.TabEntry{{New: func(*core.Shared) core.Screen { return ed }}})
	return ed, sh, diskPump(router, router.Init())
}

func TestDiskCheckContentAndReloadState(t *testing.T) {
	ed, _, model := diskFixture(t, strings.Repeat("line\n", 40))
	ed.Reveal(Position{Line: 25, Column: 3})
	caret, scroll := ed.CursorPosition(), ed.scrY
	before := ed.EditSeq()
	st, err := os.Stat(ed.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(ed.path, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.EditSeq() != before || ed.CursorPosition() != caret {
		t.Fatal("a content-identical touch reloaded")
	}
	diskWrite(t, ed.path, strings.Repeat("text\n", 40))
	if err := os.Chtimes(ed.path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.Text() != strings.Repeat("text\n", 40) || ed.Dirty() || ed.DiskChanged() {
		t.Fatal("same-size/timestamp content change was not cleanly reloaded")
	}
	if ed.CursorPosition() != caret || ed.scrY != scroll {
		t.Fatal("reload moved caret/viewport")
	}
	diskWrite(t, ed.path, "x")
	diskPump(model, ed.CheckDiskChanges())
	if ed.CursorPosition() != (Position{Column: 1}) || ed.scrY != 0 {
		t.Fatal("shortened reload did not clamp")
	}
}

func TestDiskCheckDirtyAndUndo(t *testing.T) {
	ed, sh, model := diskFixture(t, "original")
	ed.Update(sh, keyMsg("!"))
	diskWrite(t, ed.path, "external")
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.Text() != "!original" || ed.ChangeMark() != " (!*)" || len(ed.undoStack) == 0 {
		t.Fatal("dirty content/history was not retained")
	}
	var cmd tea.Cmd
	model, cmd = model.Update(keyMsg("ctrl+z"))
	diskPump(model, cmd)
	if ed.Text() != "external" || ed.Dirty() || ed.DiskChanged() || len(ed.undoStack) != 0 || len(ed.redoStack) != 0 {
		t.Fatal("undo to clean did not reload and clear obsolete history")
	}
}

func TestDiskCheckPendingEditAndStaleResults(t *testing.T) {
	t.Run("edit during read", func(t *testing.T) {
		ed, sh, model := diskFixture(t, "original")
		diskWrite(t, ed.path, "external")
		msg := ed.CheckDiskChanges()()
		ed.Update(sh, keyMsg("!"))
		model, cmd := model.Update(msg)
		diskPump(model, cmd)
		if ed.Text() != "!original" || !ed.DiskChanged() {
			t.Fatal("pending read overwrote an edit")
		}
	})
	for _, change := range []string{"save", "rename", "newer check", "SetText"} {
		t.Run(change, func(t *testing.T) {
			ed, _, model := diskFixture(t, "original")
			diskWrite(t, ed.path, "external")
			stale := ed.CheckDiskChanges()()
			switch change {
			case "save":
				model = diskPump(model, ed.saveCmd())
			case "rename":
				ed.SetPath(filepath.Join(t.TempDir(), "renamed.txt"))
			case "newer check":
				diskWrite(t, ed.path, "newest")
				model = diskPump(model, ed.CheckDiskChanges())
			case "SetText":
				ed.SetText("new seed")
			}
			want := ed.Text()
			model, cmd := model.Update(stale)
			diskPump(model, cmd)
			if ed.Text() != want || ed.DiskChanged() {
				t.Fatal("stale result was applied")
			}
		})
	}
}

func TestDiskMissingAndCRLF(t *testing.T) {
	ed, _, model := diskFixture(t, "one\r\ntwo\r\n")
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.DiskChanged() || ed.lineEnding != "\r\n" {
		t.Fatal("CRLF baseline differs")
	}
	diskWrite(t, ed.path, "new\r\ntext\r\n")
	model = diskPump(model, ed.CheckDiskChanges())
	model = diskPump(model, ed.saveCmd())
	b, err := os.ReadFile(ed.path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "new\r\ntext\r\n" {
		t.Fatal("reload/save changed line endings")
	}
	if err := os.Remove(ed.path); err != nil {
		t.Fatal(err)
	}
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.Text() != "new\ntext\n" || ed.ChangeMark() != " (!)" {
		t.Fatal("deletion cleared content or was not flagged")
	}
	diskWrite(t, ed.path, "recreated")
	diskPump(model, ed.CheckDiskChanges())
	if ed.Text() != "recreated" || ed.DiskChanged() {
		t.Fatal("reappearance did not recover")
	}
}

func TestDiskSaveAcknowledgementAndCancellation(t *testing.T) {
	for _, exit := range []bool{false, true} {
		name := "save"
		if exit {
			name = "save before close"
		}
		t.Run(name, func(t *testing.T) {
			ed, _, model := diskFixture(t, "original")
			model, _ = model.Update(keyMsg("!"))
			diskWrite(t, ed.path, "external")
			if exit {
				model, _ = model.Update(keyMsg("ctrl+x"))
				model, _ = model.Update(keyMsg("y"))
			} else {
				model, _ = model.Update(keyMsg("ctrl+s"))
			}
			model, cmd := model.Update(keyMsg("enter"))
			model = diskPump(model, cmd)
			if _, ok := model.(core.Router).Top().(*components.DialogScreen); !ok {
				t.Fatalf("expected warning, got %T", model.(core.Router).Top())
			}
			b, _ := os.ReadFile(ed.path)
			if string(b) != "external" {
				t.Fatal("wrote before acknowledgement")
			}
			model, _ = model.Update(keyMsg("esc"))
			if ed.ChangeMark() != " (!*)" {
				t.Fatal("cancel cleared conflict")
			}
			model, cmd = model.Update(keyMsg("enter"))
			model = diskPump(model, cmd)
			model, cmd = model.Update(keyMsg("y"))
			model = diskPump(model, cmd)
			b, _ = os.ReadFile(ed.path)
			if string(b) != "!original" || ed.Dirty() || ed.DiskChanged() {
				t.Fatal("acknowledged save did not resolve conflict")
			}
		})
	}
}

func TestDiskSaveCheckCancelledWhileReading(t *testing.T) {
	ed, _, model := diskFixture(t, "original")
	model, _ = model.Update(keyMsg("!"))
	model, _ = model.Update(keyMsg("ctrl+s"))
	model, cmd := model.Update(keyMsg("enter"))
	result := cmd()
	model, _ = model.Update(keyMsg("esc"))
	model, cmd = model.Update(result)
	diskPump(model, cmd)
	b, _ := os.ReadFile(ed.path)
	if string(b) != "original" || !ed.Dirty() {
		t.Fatal("cancelled pending save wrote")
	}
}

func TestDiskSaveSnapshotAndFailure(t *testing.T) {
	ed, sh, model := diskFixture(t, "original")
	ed.Update(sh, keyMsg("!"))
	write := ed.saveCmd()
	ed.Update(sh, keyMsg("?"))
	model = diskPump(model, write)
	model = diskPump(model, ed.CheckDiskChanges())
	if !ed.Dirty() || ed.DiskChanged() {
		t.Fatal("saved baseline used newer buffer rather than written snapshot")
	}
	// A directory at the file path gives a deterministic read/write error on all OSes.
	if err := os.Remove(ed.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ed.path, 0o755); err != nil {
		t.Fatal(err)
	}
	ed.disk.changed = true
	prior := ed.disk.baseline
	msg := ed.saveCmd()()
	model.Update(msg) // do not wait on the status-clear timer
	if !ed.DiskChanged() || !ed.Dirty() || ed.disk.baseline != prior || ed.disk.writing {
		t.Fatal("failed save changed baseline or flags")
	}
}

func TestDiskInitialMissingAndUnread(t *testing.T) {
	ed, sh := newEditor(Opts{Path: filepath.Join(t.TempDir(), "missing.txt"), TrackDiskChanges: true})
	if ed.CheckDiskChanges() != nil {
		t.Fatal("unloaded editor read eagerly")
	}
	router := core.NewRouter(sh, []core.TabEntry{{New: func(*core.Shared) core.Screen { return ed }}})
	model := diskPump(router, router.Init())
	model = diskPump(model, ed.CheckDiskChanges())
	if ed.DiskChanged() {
		t.Fatal("initially absent file flagged")
	}
	diskWrite(t, ed.path, "created externally")
	diskPump(model, ed.CheckDiskChanges())
	if ed.Text() != "created externally" {
		t.Fatal("new file did not load")
	}
}

func TestDiskReadFailurePreservesBufferAndStopsSave(t *testing.T) {
	ed, _, model := diskFixture(t, "original")
	model, _ = model.Update(keyMsg("!"))
	before := ed.Text()
	if err := os.Remove(ed.path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(ed.path, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deliver the read result but skip the status-clear timer.
	model, _ = model.Update(ed.CheckDiskChanges()())
	if ed.Text() != before || !ed.Dirty() {
		t.Fatal("read error destroyed buffer")
	}
	model, _ = model.Update(keyMsg("ctrl+s"))
	model, cmd := model.Update(keyMsg("enter"))
	model, _ = model.Update(cmd())
	if _, ok := model.(core.Router).Top().(*components.LineEditScreen); !ok {
		t.Fatal("failed pre-save check continued save flow")
	}
	if ed.disk.writing {
		t.Fatal("failed check started a write")
	}
}

func TestDiskDeletedFileSaveRequiresAcknowledgement(t *testing.T) {
	ed, _, model := diskFixture(t, "original")
	if err := os.Remove(ed.path); err != nil {
		t.Fatal(err)
	}
	model, _ = model.Update(keyMsg("ctrl+s"))
	model, cmd := model.Update(keyMsg("enter"))
	model = diskPump(model, cmd)
	if _, ok := model.(core.Router).Top().(*components.DialogScreen); !ok {
		t.Fatal("deletion did not require acknowledgement")
	}
	if _, err := os.Stat(ed.path); !os.IsNotExist(err) {
		t.Fatal("file recreated before acknowledgement")
	}
	model, cmd = model.Update(keyMsg("y"))
	diskPump(model, cmd)
	b, err := os.ReadFile(ed.path)
	if err != nil || string(b) != "original" || ed.DiskChanged() {
		t.Fatal("confirmed recreation failed")
	}
}

func TestDiskSaveAsLeavesChangedSourceAlone(t *testing.T) {
	ed, _, model := diskFixture(t, "original")
	oldPath := ed.path
	diskWrite(t, oldPath, "external")
	model = diskPump(model, ed.CheckDiskChanges())
	model, _ = model.Update(keyMsg("!"))
	model, _ = model.Update(keyMsg("ctrl+s"))
	newPath := filepath.Join(t.TempDir(), "copy.txt")
	model.(core.Router).Top().(*components.LineEditScreen).SetValue(newPath)
	model, _ = model.Update(keyMsg("enter"))
	dialog, ok := model.(core.Router).Top().(*components.DialogScreen)
	if !ok || dialog.Title != "save as" {
		t.Fatal("Save As changed its confirmation")
	}
	model, cmd := model.Update(keyMsg("y"))
	model = diskPump(model, cmd)
	if model.(core.Router).Top() != ed {
		t.Fatal("Save As added a source warning")
	}
	b, _ := os.ReadFile(oldPath)
	if string(b) != "external" {
		t.Fatal("Save As overwrote source")
	}
	b, _ = os.ReadFile(newPath)
	if string(b) != "!external" {
		t.Fatal("Save As lost buffer")
	}
	diskPump(model, ed.CheckDiskChanges())
	if ed.DiskChanged() {
		t.Fatal("Save As baseline was not updated")
	}
}
