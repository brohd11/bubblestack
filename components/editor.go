package components

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atotto/clipboard"
)

// EditorScreen is the simple nano-like text editor: it loads a file (or starts empty),
// lets the user type freely, and exits on ctrl+x with a "save modified buffer?"
// three-way prompt when the buffer is dirty (n = discard & exit, esc/c = cancel, and
// y = a filename prompt seeded with the current name — nano's "File Name to Write",
// so saving under a different name is a save-as). Enter splits lines (and may be
// extended by a handler registered for the file type), tab (or shift+tab, on a
// standalone screen — see key) inserts a tab except over a selection spanning lines,
// where it indents every one of them (alt+, and alt+. dedent and indent the same span,
// alt+i cycles the unit — see editor_indent.go),
// the arrows move the cursor, ctrl+z/ctrl+y undo and redo logical key events,
// alt+c/alt+x/alt+v copy, cut and paste through the system clipboard (with no selection
// the line the cursor is on is the target), and the left mouse button places the cursor
// and selects with drag/double/triple click.
// The right button is the context menu, when the host opts into it (EditorOpts.ContextMenu):
// a press raises a MenuScreen at the pointer offering copy/cut/paste — pressing inside an
// existing selection acts on it, pressing outside puts the caret there first, so a paste
// lands where the pointer did. Typing a configured surrounding delimiter over a selection wraps the
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

	hl         Highlighter // latest exact syntax snapshot; nil ⇒ plain render
	hlFactory  func() Highlighter
	hlExplicit bool   // hl came from EditorOpts: a rename must not replace it
	editSeq    int    // bumped at every buffer mutation
	hlSeq      int    // edit sequence represented by hl (-1 ⇒ never)
	hlEpoch    uint64 // language identity; rejects a parse finishing after a rename
	hlRows     []int  // current row → row in hl; -1 means text affected by an edit
	hlPreview  map[int][]Span
	hlPrevSeq  int
	hlPrevFrom int
	hlPrevTo   int
	hlDirty    int // earliest row whose lexical state may have changed; -1 ⇒ exact
	hlAnchor   int // best current-buffer restart row for the provisional parse
	hlFar      bool
	hlParsing  bool
	hlJob      uint64
	hlChanged  time.Time // latest edit; exact parsing waits for a quiet window
	hlDebounce time.Duration
	textCache  string // lines joined for text consumers at textSeq
	textSeq    int    // editSeq represented by textCache (-1 ⇒ stale)

	resolveLanguage EditorLanguageResolver // host-owned path → behavior seam
	autoPairs       map[rune]rune          // typed opener → closer; nil ⇒ literal typing
	surroundPairs   map[rune]rune          // selected opener → closer; nil ⇒ replacement
	onEnter         EditorEnterHandler     // structured newline; nil ⇒ plain split
	lineComment     string                 // the profile's line-comment delimiter; "" ⇒ none
	blockComment    [2]string              // wrapping pair the toggle falls back to

	indentMode          IndentMode // which unit the block gestures use (see editor_indent.go)
	indentWidth         int        // spaces per level under IndentSpaces
	indentWidthExplicit bool       // that width came from EditorOpts: a rename must not replace it
	autoIndentSpaces    int        // what the resolved profile asks for; 0 ⇒ a literal tab
	indentGuides        bool       // render leading indent levels without changing buffer geometry

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

	signColumns map[string]*signColumn // host-named per-line decoration columns (see editor_signs.go)
	signOrder   []string               // outermost to innermost; registration order for unlisted columns

	dragging                  bool    // the active mouse gesture is extending a selection
	dragAnchor, dragAnchorEnd textPos // inclusive anchor cell as [start,end)
	selStart, selEnd          textPos // normalized half-open selected buffer range

	clickPos   textPos   // where the previous left press landed
	clickTime  time.Time // when it landed, for the multi-click window
	clickCount int       // 1 = caret, 2 = word, 3 = line; 0 ⇒ no press to build on

	contextMenu  bool                          // EditorOpts.ContextMenu: a right press raises the edit menu
	contextItems func(*core.Shared) []MenuItem // host rows appended to that menu below a rule

	undoStack, redoStack                  []editorHistoryEntry
	activeEdit                            *editorHistoryEntry
	revision, savedRevision, nextRevision uint64
	completion                            *editorCompletionSession

	searchEnabled bool          // EditorOpts.Search: ctrl+f and match rendering are available
	searchEditing bool          // the modal line edit is open; keeps its bottom rows reserved even while empty
	searchQuery   string        // live query; retained after enter or escape
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

