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
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// TestFilePanelRowAnchor: the anchor opens below the whole item and flips clear above it,
// and it tracks the density — the two numbers a caller must not hold a copy of.
func TestFilePanelRowAnchor(t *testing.T) {
	root := fileTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 20)

	const originY = 4
	idx := 1
	row, ok := p.RowY(idx)
	if !ok {
		t.Fatal("row should be on-page")
	}
	a, ok := p.RowAnchor(idx, 0, originY)
	if !ok {
		t.Fatal("RowAnchor should agree with RowY about the page")
	}
	top := originY + row
	if a.FlipY != top {
		t.Fatalf("FlipY = %d, want the item's own top row %d", a.FlipY, top)
	}
	if a.Y != top+1 {
		t.Fatalf("compact: Y = %d, want the row below a 1-row item (%d)", a.Y, top+1)
	}

	p.SetCompact(false)
	row, ok = p.RowY(idx)
	if !ok {
		t.Fatal("row should still be on-page at the standard density")
	}
	a, ok = p.RowAnchor(idx, 0, originY)
	if !ok {
		t.Fatal("RowAnchor should agree with RowY at the standard density too")
	}
	if want := originY + row + 3; a.Y != want {
		t.Fatalf("standard: Y = %d, want the row below a 3-row item (%d)", a.Y, want)
	}

	if _, ok := p.RowAnchor(999, 0, originY); ok {
		t.Fatal("an off-page index should report false, as RowY does")
	}
}

// ---------- type coloring ----------

// colorTree builds a fixture with one entry of every kind the classifier names.
func colorTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, d := range []string{"sub", ".git"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]fs.FileMode{
		"main.go":  0o644,
		"notes.md": 0o644,
		"logo.png": 0o644,
		"src.zip":  0o644,
		"run.sh":   0o755,
		"LICENSE":  0o644,
		".profile": 0o644,
	}
	for name, mode := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return root
}

// TestClassifyFile: every branch of the precedence order, over a real directory read the
// way FilePanel reads one.
func TestClassifyFile(t *testing.T) {
	root := colorTree(t)
	des, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FileKind{
		"sub":      KindDir,
		".git":     KindHiddenDir,
		".profile": KindHiddenFile,
		"link":     KindSymlink,
		"run.sh":   KindExec, // the exec bit outranks the .sh suffix
		"main.go":  KindCode,
		"notes.md": KindDoc,
		"logo.png": KindImage,
		"src.zip":  KindArchive,
		"LICENSE":  KindFile, // no extension, no bit: an ordinary file
	}
	for _, d := range des {
		var info fs.FileInfo
		if !d.IsDir() {
			info, _ = d.Info()
		}
		if got := ClassifyFile(d, info); got != want[d.Name()] {
			t.Errorf("%s: kind %d, want %d", d.Name(), got, want[d.Name()])
		}
	}
}

// TestClassifyFileNilInfo: a stat that failed loses the exec distinction and falls through
// to the tables rather than erroring — run.sh reads as code, not as a program.
func TestClassifyFileNilInfo(t *testing.T) {
	root := colorTree(t)
	des, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range des {
		if d.Name() != "run.sh" {
			continue
		}
		if got := ClassifyFile(d, nil); got != KindCode {
			t.Fatalf("run.sh with no stat: kind %d, want %d", got, KindCode)
		}
		return
	}
	t.Fatal("fixture lost run.sh")
}

// TestFilePanelRowColors: the panel classifies at read time, and the ".." row — which has
// no fs.DirEntry behind it — is colored as the folder it is.
func TestFilePanelRowColors(t *testing.T) {
	root := colorTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: filepath.Join(root, "sub"), Compact: true, Colors: true})
	p.SetSize(30, 12)

	got := map[string]lipgloss.TerminalColor{}
	for _, it := range p.List().VisibleItems() {
		fi, ok := it.(fileItem)
		if !ok {
			continue
		}
		got[fi.Title()] = fi.TitleColor()
	}
	if got[".."] != FileKindColor(KindDir) {
		t.Errorf("the parent row should be dir-colored, got %v", got[".."])
	}

	p = NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true, Colors: true})
	p.SetSize(30, 12)
	want := map[string]FileKind{
		"sub/":     KindDir,
		".git/":    KindHiddenDir,
		".profile": KindHiddenFile,
		"run.sh":   KindExec,
		"main.go":  KindCode,
		"notes.md": KindDoc,
		"LICENSE":  KindFile,
	}
	for _, it := range p.List().VisibleItems() {
		fi, ok := it.(fileItem)
		if !ok {
			continue
		}
		k, listed := want[fi.Title()]
		if !listed {
			continue
		}
		if fi.TitleColor() != FileKindColor(k) {
			t.Errorf("%s: color %v, want %v (kind %d)", fi.Title(), fi.TitleColor(), FileKindColor(k), k)
		}
	}
}

