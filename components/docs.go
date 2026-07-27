package components

import (
	"io/fs"
	"regexp"
	"strings"

	"github.com/brohd11/bubblestack/core"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// This file is the shared in-TUI manual engine: parse a set of markdown pages, render
// them with a deliberately partial markdown reader, and build a Docs index over them. It
// names no domain type — a consumer supplies its own embedded pages/*.md and the page
// title/description come out of the markdown itself — so every bubblestack app (gdaddon,
// repoview, …) shares one renderer and index instead of copying ~300 lines.
//
// Adding a page is dropping a numbered .md into the consumer's pages dir: the filename
// orders it (fs.ReadDir returns sorted), the first "# " heading is its title, and the
// first line under that heading is its one-line index description.

// DocPage is one parsed manual page: its title + one-line description (both read out of
// the markdown) and the body the renderer folds to width.
type DocPage struct {
	Title string
	Desc  string
	Body  string // everything after the title line (the description is its first paragraph)
}

// ParseDocPages reads dir out of fsys (typically an embed.FS) in filename order and parses
// each entry into a DocPage. A read error yields no pages (an empty menu), never a panic.
func ParseDocPages(fsys fs.FS, dir string) []DocPage {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil
	}
	var pages []DocPage
	for _, e := range entries {
		data, err := fs.ReadFile(fsys, dir+"/"+e.Name())
		if err != nil {
			continue
		}
		pages = append(pages, parseDocPage(string(data)))
	}
	return pages
}

// parseDocPage pulls the title (the first "# " heading) and description (the first
// non-empty line under it) out of a page, leaving the body to the renderer. A page missing
// its heading still reads — it just falls back to its first line as the title.
func parseDocPage(src string) DocPage {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	p := DocPage{Body: src}
	for i, line := range lines {
		if !strings.HasPrefix(line, "# ") {
			continue
		}
		p.Title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
		p.Body = strings.Join(lines[i+1:], "\n")
		for _, rest := range lines[i+1:] {
			if strings.TrimSpace(rest) != "" {
				p.Desc = plain(strings.TrimSpace(rest))
				break
			}
		}
		break
	}
	if p.Title == "" && len(lines) > 0 {
		p.Title = strings.TrimSpace(lines[0])
	}
	return p
}

// DocsIndex is the manual menu: one self-dispatching row per page, each pushing its own
// scrollable DocScreen (the body re-renders at whatever width the screen is given, so it
// re-wraps on resize). title/crumb name the index screen; an empty page set shows a
// placeholder row.
func DocsIndex(title, crumb string, pages []DocPage) *PickerScreen {
	var items []list.Item
	for _, p := range pages {
		p := p
		items = append(items, Item{
			Name: p.Title,
			Desc: p.Desc,
			Pick: func(*core.Shared) core.Action { return core.Push(newDocPage(p)) },
		})
	}
	items = EnsurePlaceholder(items, "(no pages)", "the docs pages didn't compile into this build")
	return NewPicker(items, PickerOpts{Title: title, Crumb: crumb})
}

func newDocPage(p DocPage) *DocScreen {
	return NewDocScreen(DocOpts{
		Title:  p.Title,
		Render: func(width int) string { return RenderMarkdown(p.Body, width) },
	})
}

// The pages are markdown, but only the handful of constructs below are honored — this
// is a reader for prose we write ourselves, not a general markdown implementation (a
// full renderer would drag in a dependency and bring its own theme, which would fight
// core's). Everything is re-flowed to the width the DocScreen hands us:
//
//	## heading      bold accent, blank line above
//	### heading     bold, muted
//	- item          bulleted, wrapped with a hanging indent (an indented line continues it)
//	```fence```     indented, muted, hard-wrapped (never re-flowed)
//	`code`          accent, inline
//	anything else   a paragraph: consecutive lines join, then wrap as one block
//
// Styles are read per call (not cached) so a theme switch repaints the page — the same
// rule core.StyleList follows.

// inlineCode matches a `code span`; the capture is the text between the backticks.
var inlineCode = regexp.MustCompile("`([^`]+)`")

