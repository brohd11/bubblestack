package components

import (
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

// FilePanel lists one directory as a ModularScreen panel: enter or a click on a folder
// walks into it, and enter on a file is the host's business. It is a navigator, not a
// tree. It composes a ListPanel, so wheel, clicks, filtering, pagination, frame and
// marquee match every other sidebar; SetCompact swaps between its two densities.
// Standalone defaults come from nil hooks; Border, Root and Include are the instancer's.
type FilePanel struct {
	opts  FilePanelOpts
	panel *ListPanel // whichever density is live; rebuilt by SetCompact

	dir     string     // the directory currently listed, absolute and clean
	root    string     // navigation floor, absolute and clean; "" is unclamped
	items   []fileItem // the current directory's rows, filtered and sorted
	compact bool

	w, h  int // last allocation, re-applied to a rebuilt inner panel
	upKey key.Binding
}

var _ Panel = (*FilePanel)(nil)
var _ Focusable = (*FilePanel)(nil)
var _ PanelUpdater = (*FilePanel)(nil)
var _ Capturing = (*FilePanel)(nil)
var _ PanelHelper = (*FilePanel)(nil)
var _ panelInitializer = (*FilePanel)(nil)
var _ FocusNotifier = (*FilePanel)(nil)

// FileEntry is one listed file as handed to hooks. Dir is the directory it was listed
// from, so a host can resolve siblings.
type FileEntry struct {
	Name string // base name as listed
	Path string // absolute path
	Dir  string // the directory it was listed from
	// IsDir is "can this row be walked into": a directory, or a symlink to one.
	IsDir bool
	Up    bool // the ".." row; Path is the parent directory
}

// FilePanelOpts configures a FilePanel. Every hook is optional; nil means the panel's own
// default, which for a directory is "walk into it" and for everything else is "do nothing".
type FilePanelOpts struct {
	Dir    string     // starting directory; "" is the working directory
	Root   string     // navigation floor; "" leaves the panel free to walk to the filesystem root
	Title  string     // fixed border legend; "" tracks the current directory's base name
	Border bool       // draw the shared frame (the instancer's call, never the embedder's)
	Frame  FrameStyle // another frame look (see ListPanelOpts.Frame); implies Border
	// Selection styles the selected row (see ListPanelOpts.Selection).
	Selection core.SelectionOpts
	// Colors controls built-in file-type colors. The zero value leaves all rows plain.
	Colors FileColorMode
	// TitleColor optionally overrides a row's foreground. nil falls back to Colors.
	// Called during rendering: use cached state, never filesystem or subprocess work.
	TitleColor func(FileEntry) color.Color
	// KeepColor opts every row out of the selection accent (core.KeepColorItem), for colors
	// that carry state the reader wants under the cursor, like git status.
	KeepColor bool

	// Compact picks the starting density; DensityKey flips it live. An unbound DensityKey
	// leaves the chord to the host (ToggleDensity).
	Compact    bool
	DensityKey key.Binding

	// UpKey walks to the parent directory; the zero value means backspace. It is claimed only
	// while there is a parent, so at Root backspace still means "back" to the host.
	// Left/right are left to the list's pagination.
	UpKey key.Binding

	// Include decides what is listed, directories included; nil lists everything. It gets
	// the full path so a host can sniff content.
	Include func(path string, d fs.DirEntry) bool

	// Less orders the rows. nil sorts directories first, then by name, case-insensitively.
	Less func(a, b FileEntry) bool

	// Rows adds host rows above "..", rebuilt on every directory change. Give them an empty
	// FilterValue to keep them out of searches. OnRow runs when one is picked; Item and
	// CompactItem rows dispatch themselves.
	Rows  func(dir string) []list.Item
	OnRow func(*core.Shared, list.Item) core.Action

	// OnSelect runs on a file row. OnOpenDir can intercept a directory row before the walk;
	// handled=false lets the walk happen.
	OnSelect  func(*core.Shared, FileEntry) core.Action
	OnOpenDir func(*core.Shared, FileEntry) (core.Action, bool)

	// OnKey claims extra row keys, exactly as ListPanelOpts.OnKey does, but typed to the
	// entry under the cursor. It does not fire on the ".." row or on a host row.
	OnKey func(*core.Shared, string, FileEntry) (core.Action, bool)

	// OnDir fires after the listed directory changed, for a breadcrumb or a status line.
	// OnError fires when a directory cannot be read; the panel stays where it was.
	OnDir   func(*core.Shared, string) core.Action
	OnError func(*core.Shared, error) core.Action

	Help []key.Binding
}

// defaultUpKey is what UpKey falls back to. See FilePanelOpts.UpKey for why backspace is
// safe to share with core.Keys.Back.
var defaultUpKey = key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "up"))

