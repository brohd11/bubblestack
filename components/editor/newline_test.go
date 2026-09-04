package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSavePreservesPureCRLF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "windows.txt")
	s := &Screen{path: path}
	s.setContent("one\r\ntwo\r\n")
	if got := s.Text(); got != "one\ntwo\n" {
		t.Fatalf("Text() = %q, want normalized LF", got)
	}
	s.lines[0] = []rune("changed")
	s.editSeq++
	msg := s.saveCmd()()
	if saved, ok := msg.(editorSavedMsg); !ok || saved.err != nil {
		t.Fatalf("save result = %#v", msg)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), "changed\r\ntwo\r\n"; got != want {
		t.Fatalf("saved content = %q, want %q", got, want)
	}
}

func TestSaveUsesLFForLFAndMixedContent(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"lf", "one\ntwo\n", "one\ntwo\n"},
		{"mixed", "one\r\ntwo\n", "one\ntwo\n"},
		{"no newline", "one", "one"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "text.txt")
			s := &Screen{path: path}
			s.setContent(tc.content)
			msg := s.saveCmd()()
			if saved, ok := msg.(editorSavedMsg); !ok || saved.err != nil {
				t.Fatalf("save result = %#v", msg)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(data); got != tc.want {
				t.Fatalf("saved content = %q, want %q", got, tc.want)
			}
		})
	}
}
