package components

import (
	"testing"
)

func pressEnter(s *EditorScreen) {
	s.key(nil, keyMsg("enter"))
}

func TestMarkdownEditorKeyContinuesDashLists(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		curX    int
		want    string
	}{
		{name: "plain", path: "notes.md", content: "- item", curX: 6, want: "- item\n- "},
		{name: "space indented", path: "notes.markdown", content: " - item", curX: 7, want: " - item\n - "},
		{name: "tab indented", path: "NOTES.MD", content: "\t- item", curX: 7, want: "\t- item\n\t- "},
		{name: "empty item continues", path: "notes.md", content: "  - ", curX: 4, want: "  - \n  - "},
		{name: "split keeps tail", path: "notes.md", content: "  - before after", curX: 10, want: "  - before\n  -  after"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{Path: tc.path})
			s.setContent(tc.content)
			s.curX = tc.curX
			pressEnter(s)
			if got := buffer(s); got != tc.want {
				t.Fatalf("buffer = %q, want %q", got, tc.want)
			}
			if s.curY != 1 {
				t.Fatalf("cursor row = %d, want 1", s.curY)
			}
		})
	}
}

func TestMarkdownEditorKeyFallsBack(t *testing.T) {
	tests := []struct {
		name    string
		content string
		curX    int
		want    string
	}{
		{name: "asterisk", content: "* item", curX: 6, want: "* item\n"},
		{name: "plus", content: "+ item", curX: 6, want: "+ item\n"},
		{name: "ordered", content: "1. item", curX: 7, want: "1. item\n"},
		{name: "dash without space", content: "-item", curX: 5, want: "-item\n"},
		{name: "cursor inside marker", content: "  - item", curX: 2, want: "  \n- item"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{Path: "notes.md"})
			s.setContent(tc.content)
			s.curX = tc.curX
			pressEnter(s)
			if got := buffer(s); got != tc.want {
				t.Fatalf("buffer = %q, want ordinary newline %q", got, tc.want)
			}
		})
	}
}

func TestYAMLEditorKeyCarriesIndentation(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		content string
		curX    int
		want    string
	}{
		{name: "spaces", path: "config.yaml", content: "  child: value", curX: 14, want: "  child: value\n  "},
		{name: "tabs", path: "config.yml", content: "\t\tchild", curX: 7, want: "\t\tchild\n\t\t"},
		{name: "uppercase extension", path: "CONFIG.YAML", content: "   value", curX: 8, want: "   value\n   "},
		{name: "split keeps tail", path: "config.yaml", content: "  before after", curX: 8, want: "  before\n   after"},
		{name: "cursor inside indent falls back", path: "config.yaml", content: "   value", curX: 1, want: " \n  value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newEditor(EditorOpts{Path: tc.path})
			s.setContent(tc.content)
			s.curX = tc.curX
			pressEnter(s)
			if got := buffer(s); got != tc.want {
				t.Fatalf("buffer = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEditorKeyHandlerFollowsSaveAsExtension(t *testing.T) {
	s, _ := newEditor(EditorOpts{Path: "notes.txt"})
	s.setContent("- item")
	s.curX = len(s.lines[0])
	pressEnter(s)
	if got := buffer(s); got != "- item\n" {
		t.Fatalf("unregistered extension buffer = %q, want ordinary newline", got)
	}

	s.setContent("- item")
	s.curX = len(s.lines[0])
	s.applySaveName("notes.md")
	pressEnter(s)
	if got := buffer(s); got != "- item\n- " {
		t.Fatalf("rename to markdown buffer = %q, want list continuation", got)
	}

	s.setContent("- item")
	s.curX = len(s.lines[0])
	s.applySaveName("notes.txt")
	pressEnter(s)
	if got := buffer(s); got != "- item\n" {
		t.Fatalf("rename away from markdown buffer = %q, want ordinary newline", got)
	}
}
