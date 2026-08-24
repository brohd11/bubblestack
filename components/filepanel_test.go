package components

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// fileTree builds a fixture: root/{sub/{deep.txt}, alpha.txt, Beta.md, .hidden}.
func fileTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"alpha.txt", "Beta.md", ".hidden"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "deep.txt"), []byte("xx"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// titles is the panel's rows as the user reads them.
func titles(p *FilePanel) []string {
	var out []string
	for _, it := range p.List().VisibleItems() {
		switch v := it.(type) {
		case fileItem:
			out = append(out, v.Title())
		case Item:
			out = append(out, v.Title())
		case CompactItem:
			out = append(out, v.Title())
		}
	}
	return out
}

func selectTitle(t *testing.T, p *FilePanel, title string) {
	t.Helper()
	for i, name := range titles(p) {
		if name == title {
			p.List().Select(i)
			return
		}
	}
	t.Fatalf("no row %q in %v", title, titles(p))
}

// TestFilePanelListsDirectory: directories first, then files, case-insensitively by name,
// and nothing else — no ".." at the root, which is where the panel starts.
func TestFilePanelListsDirectory(t *testing.T) {
	root := fileTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 12)

	want := []string{"sub/", ".hidden", "alpha.txt", "Beta.md"}
	if got := titles(p); !equalStrings(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if p.Dir() != root {
		t.Fatalf("Dir() = %q, want %q", p.Dir(), root)
	}
}

// TestFilePanelEnterAndUp: enter walks into a folder, and both ways back out land on the
// folder just left rather than resetting the cursor to the top.
func TestFilePanelEnterAndUp(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 12)

	selectTitle(t, p, "sub/")
	p.UpdatePanel(sh, keyMsg("enter"))
	if p.Dir() != filepath.Join(root, "sub") {
		t.Fatalf("enter on a folder should walk into it; Dir() = %q", p.Dir())
	}
	if got := titles(p); !equalStrings(got, []string{"..", "deep.txt"}) {
		t.Fatalf("rows inside sub = %v, want [.. deep.txt]", got)
	}

	// Out through the ".." row.
	p.List().Select(0)
	p.UpdatePanel(sh, keyMsg("enter"))
	if p.Dir() != root {
		t.Fatalf("the .. row should walk to the parent; Dir() = %q", p.Dir())
	}
	if sel, _ := p.Selected(); sel.Name != "sub" {
		t.Fatalf("walking up should select the folder just left, got %q", sel.Name)
	}

	// And again through the key.
	selectTitle(t, p, "sub/")
	p.UpdatePanel(sh, keyMsg("enter"))
	if _, handled := p.UpdatePanel(sh, keyMsg("backspace")); !handled {
		t.Fatal("backspace should be claimed while there is a parent to go to")
	}
	if p.Dir() != root {
		t.Fatalf("backspace should walk to the parent; Dir() = %q", p.Dir())
	}
}

// TestFilePanelRootClamp: at Root there is no way further up — no row, and backspace is
// left to the host, where it still means Back.
func TestFilePanelRootClamp(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 12)

	for _, name := range titles(p) {
		if name == ".." {
			t.Fatal("the root of the panel must not offer a way above it")
		}
	}
	if _, handled := p.UpdatePanel(sh, keyMsg("backspace")); handled {
		t.Fatal("backspace at the root must fall through to the host's Back")
	}
	// A programmatic jump above the floor lands on the floor, not outside it.
	p.SetDir(sh, filepath.Dir(root))
	if p.Dir() != root {
		t.Fatalf("SetDir above Root should clamp to it; Dir() = %q", p.Dir())
	}
}

// TestFilePanelUnclamped: without a Root the panel walks all the way out.
func TestFilePanelUnclamped(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{Dir: root, Compact: true})
	p.SetSize(30, 12)

	if _, handled := p.UpdatePanel(sh, keyMsg("backspace")); !handled {
		t.Fatal("an unclamped panel should walk above its starting directory")
	}
	if p.Dir() != filepath.Dir(root) {
		t.Fatalf("Dir() = %q, want %q", p.Dir(), filepath.Dir(root))
	}
}

