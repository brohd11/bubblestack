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

// FilePanel is a directory listing packaged as a ModularScreen panel: the folders and
// files of ONE directory, where enter (or a click) on a folder walks into it and enter on
// a file is the host's business. It is a navigator, not a tree — nothing expands in place,
// so a row is always one line's worth of one directory.
//
// It composes a ListPanel rather than reimplementing one, which is what keeps the wheel,
// click hit-testing, /-filtering, wrap-at-ends, pagination, the border frame and the
// compact marquee identical to every other sidebar in the framework. The two densities are
// ListPanel's own (NewListPanel's 3-row delegate, NewCompactListPanel's 1-row), and
// SetCompact swaps between them in place.
//
// Like editor.Screen, every difference between "the whole app is this" and "this is one
// pane" is either a hook the host sets or a bool the instancer chooses: Border is the
// composing caller's, Root bounds navigation, Include decides what is listed at all, and a
// nil hook is the sensible standalone default.
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

// FileEntry is one listed file, as handed to the panel's hooks. Dir is the directory the
// row was listed FROM, which is what a host needs to resolve a sibling ("create a file
// here") without asking the panel where it currently is.
type FileEntry struct {
	Name string // base name as listed
	Path string // absolute path
	Dir  string // the directory it was listed from
	// IsDir is "can this row be walked into": a directory, or a symlink to one. It follows
	// the link because that is the question every host branching on it is really asking,
	// and os.ReadDir's own answer (the link is not a directory) makes a linked folder
	// unreachable — see read.
	IsDir bool
	Up    bool // the ".." row; Path is the parent directory
}

