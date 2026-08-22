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

// TestOpenDirKeyFiresWhileFiltering checks OpenDir still fires on a filtering screen. It is
// the one dir key on a modified combo ("ctrl+t"), and modifiedKey exempts those from the
// text-capture gate — a filter can't type ctrl+t, so there is nothing to steal. The
// unmodified siblings ("t"/"T") pass through instead; see the Terminal tests.
func TestOpenDirKeyFiresWhileFiltering(t *testing.T) {
	r, calls, _ := openDirTestRouter(dirFilterScreen{dirScreen{dir: "/repo/x", ok: true}})
	pump(sized(r), keyMsg(Keys.OpenDir.Keys()[0]))
	if *calls != 1 {
		t.Fatalf("ctrl+t should survive a filtering screen, calls=%d want 1", *calls)
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

// TestDirKeysDistinct checks the three DirLocator keys don't cross-fire: "t" runs only the
// inline terminal action, "T" only the window action and "ctrl+t" only the open-dir action,
// on a router wired for all three.
func TestDirKeysDistinct(t *testing.T) {
	var inline, window, open int
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return dirScreen{dir: "/repo/x", ok: true} }}})
	r.SetTerminalAction(func(string) Action { inline++; return Action{} })
	r.SetTerminalWindowAction(func(string) Action { window++; return Action{} })
	r.SetOpenDirAction(func(string) Action { open++; return Action{} })

	tm := sized(r)
	for _, tc := range []struct {
		name                       string
		key                        string
		wInline, wWindow, wOpenDir int
	}{
		{"t", Keys.Terminal.Keys()[0], 1, 0, 0},
		{"T", Keys.TerminalWindow.Keys()[0], 1, 1, 0},
		{"ctrl+t", Keys.OpenDir.Keys()[0], 1, 1, 1},
	} {
		tm = pump(tm, keyMsg(tc.key))
		if inline != tc.wInline || window != tc.wWindow || open != tc.wOpenDir {
			t.Fatalf("after %q: inline=%d window=%d open=%d, want %d %d %d",
				tc.name, inline, window, open, tc.wInline, tc.wWindow, tc.wOpenDir)
		}
	}
}

// TestTerminalWindowKeyFilteringPassesThrough checks "T" is left to a filtering screen, the
// same gate the inline "t" gets — both are plain letters a filter can legitimately type.
func TestTerminalWindowKeyFilteringPassesThrough(t *testing.T) {
	calls := 0
	sh := NewShared(nil)
	sh.Chrome = &Chrome{Output: &fakeOutput{}, Status: &fakeStatus{}}
	top := dirFilterScreen{dirScreen{dir: "/repo/x", ok: true}}
	r := NewRouter(sh, []TabEntry{{Title: "T", New: func(*Shared) Screen { return top }}})
	r.SetTerminalWindowAction(func(string) Action { calls++; return Action{} })
	pump(sized(r), keyMsg(Keys.TerminalWindow.Keys()[0]))
	if calls != 0 {
		t.Fatalf("filtering screen should keep 'T', calls=%d want 0", calls)
	}
}
