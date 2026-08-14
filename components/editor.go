package components

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// EditorScreen is the simple nano-like text editor: it loads a file (or starts empty),
// lets the user type freely, and exits on ctrl+x with a "save modified buffer?"
// three-way prompt when the buffer is dirty (n = discard & exit, esc/c = cancel, and
// y = a filename prompt seeded with the current name — nano's "File Name to Write",
// so saving under a different name is a save-as). Enter splits lines (and may be
// extended by a handler registered for the file type), tab (or shift+tab) inserts a
// tab, the arrows move the cursor, ctrl+z/ctrl+y undo and redo logical key events,
// and either mouse button places the cursor and selects with drag/double/triple click.
// Left-button gestures only select. A right-button release copies its selection to the
// system clipboard; a stationary right click inside an existing selection preserves
// and copies it. Typing one of the delimiters in editorPairs over a selection wraps the
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
// Deliberately minimal: no keyboard cut/paste; optional literal search is enabled by
// the host through EditorOpts.Search.
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

	dragging                  bool // the active mouse gesture is extending a selection
	preserveSelection         bool // a right press inside the selection is still stationary
	mouseButton               tea.MouseButton
	dragAnchor, dragAnchorEnd textPos // inclusive anchor cell as [start,end)
	selStart, selEnd          textPos // normalized half-open selected buffer range

	clickPos    textPos // where the previous selection-button press landed
	clickButton tea.MouseButton
	clickTime   time.Time // when it landed, for the multi-click window
	clickCount  int       // 1 = caret, 2 = word, 3 = line; 0 ⇒ no press to build on

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
type EditorOpts struct {
	Path        string
	Title       string
	Crumb       string
	Border      bool
	OnExit      func(*core.Shared) core.Action
	OnRelease   func(*core.Shared) core.Action
	OnSaved     func(*core.Shared, string) core.Action
	Highlighter Highlighter
	Search      bool
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

// editorCopiedMsg reports the asynchronous system-clipboard write.
type editorCopiedMsg struct {
	n   int
	err error
}

var writeEditorClipboard = clipboard.WriteAll

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

// editorMultiClickWindow is how long a selection-button press stays eligible to be the
// second or third click of a same-button multi-click. tea.MouseMsg carries neither a
// timestamp nor a click count, so the editor keeps both itself.
const editorMultiClickWindow = 500 * time.Millisecond

// editorControlPlaceholder stands in for a control rune that reached the buffer anyway —
// a file loaded with a lone '\r' or a NUL, which setContent does not strip. One cell wide,
// so cellOfCol/colAtCell (which count every non-tab rune as one) stay exact.
const editorControlPlaceholder = '·'

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
	edit.Crumb = "search"
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
		if m.err != nil {
			return s, core.SetStatusAndLog("copy failed: " + m.err.Error())
		}
		return s, core.SetStatus(fmt.Sprintf("copied %d characters", m.n))
	case tea.KeyMsg:
		return s.key(sh, m)
	case tea.MouseMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		if m.Action == tea.MouseActionPress {
			switch m.Button {
			case tea.MouseButtonLeft, tea.MouseButtonRight:
				s.pressSelection(sh, m.X, m.Y, m.Button, time.Now())
			case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
				// The wheel is browse-only and only while focused: mouse msgs are
				// broadcast to every pane, so an unfocused editor must not roll.
				if s.focused {
					if m.Button == tea.MouseButtonWheelUp {
						s.scrollLines(-editorWheelStep)
					} else {
						s.scrollLines(editorWheelStep)
					}
				}
			}
		} else if s.mouseButton != tea.MouseButtonNone && m.Action == tea.MouseActionMotion {
			s.extendMouseGesture(sh, m.X, m.Y)
		} else if s.mouseButton != tea.MouseButtonNone && m.Action == tea.MouseActionRelease {
			s.extendMouseGesture(sh, m.X, m.Y)
			copy := s.mouseButton == tea.MouseButtonRight
			s.resetMouseGesture()
			if text := s.selectedText(); copy && text != "" {
				return s, copySelectionCmd(text)
			}
		}
		return s, core.Action{}
	}
	return s, core.Action{}
}

// copySelectionCmd is the clipboard write issued by a right-button release that leaves
// a selection.
func copySelectionCmd(text string) core.Action {
	return core.Async(func() tea.Msg {
		return editorCopiedMsg{n: utf8.RuneCountInString(text), err: writeEditorClipboard(text)}
	})
}

