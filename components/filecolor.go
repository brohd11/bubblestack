package components

import (
	"image/color"
	"io/fs"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// File-type coloring for listing rows. FilePanel classifies each row once at read time
// and passes it through core.ColorItem; the selection accent still wins. Classification
// uses only the directory entry and the stat already taken (no content sniffing), so
// the type comes from extension tables.

// FileColorMode controls the built-in classification palette, independently of a
// host's TitleColor override.
type FileColorMode int

const (
	FileColorsNone FileColorMode = iota
	FileColorsDirs
	FileColorsAll
)

// FileKind is what a listed entry is. The zero value is an ordinary, unstyled file.
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

// FileKindColor uses raw ANSI 0-15 rather than theme colors: these are semantic, like
// ls, and follow the user's terminal palette (legible on light backgrounds too). Bright
// colors are structure (folders, links, programs), normal ones content; the yellow
// family is text.
func FileKindColor(k FileKind) color.Color {
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

// ClassifyFile names an entry's kind. info is its lstat, possibly nil (then the exec test
// is skipped). Precedence: symlink, directory, dotfile, executable, extension tables.
func ClassifyFile(d fs.DirEntry, info fs.FileInfo) FileKind {
	name := d.Name()
	// Symlink before directory, even when the link targets a folder, as ls does.
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

// The tables are keyed by lowercase extension with the dot, and are meant to be extended.
// codeExts covers the languages chroma lexes plus markdown; data and prose are docExts.
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
