package editor

import (
	"strings"
	"testing"

	"github.com/brohd11/bubblestack/core"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// The frame benchmarks. They exist because the reported latency is on a 77-line file,
// where no parse cost can explain it — so what has to be measured is the per-message
// cost of Update and View themselves, with and without an active selection.

// benchStyles is a highlighter's palette: a fixed table spans point into. The nil entry
// is the unstyled run every real lexer leaves behind on punctuation and whitespace.
var benchStyles = []*lipgloss.Style{
	nil,
	ptr(lipgloss.NewStyle().Foreground(lipgloss.Color("4"))),
	ptr(lipgloss.NewStyle().Foreground(lipgloss.Color("2"))),
	ptr(lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)),
}

func ptr(st lipgloss.Style) *lipgloss.Style { return &st }

// tokenHL splits every line into alternating word/space runs, so a rendered row carries
// the handful of spans a real lexer would give it rather than one span for the whole line.
type tokenHL struct{ lines [][]Span }

func (h *tokenHL) Parse(doc string) {
	rows := strings.Split(doc, "\n")
	h.lines = make([][]Span, len(rows))
	for i, row := range rows {
		if row == "" {
			continue
		}
		var spans []Span
		for j := 0; j < len(row); {
			k := j + 1
			space := row[j] == ' '
			for k < len(row) && (row[k] == ' ') == space {
				k++
			}
			spans = append(spans, Span{Text: row[j:k], Style: benchStyles[len(spans)%len(benchStyles)]})
			j = k
		}
		h.lines[i] = spans
	}
}

func (h *tokenHL) HighlightLine(row int) []Span {
	if row < 0 || row >= len(h.lines) {
		return nil
	}
	return h.lines[row]
}

// configDoc is a 77-line YAML-shaped buffer: the reported repro's size and line width.
func configDoc() string {
	var b strings.Builder
	for i := range 77 {
		switch i % 4 {
		case 0:
			b.WriteString("language_servers:\n")
		case 1:
			b.WriteString("    gopls:\n")
		case 2:
			b.WriteString("        disabled: false\n")
		default:
			b.WriteString("        command: [gopls, serve, --remote=auto]\n")
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// proseDoc is 77 long lines: the same document shape with paragraph-width rows, where any
// per-cell work that is O(line) turns quadratic.
func proseDoc() string {
	line := strings.TrimSpace(strings.Repeat("the quick brown fox jumps over the lazy dog ", 12))
	return strings.TrimSuffix(strings.Repeat(line+"\n", 77), "\n")
}

func benchEditor(doc string) (*Screen, *core.Shared) {
	s, sh := newEditor(Opts{Highlighter: &tokenHL{}})
	s.setContent(doc)
	s.View(sh) // settle the lazy parse so the loop measures steady state
	return s, sh
}

func BenchmarkEditorViewPlain(b *testing.B) {
	s, sh := benchEditor(configDoc())
	b.ReportAllocs()
	for b.Loop() {
		benchSink = s.View(sh)
	}
}

// BenchmarkEditorViewSelection is the reported gesture: ten lines highlighted, the frame
// redrawn while the pointer is still down.
func BenchmarkEditorViewSelection(b *testing.B) {
	s, sh := benchEditor(configDoc())
	s.selStart, s.selEnd = textPos{2, 0}, textPos{12, 0}
	b.ReportAllocs()
	for b.Loop() {
		benchSink = s.View(sh)
	}
}

func BenchmarkEditorViewSelectionLongLines(b *testing.B) {
	s, sh := benchEditor(proseDoc())
	s.selStart, s.selEnd = textPos{2, 0}, textPos{12, 0}
	b.ReportAllocs()
	for b.Loop() {
		benchSink = s.View(sh)
	}
}

// BenchmarkEditorKeystroke is one edit and the frame that follows it — the whole cost of
// pressing a key, minus the router chrome around it.
func BenchmarkEditorKeystroke(b *testing.B) {
	s, sh := benchEditor(configDoc())
	b.ReportAllocs()
	for b.Loop() {
		s.Update(sh, keyMsg("x"))
		s.Update(sh, keyMsg("backspace"))
		benchSink = s.View(sh)
	}
}

// BenchmarkEditorDragMotion is one frame of a drag-select: the message a held pointer
// sends for every cell it crosses.
func BenchmarkEditorDragMotion(b *testing.B) {
	s, sh := benchEditor(configDoc())
	s.startDragAt(textPos{2, 0})
	b.ReportAllocs()
	y := 0
	for b.Loop() {
		y = 3 + (y+1)%10
		s.Update(sh, tea.MouseMotionMsg{X: 20, Y: y, Button: tea.MouseLeft})
		benchSink = s.View(sh)
	}
}

// BenchmarkEditorMessageWrapped is what a message costs an editor pane in wrap mode: the
// router re-sizes the top screen after every message, and a wrap rebuild walks and expands
// the whole document. The SetSize call is part of the measurement on purpose.
func BenchmarkEditorMessageWrapped(b *testing.B) {
	s, sh := benchEditor(proseDoc())
	s.ToggleWrap()
	s.View(sh)
	b.ReportAllocs()
	for b.Loop() {
		s.SetSize(sh, 80, 20)
		benchSink = s.View(sh)
	}
}

var benchSink string