// key routes one keystroke. After the exit prompt, a handler registered for the
// buffer's file extension gets first refusal; returning false preserves every default
// below. Editor-local keys are matched as raw strings: ctrl+x / tab / enter are this
// screen's own keys with no core.Keys binding, and the arrows
// match only the raw keycodes (not the k/j/h/l alternates core.Keys.Up et al. carry
// — those letters must stay typable). The word/line editing combos mirror
// bubbles/textinput's KeyMap verbatim (alt+←→ word jumps, alt+⌫ word delete,
// ctrl+u/k line deletes, ctrl+a/e line ends, ctrl+h/d char-delete aliases) so the
// editor behaves like the form field. shift+tab is kept as an alias for tab: it is
// what a form binds to PrevField, so the finger that reaches for it in a field
// shouldn't do nothing here.
func (s *EditorScreen) key(sh *core.Shared, m tea.KeyMsg) (core.Screen, core.Action) {
	k := m.String()
	s.resetMouseGesture()
	s.clickCount = 0 // typing between two clicks makes the second one a fresh first click
	s.clickButton = tea.MouseButtonNone
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

// ---------- buffer editing ----------

// setContent replaces the buffer with loaded file content, marking it clean.
func (s *EditorScreen) setContent(content string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	raw := strings.Split(content, "\n")
	s.lines = make([][]rune, len(raw))
	for i, l := range raw {
		s.lines[i] = []rune(l)
	}
	s.curY, s.curX, s.wantX = 0, 0, 0
	s.scrY, s.scrX = 0, 0
	s.resetMouseGesture()
	s.clickCount = 0
	s.clickButton = tea.MouseButtonNone
	s.clearSelection()
	s.dirty = false
	s.undoStack, s.redoStack = nil, nil
	s.revision, s.savedRevision, s.nextRevision = 0, 0, 1
	s.editSeq++ // the buffer changed even though the load is clean: reparse
	s.wrapDirty = true
}

// editorEditKey identifies keys worth snapshotting before dispatch. A no-op still
// allocates a temporary snapshot, but recordEdit only retains it when editSeq changed.
func editorEditKey(k string, m tea.KeyMsg) bool {
	if len(m.Runes) > 0 {
		return true
	}
	switch k {
	case "tab", "shift+tab", "enter", "backspace", "ctrl+h", "delete", "ctrl+d",
		"alt+backspace", "ctrl+w", "alt+delete", "alt+d", "ctrl+u", "ctrl+k":
		return true
	}
	return false
}

func cloneEditorLines(lines [][]rune) [][]rune {
	cloned := make([][]rune, len(lines))
	for i := range lines {
		cloned[i] = append([]rune(nil), lines[i]...)
	}
	return cloned
}

func (s *EditorScreen) snapshot() editorSnapshot {
	return editorSnapshot{
		lines: cloneEditorLines(s.lines), curY: s.curY, curX: s.curX, wantX: s.wantX,
		selStart: s.selStart, selEnd: s.selEnd, revision: s.revision,
	}
}

func pushEditorSnapshot(stack []editorSnapshot, snap editorSnapshot) []editorSnapshot {
	if len(stack) == editorHistoryLimit {
		copy(stack, stack[1:])
		stack[len(stack)-1] = snap
		return stack
	}
	return append(stack, snap)
}

func (s *EditorScreen) recordEdit(before editorSnapshot) {
	s.undoStack = pushEditorSnapshot(s.undoStack, before)
	s.redoStack = nil
	s.revision = s.nextRevision
	s.nextRevision++
	s.dirty = s.revision != s.savedRevision
}

func (s *EditorScreen) restoreSnapshot(snap editorSnapshot) {
	s.lines = cloneEditorLines(snap.lines)
	s.curY, s.curX, s.wantX = snap.curY, snap.curX, snap.wantX
	s.selStart, s.selEnd = snap.selStart, snap.selEnd
	s.revision = snap.revision
	s.dirty = s.revision != s.savedRevision
	s.resetMouseGesture()
	s.editSeq++
	s.wrapDirty = true
	s.clampScroll()
}

func (s *EditorScreen) undo() {
	if len(s.undoStack) == 0 {
		return
	}
	last := len(s.undoStack) - 1
	target := s.undoStack[last]
	s.undoStack = s.undoStack[:last]
	s.redoStack = pushEditorSnapshot(s.redoStack, s.snapshot())
	s.restoreSnapshot(target)
}

func (s *EditorScreen) redo() {
	if len(s.redoStack) == 0 {
		return
	}
	last := len(s.redoStack) - 1
	target := s.redoStack[last]
	s.redoStack = s.redoStack[:last]
	s.undoStack = pushEditorSnapshot(s.undoStack, s.snapshot())
	s.restoreSnapshot(target)
}

// splitPastedLines turns arbitrary incoming text into the buffer lines it should become.
// Bracketed paste arrives as one KeyMsg carrying the payload verbatim, so this is where
// the line breaks and the runes that have no display cell are dealt with: '\n' ends a
// line, '\r' ends a line and swallows a following '\n' (CRLF), tabs stay raw the way the
// tab key inserts them (expandLine owns their width), and every other control rune is
// dropped — leaving one in the buffer breaks the same row geometry the editorTabWidth
// note describes. Always returns at least one line.
func splitPastedLines(text string) [][]rune {
	out := [][]rune{{}}
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			out = append(out, []rune{})
		case r == '\n':
			out = append(out, []rune{})
		case r == '\t':
			out[len(out)-1] = append(out[len(out)-1], r)
		case r < 0x20 || r == 0x7f:
			// no cell to render it in; drop it
		default:
			out[len(out)-1] = append(out[len(out)-1], r)
		}
	}
	return out
}

