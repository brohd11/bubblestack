package core

import (
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
)

// KeyMap is the single source of truth for shared keybindings. Dispatch sites match with
// MatchKey and help bars build from Hint/FullHint, so adding a key to a WithKeys list
// rebinds it everywhere. A screen's own one-off keys (the editor's ctrl+x) may still be
// matched as raw strings; any key shared between sites belongs here.
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
	Actions        key.Binding // open the app's Actions picker; the app owns the menu and the push
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

	// Pane navigation over a ModularScreen. The host matches these above every panel,
	// capturing or not, so they always leave a pane; hence the modifier, and nothing else may
	// bind them. The cycle suits two or three panes; directional moves suit bigger grids.
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

// Keys is the active keymap. ctrl+c is handled directly by the router as quit.
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

	// ctrl+alt+a reaches the Actions picker from screens that capture every key (an embedded
	// editor): modified keys pass the capture gates (see modifiedKey). It needs the
	// terminal's option-as-meta setting; "a" stays primary.
	Actions: key.NewBinding(key.WithKeys("a", "ctrl+alt+a"), key.WithHelp("a", "actions")),

	// The shift+arrows belong to the focused screen (editor selection), so the router leaves
	// them alone.
	NextTab:      key.NewBinding(key.WithKeys("]", "x")),
	PrevTab:      key.NewBinding(key.WithKeys("[", "z")),
	ToggleOutput: key.NewBinding(key.WithKeys("O")),
	Output:       key.NewBinding(key.WithKeys("o")),
	Wrap:         key.NewBinding(key.WithKeys("w")),
	Mouse:        key.NewBinding(key.WithKeys("ctrl+g")),
	Clear:        key.NewBinding(key.WithKeys("C")),
	// alt+u guards the stack reset against stray presses, and as a modified key it passes
	// the filter gate on screens that capture every key (a MenuScreen). Bare u works when
	// nothing is capturing.
	Unwind:  key.NewBinding(key.WithKeys("alt+u", "u")),
	Refresh: key.NewBinding(key.WithKeys("r")),
	// t hands the terminal to a shell inline, T opens a detached window, ctrl+t opens the
	// file manager (a modified key, so it works over a filtering list).
	Terminal:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "terminal")),
	TerminalWindow: key.NewBinding(key.WithKeys("T"), key.WithHelp("T", "term window")),
	OpenDir:        key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "open dir")),

	// shift+tab is the only pane key, and the cycle is forward-only, wrapping so every pane
	// is reachable. shift+tab is the one shift combo every terminal delivers intact.
	// PanePrev keeps its field and moveFocus case so it can be bound later.
	//
	// Inside a ModularScreen it shadows PrevField and the editor's shift+tab (pane keys are
	// consumed above capture): forms keep ↑/↓ and the editor keeps tab, and a key that only
	// sometimes moved panes would be worse.
	PaneNext: key.NewBinding(key.WithKeys("shift+tab")),
	PanePrev: key.NewBinding(),

	// The directional moves are implemented (ModularScreen.neighbor) but unbound. shift+↑/↓
	// lose their modifier on Apple Terminal and shift+←/→ are the editor's selection keys,
	// so they need keys a stock terminal delivers intact, likely ctrl+letter.
	PaneUp:    key.NewBinding(),
	PaneDown:  key.NewBinding(),
	PaneLeft:  key.NewBinding(),
	PaneRight: key.NewBinding(),

	NextField: key.NewBinding(key.WithKeys("down", "tab")),
	// shift+tab here is the pushed-form binding; a form living in a ModularScreen pane
	// loses it to PaneNext (see above) and moves fields on ↑/↓.
	PrevField: key.NewBinding(key.WithKeys("up", "shift+tab")),
	// v2 names a bare space "space". It only reaches a form's switch on a non-text field;
	// QueryUpdate sends printable keys to a focused text field first.
	Toggle: key.NewBinding(key.WithKeys("space")),

	PageNext: key.NewBinding(key.WithKeys("'", "3")),
	PagePrev: key.NewBinding(key.WithKeys(";", "2")),
}

// MatchKey reports whether key string k is one of b's keys: key.Matches for a string,
// usable in KeyPressMsg switches and OnKey closures alike.
func MatchKey(k string, b key.Binding) bool {
	return slices.Contains(b.Keys(), k)
}

// modifiedKey reports whether k carries a modifier. Modified keys type no text, so the
// Filtering gates let them through (how global combos stay reachable from full-capture
// screens).
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
	default:
		return k
	}
}

// Hint builds one help entry from central bindings. The label shows only the first
// keycode of each binding (the help-bar rule); the entry still matches all of them.
func Hint(desc string, binds ...key.Binding) key.Binding { return hint(desc, false, binds) }

// FullHint is Hint with every keycode in the label (the full-help rule).
func FullHint(desc string, binds ...key.Binding) key.Binding { return hint(desc, true, binds) }

func hint(desc string, all bool, binds []key.Binding) key.Binding {
	var labels, keys []string
	for _, b := range binds {
		bk := b.Keys()
		if len(bk) == 0 {
			continue
		}
		shown := bk[:1]
		if all {
			shown = bk
		}
		for _, k := range shown {
			labels = append(labels, prettyKey(k))
		}
		keys = append(keys, bk...)
	}
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(strings.Join(labels, "/"), desc))
}

// Legend renders bindings as the unstyled "key desc · key desc" run a bordered pane
// paints into its top edge, skipping unbound entries. It should carry only keys that act
// on that pane; screen-wide keys belong in the help bar.
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

// DirKeyHints are the full-help entries for the DirLocator keys (t, T, ctrl+t), for
// screens that advertise a directory. Full help only: never on a bar (see ShortHelp).
func DirKeyHints() []key.Binding {
	return []key.Binding{
		Hint("terminal", Keys.Terminal),
		Hint("term window", Keys.TerminalWindow),
		Hint("open dir", Keys.OpenDir),
	}
}

// PaneHint is the single help entry for pane navigation (help bar and full help, not
// pane legends). Its label lists whichever pane bindings carry keys; it matches all of
// them.
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
