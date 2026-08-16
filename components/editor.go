package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EditorScreen is the simple nano-like text editor: it loads a file (or starts empty),
// lets the user type freely, and exits on ctrl+x with a "save modified buffer?"
// three-way prompt when the buffer is dirty (n = discard & exit, esc/c = cancel, and
// y = a filename prompt seeded with the current name — nano's "File Name to Write",
// so saving under a different name is a save-as). Enter splits lines (and may be
// extended by a handler registered for the file type), tab (or shift+tab, on a
// standalone screen — see key) inserts a tab, the arrows move the cursor, ctrl+z/ctrl+y undo and redo logical key events,
// alt+c/alt+x/alt+v copy, cut and paste through the system clipboard (with no selection
// the line the cursor is on is the target), and the left mouse button places the cursor
// and selects with drag/double/triple click.
// The right button is the context menu, when the host opts into it (EditorOpts.ContextMenu):
// a press raises a MenuScreen at the pointer offering copy/cut/paste — pressing inside an
// existing selection acts on it, pressing outside puts the caret there first, so a paste
// lands where the pointer did. Typing one of the delimiters in editorPairs over a selection wraps the
// selection in it and keeps it selected, so the key repeats to nest. The wheel scrolls
// the view without moving the cursor (a cursor move then snaps the view back to it), and
// when the buffer overflows the viewport a proportional scrollbar takes the rightmost
// column.
//
// It is a standalone screen owning the whole body, not a ModularScreen panel: it
// captures every keystroke (Filtering reports true the whole time, so the router's
// global single-key shortcuts never steal typed text — ctrl+c remains the hard quit).
// Embedded in a pane layout the capture still holds, minus the host's reserved
// pane keys (core.Keys.PaneNext et al.), which is how the keyboard leaves the pane;
// EditorOpts.OnExit is then about closing the BUFFER (ctrl+x, with the save prompt),
// not about escaping a trap. Embedded (ScreenPanel calls SetEmbedded, see
// core.Embeddable) it reads mouse coordinates as pane-relative and indents the body
// one column off the pane edge; it denotes focus by muting the body and dropping the
// cursor, so an unfocused pane reads as inactive without a frame. Whether it draws a
// frame at all is the instancer's call (EditorOpts.Border) and independent of both.
//
// The buffer is a hand-rolled lines/cursor/scroll model rather than bubbles/textarea
// because click-to-cursor needs the scroll offset, which textarea does not export, and
// because tabs have to be stored raw but never rendered raw (see expandLine).
// Deliberately minimal: the clipboard verbs are the three alt chords and, when the host
// opts in, the right-click menu — there is no keyboard selection, which is why the chords
// fall back to the whole line; optional literal search is enabled by the host through
// EditorOpts.Search.
type EditorScreen struct {
	path  string // file to load/save; empty ⇒ unsavable scratch buffer
	title string // title-bar text (defaults to the file's base name, else "Editor")
	crumb string // breadcrumb segment; defaults to title

	onExit    func(*core.Shared) core.Action         // embedded mode: replaces Pop on exit (nil ⇒ Pop)
	onRelease func(*core.Shared) core.Action         // esc: hand the keys back to the host (nil ⇒ esc ignored)
	onSaved   func(*core.Shared, string) core.Action // ctrl+s landed: the path written (nil ⇒ nothing)

	lines      [][]rune // the buffer; always at least one (possibly empty) line
	curY, curX int      // cursor: line index and rune column within it
	wantX      int      // column vertical moves aim for (clamped per line)
	scrY       int      // topmost visible buffer line
	scrX       int      // leftmost visible DISPLAY cell (tabs expand, so cells ≠ runes)

	loaded      bool // the one-time file read has been dispatched; a re-Init must not re-read
	dirty       bool // buffer differs from what was loaded/last saved
	confirmExit bool // the nano-style save/discard/cancel prompt is showing
	saveExits   bool // the save in flight came from the exit prompt, so it ends in exit

	hl         Highlighter      // syntax coloring; nil ⇒ plain render (see EditorOpts.Highlighter)
	hlExplicit bool             // hl came from EditorOpts, not the registry: a rename must not replace it
	editSeq    int              // bumped at every buffer mutation; hl reparses when hlSeq lags
	hlSeq      int              // the edit sequence hl last parsed (-1 ⇒ never)
	keyHandler editorKeyHandler // file-type-specific key diversion; nil ⇒ ordinary editing

	bordered bool // EditorOpts.Border: draw the frame instead of the title bar
	embedded bool // one pane of a layout (core.Embeddable): pane-relative mouse, gutter
	focused  bool // false ⇒ muted body, no cursor (core.FocusableScreen); true standalone

	originX, originY int  // the pane's absolute top-left (components.PaneOriginer)
	hasOrigin        bool // false standalone ⇒ the save-as box spans the full width

	w, h  int // viewport dims in cells (the body net of editor chrome), set by SetSize
	paneH int // full height assigned to the editor, including title/frame and search bar

	wrap      bool      // soft-wrap long lines to the viewport width
	lineNums  bool      // the sticky ctrl+l preference (see gutterOn: wrap forces the gutter on)
	wrapRows  []wrapRow // the wrapped display rows scrY indexes while wrap is on
	wrapBar   bool      // whether those rows overflow the viewport — resolved by rebuildWrapRows
	wrapDirty bool      // the wrap cache needs a rebuild: an edit, a resize or a toggle moved it

	dragging                  bool    // the active mouse gesture is extending a selection
	dragAnchor, dragAnchorEnd textPos // inclusive anchor cell as [start,end)
	selStart, selEnd          textPos // normalized half-open selected buffer range

	clickPos   textPos   // where the previous left press landed
	clickTime  time.Time // when it landed, for the multi-click window
	clickCount int       // 1 = caret, 2 = word, 3 = line; 0 ⇒ no press to build on

	contextMenu  bool                          // EditorOpts.ContextMenu: a right press raises the edit menu
	contextItems func(*core.Shared) []MenuItem // host rows appended to that menu below a rule

	emphasisPairs bool // '*'/'_' auto-close here: a prose buffer, not code (see emphasisPairExt)

	undoStack, redoStack                  []editorSnapshot
	revision, savedRevision, nextRevision uint64

	searchEnabled bool          // EditorOpts.Search: ctrl+f and match rendering are available
	searchEditing bool          // the modal line edit is open; keeps its bottom rows reserved even while empty
	searchQuery   string        // live query; retained after enter
	searchBefore  string        // query restored when an adjustment is cancelled
	searchSeq     int           // editSeq represented by searchMatches (-1 means stale)
	searchCached  string        // query represented by searchMatches
	searchMatches [][]textRange // per-line, non-overlapping display-cell ranges
}

