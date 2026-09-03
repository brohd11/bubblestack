package components

import (
	"strings"
	"testing"
)

func commentEditor(t *testing.T, path, content string) *EditorScreen {
	t.Helper()
	s, _ := newEditor(EditorOpts{Path: path, ResolveLanguage: testLanguage})
	s.setContent(content)
	return s
}

func toggle(s *EditorScreen) { s.key(nil, keyMsg("ctrl+_")) }

func TestToggleCommentSingleLine(t *testing.T) {
	s := commentEditor(t, "commented.slash", "  call()")
	s.curY, s.curX, s.wantX = 0, 8, 8
	toggle(s)
	if got, want := buffer(s), "  // call()"; got != want {
		t.Fatalf("comment = %q, want %q", got, want)
	}
	if s.curX != 11 {
		t.Fatalf("caret x = %d, want 11 — it should ride along with its text", s.curX)
	}
	toggle(s)
	if got, want := buffer(s), "  call()"; got != want {
		t.Fatalf("uncomment = %q, want %q", got, want)
	}
	if s.curX != 8 {
		t.Fatalf("caret x = %d, want 8", s.curX)
	}
}

// The delimiter goes at the span's shallowest indent, so the block keeps its shape, and
// uncommenting has to return the text byte for byte.
func TestToggleCommentBlockRoundTrips(t *testing.T) {
	const src = "func f() {\n\tif x {\n\t\tcall()\n\t}\n}"
	s := commentEditor(t, "commented.slash", src)
	selectRange(s, 1, 0, 3, 2)
	toggle(s)
	// One uniform delimiter column, so the block stays a rectangle: the deeper line keeps
	// its extra indent AFTER the delimiter rather than pushing the comment right.
	want := "func f() {\n\t// if x {\n\t// \tcall()\n\t// }\n}"
	if got := buffer(s); got != want {
		t.Fatalf("comment block = %q, want %q", got, want)
	}
	toggle(s)
	if got := buffer(s); got != src {
		t.Fatalf("round trip = %q, want the original %q", got, src)
	}
}

// A partly commented span finishes the job instead of flipping each line, which would leave
// it exactly as mixed as it started.
func TestToggleCommentPartialSpanComments(t *testing.T) {
	s := commentEditor(t, "commented.slash", "// one\ntwo\n// three")
	selectRange(s, 0, 0, 2, 8)
	toggle(s)
	want := "// // one\n// two\n// // three"
	if got := buffer(s); got != want {
		t.Fatalf("partial span = %q, want %q", got, want)
	}
	toggle(s)
	if got, want := buffer(s), "// one\ntwo\n// three"; got != want {
		t.Fatalf("back = %q, want %q", got, want)
	}
}

func TestToggleCommentSkipsBlankLines(t *testing.T) {
	s := commentEditor(t, "commented.slash", "one\n\n   \ntwo")
	selectRange(s, 0, 0, 3, 3)
	toggle(s)
	want := "// one\n\n   \n// two"
	if got := buffer(s); got != want {
		t.Fatalf("blank lines = %q, want them untouched: %q", got, want)
	}
	// They must not vote on direction either — every non-blank line is commented, so this
	// uncomments rather than commenting again.
	toggle(s)
	if got, want := buffer(s), "one\n\n   \ntwo"; got != want {
		t.Fatalf("uncomment = %q, want %q", got, want)
	}
}

// Hand-written comments have the space or they don't.
func TestToggleCommentUncommentsWithoutSpace(t *testing.T) {
	s := commentEditor(t, "commented.slash", "//tight\n// loose")
	selectRange(s, 0, 0, 1, 8)
	toggle(s)
	if got, want := buffer(s), "tight\nloose"; got != want {
		t.Fatalf("uncomment = %q, want %q", got, want)
	}
}

func TestToggleCommentHashAndBlock(t *testing.T) {
	s := commentEditor(t, "commented.hash", "key = 1")
	toggle(s)
	if got, want := buffer(s), "# key = 1"; got != want {
		t.Fatalf("hash comment = %q, want %q", got, want)
	}

	// A block-only language wraps each line on its own, so each stays reversible.
	s = commentEditor(t, "commented.block", ".btn {\n  color: red;\n}")
	selectRange(s, 0, 0, 2, 1)
	toggle(s)
	want := "/* .btn { */\n/*   color: red; */\n/* } */"
	if got := buffer(s); got != want {
		t.Fatalf("block comment = %q, want %q", got, want)
	}
	toggle(s)
	if got, want := buffer(s), ".btn {\n  color: red;\n}"; got != want {
		t.Fatalf("block round trip = %q, want %q", got, want)
	}
}

