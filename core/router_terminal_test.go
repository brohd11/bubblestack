package core

import "testing"

// dirScreen is a stubScreen that advertises a directory (core.DirLocator), so the global
// Terminal key resolves to it. ok mirrors LocateDir's second return, letting a test model a
// screen that has no directory to offer.
type dirScreen struct {
	stubScreen
	dir string
	ok  bool
}

func (s dirScreen) LocateDir() (string, bool) { return s.dir, s.ok }

// dirFilterScreen advertises a directory but is also capturing filter text, so the Terminal
// key must pass through (a literal "t") rather than open a terminal.
type dirFilterScreen struct{ dirScreen }

func (dirFilterScreen) Filtering() bool { return true }

// terminalTestRouter builds a router whose only screen is top, with a recording terminal
// action. The returned pointers observe whether the action fired and with what directory.
func terminalTestRouter(top Screen) (Router, *int, *string) {
	calls := 0
	var gotDir string
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return top }}})
	r.SetTerminalAction(func(dir string) Action {
		calls++
		gotDir = dir
		return Action{} // the recorder above is what the assertions read; keep the action inert (no timer)
	})
	return r, &calls, &gotDir
}

// TestTerminalKeyOnDirLocator checks the global Terminal key opens a terminal at the top
// screen's advertised directory — the "t works once you've drilled into a repo" case.
func TestTerminalKeyOnDirLocator(t *testing.T) {
	r, calls, gotDir := terminalTestRouter(dirScreen{dir: "/repo/x", ok: true})
	pump(sized(r), keyMsg(Keys.Terminal.Keys()[0]))
	if *calls != 1 || *gotDir != "/repo/x" {
		t.Fatalf("terminal action: calls=%d dir=%q, want 1 /repo/x", *calls, *gotDir)
	}
}

// TestTerminalKeyFilteringPassesThrough checks that a screen capturing filter text keeps
// "t" (types it) even though it advertises a directory — the gate that lets a filter accept
// the letter.
func TestTerminalKeyFilteringPassesThrough(t *testing.T) {
	r, calls, _ := terminalTestRouter(dirFilterScreen{dirScreen{dir: "/repo/x", ok: true}})
	pump(sized(r), keyMsg(Keys.Terminal.Keys()[0]))
	if *calls != 0 {
		t.Fatalf("filtering screen should keep 't', calls=%d want 0", *calls)
	}
}

// TestTerminalKeyNonLocatorPassesThrough checks that a screen with no DirLocator (a plain
// list) does not trigger the global action, so its own row-level "t" still gets the key.
func TestTerminalKeyNonLocatorPassesThrough(t *testing.T) {
	r, calls, _ := terminalTestRouter(stubScreen{})
	pump(sized(r), keyMsg(Keys.Terminal.Keys()[0]))
	if *calls != 0 {
		t.Fatalf("non-locator should not open terminal, calls=%d want 0", *calls)
	}
}

// TestTerminalKeyEmptyDirPassesThrough checks that a DirLocator that reports no directory
// (ok=false) is treated as if it had none — the key falls through.
func TestTerminalKeyEmptyDirPassesThrough(t *testing.T) {
	r, calls, _ := terminalTestRouter(dirScreen{dir: "", ok: false})
	pump(sized(r), keyMsg(Keys.Terminal.Keys()[0]))
	if *calls != 0 {
		t.Fatalf("no-dir locator should not open terminal, calls=%d want 0", *calls)
	}
}

// TestTerminalKeyNoActionWired checks that without a wired terminal action the key is left
// entirely to the active screen (nil-action guard), matching the Refresh key's contract.
func TestTerminalKeyNoActionWired(t *testing.T) {
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return dirScreen{dir: "/repo/x", ok: true} }}})
	// No SetTerminalAction: globalKey must not panic and must not consume the key.
	act, handled := r.globalKey(keyMsg(Keys.Terminal.Keys()[0]))
	if handled || act.Msg != nil || act.Cmd != nil {
		t.Fatalf("no action wired: want pass-through, got handled=%v act=%+v", handled, act)
	}
}
