package catalog

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const twoApps = `apps:
  - id: procs
    panels: [{id: p, source: ps}]
  - id: logs
    name: Logs
    panels: [{id: f, source: ls}]
`

func TestFindAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", twoApps)
	write(t, dir, "ecs.yml", "name: AWS ECS\npanels: [{id: c, source: aws}]")
	write(t, dir, "notes.txt", "ignored")
	c := Scan(dir)

	for id, file := range map[string]string{"procs": "config.yaml", "logs": "config.yaml", "ecs": "ecs.yml"} {
		d, err := c.Find(id)
		if err != nil {
			t.Errorf("Find(%s): %v", id, err)
			continue
		}
		if d.ID != id || filepath.Base(d.File) != file {
			t.Errorf("Find(%s) = %s from %s", id, d.ID, d.File)
		}
	}
	var got []string
	for _, e := range c.Entries() {
		got = append(got, e.ID+"|"+e.Name+"|"+filepath.Base(e.File))
	}
	want := []string{"ecs|AWS ECS|ecs.yml", "logs|Logs|config.yaml", "procs|procs|config.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("entries = %v", got)
	}
}

func TestNotFoundListsKnownIDs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", twoApps)
	_, err := Scan(dir).Find("nope")
	if err == nil || !strings.Contains(err.Error(), `no app "nope"`) || !strings.Contains(err.Error(), "logs, procs") {
		t.Errorf("err = %v", err)
	}
}

func TestBrokenNeighboursDoNotBlock(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", twoApps)
	write(t, dir, "broken.yaml", "panels: [")
	write(t, dir, "invalid.yaml", "panels: [{id: a}]")
	c := Scan(dir)
	if _, err := c.Find("procs"); err != nil {
		t.Errorf("procs blocked by a broken neighbour: %v", err)
	}
	if _, err := c.Find("invalid"); err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Errorf("invalid app should report its own error, got %v", err)
	}
	_, err := c.Find("nope")
	if err == nil || !strings.Contains(err.Error(), "broken.yaml") {
		t.Errorf("not-found should mention unreadable files: %v", err)
	}
	probs := strings.Join(errStrings(c.Problems()), "\n")
	for _, want := range []string{"broken.yaml:1", "invalid.yaml:1: panel a: source is required"} {
		if !strings.Contains(probs, want) {
			t.Errorf("problems %q missing %q", probs, want)
		}
	}
}

func TestDuplicateIDs(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "config.yaml", twoApps)
	write(t, dir, "procs.yaml", "panels: [{id: p, source: ps}]")
	c := Scan(dir)
	_, err := c.Find("procs")
	if err == nil || !strings.Contains(err.Error(), "config.yaml:2") || !strings.Contains(err.Error(), "procs.yaml:1") {
		t.Errorf("err = %v", err)
	}
	if _, err := c.Find("logs"); err != nil {
		t.Errorf("other ids unaffected: %v", err)
	}
	if !strings.Contains(strings.Join(errStrings(c.Problems()), "\n"), `app "procs" is defined more than once`) {
		t.Errorf("problems = %v", c.Problems())
	}
}

func TestMissingDirIsEmpty(t *testing.T) {
	c := Scan(filepath.Join(t.TempDir(), "none"))
	if len(c.Entries()) != 0 || len(c.Problems()) != 0 {
		t.Errorf("entries %v problems %v", c.Entries(), c.Problems())
	}
}

func errStrings(errs []error) []string {
	var out []string
	for _, e := range errs {
		out = append(out, e.Error())
	}
	return out
}
