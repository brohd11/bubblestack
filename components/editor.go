package components

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EditorScreen is the simple nano-like text editor: it loads a file (or starts empty),
// lets the user type freely, and exits on ctrl+x with a "save modified buffer?"
// three-way prompt when the buffer is dirty (n = discard & exit, esc/c = cancel, and
// y = a filename prompt seeded with the current name — nano's "File Name to Write",
// so saving under a different name is a save-as). Enter splits lines, tab (or
// shift+tab) inserts a tab, the arrows
// move the cursor, and a left click places it. The wheel scrolls the view without
// moving the cursor (a cursor move then snaps the view back to it), and when the
// buffer overflows the viewport a proportional scrollbar takes the rightmost column.
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
// Deliberately minimal: no soft-wrap (long lines scroll horizontally), no cut/paste,
// no search.
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

	dirty       bool // buffer differs from what was loaded/last saved
	confirmExit bool // the nano-style save/discard/cancel prompt is showing
	saveExits   bool // the save in flight came from the exit prompt, so it ends in exit

	hl         Highlighter // syntax coloring; nil ⇒ plain render (see EditorOpts.Highlighter)
	hlExplicit bool        // hl came from EditorOpts, not the registry: a rename must not replace it
	editSeq    int         // bumped at every buffer mutation; hl reparses when hlSeq lags
	hlSeq      int         // the edit sequence hl last parsed (-1 ⇒ never)

	bordered bool // EditorOpts.Border: draw the frame instead of the title bar
	embedded bool // one pane of a layout (core.Embeddable): pane-relative mouse, gutter
	focused  bool // false ⇒ muted body, no cursor (core.FocusableScreen); true standalone

	originX, originY int  // the pane's absolute top-left (components.PaneOriginer)
	hasOrigin        bool // false standalone ⇒ the save-as box spans the full width

	w, h int // viewport dims in cells (the body net of the title bar or frame), set by SetSize

	wrap      bool      // soft-wrap long lines to the viewport width
	lineNums  bool      // the sticky ctrl+l preference (see gutterOn: wrap forces the gutter on)
	wrapRows  []wrapRow // the wrapped display rows scrY indexes while wrap is on
	wrapBar   bool      // whether those rows overflow the viewport — resolved by rebuildWrapRows
	wrapDirty bool      // the wrap cache needs a rebuild: an edit, a resize or a toggle moved it
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
type EditorOpts struct {
	Path        string
	Title       string
	Crumb       string
	Border      bool
	OnExit      func(*core.Shared) core.Action
	OnRelease   func(*core.Shared) core.Action
	OnSaved     func(*core.Shared, string) core.Action
	Highlighter Highlighter
}

// editorLoadedMsg carries the async file read from Init back to Update.
type editorLoadedMsg struct {
	content string
	err     error
}

// editorSavedMsg carries the async write from the exit prompt back to Update.
type editorSavedMsg struct{ err error }

var (
	editorCursorStyle = lipgloss.NewStyle().Reverse(true)
	editorPromptStyle = lipgloss.NewStyle().Bold(true)
)

// editorTabWidth is the display width a tab expands to when rendering. Raw '\t' must
// never reach the View output: the terminal expands it to the next tab stop while the
// renderer measures it as zero-width, so the padded frame line overflows, wraps, and
// every later frame shifts (the "screen advances a line" corruption).
const editorTabWidth = 4

// editorWheelStep is how many lines one wheel notch scrolls the viewport.
const editorWheelStep = 3

