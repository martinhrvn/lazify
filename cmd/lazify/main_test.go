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

func TestResolve(t *testing.T) {
	cfg := t.TempDir()
	writeDef(t, cfg, "ecs.yaml", validDef)
	writeDef(t, cfg, "logs.yml", validDef)
	local := writeDef(t, t.TempDir(), "mine.yaml", validDef)

	tests := []struct{ in, want string }{
		{local, local},
		{"ecs", filepath.Join(cfg, "ecs.yaml")},
		{"logs", filepath.Join(cfg, "logs.yml")},
	}
	for _, tt := range tests {
		got, err := resolve(tt.in, cfg)
		if err != nil || got != tt.want {
			t.Errorf("resolve(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	if _, err := resolve("nope", cfg); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("want not-found error naming the definition, got %v", err)
	}
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

func TestList(t *testing.T) {
	cfg := t.TempDir()
	writeDef(t, cfg, "ecs.yaml", "name: lazyecs\n"+validDef)
	writeDef(t, cfg, "git.yml", validDef)
	writeDef(t, cfg, "notes.txt", "x")

	var out bytes.Buffer
	if code := run([]string{"list"}, &out, &bytes.Buffer{}, cfg); code != 0 {
		t.Fatalf("code %d", code)
	}
	if got := strings.Fields(out.String()); !reflect.DeepEqual(got, []string{"ecs", "git"}) {
		t.Errorf("list = %q", out.String())
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
