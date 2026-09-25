package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const filterUIDef = `
panels:
  - {id: branches, title: Branches, source: git branch}
  - {id: log, title: Log, source: "git log {{branches.line}}"}
`

func filterRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"git branch":             "main\nfeature/login\nfeature/logout\n",
		"git log main":           "m1\n",
		"git log feature/login":  "l1\n",
		"git log feature/logout": "o1\n",
	}}
}

func TestFilterAsYouType(t *testing.T) {
	m := start(t, filterUIDef, filterRunner())
	if !strings.Contains(statusOf(m), "/ filter") {
		t.Errorf("hint missing: %q", statusOf(m))
	}
	m = key(t, m, "/")
	if s := statusOf(m); !strings.HasPrefix(strings.TrimSpace(s), "/") {
		t.Errorf("filter input not shown: %q", s)
	}
	m = key(t, m, "o")
	m = key(t, m, "u")
	m = key(t, m, "t") // "out": typed keys go to the filter, not to panels
	s := screen(m)
	if !strings.Contains(s, "feature/logout") || strings.Contains(s, "feature/login") || strings.Contains(s, "│main") {
		t.Fatalf("rows should narrow as you type:\n%s", s)
	}
	if !strings.Contains(s, "Branches /out") {
		t.Errorf("title should show the filter:\n%s", s)
	}
	m = key(t, m, "enter") // keep it
	if s := statusOf(m); strings.HasPrefix(strings.TrimSpace(s), "/out") {
		t.Errorf("enter should leave the input: %q", s)
	}
	if s := screen(m); !strings.Contains(s, "o1") || !strings.Contains(s, "Branches /out") {
		t.Errorf("filter kept and log follows the selection:\n%s", s)
	}
	if !strings.Contains(statusOf(m), "esc clear filter") {
		t.Errorf("hint: %q", statusOf(m))
	}
	m = key(t, m, "esc")
	if s := screen(m); !strings.Contains(s, "│main") || strings.Contains(s, "/out") {
		t.Errorf("esc clears the filter:\n%s", s)
	}
}

func TestFilterInputEscAndQuitKey(t *testing.T) {
	m := start(t, filterUIDef, filterRunner())
	m = key(t, m, "/")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, quit := cmd().(tea.QuitMsg); quit {
			t.Fatal("q typed into the filter must not quit")
		}
	}
	m = key(t, m, "zz")
	if s := screen(m); !strings.Contains(s, "(no matches)") {
		t.Errorf("no matches not shown:\n%s", s)
	}
	m = key(t, m, "esc") // esc while typing clears and leaves
	if s := screen(m); !strings.Contains(s, "│main") || strings.HasPrefix(strings.TrimSpace(statusOf(m)), "/zz") {
		t.Errorf("esc should clear and close the filter:\n%s", s)
	}
}
