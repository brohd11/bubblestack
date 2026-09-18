// Package config is bubblestack's own user config: framework-level settings shared by
// every tool built on the framework, kept in ~/.bubblestack/config.yml. Theme and list
// density choices follow the user across tools on subsequent launches. Density applies
// to apps opting into ListDensityProvider; an explicit startup theme overrides the store.
//
// It is a directory, not a bare file, to leave room for expansion (a keybinds file is the
// next planned addition). The file is read per call — there is no process-wide cache, so
// callers always see the current on-disk state (and tests that swap $HOME keep working). A
// missing file is not an error: it yields the zero value, so a fresh install simply starts
// on the framework default until the user picks a theme.
package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/brohd11/goutil/configdir"
	"gopkg.in/yaml.v3"
)

// Config is the parsed ~/.bubblestack/config.yml. A missing file yields the zero value, so
// every field is optional; omitempty keeps a surgically written file free of blank knobs.
type Config struct {
	Theme       string `yaml:"theme,omitempty"`        // last-selected TUI theme
	ListDensity string `yaml:"list_density,omitempty"` // compact or expanded; absent leaves the app default
}

// Dir is ~/.bubblestack, the home for config.yml (and future config files). The
// ~/.<app> convention itself lives in goutil/configdir, so the framework follows the
// same rule it hands the apps rather than restating it.
func Dir() (string, error) { return configdir.Dir("bubblestack") }

// Path is ~/.bubblestack/config.yml.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yml"), nil
}

// Load reads ~/.bubblestack/config.yml. A missing file is not an error — it returns the
// zero Config. A malformed file returns the parse error.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Theme returns the persisted theme name, or "" when none is saved (which leaves the
// framework default). Any read/parse error degrades to "" rather than failing startup —
// the theme is a preference, never a reason not to launch.
func Theme() string {
	cfg, err := Load()
	if err != nil {
		return ""
	}
	return cfg.Theme
}

// SaveTheme persists name as the theme in ~/.bubblestack/config.yml, creating the dir
// lazily on first save. It is a surgical edit — only the theme key is set (or appended),
// so the user's other keys and comments survive untouched, which matters once the file
// holds more than one setting.
func SaveTheme(name string) error { return saveKey("theme", name) }

// ListDensity returns the saved standard-list density. Missing, invalid or
// unreadable settings return ok=false so the caller keeps its existing default.
func ListDensity() (compact bool, ok bool) {
	cfg, err := Load()
	if err != nil {
		return false, false
	}
	switch cfg.ListDensity {
	case "compact":
		return true, true
	case "expanded":
		return false, true
	default:
		return false, false
	}
}

// SaveListDensity changes only list_density, preserving theme and other settings.
func SaveListDensity(compact bool) error {
	value := "expanded"
	if compact {
		value = "compact"
	}
	return saveKey("list_density", value)
}

// saveKey sets key=value on the top-level mapping of ~/.bubblestack/config.yml
// surgically, preserving every other key and any comments. A missing file is created
// (with its parent dir) as a fresh single-key document.
//
// The node-tree surgery itself is goutil/configdir.SaveKey -- gdaddon had grown a
// verbatim copy of it, differing only in seeding a missing file from its defaults.
func saveKey(key, value string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return configdir.SaveKey(path, key, value, nil)
}