// expandLine renders a buffer line to display runes, tabs expanded to spaces. Display
// cells then equal display-rune indexes (double-width runes are the accepted
// limitation of this simple editor).
func expandLine(line []rune) []rune {
	var out []rune
	for _, r := range line {
		if r == '\t' {
			for i := 0; i < editorTabWidth; i++ {
				out = append(out, ' ')
			}
		} else {
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
// asynchronously from Init (the framework idiom — IO only in the cmd lane).
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
		path:       opts.Path,
		title:      title,
		crumb:      crumb,
		onExit:     opts.OnExit,
		onRelease:  opts.OnRelease,
		onSaved:    opts.OnSaved,
		lines:      [][]rune{{}},
		bordered:   opts.Border,
		focused:    true, // standalone the editor is always focused; a panel blurs it
		hl:         hl,
		hlExplicit: hlExplicit,
		hlSeq:      -1,   // nothing parsed yet, even before the first edit
		wrapDirty:  true, // no rows measured yet, even before the first edit
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
func (s *EditorScreen) Init(*core.Shared) tea.Cmd {
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
		s.dirty = false
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
	case tea.KeyMsg:
		return s.key(sh, m)
	case tea.MouseMsg:
		if s.confirmExit || m.Action != tea.MouseActionPress {
			return s, core.Action{}
		}
		switch m.Button {
		case tea.MouseButtonLeft:
			s.clickAt(sh, m.X, m.Y)
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
		return s, core.Action{}
	}
	return s, core.Action{}
}

// key routes one keystroke. Editor-local keys are matched as raw strings: ctrl+x /
// tab / enter are this screen's own keys with no core.Keys binding, and the arrows
// match only the raw keycodes (not the k/j/h/l alternates core.Keys.Up et al. carry
// — those letters must stay typable). The word/line editing combos mirror
// bubbles/textinput's KeyMap verbatim (alt+←→ word jumps, alt+⌫ word delete,
// ctrl+u/k line deletes, ctrl+a/e line ends, ctrl+h/d char-delete aliases) so the
// editor behaves like the form field. shift+tab is kept as an alias for tab: it is
// what a form binds to PrevField, so the finger that reaches for it in a field
// shouldn't do nothing here.
func (s *EditorScreen) key(sh *core.Shared, m tea.KeyMsg) (core.Screen, core.Action) {
	k := m.String()
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
		if len(m.Runes) > 0 {
			s.insertRunes(m.Runes...)
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
	s.dirty = false
	s.editSeq++ // the buffer changed even though the load is clean: reparse
	s.wrapDirty = true
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

// ---------- word/line operations (the bubbles/textinput KeyMap mirror) ----------

// isWordSpace delimits words: whitespace only, the same notion textinput uses (no
// alnum/punct classes — keep it stupidly simple).
func isWordSpace(r rune) bool { return unicode.IsSpace(r) }

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

// deleteWordBack is DeleteWordBackward: deletes from wordBackPos to the cursor. At
// column 0 it is a plain line join, exactly like backspace.
func (s *EditorScreen) deleteWordBack() {
	y, x := s.wordBackPos()
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
func (s *EditorScreen) moveVertical(delta int) {
	y := s.curY + delta
	if y < 0 || y >= len(s.lines) {
		return
	}
	s.curY = y
	if s.curX = s.wantX; s.curX > len(s.lines[y]) {
		s.curX = len(s.lines[y])
	}
}

// clickAt maps a left press to a buffer position: the offsets (see insetX/insetY)
// come off first, then the scroll offsets turn viewport cells into buffer coordinates
// (the column via colAtCell, since clicks land in display cells but curX counts
// runes), clamped to the line. A click left of the body reads as column 0; one above
// it is ignored.
func (s *EditorScreen) clickAt(sh *core.Shared, x, y int) {
	if x -= s.insetX(); x < 0 {
		x = 0
	}
	if s.barVisible() && x >= s.textW() {
		return // the scrollbar column carries no buffer position
	}
	if x -= s.numGutterWidth(); x < 0 {
		x = 0 // a click on the line number reads as column 0, as one left of the body does
	}
	rel := y - s.insetY()
	if !s.embedded {
		rel -= sh.BodyY() // absolute coordinates: the chrome rows come off too
	}
	if rel < 0 {
		return
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
	s.curY, s.curX, s.wantX = row, col, col
	s.clampScroll()
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
// The styles are built per call so a theme switch repaints, as renderLine's muted
// style does.
func (s *EditorScreen) scrollbarCell(row int) string {
	total := max(s.rowCount(), 1) // rows, not lines: wrapped, one line can be many
	thumb := max(s.h*s.h/total, 1)
	top := 0
	if d := total - s.h; d > 0 {
		top = min(s.scrY, d) * (s.h - thumb) / d
	}
	color, glyph := core.MutedColor, "│"
	if row >= top && row < top+thumb {
		color, glyph = core.FocusedColor, "█"
	}
	if !s.focused {
		color = core.MutedColor
	}
	return lipgloss.NewStyle().Foreground(color).Render(glyph)
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

func (s *EditorScreen) titleText() string {
	if s.dirty {
		return s.title + " [+]"
	}
	return s.title
}

// View renders the buffer window under its title, both tracking focus: bordered, the
// title (with its [+] modified marker) is the frame's top-border legend and the frame
// carries the tint; unbordered, it is the title bar above the body, muted while a
// sibling pane holds the keys.
func (s *EditorScreen) View(*core.Shared) string {
	if s.bordered {
		return frame(s.titleText(), s.body(), s.w+s.gutter(), s.focused)
	}
	return core.WithTitleFocused(s.titleText(), s.body(), s.focused)
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
	vis := disp[start:end]
	num := s.lineNumText(r.line, r.start == 0)
	// A caret one cell past this chunk is only THIS row's to draw when the line ends
	// here. Mid-line it belongs to the next row, at its column 0.
	eol := s.lastRowOfLine(idx)

	if !s.focused {
		return num + lipgloss.NewStyle().Foreground(core.MutedColor).Render(string(vis))
	}
	if s.hl != nil {
		if styled, ok := s.renderLineStyled(r.line, start, end, eol); ok {
			return num + styled
		}
	}
	if r.line != s.curY {
		return num + string(vis)
	}
	c := cellOfCol(line, s.curX) - start
	if c >= 0 && c < len(vis) {
		return num + string(vis[:c]) + editorCursorStyle.Render(string(vis[c])) + string(vis[c+1:])
	}
	if c == len(vis) && eol {
		return num + string(vis) + editorCursorStyle.Render(" ")
	}
	return num + string(vis)
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
	vis := disp[start:end]
	num := s.lineNumText(row, true)
	if !s.focused {
		return num + lipgloss.NewStyle().Foreground(core.MutedColor).Render(string(vis))
	}
	if s.hl != nil {
		if styled, ok := s.renderLineStyled(row, start, end, true); ok {
			return num + styled
		}
	}
	if row != s.curY {
		return num + string(vis)
	}
	c := cellOfCol(s.lines[row], s.curX) - start // clampScroll keeps this within [0, s.contentW())
	if c >= 0 && c < len(vis) {
		return num + string(vis[:c]) + editorCursorStyle.Render(string(vis[c])) + string(vis[c+1:])
	}
	if c == len(vis) { // end of line, inside the window
		return num + string(vis) + editorCursorStyle.Render(" ")
	}
	return num + string(vis) // the caret is off the window (browse mode): draw none
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
		j := i + 1
		for j < len(vis) && j != c && vidx[j] == vidx[i] {
			j++
		}
		b.WriteString(spans[vidx[i]].Style.Render(string(vis[i:j])))
		i = j
	}
	if row == s.curY && c == len(vis) && eol { // only the window the line ends in
		b.WriteString(editorCursorStyle.Render(" "))
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
	if s.onRelease != nil {
		hints = append(hints, key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "leave pane")))
	}
	return append(hints,
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
	top := s.topLine() // in the mode we are leaving
	s.wrap = !s.wrap
	s.wrapDirty = true // the gutter appears or goes: the whole geometry moved
	if s.wrap {
		s.scrY = s.firstRowOfLine(top)
	} else {
		s.scrY = top
	}
	s.clampScrollBounds()
}

// topLine is the buffer line showing at the top of the viewport, in either mode.
func (s *EditorScreen) topLine() int {
	if !s.wrap {
		return s.scrY
	}
	s.rebuildWrapRows()
	if s.scrY < len(s.wrapRows) {
		return s.wrapRows[s.scrY].line
	}
	return len(s.lines) - 1
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
	s.w, s.h = width-s.insetX(), bodyHeight-s.insetY()
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
	x, y, w := 0, sh.BodyY(), sh.Width()
	if s.hasOrigin {
		x, y, w = s.originX, s.originY, s.paneW()
	}
	edit := NewLineEdit("file name to write", x, y+s.insetY()+s.h-2, w,
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

// paneW is the full width the pane gave SetSize — the text window plus the chrome
// around it — so the save-as box covers exactly the editor's bottom.
func (s *EditorScreen) paneW() int {
	w := s.w + s.insetX()
	if s.bordered {
		w++ // the right border
	}
	return w
}

// applySaveName points the buffer at name: a save-as renames it, so the title bar
// and crumb follow the new base name — and so does the syntax coloring, since the
// extension is what picks it. Only a registry-chosen highlighter is re-picked: one
// passed through EditorOpts was a deliberate override and a rename must not undo it.
// hlSeq is reset rather than bumped because the new highlighter has parsed nothing.
func (s *EditorScreen) applySaveName(name string) {
	s.path = name
	s.title = filepath.Base(name)
	s.crumb = s.title
	if !s.hlExplicit {
		s.hl = lookupHighlighter(strings.ToLower(filepath.Ext(name)))
		s.hlSeq = -1
	}
}

// saveCmd snapshots the buffer and writes it to Path asynchronously (IO in the cmd
// lane); the result arrives as an editorSavedMsg. An empty path is an error — a
// scratch buffer has nowhere to save to. Parent directories are created: a save-as
// names a LOCATION, and refusing one because a folder in it doesn't exist yet would
// make the box ask for something it won't accept.
func (s *EditorScreen) saveCmd() tea.Cmd {
	path := s.path
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
			return editorSavedMsg{err: errors.New("no file path")}
		}
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return editorSavedMsg{err: err}
			}
		}
		return editorSavedMsg{err: os.WriteFile(path, []byte(content), 0o644)}
	}
}