// textPos is an insertion position in the rune buffer. Selection ranges are stored as
// [start,end); mouse endpoint cells are converted to these positions before sorting.
type textPos struct{ y, x int }

// textRange is a half-open range of display cells within one buffer line. Search
// results are found in rune columns, then cached in cells so rendering does not
// repeatedly walk every prefix of a tabbed line.
type textRange struct{ from, to int }

// editorSnapshot is one logical edit boundary. Lines are deep-copied because the
// editor's mutation helpers edit rune slices in place; the remaining fields restore
// the exact insertion/selection state without treating viewport browsing as history.
type editorSnapshot struct {
	lines             [][]rune
	curY, curX, wantX int
	selStart, selEnd  textPos
	revision          uint64
}

// wrapRow is one display row of a soft-wrapped buffer line: the half-open chunk
// [start, end) of that line's display cells (expandLine's output) the row shows. Every
// buffer line contributes at least one row, and a line ending exactly on the wrap
// margin contributes a trailing empty one so the caret at end of line has a cell to sit
// in rather than a position one column past the frame.
type wrapRow struct{ line, start, end int }

// EditorOpts configures an EditorScreen. Path names the file to edit; a missing or
// unreadable file starts an empty buffer that the first save creates (nano's
// behavior). The exit prompt's "y" may replace Path at runtime (save-as). Title/Crumb
// default from the path's base name.
//
// OnExit, when set, replaces the exit navigation (ctrl+x on a clean buffer, "n" =
// discard, and a successful save from the exit prompt): instead of core.Pop() the
// hook's Action is returned. Embed it in a pane layout (via ScreenPanel) with the
// hook set — a raw Pop there would dismiss the host ModularScreen, and the router
// ignores a Pop of the root screen, leaving the editor's capture with no keyboard
// way out. Standalone use leaves it nil and keeps the Pop.
//
// OnRelease, when set, binds esc to "the keys go elsewhere, the buffer stays": the
// hook's Action is returned and nothing about the buffer changes. It is the light
// counterpart to OnExit — a pane host points it at its own focus move, so leaving a
// capturing editor costs one key instead of a pane-nav chord, without closing what
// you were editing. Left nil, esc is ignored (the editor types everything else, and
// a bare esc that popped the host would be a trap). The exit prompt keeps its own
// esc = cancel: that branch runs first.
//
// OnSaved, when set, is called after a successful ctrl+s — the save that does NOT
// exit — with the path actually written, which is the typed one when the save-as box
// was pointed somewhere new. It is how a host keeps its own bookkeeping in step with a
// buffer that renamed itself under it (a path-keyed open-file map, a doc list that has
// a new file in it now). The exit prompt's save does not call it: that path ends in
// OnExit, which the host is already handling. Left nil, a save is silent — the title
// bar dropping its [+] marker is the only feedback, which is all a standalone editor
// needs.
//
// Border draws the shared frame (the ScrollContainer look) with the title as its
// top-edge legend instead of the title bar, the same opt-in ListPanelOpts.Border
// carries: which chrome an instance wears is the composing caller's choice, not the
// embedder's, so the same screen can be framed in one layout and plain in another.
// Default off — an editor denotes focus by muting its text either way.
//
// Highlighter adds syntax coloring. Left nil, the registry is consulted with
// Path's lowercased extension (RegisterHighlighter), so an ".md" buffer picks
// the markdown highlighter up on its own; an extension nobody registered (or an
// empty Path) renders plain, exactly as if highlighting did not exist. Set
// explicitly, it wins over the registry — pass a highlighter for a path-less
// scratch buffer, or to override the file's own kind. Highlighting is
// render-only: styles never change cell widths, and a highlighter whose spans
// don't reconstruct the line exactly is ignored (plain render), so the frame
// contract — no raw tabs, rectangular body — can't be broken by one.
//
// Search enables the editor's ctrl+f literal search. A floating LineEditScreen opens
// over a reserved bar at the editor's bottom edge and every case-insensitive match is
// highlighted in the buffer. A non-empty query leaves the same bar visible but
// unfocused after the overlay closes. It is opt-in so the shared editor does not
// change existing consumers' shortcuts or viewport geometry.
//
// ContextMenu enables the right-click menu (copy/cut/paste, see editMenu). It is opt-in
// for the same reason Search is: it takes the right button away from whatever the host
// was doing with it, and with it off a right press does nothing at all. ContextItems,
// when set, is consulted at press time and its rows are appended below a rule, so a host
// can hang its own entries off the same gesture; returning them fresh per press is what
// lets a row's Disabled reflect live state. An empty or nil return appends nothing — no
// dangling separator. ContextItems does NOT imply ContextMenu: one flag gates the whole
// gesture, so a host can mute it without nil-ing its items. Rows should leave MenuItem.Hint
// empty — the menu dispatches no accelerators, and the editor binds no cut/paste chords.
type EditorOpts struct {
	Path         string
	Title        string
	Crumb        string
	Border       bool
	OnExit       func(*core.Shared) core.Action
	OnRelease    func(*core.Shared) core.Action
	OnSaved      func(*core.Shared, string) core.Action
	Highlighter  Highlighter
	Search       bool
	ContextMenu  bool
	ContextItems func(*core.Shared) []MenuItem
}

