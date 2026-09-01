package core

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// KeyMap is the single source of truth for every keybinding in the TUI. Each
// field is a key.Binding, which is already "an array of keycodes plus a help
// label" in one value. Dispatch sites match against these with MatchKey, and the
// help bars are built from these with Hint/FullHint — so a binding only ever has
// to be edited here. To add an alternate scheme (e.g. wasd) append the keys to
// the relevant WithKeys list below and it propagates to dispatch, list scrolling,
// the help bar, and the full (?) help automatically.
//
// Convention: ALL key handling — tab/screen Update loops, the shared components,
// and the help bars — dispatches via these bindings with MatchKey and builds help
// with Hint/FullHint; no site matches a raw keycode or key.Type. (A screen's own
// one-off keys — the editor's ctrl+x, say — are still matched as raw strings; the
// rule is that no key shared between sites goes unnamed here.) The shared Update
// helpers that apply these bindings (components.RootUpdate for tab roots,
// components.QueryUpdate for text-entry screens) live in components/update.go,
// since they operate on the reusable list.Model / Item pieces.
type KeyMap struct {
	// navigation
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
	// Jump to either end of whatever is scrolling — the output pane, a ScrollContainer,
	// a menu. Never on a help bar (a jump is a command): the (?) menu carries them.
	Top    key.Binding // jump to the oldest content
	Bottom key.Binding // jump to the newest content

	// actions
	Select key.Binding
	Back   key.Binding
	Quit   key.Binding

	// confirm — Yes carries enter, No carries esc, so a confirm screen matches
	// them directly without consulting Select/Back.
	Yes key.Binding
	No  key.Binding

	// global chrome
	NextTab        key.Binding
	PrevTab        key.Binding
	ToggleOutput   key.Binding // focus/unfocus the output pane for scrolling (O; o shows/hides)
	Output         key.Binding // show/hide the output box
	Wrap           key.Binding // toggle the output pane's wrap render mode (optional Wrapper)
	Mouse          key.Binding // toggle mouse capture; off restores terminal text selection
	Clear          key.Binding
	Unwind         key.Binding
	Refresh        key.Binding // reload all views; action is consumer-supplied
	Terminal       key.Binding // open a terminal in this process at the top screen's directory (DirLocator); action is consumer-supplied
	TerminalWindow key.Binding // open a detached terminal window at the same directory; action is consumer-supplied
	OpenDir        key.Binding // open the top screen's directory in the OS file manager (DirLocator); action is consumer-supplied

	// pane navigation over a ModularScreen's grid of panels. The host screen
	// matches these ABOVE every panel, capturing or not, so they stay the way out
	// of a pane that claims every other keystroke (an embedded editor). That
	// reservation is why they carry a modifier and why nothing else may bind
	// them: keys the panels never see.
	//
	// Two schemes, both intended to stay. The cycle steps through the panes in
	// declaration order and is the right gesture on the common two- or three-pane
	// screen, where "the next one" is unambiguous. The directional moves aim at a
	// particular pane by its place in the grid, which is what you want once a
	// layout is big enough that "next" stops meaning anything.
	PaneNext key.Binding
	PanePrev key.Binding

	PaneUp    key.Binding
	PaneDown  key.Binding
	PaneLeft  key.Binding
	PaneRight key.Binding

	// form
	NextField key.Binding
	PrevField key.Binding
	Toggle    key.Binding // flip the focused field in place (a checkbox, or a switch stepped forward)

	// pagination
	PageNext key.Binding
	PagePrev key.Binding
}

