package editor

import (
	"strings"

	"github.com/brohd11/bubblestack/internal/tuitest"

	tea "charm.land/bubbletea/v2"
)

// keyMsg and press are tuitest's constructors under the names these tests already call.
// The implementations live in internal/tuitest so this package and components drive
// Update from one string→Key and one mouse-message shape.
func keyMsg(s string) tea.KeyPressMsg { return tuitest.KeyMsg(s) }

func press(x, y int, b tea.MouseButton) tea.MouseMsg { return tuitest.Press(x, y, b) }

// collapse strips all whitespace, for assertions about what a render contains rather
// than how it is spaced.
func collapse(s string) string { return strings.Join(strings.Fields(s), "") }
