// Package editor is bubblestack's text editor screen: syntax highlighting, undo, block
// indent, comment toggling, search, LSP-shaped completion and edits, and host sign
// columns. It composes the shared components (MenuScreen, LineEditScreen, DialogScreen,
// Frame). Start at New and Opts; the other files split one type by concern.
package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/components"
	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/bubblestack/internal/clipboard"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Screen is a nano-like editor. ctrl+x exits, prompting to save a dirty buffer (y opens
// a save-as box seeded with the current name, n discards, esc/c cancels). ctrl+z/ctrl+y
// undo and redo; alt+c/x/v copy, cut and paste (the whole line with no selection); tab
// over a multi-line selection indents it (alt+, / alt+. / alt+i: see indent.go). The
// left mouse button places the caret and selects (drag, double, triple click); the
// right one raises a copy/cut/paste menu when Opts.ContextMenu is set. The wheel
// scrolls without moving the caret.
//
// It reports Filtering at all times so the router's single-key shortcuts never steal
// typed text. Embedded in a pane layout (SetEmbedded), mouse coordinates are
// pane-relative and focus shows by muting the body; the host's pane keys still leave it.
//
// The buffer is a hand-rolled lines/cursor/scroll model rather than bubbles/textarea:
// click-to-cursor needs the scroll offset, and tabs are stored raw but never rendered raw
// (see expandLine).
type Screen struct {
	path    string // file to load/save; empty ⇒ unsavable scratch buffer
	baseDir string // directory a relative save-as name resolves against; empty ⇒ the process cwd
	title   string // title-bar text (defaults to the file's base name, else "Editor")
	crumb   string // breadcrumb segment; defaults to title

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

	hl               Highlighter // latest exact syntax snapshot; nil ⇒ plain render
	hlFactory        func() Highlighter
	hlExplicit       bool   // hl came from Opts: a rename must not replace it
	editSeq          int    // bumped at every buffer mutation
	hlSeq            int    // edit sequence represented by hl (-1 ⇒ never)
	hlEpoch          uint64 // language identity; rejects a parse finishing after a rename
	hlRows           []int  // current row → row in hl; -1 means text affected by an edit
	hlPreview        map[int][]Span
	hlPrevSeq        int
	hlPrevFrom       int
	hlPrevTo         int
	hlDirty          int // earliest row whose lexical state may have changed; -1 ⇒ exact
	hlAnchor         int // best current-buffer restart row for the provisional parse
	hlAnchorSnapshot int // exact-snapshot row corresponding to hlAnchor; -1 when unknown
	hlFar            bool
	hlParsing        bool
	hlJob            uint64
	hlChanged        time.Time // latest edit; exact parsing waits for a quiet window
	hlDebounce       time.Duration
	hlOverlay        [][]highlightOverlayRange // immutable host overlay, addressed through hlOverlayRows
	hlOverlayRows    []int                     // current row → row in hlOverlay; -1 means edited
	textCache        string                    // lines joined for text consumers at textSeq
	textSeq          int                       // editSeq represented by textCache (-1 ⇒ stale)
	lineEnding       string                    // serialized newline: "\r\n" for a pure CRLF load, otherwise "\n"

	resolveLanguage LanguageResolver // host-owned path → behavior seam
	autoPairs       map[rune]rune    // typed opener → closer; nil ⇒ literal typing
	surroundPairs   map[rune]rune    // selected opener → closer; nil ⇒ replacement
	onEnter         EnterHandler     // structured newline; nil ⇒ plain split
	lineComment     string           // the profile's line-comment delimiter; "" ⇒ none
	blockComment    [2]string        // wrapping pair the toggle falls back to

	indentMode          IndentMode // which unit the block gestures use (see indent.go)
	indentWidth         int        // spaces per level under IndentSpaces
	indentWidthExplicit bool       // that width came from Opts: a rename must not replace it
	autoIndentSpaces    int        // what the resolved profile asks for; 0 ⇒ a literal tab
	indentGuides        bool       // render leading indent levels without changing buffer geometry

	bordered  bool // Opts.Border: draw the frame instead of the title bar
	hideTitle bool // host supplies the document label (for example, in a tab bar)
	embedded  bool // one pane of a layout (core.Embeddable): pane-relative mouse, gutter
	focused   bool // false ⇒ muted body, no cursor (core.FocusableScreen); true standalone

	originX, originY int  // the pane's absolute top-left (components.PaneOriginer)
	hasOrigin        bool // false standalone ⇒ the save-as box spans the full width

	w, h  int // viewport dims in cells (the body net of editor chrome), set by SetSize
	paneH int // full height assigned to the editor, including title/frame and search bar

	wrap          bool      // soft-wrap long lines to the viewport width
	lineNums      bool      // line numbers, independent of wrapping
	wrapRows      []wrapRow // the wrapped display rows scrY indexes while wrap is on
	wrapBar       bool      // whether those rows overflow the viewport — resolved by rebuildWrapRows
	wrapDirty     bool      // the wrap cache needs a rebuild: an edit, a resize or a toggle moved it
	wrapGoal      int       // desired display column for consecutive wrapped vertical moves
	wrapGoalValid bool

	// The geometry SetSize last laid out, so an unchanged re-size costs nothing. See
	// SetSize; sizeDirty is how a change that is not a dimension (SetEmbedded) forces it.
	lastSizeW     int
	lastSizeH     int
	lastSearchBar bool
	sizeDirty     bool

	signColumns map[string]*signColumn // host-named per-line decoration columns (see signs.go)
	signOrder   []string               // outermost to innermost; registration order for unlisted columns

	dragging                  bool    // the active mouse gesture is extending a selection
	dragAnchor, dragAnchorEnd textPos // inclusive anchor cell as [start,end)
	dragX, dragY              int     // the last pointer cell, in the frame positionAt reads
	dragScrY, dragScrX        int     // the view offsets that cell was last resolved against (trackDrag)
	dragScrolling             bool    // an auto-scroll tick is in flight for that pointer
	dragSeq                   int     // bumped on reset AND tick consumption, so duplicate broadcasts die
	selStart, selEnd          textPos // normalized half-open selected buffer range

	clickPos   textPos   // where the previous left press landed
	clickTime  time.Time // when it landed, for the multi-click window
	clickCount int       // 1 = caret, 2 = word, 3 = line; 0 ⇒ no press to build on

	contextMenu  bool                                     // Opts.ContextMenu: a right press raises the edit menu
	contextItems func(*core.Shared) []components.MenuItem // host rows appended to that menu below a rule

	undoStack, redoStack                  []editorHistoryEntry
	activeEdit                            *editorHistoryEntry
	revision, savedRevision, nextRevision uint64
	completion                            *editorCompletionSession

	searchEnabled bool          // Opts.Search: ctrl+f and match rendering are available
	searchEditing bool          // the modal line edit is open; keeps its bottom rows reserved even while empty
	searchQuery   string        // live query; retained after enter or escape
	searchSeq     int           // editSeq represented by searchMatches (-1 means stale)
	searchCached  string        // query represented by searchMatches
	searchMatches [][]textRange // per-line, non-overlapping display-cell ranges
}