// insertText inserts text at the cursor, splitting it across buffer lines. This is the
// path every rune-bearing key takes: ordinary typing is the single-line case and lands in
// insertRunes, while a paste splices its lines in so that one buffer line stays one
// physical row. Counts as a single edit, so undo takes the whole paste back.
func (s *EditorScreen) insertText(text string) {
	parts := splitPastedLines(text)
	if len(parts) == 1 {
		if len(parts[0]) > 0 {
			s.insertRunes(parts[0]...)
		}
		return
	}
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	s.lines[s.curY] = append(line[:s.curX], parts[0]...)
	added := parts[1:]
	last := len(added) - 1
	endX := len(added[last]) // where the caret lands: after the paste, before the tail
	added[last] = append(added[last], tail...)
	// A fresh copy of the lines below: appending added in place would overwrite them.
	rest := append([][]rune{}, s.lines[s.curY+1:]...)
	s.lines = append(append(s.lines[:s.curY+1], added...), rest...)
	s.curY += len(added)
	s.curX = endX
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// insertRunes inserts rs at the cursor and advances past them.
func (s *EditorScreen) insertRunes(rs ...rune) {
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	line = append(line[:s.curX], rs...)
	s.lines[s.curY] = append(line, tail...)
	s.curX += len(rs)
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// newline splits the current line at the cursor; the tail moves to a new line below.
func (s *EditorScreen) newline() {
	line := s.lines[s.curY]
	tail := append([]rune{}, line[s.curX:]...)
	s.lines[s.curY] = line[:s.curX]
	s.lines = append(s.lines, nil)
	copy(s.lines[s.curY+2:], s.lines[s.curY+1:])
	s.lines[s.curY+1] = tail
	s.curY++
	s.curX, s.wantX = 0, 0
	s.dirty = true
	s.editSeq++
}

// backspace deletes the rune before the cursor, or joins the line onto the previous
// one at column 0.
func (s *EditorScreen) backspace() {
	if s.curX > 0 {
		line := s.lines[s.curY]
		s.lines[s.curY] = append(line[:s.curX-1], line[s.curX:]...)
		s.curX--
	} else if s.curY > 0 {
		prev := s.lines[s.curY-1]
		s.curX = len(prev)
		s.lines[s.curY-1] = append(prev, s.lines[s.curY]...)
		s.lines = append(s.lines[:s.curY], s.lines[s.curY+1:]...)
		s.curY--
	} else {
		return
	}
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// forwardDelete deletes the rune under the cursor (delete key), or pulls the next line
// up at end of line.
func (s *EditorScreen) forwardDelete() {
	line := s.lines[s.curY]
	if s.curX < len(line) {
		s.lines[s.curY] = append(line[:s.curX], line[s.curX+1:]...)
	} else if s.curY < len(s.lines)-1 {
		s.lines[s.curY] = append(line, s.lines[s.curY+1]...)
		s.lines = append(s.lines[:s.curY+1], s.lines[s.curY+2:]...)
	} else {
		return
	}
	s.wantX = s.curX
	s.dirty = true
	s.editSeq++
}

// ---------- delimiter pairs ----------

// editorPairs maps an opening delimiter to the one that closes it. Typing a key in this
// table over a selection wraps the selection in the pair; typing it with no selection
// inserts both and leaves the caret between them. Nothing else in the pair story is
// clever: backspace does not swallow the closer, and typing the closing delimiter over
// an auto-inserted one inserts a second.
var editorPairs = map[rune]rune{'(': ')', '[': ']', '{': '}', '*': '*', '_': '_'}

// emphasisPairRune marks the pairs that only auto-close in prose. In code '*' is
// multiplication or a pointer and '_' is an identifier character, so closing them on
// every keystroke would fight ordinary typing; wrapping a SELECTION in them stays
// available everywhere because that gesture is always deliberate.
func emphasisPairRune(r rune) bool { return r == '*' || r == '_' }

// emphasisPairExt reports the file types where emphasisPairRune delimiters auto-close.
func emphasisPairExt(ext string) bool {
	switch ext {
	case ".md", ".markdown":
		return true
	}
	return false
}

// surroundSelection wraps the selection in open/close and keeps the original text
// selected, so repeating the key nests another pair: word → *word* → **word**. The
// closer goes in first — inserting the opener first would shift a single-line
// selection's end column out from under the second splice.
func (s *EditorScreen) surroundSelection(open, close rune) {
	start, end := s.selStart, s.selEnd
	if end.x == 0 && end.y > start.y {
		// A triple-clicked line takes the newline ending it, and a closer at column 0 of
		// the next line reads as wrapping the break rather than the text. Pull it back to
		// the end of the last selected line; the selection narrows to the text it wrapped.
		end = textPos{end.y - 1, len(s.lines[end.y-1])}
	}
	s.lines[end.y] = spliceRune(s.lines[end.y], end.x, close)
	s.lines[start.y] = spliceRune(s.lines[start.y], start.x, open)
	s.selStart = textPos{start.y, start.x + 1}
	if start.y == end.y {
		s.selEnd = textPos{end.y, end.x + 1} // the opener pushed the closer along too
	} else {
		s.selEnd = textPos{end.y, end.x} // a different line: only the closer moved it
	}
	s.curY, s.curX, s.wantX = s.selEnd.y, s.selEnd.x, s.selEnd.x
	s.dirty = true
	s.editSeq++
}

// spliceRune inserts r at column x, returning a line that shares no backing array with
// the original. The editing helpers splice in place around the cursor; this one is for
// the two edits surroundSelection makes away from it.
func spliceRune(line []rune, x int, r rune) []rune {
	out := make([]rune, 0, len(line)+1)
	out = append(out, line[:x]...)
	out = append(out, r)
	return append(out, line[x:]...)
}

// ---------- word/line operations (the bubbles/textinput KeyMap mirror) ----------

// isWordSpace delimits words: whitespace only, the same notion textinput uses (no
// alnum/punct classes — keep it stupidly simple).
func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

// isBackwardDeleteSymbol marks punctuation that forms its own token for
// alt+backspace/ctrl+w. Word movement and forward deletion keep their existing
// whitespace-only behavior; this is intentionally the narrower editing gesture the
// user invokes when peeling a path or expression apart from right to left.
func isBackwardDeleteSymbol(r rune) bool {
	return strings.ContainsRune("()[]{}.,|/", r)
}

// editorWordClass groups runes for double-click selection. The whitespace-only split
// word movement uses is too coarse here — it would take all of "foo.bar(baz)" as one
// word — so punctuation forms its own runs and a double-click on the '.' in "foo.bar"
// takes just the dot.
func editorWordClass(r rune) int {
	switch {
	case isWordSpace(r):
		return 0
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return 1
	default:
		return 2
	}
}

// wordBoundsAt returns the half-open column range of the run of same-class runes around
// col. Unlike wordBackPos/wordForwardPos it is a pure function of the line and does not
// swallow the whitespace next to the word. A column past the end of the line takes the
// last run, which is where a click in the empty space right of the text lands.
func wordBoundsAt(line []rune, col int) (int, int) {
	if len(line) == 0 {
		return 0, 0
	}
	if col >= len(line) {
		col = len(line) - 1
	}
	class := editorWordClass(line[col])
	from, to := col, col+1
	for from > 0 && editorWordClass(line[from-1]) == class {
		from--
	}
	for to < len(line) && editorWordClass(line[to]) == class {
		to++
	}
	return from, to
}

// wordBackPos is the position WordBackward would move to from the cursor: at column 0
// the previous line's end (the caller treats that one as a plain join/move), else
// past any spaces then the word before them.
func (s *EditorScreen) wordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	for x > 0 && !isWordSpace(line[x-1]) {
		x--
	}
	return y, x
}

// wordForwardPos is the position WordForward would move to: at end of line the next
// line's start, else past the rest of the current word then any spaces after it.
func (s *EditorScreen) wordForwardPos() (int, int) {
	y, x := s.curY, s.curX
	line := s.lines[y]
	if x >= len(line) {
		if y >= len(s.lines)-1 {
			return y, len(line)
		}
		return y + 1, 0
	}
	for x < len(line) && !isWordSpace(line[x]) {
		x++
	}
	for x < len(line) && isWordSpace(line[x]) {
		x++
	}
	return y, x
}

// deleteWordBackPos is wordBackPos with punctuation-aware token boundaries. It
// preserves the existing whitespace rule (trailing spaces are removed together with
// the token before them), then removes either one run of configured symbols or one
// run of ordinary word characters. Thus repeated alt+backspace on "file.md" removes
// "md", then ".", then "file".
func (s *EditorScreen) deleteWordBackPos() (int, int) {
	y, x := s.curY, s.curX
	if x == 0 {
		if y == 0 {
			return 0, 0
		}
		return y - 1, len(s.lines[y-1])
	}
	line := s.lines[y]
	for x > 0 && isWordSpace(line[x-1]) {
		x--
	}
	if x == 0 {
		return y, x
	}
	if isBackwardDeleteSymbol(line[x-1]) {
		for x > 0 && isBackwardDeleteSymbol(line[x-1]) {
			x--
		}
		return y, x
	}
	for x > 0 && !isWordSpace(line[x-1]) && !isBackwardDeleteSymbol(line[x-1]) {
		x--
	}
	return y, x
}

func (s *EditorScreen) moveWordBack() {
	y, x := s.wordBackPos()
	s.curY, s.curX, s.wantX = y, x, x
}

func (s *EditorScreen) moveWordForward() {
	y, x := s.wordForwardPos()
	s.curY, s.curX, s.wantX = y, x, x
}

// deleteRange removes the text from (y1, x1) to (y2, x2), merging the two line ends
// into y1 and dropping the lines between. An empty range is a no-op (and stays
// clean); the caller owns the cursor afterwards.
func (s *EditorScreen) deleteRange(y1, x1, y2, x2 int) {
	if y1 == y2 && x1 == x2 {
		return
	}
	s.lines[y1] = append(s.lines[y1][:x1], s.lines[y2][x2:]...)
	s.lines = append(s.lines[:y1+1], s.lines[y2+1:]...)
	s.dirty = true
	s.editSeq++
}

// deleteWordBack is DeleteWordBackward: deletes from deleteWordBackPos to the
// cursor. At column 0 it is a plain line join, exactly like backspace.
func (s *EditorScreen) deleteWordBack() {
	y, x := s.deleteWordBackPos()
	if y == s.curY && x == s.curX {
		return // start of buffer
	}
	if x == len(s.lines[y]) && y == s.curY-1 {
		s.backspace() // column 0: join, not a word delete
		return
	}
	s.deleteRange(y, x, s.curY, s.curX)
	s.curY, s.curX, s.wantX = y, x, x
}

// deleteWordForward is DeleteWordForward: deletes from the cursor to wordForwardPos,
// which pulls the next line up when the cursor sits at end of line.
func (s *EditorScreen) deleteWordForward() {
	y, x := s.wordForwardPos()
	s.deleteRange(s.curY, s.curX, y, x)
}

// ---------- cursor movement ----------

func (s *EditorScreen) moveLeft() {
	if s.curX > 0 {
		s.curX--
	} else if s.curY > 0 {
		s.curY--
		s.curX = len(s.lines[s.curY])
	}
	s.wantX = s.curX
}

func (s *EditorScreen) moveRight() {
	if s.curX < len(s.lines[s.curY]) {
		s.curX++
	} else if s.curY < len(s.lines)-1 {
		s.curY++
		s.curX = 0
	}
	s.wantX = s.curX
}

// moveVertical moves the cursor delta lines, keeping the wantX target column so a
// run of up/down moves across short lines returns to the column the user started from.
//
// Off either end of the buffer the move becomes a horizontal one instead of a no-op:
// down on the last line lands at end of line, up on the first line at column zero.
// That is what makes holding an arrow reach the end of the document rather than stall
// mid-line, and both ends reset wantX — the caret really moved, so the sticky column
// follows it exactly as it does for home/end.
func (s *EditorScreen) moveVertical(delta int) {
	y := s.curY + delta
	if y < 0 {
		s.curX, s.wantX = 0, 0
		return
	}
	if y >= len(s.lines) {
		s.curX = len(s.lines[s.curY])
		s.wantX = s.curX
		return
	}
	s.curY = y
	if s.curX = s.wantX; s.curX > len(s.lines[y]) {
		s.curX = len(s.lines[y])
	}
}

// positionAt maps a mouse cell to a buffer position. Drag events may arrive beyond
// the pane because ModularScreen keeps the gesture with its originating slot; clamp
// keeps those endpoints on the nearest visible text cell without scrolling.
func (s *EditorScreen) positionAt(sh *core.Shared, x, y int, clamp bool) (textPos, bool) {
	if x -= s.insetX(); x < 0 {
		x = 0 // a press left of text reads as column zero, preserving click behavior
	}
	if s.barVisible() && x >= s.textW() {
		if !clamp {
			return textPos{}, false // a press/release on the scrollbar is not buffer text
		}
		x = s.textW() - 1
	}
	if x -= s.numGutterWidth(); x < 0 {
		x = 0 // a click on the line number reads as column 0, as one left of the body does
	}
	if x >= s.contentW() {
		x = s.contentW() - 1
	}
	rel := y - s.insetY()
	if !s.embedded {
		rel -= sh.BodyY() // absolute coordinates: the chrome rows come off too
	}
	if rel < 0 {
		if !clamp {
			return textPos{}, false
		}
		rel = 0
	}
	if rel >= s.h {
		if !clamp {
			return textPos{}, false
		}
		rel = s.h - 1
	}
	row, cell := s.scrY+rel, s.scrX+x
	if s.wrap {
		// The clicked row is a wrapped chunk: it names the line, and its start is the
		// origin the click's column counts from.
		s.rebuildWrapRows()
		r := s.wrapRows[min(row, len(s.wrapRows)-1)]
		row, cell = r.line, r.start+x
	} else if row >= len(s.lines) {
		row = len(s.lines) - 1
	}
	col := colAtCell(s.lines[row], cell)
	return textPos{row, col}, true
}

// clickAt preserves the editor's original single-click behavior. It is kept as a
// small wrapper because tests and callers inside this package use it directly.
func (s *EditorScreen) clickAt(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.resetMouseGesture()
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScroll()
}

// pressSelection handles a left or right selection press. Repeated presses from the
// same button on the same character inside editorMultiClickWindow promote to word and
// line selection; a fourth starts over as a caret/drag gesture. A right press inside
// an existing selection preserves it unless the gesture moves to another character.
// The clock is a parameter so tests can drive click cadence.
func (s *EditorScreen) pressSelection(sh *core.Shared, x, y int, button tea.MouseButton, now time.Time) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		s.clickCount = 0
		s.clickButton = tea.MouseButtonNone
		s.resetMouseGesture()
		return
	}
	continued := s.clickCount > 0 && button == s.clickButton && p == s.clickPos &&
		now.Sub(s.clickTime) < editorMultiClickWindow
	if continued {
		s.clickCount++
	} else {
		s.clickCount = 1
	}
	s.clickPos, s.clickButton, s.clickTime = p, button, now
	s.resetMouseGesture()
	s.mouseButton = button
	switch s.clickCount {
	case 2:
		s.selectWordAt(p)
	case 3:
		s.selectLineAt(p)
	default:
		s.clickCount = 1
		if button == tea.MouseButtonRight && s.positionSelected(p) {
			s.dragAnchor = p
			s.dragAnchorEnd = s.cellEnd(p)
			s.preserveSelection = true
			return
		}
		s.startDragAt(p)
		return
	}
	// Multi-click selection is complete on the press. Keep only mouseButton active so a
	// right release can copy it without motion extending it back onto the anchor.
	s.clampScrollBounds()
}

