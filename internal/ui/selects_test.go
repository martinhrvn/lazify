package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

const selectsUIDef = `
panels:
  - {id: dir, title: Dir, select: true, values: [/etc, /usr/share, /tmp]}
  - {id: files, title: Files, source: "ls {{dir.line}}"}
`

func selectsRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{"ls /etc": "passwd\n", "ls /usr/share": "doc\n", "ls /tmp": "x\n"}}
}

func TestSelectPanelAndPicker(t *testing.T) {
	m := start(t, selectsUIDef, selectsRunner())
	s := screen(m)
	if !strings.Contains(s, "[1] Dir") || !strings.Contains(s, "│/etc") || !strings.Contains(s, "passwd") {
		t.Fatalf("select or dependent not shown:\n%s", s)
	}
	if h := boxHeight(t, s, "Dir"); h != 3 {
		t.Errorf("a select is one line tall by default, box height %d", h)
	}
	if strings.Contains(s, "/usr/share") {
		t.Error("the select should show only its choice")
	}
	m = key(t, m, "1") // its number opens the picker
	s = screen(m)
	if !strings.Contains(s, "/usr/share") || !strings.Contains(s, "/tmp") {
		t.Fatalf("picker not shown:\n%s", s)
	}
	if !strings.Contains(statusOf(m), "enter choose") || !strings.Contains(statusOf(m), "esc cancel") {
		t.Errorf("picker hints: %q", statusOf(m))
	}
	m = key(t, m, "j")
	m = key(t, m, "enter")
	s = screen(m)
	if !strings.Contains(s, "doc") || strings.Contains(s, "passwd") || !strings.Contains(s, "│/usr/share") {
		t.Errorf("choosing /usr/share should re-run files:\n%s", s)
	}
	if strings.Contains(s, "/tmp") {
		t.Error("picker should be closed")
	}
}

func TestPickerEsc(t *testing.T) {
	r := selectsRunner()
	m := start(t, selectsUIDef, r)
	m = key(t, m, "1")
	m = key(t, m, "j")
	m = key(t, m, "esc")
	s := screen(m)
	if !strings.Contains(s, "passwd") || strings.Contains(s, "/usr/share") {
		t.Errorf("esc should change nothing:\n%s", s)
	}
}

func TestFitsWindowWithPicker(t *testing.T) {
	m := start(t, selectsUIDef, selectsRunner())
	m = key(t, m, "1")
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Errorf("view has %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}