// editorLoadedMsg carries the async file read from Init back to Update.
type editorLoadedMsg struct {
	content string
	err     error
}

// editorSavedMsg carries the async write and the revision it snapshotted back to
// Update, so an edit made while the write is in flight remains dirty.
type editorSavedMsg struct {
	err      error
	revision uint64
}

// editorCopiedMsg reports the asynchronous system-clipboard write. cut only picks the
// status line's verb: the buffer deletion already happened, synchronously.
type editorCopiedMsg struct {
	n   int
	err error
	cut bool
}

// editorPastedMsg carries the asynchronous clipboard READ back to Update, where the text
// is spliced into the buffer. target names the editor that asked: async messages reach an
// embedded editor through ModularScreen's broadcast to every panel, and a paste mutates
// the buffer — it must not land in a sibling editor pane that never asked for one.
type editorPastedMsg struct {
	target *EditorScreen
	text   string
	err    error
}

var writeEditorClipboard = clipboard.WriteAll

// readEditorClipboard is the read seam, mirroring writeEditorClipboard so tests can drive
// a paste without a system clipboard.
var readEditorClipboard = clipboard.ReadAll

var (
	editorCursorStyle = lipgloss.NewStyle().Reverse(true)
	editorPromptStyle = lipgloss.NewStyle().Bold(true)
)

// editorTabWidth is the display width a tab expands to when rendering. Raw '\t' must
// never reach the View output: the terminal expands it to the next tab stop while the
// renderer measures it as zero-width, so the padded frame line overflows, wraps, and
// every later frame shifts (the "screen advances a line" corruption).
const editorTabWidth = 4

// editorHistoryLimit bounds each of the whole-buffer snapshot stacks.
const editorHistoryLimit = 100

// editorWheelStep is how many lines one wheel notch scrolls the viewport.
const editorWheelStep = 3

// editorHWheelStep is how many display cells one horizontal wheel notch scrolls. Wider
// than the vertical step because a cell is a fraction of a word, where a line is a whole
// thought — six lands roughly a word over per notch.
const editorHWheelStep = 6

// editorMultiClickWindow is how long a left press stays eligible to be the
// second or third click of a same-button multi-click. tea.MouseMsg carries neither a
// timestamp nor a click count, so the editor keeps both itself.
const editorMultiClickWindow = 500 * time.Millisecond

// editorControlPlaceholder stands in for a control rune that reached the buffer anyway —
// a file loaded with a lone '\r' or a NUL, which setContent does not strip. One cell wide,
// so cellOfCol/colAtCell (which count every non-tab rune as one) stay exact.
const editorControlPlaceholder = '·'

