package editor

import "charm.land/lipgloss/v2"

// The sign column: one decorated cell per buffer line, left of the line numbers.
//
// It is deliberately empty of meaning. The editor knows how wide the column is, which
// line each sign belongs to and how to draw it beside the numbers without knocking the
// text geometry out of alignment — and nothing else. What a sign SAYS is the host's:
// git diff markers, lint severities, breakpoints, read marks. This is the same division
// Opts.Highlighter already draws, and for the same reason: the component owns the
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

type signColumn struct {
	signs map[int]Sign
	shown bool
}

// legacySignColumn is the reserved name behind the original one-column API. Keeping
// that API as a real named column lets existing hosts coexist with newer consumers
// instead of making SetSigns unexpectedly erase their columns.
const legacySignColumn = "\x00default"

// ensureSignColumn returns id's column, registering a new one at the inner edge. Hosts
// wanting a different deterministic order can call SetSignColumnOrder; registration
// order remains a useful no-setup default.
func (s *Screen) ensureSignColumn(id string) *signColumn {
	if s.signColumns == nil {
		s.signColumns = make(map[string]*signColumn)
	}
	if col := s.signColumns[id]; col != nil {
		return col
	}
	col := &signColumn{}
	s.signColumns[id] = col
	listed := false
	for _, name := range s.signOrder {
		listed = listed || name == id
	}
	if !listed {
		s.signOrder = append(s.signOrder, id)
	}
	return col
}

// SetSignColumn replaces one named column's signs, keyed by 0-based buffer line. It
// does not affect any other column or its visibility.
func (s *Screen) SetSignColumn(id string, signs map[int]Sign) {
	s.ensureSignColumn(id).signs = signs
}

// ShowSignColumn draws or hides one named column. Columns are retained while hidden so
// a host can toggle them without recomputing their contents.
func (s *Screen) ShowSignColumn(id string, on bool) {
	col := s.ensureSignColumn(id)
	if col.shown == on {
		return
	}
	col.shown = on
	s.wrapDirty = true
	s.clampScrollBounds()
}

// ToggleSignColumn flips one named column.
func (s *Screen) ToggleSignColumn(id string) {
	s.ShowSignColumn(id, !s.SignColumnMode(id))
}

// SignColumnMode reports whether a named column is enabled.
func (s *Screen) SignColumnMode(id string) bool {
	col := s.signColumns[id]
	return col != nil && col.shown
}

// SignsForColumn reads back one named column's live sign map. As with Signs, the map is
// borrowed: replace it with SetSignColumn rather than mutating it in place.
func (s *Screen) SignsForColumn(id string) map[int]Sign {
	if col := s.signColumns[id]; col != nil {
		return col.signs
	}
	return nil
}

// SetSignColumnOrder sets the outer-to-inner order for named columns. Existing columns
// omitted from ids are appended in their prior order, so changing one host's preferred
// columns cannot silently hide a column installed by another host.
func (s *Screen) SetSignColumnOrder(ids ...string) {
	seen := make(map[string]bool, len(ids)+len(s.signOrder))
	order := make([]string, 0, len(ids)+len(s.signOrder))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		order = append(order, id)
	}
	for _, id := range s.signOrder {
		if !seen[id] {
			seen[id] = true
			order = append(order, id)
		}
	}
	s.signOrder = order
}

// RemoveSignColumn forgets a named column and its place in the order.
func (s *Screen) RemoveSignColumn(id string) {
	col := s.signColumns[id]
	if col == nil {
		return
	}
	delete(s.signColumns, id)
	for i, name := range s.signOrder {
		if name == id {
			s.signOrder = append(s.signOrder[:i], s.signOrder[i+1:]...)
			break
		}
	}
	if col.shown {
		s.wrapDirty = true
		s.clampScrollBounds()
	}
}

// SetSigns replaces the whole sign map, keyed by 0-based buffer line. A nil or empty map
// clears it. Keys outside the buffer are harmless — nothing looks them up — which is what
// lets a host set signs computed against a buffer that has since shrunk without having to
// clamp them first.
//
// This does NOT dirty the wrap cache: the column's width depends on whether signs are
// shown (ShowSigns), never on what is in them, so a recompute on every keystroke costs a
// map swap and no re-measure.
func (s *Screen) SetSigns(signs map[int]Sign) { s.SetSignColumn(legacySignColumn, signs) }

// ShowSigns draws or hides the column. Unlike the line-number gutter it is independent of
// wrap and of the ctrl+l preference — a host that wants change markers without line
// numbers should get them — so it has its own flag rather than joining gutterOn.
func (s *Screen) ShowSigns(on bool) {
	s.ShowSignColumn(legacySignColumn, on)
}

// ToggleSigns flips the column, for a host binding it to a key.
func (s *Screen) ToggleSigns() { s.ToggleSignColumn(legacySignColumn) }

// SignsMode reports whether the column is drawn, so a host can keep its own UI in sync
// (as WrapMode and LineNumMode do).
func (s *Screen) SignsMode() bool { return s.SignColumnMode(legacySignColumn) }

// Signs reads back the map last set. It is the live map, not a copy — a host that hands
// signs over has given them away and should build a fresh map rather than mutate this
// one, exactly as it would with the slice behind SetItems.
func (s *Screen) Signs() map[int]Sign {
	if col := s.signColumns[legacySignColumn]; col != nil {
		return col.signs
	}
	return nil
}

// EditSeq is the buffer's change generation, bumped by every mutation. A host computing
// something from the text — a diff, a lint pass — compares it to decide whether its
// result is still current, which is what the internal search and highlight caches
// already do with searchSeq and hlSeq. It is the honest debounce key: unlike a timer it
// cannot be fooled by an edit that arrives while the work is in flight.
func (s *Screen) EditSeq() int { return s.editSeq }

// shownSignColumns returns the enabled columns in their outer-to-inner order. Like
// numGutterWidth it must not consult textW or barVisible — see leftGutterWidth.
func (s *Screen) shownSignColumns() []string {
	ids := make([]string, 0, len(s.signOrder))
	for _, id := range s.signOrder {
		if col := s.signColumns[id]; col != nil && col.shown {
			ids = append(ids, id)
		}
	}
	return ids
}

// signText renders the supplied outer-to-inner columns for one display row: a line's
// signs on its first row and blanks on wrapped continuations or missing entries.
func (s *Screen) signText(ids []string, line int, first bool) string {
	var out string
	for _, id := range ids {
		sign, ok := s.signColumns[id].signs[line]
		if !ok || !first || sign.Text == "" {
			out += " "
			continue
		}
		out += sign.Style.Render(sign.Text)
	}
	return out
}
