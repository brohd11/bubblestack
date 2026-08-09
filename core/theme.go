package core

import (
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// Theme is a named set of the framework's semantic colors. The derived styles in
// shared.go are built from these colors, so a Theme is the single knob that repaints
// the whole TUI. SetTheme swaps the active Theme and rebuilds those styles. Presets
// ship below; consumers can add their own with RegisterTheme (the hook a future config
// file will load presets through).
//
// Each field is a lipgloss.TerminalColor, so a color can be a flat lipgloss.Color or a
// lipgloss.AdaptiveColor{Light, Dark} that resolves against the terminal's detected
// background (see bubblestack.Run, which primes that detection at startup). Every preset
// below is adaptive: the neutrals share one light/dark pair, and each accent darkens on a
// light terminal so it still reads as selected-item text — the fix for e.g. mono's
// near-white accent vanishing on a white background.
type Theme struct {
	Name      string
	Muted     lipgloss.TerminalColor // secondary text: labels, help, list descriptions
	Log       lipgloss.TerminalColor // near-white output/log text
	Border    lipgloss.TerminalColor // box/rule borders
	Focused   lipgloss.TerminalColor // selection / active accent
	OnFocused lipgloss.TerminalColor // text drawn on the accent (title bar); nil ⇒ defaultOnFocused
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
var defaultOnFocused = lipgloss.AdaptiveColor{Light: "255", Dark: "232"}

// Shared neutral palette, identical across every preset. Values are ANSI-256 indices:
// darker on a light terminal, lighter on a dark one, so borders/labels/log text keep
// contrast either way.
var (
	neutralMuted  = lipgloss.AdaptiveColor{Light: "240", Dark: "247"}
	neutralLog    = lipgloss.AdaptiveColor{Light: "236", Dark: "252"}
	neutralBorder = lipgloss.AdaptiveColor{Light: "244", Dark: "243"}
)

// themes is the preset registry, keyed by Theme.Name. Only the accent (Focused) varies
// per theme; the neutrals are shared and OnFocused falls back to defaultOnFocused.
var themes = map[string]Theme{
	"lipgloss": {Name: "lipgloss", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "162", Dark: "212"}},
	// mono is a monochrome black/white/grey palette: the accent is the terminal's own
	// extreme (black on a light background, white on a dark one), with greys for borders
	// and secondary text. That extreme is indistinguishable from body text, which a
	// markdown page needs it not to be — so mono alone borrows an accent for those.
	"mono":  {Name: "mono", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "232", Dark: "255"}, MarkdownFrom: "lipgloss"},
	"godot": {Name: "godot", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "25", Dark: "67"}},
	"red":   {Name: "red", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "160", Dark: "203"}},
	"green": {Name: "green", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "28", Dark: "114"}},
	"amber": {Name: "amber", Muted: neutralMuted, Log: neutralLog, Border: neutralBorder, Focused: lipgloss.AdaptiveColor{Light: "130", Dark: "214"}},
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
// cached, so a theme switch repaints — the rule the derived styles follow. A
// MarkdownFrom naming a theme nobody registered falls back to the active accent
// rather than answering nil, so a typo dims a page instead of blanking it.
func MarkdownAccent() lipgloss.TerminalColor {
	if from := current.MarkdownFrom; from != "" {
		if t, ok := themes[from]; ok && t.Focused != nil {
			return t.Focused
		}
	}
	return FocusedColor
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

// applyTheme makes t the active theme: it points the exported color vars at t's
// colors, then rebuilds every derived style from them.
func applyTheme(t Theme) {
	current = t
	MutedColor, logColor, BorderColor, FocusedColor = t.Muted, t.Log, t.Border, t.Focused
	OnFocusedColor = t.OnFocused
	if t.OnFocused == nil {
		OnFocusedColor = defaultOnFocused
	}
	rebuildStyles()
}