// editorOverflowMark is the one dim cell that stands in the rightmost content column for
// the rest of a line the window cuts off (unwrapped only — wrapped, every cell is on
// screen already). It costs a column of text, so clampScroll keeps the caret out of it:
// a caret hidden under the marker would be worse than the ambiguity the marker fixes.
const editorOverflowMark = '~'

// expandLine renders a buffer line to display runes, tabs expanded to spaces and any
// other control rune replaced by a placeholder — none of them may reach View, where the
// terminal would act on them while the renderer measured them as zero-width. Display
// cells then equal display-rune indexes (double-width runes are the accepted
// limitation of this simple editor).
func expandLine(line []rune) []rune {
	var out []rune
	for _, r := range line {
		switch {
		case r == '\t':
			for i := 0; i < editorTabWidth; i++ {
				out = append(out, ' ')
			}
		case r < 0x20 || r == 0x7f:
			out = append(out, editorControlPlaceholder)
		default:
			out = append(out, r)
		}
	}
	return out
}

// cellOfCol is the display cell of a rune column within line (tabs count full width).
func cellOfCol(line []rune, col int) int {
	cell := 0
	for _, r := range line[:col] {
		if r == '\t' {
			cell += editorTabWidth
		} else {
			cell++
		}
	}
	return cell
}

// colAtCell is the rune column at or before a display cell — the inverse of cellOfCol
// for mapping mouse clicks back into the buffer. A click inside a tab's expansion
// lands on the tab itself.
func colAtCell(line []rune, cell int) int {
	c := 0
	for i, r := range line {
		w := 1
		if r == '\t' {
			w = editorTabWidth
		}
		if c+w > cell {
			return i
		}
		c += w
	}
	return len(line)
}

var _ core.Screen = (*EditorScreen)(nil)
var _ core.Filterer = (*EditorScreen)(nil)
var _ core.Crumber = (*EditorScreen)(nil)
var _ core.Embeddable = (*EditorScreen)(nil)
var _ core.FocusableScreen = (*EditorScreen)(nil)
var _ PaneOriginer = (*EditorScreen)(nil)

// NewEditorScreen builds the screen with an empty buffer; a configured Path is read
// asynchronously from the FIRST Init (the framework idiom — IO only in the cmd lane).
// Later Inits are no-ops, so the instance is what holds the buffer for its lifetime.
func NewEditorScreen(opts EditorOpts) *EditorScreen {
	title := opts.Title
	if title == "" {
		if opts.Path != "" {
			title = filepath.Base(opts.Path)
		} else {
			title = "Editor"
		}
	}
	crumb := opts.Crumb
	if crumb == "" {
		crumb = title
	}
	hl, hlExplicit := opts.Highlighter, opts.Highlighter != nil
	if hl == nil {
		hl = lookupHighlighter(strings.ToLower(filepath.Ext(opts.Path)))
	}
	return &EditorScreen{
		path:          opts.Path,
		title:         title,
		crumb:         crumb,
		onExit:        opts.OnExit,
		onRelease:     opts.OnRelease,
		onSaved:       opts.OnSaved,
		lines:         [][]rune{{}},
		bordered:      opts.Border,
		focused:       true, // standalone the editor is always focused; a panel blurs it
		hl:            hl,
		hlExplicit:    hlExplicit,
		keyHandler:    lookupEditorKeyHandler(strings.ToLower(filepath.Ext(opts.Path))),
		emphasisPairs: emphasisPairExt(strings.ToLower(filepath.Ext(opts.Path))),
		hlSeq:         -1,   // nothing parsed yet, even before the first edit
		wrapDirty:     true, // no rows measured yet, even before the first edit
		nextRevision:  1,
		searchEnabled: opts.Search,
		searchSeq:     -1,
		contextMenu:   opts.ContextMenu,
		contextItems:  opts.ContextItems,
	}
}

// exit produces the screen's exit navigation: the OnExit hook's Action when one is
// configured (embedded use), else a plain Pop (standalone use).
func (s *EditorScreen) exit(sh *core.Shared) core.Action {
	if s.onExit != nil {
		return s.onExit(sh)
	}
	return core.Pop()
}

// Init kicks off the file read when a path is configured; the result arrives as an
// editorLoadedMsg. No path ⇒ nothing to load.
//
// The read happens once per instance. A host that swaps this editor back into a pane
// calls Init again (ScreenPanel.SetChild does), and re-reading there would hand
// setContent the file — discarding unsaved edits, the undo history and the cursor of the
// very buffer the swap-back exists to return to. The flag is set before the empty-path
// return, so a scratch buffer that later gains a path (a save-as, or SetPath after the
// host renamed the file) is never read back off disk either: in both cases the buffer is
// already the authoritative content. It is set at dispatch rather than on arrival, so a
// second Init while the first read is in flight cannot queue a duplicate.
func (s *EditorScreen) Init(*core.Shared) tea.Cmd {
	if s.loaded {
		return nil
	}
	s.loaded = true
	if s.path == "" {
		return nil
	}
	path := s.path
	return func() tea.Msg {
		b, err := os.ReadFile(path)
		return editorLoadedMsg{content: string(b), err: err}
	}
}