// positionSelected reports whether the buffer character at p belongs to the current
// half-open selection. For a multiline range, the insertion position after a line's
// final rune represents its selected newline.
func (s *EditorScreen) positionSelected(p textPos) bool {
	return s.selectionActive() && !posLess(p, s.selStart) && posLess(p, s.selEnd)
}

// selectWordAt selects the run of same-class runes under p. A blank line has no run, so
// it stays an ordinary caret placement.
func (s *EditorScreen) selectWordAt(p textPos) {
	from, to := wordBoundsAt(s.lines[p.y], p.x)
	if from == to {
		s.clearSelection()
		s.curY, s.curX, s.wantX = p.y, p.x, p.x
		return
	}
	s.selStart, s.selEnd = textPos{p.y, from}, textPos{p.y, to}
	s.curY, s.curX, s.wantX = p.y, to, to
}

// selectLineAt selects p's whole line including the newline ending it, so the selection
// deletes as a line and copies as one. The last line has no newline to take.
func (s *EditorScreen) selectLineAt(p textPos) {
	end := textPos{p.y, len(s.lines[p.y])}
	if p.y < len(s.lines)-1 {
		end = textPos{p.y + 1, 0}
	}
	s.selStart, s.selEnd = textPos{p.y, 0}, end
	s.curY, s.curX, s.wantX = end.y, end.x, end.x
}