// TestFilePanelInclude: the hook decides what is listed, files and directories alike — the
// seam gote's DocFilter.Match plugs into.
func TestFilePanelInclude(t *testing.T) {
	root := fileTree(t)
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Root: root, Compact: true,
		Include: func(path string, d fs.DirEntry) bool {
			if strings.HasPrefix(d.Name(), ".") {
				return false
			}
			return d.IsDir() || filepath.Ext(d.Name()) == ".md"
		},
	})
	p.SetSize(30, 12)

	if got := titles(p); !equalStrings(got, []string{"sub/", "Beta.md"}) {
		t.Fatalf("rows = %v, want [sub/ Beta.md]", got)
	}
}

// TestFilePanelHostRows: Rows are pinned above "..", rebuilt per directory, and routed to
// OnRow when picked.
func TestFilePanelHostRows(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	var picked list.Item
	var seenDir string
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Root: root, Compact: true,
		Rows: func(dir string) []list.Item {
			seenDir = dir
			return []list.Item{CompactItem{Name: "+ new file"}}
		},
		OnRow: func(_ *core.Shared, it list.Item) core.Action { picked = it; return core.Action{} },
	})
	p.SetSize(30, 12)

	if got := titles(p); got[0] != "+ new file" {
		t.Fatalf("the host's row should come first, got %v", got)
	}
	p.List().Select(0)
	p.UpdatePanel(sh, keyMsg("enter"))
	if picked == nil {
		t.Fatal("picking a host row should reach OnRow")
	}

	selectTitle(t, p, "sub/")
	p.UpdatePanel(sh, keyMsg("enter"))
	if seenDir != filepath.Join(root, "sub") {
		t.Fatalf("Rows should be rebuilt for the new directory, saw %q", seenDir)
	}
}

// TestFilePanelUpRowLeavesFilter: ".." is a way out of the folder, not an answer to a
// question about which files you want.
func TestFilePanelUpRowLeavesFilter(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{Dir: root, Compact: true})
	p.SetSize(30, 12)

	p.List().SetFilterText("e")
	for _, name := range titles(p) {
		if name == ".." {
			t.Fatal("the .. row must not match a filter query")
		}
	}
	p.List().ResetFilter()
	_ = sh
}

// TestFilePanelDensityToggle: the flip is a rebuild, so the things a rebuild could drop —
// the directory, the cursor, the allocation, focus — are the things asserted here.
func TestFilePanelDensityToggle(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Root: root, Compact: true,
		DensityKey: key.NewBinding(key.WithKeys("v")),
	})
	p.SetSize(30, 12)
	p.Focus()
	selectTitle(t, p, "alpha.txt")
	idx := p.List().Index()

	compactRow, ok := p.RowY(idx)
	if !ok {
		t.Fatal("the selected row should be on-page")
	}
	if _, handled := p.UpdatePanel(sh, keyMsg("v")); !handled {
		t.Fatal("DensityKey should be claimed by the panel")
	}
	if p.Compact() {
		t.Fatal("the density key should have flipped the density")
	}
	if p.Dir() != root {
		t.Fatalf("the flip must not move the panel; Dir() = %q", p.Dir())
	}
	if got := p.List().Index(); got != idx {
		t.Fatalf("cursor = %d after the flip, want %d", got, idx)
	}
	if !p.Focused() {
		t.Fatal("the flip must not drop focus")
	}
	if w, h := p.List().Width(), p.List().Height(); w != 30 || h != 12 {
		t.Fatalf("the rebuilt list is %dx%d, want the panel's 30x12", w, h)
	}
	standardRow, ok := p.RowY(idx)
	if !ok {
		t.Fatal("the selected row should still be on-page at the standard density")
	}
	if standardRow <= compactRow {
		t.Fatalf("standard rows are taller than compact ones: RowY %d vs %d", standardRow, compactRow)
	}
	if _, handled := p.UpdatePanel(sh, keyMsg("v")); !handled || !p.Compact() {
		t.Fatal("the density key should flip back")
	}
}