// SetEmbedded implements core.Embeddable: ScreenPanel calls it when the editor is one
// pane of a layout. It shifts the geometry only — mouse coordinates arrive
// pane-relative (no Shared.BodyY to subtract) and the body indents one column off the
// pane edge so text doesn't butt against a neighbouring pane's border. The look is
// unaffected: EditorOpts.Border decides the frame.
func (s *EditorScreen) SetEmbedded(on bool) { s.embedded = on }

// SetFocused implements core.FocusableScreen: the host ModularScreen's focus arrives
// through ScreenPanel. Unfocused, the editor mutes its body text, drops the cursor and
// (unbordered) mutes its title bar, so the pane reads as inactive; the router drives
// the same transition on a standalone editor when the output pane takes the keys.
func (s *EditorScreen) SetFocused(focused bool) { s.focused = focused }

// Text is the live buffer as one string, lines joined with '\n' — what a save
// would write, and what a host reads to do something with the content while it is
// still being edited (a rendered preview beside the pane, a word count).
func (s *EditorScreen) Text() string {
	var b strings.Builder
	for i, l := range s.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	return b.String()
}

// SetPaneOrigin implements PaneOriginer: ScreenPanel forwards the host layout's
// rendered origin, which the save-as overlay anchors from (see saveAsEdit).
func (s *EditorScreen) SetPaneOrigin(x, y int) {
	s.originX, s.originY, s.hasOrigin = x, y, true
}

// Filtering reports text capture at all times: the editor types every printable key,
// so the router's global single-key shortcuts (q, o, r, [, ], t, …) must never fire
// over it. ctrl+c stays the router's hard quit.
func (s *EditorScreen) Filtering() bool { return true }

// CrumbLabel contributes the screen's breadcrumb segment (title, or the short crumb
// when one was configured).
func (s *EditorScreen) CrumbLabel(short bool) string {
	return crumbSeg(short, "", s.crumb, s.title)
}

const editorSearchBarH = 3 // one input row plus the rounded box's top and bottom borders

// searchEdit builds the floating line edit ctrl+f pushes over the editor's reserved
// bottom bar. The component owns text capture and its bordered overlay look; the
// editor owns only the live query. A static cursor avoids a blinking box over the
// document.
func (s *EditorScreen) searchEdit(sh *core.Shared) *LineEditScreen {
	s.searchBefore = s.searchQuery
	s.searchEditing = true
	x, y, w, h := s.paneGeometry(sh)
	y += max(h-editorSearchBarH, 0)
	edit := NewLineEdit("search", x, y, w,
		func(_ *core.Shared, query string) core.Action {
			s.searchQuery = query
			s.searchEditing = false
			return core.Pop()
		}, func(*core.Shared) core.Action {
			s.searchQuery = s.searchBefore
			s.searchEditing = false
			return core.Pop()
		})
	edit.SetPrompt("find: ")
	edit.SetValue(s.searchQuery)
	edit.SetCursorBlink(false)
	edit.Help = []key.Binding{} // keep the overlay to the shared component's slim shape
	edit.OnChange = func(_ *core.Shared, query string) core.Action {
		s.searchQuery = query
		return core.Action{}
	}
	return edit
}

