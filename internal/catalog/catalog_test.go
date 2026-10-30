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

func TestFindProject(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "shop", "web", "src")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := FindProject(deep); ok {
		t.Error("no project file anywhere: none found")
	}
	file := filepath.Join(root, "shop", ".lazify.yaml")
	os.WriteFile(file, []byte("panels: []"), 0o644)
	if got, ok := FindProject(deep); !ok || got != file {
		t.Errorf("FindProject = %q, %v; want %q (nearest parent)", got, ok, file)
	}
	yml := filepath.Join(root, "shop", "web", ".lazify.yml")
	os.WriteFile(yml, []byte("panels: []"), 0o644)
	if got, _ := FindProject(deep); got != yml {
		t.Errorf("the nearest one wins: %q", got)
	}
}

// Files in subdirectories aren't scanned, but can be imported; the imported
// app is listed where it is defined.
func TestImportedApps(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "shared"), 0o755)
	write(t, dir, "config.yaml", "apps:\n  - import: shared/k8s.yaml\n")
	write(t, filepath.Join(dir, "shared"), "k8s.yaml", "\npanels: [{id: p, source: kubectl}]")
	c := Scan(dir)
	if probs := c.Problems(); len(probs) > 0 {
		t.Fatalf("problems: %v", probs)
	}
	d, err := c.Find("k8s")
	if err != nil || d.ID != "k8s" {
		t.Fatalf("Find(k8s) = %v, %v", d, err)
	}
	if es := c.Entries(); len(es) != 1 || es[0].Location() != filepath.Join(dir, "shared", "k8s.yaml")+":2" {
		t.Errorf("entries = %+v", es)
	}
}

func TestScanFile(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", "apps:\n  - import: b.yaml\n  - import: b.yaml\n  - id: x\n    panels: [{id: p, source: x}]\n")
	write(t, dir, "b.yaml", "panels: [{id: p, source: x}]")
	c := ScanFile(filepath.Join(dir, "a.yaml"))
	if !c.Has("x") || c.Has("nope") {
		t.Errorf("Has: x=%v nope=%v", c.Has("x"), c.Has("nope"))
	}
	if d, err := c.Find("x"); err != nil || d.ID != "x" {
		t.Errorf("Find(x) = %v, %v", d, err)
	}
	if _, err := c.Find("b"); err == nil || !strings.Contains(err.Error(), "defined more than once") {
		t.Errorf("Find(b) = %v", err)
	}
}
