package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const searchDef = `
panels:
  - id: files
    source: files
  - id: main
    content: {default: {tabs: [{name: Log, cmd: log}]}}
`

func searchRunner() *fakeRunner {
	var log, files strings.Builder
	for i := 1; i <= 60; i++ {
		if i%20 == 10 {
			fmt.Fprintf(&log, "line %d \x1b[31mERROR\x1b[0m boom\n", i)
		} else {
			fmt.Fprintf(&log, "line %d ok\n", i)
		}
	}
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&files, "file%d\n", i)
	}
	return &fakeRunner{out: map[string]string{"files": files.String(), "log": log.String()}}
}

// mainTitle is the details panel's title line.
func mainTitle(m tea.Model) string {
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "[2] Log") {
			return l
		}
	}
	return ""
}

func TestSearchContent(t *testing.T) {
	m := start(t, searchDef, searchRunner())
	m = key(t, m, "2") // focus the details panel
	if !strings.Contains(statusOf(m), "/ search") {
		t.Errorf("hint bar should offer search: %q", statusOf(m))
	}
	m = key(t, m, "/")
	m = typeText(t, m, "error") // lower case: case-insensitive
	m = key(t, m, "enter")
	if title := mainTitle(m); !strings.Contains(title, "/error 1/3") {
		t.Errorf("title = %q", title)
	}
	if s := screen(m); !strings.Contains(s, "line 10 ERROR") {
		t.Errorf("first match should be on screen:\n%s", s)
	}
	m = key(t, m, "n")
	if title, s := mainTitle(m), screen(m); !strings.Contains(title, "/error 2/3") || !strings.Contains(s, "line 30 ERROR") {
		t.Errorf("n: %q\n%s", title, s)
	}
	m = key(t, m, "N")
	m = key(t, m, "N") // wraps to the last match
	if title, s := mainTitle(m), screen(m); !strings.Contains(title, "/error 3/3") || !strings.Contains(s, "line 50 ERROR") {
		t.Errorf("N twice: %q\n%s", title, s)
	}
	// The match is highlighted in place; the line keeps its other colours.
	withColors(t)
	raw := m.View()
	l := lineWith(raw, "line 50")
	if i := strings.Index(l, "ERROR"); i < 1 || l[i-1] != 'm' {
		t.Errorf("match not highlighted: %q", l)
	}
	m = key(t, m, "/")
	m = typeText(t, m, "nothing")
	m = key(t, m, "enter")
	if title := mainTitle(m); !strings.Contains(title, "/nothing (no match)") {
		t.Errorf("title = %q", title)
	}
	m = key(t, m, "esc") // esc clears the search
	if title := mainTitle(m); strings.Contains(title, "/nothing") {
		t.Errorf("esc should clear: %q", title)
	}
}

func TestTopBottomAndScrollbar(t *testing.T) {
	m := start(t, searchDef, searchRunner())
	thumbRows := func(m tea.Model, col int) []int {
		var rows []int
		for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
			if r := []rune(l); col < len(r) && r[col] == '┃' {
				rows = append(rows, i)
			}
		}
		return rows
	}
	right := len([]rune(strings.Split(ansi.Strip(m.View()), "\n")[0])) - 1
	top := thumbRows(m, right)
	if len(top) == 0 {
		t.Fatalf("60 lines in a small panel: want a scroll bar\n%s", screen(m))
	}
	m = key(t, m, "2")
	m = key(t, m, "G")
	if s := screen(m); !strings.Contains(s, "line 60") {
		t.Errorf("G: bottom\n%s", s)
	}
	bottom := thumbRows(m, right)
	if len(bottom) == 0 || bottom[0] <= top[0] {
		t.Errorf("the thumb should move down: %v → %v", top, bottom)
	}
	m = key(t, m, "g")
	if s := screen(m); !strings.Contains(s, "line 1 ok") {
		t.Errorf("g: top\n%s", s)
	}
	// Lists get one too: 100 files.
	found := false
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.HasPrefix(l, "│file") && strings.Contains(l, "┃") && strings.Index(l, "┃") < strings.LastIndex(l, "│") {
			found = true
		}
	}
	if !found {
		t.Errorf("the files list should have a scroll bar:\n%s", screen(m))
	}
}