func (s *EditorScreen) startDrag(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, false)
	if !ok {
		return
	}
	s.startDragAt(p)
}

func (s *EditorScreen) startDragAt(p textPos) {
	s.clearSelection()
	s.curY, s.curX, s.wantX = p.y, p.x, p.x
	s.clampScroll()
	s.dragAnchor = p
	s.dragAnchorEnd = s.cellEnd(p)
	s.dragging = true
}

// extendMouseGesture advances an ordinary drag. A pending right click that began in
// selected text changes into a new drag only after the pointer maps to another buffer
// position; motion within another display cell of the same tab/wide rune preserves the
// old selection.
func (s *EditorScreen) extendMouseGesture(sh *core.Shared, x, y int) {
	if s.preserveSelection {
		p, ok := s.positionAt(sh, x, y, true)
		if !ok || p == s.dragAnchor {
			return
		}
		s.preserveSelection = false
		s.clickCount = 0
		s.clickButton = tea.MouseButtonNone
		s.startDragAt(s.dragAnchor)
	}
	if s.dragging {
		s.extendDrag(sh, x, y)
	}
}

func (s *EditorScreen) resetMouseGesture() {
	s.dragging = false
	s.preserveSelection = false
	s.mouseButton = tea.MouseButtonNone
}

func (s *EditorScreen) extendDrag(sh *core.Shared, x, y int) {
	p, ok := s.positionAt(sh, x, y, true)
	if !ok {
		return
	}
	if p == s.dragAnchor {
		s.clearSelection()
		s.curY, s.curX, s.wantX = p.y, p.x, p.x
		return
	}
	end := s.cellEnd(p)
	if posLess(p, s.dragAnchor) {
		s.selStart, s.selEnd = p, s.dragAnchorEnd
		s.curY, s.curX = p.y, p.x
	} else {
		s.selStart, s.selEnd = s.dragAnchor, end
		s.curY, s.curX = end.y, end.x
	}
	if !posLess(s.selStart, s.selEnd) {
		s.clearSelection()
	}
	s.wantX = s.curX
	s.clampScrollBounds()
}

func (s *EditorScreen) cellEnd(p textPos) textPos {
	if p.x < len(s.lines[p.y]) {
		return textPos{p.y, p.x + 1}
	}
	return p
}

func posLess(a, b textPos) bool { return a.y < b.y || a.y == b.y && a.x < b.x }

func (s *EditorScreen) selectionActive() bool { return posLess(s.selStart, s.selEnd) }

func (s *EditorScreen) clearSelection() { s.selStart, s.selEnd = textPos{}, textPos{} }

func (s *EditorScreen) deleteSelection() {
	if !s.selectionActive() {
		return
	}
	start := s.selStart
	s.deleteRange(start.y, start.x, s.selEnd.y, s.selEnd.x)
	s.curY, s.curX, s.wantX = start.y, start.x, start.x
	s.clearSelection()
}

func (s *EditorScreen) selectedText() string {
	if !s.selectionActive() {
		return ""
	}
	if s.selStart.y == s.selEnd.y {
		return string(s.lines[s.selStart.y][s.selStart.x:s.selEnd.x])
	}
	var b strings.Builder
	b.WriteString(string(s.lines[s.selStart.y][s.selStart.x:]))
	for y := s.selStart.y + 1; y < s.selEnd.y; y++ {
		b.WriteByte('\n')
		b.WriteString(string(s.lines[y]))
	}
	b.WriteByte('\n')
	b.WriteString(string(s.lines[s.selEnd.y][:s.selEnd.x]))
	return b.String()
}

// scrollLines moves the viewport delta lines without touching the caret — the
// wheel's browse mode — clamped so the view never passes the buffer's ends. The
// caret may leave the screen; the next caret-moving key snaps the view back to it
// (clampScroll runs after every keystroke).
func (s *EditorScreen) scrollLines(delta int) {
	s.scrY += delta
	s.clampScrollBounds()
}

// clampScrollBounds keeps the scroll offsets inside the buffer WITHOUT chasing the
// caret — the resize-time clamp. The router re-lays out after every message
// (core.Router.Update), so a caret-chasing clamp here (clampScroll) would snap the
// view back on every wheel tick and browse mode could never leave the caret behind.
// Typing or moving the caret re-asserts visibility through key's clampScroll.
func (s *EditorScreen) clampScrollBounds() {
	if m := s.rowCount() - s.h; s.scrY > m {
		s.scrY = m
	}
	if s.scrY < 0 {
		s.scrY = 0
	}
	if s.scrX < 0 {
		s.scrX = 0
	}
}

// clampScroll scrolls the viewport just enough to keep the cursor visible. In normal
// mode it tracks the cursor in both axes; with wrap enabled it keeps only the row on
// screen (soft wrap means the whole wrapped line is visible horizontally).
func (s *EditorScreen) clampScroll() {
	if s.w < 1 || s.h < 1 {
		return
	}
	if s.wrap {
		row := s.wrapRowForCursor()
		if row < s.scrY {
			s.scrY = row
		}
		if row >= s.scrY+s.h {
			s.scrY = row - s.h + 1
		}
		return
	}
	if s.curY < s.scrY {
		s.scrY = s.curY
	}
	if s.curY >= s.scrY+s.h {
		s.scrY = s.curY - s.h + 1
	}
	curCell := cellOfCol(s.lines[s.curY], s.curX)
	if curCell < s.scrX {
		s.scrX = curCell
	}
	if w := s.contentW(); curCell >= s.scrX+w {
		s.scrX = curCell - w + 1
	}
}

// barVisible reports whether the scrollbar column is drawn: only when the buffer
// overflows the viewport. Wrapped, the answer is the one rebuildWrapRows settled while
// measuring the rows — asking again from the row count here would be the same question
// the rebuild already had to answer to pick its width.
func (s *EditorScreen) barVisible() bool {
	if s.wrap {
		s.rebuildWrapRows()
		return s.wrapBar
	}
	return len(s.lines) > s.h
}

// textW is the width the text window gets — one column short of s.w while the
// scrollbar takes the rightmost cell, so the caret can never hide under the bar.
func (s *EditorScreen) textW() int {
	if s.barVisible() {
		return s.w - 1
	}
	return s.w
}

// contentW is what the buffer text itself gets: the text window net of the line-number
// gutter. It is the horizontal window renderLine cuts and clampScroll scrolls, and the
// width buildWrapRows breaks lines at.
func (s *EditorScreen) contentW() int {
	return max(s.textW()-s.numGutterWidth(), 1)
}

