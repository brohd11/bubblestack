package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points $HOME at a temp dir for the duration of a test, so the store reads and
// writes an isolated ~/.bubblestack rather than the developer's real one.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// TestSaveThemeRoundTrip: SaveTheme then Theme returns what was written, and the file
// lands at ~/.bubblestack/config.yml.
func TestSaveThemeRoundTrip(t *testing.T) {
	home := withHome(t)

	if got := Theme(); got != "" {
		t.Fatalf("Theme() on a fresh home = %q, want empty", got)
	}
	if err := SaveTheme("godot"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	if got := Theme(); got != "godot" {
		t.Fatalf("Theme() after save = %q, want %q", got, "godot")
	}
	if _, err := os.Stat(filepath.Join(home, ".bubblestack", "config.yml")); err != nil {
		t.Fatalf("config.yml not written: %v", err)
	}

	// A second save overwrites the same key rather than appending a duplicate.
	if err := SaveTheme("amber"); err != nil {
		t.Fatalf("SaveTheme (second): %v", err)
	}
	if got := Theme(); got != "amber" {
		t.Fatalf("Theme() after second save = %q, want %q", got, "amber")
	}
}

// TestSaveThemePreservesComments: the surgical writer keeps a pre-existing comment and
// any unrelated key intact when it sets theme — the property that lets the file carry
// more than one setting (and hand-written comments) over time.
func TestSaveThemePreservesComments(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".bubblestack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := "# bubblestack config\nfuture_knob: keepme\ntheme: mono\n"
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveTheme("red"); err != nil {
		t.Fatalf("SaveTheme: %v", err)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "# bubblestack config") {
		t.Errorf("comment lost:\n%s", s)
	}
	if !strings.Contains(s, "future_knob: keepme") {
		t.Errorf("unrelated key lost:\n%s", s)
	}
	if got := Theme(); got != "red" {
		t.Errorf("Theme() = %q, want %q", got, "red")
	}
}

// TestLoadMissingFile: a missing file is the zero Config, not an error.
func TestLoadMissingFile(t *testing.T) {
	withHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg.Theme != "" {
		t.Fatalf("Theme = %q, want empty", cfg.Theme)
	}
}
