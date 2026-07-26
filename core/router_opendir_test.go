package core

import "testing"

// openDirTestRouter builds a router whose only screen is top, with a recording OpenDir
// action (the file-manager sibling of the terminal action). Reuses dirScreen/dirFilterScreen
// from router_terminal_test.go.
func openDirTestRouter(top Screen) (Router, *int, *string) {
	calls := 0
	var gotDir string
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return top }}})
	r.SetOpenDirAction(func(dir string) Action {
		calls++
		gotDir = dir
		return Action{}
	})
	return r, &calls, &gotDir
}

// TestOpenDirKeyOnDirLocator checks the global OpenDir key ("T") opens the file manager at
// the top screen's advertised directory.
func TestOpenDirKeyOnDirLocator(t *testing.T) {
	r, calls, gotDir := openDirTestRouter(dirScreen{dir: "/repo/x", ok: true})
	pump(sized(r), keyMsg(Keys.OpenDir.Keys()[0]))
	if *calls != 1 || *gotDir != "/repo/x" {
		t.Fatalf("open-dir action: calls=%d dir=%q, want 1 /repo/x", *calls, *gotDir)
	}
}

// TestOpenDirKeyFilteringPassesThrough checks a filtering screen keeps "T" (types it) even
// though it advertises a directory.
func TestOpenDirKeyFilteringPassesThrough(t *testing.T) {
	r, calls, _ := openDirTestRouter(dirFilterScreen{dirScreen{dir: "/repo/x", ok: true}})
	pump(sized(r), keyMsg(Keys.OpenDir.Keys()[0]))
	if *calls != 0 {
		t.Fatalf("filtering screen should keep 'T', calls=%d want 0", *calls)
	}
}

// TestOpenDirKeyNonLocatorPassesThrough checks a screen with no DirLocator does not trigger
// the global action.
func TestOpenDirKeyNonLocatorPassesThrough(t *testing.T) {
	r, calls, _ := openDirTestRouter(stubScreen{})
	pump(sized(r), keyMsg(Keys.OpenDir.Keys()[0]))
	if *calls != 0 {
		t.Fatalf("non-locator should not open the file manager, calls=%d want 0", *calls)
	}
}

// TestTerminalAndOpenDirDistinct checks the two DirLocator keys don't cross-fire: "t" runs
// only the terminal action and "T" only the open-dir action, on a router wired for both.
func TestTerminalAndOpenDirDistinct(t *testing.T) {
	termCalls, openCalls := 0, 0
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return dirScreen{dir: "/repo/x", ok: true} }}})
	r.SetTerminalAction(func(string) Action { termCalls++; return Action{} })
	r.SetOpenDirAction(func(string) Action { openCalls++; return Action{} })

	tm := sized(r)
	tm = pump(tm, keyMsg(Keys.Terminal.Keys()[0]))
	if termCalls != 1 || openCalls != 0 {
		t.Fatalf("after 't': term=%d open=%d, want 1 0", termCalls, openCalls)
	}
	pump(tm, keyMsg(Keys.OpenDir.Keys()[0]))
	if termCalls != 1 || openCalls != 1 {
		t.Fatalf("after 'T': term=%d open=%d, want 1 1", termCalls, openCalls)
	}
}
