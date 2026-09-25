package ui

import (
	"strings"
	"testing"
)

const tabsUIDef = `
panels:
  - {id: branches, title: Branches, source: git branch}
  - {id: remotes, title: Remotes, tab_of: branches, source: git branch -r}
  - {id: tags, title: Tags, tab_of: branches, source: git tag}
  - {id: files, title: Files, source: ls}
`

func tabsRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"git branch": "main\n", "git branch -r": "origin/main\n", "git tag": "v1\n", "ls": "a.txt\n",
	}}
}

func TestPanelTabsInOneSlot(t *testing.T) {
	m := start(t, tabsUIDef, tabsRunner())
	s := screen(m)
	if !strings.Contains(s, "[1] Branches │ Remotes │ Tags") || !strings.Contains(s, "main") || strings.Contains(s, "origin/main") {
		t.Fatalf("tab bar or rows wrong:\n%s", s)
	}
	if !strings.Contains(statusOf(m), "[/] tabs") {
		t.Errorf("hint missing: %q", statusOf(m))
	}
	m = key(t, m, "]")
	if s := screen(m); !strings.Contains(s, "origin/main") {
		t.Errorf("] should show Remotes:\n%s", s)
	}
	m = key(t, m, "]")
	m = key(t, m, "[")
	m = key(t, m, "[")
	if s := screen(m); !strings.Contains(s, "│main") {
		t.Errorf("[ [ should return to Branches:\n%s", s)
	}
	m = key(t, m, "tab") // files: no panel tabs
	if strings.Contains(statusOf(m), "[/] tabs") {
		t.Errorf("hint shown for an untabbed slot: %q", statusOf(m))
	}
}
