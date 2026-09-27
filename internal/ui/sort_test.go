package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const sortUIDef = `
panels:
  - id: svc
    title: Services
    source: svc
    rows: .[]
    key: .name
    columns:
      - {title: Name, value: "{{.name}}"}
      - {title: Mem, value: "{{.mem}}", format: bytes}
  - id: other
    source: other
`

func sortRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{
		"svc":   `[{"name":"web","mem":9000},{"name":"api","mem":1048576},{"name":"db","mem":512}]`,
		"other": "x\n",
	}}
}

// order is the service names top to bottom as drawn.
func order(m tea.Model) string {
	var out []string
	for _, l := range strings.Split(screen(m), "\n") {
		for _, n := range []string{"web", "api", "db"} {
			if strings.HasPrefix(strings.TrimLeft(l, "│"), n+" ") {
				out = append(out, n)
			}
		}
	}
	return strings.Join(out, ",")
}

func TestSortKeys(t *testing.T) {
	m := start(t, sortUIDef, sortRunner())
	if !strings.Contains(statusOf(m), "</> sort") {
		t.Errorf("hint bar: %q", statusOf(m))
	}
	m = key(t, m, ">")
	if got := order(m); got != "api,db,web" || !strings.Contains(screen(m), "Name ▲") {
		t.Errorf("> by name: %s\n%s", got, screen(m))
	}
	m = key(t, m, ">")
	m = key(t, m, "~")
	if got := order(m); got != "api,web,db" || !strings.Contains(screen(m), "Mem ▼") {
		t.Errorf("> ~ by mem, biggest first: %s\n%s", got, screen(m))
	}
	// The indicator widens its column; the cells stay under their header.
	s := screen(m)
	header, row := lineWith(s, "Mem ▼"), lineWith(s, "1.0 MiB")
	if strings.Index(header, "Mem") != strings.Index(row, "1.0 MiB") {
		t.Errorf("misaligned:\n%s\n%s", header, row)
	}
	m = key(t, m, ">") // past the last column: the command's order
	if got := order(m); got != "web,api,db" || strings.ContainsAny(screen(m), "▲▼") {
		t.Errorf("unsorted: %s", got)
	}
}

func TestSortByClickingHeader(t *testing.T) {
	m := start(t, sortUIDef, sortRunner())
	m = key(t, m, "tab") // focus elsewhere: the click focuses Services too
	var x, y int
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if j := strings.Index(l, "Mem"); j >= 0 && strings.Contains(l, "Name") {
			x, y = ansi.StringWidth(l[:j])+1, i
		}
	}
	click := tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	m, cmd := m.Update(click)
	m = drive(t, m, cmd)
	if got := order(m); got != "db,web,api" || !strings.Contains(screen(m), "Mem ▲") {
		t.Errorf("click Mem: %s\n%s", got, screen(m))
	}
	m, cmd = m.Update(click)
	m = drive(t, m, cmd)
	if got := order(m); got != "api,web,db" || !strings.Contains(screen(m), "Mem ▼") {
		t.Errorf("click Mem again: %s\n%s", got, screen(m))
	}
}