// scrollbarCell renders row i of the scrollbar: a thumb sized to the viewport's
// share of the buffer and placed proportionally to scrY, on a full-height track.
// Track and thumb share the one glyph; the color does the talking — the track is
// dimmed, the thumb wears the theme's focus color. The styles are built per call
// so a theme switch repaints, as renderLine's muted style does.
func (s *EditorScreen) scrollbarCell(row int) string {
	total := max(s.rowCount(), 1) // rows, not lines: wrapped, one line can be many
	thumb := max(s.h*s.h/total, 1)
	top := 0
	if d := total - s.h; d > 0 {
		top = min(s.scrY, d) * (s.h - thumb) / d
	}
	color := core.MutedColor
	if row >= top && row < top+thumb {
		color = core.FocusedColor
	}
	if !s.focused {
		color = core.MutedColor
	}
	return lipgloss.NewStyle().Foreground(color).Render("│")
}

// ---------- rendering ----------

// titleH is the title bar's rendered height, subtracted from the body (and from mouse
// rows) the same way ModularScreen accounts for its own title. The focused and muted
// bars render at the same height, so focus never shifts the body.
func (s *EditorScreen) titleH() int {
	return lipgloss.Height(core.RenderTitleBar(s.titleText()))
}

// insetX and insetY are the body's offsets from the screen's own top-left: the chrome
// this editor draws above and left of the first buffer cell. They are the single
// definition SetSize (which subtracts them) and clickAt (which offsets by them) both
// read, so the two can't drift apart.
//
// Left: the frame's border column when bordered, plus the embedded gutter — one blank
// column keeping the text off a neighbouring pane's border.
func (s *EditorScreen) insetX() int {
	x := 0
	if s.bordered {
		x++
	}
	if s.embedded {
		x++
	}
	return x
}

// insetY is the frame's top border row when bordered, else the title bar's height.
func (s *EditorScreen) insetY() int {
	if s.bordered {
		return 1
	}
	return s.titleH()
}

func (s *EditorScreen) baseTitleText() string {
	if s.dirty {
		return s.title + " [+]"
	}
	return s.title
}

func (s *EditorScreen) titleText() string { return s.baseTitleText() }

// searchBarVisible reports whether the bottom rows belong to search: while the
// modal editor is open (including its initially empty state), or afterward while a
// retained query is still filtering the buffer.
func (s *EditorScreen) searchBarVisible() bool {
	return s.searchEnabled && (s.searchEditing || s.searchQuery != "")
}

// searchBar renders the non-interactive version beneath the viewport. The focused
// LineEditScreen is composited directly over it, so both states use the same rounded
// shell and full pane width. Its text is cell-truncated on narrow panes.
func (s *EditorScreen) searchBar() string {
	w := s.paneW()
	contentW := max(w-4, 1) // two border cells and one padding cell on each side
	content := ansi.Truncate("find: "+s.searchQuery, contentW, "…")
	return lineEditBox().Width(contentW + 2).Render(content)
}

// View renders the buffer window under its title, both tracking focus: bordered, the
// title (with its [+] modified marker) is the frame's top-border legend and the frame
// carries the tint; unbordered, it is the title bar above the body, muted while a
// sibling pane holds the keys.
func (s *EditorScreen) View(*core.Shared) string {
	var editor string
	if s.bordered {
		editor = frame(s.titleText(), s.body(), s.w+s.gutter(), s.focused)
	} else {
		editor = core.WithTitleFocused(s.titleText(), s.body(), s.focused)
	}
	if !s.searchBarVisible() {
		return editor
	}
	return lipgloss.JoinVertical(lipgloss.Left, editor, s.searchBar())
}

// gutter is the embedded body's one-column left indent (0 standalone) — the part of
// insetX that lives INSIDE the frame, so View adds it back to the frame's inner run.
func (s *EditorScreen) gutter() int {
	if s.embedded {
		return 1
	}
	return 0
}

// body is the viewport itself: s.h rows of the visible row window and — while the
// exit prompt is up — the prompt as the last row, each indented by the gutter. When
// the buffer overflows, the rightmost column is the scrollbar (rows padded up to it,
// so the bar reads as one solid column). Always exactly s.h lines tall AND exactly
// s.w cells wide, so the frame around it stays rectangular: a row one cell over
// wraps in the terminal and shifts every frame after it.
//
// What a "row" is depends on the mode — a buffer line, or one wrapped chunk of one —
// but only rowCount and renderRow know that, so the two modes cannot drift apart in
// how they pad or where they put the bar.
func (s *EditorScreen) body() string {
	rows := s.h
	if s.confirmExit {
		rows-- // the prompt takes the last body row
	}
	bar := s.barVisible()
	total := s.rowCount()
	pad := strings.Repeat(" ", s.gutter())
	var b strings.Builder
	for i := 0; i < rows; i++ {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(pad)
		if row := s.scrY + i; row < total {
			line := s.renderRow(row)
			b.WriteString(line)
			if bar {
				b.WriteString(strings.Repeat(" ", max(s.textW()-lipgloss.Width(line), 0)))
			}
		} else if bar {
			b.WriteString(strings.Repeat(" ", s.textW()))
		}
		if bar {
			b.WriteString(s.scrollbarCell(i))
		}
	}
	if s.confirmExit {
		if rows > 0 {
			b.WriteByte('\n')
		}
		prompt := editorPromptStyle.Render("Save modified buffer? (y)es (n)o (c)ancel")
		b.WriteString(pad + prompt)
		if bar {
			b.WriteString(strings.Repeat(" ", max(s.textW()-lipgloss.Width(prompt), 0)))
			b.WriteString(s.scrollbarCell(s.h - 1))
		}
	}
	return b.String()
}

// rowCount is how many rows the viewport scrolls through — what scrY indexes and what
// the scrollbar measures itself against: wrapped display rows while wrap is on, buffer
// lines while it is off.
func (s *EditorScreen) rowCount() int {
	if s.wrap {
		return s.wrapTotalRows()
	}
	return len(s.lines)
}

// renderRow renders row i of rowCount's sequence.
func (s *EditorScreen) renderRow(i int) string {
	if s.wrap {
		return s.renderWrappedRow(i)
	}
	return s.renderLine(i)
}

// gutterOn reports whether the line-number column is drawn: the sticky ctrl+l
// preference, or unconditionally while wrapped — in wrapped text the numbers are the
// only thing separating a soft break from a real line, so wrap turns them on without
// disturbing the preference the toggle goes back to.
func (s *EditorScreen) gutterOn() bool { return s.lineNums || s.wrap }

// numGutterWidth returns the fixed width of the line-number prefix when it is drawn.
// It is just wide enough for the highest line number in the buffer, so short docs do
// not waste columns and tall docs stay aligned. A viewport too narrow to hold the
// numbers and any text gets none: the text wins.
//
// It must not consult textW — that reads barVisible, which under wrap reads the row
// cache, which is measured against this width.
func (s *EditorScreen) numGutterWidth() int {
	if !s.gutterOn() {
		return 0
	}
	digits := 1
	for n := len(s.lines); n >= 10; n /= 10 {
		digits++
	}
	w := digits + 1 // a trailing space separates the number from the text
	if w > s.w-2 {
		return 0
	}
	return w
}

