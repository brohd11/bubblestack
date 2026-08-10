package core

import tea "github.com/charmbracelet/bubbletea"

// Action is the dual-lane result a screen's Update (and the Pick/handler closures it
// drives) hands back, bundling the two return values that used to travel as a bare
// (tea.Msg, tea.Cmd) pair:
//   - Msg is a synchronous control message the router applies inline this same tick (a
//     nav command from core such as Push/Pop; nil for none).
//   - Cmd is a genuine async tea.Cmd the router hands to bubbletea (IO/streaming; nil
//     for none).
//
// The zero Action does nothing. Splitting the two lanes means navigation no longer
// round-trips through the command queue, while real IO can only travel in the cmd lane
// (so nothing can accidentally block the update loop). Build one with a nav constructor
// (Push/Pop/…, which set only Msg), with Async (only Cmd), or as a struct literal.
type Action struct {
	Msg tea.Msg
	Cmd tea.Cmd
}

// Async wraps a bare async cmd as an Action with no control message — the common
// "no navigation, just run this cmd" return (e.g. a list/input fall-through).
func Async(cmd tea.Cmd) Action { return Action{Cmd: cmd} }

// screen is one navigable view. The router owns the optional chrome (header, status,
// output pane — see chrome.go), the help bar, and the navigation stack; a screen
// renders only its own body and handles its own keys. Implementations are pointer
// types so Update and SetSize can mutate in place.
// Update returns the (possibly new) screen and an Action carrying its synchronous
// control message (applied inline this same tick) and/or its async tea.Cmd.
type Screen interface {
	Init(*Shared) tea.Cmd
	Update(*Shared, tea.Msg) (Screen, Action)
	View(*Shared) string     // body between the header and the help bar
	HelpView(*Shared) string // the fully-rendered help bar line(s)
	SetSize(s *Shared, width, bodyHeight int)
}

// Optional behaviors the router type-asserts for, so a screen only opts in when
// relevant rather than every screen carrying a stub.

// filterer reports an active text filter, so the router's global single-key
// shortcuts (O/c) don't steal keystrokes meant for the filter input.
type Filterer interface{ Filtering() bool }

// QuitGater is implemented by a screen that wants to intercept the global quit
// keys (q, ctrl+c) — typically to confirm when unsaved work would be lost.
// Returning (act, true) replaces the quit with act; (Action{}, false) abstains.
// The stack is consulted top-down and the first handler wins. A screen pushed BY
// a gate (the confirm popup) must answer for itself (see DialogScreen.OnQuit) —
// otherwise the walk finds the same gate below it and stacks popup upon popup.
type QuitGater interface{ QuitGate(sh *Shared) (Action, bool) }

// FocusableScreen is a screen that can render a focused and an unfocused state
// (a form tinting its box border, a ModularScreen dimming its active pane). The
// router drives it on output-pane focus transitions: SetFocused(false) when the
// pane takes the keys, SetFocused(true) when they return. Opt-in like every
// other capability — a screen without it renders the same either way, and a
// screen that is always the whole body (never visually "blurred") simply
// doesn't implement it.
type FocusableScreen interface{ SetFocused(bool) }

// Embeddable is a screen that can be told it is one pane of a layout rather than
// the whole body. The router never asserts it — the EMBEDDER does:
// components.ScreenPanel calls SetEmbedded(true) when it wraps a screen as a pane.
// It reports a fact about PLACEMENT, not a style: what changes is the geometry the
// screen computes against.
//
//   - mouse: the host ModularScreen forwards presses in PANE-RELATIVE coordinates,
//     so an embedded screen must not subtract Shared.BodyY (it already came off);
//     standalone, coordinates are absolute and BodyY does come off. This is the
//     whole reason the capability exists — a screen cannot derive it, and getting
//     it wrong lands clicks a chrome's worth of rows away from the pointer.
//   - size: the dims handed to SetSize are the pane's outer cells, so whatever
//     chrome the screen draws comes out of them (the panel convention).
//
// What the screen LOOKS like is not this interface's business: a border is a
// construction-time option of the screen itself (EditorOpts.Border, mirroring
// ListPanelOpts.Border), so the instancer composes it, embedded or not. Focus is
// likewise separate — a screen implementing FocusableScreen has SetFocused
// forwarded by the panel and denotes focus however suits it.
type Embeddable interface{ SetEmbedded(bool) }

