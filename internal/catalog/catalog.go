// Package catalog finds apps by id across the definition files in a config
// directory (~/.config/lazify): every *.yaml / *.yml file there holds one app
// or several under `apps:`.
package catalog

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/martinhrvn/lazify/internal/def"
)

// Entry is one app found in the directory.
type Entry struct {
	ID, Name string
	File     string
	Line     int
	Def      *def.Definition // nil when Err is set
	Err      error
}

// Location is where the entry is defined, as file:line.
func (e Entry) Location() string { return fmt.Sprintf("%s:%d", e.File, e.Line) }

// Catalog is the result of scanning a directory.
type Catalog struct {
	Dir      string
	entries  []Entry
	fileErrs []error // files that could not be read or parsed as a whole
}

// Scan reads every definition file in dir. A missing directory is empty.
func Scan(dir string) *Catalog {
	c := &Catalog{Dir: dir}
	files, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.fileErrs = append(c.fileErrs, err)
		}
		return c
	}
	for _, f := range files {
		ext := filepath.Ext(f.Name())
		if f.IsDir() || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		path := filepath.Join(dir, f.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			c.fileErrs = append(c.fileErrs, err)
			continue
		}
		apps, err := def.ParseFile(data, path)
		if err != nil {
			c.fileErrs = append(c.fileErrs, err)
		}
		for _, a := range apps {
			c.entries = append(c.entries, Entry{ID: a.ID, Name: a.Name, File: path, Line: a.Line, Def: a.Def, Err: a.Err})
		}
	}
	sort.SliceStable(c.entries, func(i, j int) bool { return c.entries[i].ID < c.entries[j].ID })
	return c
}

// Entries lists every app, sorted by id.
func (c *Catalog) Entries() []Entry { return c.entries }

func (c *Catalog) matches(id string) []Entry {
	var out []Entry
	for _, e := range c.entries {
		if e.ID == id {
			out = append(out, e)
		}
	}
	return out
}

// Find returns the app with the given id. Problems elsewhere in the directory
// don't matter unless the id cannot be found at all.
func (c *Catalog) Find(id string) (*def.Definition, error) {
	switch m := c.matches(id); len(m) {
	case 0:
		msg := fmt.Sprintf("no app %q in %s", id, c.Dir)
		if ids := c.ids(); len(ids) > 0 {
			msg += " (known: " + strings.Join(ids, ", ") + ")"
		}
		for _, e := range c.fileErrs {
			first, _, _ := strings.Cut(e.Error(), "\n")
			msg += "\n  could not read: " + first
		}
		return nil, errors.New(msg)
	case 1:
		return m[0].Def, m[0].Err
	default:
		return nil, duplicate(id, m)
	}
}

func duplicate(id string, m []Entry) error {
	locs := make([]string, len(m))
	for i, e := range m {
		locs[i] = e.Location()
	}
	return fmt.Errorf("app %q is defined more than once: %s", id, strings.Join(locs, ", "))
}

// Duplicate reports whether more than one app uses id.
func (c *Catalog) Duplicate(id string) bool { return len(c.matches(id)) > 1 }

func (c *Catalog) ids() []string {
	var ids []string
	for _, e := range c.entries {
		if len(ids) == 0 || ids[len(ids)-1] != e.ID {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

// Problems lists everything wrong in the directory: unreadable files, invalid
// apps and duplicate ids.
func (c *Catalog) Problems() []error {
	probs := append([]error(nil), c.fileErrs...)
	for _, e := range c.entries {
		if e.Err != nil {
			probs = append(probs, e.Err)
		}
	}
	for _, id := range c.ids() {
		if m := c.matches(id); len(m) > 1 {
			probs = append(probs, duplicate(id, m))
		}
	}
	return probs
}
