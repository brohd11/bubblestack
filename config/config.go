// Package config is bubblestack's own user config: framework-level settings shared by
// every tool built on the framework, kept in ~/.bubblestack/config.yml. The theme is the
// first such setting — pick it in any bubblestack app and every other one follows next
// launch, because they all read this one file (bubblestack.Run loads it when the consumer
// doesn't pass an explicit theme).
//
// It is a directory, not a bare file, to leave room for expansion (a keybinds file is the
// next planned addition). The file is read per call — there is no process-wide cache, so
// callers always see the current on-disk state (and tests that swap $HOME keep working). A
// missing file is not an error: it yields the zero value, so a fresh install simply starts
// on the framework default until the user picks a theme.
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the parsed ~/.bubblestack/config.yml. A missing file yields the zero value, so
// every field is optional; omitempty keeps a surgically written file free of blank knobs.
type Config struct {
	Theme string `yaml:"theme,omitempty"` // last-selected TUI theme; loaded at startup, saved on change
}

// Dir is ~/.bubblestack, the home for config.yml (and future config files).
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bubblestack"), nil
}

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
		if os.IsNotExist(err) {
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

// saveKey sets key=value on the top-level mapping of ~/.bubblestack/config.yml surgically,
// preserving every other key and any comments. A missing file is created (with its parent
// dir) as a fresh single-key document.
func saveKey(key, value string) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "config.yml")

	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		data = nil // start from an empty document; setMappingScalar seeds the mapping
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return err
	}
	setMappingScalar(&doc, key, value)
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// setMappingScalar sets key=value on the top-level mapping of a parsed YAML document,
// overwriting an existing key's value or appending the pair when absent. An empty document
// is initialized to a mapping first.
func setMappingScalar(doc *yaml.Node, key, value string) {
	if len(doc.Content) == 0 {
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode}}
	}
	m := doc.Content[0]
	if m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Kind = yaml.ScalarNode
			m.Content[i+1].Tag = "!!str"
			m.Content[i+1].Value = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}
