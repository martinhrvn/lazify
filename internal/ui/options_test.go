package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const optionsUIDef = `
panels:
  - id: commits
    source: log {{opt.since}} {{opt.author}} {{opt.merges}} -n {{opt.max}}
    options:
      - {id: since, title: Since, type: date, default: 2026-09-01, flag: --since=}
      - {id: author, title: Author, flag: --author=}
      - {id: merges, title: No merges, type: toggle, flag: --no-merges}
      - {id: max, title: Max, values: ["50", "200"], default: "200"}
  - id: files
    source: files
  - id: main
    content:
      commits:
        options: [{id: context, title: Context, values: ["3", "10"]}]
        tabs: [{name: Diff, cmd: "show -U{{opt.context}} {{.line}}"}]
`

func optionsRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"log --since=2026-09-01   -n 200":                        "a1\n",
		"log --since=2026-09-01 --author=jane --no-merges -n 50": "b2\n",
		"show -U3 a1":  "diff a1\n",
		"show -U10 b2": "diff b2\n",
		"files":        "x.go\n",
	}}
}

func typeText(t *testing.T, m tea.Model, s string) tea.Model {
	for _, r := range s {
		m = key(t, m, string(r))
	}
	return m
}

func TestOptionsForm(t *testing.T) {
	r := optionsRunner()
	m := start(t, optionsUIDef, r)
	if !strings.Contains(statusOf(m), "o options") {
		t.Errorf("hint bar should offer o: %q", statusOf(m))
	}
	m = key(t, m, "o")
	s := screen(m)
	for _, want := range []string{"Options", "commits", "Since", "2026-09-01", "Author", "No merges", "[ ]", "Max", "‹ 200 ›", "main · commits", "Context", "‹ 3 ›"} {
		if !strings.Contains(s, want) {
			t.Errorf("form lacks %q:\n%s", want, s)
		}
	}
	m = key(t, m, "tab")       // Author
	m = typeText(t, m, "jane") // text field: typed, not moving (j) or anything else
	m = key(t, m, "tab")       // No merges
	m = key(t, m, "space")     // on
	m = key(t, m, "tab")       // Max
	m = key(t, m, "left")      // 200 → 50
	m = key(t, m, "tab")       // Context
	m = key(t, m, "right")     // 3 → 10
	if s := screen(m); !strings.Contains(s, "[x]") || !strings.Contains(s, "‹ 50 ›") || !strings.Contains(s, "‹ 10 ›") {
		t.Errorf("edits not shown:\n%s", s)
	}
	if slices.Contains(r.commands(), "log --since=2026-09-01 --author=jane --no-merges -n 50") {
		t.Fatal("nothing runs before enter")
	}
	m = key(t, m, "enter")
	if !slices.Contains(r.commands(), "log --since=2026-09-01 --author=jane --no-merges -n 50") {
		t.Errorf("enter should apply: ran %v", r.commands())
	}
	s = screen(m)
	if strings.Contains(s, "No merges") {
		t.Errorf("form still open:\n%s", s)
	}
	if !strings.Contains(s, "commits author=jane merges max=") || !strings.Contains(s, "Diff context=10") {
		t.Errorf("title should list non-defaults:\n%s", s)
	}
}

func TestOptionsFormCancelResetAndErrors(t *testing.T) {
	r := optionsRunner()
	m := start(t, optionsUIDef, r)
	m = key(t, m, "o")
	m = key(t, m, "tab")
	m = typeText(t, m, "jane")
	m = key(t, m, "esc")
	if s := screen(m); strings.Contains(s, "author=") || strings.Contains(s, "Author") {
		t.Errorf("esc cancels:\n%s", s)
	}
	// An invalid date keeps the form open with the error; ctrl+r resets it.
	m = key(t, m, "o")
	m = typeText(t, m, "x")
	m = key(t, m, "enter")
	if s := screen(m); !strings.Contains(s, "not a date") || !strings.Contains(s, "Since") {
		t.Errorf("want the error in the open form:\n%s", s)
	}
	m = key(t, m, "ctrl+r")
	m = key(t, m, "enter")
	if s := screen(m); strings.Contains(s, "not a date") || strings.Contains(s, "Since") {
		t.Errorf("reset date should apply cleanly:\n%s", s)
	}
	// Somewhere without options, o says so.
	m = key(t, m, "tab") // files
	m = key(t, m, "o")
	if s := statusOf(m); !strings.Contains(s, "no options here") {
		t.Errorf("status = %q", s)
	}
}
