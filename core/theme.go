package core

import (
	"image/color"
	"sort"

	"charm.land/lipgloss/v2"
)

// Color is one semantic color as the adaptive ANSI-256 index pair the palette has
// always been: the index to use on a light terminal and the one to use on a dark one.
// lipgloss v2 has no adaptive color type of its own — a style takes a resolved
// image/color.Color — so the pair is bubblestack's, and Resolve turns it into the
// concrete color for the detected background. Every preset below was already such a
// pair, so this states the existing invariant in the type rather than adding one.
type Color struct{ Light, Dark uint8 }

// isDark is the detected terminal background, the input Resolve reads. It defaults to
// dark because that is the safe guess for a non-TTY run (and the one lipgloss v1
// hard-coded); SetBackgroundIsDark corrects it when the terminal answers, which the
// router does on tea.BackgroundColorMsg.
var isDark = true

// Resolve picks c's variant for the detected background. It is the one place a Color
// becomes something a lipgloss style will accept.
func Resolve(c Color) color.Color {
	return lipgloss.LightDark(isDark)(lipgloss.ANSIColor(c.Light), lipgloss.ANSIColor(c.Dark))
}

// SetBackgroundIsDark records the terminal's detected background and repaints: every
// derived style is rebuilt against the new answer. Router.Update calls it from
// tea.BackgroundColorMsg, which is v2's replacement for lipgloss v1's process-global
// HasDarkBackground query. Returns whether the value actually changed, so the caller
// can skip a redundant rebuild.
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

// Theme is a named set of the framework's semantic colors. The derived styles in
// shared.go are built from these colors, so a Theme is the single knob that repaints
// the whole TUI. SetTheme swaps the active Theme and rebuilds those styles. Presets
// ship below; consumers can add their own with RegisterTheme (the hook a future config
// file will load presets through).
//
// Each field is an adaptive Color pair resolved against the terminal's detected
// background (see Resolve). Every preset below is adaptive: the neutrals share one
// light/dark pair, and each accent darkens on a light terminal so it still reads as
// selected-item text — the fix for e.g. mono's near-white accent vanishing on a white
// background.
type Theme struct {
	Name      string
	Muted     Color  // secondary text: labels, help, list descriptions
	Log       Color  // near-white output/log text
	Border    Color  // box/rule borders
	Focused   Color  // selection / active accent
	OnFocused *Color // text drawn on the accent (title bar); nil ⇒ defaultOnFocused
	// MarkdownFrom names another registered theme whose accent the markdown
	// renderer should borrow (see MarkdownAccent). Empty — every preset but mono —
	// means "use my own Focused". It exists because a rendered markdown page leans
	// on the accent for three different things at once (headings, code spans,
	// links), and a theme whose accent IS the terminal's own extreme has nothing
	// left to distinguish them from body text: under mono the page flattens. One
	// borrowed color is the cheap fix; a per-theme markdown palette is the
	// thorough one, for the day this proves too blunt.
	MarkdownFrom string
}

// defaultOnFocused is the title-bar text color used when a theme leaves OnFocused unset.
// The accent is light on a dark terminal and dark on a light terminal, so the text drawn
// on it is always the inverse — one adaptive value serves every theme.
var defaultOnFocused = Color{Light: 255, Dark: 232}

// Shared neutral palette, identical across every preset. Values are ANSI-256 indices:
// darker on a light terminal, lighter on a dark one, so borders/labels/log text keep
// contrast either way.
var (
	neutralMuted  = Color{Light: 240, Dark: 247}
	neutralLog    = Color{Light: 236, Dark: 252}
	neutralBorder = Color{Light: 244, Dark: 243}
)

// themes is the preset registry, keyed by Theme.Name. Only the accent (Focused) varies
// per theme; the neutrals are shared and OnFocused falls back to defaultOnFocused.
var themes = map[string]Theme{
	"lipgloss": {Name: "lipgloss", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 162, Dark: 212}},
	// mono is a monochrome black/white/grey palette: the accent is the terminal's own
	// extreme (black on a light background, white on a dark one), with greys for borders
	// and secondary text. That extreme is indistinguishable from body text, which a
	// markdown page needs it not to be — so mono alone borrows an accent for those.
	"mono":  {Name: "mono", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 232, Dark: 255}, MarkdownFrom: "lipgloss"},
	"godot": {Name: "godot", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 25, Dark: 67}},
	"red":   {Name: "red", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 160, Dark: 203}},
	"green": {Name: "green", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 28, Dark: 114}},
	"amber": {Name: "amber", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: Color{Light: 130, Dark: 214}},
}

// current is the active theme; applyTheme keeps it and the color vars in sync.
var current = themes["mono"]

// RegisterTheme adds or overrides a preset (keyed by t.Name). This is the entry
// point a config file uses to define custom themes; it does not switch to the
// theme — call SetTheme(t.Name) for that.
func RegisterTheme(t Theme) { themes[t.Name] = t }

// SetTheme switches to the named preset, reassigns the palette, and rebuilds the
// derived styles so the next render uses the new colors. An unknown name leaves
// the current theme untouched and returns false.
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

// MarkdownAccent is the accent a rendered markdown page should use: the active
// theme's own, or the one it borrows via Theme.MarkdownFrom. Read per call, never
// cached, so a theme switch repaints — the rule the derived styles follow. It answers
// the unresolved pair, since callers both Resolve it for a style and Dim it. A
// MarkdownFrom naming a theme nobody registered falls back to the active accent rather
// than a zero pair, so a typo dims a page instead of blanking it.
func MarkdownAccent() Color {
	if from := current.MarkdownFrom; from != "" {
		if t, ok := themes[from]; ok {
			return t.Focused
		}
	}
	return current.Focused
}

// ApplyTheme is the in-TUI form of SetTheme: it switches the theme (synchronously) and
// broadcasts MsgThemeChanged via PropagateAll. The router only routes the payload; a
// consumer's App Receive recognizes it (see OnThemeChange) and returns RefreshRoots() to
// rebuild the cached tab roots with the new palette — so the framework no longer hard-codes
// that policy. A picker's row returns this Action on select.
func ApplyTheme(name string) Action {
	SetTheme(name)
	return PropagateAll(MsgThemeChanged{})
}

// OnThemeChange is the standard App-side reaction to a theme switch: when payload is the
// MsgThemeChanged broadcast, return RefreshRoots() so the cached tab roots re-bake their
// styles from the new palette; otherwise a no-op Action. A consumer's App Receive just
// returns this. It stays a helper rather than framework-hardcoded policy so a consumer
// whose roots must not be reinstanced can decline it and handle the broadcast itself.
func OnThemeChange(payload any) Action {
	if _, ok := payload.(MsgThemeChanged); ok {
		return RefreshRoots()
	}
	return Action{}
}

// ThemeNames returns the registered preset names, sorted, for a picker/listing.
func ThemeNames() []string {
	names := make([]string, 0, len(themes))
	for name := range themes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

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
	rebuildStyles()
}
