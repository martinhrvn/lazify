package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
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
	var errOut bytes.Buffer
	if code := run(nil, &bytes.Buffer{}, &errOut, t.TempDir()); code != 2 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestUnknownContextInSetIsError(t *testing.T) {
	cfg := t.TempDir()
	p := writeDef(t, cfg, "a.yaml", validDef)
	var errOut bytes.Buffer
	if code := run([]string{p, "--set", "region=x"}, &bytes.Buffer{}, &errOut, cfg); code != 2 {
		t.Errorf("code %d", code)
	}
	if !strings.Contains(errOut.String(), `unknown context "region"`) {
		t.Errorf("stderr = %q", errOut.String())
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