// NewFilePanel reads the first directory immediately rather than in Init: a panel
// swapped in by a layout rebuild never sees Init.
func NewFilePanel(opts FilePanelOpts) *FilePanel {
	p := &FilePanel{opts: opts, compact: opts.Compact, upKey: opts.UpKey}
	if len(p.upKey.Keys()) == 0 {
		p.upKey = defaultUpKey
	}
	// Root only when one was configured: an empty Root means unclamped, and passing it
	// through absDir would silently make the working directory a floor.
	if opts.Root != "" {
		p.root = absDir(opts.Root)
	}
	p.dir = p.clamp(absDir(opts.Dir))
	p.items, _ = p.read(p.dir) // an unreadable start directory renders as an empty column
	p.panel = p.build()
	return p
}

// absDir makes dir absolute and clean ("" is the working directory). An unresolvable dir
// is kept as given; the read then reports it empty.
func absDir(dir string) string {
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		return wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	return abs
}

// build makes the inner ListPanel at the current density. The compact constructor wraps a
// *ListPanel, so taking that field covers both densities.
func (p *FilePanel) build() *ListPanel {
	opts := ListPanelOpts{
		OnSelect:  p.pick,
		OnKey:     p.key,
		OnPointer: p.pointer,
		Help:      p.opts.Help,
		Border:    p.opts.Border,
		Frame:     p.opts.Frame,
		Selection: p.opts.Selection,
	}
	if p.compact {
		return NewCompactListPanel(p.rows(), p.title(), opts).ListPanel
	}
	return NewListPanel(p.rows(), p.title(), opts)
}

// title is the configured legend, or the current directory's base name.
func (p *FilePanel) title() string {
	if p.opts.Title != "" {
		return p.opts.Title
	}
	if base := filepath.Base(p.dir); base != "" && base != "." {
		return base
	}
	return p.dir
}

// setTitle updates both the frame legend and the list's own title after a directory
// change.
func (p *FilePanel) setTitle() {
	t := p.title()
	p.panel.title = t
	if p.panel.frame == nil {
		p.panel.list.Title = t
	}
}

// ---------- listing ----------

// linkStat follows a symlink row (os.ReadDir does not), costing one stat only for links.
// nil for non-links and for dangling or cyclic links, which then list as plain files.
func linkStat(path string, d fs.DirEntry) fs.FileInfo {
	if d.Type()&fs.ModeSymlink == 0 {
		return nil
	}
	info, err := os.Stat(path) // follows, where DirEntry.Info does not
	if err != nil {
		return nil
	}
	return info
}

// rowColor applies the built-in mode. nil lets the delegate use its normal style.
func (p *FilePanel) rowColor(k FileKind) color.Color {
	if p.opts.Colors == FileColorsNone || (p.opts.Colors == FileColorsDirs && k != KindDir && k != KindHiddenDir) {
		return nil
	}
	return FileKindColor(k)
}

// read lists dir through the Include filter and the sort. Errors are the caller's to
// route: navigation surfaces them through OnError and leaves the panel where it was.
func (p *FilePanel) read(dir string) ([]fileItem, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]fileItem, 0, len(des))
	for _, d := range des {
		path := filepath.Join(dir, d.Name())
		if p.opts.Include != nil && !p.opts.Include(path, d) {
			continue
		}
		// One stat per row, shared by the size line and the type color; plain directories skip
		// it. Symlinks are followed, so a link to a folder sorts, reads and walks as a folder.
		isDir := d.IsDir()
		var info fs.FileInfo
		if target := linkStat(path, d); target != nil {
			isDir, info = target.IsDir(), target
		} else if !isDir {
			info, _ = d.Info() // an lstat: the entry itself
		}
		kind := ClassifyFile(d, info)
		if p.opts.Colors == FileColorsDirs && isDir {
			kind = KindDir
			if isHiddenName(d.Name()) {
				kind = KindHiddenDir
			}
		}
		out = append(out, fileItem{
			entry: FileEntry{Name: d.Name(), Path: path, Dir: dir, IsDir: isDir},
			desc:  entryDesc(isDir, info),
			// All mode preserves symlink colors; dirs mode colors navigable folders.
			color:      p.rowColor(kind),
			titleColor: p.opts.TitleColor,
			keepColor:  p.opts.KeepColor,
		})
	}
	less := p.opts.Less
	if less == nil {
		less = defaultFileLess
	}
	sort.SliceStable(out, func(i, j int) bool { return less(out[i].entry, out[j].entry) })
	return out, nil
}