// lineNumText is the gutter cell for one display row: the 1-based line number on a
// line's first row, blanks on its wrapped continuations.
func (s *EditorScreen) lineNumText(line int, first bool) string {
	w := s.numGutterWidth()
	if w == 0 {
		return ""
	}
	if !first {
		return strings.Repeat(" ", w)
	}
	return fmt.Sprintf("%*d ", w-1, line+1)
}

// rebuildWrapRows recomputes the display rows, and with them whether the scrollbar
// column is needed. Nothing it calls may consult textW or barVisible: the text width
// derives from the bar, the bar derives from the row count, and the row count is what
// this builds — reading either here recurses until the stack gives out. So the width is
// settled directly instead: measure at the full width, and if the document overflows,
// measure again one column narrower to make room for the bar. Narrowing can only add
// rows, never remove them, so an overflow at the full width is still an overflow at the
// narrower one and the second pass is final.
//
// The cache is invalidated (wrapDirty) by every edit, resize and toggle.
func (s *EditorScreen) rebuildWrapRows() {
	if !s.wrapDirty {
		return
	}
	s.wrapDirty = false // cleared first: nothing below may re-enter the rebuild
	s.wrapBar = false
	s.buildWrapRows(s.w - s.numGutterWidth())
	if len(s.wrapRows) > s.h {
		s.wrapBar = true
		s.buildWrapRows(s.w - 1 - s.numGutterWidth())
	}
}

// buildWrapRows fills the row cache, breaking each buffer line into chunks of at most w
// display cells. A line whose width is an exact multiple of w (an empty line included)
// gets a trailing empty row: without it the caret at end of line would have to sit one
// column past the last chunk, which is off the frame.
func (s *EditorScreen) buildWrapRows(w int) {
	if w < 1 {
		w = 1
	}
	s.wrapRows = s.wrapRows[:0]
	for i, line := range s.lines {
		n := len(expandLine(line))
		for start := 0; start < n; start += w {
			s.wrapRows = append(s.wrapRows, wrapRow{i, start, min(start+w, n)})
		}
		if n%w == 0 {
			s.wrapRows = append(s.wrapRows, wrapRow{i, n, n})
		}
	}
}

// wrapTotalRows is the number of display rows the wrapped buffer occupies.
func (s *EditorScreen) wrapTotalRows() int {
	s.rebuildWrapRows()
	return len(s.wrapRows)
}

// wrapRowForCursor is the display row holding the caret, FOUND in the same cache the
// render reads rather than recomputed from the wrap width — the two agreeing is what
// keeps the caret on the row it is drawn on.
func (s *EditorScreen) wrapRowForCursor() int {
	s.rebuildWrapRows()
	cell := cellOfCol(s.lines[s.curY], s.curX)
	last := 0
	for i, r := range s.wrapRows {
		if r.line != s.curY {
			continue
		}
		if cell < r.end {
			return i
		}
		last = i // end of line: the line's last row owns the caret
	}
	return last
}

// renderWrappedRow renders display row idx: its line-number gutter (numbered on the
// line's first row, blank on its continuations) and its chunk of the line, with the
// caret when this is the row the caret is on. The chunk's start is the window origin
// here, exactly as scrX is in the unwrapped render.
func (s *EditorScreen) renderWrappedRow(idx int) string {
	r := s.wrapRows[idx]
	line := s.lines[r.line]
	disp := expandLine(line)
	start, end := min(r.start, len(disp)), min(r.end, len(disp))
	num := s.lineNumText(r.line, r.start == 0)
	// A caret one cell past this chunk is only THIS row's to draw when the line ends
	// here. Mid-line it belongs to the next row, at its column 0.
	eol := s.lastRowOfLine(idx)

	if s.focused && s.hl != nil {
		if styled, ok := s.renderLineStyled(r.line, start, end, eol); ok {
			return num + styled
		}
	}
	return num + s.renderLinePlain(r.line, start, end, eol)
}

// lastRowOfLine reports whether display row idx is the final one of its buffer line.
func (s *EditorScreen) lastRowOfLine(idx int) bool {
	return idx == len(s.wrapRows)-1 || s.wrapRows[idx+1].line != s.wrapRows[idx].line
}

// renderLine renders one buffer row's horizontal window in display cells (tabs
// expanded via expandLine — the raw '\t' never reaches the frame), behind the line
// number gutter when it is on and narrowed by it (contentW), with the cursor
// cell (a reverse-video rune, or a blank at end of line) when the row holds the
// cursor. A cursor sitting on a tab reverses the expansion's first cell.
//
// With a Highlighter set (and focused), the window renders through the spans
// instead: contiguous same-style runs, tabs carrying their span's style through
// the expansion, the cursor cell still reverse-video — the cursor wins over the
// syntax style, exactly as it wins over plain text. Styles never change cell
// widths, so the styled render measures the same as the plain one.
//
// Unfocused the whole window goes muted and the cursor is dropped: a caret in a pane
// the keys don't reach reads as a lie about where typing lands, and one caret per
// pane would leave nothing marking the live one. The muted style is built per call so
// a theme switch repaints it, as styleHelp and StyleList do.
func (s *EditorScreen) renderLine(row int) string {
	disp := expandLine(s.lines[row])
	start := s.scrX
	if start > len(disp) {
		start = len(disp)
	}
	end := s.scrX + s.contentW()
	if end > len(disp) {
		end = len(disp)
	}
	num := s.lineNumText(row, true)
	if s.focused && s.hl != nil {
		if styled, ok := s.renderLineStyled(row, start, end, true); ok {
			return num + styled
		}
	}
	return num + s.renderLinePlain(row, start, end, true)
}

// renderLinePlain applies the muted/unfocused, selection, and caret layers to a
// display-cell window. Selection is measured in rune columns but converted to cells,
// so every cell of an expanded tab receives the same background.
func (s *EditorScreen) renderLinePlain(row, start, end int, eol bool) string {
	disp := expandLine(s.lines[row])
	vis := disp[start:end]
	c := -1
	if s.focused && row == s.curY {
		c = cellOfCol(s.lines[row], s.curX) - start
	}
	muted := lipgloss.NewStyle().Foreground(core.MutedColor)
	selected := lipgloss.NewStyle().Background(core.MutedColor).Foreground(core.OnFocusedColor)
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := s.cellSelected(row, start+i)
		match := s.cellMatched(row, start+i)
		j := i + 1
		for j < len(vis) && j != c && s.cellSelected(row, start+j) == sel && s.cellMatched(row, start+j) == match {
			j++
		}
		style := lipgloss.NewStyle()
		styled := false
		if !s.focused {
			style = muted
			styled = true
		}
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
			styled = true
		} else if match {
			style = s.editorSearchStyle()
			styled = true
		}
		if styled {
			b.WriteString(style.Render(string(vis[i:j])))
		} else {
			b.WriteString(string(vis[i:j]))
		}
		i = j
	}
	if eol && end-start < s.contentW() {
		switch {
		case c == len(vis):
			b.WriteString(editorCursorStyle.Render(" "))
		case s.newlineSelected(row):
			b.WriteString(selected.Render(" "))
		}
	}
	return b.String()
}

