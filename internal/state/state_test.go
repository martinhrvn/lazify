package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new", "lazify") // created on save
	m := map[string]string{"region": "eu-west-1", "clusters": "arn:aws:ecs:eu-west-1:1:cluster/web"}
	if err := Save(dir, "ecs", m); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir, "ecs")
	if err != nil || !reflect.DeepEqual(got, m) {
		t.Errorf("Load = %v, %v; want %v", got, err, m)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "ecs.yaml" {
		t.Errorf("dir has %v; want just ecs.yaml (no temp files left)", entries)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	got, err := Load(t.TempDir(), "nope")
	if err != nil || len(got) != 0 {
		t.Errorf("Load = %v, %v", got, err)
	}
}

func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.yaml"), []byte("region: [unclosed"), 0o644)
	if _, err := Load(dir, "x"); err == nil || !strings.Contains(err.Error(), "x.yaml") {
		t.Errorf("err = %v, want one naming the file", err)
	}
}

func TestDir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/tmp/xdg")
	if got := Dir(); got != "/tmp/xdg/lazify" {
		t.Errorf("Dir = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "/home/u")
	if got := Dir(); got != "/home/u/.local/state/lazify" {
		t.Errorf("Dir = %q", got)
	}
}
