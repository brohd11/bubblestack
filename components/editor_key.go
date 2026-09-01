package components

import (
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// editorKeyHandler gets first refusal on a key for buffers carrying one of its
// registered extensions. Handlers are internal editor behaviors: they may mutate the
// buffer directly and report true, or leave it untouched and report false so the
// editor's ordinary key handling runs.
type editorKeyHandler func(*EditorScreen, tea.KeyPressMsg) bool

var (
	editorKeyMu       sync.RWMutex
	editorKeyHandlers = map[string]editorKeyHandler{}
)

// registerEditorKeyHandler associates handler with each extension. Extensions are
// normalized here as well as at lookup so registration is case-insensitive.
func registerEditorKeyHandler(handler editorKeyHandler, exts ...string) {
	editorKeyMu.Lock()
	defer editorKeyMu.Unlock()
	for _, ext := range exts {
		editorKeyHandlers[strings.ToLower(ext)] = handler
	}
}

func lookupEditorKeyHandler(ext string) editorKeyHandler {
	editorKeyMu.RLock()
	defer editorKeyMu.RUnlock()
	return editorKeyHandlers[strings.ToLower(ext)]
}

// leadingWhitespace returns the raw space/tab prefix. Tabs stay tabs in the buffer;
// rendering remains responsible for expanding them to cells.
func leadingWhitespace(line []rune) []rune {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return line[:i]
}
