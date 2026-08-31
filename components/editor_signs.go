package components

import "github.com/charmbracelet/lipgloss"

// The sign column: one decorated cell per buffer line, left of the line numbers.
//
// It is deliberately empty of meaning. The editor knows how wide the column is, which
// line each sign belongs to and how to draw it beside the numbers without knocking the
// text geometry out of alignment — and nothing else. What a sign SAYS is the host's:
// git diff markers, lint severities, breakpoints, read marks. This is the same division
// EditorOpts.Highlighter already draws, and for the same reason: the component owns the
// rendering, the app owns the domain.
//
// The alternative — teaching the editor about git — would put a VCS dependency inside a
// general-purpose text component and still not serve the second consumer that wanted a
// different marker.

// Sign is one line's decoration. Text must measure exactly one display cell: the column
// is one cell wide and every width calculation downstream (contentW, the wrap rows, the
// click-to-cursor math) is derived from that, so a two-cell glyph would shift the whole
// body one column right of where clicks land. Style is applied per render, so a sign
// built once still repaints when the theme changes.
type Sign struct {
	Text  string
	Style lipgloss.Style
}

// SetSigns replaces the whole sign map, keyed by 0-based buffer line. A nil or empty map
// clears it. Keys outside the buffer are harmless — nothing looks them up — which is what
// lets a host set signs computed against a buffer that has since shrunk without having to
// clamp them first.
//
// This does NOT dirty the wrap cache: the column's width depends on whether signs are
// shown (ShowSigns), never on what is in them, so a recompute on every keystroke costs a
// map swap and no re-measure.
func (s *EditorScreen) SetSigns(signs map[int]Sign) { s.signs = signs }

// ShowSigns draws or hides the column. Unlike the line-number gutter it is independent of
// wrap and of the ctrl+l preference — a host that wants change markers without line
// numbers should get them — so it has its own flag rather than joining gutterOn.
func (s *EditorScreen) ShowSigns(on bool) {
	if s.signsOn == on {
		return
	}
	s.signsOn = on
	s.wrapDirty = true // a column appears or goes: the whole geometry moved
	s.clampScrollBounds()
}

// ToggleSigns flips the column, for a host binding it to a key.
func (s *EditorScreen) ToggleSigns() { s.ShowSigns(!s.signsOn) }

// SignsMode reports whether the column is drawn, so a host can keep its own UI in sync
// (as WrapMode and LineNumMode do).
func (s *EditorScreen) SignsMode() bool { return s.signsOn }

// Signs reads back the map last set. It is the live map, not a copy — a host that hands
// signs over has given them away and should build a fresh map rather than mutate this
// one, exactly as it would with the slice behind SetItems.
func (s *EditorScreen) Signs() map[int]Sign { return s.signs }

// EditSeq is the buffer's change generation, bumped by every mutation. A host computing
// something from the text — a diff, a lint pass — compares it to decide whether its
// result is still current, which is what the internal search and highlight caches
// already do with searchSeq and hlSeq. It is the honest debounce key: unlike a timer it
// cannot be fooled by an edit that arrives while the work is in flight.
func (s *EditorScreen) EditSeq() int { return s.editSeq }

// signGutterWidth is the column's contribution to the left gutter: one cell, or none.
// Like numGutterWidth it must not consult textW or barVisible — see leftGutterWidth.
func (s *EditorScreen) signGutterWidth() int {
	if !s.signsOn {
		return 0
	}
	return 1
}

// signText is the sign cell for one display row: the line's sign on its first row, a
// blank on its wrapped continuations and on any line that has none.
func (s *EditorScreen) signText(line int, first bool) string {
	if s.signGutterWidth() == 0 {
		return ""
	}
	sign, ok := s.signs[line]
	if !ok || !first || sign.Text == "" {
		return " "
	}
	return sign.Style.Render(sign.Text)
}
