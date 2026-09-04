package editor

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func completionEditor(t *testing.T, text string) *Screen {
	t.Helper()
	s, _ := newEditor(Opts{ResolveLanguage: func(string) *LanguageConfig {
		return &LanguageConfig{AutoClosingPairs: []Pair{
			{Open: '(', Close: ')'}, {Open: '"', Close: '"'},
		}}
	}})
	s.SetText(text)
	return s
}

func TestEditorCompletionPairsTrailingOpenerAtomically(t *testing.T) {
	s := completionEditor(t, "my")
	if !s.ApplyCompletion(CompletionEdit{
		Range: Range{End: Position{Column: 2}}, Text: "my_func(", PairTrailingOpener: true,
	}) {
		t.Fatal("completion was rejected")
	}
	if got := s.Text(); got != "my_func()" {
		t.Fatalf("paired completion = %q", got)
	}
	if got := s.CursorPosition(); got != (Position{Column: 8}) {
		t.Fatalf("paired caret = %+v", got)
	}
	s.undo()
	if got := s.Text(); got != "my" {
		t.Fatalf("single undo = %q", got)
	}

	s.SetText("my)")
	if !s.ApplyCompletion(CompletionEdit{
		Range: Range{End: Position{Column: 2}}, Text: "my_func(", PairTrailingOpener: true,
	}) || s.Text() != "my_func()" || s.CursorPosition() != (Position{Column: 8}) {
		t.Fatalf("existing closer was not reused: text=%q caret=%+v", s.Text(), s.CursorPosition())
	}

	s.SetText("value")
	if !s.ApplyCompletion(CompletionEdit{
		Range: Range{End: Position{Column: 5}}, Text: "value\"", PairTrailingOpener: true,
	}) || s.Text() != "value\"" {
		t.Fatalf("symmetric delimiter was paired: %q", s.Text())
	}
}

func TestEditorCompletionSnippetStopsRebaseThroughEdits(t *testing.T) {
	s := completionEditor(t, "ca")
	if !s.ApplyCompletion(CompletionEdit{
		Range: Range{End: Position{Column: 2}},
		Text:  "call(first, second)",
		Stops: []CompletionStop{
			{Index: 2, Start: 12, End: 18},
			{Index: 0, Start: 19, End: 19},
			{Index: 1, Start: 5, End: 10},
		},
	}) {
		t.Fatal("snippet completion was rejected")
	}
	if got := s.selectedText(); got != "first" {
		t.Fatalf("first stop selected %q", got)
	}
	s.key(nil, keyMsg("x"))
	s.key(nil, keyMsg("tab"))
	if got := s.selectedText(); got != "second" {
		t.Fatalf("rebased second stop selected %q in %q", got, s.Text())
	}
	s.Update(nil, tea.PasteMsg{Content: "y\nz"})
	s.key(nil, keyMsg("tab"))
	if got := s.Text(); got != "call(x, y\nz)" {
		t.Fatalf("multiline placeholder edit = %q", got)
	}
	if got := s.CursorPosition(); got != (Position{Line: 1, Column: 2}) || s.completion != nil {
		t.Fatalf("final stop = %+v session=%v", got, s.completion != nil)
	}
	s.undo()
	if got := s.Text(); got != "call(x, second)" {
		t.Fatalf("placeholder paste undo = %q", got)
	}
}

func TestEditorCompletionSnippetEscapeAndValidation(t *testing.T) {
	s := completionEditor(t, "x")
	bad := CompletionEdit{
		Range: Range{End: Position{Column: 1}}, Text: "value",
		Stops: []CompletionStop{{Index: 1, Start: 0, End: 1}, {Index: 1, Start: 2, End: 3}},
	}
	if s.ApplyCompletion(bad) || s.Text() != "x" {
		t.Fatalf("invalid stops changed the buffer: %q", s.Text())
	}
	if !s.ApplyCompletion(CompletionEdit{
		Range: Range{End: Position{Column: 1}}, Text: "value",
		Stops: []CompletionStop{{Index: 1, Start: 0, End: 5}},
	}) {
		t.Fatal("valid snippet was rejected")
	}
	if _, action := s.key(nil, keyMsg("esc")); action.Msg != nil || s.completion != nil || s.selectionActive() || s.Text() != "value" {
		t.Fatalf("escape did not retain text and cancel the session: action=%+v", action)
	}
	s.key(nil, keyMsg("tab"))
	if s.Text() != "value\t" {
		t.Fatalf("tab after cancellation = %q", s.Text())
	}
}