// TestFilePanelClickSelects: a left click picks the row under the cursor at both
// densities, using RowY as the geometry both the click math and an overlay anchor read.
func TestFilePanelClickSelects(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	for _, compact := range []bool{true, false} {
		var opened string
		p := NewFilePanel(FilePanelOpts{
			Dir: root, Root: root, Compact: compact,
			OnSelect: func(_ *core.Shared, e FileEntry) core.Action { opened = e.Name; return core.Action{} },
		})
		p.SetSize(30, 20)
		p.Focus()

		idx := 2 // "alpha.txt": sub/, .hidden, alpha.txt
		y, ok := p.RowY(idx)
		if !ok {
			t.Fatalf("compact=%v: row %d should be on-page", compact, idx)
		}
		p.UpdatePanel(sh, tea.MouseMsg{
			Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, Y: y,
		})
		if opened != "alpha.txt" {
			t.Fatalf("compact=%v: click at row %d opened %q, want alpha.txt", compact, y, opened)
		}
	}
}

// TestFilePanelUnreadableDir: a directory that cannot be read leaves the panel where it
// was and reports through OnError.
func TestFilePanelUnreadableDir(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	var gotErr error
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Root: root, Compact: true,
		OnError: func(_ *core.Shared, err error) core.Action { gotErr = err; return core.Action{} },
	})
	p.SetSize(30, 12)
	before := titles(p)

	p.SetDir(sh, filepath.Join(root, "sub", "missing"))
	if gotErr == nil {
		t.Fatal("an unreadable directory should reach OnError")
	}
	if p.Dir() != root {
		t.Fatalf("the panel should stay put; Dir() = %q", p.Dir())
	}
	if got := titles(p); !equalStrings(got, before) {
		t.Fatalf("rows changed on a failed navigation: %v", got)
	}
}

// TestFilePanelRowKeys: OnKey sees the entry under the cursor, and never the ".." row —
// there is no file there to act on.
func TestFilePanelRowKeys(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	var acted string
	p := NewFilePanel(FilePanelOpts{
		Dir: filepath.Join(root, "sub"), Root: root, Compact: true,
		OnKey: func(_ *core.Shared, k string, e FileEntry) (core.Action, bool) {
			if k == "d" {
				acted = e.Name
				return core.Action{}, true
			}
			return core.Action{}, false
		},
	})
	p.SetSize(30, 12)

	p.List().Select(0) // ".."
	p.UpdatePanel(sh, keyMsg("d"))
	if acted != "" {
		t.Fatalf("OnKey should not fire on the .. row, got %q", acted)
	}
	selectTitle(t, p, "deep.txt")
	p.UpdatePanel(sh, keyMsg("d"))
	if acted != "deep.txt" {
		t.Fatalf("OnKey saw %q, want deep.txt", acted)
	}
}

// TestFilePanelRefreshKeepsCursor: a host reseed re-reads without moving the user.
func TestFilePanelRefreshKeepsCursor(t *testing.T) {
	root := fileTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 12)
	selectTitle(t, p, "alpha.txt")
	idx := p.List().Index()

	if err := os.WriteFile(filepath.Join(root, "gamma.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.Refresh()
	if got := titles(p); !contains(got, "gamma.txt") {
		t.Fatalf("Refresh should pick up a new file, rows = %v", got)
	}
	if got := p.List().Index(); got != idx {
		t.Fatalf("cursor = %d after Refresh, want %d", got, idx)
	}
}

// TestFilePanelBorderLegendTracksDir: the frame says which folder you are in, and keeps
// saying it after a walk.
func TestFilePanelBorderLegendTracksDir(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true, Border: true})
	p.SetSize(30, 12)

	if !strings.Contains(p.View(false), filepath.Base(root)) {
		t.Fatal("the legend should name the starting directory")
	}
	selectTitle(t, p, "sub/")
	p.UpdatePanel(sh, keyMsg("enter"))
	if !strings.Contains(p.View(false), "sub") {
		t.Fatal("the legend should follow the walk")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func contains(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}