// Keys is the active keymap. Edit a WithKeys list here (e.g. add "w"/"s" to
// Up/Down) to rebind everywhere at once. ctrl+c is handled directly in the
// router (as a Quit alias) and is intentionally not represented here.
var Keys = KeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k", "alt+w")),
	Down:   key.NewBinding(key.WithKeys("down", "j", "alt+s")),
	Left:   key.NewBinding(key.WithKeys("left", "h", "alt+a")),
	Right:  key.NewBinding(key.WithKeys("right", "l", "alt+d")),
	Top:    key.NewBinding(key.WithKeys("g", "home")),
	Bottom: key.NewBinding(key.WithKeys("G", "end")),

	Select: key.NewBinding(key.WithKeys("enter", "e")),
	Back:   key.NewBinding(key.WithKeys("esc", "backspace", "c")),
	Quit:   key.NewBinding(key.WithKeys("q")),

	Yes: key.NewBinding(key.WithKeys("enter", "y", "Y", "e")),
	No:  key.NewBinding(key.WithKeys("esc", "n", "N", "c")),

	// The shift+arrows used to alias these. They belong to the focused screen now —
	// an editor selects text with them (components.EditorScreen.selectMove) — so the
	// router must leave them alone rather than claim them here. [ ] and z x remain.
	NextTab:      key.NewBinding(key.WithKeys("]", "x")),
	PrevTab:      key.NewBinding(key.WithKeys("[", "z")),
	ToggleOutput: key.NewBinding(key.WithKeys("O")),
	Output:       key.NewBinding(key.WithKeys("o")),
	Wrap:         key.NewBinding(key.WithKeys("w")),
	Mouse:        key.NewBinding(key.WithKeys("ctrl+g")),
	Clear:        key.NewBinding(key.WithKeys("C")),
	// alt+u rather than the backtick it used to carry: a stack reset is a big gesture for
	// a bare key sitting under the ESC row, and the modifier is what stops a stray press
	// from throwing away a deep navigation. Being modified it also clears the router's
	// filter gate (modifiedKey), so it still reaches globalKey from a screen that captures
	// every keystroke — a MenuScreen, whose Filtering() is unconditionally true, above all.
	// Bare u stays for the times the stack is not capturing. The backtick is now bound
	// nowhere.
	Unwind:  key.NewBinding(key.WithKeys("alt+u", "u")),
	Refresh: key.NewBinding(key.WithKeys("r")),
	// t hands this process's terminal to a shell and comes back when it exits; T is the
	// detached window. The inline form gets the unshifted key because it is the one you
	// reach for mid-task — a two-command detour shouldn't cost a window. That pushed the
	// file manager off T and onto ctrl+t, which has the side benefit of surviving a
	// filtering list (see modifiedKey).
	Terminal:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "terminal")),
	TerminalWindow: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "term window")),
	OpenDir:        key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "open dir")),

	// shift+tab is the whole scheme now, and the cycle is FORWARD-ONLY: it wraps at both
	// ends, so every pane is still reachable by stepping on round. The shift+arrows that
	// used to carry it went to the editor, which selects text with them — a reserved key
	// costs every panel that key on every screen, and text selection is worth more on
	// shift+←/→ than a second way round a cycle that already wraps.
	//
	// PanePrev keeps its field and its moveFocus case rather than being deleted: like the
	// directional binds below it is live code with no keycodes on it, and giving the
	// backward step a key again is a WithKeys list here and nothing else.
	//
	// shift+tab is also the one shift combo every terminal delivers intact (as backtab),
	// which is what makes it safe as the only pane key.
	//
	// It SHADOWS PrevField and the editor's tab alias inside a ModularScreen:
	// moveFocus consumes pane keys above the capture gate, so a form or editor sitting
	// in a pane never sees it. That cost was weighed and taken — a form keeps ↑/↓ for
	// field moves and the editor keeps bare tab for indent, while a reserved key that
	// only sometimes moves panes would be worse than one that always does. Outside a
	// ModularScreen nothing claims these, so a pushed form behaves as it always did.
	PaneNext: key.NewBinding(key.WithKeys("shift+tab")),
	PanePrev: key.NewBinding(),

	// The directional moves are implemented (see ModularScreen.neighbor) but carry
	// no keycodes yet, so they match nothing: MatchKey against an empty binding is
	// false. Filling a WithKeys list here is the only step needed to turn them on.
	//
	// They are unbound rather than sitting on shift+↑/↓ because Apple Terminal
	// strips the modifier from the vertical arrows — shift+↑ arrives as a bare
	// "up" — so binding them there would silently hand the key to the focused
	// panel instead, which reads as the feature being broken. Whatever replaces
	// them has to be something a stock terminal delivers unmodified; shift+tab
	// (above) is the proof that such keys exist, and ctrl+letter combos are the
	// realistic space for them.
	//
	// The horizontal pair has no fallback to shift+←/→ either: those are the
	// editor's selection keys now, so all four directions need somewhere new.
	PaneUp:    key.NewBinding(),
	PaneDown:  key.NewBinding(),
	PaneLeft:  key.NewBinding(),
	PaneRight: key.NewBinding(),

	NextField: key.NewBinding(key.WithKeys("down", "tab")),
	// shift+tab here is the pushed-form binding; a form living in a ModularScreen pane
	// loses it to PaneNext (see above) and moves fields on ↑/↓.
	PrevField: key.NewBinding(key.WithKeys("up", "shift+tab")),
	// Space is " " as a key string: bubbletea normalizes a bare space rune to KeySpace,
	// whose name is " " (key.go, keyNames). It only ever reaches a form's keybind switch
	// on a non-text field — components.QueryUpdate diverts KeySpace into a focused text
	// field before the switch runs.
	Toggle: key.NewBinding(key.WithKeys(" ")),

	PageNext: key.NewBinding(key.WithKeys("'", "3")),
	PagePrev: key.NewBinding(key.WithKeys(";", "2")),
}

// MatchKey reports whether the pressed key string k is one of binding b's keys.
// It is the string-based analog of key.Matches (which needs a tea.KeyMsg): it
// reads like "k in Keys.Up" and works both in tea.KeyMsg switches (pass
// msg.String()) and in the OnKey closures that already receive a string.
func MatchKey(k string, b key.Binding) bool {
	return slices.Contains(b.Keys(), k)
}

// modifiedKey reports whether k carries a modifier (ctrl+/alt+/shift+…), i.e. is a
// combo rather than typable text. The Filtering gate exists to stop the router's
// global single-key shortcuts from stealing characters meant for a text input; a
// modified combo produces no text, so the gates let it through even while a screen
// filters (which is how a global combo like the mouse toggle stays reachable from a
// full-capture screen such as the editor).
func modifiedKey(k string) bool { return strings.Contains(k, "+") }

