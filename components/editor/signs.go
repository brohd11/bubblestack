package editor

import (
	"slices"

	"charm.land/lipgloss/v2"
)

// The sign column: one decorated cell per line, left of the line numbers. The editor
// draws signs; what they mean (git markers, lint, breakpoints) is the host's.

// Sign is one line's decoration. Text must be exactly one cell wide: every width
// calculation (contentW, wrap rows, click mapping) assumes it. Style is applied per
// render, so it follows theme changes.
type Sign struct {
	Text  string
	Style lipgloss.Style
}

type signColumn struct {
	signs map[int]Sign
	shown bool
}

// legacySignColumn is the named column behind the one-column API, so old and new hosts
// can coexist.
const legacySignColumn = "\x00default"

// ensureSignColumn returns id's column, registering it at the inner edge. Use
// SetSignColumnOrder for a specific order.
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

// SetSignColumnOrder sets the outer-to-inner column order. Columns missing from ids keep
// their order after the listed ones, so no host hides another's.
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
	if i := slices.Index(s.signOrder, id); i >= 0 {
		s.signOrder = slices.Delete(s.signOrder, i, i+1)
	}
	if col.shown {
		s.wrapDirty = true
		s.clampScrollBounds()
	}
}

// SetSigns replaces the legacy column's signs, keyed by 0-based line (nil clears).
// Out-of-range lines are harmless. It does not dirty the wrap cache: the column's width
// depends only on whether it is shown.
func (s *Screen) SetSigns(signs map[int]Sign) { s.SetSignColumn(legacySignColumn, signs) }

// ShowSigns draws or hides the column, independent of wrap and line numbers.
func (s *Screen) ShowSigns(on bool) {
	s.ShowSignColumn(legacySignColumn, on)
}

// ToggleSigns flips the column, for a host binding it to a key.
func (s *Screen) ToggleSigns() { s.ToggleSignColumn(legacySignColumn) }

// SignsMode reports whether the column is drawn, so a host can keep its own UI in sync
// (as WrapMode and LineNumMode do).
func (s *Screen) SignsMode() bool { return s.SignColumnMode(legacySignColumn) }

// Signs returns the live map last set; build a new map rather than mutating it.
func (s *Screen) Signs() map[int]Sign {
	if col := s.signColumns[legacySignColumn]; col != nil {
		return col.signs
	}
	return nil
}

// EditSeq is the buffer's change generation, bumped by every mutation. A host computing
// from the text compares it to tell whether a result is still current.
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
