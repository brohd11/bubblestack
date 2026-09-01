package components

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// File-type coloring for a listing's rows: what an entry IS, and the color that says so.
// FilePanel classifies each row once at read time and hands the result to the delegate
// through core.ColorItem, which applies it as a foreground on the row's title style — no
// ANSI ever enters a Title(), and the selection accent still outranks everything here.
//
// The classification is deliberately cheap. It reads the fs.DirEntry the directory scan
// already produced and the one fs.FileInfo entryDesc already stats for the size line;
// nothing here opens a file. A content sniff (goutil/textfile.IsText) would be a truer
// text-vs-binary answer and costs a 512-byte read PER ROW, which is a different budget
// from the one this component was built to (see entryDesc) — so the type axis is carried
// by the extension tables below instead.

// FileKind is what one listed entry is, for coloring purposes. The zero value is an
// ordinary file, which is drawn unstyled — so an unclassified row inherits the terminal's
// own foreground rather than a color this package chose for it.
type FileKind int

const (
	KindFile FileKind = iota
	KindDir
	KindHiddenDir
	KindHiddenFile
	KindSymlink
	KindExec
	KindCode
	KindDoc
	KindImage
	KindArchive
)

// The palette is raw ANSI 0-15 rather than theme colors, following the convention
// gitstack/repoui/logrender.go sets out for git's decorations: these are SEMANTIC — they
// mean "this is a directory", not "this is emphasis" — and resolving against the user's own
// sixteen-color palette is what makes a listing match the terminal it sits in, the way ls
// does. core.Theme's five colors are framework roles and shouldn't grow filesystem
// vocabulary. Being terminal-defined, they also stay legible on a light background without
// the AdaptiveColor pairs the theme presets need.
//
// The scheme: the bright half is structure you act on (folders, links, programs), the
// normal half is content you read. The yellow family carries the text axis, brightness
// separating source from prose and data. Nothing is final — this one function is the whole
// knob.
func FileKindColor(k FileKind) lipgloss.TerminalColor {
	switch k {
	case KindDir:
		return lipgloss.Color("12") // bright blue
	case KindHiddenDir:
		return lipgloss.Color("4") // blue, the dim counterpart in the basic sixteen
	case KindSymlink:
		return lipgloss.Color("14") // bright cyan
	case KindExec:
		return lipgloss.Color("10") // bright green
	case KindArchive:
		return lipgloss.Color("9") // bright red
	case KindImage:
		return lipgloss.Color("13") // bright magenta
	case KindCode:
		return lipgloss.Color("11") // bright yellow
	case KindDoc:
		return lipgloss.Color("3") // yellow
	case KindHiddenFile:
		return lipgloss.Color("8") // grey
	default:
		return nil // an ordinary file keeps the terminal's foreground
	}
}

// ClassifyFile names the kind of one listed entry. info is the entry's own fs.FileInfo (an
// lstat, so a symlink reports itself rather than its target) and may be nil when the stat
// failed — the exec test is then skipped and the extension tables answer instead, so a
// row whose stat failed loses a distinction rather than its color.
//
// Precedence, and why: a symlink is what the row IS whatever it points at; a directory
// next, so the dot-test can split it; then the dot-test on files, because "private" is a
// category the user reads as one regardless of suffix; then the exec bit, which outranks a
// suffix because a runnable .sh is a program before it is a shell script; then the tables.
func ClassifyFile(d fs.DirEntry, info fs.FileInfo) FileKind {
	name := d.Name()
	if d.Type()&fs.ModeSymlink != 0 {
		return KindSymlink
	}
	if d.IsDir() {
		if isHiddenName(name) {
			return KindHiddenDir
		}
		return KindDir
	}
	if isHiddenName(name) {
		return KindHiddenFile
	}
	if info != nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
		return KindExec
	}
	switch ext := strings.ToLower(filepath.Ext(name)); {
	case codeExts[ext]:
		return KindCode
	case docExts[ext]:
		return KindDoc
	case imageExts[ext]:
		return KindImage
	case archiveExts[ext]:
		return KindArchive
	}
	return KindFile
}

// isHiddenName is the dot-file test, and the "." and ".." entries are not hidden by it: the
// panel's own parent row is spelled ".." and must read as the directory it is.
func isHiddenName(name string) bool {
	return strings.HasPrefix(name, ".") && name != "." && name != ".."
}

// The tables are keyed by lowercase extension WITH the dot, the same key shape as the
// framework's highlighter registry (highlight.go). An extension in none of them is an
// ordinary file — the tables are meant to be extended, not exhaustive.
//
// codeExts is gote's chromaExts list (the extensions chroma has a lexer for), which is
// already curated by language family, plus the markdown pair chroma deliberately leaves to
// bubblestack's own highlighter — minus the data and prose formats, which belong to docExts.
var codeExts = set(
	".go", ".py", ".rb", ".rs", ".java", ".lua", ".php", ".pl", ".r",
	".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
	".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".kt", ".swift", ".dart",
	".sh", ".bash", ".zsh", ".fish", ".vim",
	".html", ".css", ".scss", ".sql",
	".tf", ".gradle", ".proto", ".mk",
)

// docExts is prose, data and configuration: everything you open to read rather than to run.
var docExts = set(
	".md", ".markdown", ".txt", ".rst", ".adoc", ".pdf",
	".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env",
	".csv", ".tsv", ".xml", ".log", ".diff", ".patch",
)

var imageExts = set(
	".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp", ".ico", ".svg",
	".mp3", ".wav", ".flac", ".ogg", ".m4a",
	".mp4", ".mov", ".mkv", ".avi", ".webm",
)

var archiveExts = set(
	".zip", ".tar", ".gz", ".tgz", ".bz2", ".xz", ".zst", ".7z", ".rar",
	".jar", ".deb", ".rpm", ".dmg", ".iso", ".exe", ".dll", ".so", ".dylib",
)

func set(keys ...string) map[string]bool {
	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}
