package core

import tea "charm.land/bubbletea/v2"

// Action is what a screen's Update and handler closures return, in two lanes:
//   - Msg is a control message the router applies this tick (Push, Pop, ...).
//   - Cmd is an async tea.Cmd for IO.
//
// The zero Action does nothing. Navigation never round-trips through the command queue,
// and IO can only travel as a cmd. Build one with a nav constructor, Async, or a literal.
type Action struct {
	Msg tea.Msg
	Cmd tea.Cmd
}

// Async wraps a bare async cmd as an Action with no control message — the common
// "no navigation, just run this cmd" return (e.g. a list/input fall-through).
func Async(cmd tea.Cmd) Action { return Action{Cmd: cmd} }

// Screen is one navigable view. The router owns the chrome, help bar and navigation stack;
// a screen renders its own body and handles its own keys. Implementations are pointers so
// Update and SetSize mutate in place.
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

// QuitGater lets a screen intercept the quit keys (q, ctrl+c), e.g. to confirm losing
// unsaved work: (act, true) replaces the quit. The stack is walked top-down and the first
// taker wins, so a screen pushed by a gate must answer for itself (DialogScreen.OnQuit) or
// the walk reaches the same gate again.
type QuitGater interface {
	QuitGate(sh *Shared) (Action, bool)
}

// FocusableScreen renders focused and unfocused states. The router calls SetFocused(false)
// when the output pane takes the keys and SetFocused(true) when they return.
type FocusableScreen interface{ SetFocused(bool) }

// Embeddable is a screen that can be told it is one pane of a layout; the embedder
// (components.ScreenPanel) calls it, never the router. Embedded, mouse coordinates arrive
// pane-relative (do not subtract Shared.BodyY) and SetSize gets the pane's outer cells.
// Borders and focus are separate concerns.
type Embeddable interface{ SetEmbedded(bool) }

// Receiver reacts to PropagateAll broadcasts. Payloads are opaque; a screen handles those
// it recognizes and may return an Action, resolved the same tick.
type Receiver interface {
	Receive(sh *Shared, payload any) Action
}

// PopStopper marks a screen PopTo stops at, so a sub-flow can return to its hub without
// knowing the stack depth.
type PopStopper interface{ PopStop() bool }

// ChromeMask marks which chrome a screen hides while on top (true = hidden).
// FullscreenMask hides everything.
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

// ChromeMasker lets the top screen hide chrome. The router asks each render, so popping
// back to a screen without it restores the chrome.
type ChromeMasker interface{ ChromeMask() ChromeMask }

// DirLocator is a screen concerning one directory (a repo), which the global terminal and
// open-dir keys use. Return ("", false) for none.
type DirLocator interface{ LocateDir() (string, bool) }

// Crumber contributes one breadcrumb segment: CrumbLabel(false) for the top screen,
// CrumbLabel(true) (a short form) upstream. "" contributes nothing.
type Crumber interface{ CrumbLabel(short bool) string }

// Overlayer marks a screen drawn over the one below (a popup) rather than replacing it.
// Only the top screen gets input, so overlays are modal. The bool is read each frame, so
// DialogScreen can be either.
type Overlayer interface{ IsOverlay() bool }

// OverlayPositioner places an overlay's box at a given cell instead of centering it. It
// gets the box dims; the router clamps the result into the frame.
type OverlayPositioner interface {
	// OverlayPos returns the zero-based cell (column, row) the box's top-left
	// corner composits at. boxW/boxH are the rendered box dimensions.
	OverlayPos(boxW, boxH int) (x, y int)
}