const (
	bulletMark = "• "
	indent     = "  "
	// wrapBreaks are extra wrap points beyond whitespace, so a long path or URL folds
	// at a separator rather than mid-name.
	wrapBreaks = "/_"
)

// RenderMarkdown folds a page's markdown body to width display columns.
func RenderMarkdown(body string, width int) string {
	if width < 20 {
		width = 20
	}
	r := &docRenderer{width: width}
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		r.line(line)
	}
	r.flush()
	return strings.Trim(strings.Join(r.out, "\n"), "\n")
}

// docRenderer walks the page a line at a time, accumulating the current block (a paragraph
// or a bullet) until something ends it. Blocks — not source lines — are what get wrapped,
// so a paragraph the author hard-wrapped at 88 columns re-flows to the terminal instead
// of breaking wherever the .md file happened to break.
type docRenderer struct {
	width int
	out   []string

	pending []string // the lines of the block being accumulated
	bullet  bool     // the pending block is a bullet
	fence   bool     // inside a ``` code fence
}

func (r *docRenderer) line(line string) {
	trimmed := strings.TrimSpace(line)

	if strings.HasPrefix(trimmed, "```") {
		r.flush()
		r.fence = !r.fence
		return
	}
	if r.fence {
		r.emit(codeStyle().Render(indent + core.HardWrap(line, r.width-len(indent))))
		return
	}

	switch {
	case trimmed == "":
		r.flush()
		r.blank()
	case strings.HasPrefix(trimmed, "### "):
		r.heading(subheadingStyle().Render(strings.TrimPrefix(trimmed, "### ")))
	case strings.HasPrefix(trimmed, "## "):
		r.heading(headingStyle().Render(strings.TrimPrefix(trimmed, "## ")))
	case strings.HasPrefix(trimmed, "- "):
		r.flush()
		r.bullet = true
		r.pending = []string{strings.TrimPrefix(trimmed, "- ")}
	case r.bullet && line != trimmed:
		// An indented line under a bullet continues it rather than starting a paragraph.
		r.pending = append(r.pending, trimmed)
	default:
		r.pending = append(r.pending, trimmed)
	}
}

func (r *docRenderer) heading(rendered string) {
	r.flush()
	r.blank()
	r.emit(rendered)
}

// flush wraps the accumulated block and empties it.
func (r *docRenderer) flush() {
	if len(r.pending) == 0 {
		return
	}
	text := inline(strings.Join(r.pending, " "))
	if r.bullet {
		r.emit(hang(bulletMark, text, r.width))
	} else {
		r.emit(ansi.Wrap(text, r.width, wrapBreaks))
	}
	r.pending, r.bullet = nil, false
}

func (r *docRenderer) emit(s string) { r.out = append(r.out, s) }

// blank appends a separator line, collapsing runs (the source's blank line before a
// heading and the one the heading adds itself would otherwise double up).
func (r *docRenderer) blank() {
	if len(r.out) == 0 || r.out[len(r.out)-1] == "" {
		return
	}
	r.emit("")
}

// hang wraps text under a marker, indenting the continuation rows to sit under the first
// row's text so the entry still reads as one unit (the idiom LogPane's wrapped mode uses).
func hang(marker, text string, width int) string {
	w := width - lipgloss.Width(marker)
	if w < 1 {
		w = 1
	}
	rows := strings.Split(ansi.Wrap(text, w, wrapBreaks), "\n")
	for i, row := range rows {
		if i == 0 {
			rows[i] = marker + row
			continue
		}
		rows[i] = indent + row
	}
	return strings.Join(rows, "\n")
}

// inline styles the spans inside a block of prose. Styling before wrapping is safe:
// ansi.Wrap measures display cells, not bytes.
func inline(s string) string {
	return inlineCode.ReplaceAllStringFunc(s, func(m string) string {
		return codeSpanStyle().Render(strings.Trim(m, "`"))
	})
}

// plain strips the inline markup from a line, for places that show it unstyled (the
// index's one-line descriptions).
func plain(s string) string {
	return inlineCode.ReplaceAllString(s, "$1")
}

func headingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.FocusedColor)
}

func subheadingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Bold(true).Foreground(core.MutedColor)
}

func codeStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.MutedColor)
}

func codeSpanStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(core.FocusedColor)
}
