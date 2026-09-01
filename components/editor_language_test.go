package components

import "testing"

func testLanguage(path string) *EditorLanguageConfig {
	switch path {
	case "structured.one":
		return &EditorLanguageConfig{
			IndentSpaces: 2,
			OnEnter: func(ctx EditorEnterContext) (EditorEnterAction, bool) {
				return EditorEnterAction{Prefix: ctx.LeadingIndent + "> "}, true
			},
		}
	case "structured.two":
		return &EditorLanguageConfig{
			IndentSpaces: 4,
			OnEnter: func(ctx EditorEnterContext) (EditorEnterAction, bool) {
				return EditorEnterAction{Prefix: ctx.LeadingIndent + ctx.IndentUnit}, true
			},
		}
	}
	return nil
}

func TestEditorDefaultTypingIsLiteral(t *testing.T) {
	s, _ := newEditor(EditorOpts{})
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
	s, _ := newEditor(EditorOpts{Path: "structured.one", ResolveLanguage: testLanguage})
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
	s, _ := newEditor(EditorOpts{Path: "plain.txt", ResolveLanguage: testLanguage})
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