// TestFilePanelColorIsStyleOnly: the whole point of coloring through the delegate's style
// rather than through Title() — the panel prints color, and stripping it back off leaves
// exactly the text an uncolored terminal draws. A color that moved a cell would show up
// here as a mismatch, whatever the frame, the truncation or the marquee did with it.
func TestFilePanelColorIsStyleOnly(t *testing.T) {
	root := colorTree(t)
	build := func() string {
		p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true, Border: true, Colors: true})
		p.SetSize(24, 12) // narrow enough that names truncate
		return p.View(false)
	}
	plain := build() // no TTY under `go test`: the Ascii profile drops every color
	withColor(t)
	colored := build()

	if colored == plain {
		t.Fatal("a listing of dirs, dotfiles and source files should print some color")
	}
	if ansi.Strip(colored) != plain {
		t.Fatalf("color must not change what the panel draws:\n%q\n%q", ansi.Strip(colored), plain)
	}
}

// TestFilePanelColorsAreOptIn is the flag's contract, and the mirror of
// TestFilePanelColorIsStyleOnly: a panel built WITHOUT Colors has no opinion about any row
// — nil from every TitleColor, the synthetic ".." row included — and prints the same bytes
// whether or not the terminal can show color. It is what would catch an edit that went back
// to coloring unconditionally, which no other test in this file would notice.
func TestFilePanelColorsAreOptIn(t *testing.T) {
	root := colorTree(t)
	build := func() *FilePanel {
		p := NewFilePanel(FilePanelOpts{Dir: root, Compact: true, Border: true})
		p.SetSize(24, 12)
		return p
	}

	for _, it := range build().List().VisibleItems() {
		fi, ok := it.(fileItem)
		if !ok {
			continue
		}
		if c := fi.TitleColor(); c != nil {
			t.Errorf("%s: color %v without Colors, want nil", fi.Title(), c)
		}
	}

	// And the flag really does reach the render. The comparison is against an opted-IN
	// panel over the same directory rather than against an uncolored render of this one:
	// the frame, the cursor accent and the muted size column are color too, so "prints no
	// escape codes" was never the claim — "prints no file-type color" is.
	withColor(t)
	off := build().View(false)
	on := func() string {
		p := NewFilePanel(FilePanelOpts{Dir: root, Compact: true, Border: true, Colors: true})
		p.SetSize(24, 12)
		return p.View(false)
	}()
	if off == on {
		t.Fatal("Colors must change what the panel prints; the flag is not reaching the render")
	}
	if ansi.Strip(off) != ansi.Strip(on) {
		t.Fatalf("the flag must change only color, never text:\n%q\n%q", ansi.Strip(off), ansi.Strip(on))
	}
}

// ---------- symlinks ----------

// linkTree builds root/{target/{inside.txt}, plain.txt, dirlink -> target,
// filelink -> plain.txt, deadlink -> nowhere}. Skips on a platform that cannot symlink.
func linkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "target"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		"plain.txt":         "hello\n",
		"target/inside.txt": "in\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	links := map[string]string{
		"dirlink":  filepath.Join(root, "target"),
		"filelink": filepath.Join(root, "plain.txt"),
		"deadlink": filepath.Join(root, "nowhere"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	return root
}

// rowDesc is the description of the row titled name, for the size-vs-"dir" assertions.
func rowDesc(p *FilePanel, name string) (string, bool) {
	for _, it := range p.List().VisibleItems() {
		if fi, ok := it.(fileItem); ok && fi.Title() == name {
			return fi.Description(), true
		}
	}
	return "", false
}

// TestFilePanelSymlinkToDirIsADirectory: os.ReadDir does not follow links, so a symlink to a
// folder used to arrive with IsDir false — no trailing slash, sorted among the files, and
// described by the link's own byte length. read follows it now, so all three answer for the
// target.
func TestFilePanelSymlinkToDirIsADirectory(t *testing.T) {
	root := linkTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true})
	p.SetSize(30, 12)

	got := titles(p)
	// Directories first, each with a slash: the linked one sorts and prints as a folder.
	want := []string{"dirlink/", "target/", "deadlink", "filelink", "plain.txt"}
	if !equalStrings(got, want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	if desc, ok := rowDesc(p, "dirlink/"); !ok || desc != "dir" {
		t.Fatalf("a linked folder should describe itself as a dir, got %q", desc)
	}
}