// editorState is the small non-text state restored at one side of a history entry.
// Viewport browsing is deliberately absent, as it was from the old snapshots.
type editorState struct {
	curY, curX, wantX int
	selStart, selEnd  textPos
	revision          uint64
}

// editorChange is one reversible replacement made during a logical key event. Text is
// immutable and proportional to the replacement rather than the rest of the buffer.
type editorChange struct {
	start             textPos
	deleted, inserted string
}

type editorHistoryEntry struct {
	changes       []editorChange
	before, after editorState
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
// bar dropping its (*) marker is the only feedback, which is all a standalone editor
// needs.
//
// Border draws the shared frame (the ScrollContainer look) with the title as its
// top-edge legend instead of the title bar, the same opt-in ListPanelOpts.Border
// carries: which chrome an instance wears is the composing caller's choice, not the
// embedder's, so the same screen can be framed in one layout and plain in another.
// Default off — an editor denotes focus by muting its text either way.
//
// Highlighter adds syntax coloring. Set explicitly, it wins over any highlighter
// returned by ResolveLanguage and survives save-as/SetPath; as a direct instance it
// keeps the legacy synchronous debounce. Left nil, the active language config may use
// its factory for bounded immediate previews and asynchronous exact snapshots. With
// neither source the editor renders plain. Highlighting is render-only: styles never
// change cell widths, and a highlighter whose spans don't reconstruct the line exactly
// is ignored (plain render), so the frame contract — no raw tabs, rectangular body —
// can't be broken by one.
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
//
// Indent and IndentWidth pick the unit the BLOCK gestures use (see editor_indent.go); a
// plain tab keypress always inserts a literal '\t' regardless. The zero Indent is
// IndentAuto, which reads the unit from ResolveLanguage; without a profile it uses a
// literal tab. IndentWidth is the spaces per level under IndentSpaces; left at zero it
// follows the profile, and set explicitly it survives a save-as rename the way an
// explicit Highlighter does.
//
// ResolveLanguage is the only path-derived behavior seam. It may provide pairs,
// structured Enter, an automatic indent unit and a highlighter factory. A nil resolver
// or nil result leaves the editor literal, and the resolver is consulted again after a
// save-as or SetPath changes the path.
//
// IndentGuides draws a muted vertical guide in the leading whitespace occupied by each
// complete live indent unit. It is render-only and defaults off.
type EditorOpts struct {
	Path            string
	Title           string
	Crumb           string
	Border          bool
	OnExit          func(*core.Shared) core.Action
	OnRelease       func(*core.Shared) core.Action
	OnSaved         func(*core.Shared, string) core.Action
	Highlighter     Highlighter
	ResolveLanguage EditorLanguageResolver
	Search          bool
	ContextMenu     bool
	ContextItems    func(*core.Shared) []MenuItem
	Indent          IndentMode
	IndentWidth     int
	IndentGuides    bool
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

// editorHighlightMsg wakes the editor after an edit's quiet window. It is addressed
// because async messages may reach a different editor after a pane switch.
type editorHighlightMsg struct {
	target *EditorScreen
	seq    int
}

// editorHighlightReadyMsg carries an independently parsed snapshot back to the editor.
// job identifies the single in-flight worker; epoch and seq guard language/buffer moves.
type editorHighlightReadyMsg struct {
	target *EditorScreen
	job    uint64
	epoch  uint64
	seq    int
	hl     Highlighter
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

// editorHighlightDebounce coalesces full-document parsers while the user is typing.
const editorHighlightDebounce = 250 * time.Millisecond

// editorHighlightPreviewLines bounds work allowed to run synchronously in the input
// path. A farther multiline opener is left to the exact background snapshot.
const editorHighlightPreviewLines = 128

// editorHistoryLimit bounds each stack in logical edit events.
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

// editorIndentGuide replaces one existing leading-whitespace cell when guides are
// enabled. Like the overflow and control marks it is exactly one display cell wide.
const editorIndentGuide = '│'

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
var _ core.Receiver = (*EditorScreen)(nil)
var _ PaneOriginer = (*EditorScreen)(nil)

// NewEditorScreen builds the screen with an empty buffer; a configured Path is read
// asynchronously from the FIRST Init (the framework idiom — IO only in the cmd lane).
// Later Inits never re-read the file; they may resume an outstanding factory-backed
// highlight parse, while the instance continues to hold the buffer for its lifetime.
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
	ed := &EditorScreen{
		path:            opts.Path,
		title:           title,
		crumb:           crumb,
		onExit:          opts.OnExit,
		onRelease:       opts.OnRelease,
		onSaved:         opts.OnSaved,
		lines:           [][]rune{{}},
		bordered:        opts.Border,
		focused:         true, // standalone the editor is always focused; a panel blurs it
		hl:              hl,
		hlFactory:       nil,
		hlExplicit:      hlExplicit,
		resolveLanguage: opts.ResolveLanguage,
		hlSeq:           -1, // nothing parsed yet, even before the first edit
		hlPrevSeq:       -1,
		hlPrevFrom:      -1,
		hlPrevTo:        -1,
		hlDirty:         0,
		hlAnchor:        0,
		hlDebounce:      editorHighlightDebounce,
		textSeq:         -1,
		wrapDirty:       true, // no rows measured yet, even before the first edit
		nextRevision:    1,
		searchEnabled:   opts.Search,
		searchSeq:       -1,
		contextMenu:     opts.ContextMenu,
		contextItems:    opts.ContextItems,

		indentMode:          opts.Indent,
		indentWidth:         opts.IndentWidth,
		indentWidthExplicit: opts.IndentWidth > 0,
		indentGuides:        opts.IndentGuides,
	}
	ed.applyLanguage(opts.Path)
	return ed
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
// The file read happens once per instance. A host that swaps this editor back into a
// pane calls Init again (ScreenPanel.SetChild does), and re-reading there would hand
// setContent the file — discarding unsaved edits, the undo history and the cursor of the
// very buffer the swap-back exists to return to. The flag is set before the empty-path
// return, so a scratch buffer that later gains a path (a save-as, or SetPath after the
// host renamed the file) is never read back off disk either: in both cases the buffer is
// already the authoritative content. It is set at dispatch rather than on arrival, so a
// second Init while the first read is in flight cannot queue a duplicate.
func (s *EditorScreen) Init(*core.Shared) tea.Cmd {
	if s.loaded {
		s.refreshHighlightPreview()
		return s.startHighlightParse()
	}
	s.loaded = true
	if s.path == "" {
		s.refreshHighlightPreview()
		return s.startHighlightParse()
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
	if s.textSeq == s.editSeq {
		return s.textCache
	}
	var b strings.Builder
	for i, l := range s.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(string(l))
	}
	s.textCache = b.String()
	s.textSeq = s.editSeq
	return s.textCache
}

// SetText replaces the buffer with content, exactly as a completed load does: the caret
// returns to the top, the undo history is dropped and the buffer is clean (setContent).
//
// It also marks the editor LOADED, which is the point of it. Init's read comes back as an
// editorLoadedMsg, and the router delivers a message only to the TOP screen — so a host
// that pushes something over this editor before that read lands loses it, leaving an empty
// buffer pointed at a file that is not empty. A host already holding the document (it read
// the file to show it somewhere else) seeds the buffer here instead, and Init then
// dispatches no read at all rather than one whose result goes nowhere.
//
// Setting the flag first matters only when this runs BEFORE Init, which is the case it
// exists for; afterwards Init has already set it.
func (s *EditorScreen) SetText(content string) {
	s.loaded = true
	s.setContent(content)
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
	return s.buildSearchEdit(sh, true)
}

// buildSearchEdit creates the focused overlay. ctrl+f asks it to seed from a
// single-line selection; clicking the retained bar does not, because that gesture means
// "continue editing this query" and must not silently replace it with selected text.
func (s *EditorScreen) buildSearchEdit(sh *core.Shared, seedSelection bool) *LineEditScreen {
	initial := s.searchQuery
	if selected := s.selectedText(); seedSelection && selected != "" && !strings.ContainsRune(selected, '\n') {
		initial = selected
		s.searchQuery = selected
	}
	s.searchEditing = true
	x, y, w, h := s.paneGeometry(sh)
	y += max(h-editorSearchBarH, 0)
	edit := NewLineEdit("search", x, y, w,
		func(_ *core.Shared, query string) core.Action {
			s.searchQuery = query
			s.searchEditing = false
			return core.Pop()
		}, func(*core.Shared) core.Action {
			// Search is live while the field is open, so escape is another way to
			// accept the current value rather than rolling the visible matches back.
			s.searchEditing = false
			return core.Pop()
		})
	edit.SetPrompt("find: ")
	edit.SetValue(initial)
	edit.SetCursorBlink(false)
	edit.Help = []key.Binding{} // keep the overlay to the shared component's slim shape
	edit.OnChange = func(_ *core.Shared, query string) core.Action {
		s.searchQuery = query
		return core.Action{}
	}
	return edit
}

// searchBarHit reports whether an incoming pane-relative (embedded) or absolute
// (standalone) mouse cell lies in the retained three-row search box.
func (s *EditorScreen) searchBarHit(sh *core.Shared, x, y int) bool {
	if !s.searchBarVisible() || s.searchEditing {
		return false
	}
	ax, ay := s.absCell(sh, x, y)
	px, py, w, h := s.paneGeometry(sh)
	top := py + max(h-editorSearchBarH, 0)
	return ax >= px && ax < px+w && ay >= top && ay < py+h
}

func (s *EditorScreen) highlightDelay() time.Duration {
	if s.hlDebounce <= 0 {
		return editorHighlightDebounce
	}
	return s.hlDebounce
}

func (s *EditorScreen) highlightWait(now time.Time) time.Duration {
	if s.hlChanged.IsZero() {
		return 0
	}
	return max(s.hlChanged.Add(s.highlightDelay()).Sub(now), 0)
}

func (s *EditorScreen) parseHighlight() {
	if s.hl == nil || s.hlSeq == s.editSeq {
		return
	}
	s.hl.Parse(s.Text())
	s.acceptHighlight(s.hl, s.editSeq)
}

// Update handles the async load/save results, mouse presses, and keystrokes — in the
// exit prompt's mode only its y/n/esc/c answers are live. An edit made here schedules a
// repaint at the end of the highlighter's quiet window without replacing another cmd.
func (s *EditorScreen) Update(sh *core.Shared, msg tea.Msg) (screen core.Screen, action core.Action) {
	seq := s.editSeq
	epoch := s.hlEpoch
	scroll := s.scrY
	defer func() {
		if s.hlEpoch != epoch && s.hlFactory != nil {
			s.refreshHighlightPreview()
			action.Cmd = tea.Batch(action.Cmd, s.startHighlightParse())
		} else if s.hl != nil && s.editSeq != seq && !s.hlChanged.IsZero() {
			s.refreshHighlightPreview()
			action.Cmd = tea.Batch(action.Cmd, s.highlightRefreshCmd(s.editSeq, s.highlightDelay()))
		} else if s.hlFactory != nil && s.hlSeq != s.editSeq && s.scrY != scroll {
			s.refreshHighlightPreview()
		}
	}()
	switch m := msg.(type) {
	case editorHighlightMsg:
		return s, s.handleHighlightWake(m)
	case editorHighlightReadyMsg:
		return s, s.handleHighlightReady(m)
	case editorLoadedMsg:
		if m.err == nil {
			s.setContent(m.content)
			s.refreshHighlightPreview()
			action.Cmd = s.startHighlightParse()
		}
		// A read error (missing file, permissions) leaves the empty buffer: the first
		// save creates the file, as nano does.
		return s, action
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
	// A bracketed terminal paste. v1 delivered it as a rune-bearing key with Paste
	// set, so the whole key path had to keep guarding against reading a payload as
	// typing; v2 gives it a message of its own, which lands here and shares the
	// clipboard paste's insert — one undo step, selection replaced, no auto-pairing,
	// and the structured Enter hook never sees it.
	case tea.PasteMsg:
		if s.confirmExit || m.Content == "" {
			return s, core.Action{}
		}
		s.resetMouseGesture()
		s.clickCount = 0
		s.editAtomic(func() {
			s.deleteSelection() // a no-op without a selection
			s.insertText(m.Content)
		})
		return s, core.Action{}
	case tea.KeyPressMsg:
		return s.key(sh, m)
	// v2 splits the old Action field into one message type per kind of event; the
	// arms below are the same three cases in that shape.
	case tea.MouseClickMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		s.cancelCompletionSession()
		mm := m.Mouse()
		switch mm.Button {
		case tea.MouseLeft:
			if s.searchBarHit(sh, mm.X, mm.Y) {
				s.resetMouseGesture()
				s.clickCount = 0
				return s, core.Push(s.buildSearchEdit(sh, false))
			}
			if row, ok := s.scrollbarRowAt(sh, mm.X, mm.Y); ok {
				s.resetMouseGesture()
				s.clickCount = 0
				s.scrollToBarRow(row)
				return s, core.Action{}
			}
			if editorExtendClick(mm) {
				s.extendSelectionTo(sh, mm.X, mm.Y)
			} else {
				s.pressSelection(sh, mm.X, mm.Y, time.Now())
			}
		case tea.MouseRight:
			if s.contextMenu {
				return s, s.pressContext(sh, mm.X, mm.Y)
			}
		}
		return s, core.Action{}
	case tea.MouseWheelMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		// The wheel is browse-only and only while focused: mouse msgs are
		// broadcast to every pane, so an unfocused editor must not roll.
		if s.focused {
			s.wheel(m.Mouse())
		}
		return s, core.Action{}
	// A drag is the only state motion and release can act on now that the left button
	// is the only one that starts a gesture. Neither ever arrives while the context
	// menu is up: the menu is the top screen, and it consumes every message.
	case tea.MouseMotionMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		if s.dragging {
			mm := m.Mouse()
			s.extendDrag(sh, mm.X, mm.Y)
		}
		return s, core.Action{}
	case tea.MouseReleaseMsg:
		if s.confirmExit {
			return s, core.Action{}
		}
		if s.dragging {
			mm := m.Mouse()
			s.extendDrag(sh, mm.X, mm.Y)
		}
		s.resetMouseGesture()
		return s, core.Action{}
	}
	return s, core.Action{}
}

// editorTypedRune reports the single printable rune a key press typed, and false for
// anything else. v2 populates Key.Text only for keys that stand for printable
// characters — never for special keys or modifier combos — so it replaces v1's
// len(Runes) == 1 && !Alt idiom outright. A bracketed paste can no longer reach here
// carrying a whole payload either: v2 delivers it as tea.PasteMsg.
func editorTypedRune(m tea.KeyPressMsg) (rune, bool) {
	r := []rune(m.Text)
	if len(r) != 1 {
		return 0, false
	}
	return r[0], true
}

// editorExtendClick reports whether a left press EXTENDS the selection rather than
// starting a fresh one. It is a predicate of its own, on one line, on purpose: shift is
// the modifier terminals reserve for bypassing mouse reporting to run their own text
// selection (the same fact that put the wheel's sideways mode on alt — see wheel), so on
// many terminals this press never reaches us at all. If that proves too many of them,
// moving the gesture to alt is this line and nothing else.
func editorExtendClick(m tea.Mouse) bool { return m.Mod.Contains(tea.ModShift) }

// copySelectionCmd is the clipboard write the menu's Copy and Cut rows issue. The write
// travels in the cmd lane because atotto shells out to pbcopy/xclip, which must never run
// inside Update.
func (s *EditorScreen) key(sh *core.Shared, m tea.KeyPressMsg) (core.Screen, core.Action) {
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
	if s.handleCompletionKey(k, m) {
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
	// The shifted motions are matched HERE, above the selection pre-pass below: that
	// switch clears the selection for every UNSHIFTED move, so extending one has to be
	// decided before the code that would throw it away. They are above the editorEditKey
	// gate too — they touch no text, so they take no undo step — and above language Enter
	// hook, because a selection gesture is not language-profile business.
	//
	// The early return skips the tail's wrapDirty (nothing moved in the buffer), which is
	// why selectFrom does its own clampScroll.
	if move := s.selectMove(k); move != nil {
		s.extendSelection(move)
		return s, core.Action{}
	}
	if editorEditKey(k, m) {
		entry := s.beginHistory()
		defer s.finishHistory(entry)
	}
	// Wrapping the selection has to be decided before the pre-pass below, which deletes
	// the selection for every rune-bearing key. The history transaction above is already
	// open (a rune key is always an edit key), so both insertions land in one step.
	if r, typed := editorTypedRune(m); typed && s.selectionActive() {
		if closer, ok := s.surroundPairs[r]; ok {
			s.surroundSelection(r, closer)
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
		case "tab":
			// The one gesture that reads a selection as lines rather than as text to
			// replace. shift+tab is deliberately NOT here: it is core.Keys.PaneNext and
			// never reaches an embedded editor at all, so hanging an edit on it would be
			// a chord that works in one host and silently navigates in the other.
			if first, last := s.indentSpan(); last > first {
				s.shiftSelectionIndent(1)
				s.wrapDirty = true
				s.clampScroll()
				return s, core.Action{}
			}
			s.deleteSelection()
		case "shift+tab", "enter":
			s.deleteSelection()
		case "up", "down", "left", "right", "alt+left", "ctrl+left", "alt+b",
			"alt+right", "ctrl+right", "alt+f", "home", "ctrl+a", "end", "ctrl+e":
			s.clearSelection()
		default:
			if m.Text != "" {
				s.deleteSelection()
			}
		}
	}
	if k == "enter" && s.languageEnter() {
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
	case "alt+.":
		s.shiftSelectionIndent(1)
	case "alt+,":
		s.shiftSelectionIndent(-1)
	// ctrl+_ is what a terminal sends for ctrl+/ — the chord puts byte 0x1f on the wire and
	// the key decoder names that ctrl+_. Reporting a literal "ctrl+/" needs the Kitty
	// keyboard protocol, so alt+/ is bound beside it as the form that always arrives.
	case "ctrl+_", "alt+/":
		s.toggleComment()
	case "alt+i":
		return s, core.SetStatus(s.cycleIndentMode())
	case "tab", "shift+tab":
		s.insertRunes('\t')
	case "enter":
		s.newline()
	case "backspace", "ctrl+h":
		if !s.deleteEmptyAutoPair() {
			s.backspace()
		}
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
			s.deleteRange(s.curY, s.curX, s.curY, len(s.lines[s.curY]))
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
		s.moveHome()
	case "end", "ctrl+e":
		s.moveEnd()
	default:
		// A single typed opening delimiter brings its closer with it and leaves the caret
		// between the two. The one-rune guard keeps a bracketed paste (one KeyMsg carrying
		// the whole payload) on the insertText path below.
		if r, typed := editorTypedRune(m); typed {
			if closer, ok := s.autoPairs[r]; ok {
				s.insertRunes(r, closer)
				s.curX--
				s.wantX = s.curX
				break
			}
		}
		// Every unmodified rune-bearing key, typed or pasted: a bracketed paste is one
		// KeyMsg whose Runes carry newlines, so this must go through insertText, not
		// insertRunes. Unknown Alt runes are control chords, never text — besides avoiding
		// accidental chord insertion, that is the editor-side guard against a truncated
		// terminal escape sequence reaching a screen outside bubblestack.Run's filter.
		if m.Text != "" {
			s.insertText(m.Text)
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
		key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "indent block")),
		key.NewBinding(key.WithKeys("alt+,", "alt+."), key.WithHelp("alt+, .", "dedent/indent")),
		// Helped as ctrl+/ because that is the chord pressed; ctrl+_ is only the name the
		// byte it sends decodes to, and nobody reaches for underscore to comment a line.
		key.NewBinding(key.WithKeys("ctrl+_", "alt+/"), key.WithHelp("ctrl+/", "comment")),
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "newline")),
		key.NewBinding(key.WithKeys("up", "down", "left", "right"), key.WithHelp("↑↓←→", "move")),
		key.NewBinding(key.WithKeys("shift+left", "shift+right", "shift+up", "shift+down",
			"shift+home", "shift+end", "ctrl+shift+left", "ctrl+shift+right"),
			key.WithHelp("shift+←→", "select")),
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

// Dirty reports unsaved changes in the buffer — what the (*) title marker shows —
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
