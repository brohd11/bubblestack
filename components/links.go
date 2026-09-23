package components

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/brohd11/bubblestack/core"
	"github.com/brohd11/goutil/textfile"

	"github.com/charmbracelet/x/ansi"
)

// Clickable links in rendered content. RenderMarkdown wraps link labels in OSC 8 escapes,
// which cost no cells and survive wrapping and layout; ScanLinks reads them back from the
// finished text, so any pane can hit-test links without the renderer in scope.

// Link is a hyperlink span in rendered content. Row and Col are content coordinates
// (before scrolling); a pane subtracts its chrome and adds its scroll offset. Path and
// Exists are filled in by LinkHooks.Do.
type Link struct {
	Target string // the destination exactly as the markdown wrote it
	Text   string // the label's visible text on this row, unstyled
	Row    int
	Col    int
	Width  int

	Path   string // the resolved filesystem path; "" when the target is a URL or unresolvable
	Exists bool   // Path names something that is actually there
}

// LinkMap is the hyperlink spans of one rendered block, in reading order.
type LinkMap []Link

// At answers the link covering a content cell, if any.
func (m LinkMap) At(row, col int) (Link, bool) {
	for _, l := range m {
		if l.Row == row && col >= l.Col && col < l.Col+l.Width {
			return l, true
		}
	}
	return Link{}, false
}

// ScanLinks finds the OSC 8 spans in rendered content, measuring columns in display cells.
// A link continuing across a wrap is followed onto the next row, so both halves stay
// clickable.
func ScanLinks(rendered string) LinkMap {
	// The overwhelmingly common case is a page with no links: skip the walk entirely.
	// Both OSC forms are checked so the fast path can't disagree with hyperlinkTarget.
	if !strings.Contains(rendered, "\x1b]8;") && !strings.Contains(rendered, "\x9d8;") {
		return nil
	}
	var (
		links LinkMap
		open  string // the target of the hyperlink currently open; "" ⇒ none
		span  Link
		text  strings.Builder
		live  bool // a span is being accumulated on THIS row
	)
	flush := func() {
		if live && span.Width > 0 {
			span.Text = text.String()
			links = append(links, span)
		}
		live = false
		text.Reset()
	}
	for row, line := range strings.Split(rendered, "\n") {
		col := 0
		var state byte // NormalState; sequences never straddle a row in rendered output
		for len(line) > 0 {
			seq, width, n, newState := ansi.DecodeSequence(line, state, nil)
			if width > 0 {
				if open != "" && !live {
					span, live = Link{Target: open, Row: row, Col: col}, true
				}
				if live {
					span.Width += width
					text.WriteString(seq)
				}
			} else if ansi.HasOscPrefix(seq) {
				if target, ok := hyperlinkTarget(seq); ok {
					flush()
					open = target
				}
			}
			col += width
			line, state = line[n:], newState
		}
		flush() // the row ended; a still-open link opens a fresh span on the next one
	}
	return links
}

// hyperlinkTarget reads an OSC 8 sequence's target (empty for the closing form); ok is
// false for other OSCs. Splitting into at most three fields keeps a ";" in the target.
func hyperlinkTarget(seq string) (string, bool) {
	payload := seq
	switch {
	case strings.HasPrefix(payload, "\x1b]"):
		payload = payload[2:]
	case strings.HasPrefix(payload, "\x9d"):
		payload = payload[1:]
	default:
		return "", false
	}
	payload = strings.TrimSuffix(payload, "\x07")
	payload = strings.TrimSuffix(payload, "\x1b\\")
	payload = strings.TrimSuffix(payload, "\x9c")
	fields := strings.SplitN(payload, ";", 3)
	if len(fields) != 3 || fields[0] != "8" {
		return "", false
	}
	return fields[2], true
}

// LinkHooks decides what a click on a link does, one hook per kind of destination (URL,
// text file, other file); nil hooks do nothing. The URL and file cases are the same
// everywhere; the text case is what differs between apps.
type LinkHooks struct {
	// Base is the directory relative targets resolve against; "" leaves them unresolved (an
	// embedded page set matches targets itself).
	Base string

	URL  func(*core.Shared, Link) core.Action // a scheme, "//" or "www."
	Text func(*core.Shared, Link) core.Action // a path that holds text, or one that didn't resolve
	File func(*core.Shared, Link) core.Action // any other path: a binary, an image, a directory
}

// linkScheme matches a URL scheme of two or more characters, so "C:" is not one.
var linkScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]+:`)

// Do classifies a link and calls the matching hook: URLs to URL, and paths resolved
// against Base and sniffed (textfile.IsText) to Text or File. Unresolvable paths go to Text
// with Exists false. A bare "#fragment" does nothing.
func (h LinkHooks) Do(sh *core.Shared, l Link) core.Action {
	target := strings.TrimSpace(l.Target)
	if target == "" || strings.HasPrefix(target, "#") {
		return core.Action{}
	}
	if linkScheme.MatchString(target) || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "www.") {
		return call(h.URL, sh, l)
	}

	// A path: the fragment is not part of it, and a target written with %20 for a space
	// has to be decoded before it names a file.
	p, _, _ := strings.Cut(target, "#")
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}
	switch {
	case p == "":
		return core.Action{}
	case filepath.IsAbs(p):
		l.Path = filepath.Clean(p)
	case h.Base != "":
		l.Path = filepath.Join(h.Base, p)
	}
	if l.Path == "" {
		return call(h.Text, sh, l)
	}
	info, err := os.Stat(l.Path)
	if err != nil {
		return call(h.Text, sh, l)
	}
	l.Exists = true
	if info.IsDir() || !textfile.IsText(l.Path) {
		return call(h.File, sh, l)
	}
	return call(h.Text, sh, l)
}

// call runs a hook, treating nil as "this kind of link does nothing".
func call(hook func(*core.Shared, Link) core.Action, sh *core.Shared, l Link) core.Action {
	if hook == nil {
		return core.Action{}
	}
	return hook(sh, l)
}