// TestFilePanelWalksIntoSymlinkedDir is the assertion the bug report reduces to: enter on a
// linked folder must reach SetDir, not OnSelect. Before the fix pick saw IsDir false and
// handed the row to the host as a FILE, which is why ~/.gdaddon could not be opened.
func TestFilePanelWalksIntoSymlinkedDir(t *testing.T) {
	root := linkTree(t)
	sh := core.NewShared(nil)
	picked := ""
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Compact: true,
		OnSelect: func(_ *core.Shared, e FileEntry) core.Action { picked = e.Name; return core.Action{} },
	})
	p.SetSize(30, 12)

	selectTitle(t, p, "dirlink/")
	p.UpdatePanel(sh, keyMsg("enter"))
	if picked != "" {
		t.Fatalf("a linked folder must walk, not go to OnSelect (got %q)", picked)
	}
	if want := filepath.Join(root, "dirlink"); p.Dir() != want {
		t.Fatalf("Dir() = %q, want the LINK's path %q — walking must not resolve to the target, or \"..\" would leave the folder you were looking at", p.Dir(), want)
	}
	if got := titles(p); !equalStrings(got, []string{"..", "inside.txt"}) {
		t.Fatalf("the listing should be the target's contents, got %v", got)
	}

	// Out again: the parent of the link's path, which is where the link was seen.
	p.UpdatePanel(sh, keyMsg("backspace"))
	if p.Dir() != root {
		t.Fatalf("walking out of a linked folder should land where the link was; Dir() = %q", p.Dir())
	}
}

// TestFilePanelSymlinkToFileStaysAFile: only the DIRECTORY case changes. A link to a file,
// and a dangling link, both stay files — they go to OnSelect and never walk.
func TestFilePanelSymlinkToFileStaysAFile(t *testing.T) {
	root := linkTree(t)
	sh := core.NewShared(nil)
	var picked []string
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Compact: true,
		OnSelect: func(_ *core.Shared, e FileEntry) core.Action { picked = append(picked, e.Name); return core.Action{} },
	})
	p.SetSize(30, 12)

	for _, name := range []string{"filelink", "deadlink"} {
		selectTitle(t, p, name)
		p.UpdatePanel(sh, keyMsg("enter"))
		if p.Dir() != root {
			t.Fatalf("%s must not be walked into; Dir() = %q", name, p.Dir())
		}
	}
	if !equalStrings(picked, []string{"filelink", "deadlink"}) {
		t.Fatalf("both should have reached OnSelect, got %v", picked)
	}
}

// TestFilePanelSymlinkedDirStaysSymlinkColored: resolving IsDir must not make a linked folder
// look like an ordinary one. The color keys off the LINK's own type, which is the only thing
// left on the row saying an indirection is involved.
func TestFilePanelSymlinkedDirStaysSymlinkColored(t *testing.T) {
	root := linkTree(t)
	p := NewFilePanel(FilePanelOpts{Dir: root, Root: root, Compact: true, Colors: true})
	p.SetSize(30, 12)

	for _, it := range p.List().VisibleItems() {
		fi, ok := it.(fileItem)
		if !ok {
			continue
		}
		switch fi.Title() {
		case "dirlink/":
			if fi.TitleColor() != FileKindColor(KindSymlink) {
				t.Errorf("a linked folder should stay symlink-colored, got %v", fi.TitleColor())
			}
		case "target/":
			if fi.TitleColor() != FileKindColor(KindDir) {
				t.Errorf("a real folder should stay dir-colored, got %v", fi.TitleColor())
			}
		}
	}
}

// ---------- mouse buttons ----------

// rowPress is a mouse press on panel-relative row y with the given button.
func rowPress(y int, button tea.MouseButton) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: 5, Y: y}
}

// clickRow presses button over the row titled name, which must be on-page.
func clickRow(t *testing.T, p *FilePanel, sh *core.Shared, name string, button tea.MouseButton) {
	t.Helper()
	for i, title := range titles(p) {
		if title != name {
			continue
		}
		y, ok := p.RowY(i)
		if !ok {
			t.Fatalf("row %q should be on-page", name)
		}
		p.UpdatePanel(sh, rowPress(y, button))
		return
	}
	t.Fatalf("no row %q in %v", name, titles(p))
}

