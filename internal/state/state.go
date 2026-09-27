// Package state keeps what lazify remembers between runs: per app, the row
// each remembering panel was on. One small YAML file per app id.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Dir is where state lives: $XDG_STATE_HOME/lazify, else ~/.local/state/lazify.
func Dir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "lazify")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "lazify")
}

func file(dir, id string) string { return filepath.Join(dir, id+".yaml") }

// Load reads app id's remembered rows (panel id → row key). A missing file
// is not an error: nothing is remembered yet.
func Load(dir, id string) (map[string]string, error) {
	m := map[string]string{}
	b, err := os.ReadFile(file(dir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err == nil {
		err = yaml.Unmarshal(b, &m)
	}
	if err != nil {
		return map[string]string{}, fmt.Errorf("%s: %w", file(dir, id), err)
	}
	return m, nil
}

// Save writes app id's remembered rows, atomically (temp file + rename).
func Save(dir, id string, m map[string]string) error {
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+id+"-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file(dir, id))
}
