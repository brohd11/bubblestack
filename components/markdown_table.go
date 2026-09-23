package components

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// tableDelim matches a GFM delimiter row: cells of "-" with optional ":" alignment
// markers, outer pipes optional. It also matches a bare "---"; tableStart requires a "|"
// so a thematic break stays one.
var tableDelim = regexp.MustCompile(`^\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?$`)

// tableStart reports whether trimmed opens a table: a pipe row with a delimiter row of
// the same cell count under it. Without the delimiter, pipes are prose (a shell pipeline,
// an ASCII diagram).
func tableStart(trimmed, next string) bool {
	if !strings.Contains(trimmed, "|") {
		return false
	}
	next = strings.TrimSpace(next)
	if !strings.Contains(next, "|") || !tableDelim.MatchString(next) {
		return false
	}
	return len(tableCells(trimmed)) == len(tableCells(next))
}

// tableOpen reports whether trimmed continues an open table. A table runs to a blank line
// or the next block construct; list markers are checked here because their cases come
// after the table's.
func tableOpen(trimmed string) bool {
	return !strings.HasPrefix(trimmed, "#") &&
		!strings.HasPrefix(trimmed, ">") &&
		!strings.HasPrefix(trimmed, "- ") &&
		!orderedItem.MatchString(trimmed)
}

// tableCells splits a row into cells, dropping one leading and trailing pipe. Escapes are
// not honored, so a cell cannot hold a literal pipe (GFM splits inside code spans too).
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimSuffix(strings.TrimPrefix(row, "|"), "|")
	cells := strings.Split(row, "|")
	for i, c := range cells {
		cells[i] = strings.TrimSpace(c)
	}
	return cells
}

// The horizontal alignments a delimiter row's colons can ask for.
type tableAlign int

const (
	tableLeft tableAlign = iota
	tableCenter
	tableRight
)

// tableSpec is one parsed pipe table: the header cells, the body rows — every one squared
// off to the header's cell count — and the per-column alignment read off the delimiter.
type tableSpec struct {
	head  []string
	rows  [][]string
	align []tableAlign
}

// tableAligns reads the per-column alignment off a delimiter row: ":-" left, ":-:" center,
// "-:" right, and a bare "-" left, which is GFM's default.
func tableAligns(delim string) []tableAlign {
	cells := tableCells(delim)
	out := make([]tableAlign, len(cells))
	for i, c := range cells {
		lead, trail := strings.HasPrefix(c, ":"), strings.HasSuffix(c, ":")
		switch {
		case lead && trail:
			out[i] = tableCenter
		case trail:
			out[i] = tableRight
		}
	}
	return out
}

// parseTable turns a table block into a spec with every row squared to the header's cell
// count. ok is false when it is not a table yet; it must handle half-typed input (gote's
// preview renders every keystroke).
func parseTable(lines []string) (tableSpec, bool) {
	if len(lines) < 2 || !tableDelim.MatchString(strings.TrimSpace(lines[1])) {
		return tableSpec{}, false
	}
	t := tableSpec{head: tableCells(lines[0]), align: tableAligns(lines[1])}
	if len(t.align) != len(t.head) {
		return tableSpec{}, false
	}
	for _, l := range lines[2:] {
		row := make([]string, len(t.head))
		copy(row, tableCells(l)) // copy pads the short rows and drops the long ones' tails
		t.rows = append(t.rows, row)
	}
	return t, true
}

// tableCellWidth is a cell's rendered width: code chips are wider than their source and
// links narrower.
func tableCellWidth(text string) int { return lipgloss.Width(inline(text)) }

// tableWidths picks each column's width: natural (widest rendered cell) when the row
// fits, otherwise the widest columns shrink equally to the largest cap that fits
// (water-filling). nil when not even tableMinCol per column fits.
func tableWidths(t tableSpec, width int) []int {
	n := len(t.head)
	if n == 0 {
		return nil
	}
	nat, widest := make([]int, n), 0
	for i, c := range t.head {
		nat[i] = max(1, tableCellWidth(c))
	}
	for _, row := range t.rows {
		for i, c := range row {
			nat[i] = max(nat[i], tableCellWidth(c))
		}
	}
	// Measured from the constant, never a hard-coded 3, so widening the separator cannot
	// desync this budget from the rule tableRule draws to it.
	total := (n - 1) * lipgloss.Width(tableSep)
	avail := width - total
	for _, w := range nat {
		total += w
		widest = max(widest, w)
	}
	if total <= width {
		return nat
	}

	// capped is the width the columns take at cap c — the water-filling level's own sum,
	// so the feasibility test and the fill are the same expression.
	capped := func(c int) int {
		sum := 0
		for _, w := range nat {
			sum += min(w, c)
		}
		return sum
	}
	level := 0
	for c := 1; c <= widest && capped(c) <= avail; c++ {
		level = c
	}
	if level < tableMinCol {
		return nil
	}
	w := make([]int, n)
	for i := range nat {
		w[i] = min(nat[i], level)
	}
	// Hand leftover cells to the squeezed columns one at a time, left to right; one pass is
	// always enough.
	for i, left := 0, avail-capped(level); i < n && left > 0; i++ {
		if nat[i] > level {
			w[i]++
			left--
		}
	}
	return w
}