// receiver lets a screen react to a broadcast notification (PropagateAll). The
// framework only routes the payload (opaque, consumer-defined); a screen type-
// switches on payloads it recognizes and ignores the rest. It may return an Action
// (e.g. ShowTab to grab focus), which the router resolves in the same tick, or the zero
// Action to do nothing. Optional — screens opt in by implementing it.
type Receiver interface {
	Receive(sh *Shared, payload any) Action
}

// popStopper marks a screen the router stops at when handling PopTo: a sub-flow
// can pop back to its command hub (the nearest stopper) without knowing the stack
// depth. Returns false to act as a normal screen.
type PopStopper interface{ PopStop() bool }

// ChromeMask marks which chrome elements a screen suppresses while it is on top
// (true ⇒ hidden). The zero value hides nothing; FullscreenMask hides everything,
// giving a screen the whole canvas.
type ChromeMask struct {
	Header     bool
	TabStrip   bool
	Breadcrumb bool
	Status     bool
	Output     bool
	Help       bool
}

// FullscreenMask suppresses every chrome element — the mask a fullscreen screen
// returns from ChromeMask.
func FullscreenMask() ChromeMask {
	return ChromeMask{Header: true, TabStrip: true, Breadcrumb: true, Status: true, Output: true, Help: true}
}

// chromeMasker is the optional interface a screen implements to suppress chrome
// elements while it is the active (top) screen. The router queries the top screen
// each render and resize, so popping back to a screen that doesn't implement it
// restores the chrome automatically — no shared state to reset.
type ChromeMasker interface{ ChromeMask() ChromeMask }

// DirLocator is implemented by a screen that concerns one filesystem directory (a repo
// checkout). The router queries the top screen for it to resolve where the global Terminal
// key opens a terminal (and, later, a filesystem-open key — the interface names the
// directory, not the terminal). Optional — a screen opts in by implementing it. Names no
// domain type (a directory path is not domain state), matching the other optional screen
// capabilities. Return ("", false) to advertise no directory.
type DirLocator interface{ LocateDir() (string, bool) }

// Crumber lets a screen contribute one segment to the router-drawn breadcrumb bar
// (rendered under the tab strip). The router walks the active stack root→top and
// asks every screen that implements this for its segment: CrumbLabel(false) for the
// current (top) screen, CrumbLabel(true) for the upstream ones, joining the non-empty
// results into a path. short asks for a compact form, supplied when the trail grows
// long; return "" to contribute no segment. Optional — screens opt in by implementing
// it.
type Crumber interface{ CrumbLabel(short bool) string }

// Overlayer marks a screen the router draws *on top of* the screen below it (a
// popup/modal) instead of replacing it: rather than rendering only the top screen,
// the router renders the below-screen's full frame as the background and then
// composites this screen's View() box centered over it (see Composite), so the
// screen underneath stays visible around the box. Input is unaffected — only the
// top screen receives Update — so an overlay is naturally modal. Optional; a screen
// opts in by implementing it. The router honors the bool return each frame, so a
// screen can implement Overlayer and still render full-screen by returning false
// (DialogScreen does this — one type serves both confirm and popup).
type Overlayer interface{ IsOverlay() bool }

// OverlayPositioner is an opt-in interface for an overlay screen (Overlayer) that
// wants its box composited at a specific cell position instead of centered — a
// floating line edit anchored over the list row it edits, say. The router passes
// the rendered box dimensions so the screen can account for them (placing the box
// above vs. below an anchor), and clamps the returned position into the visible
// frame either way, so an implementation may return its raw anchor. Optional; an
// overlay that doesn't implement it centers as before.
type OverlayPositioner interface {
	// OverlayPos returns the zero-based cell (column, row) the box's top-left
	// corner composits at. boxW/boxH are the rendered box dimensions.
	OverlayPos(boxW, boxH int) (x, y int)
}