func (s *EditorScreen) cellSelected(row, cell int) bool {
	if !s.selectionActive() || row < s.selStart.y || row > s.selEnd.y {
		return false
	}
	from, to := 0, len(s.lines[row])
	if row == s.selStart.y {
		from = s.selStart.x
	}
	if row == s.selEnd.y {
		to = s.selEnd.x
	}
	return cell >= cellOfCol(s.lines[row], from) && cell < cellOfCol(s.lines[row], to)
}

// newlineSelected reports whether the half-open range crosses the newline following
// row. Rendering one dim blank makes multiline selections and selected empty lines
// visible without putting a newline rune into the terminal output.
func (s *EditorScreen) newlineSelected(row int) bool {
	return s.selectionActive() && row >= s.selStart.y && row < s.selEnd.y
}

// rebuildSearchMatches refreshes the per-line match cache when either the query or
// buffer changes. Search is literal, case-insensitive and line-local because the
// input itself is single-line. Advancing by the query width makes results
// non-overlapping, matching conventional find behavior.
func (s *EditorScreen) rebuildSearchMatches() {
	if s.searchSeq == s.editSeq && s.searchCached == s.searchQuery {
		return
	}
	s.searchSeq, s.searchCached = s.editSeq, s.searchQuery
	s.searchMatches = make([][]textRange, len(s.lines))
	query := []rune(s.searchQuery)
	if !s.searchEnabled || len(query) == 0 {
		return
	}
	for row, line := range s.lines {
		for from := 0; from+len(query) <= len(line); {
			to := from + len(query)
			if strings.EqualFold(string(line[from:to]), s.searchQuery) {
				s.searchMatches[row] = append(s.searchMatches[row], textRange{
					from: cellOfCol(line, from),
					to:   cellOfCol(line, to),
				})
				from = to
				continue
			}
			from++
		}
	}
}

// cellMatched reports whether one display cell belongs to a search match. Cached
// ranges are sorted, so the lookup narrows to the first range ending after cell
// instead of scanning every match on a common-character search.
func (s *EditorScreen) cellMatched(row, cell int) bool {
	if s.searchQuery == "" || row < 0 || row >= len(s.lines) {
		return false
	}
	s.rebuildSearchMatches()
	matches := s.searchMatches[row]
	lo, hi := 0, len(matches)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if matches[mid].to <= cell {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo < len(matches) && cell >= matches[lo].from
}

// editorSearchStyle is deliberately distinct from selection: a focused match uses
// the accent, while an unfocused pane mutes it along with the rest of the editor.
func (s *EditorScreen) editorSearchStyle() lipgloss.Style {
	bg := core.FocusedColor
	if !s.focused {
		bg = core.MutedColor
	}
	return lipgloss.NewStyle().Background(bg).Foreground(core.OnFocusedColor)
}

// hlSpans answers the row's validated spans, reparsing the buffer first when it
// changed since the last parse — lazy and once per edit sequence, never per
// frame or per row. nil means "render plain": the row is unstyled, or the spans
// failed validation (their concatenated text must reconstruct the buffer line
// exactly — the check that keeps a buggy highlighter from corrupting the frame).
func (s *EditorScreen) hlSpans(row int) []Span {
	if s.hlSeq != s.editSeq {
		s.hl.Parse(s.Text())
		s.hlSeq = s.editSeq
	}
	spans := s.hl.HighlightLine(row)
	if spansText(spans) != string(s.lines[row]) {
		return nil
	}
	return spans
}

// renderLineStyled renders the row's window [start, end) through the
// highlighter's spans — start being the window's origin in display cells, scrX
// unwrapped and the chunk's start wrapped, so the caret lands in the right window
// either way. Per-rune span indexes ride through the tab expansion
// (a tab's cells take its span's style), contiguous same-span runs render in
// one style.Render, and the cursor cell splices in reverse-video — at end of
// line, as the appended styled blank, which only a window the line actually ends
// in (eol) may draw. ok=false falls back to the plain render.
func (s *EditorScreen) renderLineStyled(row, start, end int, eol bool) (string, bool) {
	spans := s.hlSpans(row)
	if spans == nil {
		return "", false
	}
	line := s.lines[row]
	// Cells sharing a span share its style, so the run grouping compares span
	// indexes — never lipgloss.Style values (they carry a func field, so == does
	// not even compile).
	idx := make([]int, len(line))
	pos := 0
	for i, sp := range spans {
		n := utf8.RuneCountInString(sp.Text)
		for c := pos; c < pos+n && c < len(idx); c++ {
			idx[c] = i
		}
		pos += n
	}
	var drunes []rune
	var didx []int
	for i, r := range line {
		if r == '\t' {
			for k := 0; k < editorTabWidth; k++ {
				drunes = append(drunes, ' ')
				didx = append(didx, idx[i])
			}
		} else {
			drunes = append(drunes, r)
			didx = append(didx, idx[i])
		}
	}
	vis, vidx := drunes[start:end], didx[start:end]
	c := -1 // no cursor splice off the cursor row
	if row == s.curY {
		c = cellOfCol(line, s.curX) - start // start is the window origin in BOTH modes
	}
	var b strings.Builder
	for i := 0; i < len(vis); {
		if i == c {
			b.WriteString(editorCursorStyle.Render(string(vis[i])))
			i++
			continue
		}
		sel := s.cellSelected(row, start+i)
		match := s.cellMatched(row, start+i)
		j := i + 1
		for j < len(vis) && j != c && vidx[j] == vidx[i] && s.cellSelected(row, start+j) == sel && s.cellMatched(row, start+j) == match {
			j++
		}
		style := spans[vidx[i]].Style
		if sel {
			style = style.Background(core.MutedColor).Foreground(core.OnFocusedColor)
		} else if match {
			style = s.editorSearchStyle()
		}
		b.WriteString(style.Render(string(vis[i:j])))
		i = j
	}
	if eol && end-start < s.contentW() {
		switch {
		case row == s.curY && c == len(vis): // only the window the line ends in
			b.WriteString(editorCursorStyle.Render(" "))
		case s.newlineSelected(row):
			b.WriteString(lipgloss.NewStyle().Background(core.MutedColor).Foreground(core.OnFocusedColor).Render(" "))
		}
	}
	return b.String(), true
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
		key.NewBinding(key.WithKeys("alt+left", "alt+right"), key.WithHelp("⌥←→", "word")),
		key.NewBinding(key.WithKeys("alt+backspace"), key.WithHelp("⌥⌫", "del word")),
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
