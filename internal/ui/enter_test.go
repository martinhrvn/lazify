package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
)

const enterUIDef = `
panels:
  - {id: commits, title: Commits, source: git log, enter: files}
  - id: files
    title: Files
    source: "git show {{commits.line}}"
    enter: {panel: diff, popup: {width: 50, height: 50}}
  - id: diff
    title: Diff
    content: {default: {tabs: [{name: Diff, cmd: "diff {{files.line}}"}]}}
`

func enterRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"git log":     "a1\nb2\n",
		"git show a1": "x.go\ny.go\n",
		"diff y.go":   numbered("d", 40),
	}}
}

func TestDrillDownAndBack(t *testing.T) {
	m := start(t, enterUIDef, enterRunner())
	if s := statusOf(m); !strings.Contains(s, "enter Files") {
		t.Errorf("hint bar should offer enter: %q", s)
	}
	m = key(t, m, "enter")
	s := screen(m)
	if !strings.Contains(s, "[1] Commits › a1 › Files") || !strings.Contains(s, "x.go") {
		t.Fatalf("drill-down not shown:\n%s", s)
	}
	if !strings.Contains(statusOf(m), "esc back") {
		t.Errorf("hint bar should offer esc: %q", statusOf(m))
	}
	m = key(t, m, "esc")
	s = screen(m)
	if strings.Contains(s, "x.go") || !strings.Contains(s, "[1] Commits ") || !strings.Contains(s, "b2") {
		t.Errorf("esc should go back to commits:\n%s", s)
	}
}

func TestPopupOverlayAndEsc(t *testing.T) {
	m := start(t, enterUIDef, enterRunner())
	m = key(t, m, "enter")
	m = key(t, m, "j") // y.go
	m = key(t, m, "enter")
	s := screen(m)
	if !strings.Contains(s, "d 0 ") || !strings.Contains(s, "Diff") {
		t.Fatalf("popup not shown:\n%s", s)
	}
	// It is drawn over the UI, which stays visible around it.
	if first := strings.Split(s, "\n")[0]; !strings.Contains(first, "Files") {
		t.Errorf("underlying UI hidden: %q", first)
	}
	m = key(t, m, "j") // j scrolls the focused popup content
	if s := screen(m); strings.Contains(s, "d 0 ") || !strings.Contains(s, "d 1 ") {
		t.Errorf("j should scroll the popup:\n%s", s)
	}
	m = key(t, m, "esc")
	s = screen(m)
	if strings.Contains(s, "d 1 ") || !strings.Contains(s, "y.go") {
		t.Errorf("esc should close the popup:\n%s", s)
	}
	m = key(t, m, "esc")
	if strings.Contains(screen(m), "y.go") {
		t.Error("second esc should leave the drill-down")
	}
}

func TestFitsWindowWithFullPopup(t *testing.T) {
	src := strings.Replace(enterUIDef, "popup: {width: 50, height: 50}", "popup: full", 1)
	d, err := def.Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var m tea.Model = New(d, enterRunner(), nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = drive(t, m, m.Init())
	for _, k := range []string{"enter", "j", "enter"} {
		m = key(t, m, k)
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 20 {
		t.Errorf("view has %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	if !strings.Contains(ansi.Strip(lines[0]), "Diff") {
		t.Errorf("full popup should cover the screen: %q", ansi.Strip(lines[0]))
	}
}
