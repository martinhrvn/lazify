package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
)

// fakeRunner returns canned output per command.
type fakeRunner struct {
	out  map[string]string
	errs map[string]error
	ran  []string
}

func (f *fakeRunner) Run(_ context.Context, req runner.Request) (runner.Result, error) {
	f.ran = append(f.ran, req.Cmd)
	return runner.Result{Stdout: []byte(f.out[req.Cmd])}, f.errs[req.Cmd]
}

// drive executes cmd and every command it leads to, feeding messages back
// into the model, until nothing is left.
func drive(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tea.QuitMsg:
			return m
		default:
			var next tea.Cmd
			m, next = m.Update(msg)
			queue = append(queue, next)
		}
	}
	return m
}

func key(t *testing.T, m tea.Model, k string) tea.Model {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m, cmd := m.Update(msg)
	return drive(t, m, cmd)
}

func start(t *testing.T, src string, r runner.Runner) tea.Model {
	t.Helper()
	d, err := def.Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var m tea.Model = New(d, r, nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return drive(t, m, m.Init())
}

func screen(m tea.Model) string { return ansi.Strip(m.View()) }

const twoPanels = `
panels:
  - {id: branches, title: Branches, source: git branch}
  - {id: tags, title: Tags, source: git tag}
`

func TestRendersPanelsWithRows(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git branch": "main\nfeature\n", "git tag": "v1\n"}}
	s := screen(start(t, twoPanels, r))
	for _, want := range []string{"Branches", "Tags", "main", "feature", "v1"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen missing %q:\n%s", want, s)
		}
	}
}

func TestFitsWindow(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git branch": strings.Repeat("b\n", 100)}}
	s := start(t, twoPanels, r).View()
	lines := strings.Split(s, "\n")
	if len(lines) != 30 {
		t.Errorf("view has %d lines, want 30", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("line %d is %d wide", i, w)
		}
	}
}

func TestCursorMovesAndScrolls(t *testing.T) {
	var out strings.Builder
	for i := range 60 {
		out.WriteString("branch-")
		out.WriteString(string(rune('A' + i%26)))
		out.WriteString(strings.Repeat("x", i/26))
		out.WriteString("\n")
	}
	r := &fakeRunner{out: map[string]string{"git branch": out.String()}}
	m := start(t, twoPanels, r)
	for range 59 {
		m = key(t, m, "j")
	}
	if s := screen(m); !strings.Contains(s, "branch-Hxx") {
		t.Errorf("last row not scrolled into view:\n%s", s)
	}
}

func TestShowsErrors(t *testing.T) {
	r := &fakeRunner{errs: map[string]error{"git branch": errors.New("exit status 128: not a git repository")}}
	if s := screen(start(t, twoPanels, r)); !strings.Contains(s, "not a git repository") {
		t.Errorf("error not shown:\n%s", s)
	}
}

func TestBlockedPanel(t *testing.T) {
	src := "panels:\n  - {id: a, source: x}\n  - {id: b, source: 'y {{a.line}}'}"
	s := screen(start(t, src, &fakeRunner{}))
	if !strings.Contains(s, "no selection in a") {
		t.Errorf("blocked message missing:\n%s", s)
	}
}

func TestRefreshReruns(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git branch": "main\n"}}
	m := start(t, twoPanels, r)
	r.out["git branch"] = "main\nnew-branch\n"
	m = key(t, m, "r")
	if s := screen(m); !strings.Contains(s, "new-branch") {
		t.Errorf("refresh not applied:\n%s", s)
	}
}

func TestTabFocusesNextPanel(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git branch": "main\n", "git tag": "v1\nv2\n"}}
	m := start(t, twoPanels, r)
	m = key(t, m, "tab")
	m = key(t, m, "r")
	if got := r.ran[len(r.ran)-1]; got != "git tag" {
		t.Errorf("refresh ran %q, want git tag (focused)", got)
	}
	m = key(t, m, "1")
	m = key(t, m, "r")
	if got := r.ran[len(r.ran)-1]; got != "git branch" {
		t.Errorf("refresh ran %q, want git branch", got)
	}
}

func TestColumnsAligned(t *testing.T) {
	src := `
panels:
  - id: svc
    source: x
    rows: .[]
    columns:
      - {title: Name, value: "{{.n}}"}
      - {title: Count, value: "{{.c}}"}
`
	r := &fakeRunner{out: map[string]string{"x": `[{"n":"web","c":1},{"n":"backend","c":22}]`}}
	s := screen(start(t, src, r))
	var header, web string
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, "Name") {
			header = l
		}
		if strings.Contains(l, "│web ") {
			web = l
		}
	}
	if header == "" || web == "" || strings.Index(header, "Count") != strings.Index(web, "1") {
		t.Errorf("columns not aligned:\n%s\n%s", header, web)
	}
}

func TestQuit(t *testing.T) {
	m := start(t, twoPanels, &fakeRunner{})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("q did not quit")
	}
}
