package ui

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
)

// Tests don't float by accident when run inside tmux or zellij.
func TestMain(m *testing.M) {
	os.Unsetenv("TMUX")
	os.Unsetenv("ZELLIJ")
	os.Exit(m.Run())
}

const floatDef = `
panels:
  - id: svc
    source: list
    actions:
      - {key: f, desc: Follow, cmd: "tail -f log", pager: "less +F"}
      - {key: s, desc: Shell, cmd: sh, mode: interactive, float: false}
`

func floatModel(t *testing.T) (*Model, *fakeRunner, *[]string) {
	d, err := def.Parse([]byte(floatDef), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{"list": "api\n"}}
	model := New(d, r, nil)
	model.debounce, model.toastTTL, model.self = 0, 0, "/lazify"
	var execd []string
	model.execProcess = func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
		execd = c.Args
		return func() tea.Msg { return cb(nil) }
	}
	return &model, r, &execd
}

func run(t *testing.T, model *Model, keys ...string) tea.Model {
	var m tea.Model = *model
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(t, m, m.Init())
	for _, k := range keys {
		m = key(t, m, k)
	}
	return m
}

func TestPagerActionTakesOverTheTerminal(t *testing.T) {
	model, _, execd := floatModel(t)
	run(t, model, "f")
	want := []string{"/bin/sh", "-c", runner.PagerCommand("/lazify", "tail -f log", "less +F")}
	if strings.Join(*execd, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("exec = %q\nwant   %q", *execd, want)
	}
}

func TestInteractiveActionsFloat(t *testing.T) {
	t.Setenv("ZELLIJ", "0")
	model, r, execd := floatModel(t)
	m := run(t, model, "f")
	var launched string
	for _, c := range r.commands() {
		if strings.HasPrefix(c, "zellij run --floating") {
			launched = c
		}
	}
	if launched == "" || !strings.Contains(launched, "__pager") || *execd != nil {
		t.Fatalf("want a zellij floating pane running the pager helper; ran %q, exec %q", r.commands(), *execd)
	}
	if s := statusOf(m); !strings.Contains(s, "Follow") {
		t.Errorf("status = %q", s)
	}
	// float: false on the action, or --no-float, keeps the terminal.
	run(t, model, "s")
	if strings.Join(*execd, " ") != "/bin/sh -c sh" {
		t.Errorf("float: false should take over the terminal: %q", *execd)
	}
	model, r, execd = floatModel(t)
	model.noFloat = true
	run(t, model, "f")
	if *execd == nil || strings.Contains(strings.Join(r.commands(), "\n"), "zellij") {
		t.Errorf("--no-float: exec %q, ran %q", *execd, r.commands())
	}
}

// Closing a floating pane (a hangup) ends it normally: no error toast.
func TestFloatClosedIsNotAnError(t *testing.T) {
	t.Setenv("ZELLIJ", "0")
	model, r, _ := floatModel(t)
	var m tea.Model = *model
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = drive(t, m, m.Init())
	// Every launcher run fails as a closed pane does.
	r.failAll = &runner.ExitError{Code: 129}
	m = key(t, m, "f")
	if s := statusOf(m); strings.Contains(s, "✗") {
		t.Errorf("a closed pane is not an error: %q", s)
	}
	r.failAll = &runner.ExitError{Code: 127, Stderr: "zellij: not found"}
	m = key(t, m, "f")
	if s := statusOf(m); !strings.Contains(s, "✗ Follow") {
		t.Errorf("a failed launch is: %q", s)
	}
}