// textPos is an insertion position in the rune buffer. Selection ranges are stored as
// [start,end); mouse endpoint cells are converted to these positions before sorting.
type textPos struct{ y, x int }

// textRange is a half-open range of display cells within one buffer line.
type textRange struct{ from, to int }

// editorState is the small non-text state restored at one side of a history entry.
// Viewport browsing position is deliberately not restored.
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

// wrapRow is one display row of a soft-wrapped line: the half-open cell range
// [start, end) of expandLine's output. A line ending exactly on the margin gets a
// trailing empty row so the caret has a cell at end of line.
type wrapRow struct{ line, start, end int }

// Opts configures a Screen.
//
//   - Path is the file to edit; a missing one starts empty and the first save creates it.
//     Title and Crumb default to its base name.
//   - OnExit replaces the exit navigation (core.Pop). Set it when embedded: a Pop would
//     dismiss the host layout.
//   - OnRelease binds esc to "keys go elsewhere, buffer stays" (a pane host's focus move).
//     Nil leaves esc unbound.
//   - OnSaved is called with the path written after a ctrl+s save (not the exit
//     prompt's), so a host can follow a save-as rename.
//   - Border draws the shared frame with the title as its legend.
//   - Highlighter overrides ResolveLanguage's highlighter and survives save-as.
//     Highlighters that do not reconstruct the line exactly are ignored.
//   - Search enables ctrl+f literal search over a bar reserved at the bottom edge.
//   - ContextMenu enables the right-click menu; ContextItems appends host rows to it,
//     consulted per press. Rows should leave Hint empty.
//   - Indent/IndentWidth pick the unit block indent uses (tab always types '\t'); the
//     zero value reads it from ResolveLanguage.
//   - ResolveLanguage supplies pairs, structured Enter, indent unit and highlighter for a
//     path, and is consulted again when the path changes.
//   - IndentGuides draws muted guides in leading whitespace.
//   - Wrap and LineNumbers independently enable soft wrapping and line numbers.
type Opts struct {
	Path string
	// BaseDir resolves a relative name typed into the save box (usually the directory the
	// host opened in). Empty uses the process cwd.
	BaseDir         string
	Title           string
	HideTitle       bool // omit the title bar or border legend; retain breadcrumb identity
	Crumb           string
	Border          bool
	OnExit          func(*core.Shared) core.Action
	OnRelease       func(*core.Shared) core.Action
	OnSaved         func(*core.Shared, string) core.Action
	Highlighter     Highlighter
	ResolveLanguage LanguageResolver
	Search          bool
	ContextMenu     bool
	ContextItems    func(*core.Shared) []components.MenuItem
	Indent          IndentMode
	IndentWidth     int
	IndentGuides    bool
	Wrap            bool
	LineNumbers     bool
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

// editorPastedMsg carries a clipboard read back to Update. target names the requesting
// editor: async messages reach every panel of a ModularScreen, and a paste must not land
// in a sibling.
type editorPastedMsg struct {
	target *Screen
	text   string
	err    error
}

// editorHighlightMsg wakes the editor after an edit's quiet window. It is addressed
// because async messages may reach a different editor after a pane switch.
type editorHighlightMsg struct {
	target *Screen
	seq    int
}

// editorDragScrollMsg is the auto-scroll clock for a drag held past the pane edge (motion
// events stop when the pointer does). Addressed to one editor, since siblings receive
// every tick too.
type editorDragScrollMsg struct {
	target *Screen
	seq    int
}

// editorHighlightReadyMsg carries an independently parsed snapshot back to the editor.
// job identifies the single in-flight worker; epoch and seq guard language/buffer moves.
type editorHighlightReadyMsg struct {
	target *Screen
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

// editorTabWidth is the display width of a tab. Raw '\t' must never reach View: the
// terminal expands it while the renderer counts zero width, shifting every later frame.
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

// editorHWheelStep is the cells one horizontal wheel notch scrolls (about a word).
const editorHWheelStep = 6

// editorHCaretNearPct and editorHCaretFarPct bound the caret's column, both measured from
// the right edge of the window: scroll right when nearer than the first, left when
// further than the second. The gap between them keeps the view still while typing.
const (
	editorHCaretNearPct = 10
	editorHCaretFarPct  = 30
)

// editorDragEdgePct is the band at each pane edge, as a share of the viewport, where a
// held drag auto-scrolls.
const editorDragEdgePct = 5

// editorDragScrollInterval is the auto-scroll frame, fast enough to feel continuous.
const editorDragScrollInterval = 50 * time.Millisecond

// editorDragScrollUnitY/X are the slowest auto-scroll step at the inner edge of the band;
// editorDragScrollMaxUnits caps the ramp once the pointer is well past the pane.
const (
	editorDragScrollUnitY    = 1
	editorDragScrollUnitX    = 2
	editorDragScrollMaxUnits = 3
)

// editorMultiClickWindow is how long a press stays eligible to be the second or third
// click; tea.MouseMsg carries no timestamp or click count.
const editorMultiClickWindow = 500 * time.Millisecond

// editorControlPlaceholder replaces a control rune in the buffer (a lone '\r' or NUL from
// a loaded file). One cell wide, so cellOfCol/colAtCell stay exact.
const editorControlPlaceholder = '·'

// editorOverflowMark marks a line cut off at the right edge (unwrapped only). clampScroll
// keeps the caret out of its column.
const editorOverflowMark = '~'

// editorIndentGuide replaces one existing leading-whitespace cell when guides are
// enabled. Like the overflow and control marks it is exactly one display cell wide.
const editorIndentGuide = '│'

// expandLine renders a buffer line to display runes: tabs expanded, other control runes
// replaced, so display cells equal rune indexes. Double-width runes are not handled.
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

// colAtCell maps a display cell back to a rune column, the inverse of cellOfCol. A
// click inside a tab's expansion lands on the tab.
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

var _ core.Screen = (*Screen)(nil)
var _ core.Filterer = (*Screen)(nil)
var _ core.Crumber = (*Screen)(nil)
var _ core.Embeddable = (*Screen)(nil)
var _ core.FocusableScreen = (*Screen)(nil)
var _ core.Receiver = (*Screen)(nil)
var _ components.PaneOriginer = (*Screen)(nil)

// New builds the screen with an empty buffer; a configured Path is read asynchronously by
// the first Init.
func New(opts Opts) *Screen {
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
	ed := &Screen{
		path:            opts.Path,
		baseDir:         opts.BaseDir,
		title:           title,
		crumb:           crumb,
		onExit:          opts.OnExit,
		onRelease:       opts.OnRelease,
		onSaved:         opts.OnSaved,
		lines:           [][]rune{{}},
		bordered:        opts.Border,
		hideTitle:       opts.HideTitle,
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
		wrap:            opts.Wrap,
		lineNums:        opts.LineNumbers,
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
func (s *Screen) exit(sh *core.Shared) core.Action {
	if s.onExit != nil {
		return s.onExit(sh)
	}
	return core.Pop()
}

// Init starts the file read, once per instance: a host swapping this editor back into a
// pane calls Init again, and a re-read would discard unsaved edits. The flag is set at
// dispatch, and also for an empty path, so a buffer that later gains one is never read
// back off disk.
func (s *Screen) Init(*core.Shared) tea.Cmd {
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

// SetEmbedded implements core.Embeddable: embedded, mouse coordinates are pane-relative
// and the body indents one column off the pane edge. Opts.Border still decides the frame.
func (s *Screen) SetEmbedded(on bool) {
	if s.embedded != on {
		s.embedded = on
		s.sizeDirty = true // insetX moved: the next SetSize must recompute, same size or not
	}
}

// SetFocused implements core.FocusableScreen. Unfocused, the body and (unbordered) title
// are muted and the caret is hidden.
func (s *Screen) SetFocused(focused bool) {
	s.focused = focused
	if !focused {
		s.resetMouseGesture()
	}
}

// SetTitleVisible changes title chrome without replacing the buffer. An
// unbordered editor reclaims the title rows immediately; a frame keeps its border.
func (s *Screen) SetTitleVisible(visible bool) {
	if s.hideTitle == !visible {
		return
	}
	s.hideTitle = !visible
	s.sizeDirty = true
	if s.lastSizeW > 0 {
		s.SetSize(nil, s.lastSizeW, s.lastSizeH)
	}
}

// Text is the buffer joined with '\n'. saveCmd restores a CRLF file's endings on disk.
func (s *Screen) Text() string {
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

// SetText replaces the buffer as a completed load does (caret to top, history dropped,
// clean) and marks it loaded, so Init reads nothing. A host already holding the document
// uses it because the router delivers a load result only to the top screen, and it may
// have pushed something over this editor before the read landed.
func (s *Screen) SetText(content string) {
	s.loaded = true
	s.setContent(content)
}

// SetPaneOrigin implements components.PaneOriginer: ScreenPanel forwards the host layout's
// rendered origin, which the save-as overlay anchors from (see saveAsEdit).
func (s *Screen) SetPaneOrigin(x, y int) {
	s.originX, s.originY, s.hasOrigin = x, y, true
}

// Filtering reports true at all times: the editor types every printable key. ctrl+c
// remains the router's quit.
func (s *Screen) Filtering() bool { return true }

// CrumbLabel contributes the screen's breadcrumb segment (title, or the short crumb
// when one was configured).
func (s *Screen) CrumbLabel(short bool) string {
	return components.CrumbSegment(short, "", s.crumb, s.title)
}

const editorSearchBarH = 3 // one input row plus the rounded box's top and bottom borders

// searchEdit builds the ctrl+f line edit over the reserved bottom bar; the editor keeps
// only the live query.
func (s *Screen) searchEdit(sh *core.Shared) *components.LineEditScreen {
	return s.buildSearchEdit(sh, true)
}

// buildSearchEdit creates the search overlay. ctrl+f seeds it from a one-line selection;
// clicking the retained bar continues the existing query instead.
func (s *Screen) buildSearchEdit(sh *core.Shared, seedSelection bool) *components.LineEditScreen {
	initial := s.searchQuery
	if selected := s.selectedText(); seedSelection && selected != "" && !strings.ContainsRune(selected, '\n') {
		initial = selected
		s.searchQuery = selected
	}
	s.searchEditing = true
	x, y, w, h := s.paneGeometry(sh)
	y += max(h-editorSearchBarH, 0)
	edit := components.NewLineEdit("search", x, y, w,
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
func (s *Screen) searchBarHit(sh *core.Shared, x, y int) bool {
	if !s.searchBarVisible() || s.searchEditing {
		return false
	}
	ax, ay := s.absCell(sh, x, y)
	px, py, w, h := s.paneGeometry(sh)
	top := py + max(h-editorSearchBarH, 0)
	return ax >= px && ax < px+w && ay >= top && ay < py+h
}

func (s *Screen) highlightDelay() time.Duration {
	if s.hlDebounce <= 0 {
		return editorHighlightDebounce
	}
	return s.hlDebounce
}

func (s *Screen) highlightWait(now time.Time) time.Duration {
	if s.hlChanged.IsZero() {
		return 0
	}
	return max(s.hlChanged.Add(s.highlightDelay()).Sub(now), 0)
}

func (s *Screen) parseHighlight() {
	if s.hl == nil || s.hlSeq == s.editSeq {
		return
	}
	s.hl.Parse(s.Text())
	s.acceptHighlight(s.hl, s.editSeq)
}

// Update handles load/save results, mouse and keys (only y/n/esc/c while the exit prompt
// is up). An edit schedules a repaint after the highlighter's quiet window.
func (s *Screen) Update(sh *core.Shared, msg tea.Msg) (screen core.Screen, action core.Action) {
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
	case tea.BlurMsg:
		s.resetMouseGesture()
		return s, core.Action{}
	case editorDragScrollMsg:
		return s, s.handleDragScroll(sh, m)
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
		// An exit-prompt save ends the session; a ctrl+s save keeps the buffer, caret and scroll.
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
	// A bracketed paste shares the clipboard paste's insert: one undo step, selection
	// replaced, no auto-pairing, no structured Enter.
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
			mm := m.Mouse()
			s.wheel(mm)
			// Mid-drag, a wheel notch grows the selection over what the view revealed, as an
			// auto-scroll frame does.
			if s.dragging {
				return s, core.Async(s.trackDrag(sh, mm.X, mm.Y))
			}
		}
		return s, core.Action{}
	// Motion and release only matter during a left drag. The context menu, when up, is the
	// top screen and consumes them.
	case tea.MouseMotionMsg:
		if m.Button != tea.MouseLeft {
			s.resetMouseGesture() // the release was lost; the button is no longer held
			return s, core.Action{}
		}
		if s.confirmExit {
			return s, core.Action{}
		}
		if s.dragging {
			mm := m.Mouse()
			return s, core.Async(s.trackDrag(sh, mm.X, mm.Y))
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

// editorTypedRune reports the single printable rune a key typed. Key.Text is set only for
// printable keys, never for special keys, modifier chords or pastes.
func editorTypedRune(m tea.KeyPressMsg) (rune, bool) {
	r := []rune(m.Text)
	if len(r) != 1 {
		return 0, false
	}
	return r[0], true
}

// editorExtendClick reports whether a left press extends the selection. Many terminals
// keep shift+click for their own text selection, so it may never arrive; switching the
// gesture to alt is a one-line change here.
func editorExtendClick(m tea.Mouse) bool { return m.Mod.Contains(tea.ModShift) }

// key routes one keystroke. Editor-local keys are matched as raw strings (arrows match
// only the arrow keycodes so k/j/h/l stay typable); the word and line chords mirror
// bubbles' textinput. shift+tab aliases tab only on a standalone editor: in a pane it is
// the host's pane key.
func (s *Screen) key(sh *core.Shared, m tea.KeyPressMsg) (core.Screen, core.Action) {
	k := m.String()
	if k != "up" && k != "down" && k != "shift+up" && k != "shift+down" {
		s.wrapGoalValid = false
	}
	s.resetMouseGesture()
	s.clickCount = 0 // typing between two clicks makes the second one a fresh first click
	if s.confirmExit {
		return s, s.exitPromptKey(sh, k)
	}
	if act, ok := s.commandKey(sh, k, m); ok {
		return s, act
	}
	if editorEditKey(k, m) {
		entry := s.beginHistory()
		defer s.finishHistory(entry)
	}
	if s.selectionKey(k, m) || k == "enter" && s.languageEnter() {
		s.wrapDirty = true
		s.clampScroll()
		return s, core.Action{}
	}
	act, settle := s.editKey(sh, k, m)
	if settle {
		s.wrapDirty = true
		s.clampScroll()
	}
	return s, act
}

// exitPromptKey answers the "save before exit?" prompt.
func (s *Screen) exitPromptKey(sh *core.Shared, k string) core.Action {
	switch k {
	case "y", "Y":
		s.saveExits = true
		return core.Push(s.saveAsEdit(sh))
	case "n", "N":
		s.confirmExit = false
		return s.exit(sh)
	case "esc", "c":
		s.confirmExit = false
	}
	return core.Action{}
}

// commandKey handles keys that run before the selection pre-pass and outside the edit
// history: completion, search, undo/redo, the clipboard chords (alt runes the pre-pass
// would treat as typing) and the shifted selection motions.
func (s *Screen) commandKey(sh *core.Shared, k string, m tea.KeyPressMsg) (core.Action, bool) {
	if s.handleCompletionKey(k, m) {
		return core.Action{}, true
	}
	switch {
	case s.searchEnabled && k == "ctrl+f":
		return core.Push(s.searchEdit(sh)), true
	case k == "ctrl+z":
		s.undo()
		return core.Action{}, true
	case k == "ctrl+y":
		s.redo()
		return core.Action{}, true
	// alt+c/x/v, since ctrl+c is the router's quit and ctrl+x this screen's exit.
	case k == "alt+c":
		return s.copyOrCut(false), true
	case k == "alt+x":
		return s.copyOrCut(true), true
	case k == "alt+v":
		return pasteClipboardCmd(s), true
	}
	if move := s.selectMove(k); move != nil {
		s.extendSelection(move) // selectFrom does its own clampScroll
		return core.Action{}, true
	}
	return core.Action{}, false
}

// selectionKey applies a key to an active selection: a typed opener surrounds it,
// deletions and a multi-line tab consume the key, motions clear it, and typing replaces
// it. It reports whether the key was fully handled.
func (s *Screen) selectionKey(k string, m tea.KeyPressMsg) bool {
	if !s.selectionActive() {
		return false
	}
	if r, typed := editorTypedRune(m); typed {
		if closer, ok := s.surroundPairs[r]; ok {
			s.surroundSelection(r, closer)
			return true
		}
	}
	switch k {
	case "backspace", "ctrl+h", "delete", "ctrl+d", "alt+backspace", "ctrl+w",
		"alt+delete", "alt+d", "ctrl+u", "ctrl+alt+backspace", "ctrl+alt+h", "ctrl+k":
		s.deleteSelection()
		return true
	case "tab":
		// Tab indents a multi-line selection. Not shift+tab: that is core.Keys.PaneNext
		// and never reaches an embedded editor.
		if first, last := s.indentSpan(); last > first {
			s.shiftSelectionIndent(1)
			return true
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
	return false
}

// editKey is the editing and motion keymap. settle is false for keys that leave the
// buffer untouched and return straight away.
func (s *Screen) editKey(sh *core.Shared, k string, m tea.KeyPressMsg) (act core.Action, settle bool) {
	switch k {
	case "ctrl+x":
		if !s.dirty {
			return s.exit(sh), false
		}
		s.confirmExit = true
	case "ctrl+s":
		// A plain save on the prefilled path, or a save-as when edited; offered on a clean
		// buffer too, as the way to fork a doc to a new name.
		s.saveExits = false
		return core.Push(s.saveAsEdit(sh)), false
	case "esc":
		if s.onRelease != nil {
			return s.onRelease(sh), false
		}
	case "alt+.":
		s.shiftSelectionIndent(1)
	case "alt+,":
		s.shiftSelectionIndent(-1)
	// ctrl+_ is what terminals send for ctrl+/; alt+/ is the form that always arrives.
	case "ctrl+_", "alt+/":
		s.toggleComment()
	case "alt+i":
		return core.SetStatus(s.cycleIndentMode()), false
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
	// Clear the line up to the caret (cmd+backspace); at column 0 it joins like backspace.
	// ctrl+alt+h is ctrl+alt+backspace on terminals that send BS; ctrl+u works everywhere.
	case "ctrl+u", "ctrl+alt+backspace", "ctrl+alt+h":
		if s.curX == 0 {
			s.backspace()
		} else {
			s.deleteRange(s.curY, 0, s.curY, s.curX)
			s.curX, s.wantX = 0, 0
		}
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
		// A single typed opener brings its closer and leaves the caret between them; a
		// bracketed paste (many runes in one key) goes through insertText instead.
		if r, typed := editorTypedRune(m); typed {
			if closer, ok := s.autoPairs[r]; ok {
				s.insertRunes(r, closer)
				s.curX--
				s.wantX = s.curX
				break
			}
		}
		// Unmodified text only: alt runes are chords, never text, which also stops a
		// truncated escape sequence from being typed.
		if m.Text != "" {
			s.insertText(m.Text)
		}
	}
	return core.Action{}, true
}

// HelpView shows the editing hints, swapped for the prompt's y/n/c answers while the
// exit prompt is up.
func (s *Screen) HelpView(sh *core.Shared) string {
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

// HelpBindings is the editor's shortcut set, exported so a host's help can list it.
func (s *Screen) HelpBindings() []key.Binding {
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
		// "alt+" rather than "⌥", matching how every other modifier is spelled.
		key.NewBinding(key.WithKeys("alt+left", "alt+right", "alt+b", "alt+f"), key.WithHelp("alt+←→", "word")),
		key.NewBinding(key.WithKeys("alt+backspace"), key.WithHelp("alt+backspace", "del word")),
		// Labelled as pressed: ctrl+alt+h is how the chord decodes on terminals without the
		// Kitty protocol, and ctrl+u also arrives where no modified backspace does.
		key.NewBinding(key.WithKeys("ctrl+alt+backspace", "ctrl+alt+h", "ctrl+u"),
			key.WithHelp("ctrl+alt+backspace", "clear to line start")),
		key.NewBinding(key.WithKeys("alt+c"), key.WithHelp("alt+c", "copy")),
		key.NewBinding(key.WithKeys("alt+x"), key.WithHelp("alt+x", "cut")),
		key.NewBinding(key.WithKeys("alt+v"), key.WithHelp("alt+v", "paste")),
	)
}

// Dirty reports unsaved changes in the buffer — what the (*) title marker shows —
// exported so a host (a quit gate) can ask before discarding the buffer.
func (s *Screen) Dirty() bool { return s.dirty }

// ToggleWrap flips soft wrapping, translating the top row across the switch (scrY counts
// display rows wrapped and lines unwrapped). The host chooses the key.
func (s *Screen) ToggleWrap() {
	top := s.TopLine() // in the mode we are leaving
	s.wrap = !s.wrap
	s.wrapGoalValid = false
	s.wrapDirty = true
	s.SetTopLine(top)
}

// TopLine is the buffer line showing at the top of the viewport, in either mode.
func (s *Screen) TopLine() int { return s.lineAtRow(s.scrY) }

// SetTopLine scrolls so line is at the top of the viewport (the inverse of TopLine),
// clamped to the buffer.
func (s *Screen) SetTopLine(line int) {
	if s.wrap {
		s.scrY = s.firstRowOfLine(line)
	} else {
		s.scrY = line
	}
	s.clampScrollBounds()
}

// CenterLine is the buffer line at the middle of the viewport, the anchor a synced view
// (gote's preview) centers on.
func (s *Screen) CenterLine() int { return s.lineAtRow(s.scrY + s.h/2) }

// ScrollSpan reports the vertical position in display rows: offset, maximum offset and
// viewport height. Rows, not lines, so a host can sync its own scroll to it.
func (s *Screen) ScrollSpan() (offset, maxOffset, height int) {
	return s.scrY, max(s.rowCount()-s.h, 0), s.h
}

// lineAtRow is the buffer line showing at a display row, in either mode.
func (s *Screen) lineAtRow(row int) int {
	row = max(row, 0)
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
func (s *Screen) firstRowOfLine(line int) int {
	s.rebuildWrapRows()
	for i, r := range s.wrapRows {
		if r.line >= line {
			return i
		}
	}
	return 0
}

// ToggleLineNums flips line numbers independently of wrapping, preserving the top line.
func (s *Screen) ToggleLineNums() {
	top := s.TopLine()
	s.lineNums = !s.lineNums
	s.wrapGoalValid = false
	s.wrapDirty = true // the gutter's width is part of the wrap geometry
	s.SetTopLine(top)
}

// WrapMode and LineNumMode return the current toggle states, so hosts can keep their
// own UI in sync.
func (s *Screen) WrapMode() bool    { return s.wrap }
func (s *Screen) LineNumMode() bool { return s.lineNums }

// SetSize lays the viewport out inside width x bodyHeight and bounds the scroll (not to
// the caret, which would undo wheel scrolls). The router calls it after every message, so
// an unchanged geometry returns early: a wrapped rebuild walks the whole document.
func (s *Screen) SetSize(_ *core.Shared, width, bodyHeight int) {
	searchBar := s.searchBarVisible()
	if !s.sizeDirty && width == s.lastSizeW && bodyHeight == s.lastSizeH && searchBar == s.lastSearchBar {
		// The buffer may have shrunk since the last message, so bound the offsets anyway.
		s.clampScrollBounds()
		return
	}
	s.lastSizeW, s.lastSizeH, s.lastSearchBar, s.sizeDirty = width, bodyHeight, searchBar, false
	s.wrapGoalValid = false
	s.paneH = bodyHeight
	s.w, s.h = width-s.insetX(), bodyHeight-s.insetY()
	if searchBar {
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
