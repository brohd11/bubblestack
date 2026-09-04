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

// Clickable links in rendered content: finding them, and deciding what a click on one
// means. The two halves are deliberately separate, because the SEAM IS THE RENDERED
// STRING, not the call that produced it.
//
// RenderMarkdown wraps every link's label in an OSC 8 hyperlink escape (see inlineOver),
// which costs no display cells and therefore survives wrapping, indenting, table layout
// and everything else the renderer does after a span is styled — the reason a link's
// finished row and column can be recovered at all. ScanLinks reads those spans back out
// of the finished text, so any pane holding OSC 8 content can hit-test it without the
// renderer, its width, or its Render closure being in scope.

// Link is one hyperlink span found in rendered content: where it sits, what it says, and
// where it points. Row and Col are CONTENT coordinates — the row within the whole
// rendered block (before any scroll offset) and the display column within that row — so a
// pane converts a click by subtracting its own chrome and adding its scroll offset.
//
// Path and Exists are filled in by LinkHooks.Do, which is the only thing that knows the
// directory a relative target resolves against; ScanLinks leaves them zero.
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

// ScanLinks finds the OSC 8 hyperlink spans in rendered content, walking it with the ANSI
// decoder so that escape sequences cost no columns and wide graphemes cost what they
// really occupy.
//
// A link left open at the end of a row continues onto the next one and lands a span on
// each: ansi.Wrap carries an escape with the word it precedes, so a label the wrap split
// has its opening sequence on the first row only. Following the state across rows is what
// keeps the second half clickable — the terminal's own rendering of it is what's lost.
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

// hyperlinkTarget reads an OSC 8 sequence's target. ok is false for any other OSC, and
// the target is empty for the reset form (OSC 8 ; ; ST), which closes the open link.
//
// The payload is split into at most three fields so a target holding its own ";" (a query
// string) survives; the params field between the command and the target is the part of
// the OSC 8 spec nothing here uses.
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

// LinkHooks is what a click on a link does, one closure per kind of destination. A nil
// hook means a click on that kind does nothing, which is the default everywhere: a
// component renders links whether or not anyone wired them.
//
// The split is by destination rather than by consumer because two of the three answers
// are the same everywhere — a URL goes to the browser, a file the app can't display gets
// revealed in the file manager — while the third is exactly what differs: gote opens a
// text file as an editor buffer, an embedded manual opens its own sibling page and must
// never reach outside its embedded FS.
type LinkHooks struct {
	// Base is the directory a relative target resolves against; "" leaves targets
	// unresolved (Link.Path empty), which is what an embedded page set wants — it has no
	// directory, and its own hook matches on the target instead.
	Base string

	URL  func(*core.Shared, Link) core.Action // a scheme, "//" or "www."
	Text func(*core.Shared, Link) core.Action // a path that holds text, or one that didn't resolve
	File func(*core.Shared, Link) core.Action // any other path: a binary, an image, a directory
}

// linkScheme matches a URL scheme. Two or more characters before the ":" deliberately:
// one would make the "C:" of a Windows path a scheme, and no scheme worth opening is a
// single letter.
var linkScheme = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.\-]+:`)

// Do classifies a link and hands it to the matching hook, resolving a relative path
// against Base and sniffing its content (goutil/textfile, so a file with no extension at
// all is judged correctly) to choose between Text and File. A path that does not resolve
// — every link in an embedded manual, and a broken one anywhere — goes to Text with
// Exists false, leaving "is this real?" to the hook that knows.
//
// A bare "#fragment" is an in-page anchor and does nothing: the renderer re-flows a page,
// so it has no heading index to jump to.
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