// Update handles the async load/save results, mouse presses, and keystrokes — in the
// exit prompt's mode only its y/n/esc/c answers are live.
func (s *EditorScreen) Update(sh *core.Shared, msg tea.Msg) (core.Screen, core.Action) {
	switch m := msg.(type) {
	case editorLoadedMsg:
		if m.err == nil {
			s.setContent(m.content)
		}
		// A read error (missing file, permissions) leaves the empty buffer: the first
		// save creates the file, as nano does.
		return s, core.Action{}
	case editorSavedMsg:
		if m.err != nil {
			s.confirmExit = false
			sh.Log("editor: save failed: " + m.err.Error())
			return s, core.SetStatus("save failed: " + m.err.Error())
		}
		s.savedRevision = m.revision
		s.dirty = s.revision != s.savedRevision
		s.confirmExit = false
		// Where a save lands depends on which key started it: the exit prompt's save
		// is the last step of leaving, ctrl+s is a checkpoint that keeps the buffer,
		// its cursor and its scroll exactly where they were.
		if s.saveExits {
			return s, s.exit(sh)
		}
		if s.onSaved != nil {
			return s, s.onSaved(sh, s.path)
		}
		return s, core.Action{}
	case editorCopiedMsg:
		verb := "copied"
		if m.cut {
			verb = "cut"
		}
		if m.err != nil {
			return s, core.SetStatusAndLog(verb + " failed: " + m.err.Error())
		}
		return s, core.SetStatus(fmt.Sprintf("%s %d characters", verb, m.n))
	case editorPastedMsg:
		if m.target != s {
			return s, core.Action{} // a broadcast meant for another editor pane
		}
		if m.err != nil {
			return s, core.SetStatusAndLog("paste failed: " + m.err.Error())
		}
		if m.text == "" {
			return s, core.Action{} // an empty clipboard pastes nothing, silently
		}
		s.editAtomic(func() {
			s.deleteSelection() // a no-op without a selection
			s.insertText(m.text)
		})
		return s, core.SetStatus(fmt.Sprintf("pasted %d characters", utf8.RuneCountInString(m.text)))
	case tea.KeyMsg:
		return s.key(sh, m)
	case tea.MouseMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		switch m.Action {
		case tea.MouseActionPress:
			switch m.Button {
			case tea.MouseButtonLeft:
				s.pressSelection(sh, m.X, m.Y, time.Now())
			case tea.MouseButtonRight:
				if s.contextMenu {
					return s, s.pressContext(sh, m.X, m.Y)
				}
			case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown,
				tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
				// The wheel is browse-only and only while focused: mouse msgs are
				// broadcast to every pane, so an unfocused editor must not roll.
				if s.focused {
					s.wheel(m)
				}
			}
		// A drag is the only state motion and release can act on now that the left button
		// is the only one that starts a gesture. Neither ever arrives while the context
		// menu is up: the menu is the top screen, and it consumes every message.
		case tea.MouseActionMotion:
			if s.dragging {
				s.extendDrag(sh, m.X, m.Y)
			}
		case tea.MouseActionRelease:
			if s.dragging {
				s.extendDrag(sh, m.X, m.Y)
			}
			s.resetMouseGesture()
		}
		return s, core.Action{}
	}
	return s, core.Action{}
}

