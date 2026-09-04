package editor

import "testing"

func testLanguage(path string) *LanguageConfig {
	switch path {
	case "structured.one":
		return &LanguageConfig{
			IndentSpaces: 2,
			OnEnter: func(ctx EnterContext) (EnterAction, bool) {
				return EnterAction{Prefix: ctx.LeadingIndent + "> "}, true
			},
		}
	case "structured.two":
		return &LanguageConfig{
			IndentSpaces: 4,
			OnEnter: func(ctx EnterContext) (EnterAction, bool) {
				return EnterAction{Prefix: ctx.LeadingIndent + ctx.IndentUnit}, true
			},
		}
	case "structured.block":
		return &LanguageConfig{
			IndentSpaces:     2,
			AutoClosingPairs: []Pair{{Open: '{', Close: '}'}},
			OnEnter: func(ctx EnterContext) (EnterAction, bool) {
				return EnterAction{
					Prefix: ctx.LeadingIndent + ctx.IndentUnit,
					Block:  true,
					Closer: ctx.LeadingIndent,
				}, true
			},
		}
	case "commented.slash":
		return &LanguageConfig{
			IndentSpaces: 2,
			LineComment:  "//",
			OnEnter: func(ctx EnterContext) (EnterAction, bool) {
				return EnterAction{Prefix: ctx.LeadingIndent + ctx.IndentUnit}, true
			},
		}
	case "commented.hash":
		// No OnEnter at all: a profile may declare only a comment (TOML, vimscript).
		return &LanguageConfig{IndentSpaces: 2, LineComment: "#"}
	case "commented.block":
		return &LanguageConfig{IndentSpaces: 2, BlockComment: [2]string{"/*", "*/"}}
	case "commented.none":
		return &LanguageConfig{IndentSpaces: 2}
	case "structured.rewrite":
		return &LanguageConfig{
			IndentSpaces: 2,
			OnEnter: func(ctx EnterContext) (EnterAction, bool) {
				return EnterAction{Rewrite: true, Line: ctx.LeadingIndent + "done"}, true
			},
		}
	}
	return nil
}

func TestEditorDefaultTypingIsLiteral(t *testing.T) {
	s, _ := newEditor(Opts{})
	typeRunes(s, '(')
	s.key(nil, keyMsg("enter"))
	typeRunes(s, '"')
	if got, want := buffer(s), "(\n\""; got != want {
		t.Fatalf("unconfigured editor = %q, want literal %q", got, want)
	}
	if s.hl != nil || s.onEnter != nil || s.autoPairs != nil || s.surroundPairs != nil {
		t.Fatal("an unconfigured editor retained language behavior")
	}
}

func TestEditorConfiguredEnterIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(Opts{Path: "structured.one", ResolveLanguage: testLanguage})
	s.setContent("  item")
	s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "  item\n  > "; got != want || len(s.undoStack) != 1 {
		t.Fatalf("structured Enter = %q history=%d, want %q/1", got, len(s.undoStack), want)
	}
	undoEditor(s)
	if got := buffer(s); got != "  item" {
		t.Fatalf("undo structured Enter = %q", got)
	}
}

func TestEditorLanguageFollowsPathChanges(t *testing.T) {
	s, _ := newEditor(Opts{Path: "plain.txt", ResolveLanguage: testLanguage})
	if s.onEnter != nil || s.indentLabel() != "auto (tab)" {
		t.Fatal("plain path should start literal with tab indentation")
	}
	s.applySaveName("structured.one")
	if s.onEnter == nil || s.indentLabel() != "auto (2 spaces)" {
		t.Fatalf("first resolved profile = handler %v indent %q", s.onEnter != nil, s.indentLabel())
	}
	s.SetPath("structured.two")
	if s.onEnter == nil || s.indentLabel() != "auto (4 spaces)" {
		t.Fatalf("second resolved profile = handler %v indent %q", s.onEnter != nil, s.indentLabel())
	}
	s.SetPath("plain.txt")
	if s.onEnter != nil || s.indentLabel() != "auto (tab)" {
		t.Fatal("renaming away from a profile should clear every derived behavior")
	}
}

// The Block shape has to leave the caret on the line it opened, not past the closer it
// pushed down — the whole point of the field is that the next thing typed lands inside.
func TestEditorBlockEnterOpensAndHoldsTheCaret(t *testing.T) {
	s, _ := newEditor(Opts{Path: "structured.block", ResolveLanguage: testLanguage})
	s.setContent("  data = ")
	s.curX, s.wantX = len(s.lines[0]), len(s.lines[0])
	typeRunes(s, '{')
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "  data = {\n    \n  }"; got != want {
		t.Fatalf("block Enter = %q, want %q", got, want)
	}
	if s.curY != 1 || s.curX != 4 {
		t.Fatalf("caret = (%d,%d), want the end of the opened line (1,4)", s.curY, s.curX)
	}
	typeRunes(s, 'x')
	if got, want := buffer(s), "  data = {\n    x\n  }"; got != want {
		t.Fatalf("typing after a block Enter = %q, want %q", got, want)
	}
}

// Three splices in one gesture still owe the user exactly one ctrl+z.
func TestEditorBlockEnterIsOneUndoStep(t *testing.T) {
	s, _ := newEditor(Opts{Path: "structured.block", ResolveLanguage: testLanguage})
	s.setContent("{}")
	s.curX, s.wantX = 1, 1
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "{\n  \n}"; got != want || len(s.undoStack) != 1 {
		t.Fatalf("block Enter = %q history=%d, want %q/1", got, len(s.undoStack), want)
	}
	undoEditor(s)
	if got := buffer(s); got != "{}" {
		t.Fatalf("undo block Enter = %q, want the original line back", got)
	}
	redoEditor(s)
	if got, want := buffer(s), "{\n  \n}"; got != want {
		t.Fatalf("redo block Enter = %q, want %q", got, want)
	}
}

// Rewrite replaces the caret's line and adds NO line: it is how a handler ends a list
// rather than continuing one, so a buffer that grew a row here would be the bug.
func TestEditorRewriteEnterReplacesTheLine(t *testing.T) {
	s, _ := newEditor(Opts{Path: "structured.rewrite", ResolveLanguage: testLanguage})
	s.setContent("one\n  two\nthree")
	s.curY, s.curX, s.wantX = 1, len(s.lines[1]), len(s.lines[1])
	s.key(nil, keyMsg("enter"))
	if got, want := buffer(s), "one\n  done\nthree"; got != want || len(s.undoStack) != 1 {
		t.Fatalf("rewrite Enter = %q history=%d, want %q/1", got, len(s.undoStack), want)
	}
	if s.curY != 1 || s.curX != len("  done") {
		t.Fatalf("caret = (%d,%d), want the end of the rewritten line", s.curY, s.curX)
	}
	undoEditor(s)
	if got := buffer(s); got != "one\n  two\nthree" {
		t.Fatalf("undo rewrite Enter = %q", got)
	}
}
