package components

import (
	"io/fs"
	"path"
	"strings"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/bubblestack/sysopen"

	"charm.land/bubbles/v2/list"
)

// The in-TUI manual: parse markdown pages, render them, and build a Docs index. Apps keep
// pages at doc/embedded/*.md, embedded by a small doc package's Pages(). The filename
// orders a page, its first "# " heading is the title, and the next line the description.

// DocPage is one parsed manual page: its title + one-line description (both read out of
// the markdown) and the body the renderer folds to width.
type DocPage struct {
	Title string
	File  string // the page's filename in the embedded set — what a link between pages names
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
		page := parseDocPage(string(data))
		page.File = e.Name()
		pages = append(pages, page)
	}
	return pages
}

// parseDocPage takes the first "# " heading as the title and the first line under it as
// the description; without a heading, the first line is the title.
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

// DocsIndex is the manual menu: one row per page, each pushing a DocScreen that re-wraps
// on resize. An empty page set shows a placeholder.
func DocsIndex(title, crumb string, pages []DocPage) *PickerScreen {
	var items []list.Item
	for _, p := range pages {
		items = append(items, Item{
			Name: p.Title,
			Desc: p.Desc,
			Pick: func(*core.Shared) core.Action { return core.Push(newDocPage(p, pages)) },
		})
	}
	items = EnsurePlaceholder(items, "(no pages)", "the docs pages didn't compile into this build")
	return NewPicker(items, PickerOpts{Title: title, Crumb: crumb})
}

// newDocPage builds one page's screen: links to other pages push them, URLs open the
// browser. Page links resolve within the embedded set only, never the filesystem.
func newDocPage(p DocPage, pages []DocPage) *DocScreen {
	return NewDocScreen(DocOpts{
		Title:  p.Title,
		Render: func(width int) string { return RenderMarkdown(p.Body, width) },
		Links: LinkHooks{
			URL: func(_ *core.Shared, l Link) core.Action { return sysopen.URL(l.Target) },
			Text: func(_ *core.Shared, l Link) core.Action {
				next, ok := findDocPage(pages, l.Target)
				if !ok {
					return core.Action{}
				}
				return core.Push(newDocPage(next, pages))
			},
		},
	})
}

// findDocPage resolves a page link by the target's last segment against filenames, then
// titles ("02-config.md" and "Config" both work). Paths and "#fragments" are dropped.
func findDocPage(pages []DocPage, target string) (DocPage, bool) {
	name, _, _ := strings.Cut(target, "#")
	name = path.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == "/" {
		return DocPage{}, false
	}
	for _, p := range pages {
		if p.File == name {
			return p, true
		}
	}
	for _, p := range pages {
		if strings.EqualFold(p.Title, name) {
			return p, true
		}
	}
	return DocPage{}, false
}

// DocsItem is the Actions-menu "? Docs" row over pages; ok is false when there are none.
func DocsItem(pages []DocPage) (item list.Item, ok bool) {
	if len(pages) == 0 {
		return nil, false
	}
	return Item{
		Name: "? Docs",
		Desc: docTopics(pages),
		Pick: func(sh *core.Shared) core.Action { return core.Push(DocsIndex("Docs", "Docs", pages)) },
	}, true
}

// docTopics summarizes the page titles for the Docs row, at most four.
func docTopics(pages []DocPage) string {
	topics := make([]string, len(pages))
	for i, p := range pages {
		topics[i] = strings.ToLower(p.Title)
	}
	if len(topics) > 4 {
		return strings.Join(topics[:4], ", ") + ", …"
	}
	return strings.Join(topics, ", ")
}