// prettyKey maps raw keycodes to display glyphs so the default bars keep their
// arrow look; unknown keys pass through unchanged.
func prettyKey(k string) string {
	switch k {
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "shift+up":
		return "⇧↑"
	case "shift+down":
		return "⇧↓"
	case "shift+left":
		return "⇧←"
	case "shift+right":
		return "⇧→"
	case "shift+tab":
		return "⇧tab"
	case " ":
		return "space" // a literal blank would render as a help entry with no key
	default:
		return k
	}
}

// Hint builds a single help entry from one or more central bindings: the label
// shows only the FIRST keycode of each binding (the always-visible help bar
// rule), while the entry still carries every keycode so it matches and so a
// FullHint over the same binds can expand to all of them. desc is the context
// label (e.g. "option", "remove").
func Hint(desc string, binds ...key.Binding) key.Binding {
	var labels, keys []string
	for _, b := range binds {
		if bk := b.Keys(); len(bk) > 0 {
			labels = append(labels, prettyKey(bk[0]))
			keys = append(keys, bk...)
		}
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(strings.Join(labels, "/"), desc))
}

// Legend renders bindings as the plain "key desc · key desc" run a bordered pane
// paints into its top edge. It is the border-legend analog of Shared.BindingHelp:
// the same entries, unstyled, because the legend takes the border's own color.
// Entries whose bindings carry no keycodes are skipped, so an unbound hint costs
// nothing and appears by itself the moment it is given keys. Building a legend from
// bindings rather than spelling one out is what keeps a pane's advertised keys from
// drifting off the keymap.
//
// A legend carries only keys that act on THAT pane. Screen-wide keys — pane navigation
// above all — belong to the help bar or the (?) menu; a border that repeats them says
// the same thing twice a row apart and is free to drift out of step with it, which is
// exactly how this legend once came to advertise pane keys no binding carried.
func Legend(binds ...key.Binding) string {
	var parts []string
	for _, b := range binds {
		h := b.Help()
		if h.Key == "" {
			continue
		}
		parts = append(parts, h.Key+" "+h.Desc)
	}
	return strings.Join(parts, " · ")
}

// FullHint is Hint but the label lists ALL keycodes (the "more help" / full-help
// menu rule): adding "w" to Keys.Up makes this read "↑/k/w" automatically.
func FullHint(desc string, binds ...key.Binding) key.Binding {
	var labels, keys []string
	for _, b := range binds {
		for _, k := range b.Keys() {
			labels = append(labels, prettyKey(k))
			keys = append(keys, k)
		}
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(strings.Join(labels, "/"), desc))
}

// DirKeyHints returns the help entries for the DirLocator-based global keys — the inline
// terminal ("t"), the terminal window ("T") and open-directory ("ctrl+t") keys the router
// fires on any screen advertising a directory. A DirLocator screen includes these in its
// (?) full help so the keys are discoverable; a screen with no directory omits them, so
// non-repo menus don't advertise keys that wouldn't fire.
//
// These are FULL-HELP entries only. Do not append them into a bar — neither a list's short
// help nor a BindingHelp bar on a static screen: the bar is kept deliberately sparse (see
// ShortHelp), and three system-open keys are exactly the kind of secondary command that
// belongs behind "?". A screen that has no full help simply doesn't advertise them; the
// keys still fire, and each app's docs page spells them out.
func DirKeyHints() []key.Binding {
	return []key.Binding{
		Hint("terminal", Keys.Terminal),
		Hint("term window", Keys.TerminalWindow),
		Hint("open dir", Keys.OpenDir),
	}
}

// PaneHint is the single help entry for pane navigation, rendered as one entry
// rather than the several Hint would produce — moving between panes is one idea
// to the user, and a row of near-identical entries would crowd a bar that already
// carries the focused panel's own keys. A ModularScreen includes it whenever it
// has more than one focusable panel.
//
// It is a help-bar and (?) entry only, deliberately not a pane legend one: moving
// between panes is the screen's key, not any one pane's (see Legend).
//
// The label is built from whichever bindings actually carry keys, so it widens by
// itself when the currently-unbound directional keys get keycodes; the entry
// still carries every keycode, so it matches and expands in full help.
func PaneHint() key.Binding {
	// Hint already skips bindings that carry no keys, so the unbound directional
	// entries cost nothing here and appear the moment they are given keycodes.
	return Hint("panes",
		Keys.PanePrev, Keys.PaneNext,
		Keys.PaneLeft, Keys.PaneRight, Keys.PaneUp, Keys.PaneDown)
}

// tabHint is the combined "[ ]" tab-switch hint shown by ShortHelp (the two tab
// binds rendered as one entry, keeping the original look).
func tabHint() key.Binding {
	keys := append(append([]string{}, Keys.PrevTab.Keys()...), Keys.NextTab.Keys()...)
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp("[ ]", "tabs"))
}