// copySelectionCmd is the clipboard write the menu's Copy and Cut rows issue. The write
// travels in the cmd lane because atotto shells out to pbcopy/xclip, which must never run
// inside Update.
func (s *EditorScreen) key(sh *core.Shared, m tea.KeyMsg) (core.Screen, core.Action) {
	k := m.String()
	s.resetMouseGesture()
	s.clickCount = 0 // typing between two clicks makes the second one a fresh first click
	if s.confirmExit {
		switch k {
		case "y", "Y":
			s.saveExits = true
			return s, core.Push(s.saveAsEdit(sh))
		case "n", "N":
			s.confirmExit = false
			return s, s.exit(sh)
		case "esc", "c":
			s.confirmExit = false
		}
		return s, core.Action{}
	}
	if s.searchEnabled && k == "ctrl+f" {
		return s, core.Push(s.searchEdit(sh))
	}
	if k == "ctrl+z" {
		s.undo()
		return s, core.Action{}
	}
	if k == "ctrl+y" {
		s.redo()
		return s, core.Action{}
	}
	// The clipboard chords are matched HERE, above the selection pre-switch below: they
	// arrive as alt-modified runes, so that switch's default would take them for typing
	// and delete the selection out from under the copy. They also record their own undo
	// step (editAtomic, inside copyOrCut and the paste's Update case), which is why they
	// sit above the editorEditKey gate rather than inside it. alt+c/x/v and not
	// ctrl+c/x/v — ctrl+c is the router's hard quit and ctrl+x this screen's exit, both
	// of which predate these and neither of which may move.
	switch k {
	case "alt+c":
		return s, s.copyOrCut(false)
	case "alt+x":
		return s, s.copyOrCut(true)
	case "alt+v":
		return s, pasteClipboardCmd(s)
	}
	if editorEditKey(k, m) {
		before, seq := s.snapshot(), s.editSeq
		defer func() {
			if s.editSeq != seq {
				s.recordEdit(before)
			}
		}()
	}
	// Wrapping the selection has to be decided before the pre-pass below, which deletes
	// the selection for every rune-bearing key. The undo gate above has already taken its
	// snapshot (a rune key is always an edit key), so both insertions land in one step.
	if s.selectionActive() && len(m.Runes) == 1 && !m.Alt {
		if closer, ok := editorPairs[m.Runes[0]]; ok {
			s.surroundSelection(m.Runes[0], closer)
			s.wrapDirty = true
			s.clampScroll()
			return s, core.Action{}
		}
	}
	if s.selectionActive() {
		switch k {
		case "backspace", "ctrl+h", "delete", "ctrl+d", "alt+backspace", "ctrl+w",
			"alt+delete", "alt+d", "ctrl+u", "ctrl+k":
			s.deleteSelection()
			s.wrapDirty = true
			s.clampScroll()
			return s, core.Action{}
		case "tab", "shift+tab", "enter":
			s.deleteSelection()
		case "up", "down", "left", "right", "alt+left", "ctrl+left", "alt+b",
			"alt+right", "ctrl+right", "alt+f", "home", "ctrl+a", "end", "ctrl+e":
			s.clearSelection()
		default:
			if len(m.Runes) > 0 {
				s.deleteSelection()
			}
		}
	}
	if s.keyHandler != nil && s.keyHandler(s, m) {
		s.wrapDirty = true
		s.clampScroll()
		return s, core.Action{}
	}
	switch k {
	case "ctrl+x":
		if !s.dirty {
			return s, s.exit(sh)
		}
		s.confirmExit = true
	case "ctrl+s":
		// The same save-as box the exit prompt's "y" raises, minus the exit: enter on
		// the prefilled path is a plain save, editing it is a save-as that the buffer
		// then belongs to. Offered even on a clean buffer — that is what makes it the
		// way to fork a doc to a new name.
		s.saveExits = false
		return s, core.Push(s.saveAsEdit(sh))
	case "esc":
		if s.onRelease != nil {
			return s, s.onRelease(sh)
		}
	case "tab", "shift+tab":
		s.insertRunes('\t')
	case "enter":
		s.newline()
	case "backspace", "ctrl+h":
		s.backspace()
	case "delete", "ctrl+d":
		s.forwardDelete()
	case "alt+backspace", "ctrl+w":
		s.deleteWordBack()
	case "alt+delete", "alt+d":
		s.deleteWordForward()
	case "ctrl+u":
		s.deleteRange(s.curY, 0, s.curY, s.curX)
		s.curX, s.wantX = 0, 0
	case "ctrl+k":
		if s.curX < len(s.lines[s.curY]) {
			s.lines[s.curY] = s.lines[s.curY][:s.curX]
			s.dirty = true
			s.editSeq++
		}
	case "up":
		s.moveVertical(-1)
	case "down":
		s.moveVertical(1)
	case "left":
		s.moveLeft()
	case "right":
		s.moveRight()
	case "alt+left", "ctrl+left", "alt+b":
		s.moveWordBack()
	case "alt+right", "ctrl+right", "alt+f":
		s.moveWordForward()
	case "home", "ctrl+a":
		s.curX, s.wantX = 0, 0
	case "end", "ctrl+e":
		s.curX = len(s.lines[s.curY])
		s.wantX = s.curX
	default:
		// A single typed opening delimiter brings its closer with it and leaves the caret
		// between the two. The one-rune guard keeps a bracketed paste (one KeyMsg carrying
		// the whole payload) on the insertText path below.
		if len(m.Runes) == 1 && !m.Alt {
			if closer, ok := editorPairs[m.Runes[0]]; ok && (s.emphasisPairs || !emphasisPairRune(m.Runes[0])) {
				s.insertRunes(m.Runes[0], closer)
				s.curX--
				s.wantX = s.curX
				break
			}
		}
		// Every rune-bearing key, typed or pasted: a bracketed paste is one KeyMsg whose
		// Runes carry newlines, so this must go through insertText, not insertRunes.
		if len(m.Runes) > 0 {
			s.insertText(string(m.Runes))
		}
	}
	s.wrapDirty = true
	s.clampScroll()
	return s, core.Action{}
}

// HelpView shows the editing hints, swapped for the prompt's y/n/c answers while the
// exit prompt is up.
func (s *EditorScreen) HelpView(sh *core.Shared) string {
	if s.confirmExit {
		return sh.BindingHelp([]key.Binding{
			key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "save as… & exit")),
			key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "discard & exit")),
			key.NewBinding(key.WithKeys("esc", "c"), key.WithHelp("esc", "cancel")),
		})
	}
	hints := s.HelpBindings()
	return sh.BindingHelp(hints)
}

// HelpBindings is the editor's non-prompt shortcut set (save, exit, leave pane,
// and the editing chords) — what HelpView renders, exported so a host's help
// overlay can list the same chords without duplicating their strings.
func (s *EditorScreen) HelpBindings() []key.Binding {
	hints := []key.Binding{
		key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "save")),
		key.NewBinding(key.WithKeys("ctrl+x"), key.WithHelp("ctrl+x", "exit")),
	}
	if s.searchEnabled {
		hints = append(hints, key.NewBinding(key.WithKeys("ctrl+f"), key.WithHelp("ctrl+f", "search")))
	}
	if s.onRelease != nil {
		hints = append(hints, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "leave pane")))
	}
	return append(hints,
		key.NewBinding(key.WithKeys("ctrl+z"), key.WithHelp("ctrl+z", "undo")),
		key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("ctrl+y", "redo")),
		key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab", "indent")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "newline")),
		key.NewBinding(key.WithKeys("up", "down", "left", "right"), key.WithHelp("↑↓←→", "move")),
		// Written "alt+", not "⌥": the keycodes are alt+ and every other modifier in
		// these bars spells itself out (ctrl+s, shift+tab), so the option glyph was the
		// one entry a reader had to translate. A host listing these alongside its own
		// alt chords (gote's ? overlay) then reads in one notation throughout.
		key.NewBinding(key.WithKeys("alt+left", "alt+right"), key.WithHelp("alt+←→", "word")),
		key.NewBinding(key.WithKeys("alt+backspace"), key.WithHelp("alt+backspace", "del word")),
		key.NewBinding(key.WithKeys("alt+c"), key.WithHelp("alt+c", "copy")),
		key.NewBinding(key.WithKeys("alt+x"), key.WithHelp("alt+x", "cut")),
		key.NewBinding(key.WithKeys("alt+v"), key.WithHelp("alt+v", "paste")),
	)
}

