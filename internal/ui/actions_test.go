package ui

import (
	"errors"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
)

const actionsUIDef = `
actions:
  - {key: R, desc: Fetch, cmd: git fetch}
  - {key: space, desc: Global space, cmd: echo global}
  - {key: S, desc: Shell, cmd: sh, mode: interactive}
panels:
  - id: branches
    title: Branches
    source: git branch
    actions:
      - {key: space, desc: Checkout, cmd: "git checkout {{.line}}", refresh: [branches]}
      - {key: n, desc: New branch, prompt: Name, cmd: "git checkout -b {{input}}"}
      - {key: D, desc: Delete, cmd: "git branch -D {{.line}}", confirm: true}
`

func actionRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{"git branch": "main\nfeature\n"}, errs: map[string]error{}}
}

func statusOf(m tea.Model) string {
	lines := strings.Split(screen(m), "\n")
	return lines[len(lines)-1]
}

func TestBackgroundActionRunsAndRefreshes(t *testing.T) {
	r := actionRunner()
	m := start(t, actionsUIDef, r)
	n := len(r.commands())
	m = key(t, m, "space")
	ran := r.commands()[n:]
	if !reflect.DeepEqual(ran, []string{"git checkout main", "git branch"}) {
		t.Errorf("ran %v, want checkout then refresh of branches", ran)
	}
	if s := statusOf(m); !strings.Contains(s, "✓ Checkout") {
		t.Errorf("status = %q", s)
	}
}

func TestFailedActionShowsErrorUntilEsc(t *testing.T) {
	r := actionRunner()
	r.errs["git fetch"] = errors.New("exit status 128: fatal: no remote\nmore detail")
	m := start(t, actionsUIDef, r)
	m = key(t, m, "R")
	if s := statusOf(m); !strings.Contains(s, "✗ Fetch: exit status 128: fatal: no remote") || strings.Contains(s, "more detail") {
		t.Errorf("status = %q", s)
	}
	m = key(t, m, "j") // other keys keep the error visible
	if s := statusOf(m); !strings.Contains(s, "✗ Fetch") {
		t.Errorf("error vanished: %q", s)
	}
	m = key(t, m, "esc")
	if s := statusOf(m); strings.Contains(s, "✗") {
		t.Errorf("esc should dismiss the error: %q", s)
	}
}

func TestConfirm(t *testing.T) {
	r := actionRunner()
	m := start(t, actionsUIDef, r)
	m = key(t, m, "D")
	if s := statusOf(m); !strings.Contains(s, "Delete: git branch -D main? [y/N]") {
		t.Errorf("confirm prompt = %q", s)
	}
	m = key(t, m, "n")
	if slices.Contains(r.commands(), "git branch -D main") {
		t.Fatal("n must not run the action")
	}
	if s := statusOf(m); strings.Contains(s, "[y/N]") {
		t.Errorf("confirm still shown: %q", s)
	}
	m = key(t, m, "D")
	m = key(t, m, "y")
	if !slices.Contains(r.commands(), "git branch -D main") {
		t.Errorf("y should run the action, ran %v", r.commands())
	}
}

func TestPrompt(t *testing.T) {
	r := actionRunner()
	m := start(t, actionsUIDef, r)
	m = key(t, m, "n")
	if s := statusOf(m); !strings.Contains(s, "Name:") {
		t.Errorf("prompt not shown: %q", s)
	}
	m = key(t, m, "feat x")
	m = key(t, m, "enter")
	if !slices.Contains(r.commands(), "git checkout -b 'feat x'") {
		t.Errorf("ran %v", r.commands())
	}
	n := len(r.commands())
	m = key(t, m, "n")
	m = key(t, m, "q") // typed into the prompt, not quitting
	m = key(t, m, "esc")
	if len(r.commands()) != n {
		t.Errorf("cancelled prompt ran %v", r.commands()[n:])
	}
	if s := statusOf(m); strings.Contains(s, "Name:") {
		t.Errorf("prompt still shown after esc: %q", s)
	}
}

func TestInteractiveAction(t *testing.T) {
	d, err := def.Parse([]byte(actionsUIDef), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	model := New(d, actionRunner(), nil)
	model.debounce, model.toastTTL = 0, 0
	var got []string
	model.execProcess = func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		got = c.Args
		return func() tea.Msg { return cb(nil) }
	}
	var m tea.Model = model
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(t, m, m.Init())
	key(t, m, "S")
	if !reflect.DeepEqual(got, []string{"/bin/sh", "-c", "sh"}) {
		t.Errorf("exec args = %v", got)
	}
}

func TestHintBar(t *testing.T) {
	m := start(t, actionsUIDef, actionRunner())
	s := statusOf(m)
	if !strings.Contains(s, "space Checkout") || !strings.Contains(s, "R Fetch") || !strings.HasSuffix(strings.TrimSpace(s), "? more") {
		t.Errorf("hint bar = %q", s)
	}
	if strings.Contains(s, "Global space") {
		t.Errorf("overridden global shown in hints: %q", s)
	}
	m, _ = m.Update(tea.WindowSizeMsg{Width: 40, Height: 30})
	s = statusOf(m)
	if w := ansi.StringWidth(s); w > 40 || !strings.HasSuffix(strings.TrimSpace(s), "? more") {
		t.Errorf("narrow hint bar (%d wide) = %q", w, s)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := start(t, actionsUIDef, actionRunner())
	m = key(t, m, "?")
	s := screen(m)
	for _, want := range []string{"Branches", "Checkout", "Global", "Fetch", "Global space (overridden)", "Navigation", "refresh"} {
		if !strings.Contains(s, want) {
			t.Errorf("help missing %q:\n%s", want, s)
		}
	}
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 30 {
		t.Errorf("view has %d lines with help open", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
	m = key(t, m, "space") // keys go to the help, not to actions
	m = key(t, m, "esc")
	if strings.Contains(screen(m), "Navigation") {
		t.Error("esc should close help")
	}
}

func TestHelpScrollsWhenTaller(t *testing.T) {
	d, err := def.Parse([]byte(actionsUIDef), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var m tea.Model = New(d, actionRunner(), nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 14})
	m = drive(t, m, m.Init())
	m = key(t, m, "?")
	if s := screen(m); !strings.Contains(s, "j/k scroll") || strings.Contains(s, "quit") {
		t.Fatalf("short screen should show a scrollable help:\n%s", s)
	}
	for range 30 {
		m = key(t, m, "j")
	}
	if s := screen(m); !strings.Contains(s, "quit") {
		t.Errorf("scrolling help should reach the end:\n%s", s)
	}
}
