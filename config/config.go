// Package config is bubblestack's shared user config, ~/.bubblestack/config.yml: theme
// and list density that follow the user across apps. It is read per call (no cache); a
// missing file yields the defaults.
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

// Dir is ~/.bubblestack.
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

// Theme returns the saved theme name, or "" when unset or unreadable: a preference must
// never stop startup.
func Theme() string {
	cfg, err := Load()
	if err != nil {
		return ""
	}
	return cfg.Theme
}

// SaveTheme saves the theme, editing only that key so other keys and comments survive.
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

// saveKey sets one top-level key in the config file, preserving the rest
// (configdir.SaveKey).
func saveKey(key, value string) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return configdir.SaveKey(path, key, value, nil)
}