// defaultFileLess puts directories above files, then orders by name case-insensitively,
// falling back to the raw name so "README" and "readme" have a stable order between them.
func defaultFileLess(a, b FileEntry) bool {
	if a.IsDir != b.IsDir {
		return a.IsDir
	}
	an, bn := strings.ToLower(a.Name), strings.ToLower(b.Name)
	if an != bn {
		return an < bn
	}
	return a.Name < b.Name
}

// rows is the full row set: the host's rows, then "..", then the listing. Rebuilt whenever
// the directory changes, so an action row is always about the folder on screen.
func (p *FilePanel) rows() []list.Item {
	var items []list.Item
	if p.opts.Rows != nil {
		items = append(items, p.opts.Rows(p.dir)...)
	}
	if p.canUp() {
		items = append(items, fileItem{
			entry: FileEntry{Name: "..", Path: filepath.Dir(p.dir), Dir: p.dir, IsDir: true, Up: true},
			desc:  "parent directory",
			// The way out is a folder, and is colored as one: it is synthetic, so there is
			// no fs.DirEntry for ClassifyFile to read.
			color:      p.rowColor(KindDir),
			titleColor: p.opts.TitleColor,
			keepColor:  p.opts.KeepColor,
		})
	}
	for _, it := range p.items {
		items = append(items, it)
	}
	return items
}

// canUp reports whether there is a parent to walk to: one that is not the path's own
// fixed point (the filesystem root), and not above Root when one is set.
func (p *FilePanel) canUp() bool {
	if parent := filepath.Dir(p.dir); parent == p.dir {
		return false
	}
	return p.root == "" || p.dir != p.root
}

// clamp keeps a target inside Root; anything outside lands on Root rather than erroring.
func (p *FilePanel) clamp(dir string) string {
	dir = filepath.Clean(dir)
	if p.root == "" || dir == p.root {
		return dir
	}
	if strings.HasPrefix(dir, p.root+string(filepath.Separator)) {
		return dir
	}
	return p.root
}

// ---------- rows ----------

// fileItem is one row, for both delegates. The suffix is empty: a one-directory listing
// has no path context to add.
type fileItem struct {
	entry      FileEntry
	desc       string
	color      color.Color // the type color, nil for an ordinary file
	titleColor func(FileEntry) color.Color
	keepColor  bool // FilePanelOpts.KeepColor, carried per row: the delegate reads the contract
}

var (
	_ core.ColorItem     = fileItem{}
	_ core.KeepColorItem = fileItem{}
)

func (i fileItem) Title() string {
	if i.entry.Up {
		return ".."
	}
	if i.entry.IsDir {
		return i.entry.Name + "/"
	}
	return i.entry.Name
}

func (i fileItem) Description() string { return i.desc }
func (i fileItem) SuffixText() string  { return "" }

// TitleColor implements core.ColorItem. Classified once at read time to keep Render cheap;
// nil leaves the row plain.
func (i fileItem) TitleColor() color.Color {
	if i.titleColor != nil {
		if c := i.titleColor(i.entry); c != nil {
			return c
		}
	}
	return i.color
}

// KeepColor implements core.KeepColorItem for whichever color TitleColor settled on.
func (i fileItem) KeepColor() bool { return i.keepColor }

// FilterValue keeps ".." out of every search: it reappears when the query is cleared.
func (i fileItem) FilterValue() string {
	if i.entry.Up {
		return ""
	}
	return i.entry.Name
}

// entryDesc is the standard delegate's second line: "dir", or the file's size. It takes
// read's stat (nil when it failed) and read's resolved isDir, so a linked folder says dir.
func entryDesc(isDir bool, info fs.FileInfo) string {
	if isDir {
		return "dir"
	}
	if info == nil {
		return ""
	}
	return formatSize(info.Size())
}

