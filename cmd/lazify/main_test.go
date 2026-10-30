package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/martinhrvn/lazify/internal/def"
)

const validDef = "panels: [{id: a, source: echo hi}]\n"

func writeDef(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseArgs(t *testing.T) {
	a, err := parseArgs([]string{"ecs", "--set", "region=us-east-1", "--set=profile=prod"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a.positional, []string{"ecs"}) {
		t.Errorf("positional = %v", a.positional)
	}
	if !reflect.DeepEqual(a.set, map[string]string{"region": "us-east-1", "profile": "prod"}) {
		t.Errorf("set = %v", a.set)
	}
	for _, bad := range [][]string{{"--set"}, {"--set", "novalue"}, {"--bogus"}} {
		if _, err := parseArgs(bad); err == nil {
			t.Errorf("parseArgs(%v) should fail", bad)
		}
	}
}

func TestLint(t *testing.T) {
	cfg := t.TempDir()
	good := writeDef(t, cfg, "good.yaml", validDef)
	writeDef(t, cfg, "bad.yaml", "panels:\n  - id: a\n")

	var out, errOut bytes.Buffer
	if code := run([]string{"lint", good}, &out, &errOut, cfg); code != 0 {
		t.Errorf("lint good: code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "ok") {
		t.Errorf("lint good output = %q", out.String())
	}

	out.Reset()
	errOut.Reset()
	if code := run([]string{"lint", good, "bad"}, &out, &errOut, cfg); code != 1 {
		t.Errorf("lint bad: code %d", code)
	}
	if !strings.Contains(errOut.String(), "bad.yaml:2: panel a: source is required") {
		t.Errorf("lint bad stderr = %q", errOut.String())
	}
}

func TestListMissingDirIsEmpty(t *testing.T) {
	var out bytes.Buffer
	if code := run([]string{"list"}, &out, &bytes.Buffer{}, filepath.Join(t.TempDir(), "none")); code != 0 {
		t.Errorf("code %d", code)
	}
}

func TestUsage(t *testing.T) {
	t.Chdir(t.TempDir()) // no .lazify.yaml up the tree
	var errOut bytes.Buffer
	if code := run(nil, &bytes.Buffer{}, &errOut, t.TempDir()); code != 2 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestSetChecksPanels(t *testing.T) {
	cfg := t.TempDir()
	p := writeDef(t, cfg, "a.yaml", "panels:\n  - {id: region, select: true, values: [eu, us]}\n  - {id: a, source: x}\n  - {id: m, content: {default: {tabs: [{name: n, cmd: c}]}}}\n")
	tests := []struct{ set, want string }{
		{"nope=x", `--set nope: no list panel "nope"`},
		{"m=x", `--set m: no list panel "m"`},
		{"region=mars", `--set region: "mars" is not one of eu, us`},
	}
	for _, tt := range tests {
		var errOut bytes.Buffer
		if code := run([]string{p, "--set", tt.set}, &bytes.Buffer{}, &errOut, cfg); code != 2 {
			t.Errorf("%s: code %d", tt.set, code)
		}
		if !strings.Contains(errOut.String(), tt.want) {
			t.Errorf("%s: stderr = %q", tt.set, errOut.String())
		}
	}
}

const multi = `apps:
  - id: procs
    panels: [{id: p, source: ps}]
  - id: logs
    name: Log tail
    panels: [{id: f, source: ls}]
`

func TestPickApp(t *testing.T) {
	cfg := t.TempDir()
	writeDef(t, cfg, "config.yaml", multi)
	writeDef(t, cfg, "ecs.yaml", validDef)
	file := writeDef(t, t.TempDir(), "apps.yaml", multi)
	single := writeDef(t, t.TempDir(), "one.yaml", validDef)

	tests := []struct {
		args   []string
		wantID string
		errHas string
	}{
		{args: []string{"logs"}, wantID: "logs"},
		{args: []string{"ecs"}, wantID: "ecs"},
		{args: []string{single}, wantID: "one"},
		{args: []string{file, "procs"}, wantID: "procs"},
		{args: []string{file}, errHas: "defines 2 apps (procs, logs)"},
		{args: []string{file, "nope"}, errHas: `no app "nope" in ` + file},
		{args: []string{"nope"}, errHas: `no app "nope"`},
	}
	for _, tt := range tests {
		d, err := pick(tt.args, cfg)
		switch {
		case tt.errHas != "":
			if err == nil || !strings.Contains(err.Error(), tt.errHas) {
				t.Errorf("pick(%v) err = %v, want %q", tt.args, err, tt.errHas)
			}
		case err != nil:
			t.Errorf("pick(%v): %v", tt.args, err)
		case d.ID != tt.wantID:
			t.Errorf("pick(%v) = %q, want %q", tt.args, d.ID, tt.wantID)
		}
	}
}

func TestListCatalog(t *testing.T) {
	cfg := t.TempDir()
	writeDef(t, cfg, "config.yaml", multi)
	writeDef(t, cfg, "broken.yaml", "panels: [{id: a}]")
	var out bytes.Buffer
	if code := run([]string{"list"}, &out, &bytes.Buffer{}, cfg); code != 0 {
		t.Fatalf("code %d", code)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[0], "ID") {
		t.Fatalf("list =\n%s", out.String())
	}
	if strings.Contains(out.String(), cfg) {
		t.Errorf("list should show files relative to the config dir:\n%s", out.String())
	}
	for i, want := range [][]string{{"broken", "invalid"}, {"logs", "Log tail", "config.yaml"}, {"procs", "config.yaml"}} {
		for _, w := range want {
			if !strings.Contains(lines[i+1], w) {
				t.Errorf("line %q missing %q", lines[i+1], w)
			}
		}
	}
}

func TestLintWholeCatalog(t *testing.T) {
	cfg := t.TempDir()
	writeDef(t, cfg, "config.yaml", multi)
	var out, errOut bytes.Buffer
	if code := run([]string{"lint"}, &out, &errOut, cfg); code != 0 {
		t.Errorf("code %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "logs: ok") || !strings.Contains(out.String(), "procs: ok") {
		t.Errorf("out = %q", out.String())
	}
	writeDef(t, cfg, "dup.yaml", "id: logs\npanels: [{id: a, source: x}]")
	writeDef(t, cfg, "broken.yaml", "panels: [")
	out.Reset()
	errOut.Reset()
	if code := run([]string{"lint"}, &out, &errOut, cfg); code != 1 {
		t.Errorf("code %d", code)
	}
	for _, want := range []string{`app "logs" is defined more than once`, "broken.yaml:1"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr %q missing %q", errOut.String(), want)
		}
	}
}

func TestUsageNamesConfigDir(t *testing.T) {
	var errOut bytes.Buffer
	run(nil, &bytes.Buffer{}, &errOut, "/home/x/.config/lazify")
	if !strings.Contains(errOut.String(), "/home/x/.config/lazify") || !strings.Contains(errOut.String(), "lazify <id>") {
		t.Errorf("usage = %q", errOut.String())
	}
}

func TestNoMouseFlag(t *testing.T) {
	a, err := parseArgs([]string{"ecs", "--no-mouse"})
	if err != nil || !a.noMouse || !reflect.DeepEqual(a.positional, []string{"ecs"}) {
		t.Errorf("args = %+v, %v", a, err)
	}
	if a, _ := parseArgs([]string{"ecs"}); a.noMouse {
		t.Error("mouse is on by default")
	}
}

func TestNoRememberFlag(t *testing.T) {
	a, err := parseArgs([]string{"ecs", "--no-remember"})
	if err != nil || !a.noRemember || !reflect.DeepEqual(a.positional, []string{"ecs"}) {
		t.Errorf("args = %+v, %v", a, err)
	}
}

func TestRememberLoadsAndMerges(t *testing.T) {
	dir := t.TempDir()
	d, err := def.Parse([]byte("panels:\n  - {id: region, select: true, values: [a, b]}\n  - {id: clusters, source: c, remember: true}"), "ecs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	recall := loadRemembered(d, dir, &errOut)
	if len(recall) != 0 || errOut.Len() != 0 {
		t.Fatalf("first run: %v %q", recall, errOut.String())
	}
	saveRemembered(d, dir, recall, map[string]string{"region": "b", "clusters": "web"}, &errOut)
	// Next run: clusters had no selection at quit (still loading) — keep web.
	recall = loadRemembered(d, dir, &errOut)
	saveRemembered(d, dir, recall, map[string]string{"region": "a"}, &errOut)
	if got := loadRemembered(d, dir, &errOut); !reflect.DeepEqual(got, map[string]string{"region": "a", "clusters": "web"}) {
		t.Errorf("remembered = %v", got)
	}
	os.WriteFile(filepath.Join(dir, d.ID+".yaml"), []byte("[broken"), 0o644)
	if got := loadRemembered(d, dir, &errOut); len(got) != 0 || !strings.Contains(errOut.String(), "lazify: can't read remembered choices") {
		t.Errorf("corrupt state: %v, %q", got, errOut.String())
	}
}

func TestPagerHelper(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"__pager", "printf 'a\\nb\\n'", "cat"}, &out, &errOut, t.TempDir()); code != 0 || out.String() != "a\nb\n" {
		t.Errorf("__pager: code %d, out %q, err %q", code, out.String(), errOut.String())
	}
	if code := run([]string{"__pager", "x"}, &out, &errOut, t.TempDir()); code != 2 {
		t.Errorf("__pager needs two arguments: %d", code)
	}
}

func TestNoFloatFlag(t *testing.T) {
	a, err := parseArgs([]string{"ecs", "--no-float"})
	if err != nil || !a.noFloat {
		t.Errorf("args = %+v, %v", a, err)
	}
}

func projectDir(t *testing.T) (root, sub string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "shop")
	sub = filepath.Join(root, "web", "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDef(t, root, ".lazify.yaml", "panels: [{id: svc, source: process-compose list}]\n")
	return root, sub
}

// Plain `lazify` in a project (or below it) runs its .lazify.yaml, from the
// project's directory.
func TestResolveProject(t *testing.T) {
	root, sub := projectDir(t)
	d, dir, err := resolve(nil, t.TempDir(), sub)
	if err != nil || d.ID != "shop" || dir != root {
		t.Errorf("resolve = %v, %q, %v", d, dir, err)
	}
	if _, _, err := resolve(nil, t.TempDir(), t.TempDir()); err != errNoApp {
		t.Errorf("no project: %v", err)
	}
	// An explicit id or file still means the catalog / that file, run here.
	cfg := t.TempDir()
	writeDef(t, cfg, "git.yaml", validDef)
	if d, dir, err := resolve([]string{"git"}, cfg, sub); err != nil || d.ID != "git" || dir != "" {
		t.Errorf("by id: %v %q %v", d, dir, err)
	}
}

func TestListAndLintIncludeProject(t *testing.T) {
	_, sub := projectDir(t)
	t.Chdir(sub)
	var out, errOut bytes.Buffer
	if code := run([]string{"list"}, &out, &errOut, t.TempDir()); code != 0 || !strings.Contains(out.String(), "shop") || !strings.Contains(out.String(), "project") {
		t.Errorf("list: %d\n%s%s", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run([]string{"lint"}, &out, &errOut, t.TempDir()); code != 0 || !strings.Contains(out.String(), ".lazify.yaml") {
		t.Errorf("lint: %d\n%s%s", code, out.String(), errOut.String())
	}
}

// multiProject is a project whose .lazify.yaml holds several apps, one of them
// imported from lazify/web.yaml.
func multiProject(t *testing.T) (root, sub string) {
	t.Helper()
	root, sub = projectDir(t)
	if err := os.Mkdir(filepath.Join(root, "lazify"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeDef(t, root, ".lazify.yaml", "apps:\n  - id: db\n    panels: [{id: a, source: x}]\n  - import: lazify/web.yaml\n")
	writeDef(t, filepath.Join(root, "lazify"), "web.yaml", validDef)
	return root, sub
}

// `lazify <id>` looks in the project first (its apps win over the config
// dir's) and runs a project app from the project.
func TestResolveProjectAppByID(t *testing.T) {
	root, sub := multiProject(t)
	cfg := t.TempDir()
	writeDef(t, cfg, "web.yaml", validDef)
	writeDef(t, cfg, "git.yaml", validDef)

	d, dir, err := resolve([]string{"web"}, cfg, sub)
	if err != nil || d.ID != "web" || dir != root || d.File != filepath.Join(root, "lazify", "web.yaml") {
		t.Errorf("web = %v, %q, %v", d, dir, err)
	}
	if d, dir, err := resolve([]string{"db"}, cfg, sub); err != nil || d.ID != "db" || dir != root {
		t.Errorf("db = %v, %q, %v", d, dir, err)
	}
	if d, dir, err := resolve([]string{"git"}, cfg, sub); err != nil || d.ID != "git" || dir != "" {
		t.Errorf("git (config dir) = %v, %q, %v", d, dir, err)
	}
	_, _, err = resolve(nil, cfg, sub)
	if err == nil || !strings.Contains(err.Error(), "defines apps: db, web; run `lazify <id>`") {
		t.Errorf("plain lazify with several apps: %v", err)
	}
}

func TestPickImportedApp(t *testing.T) {
	root, _ := multiProject(t)
	d, err := pick([]string{filepath.Join(root, ".lazify.yaml"), "web"}, t.TempDir())
	if err != nil || d.ID != "web" {
		t.Errorf("pick = %v, %v", d, err)
	}
}

func TestListAndLintProjectImports(t *testing.T) {
	_, sub := multiProject(t)
	t.Chdir(sub)
	cfg := t.TempDir()
	writeDef(t, cfg, "web.yaml", validDef)
	var out, errOut bytes.Buffer
	if code := run([]string{"list"}, &out, &errOut, cfg); code != 0 {
		t.Fatalf("list: %d %s", code, errOut.String())
	}
	if want := filepath.Join("..", "..", "lazify", "web.yaml") + ":1"; !strings.Contains(out.String(), want) {
		t.Errorf("list shows where web is defined (%s):\n%s", want, out.String())
	}
	if !strings.Contains(out.String(), "hidden by project") {
		t.Errorf("the config dir's web is hidden by the project's:\n%s", out.String())
	}
	out.Reset()
	if code := run([]string{"lint"}, &out, &errOut, t.TempDir()); code != 0 || !strings.Contains(out.String(), "web: ok") {
		t.Errorf("lint: %d\n%s%s", code, out.String(), errOut.String())
	}
}
