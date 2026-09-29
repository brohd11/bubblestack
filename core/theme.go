package core

import (
	"image/color"
	"maps"
	"slices"

	"charm.land/lipgloss/v2"
)

// Color is a semantic color as an ANSI-256 pair: the index for a light terminal and for a
// dark one. lipgloss v2 has no adaptive color, so Resolve picks the half for the detected
// background.
type Color struct{ Light, Dark uint8 }

// isDark is the detected background. Dark is the safe default for a non-TTY run; the
// router corrects it from tea.BackgroundColorMsg.
var isDark = true

// Resolve picks c's variant for the detected background. It is the one place a Color
// becomes something a lipgloss style will accept.
func Resolve(c Color) color.Color {
	return lipgloss.LightDark(isDark)(lipgloss.ANSIColor(c.Light), lipgloss.ANSIColor(c.Dark))
}

// SetBackgroundIsDark records the terminal's background and rebuilds every derived style,
// reporting whether it changed.
func SetBackgroundIsDark(dark bool) bool {
	if isDark == dark {
		return false
	}
	isDark = dark
	applyTheme(current)
	return true
}

// BackgroundIsDark reports the detected terminal background, for a caller that has to
// hand it to an API taking an isDark flag (bubbles' list.DefaultStyles, say).
func BackgroundIsDark() bool { return isDark }

// Theme is a named set of the framework's semantic colors. SetTheme swaps the active
// Theme and rebuilds the derived styles in styles.go. Every preset is adaptive: neutrals
// share one pair, and accents darken on light terminals so they stay readable.
type Theme struct {
	Name      string
	Muted     Color  // secondary text: labels, help, list descriptions
	Log       Color  // near-white output/log text
	Border    Color  // box/rule borders
	Focused   Color  // selection / active accent
	OnFocused *Color // text drawn on the accent (title bar); nil ⇒ defaultOnFocused
	Selection *Color // background of a SelectBackground list row; nil ⇒ defaultSelection
	// MarkdownFrom names a theme whose accent rendered markdown borrows (see MarkdownAccent);
	// empty uses Focused. mono needs it: its accent is the terminal's own extreme, which
	// would make headings, code and links look like body text.
	MarkdownFrom string
}

// defaultOnFocused is the title-bar text color when a theme sets no OnFocused: the
// inverse of the accent's lightness on either background.
var defaultOnFocused = Color{Light: 255, Dark: 232}

// defaultSelection is the SelectBackground row bar when a theme sets no Selection: a
// neutral a step off the terminal background, so row text keeps its contrast.
var defaultSelection = Color{Light: 254, Dark: 237}

// Shared neutral palette (ANSI-256): darker on light terminals, lighter on dark ones.
var (
	neutralMuted  = Color{Light: 240, Dark: 247}
	neutralLog    = Color{Light: 236, Dark: 252}
	neutralBorder = Color{Light: 244, Dark: 243}
)

// themes is the preset registry, keyed by Theme.Name. Only the accent (Focused) varies
// per theme; the neutrals are shared and OnFocused falls back to defaultOnFocused.
var themes = map[string]Theme{
	"lipgloss": {Name: "lipgloss", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 162, Dark: 212}},
	// mono is black/white/grey, its accent the terminal's extreme; it borrows a markdown
	// accent.
	"mono":  {Name: "mono", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 232, Dark: 255}, MarkdownFrom: "lipgloss"},
	"godot": {Name: "godot", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 25, Dark: 67}},
	"red":   {Name: "red", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 160, Dark: 203}},
	"green": {Name: "green", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 28, Dark: 114}},
	"amber": {Name: "amber", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 130, Dark: 214}},
}

// current is the active theme; applyTheme keeps it and the color vars in sync.
var current = themes["mono"]

// RegisterTheme adds or replaces a preset by name without switching to it.
func RegisterTheme(t Theme) { themes[t.Name] = t }

// SetTheme switches to the named preset and rebuilds the styles; an unknown name returns
// false and changes nothing.
func SetTheme(name string) bool {
	t, ok := themes[name]
	if !ok {
		return false
	}
	applyTheme(t)
	return true
}

// CurrentTheme is the name of the active theme, for a picker to mark/select it.
func CurrentTheme() string { return current.Name }

// MarkdownAccent is the accent for rendered markdown: the theme's own, or the one it
// borrows via MarkdownFrom (falling back to its own if that theme is unknown). Read per
// call; it returns the unresolved pair so callers can Resolve or Dim it.
func MarkdownAccent() Color {
	if from := current.MarkdownFrom; from != "" {
		if t, ok := themes[from]; ok {
			return t.Focused
		}
	}
	return current.Focused
}

// ApplyTheme switches the theme and broadcasts MsgThemeChanged; the App's Receive
// typically answers with OnThemeChange. A theme picker row returns this Action.
func ApplyTheme(name string) Action {
	SetTheme(name)
	return PropagateAll(MsgThemeChanged{})
}

// OnThemeChange is the standard App reaction to MsgThemeChanged: RefreshRoots(), so tab
// roots re-bake their styles. A consumer whose roots must not be rebuilt can handle the
// broadcast itself instead.
func OnThemeChange(payload any) Action {
	if _, ok := payload.(MsgThemeChanged); ok {
		return RefreshRoots()
	}
	return Action{}
}

// ThemeNames returns the registered preset names, sorted, for a picker/listing.
func ThemeNames() []string { return slices.Sorted(maps.Keys(themes)) }

// applyTheme makes t the active theme: it resolves t's pairs against the detected
// background into the exported color vars, then rebuilds every derived style from them.
func applyTheme(t Theme) {
	current = t
	MutedColor, logColor, BorderColor, FocusedColor = Resolve(t.Muted), Resolve(t.Log), Resolve(t.Border), Resolve(t.Focused)
	on := defaultOnFocused
	if t.OnFocused != nil {
		on = *t.OnFocused
	}
	OnFocusedColor = Resolve(on)
	sel := defaultSelection
	if t.Selection != nil {
		sel = *t.Selection
	}
	SelectionColor = Resolve(sel)
	rebuildStyles()
}