// formatSize renders a byte count for a row: whole bytes below 1K, one decimal above.
func formatSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

// ---------- dispatch ----------

// pick handles enter and a right click: a directory walks (unless OnOpenDir claims it), a
// file goes to OnSelect, and a host row to OnRow or its own dispatch. A left click on a
// directory walks instead (see pointer).
func (p *FilePanel) pick(sh *core.Shared, it list.Item) core.Action {
	fi, ok := it.(fileItem)
	if !ok {
		if p.opts.OnRow != nil {
			return p.opts.OnRow(sh, it)
		}
		if hook := itemPick(it); hook != nil {
			return hook(sh)
		}
		return core.Action{}
	}
	if fi.entry.IsDir {
		if p.opts.OnOpenDir != nil {
			if act, handled := p.opts.OnOpenDir(sh, fi.entry); handled {
				return act
			}
		}
		return p.SetDir(sh, fi.entry.Path)
	}
	if p.opts.OnSelect != nil {
		return p.opts.OnSelect(sh, fi.entry)
	}
	return core.Action{}
}

// pointer splits the mouse buttons: left opens a row (walking into a folder, never
// raising the host's menu), right does what enter does, so an OnOpenDir menu is on the
// button that means "menu". Files fall back to OnSelect either way. The host builds the
// menu because only it knows the panel's origin for the anchor.
func (p *FilePanel) pointer(sh *core.Shared, it list.Item, right bool) (core.Action, bool) {
	if right {
		return p.pick(sh, it), true
	}
	fi, ok := it.(fileItem)
	if !ok || !fi.entry.IsDir {
		return core.Action{}, false
	}
	return p.SetDir(sh, fi.entry.Path), true
}

// key is the inner panel's OnKey: host row keys typed to the entry. ".." has no file, so
// it reports unhandled.
func (p *FilePanel) key(sh *core.Shared, k string, it list.Item) (core.Action, bool) {
	fi, ok := it.(fileItem)
	if !ok {
		if keys := itemKeys(it); keys != nil {
			return keys(sh, k)
		}
		return core.Action{}, false
	}
	if fi.entry.Up || p.opts.OnKey == nil {
		return core.Action{}, false
	}
	return p.opts.OnKey(sh, k, fi.entry)
}

// UpdatePanel claims the panel's own chords before the list, except while a filter is
// capturing keys.
func (p *FilePanel) UpdatePanel(sh *core.Shared, msg tea.Msg) (core.Action, bool) {
	if km, ok := msg.(tea.KeyPressMsg); ok && !p.panel.Capturing() {
		k := km.String()
		// canUp first: at the floor backspace is not ours, so it reaches the host as Back.
		if p.canUp() && core.MatchKey(k, p.upKey) {
			return p.SetDir(sh, filepath.Dir(p.dir)), true
		}
		// An unbound DensityKey matches nothing (MatchKey over an empty binding is false).
		if core.MatchKey(k, p.opts.DensityKey) {
			return core.Async(p.ToggleDensity()), true
		}
	}
	return p.panel.UpdatePanel(sh, msg)
}

// ---------- navigation ----------

// Dir is the directory currently listed.
func (p *FilePanel) Dir() string { return p.dir }

// SetDir lists dir, clamped to Root. An unreadable directory leaves the panel where it
// was and goes to OnError. Walking up selects the folder just left.
func (p *FilePanel) SetDir(sh *core.Shared, dir string) core.Action {
	dir = p.clamp(dir)
	items, err := p.read(dir)
	if err != nil {
		if p.opts.OnError != nil {
			return p.opts.OnError(sh, err)
		}
		return core.Action{}
	}
	from := p.dir
	p.dir, p.items = dir, items
	p.setTitle()
	p.panel.SetItems(p.rows())
	if filepath.Dir(from) == dir && from != dir {
		p.selectPath(from)
	} else {
		p.panel.List().Select(0)
	}
	if p.opts.OnDir != nil {
		return p.opts.OnDir(sh, dir)
	}
	return core.Action{}
}

// Refresh re-reads the current directory, keeping the cursor; a failure keeps the last
// good listing.
func (p *FilePanel) Refresh() {
	items, err := p.read(p.dir)
	if err != nil {
		return
	}
	idx := p.panel.List().Index()
	p.items = items
	p.panel.SetItems(p.rows())
	p.selectIndex(idx)
}