// wrapCell styles a cell and then wraps it, so breaks fall where the text prints and each
// row fits its column (quoteBlock does the reverse because it owns its whole width). A
// color left open at a break is closed with a reset, so it cannot bleed into the next
// column; the continuation row loses the tint. The reset is added only when needed, so
// uncolored output stays clean.
func wrapCell(text string, w int, base lipgloss.Style) []string {
	rows := strings.Split(wrapText(inlineOver(text, base), w), "\n")
	for i, row := range rows {
		if strings.Contains(row, "\x1b") {
			rows[i] = row + ansi.ResetStyle
		}
	}
	return rows
}

// tablePad places one wrapped cell row in its column per its alignment, measured on
// rendered width.
func tablePad(row string, w int, a tableAlign) string {
	gap := w - lipgloss.Width(row)
	if gap <= 0 {
		return row
	}
	switch a {
	case tableRight:
		return strings.Repeat(" ", gap) + row
	case tableCenter:
		return strings.Repeat(" ", gap/2) + row + strings.Repeat(" ", gap-gap/2)
	}
	return row + strings.Repeat(" ", gap)
}

// tableRowBlock lays out one row: each cell wrapped, the row as tall as its tallest cell,
// shorter cells padded with blanks so the separators line up.
func tableRowBlock(cells []string, w []int, align []tableAlign, base lipgloss.Style) string {
	cols, height := make([][]string, len(w)), 1
	for i := range w {
		cols[i] = wrapCell(cells[i], w[i], base)
		height = max(height, len(cols[i]))
	}
	sep := ruleStyle().Render(tableSep)
	// A separator that ends a row drops its trailing space. Rendering the shorter separator,
	// rather than trimming the finished row, works under every color profile.
	sepEnd := ruleStyle().Render(strings.TrimRight(tableSep, " "))
	rows := make([]string, height)
	for y := range rows {
		var b strings.Builder
		for i, col := range cols {
			cell := ""
			if y < len(col) {
				cell = col[y]
			}
			cell = tablePad(cell, w[i], align[i])
			if i == len(cols)-1 {
				// The last column's own padding is width the row does not need — and only
				// it can come out empty here, since every earlier column keeps its blanks.
				cell = strings.TrimRight(cell, " ")
			}
			if i > 0 {
				if cell == "" {
					b.WriteString(sepEnd)
				} else {
					b.WriteString(sep)
				}
			}
			b.WriteString(cell)
		}
		rows[y] = b.String()
	}
	return strings.Join(rows, "\n")
}

// tableRule is the header rule: "─" per column joined by "─┼─", rendered in one call.
func tableRule(w []int) string {
	segs := make([]string, len(w))
	for i, n := range w {
		segs[i] = strings.Repeat("─", n)
	}
	return ruleStyle().Render(strings.Join(segs, tableCross))
}

// tableBlock renders a table block: accent header, rule, body. ok is false when it does
// not parse or fit, and the caller renders the source as prose.
func tableBlock(lines []string, width int) (string, bool) {
	t, ok := parseTable(lines)
	if !ok {
		return "", false
	}
	w := tableWidths(t, width)
	if w == nil {
		return "", false
	}
	out := []string{tableRowBlock(t.head, w, t.align, tableHeadStyle()), tableRule(w)}
	for _, row := range t.rows {
		out = append(out, tableRowBlock(row, w, t.align, lipgloss.Style{}))
	}
	return strings.Join(out, "\n"), true
}

// tableHeadStyle uses the heading accent, since bold alone reads like a body row on
// low-contrast themes.
func tableHeadStyle() lipgloss.Style {
	return headingStyle()
}
