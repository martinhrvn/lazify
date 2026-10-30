package def

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const onePanel = "panels: [{id: p, source: x}]\n"

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func appErrors(apps []App) string {
	var msgs []string
	for _, a := range apps {
		if a.Err != nil {
			msgs = append(msgs, a.Err.Error())
		}
	}
	return strings.Join(msgs, "\n")
}

func TestLoadFileWithoutImports(t *testing.T) {
	file := writeFile(t, filepath.Join(t.TempDir(), "git.yaml"), onePanel)
	apps, err := LoadFile(file)
	if err != nil || len(apps) != 1 || apps[0].ID != "git" || apps[0].File != file || apps[0].Def == nil {
		t.Errorf("apps = %+v, %v", apps, err)
	}
	if _, err := LoadFile(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Error("a missing file is an error")
	}
}

// Imports are relative to the importing file and nest; imported apps keep
// their own file and line, and appear where the import entry was.
func TestLoadFileImports(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shop")
	project := writeFile(t, filepath.Join(root, ".lazify.yaml"),
		"apps:\n  - import: lazify/ci.yaml\n  - id: db\n    panels: [{id: p, source: x}]\n  - import: lazify/more.yaml\n")
	ci := writeFile(t, filepath.Join(root, "lazify", "ci.yaml"), onePanel)
	more := writeFile(t, filepath.Join(root, "lazify", "more.yaml"),
		"apps:\n  - import: sub/logs.yaml\n  - id: web\n    panels: [{id: p, source: x}]\n")
	logs := writeFile(t, filepath.Join(root, "lazify", "sub", "logs.yaml"), onePanel)

	apps, err := LoadFile(project)
	if err != nil {
		t.Fatal(err)
	}
	if got := appIDs(apps); !reflect.DeepEqual(got, []string{"ci", "db", "logs", "web"}) {
		t.Fatalf("ids = %v (errors: %s)", got, appErrors(apps))
	}
	want := []struct {
		file string
		line int
	}{{ci, 1}, {project, 3}, {logs, 1}, {more, 3}}
	for i, w := range want {
		if apps[i].File != w.file || apps[i].Line != w.line || apps[i].Def == nil || apps[i].Import != "" {
			t.Errorf("apps[%d] = %+v; want %s:%d", i, apps[i], w.file, w.line)
		}
	}
}

func TestLoadFileImportFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, "shared", "k8s.yaml"), onePanel)
	file := writeFile(t, filepath.Join(t.TempDir(), "a.yaml"), "apps:\n  - import: ~/shared/k8s.yaml\n")
	apps, err := LoadFile(file)
	if err != nil || len(apps) != 1 || apps[0].ID != "k8s" || apps[0].Def == nil {
		t.Errorf("apps = %+v, %v", apps, err)
	}
}

func TestLoadFileImportErrors(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.yaml"), "panels: [")
	writeFile(t, filepath.Join(dir, "b.yaml"), "apps:\n  - import: c.yaml\n")
	writeFile(t, filepath.Join(dir, "c.yaml"), "apps:\n  - import: b.yaml\n")
	tests := []struct{ name, src, want string }{
		{"missing", "apps:\n  - import: nope.yaml\n", "a.yaml:2: apps: import nope.yaml: no such file"},
		{"self", "apps:\n  - import: a.yaml\n", "a.yaml:2: apps: import a.yaml: import cycle: a.yaml → a.yaml"},
		{"cycle", "apps:\n  - import: b.yaml\n", "c.yaml:2: apps: import b.yaml: import cycle: a.yaml → b.yaml → c.yaml → b.yaml"},
		{"broken", "apps:\n  - import: broken.yaml\n", "broken.yaml:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := writeFile(t, filepath.Join(dir, "a.yaml"), tt.src+"  - id: ok\n    panels: [{id: p, source: x}]\n")
			apps, err := LoadFile(file)
			if err != nil {
				t.Fatalf("a bad import is not an error for the whole file: %v", err)
			}
			if got := appErrors(apps); !strings.Contains(got, tt.want) {
				t.Errorf("errors %q\ndo not contain %q", got, tt.want)
			}
			if last := apps[len(apps)-1]; last.ID != "ok" || last.Def == nil {
				t.Errorf("the other apps still load: %+v", apps)
			}
		})
	}
}