// Selected is the entry under the cursor, false on a host row (or an empty list).
func (p *FilePanel) Selected() (FileEntry, bool) {
	fi, ok := p.panel.List().SelectedItem().(fileItem)
	return fi.entry, ok
}

// selectPath puts the cursor on the row for path, if it is in the current listing.
func (p *FilePanel) selectPath(path string) {
	for i, it := range p.panel.List().VisibleItems() {
		if fi, ok := it.(fileItem); ok && fi.entry.Path == path {
			p.panel.List().Select(i)
			return
		}
	}
	p.panel.List().Select(0)
}

// selectIndex restores a cursor position against a row set that may have shrunk.
func (p *FilePanel) selectIndex(idx int) {
	if n := len(p.panel.List().VisibleItems()); idx >= n {
		idx = n - 1
	}
	idx = max(idx, 0)
	p.panel.List().Select(idx)
}

// ---------- density ----------

// Compact reports the current row density.
func (p *FilePanel) Compact() bool { return p.compact }

// ToggleDensity flips between the one-row and three-row list.
func (p *FilePanel) ToggleDensity() tea.Cmd { return p.SetCompact(!p.compact) }

// SetCompact rebuilds the inner panel at the other density, carrying over directory,
// cursor, size and focus (an applied filter is lost). Returns the on-focus cmd so a
// marquee starts immediately.
func (p *FilePanel) SetCompact(compact bool) tea.Cmd {
	if compact == p.compact {
		return nil
	}
	idx := p.panel.List().Index()
	focused := p.panel.Focused()
	p.compact = compact
	p.panel = p.build()
	if focused {
		p.panel.Focus()
	}
	if p.w > 0 {
		p.panel.SetSize(p.w, p.h)
	}
	p.selectIndex(idx)
	if !focused {
		return nil
	}
	// After SetSize: the marquee measures the selected row against the list's width, and
	// an unsized list has none to measure against.
	return p.panel.OnFocus()
}

// ---------- Panel contract ----------

func (p *FilePanel) SetSize(width, height int) {
	p.w, p.h = width, height
	p.panel.SetSize(width, height)
}

func (p *FilePanel) View(focused bool) string { return p.panel.View(focused) }

// SetFrame swaps the frame, keeping it across density rebuilds (see ListPanel.SetFrame).
func (p *FilePanel) SetFrame(f FrameStyle) {
	p.opts.Frame, p.opts.Border = f, f != nil
	p.panel.SetFrame(f)
}

func (p *FilePanel) Focus()        { p.panel.Focus() }
func (p *FilePanel) Blur()         { p.panel.Blur() }
func (p *FilePanel) Focused() bool { return p.panel.Focused() }

func (p *FilePanel) Init(sh *core.Shared) tea.Cmd { return p.panel.Init(sh) }
func (p *FilePanel) OnFocus() tea.Cmd             { return p.panel.OnFocus() }
func (p *FilePanel) Capturing() bool              { return p.panel.Capturing() }
func (p *FilePanel) PanelHelp() []key.Binding     { return p.panel.PanelHelp() }

// RowAnchor is the MenuAnchor for a context menu over visible item idx, given the panel's
// absolute origin: below the item, flipping above it. It lives here because both inputs
// change with density. ok is false when idx is off-page.
func (p *FilePanel) RowAnchor(idx, originX, originY int) (MenuAnchor, bool) {
	row, ok := p.RowY(idx)
	if !ok {
		return MenuAnchor{}, false
	}
	top := originY + row
	return MenuAnchor{X: originX, Y: top + p.panel.itemRows, FlipX: originX + 1, FlipY: top}, true
}

// UpKey is the binding that walks to the parent, exported for a host's help page.
func (p *FilePanel) UpKey() key.Binding { return p.upKey }

// List exposes the underlying list model, matching ListPanel.List — a host restyling its
// lists on a theme broadcast needs the same access here.
func (p *FilePanel) List() *list.Model { return p.panel.List() }

// RowY is the panel-relative row visible item idx starts at, matching ListPanel.RowY: what
// an overlay anchored over a row (a rename box) must use.
func (p *FilePanel) RowY(idx int) (int, bool) { return p.panel.RowY(idx) }