// A profile with neither delimiter has no gesture, and must not record an empty edit.
func TestToggleCommentWithoutDelimitersIsNoOp(t *testing.T) {
	s := commentEditor(t, "commented.none", "{\n  \"a\": 1\n}")
	selectRange(s, 0, 0, 2, 1)
	toggle(s)
	if got, want := buffer(s), "{\n  \"a\": 1\n}"; got != want {
		t.Fatalf("no-delimiter toggle = %q, want %q", got, want)
	}
	if len(s.undoStack) != 0 {
		t.Fatalf("history = %d entries, want none for a no-op", len(s.undoStack))
	}
}

// However many lines it splices, the gesture owes exactly one ctrl+z.
func TestToggleCommentIsOneUndoStep(t *testing.T) {
	const src = "one\ntwo\nthree"
	s := commentEditor(t, "commented.slash", src)
	selectRange(s, 0, 0, 2, 5)
	toggle(s)
	if got := buffer(s); !strings.HasPrefix(got, "// one") || len(s.undoStack) != 1 {
		t.Fatalf("toggle = %q history=%d, want one entry", got, len(s.undoStack))
	}
	undoEditor(s)
	if got := buffer(s); got != src {
		t.Fatalf("undo = %q, want %q", got, src)
	}
	redoEditor(s)
	if got, want := buffer(s), "// one\n// two\n// three"; got != want {
		t.Fatalf("redo = %q, want %q", got, want)
	}
}

// alt+/ is the fallback for terminals that swallow ctrl+/; it must do the identical thing.
func TestToggleCommentAltSlashMatchesCtrl(t *testing.T) {
	a := commentEditor(t, "commented.slash", "  call()")
	a.key(nil, keyMsg("ctrl+_"))
	b := commentEditor(t, "commented.slash", "  call()")
	b.key(nil, keyMsg("alt+/"))
	if buffer(a) != buffer(b) {
		t.Fatalf("alt+/ = %q, ctrl+_ = %q — the two bindings must agree", buffer(b), buffer(a))
	}
}

func TestCommentEnterContinues(t *testing.T) {
	for _, tc := range []struct {
		name, path, content, want string
	}{
		{"slash", "commented.slash", "// takes a path", "// takes a path\n// "},
		{"indented", "commented.slash", "  // note", "  // note\n  // "},
		{"hash with no handler", "commented.hash", "# note", "# note\n# "},
		{"keeps the gap", "commented.slash", "//no space", "//no space\n//"},
		// A comment's structure outranks the language's: this must not indent.
		{"colon in prose", "commented.slash", "// case 1:", "// case 1:\n// "},
		// Not a comment: the profile's own handler takes it, indenting by its unit.
		{"code falls through", "commented.slash", "call()", "call()\n  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := commentEditor(t, tc.path, tc.content)
			s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
			s.key(nil, keyMsg("enter"))
			if got := buffer(s); got != tc.want {
				t.Fatalf("Enter = %q, want %q", got, tc.want)
			}
		})
	}
}

// An empty comment ends the run rather than laying down another one — the exit that keeps
// continuation from being something you have to backspace out of.
func TestCommentEnterEndsTheRun(t *testing.T) {
	s := commentEditor(t, "commented.slash", "  // note")
	s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "  // note\n  // "; got != want {
		t.Fatalf("first Enter = %q, want %q", got, want)
	}
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "  // note\n  "; got != want {
		t.Fatalf("second Enter = %q, want the marker gone: %q", got, want)
	}
}

// A shebang is a file header, not the opening line of a comment run.
func TestCommentEnterSkipsShebang(t *testing.T) {
	s := commentEditor(t, "commented.hash", "#!/bin/sh\n# note")
	s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "#!/bin/sh\n\n# note"; got != want {
		t.Fatalf("Enter on a shebang = %q, want a plain split %q", got, want)
	}
	// The same text further down IS a comment run, so only row 0 is special.
	s = commentEditor(t, "commented.hash", "x\n#!not a shebang")
	s.curY = 1
	s.curX, s.wantX = len(s.lines[1]), len(s.lines[1])
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "x\n#!not a shebang\n#"; got != want {
		t.Fatalf("Enter below row 0 = %q, want %q", got, want)
	}
}

// Both keycodes stay registered under the one help label. ctrl+_ is the binding that makes
// ctrl+/ work — the chord puts byte 0x1f on the wire and the decoder names that ctrl+_ —
// so dropping it would silently cost the advertised key while alt+/ went on working, which
// is the kind of half-broken nothing else here would notice.
func TestCommentBindingAdvertisesBothKeys(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "commented.slash", ResolveLanguage: testLanguage})
	for _, binding := range s.HelpBindings() {
		if binding.Help().Key != "ctrl+/" {
			continue
		}
		keys := strings.Join(binding.Keys(), " ")
		if !strings.Contains(keys, "ctrl+_") || !strings.Contains(keys, "alt+/") {
			t.Fatalf("comment binding keys = %q, want both ctrl+_ and alt+/", keys)
		}
		if binding.Help().Desc != "comment" {
			t.Fatalf("comment binding help = %q", binding.Help().Desc)
		}
		return
	}
	t.Fatal("no ctrl+/ binding in the editor's key hints")
}