// Dirty reports unsaved changes in the buffer — what the [+] title marker shows —
// exported so a host (a quit gate) can ask before discarding the buffer.
func (s *EditorScreen) Dirty() bool { return s.dirty }

// ToggleWrap flips soft line wrapping, carrying the viewport across the switch: scrY
// counts wrapped display rows one side of it and buffer lines the other, so the top
// row is translated rather than reinterpreted — reinterpreted, an unwrap from deep in
// a long document lands past the last line and shows nothing at all.
//
// The keys belong to whoever hosts the editor: the state lives here, the binding is the
// app's to choose (ctrl+w is already delete-word-back in this screen).
func (s *EditorScreen) ToggleWrap() {
	top := s.TopLine() // in the mode we are leaving
	s.wrap = !s.wrap
	s.wrapDirty = true // the gutter appears or goes: the whole geometry moved
	if s.wrap {
		s.scrY = s.firstRowOfLine(top)
	} else {
		s.scrY = top
	}
	s.clampScrollBounds()
}

// TopLine is the buffer line showing at the top of the viewport, in either mode.
func (s *EditorScreen) TopLine() int { return s.lineAtRow(s.scrY) }

// CenterLine is the buffer line showing at the MIDDLE of the viewport — the anchor a
// synced view (gote's preview pane) centers itself on. Aligning the middles keeps the
// correspondence readable across the whole of the other pane rather than only at its
// first row, and leaves room at both ends for the two views to disagree about how many
// rows the same text takes.
func (s *EditorScreen) CenterLine() int { return s.lineAtRow(s.scrY + s.h/2) }

// ScrollSpan reports the view's vertical position in display ROWS: the current offset,
// the largest offset the buffer allows, and the viewport's height. Rows, not lines —
// wrapped, one line is several — which is what makes it the honest measure of "how far
// down are we" for a host syncing its own scroll to this one.
func (s *EditorScreen) ScrollSpan() (offset, maxOffset, height int) {
	return s.scrY, max(s.rowCount()-s.h, 0), s.h
}

// lineAtRow is the buffer line showing at a display row, in either mode.
func (s *EditorScreen) lineAtRow(row int) int {
	if row < 0 {
		row = 0
	}
	if !s.wrap {
		return min(row, max(len(s.lines)-1, 0))
	}
	s.rebuildWrapRows()
	if row < len(s.wrapRows) {
		return s.wrapRows[row].line
	}
	return max(len(s.lines)-1, 0)
}

// firstRowOfLine is the display row a buffer line starts on.
func (s *EditorScreen) firstRowOfLine(line int) int {
	s.rebuildWrapRows()
	for i, r := range s.wrapRows {
		if r.line >= line {
			return i
		}
	}
	return 0
}

// ToggleLineNums flips the sticky line-number preference. It is what decides the gutter
// while wrap is OFF; wrapped, the gutter is on either way (see gutterOn), so a flip
// made there only shows once wrap goes back off.
func (s *EditorScreen) ToggleLineNums() {
	s.lineNums = !s.lineNums
	s.wrapDirty = true // the gutter's width is part of the wrap geometry
	s.clampScrollBounds()
}

// WrapMode and LineNumMode return the current toggle states, so hosts can keep their
// own UI in sync.
func (s *EditorScreen) WrapMode() bool    { return s.wrap }
func (s *EditorScreen) LineNumMode() bool { return s.lineNums }

// SetSize records the viewport dims — the args net of whatever chrome this editor
// draws (see insetX/insetY, plus the frame's closing border on each axis) — and
// re-clamps the scroll into the buffer's bounds. Bounds only, NOT to the caret: the
// router re-lays out after every message, so caret-chasing here would undo every
// wheel scroll the same tick it happened.
func (s *EditorScreen) SetSize(_ *core.Shared, width, bodyHeight int) {
	s.paneH = bodyHeight
	s.w, s.h = width-s.insetX(), bodyHeight-s.insetY()
	if s.searchBarVisible() {
		s.h -= editorSearchBarH
	}
	if s.bordered {
		s.w-- // the right border
		s.h-- // the bottom border
	}
	if s.w < 1 {
		s.w = 1
	}
	if s.h < 1 {
		s.h = 1
	}
	s.wrapDirty = true
	s.clampScrollBounds()
}