// mousePanel is a focused FilePanel over fileTree with recording hooks. openDir claims the
// directory the way a host raising a menu does, so a test can see whether a gesture reached
// it or walked past it.
func mousePanel(t *testing.T, root string, opened, menued *string) (*FilePanel, *core.Shared) {
	t.Helper()
	p := NewFilePanel(FilePanelOpts{
		Dir: root, Compact: true,
		OnSelect: func(_ *core.Shared, e FileEntry) core.Action { *opened = e.Name; return core.Action{} },
		OnOpenDir: func(_ *core.Shared, e FileEntry) (core.Action, bool) {
			*menued = e.Name
			return core.Action{}, true
		},
	})
	p.SetSize(30, 12)
	p.Focus()
	return p, core.NewShared(nil)
}

// TestLeftClickWalksIntoDirectory is the fix: a left click is the mouse's "d", so it walks
// past OnOpenDir rather than raising the host's menu on the commonest gesture there is.
func TestLeftClickWalksIntoDirectory(t *testing.T) {
	root := fileTree(t)
	var opened, menued string
	p, sh := mousePanel(t, root, &opened, &menued)

	clickRow(t, p, sh, "sub/", tea.MouseButtonLeft)
	if menued != "" {
		t.Fatalf("a left click must not raise the host's folder menu (got %q)", menued)
	}
	if want := filepath.Join(root, "sub"); p.Dir() != want {
		t.Fatalf("a left click should walk in; Dir() = %q, want %q", p.Dir(), want)
	}
}

// TestRightClickIsEnter: the other button acts on the row instead — the same path enter
// takes, so the host builds the same menu in the same place.
func TestRightClickIsEnter(t *testing.T) {
	root := fileTree(t)
	var opened, menued string
	p, sh := mousePanel(t, root, &opened, &menued)

	clickRow(t, p, sh, "sub/", tea.MouseButtonRight)
	if menued != "sub" {
		t.Fatalf("a right click should reach OnOpenDir, got %q", menued)
	}
	if p.Dir() != root {
		t.Fatalf("a claimed OnOpenDir must not walk; Dir() = %q", p.Dir())
	}
}

// TestClickOnFileOpensEitherButton: a file has nowhere to walk, so opening it IS the host's
// verb and both buttons go to OnSelect — which is what keeps a click on a document doing
// what it always did.
func TestClickOnFileOpensEitherButton(t *testing.T) {
	for _, tc := range []struct {
		name   string
		button tea.MouseButton
	}{{"left", tea.MouseButtonLeft}, {"right", tea.MouseButtonRight}} {
		t.Run(tc.name, func(t *testing.T) {
			root := fileTree(t)
			var opened, menued string
			p, sh := mousePanel(t, root, &opened, &menued)

			clickRow(t, p, sh, "alpha.txt", tc.button)
			if opened != "alpha.txt" {
				t.Fatalf("a %s click on a file should reach OnSelect, got %q", tc.name, opened)
			}
			if menued != "" {
				t.Fatalf("a file must never reach OnOpenDir (got %q)", menued)
			}
		})
	}
}

// TestRightClickSelectsTheRowItLandsOn: the hook runs with the clicked row already under the
// cursor, which is what lets a host anchor its menu to the cursor exactly as the key path
// does — gofer's rowAnchor reads List().Index().
func TestRightClickSelectsTheRowItLandsOn(t *testing.T) {
	root := fileTree(t)
	var opened, menued string
	p, sh := mousePanel(t, root, &opened, &menued)

	p.List().Select(0)
	clickRow(t, p, sh, "alpha.txt", tea.MouseButtonRight)
	if sel, _ := p.Selected(); sel.Name != "alpha.txt" {
		t.Fatalf("the cursor should be on the clicked row, got %q", sel.Name)
	}
}

// TestClickOnUpRowWalksUp: ".." is the parent whichever button lands on it — the left click
// walks it as a directory, and the right one reaches OnOpenDir, which a host declines on
// that row (gofer's pickDir does) so the panel walks anyway.
func TestClickOnUpRowWalksUp(t *testing.T) {
	root := fileTree(t)
	sh := core.NewShared(nil)
	for _, button := range []tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonRight} {
		p := NewFilePanel(FilePanelOpts{
			Dir: filepath.Join(root, "sub"), Compact: true,
			OnOpenDir: func(_ *core.Shared, e FileEntry) (core.Action, bool) {
				return core.Action{}, !e.Up // the ".." row is declined, as a host's is
			},
		})
		p.SetSize(30, 12)
		p.Focus()
		clickRow(t, p, sh, "..", button)
		if p.Dir() != root {
			t.Fatalf("button %v on \"..\" should walk up; Dir() = %q", button, p.Dir())
		}
	}
}