// FilePanelOpts configures a FilePanel. Every hook is optional; nil means the panel's own
// default, which for a directory is "walk into it" and for everything else is "do nothing".
type FilePanelOpts struct {
	Dir    string // starting directory; "" is the working directory
	Root   string // navigation floor; "" leaves the panel free to walk to the filesystem root
	Title  string // fixed border legend; "" tracks the current directory's base name
	Border bool   // draw the shared frame (the instancer's call, never the embedder's)
	// Colors tints each row by what the entry IS — directory, dotfile, symlink, program,
	// source, archive (see filecolor.go). The instancer's call exactly as Border is: it is
	// an appearance decision, and the zero value leaves a listing plain, so a consumer that
	// has not thought about coloring does not silently get it.
	Colors bool

	// Compact picks the starting row density; DensityKey flips it live. An unbound
	// DensityKey (the zero value) leaves the chord to the host, which calls ToggleDensity
	// from wherever it already handles keys.
	Compact    bool
	DensityKey key.Binding

	// UpKey walks to the parent directory. The zero value means backspace, which is also
	// one of core.Keys.Back's keys — deliberately: the panel claims it ONLY while there is
	// a parent to go to, so at Root it falls through and still means "back" to the host.
	// esc and c always do. Left/right are not used: core.StyleList binds them to the
	// list's own pagination.
	UpKey key.Binding

	// Include decides what is listed, directories included. nil lists everything, dot
	// files and all. It receives the full path as well as the entry so a host can sniff
	// content (gote's DocFilter.Match) and not just the name.
	Include func(path string, d fs.DirEntry) bool

	// Less orders the rows. nil sorts directories first, then by name, case-insensitively.
	Less func(a, b FileEntry) bool

	// Rows contributes host rows pinned above "..", rebuilt on every directory change (an
	// action row like "+ new file" belongs to the folder you are looking at). Give them a
	// FilterValue of "" to keep them out of a /-query. OnRow runs when one is picked; a
	// components.Item or CompactItem dispatches itself and needs no hook.
	Rows  func(dir string) []list.Item
	OnRow func(*core.Shared, list.Item) core.Action

	// OnSelect runs on a FILE row. OnOpenDir intercepts a DIRECTORY row before the panel
	// navigates — return handled=false to let the walk happen anyway, which is how a host
	// can raise a menu for some folders and not others.
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

// NewFilePanel builds the panel and reads its first directory immediately, rather than
// waiting for Init. A panel swapped into a layout by a rebuild never sees Init — gote's
// rebuildModular deliberately does not re-Init the screen it builds — so a constructor
// that only recorded the path would render an empty column until some unrelated message
// happened along. Init is left to arm the inner list's marquee.
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

// absDir resolves a configured directory to an absolute, clean path. An empty one is the
// working directory; an unresolvable one is left as given rather than failing the
// constructor — the read that follows reports it as an empty listing.
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

// build makes the inner ListPanel at the current density. The compact constructor returns
// a wrapper whose whole behavior lives on the embedded *ListPanel (own filter line,
// inline pagination, marquee), so taking that field is how one struct field covers both
// densities without a second code path.
func (p *FilePanel) build() *ListPanel {
	opts := ListPanelOpts{
		OnSelect:  p.pick,
		OnKey:     p.key,
		OnPointer: p.pointer,
		Help:      p.opts.Help,
		Border:    p.opts.Border,
	}
	if p.compact {
		return NewCompactListPanel(p.rows(), p.title(), opts).ListPanel
	}
	return NewListPanel(p.rows(), p.title(), opts)
}

// title is the border legend (or, unbordered, the list's own title bar): the configured
// one when there is one, else the current directory's base name, so the column says where
// it is without spending a row on it.
func (p *FilePanel) title() string {
	if p.opts.Title != "" {
		return p.opts.Title
	}
	if base := filepath.Base(p.dir); base != "" && base != "." {
		return base
	}
	return p.dir
}

// setTitle re-points the legend after a directory change. ListPanel keeps the legend and
// the list's own title in two places (a bordered panel drops the title bar and moves the
// text to the frame), so both are written here.
func (p *FilePanel) setTitle() {
	t := p.title()
	p.panel.title = t
	if !p.opts.Border {
		p.panel.list.Title = t
	}
}

// ---------- listing ----------

// linkStat is what a symlink row points AT — the following stat os.ReadDir deliberately
// does not do. Only symlinks pay for it, so this is one extra stat on a rare kind of row
// rather than a second stat on every row (entryDesc has the budget).
//
// nil for anything that is not a symlink, and for a link that cannot be resolved: a dangling
// one, or a cycle, which os.Stat reports as ELOOP rather than spinning. Both then list as
// the plain file they look like from here, which is what every link did before this existed.
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

// rowColor is the color a row of kind k gets, or nil when this panel was not built with
// Colors. nil rather than some neutral color is what makes an uncolored panel PLAIN: it is
// what core.ColorItem reads as "no opinion", so the delegate leaves the row to the
// terminal's own foreground.
//
// Both color sites go through here so the flag cannot be honored in one and forgotten in
// the other. ClassifyFile still runs when the flag is off — it is a d.Type() check and at
// most a few map lookups, and one gate in one place is worth more than skipping it.
func (p *FilePanel) rowColor(k FileKind) color.Color {
	if !p.opts.Colors {
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
		// One stat per row, shared: the size line and the row's type color both want it,
		// and asking twice would double the cost entryDesc's comment says this component
		// can only just afford. A plain directory needs neither, so it is not stat'd at all.
		//
		// A symlink is the exception and is FOLLOWED. os.ReadDir does not, so a link to a
		// directory arrives with IsDir false: it would sort among the files, print no
		// trailing slash, describe itself by the link's own byte length, and — the part
		// that matters — refuse to be walked into, because pick sends anything that is not
		// a directory to OnSelect. Resolving here makes all four answer for the target.
		isDir := d.IsDir()
		var info fs.FileInfo
		if target := linkStat(path, d); target != nil {
			isDir, info = target.IsDir(), target
		} else if !isDir {
			info, _ = d.Info() // an lstat: the entry itself
		}
		out = append(out, fileItem{
			entry: FileEntry{Name: d.Name(), Path: path, Dir: dir, IsDir: isDir},
			desc:  entryDesc(isDir, info),
			// ClassifyFile still reads the LINK's own type, so a linked folder stays
			// symlink-colored rather than becoming dir-colored — the ls convention, and the
			// one thing on the row that still says an indirection is involved.
			color: p.rowColor(ClassifyFile(d, info)),
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
			color: p.rowColor(KindDir),
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

// clamp keeps a target inside Root. A path outside it lands on Root itself rather than
// being refused: the panel is a listing, and the honest answer to "somewhere you may not
// go" is the floor, not an error popup.
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

// fileItem is one row. It satisfies core.SuffixItem for the compact delegate and carries a
// Description for the standard one; the suffix stays empty because a listing is one
// directory deep and there is no path context to add to a name.
type fileItem struct {
	entry FileEntry
	desc  string
	color color.Color // the type color, nil for an ordinary file
}

var _ core.ColorItem = fileItem{}

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

// TitleColor implements core.ColorItem: the row's own foreground, so a directory reads as
// one without the eye having to find the trailing slash. Classified once at read time
// rather than per frame — a delegate's Render must stay cheap, and the entry it would have
// to re-examine is gone by then. nil (an ordinary file) leaves the row unstyled, and the
// selection accent outranks this on the cursor row.
func (i fileItem) TitleColor() color.Color { return i.color }

// FilterValue keeps ".." out of every search: a filter is a question about which entries
// you want, and the way out of the folder is not one of the answers (the same rule an
// inert action row follows). Nothing matches an empty target, so the row leaves as soon as
// the query has a character and comes back when it is emptied.
func (i fileItem) FilterValue() string {
	if i.entry.Up {
		return ""
	}
	return i.entry.Name
}

// entryDesc is the standard delegate's second line: what a directory is, or how big a file
// is. Info() costs one stat per row, which a single directory can afford and a recursive
// walk could not — one more reason this component lists one folder at a time. It takes that
// stat rather than making it, so the one read also feeds the row's type color; a nil info
// is a stat that failed, and the row keeps its name and loses its size.
//
// isDir is read's RESOLVED answer, not d.IsDir(): a symlink to a folder has to say "dir"
// here rather than report the byte length of the link itself.
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

// pick routes enter and a RIGHT click — the two "act on this row" gestures. A directory
// walks (unless OnOpenDir claims it), a file goes to OnSelect, and a host row goes to OnRow
// — or dispatches itself, if it is one of the framework's self-dispatching Items.
//
// A left click on a directory does not come here: see pointer, which walks instead so the
// host's menu is not in the way of the commonest gesture there is.
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

// pointer splits the two mouse buttons, which enter cannot: LEFT is "open this row" — and a
// folder opens by being walked into, never by raising the host's menu — while RIGHT is
// exactly what enter does, so a host with an OnOpenDir menu gets it on the button that means
// "menu" everywhere else. That is the whole difference between the mouse and the keyboard
// here, and it mirrors the keys: d walks, enter acts.
//
// A file answers handled=false either way and falls back to OnSelect. It has nowhere to walk
// to, so opening it IS whatever the host does with it, and a click on a document keeps doing
// what it always did.
//
// The menu stays the HOST's to build, which is not incidental: by the time a press reaches a
// panel ModularScreen has made the coordinates pane-local, and a panel does not know its own
// origin — only the host does, which is why an anchor like gofer's rowAnchor lives there.
// Routing the right button back through pick means the click path and the enter path raise
// the same menu in the same place.
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

// key is the inner panel's OnKey: the host's row keys, typed to the entry. The ".." row
// has no file to act on, so it reports unhandled and the key falls back to the list —
// the same thing an inert action row does.
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

// UpdatePanel claims the panel's own two chords before handing everything else to the
// list. Both are gated on Capturing: a live /-filter owns every keystroke, and a component
// that stole one back would eat a character out of the query.
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

// SetDir lists dir, clamped to Root. A directory that cannot be read leaves the panel
// exactly where it was and goes to OnError, so a permission-denied folder cannot strand
// the column on nothing.
//
// Walking UP selects the folder just left. Coming out of a deep tree onto a list of forty
// siblings with the cursor reset to the top loses your place for no reason; the row you
// came from is the one you were last looking at.
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

// Refresh re-reads the current directory, keeping the cursor where it is. It is the
// panel's answer to a host's reseed broadcast; a read failure leaves the last good listing
// on screen rather than blanking the column.
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
	if idx < 0 {
		idx = 0
	}
	p.panel.List().Select(idx)
}

// ---------- density ----------

// Compact reports the current row density.
func (p *FilePanel) Compact() bool { return p.compact }

// ToggleDensity flips between the one-row and three-row list.
func (p *FilePanel) ToggleDensity() tea.Cmd { return p.SetCompact(!p.compact) }

// SetCompact rebuilds the inner panel at the other density, carrying over the directory,
// the cursor, the allocation and focus. The two densities are two ListPanel constructors
// (their delegates, filter line, pagination style and marquee all differ), so this is a
// rebuild rather than a setting — which is also why an APPLIED /-filter does not survive
// it. Returns the focused panel's on-focus cmd, so a marquee starts without waiting for
// the next keystroke.
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

func (p *FilePanel) Focus()        { p.panel.Focus() }
func (p *FilePanel) Blur()         { p.panel.Blur() }
func (p *FilePanel) Focused() bool { return p.panel.Focused() }

func (p *FilePanel) Init(sh *core.Shared) tea.Cmd { return p.panel.Init(sh) }
func (p *FilePanel) OnFocus() tea.Cmd             { return p.panel.OnFocus() }
func (p *FilePanel) Capturing() bool              { return p.panel.Capturing() }
func (p *FilePanel) PanelHelp() []key.Binding     { return p.panel.PanelHelp() }

// RowAnchor is the MenuAnchor for a context menu over visible item idx, given the panel's
// own top-left in absolute terminal cells (0, Shared.BodyY() for a panel filling the body;
// its slot's origin inside a larger layout). It is the AnchorListRow family's member for
// this component: the menu opens on the first row below the whole item and flips clear
// above it, so the row it acts on stays visible.
//
// It lives here rather than in the caller because both numbers it needs change with
// density — the item's row height, and the chrome above the list — and a consumer holding
// a copy of either would put the box a row off the moment the density flipped. ok is false
// when idx is scrolled off-page; the caller picks the fallback, as with RowY.
func (p *FilePanel) RowAnchor(idx, originX, originY int) (MenuAnchor, bool) {
	row, ok := p.RowY(idx)
	if !ok {
		return MenuAnchor{}, false
	}
	top := originY + row
	return MenuAnchor{X: originX, Y: top + p.panel.itemRows, FlipX: originX + 1, FlipY: top}, true
}

// UpKey is the binding that walks to the parent directory — the configured one, or the
// default. Exported so a host's help page states the key the panel actually answers to
// rather than a copy of it.
func (p *FilePanel) UpKey() key.Binding { return p.upKey }

// List exposes the underlying list model, matching ListPanel.List — a host restyling its
// lists on a theme broadcast needs the same access here.
func (p *FilePanel) List() *list.Model { return p.panel.List() }

// RowY is the panel-relative row visible item idx starts at, matching ListPanel.RowY: what
// an overlay anchored over a row (a rename box) must use.
func (p *FilePanel) RowY(idx int) (int, bool) { return p.panel.RowY(idx) }
