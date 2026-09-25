package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// at finds the screen position of text (first occurrence).
func at(t *testing.T, m tea.Model, text string) (x, y int) {
	t.Helper()
	for i, l := range strings.Split(screen(m), "\n") {
		if j := strings.Index(l, text); j >= 0 {
			return ansi.StringWidth(l[:j]), i
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, screen(m))
	return 0, 0
}

func click(t *testing.T, m tea.Model, text string) tea.Model {
	t.Helper()
	x, y := at(t, m, text)
	return mouse(t, m, x, y, tea.MouseButtonLeft)
}

func mouse(t *testing.T, m tea.Model, x, y int, b tea.MouseButton) tea.Model {
	t.Helper()
	m, cmd := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: tea.MouseActionPress})
	return drive(t, m, cmd)
}

func focusedOf(m tea.Model) string { return m.(Model).eng.Focused() }

func TestClickFocusesPanelAndSelectsRow(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git branch": "main\nfeature\n", "git tag": "v1\nv2\nv3\n"}}
	m := start(t, twoPanels, r)
	m = click(t, m, "v2")
	if focusedOf(m) != "tags" {
		t.Fatalf("click should focus tags, focused %q", focusedOf(m))
	}
	if c := m.(Model).eng.View("tags").Cursor; c != 1 {
		t.Errorf("click should select v2, cursor %d", c)
	}
	m = click(t, m, "feature")
	if focusedOf(m) != "branches" || m.(Model).eng.View("branches").Cursor != 1 {
		t.Errorf("focused %q cursor %d", focusedOf(m), m.(Model).eng.View("branches").Cursor)
	}
}

func TestClickSelectedRowIsEnter(t *testing.T) {
	m := start(t, enterUIDef, enterRunner())
	m = click(t, m, "b2") // select b2
	m = click(t, m, "a1") // select a1 (focus already there)
	if strings.Contains(screen(m), "› Files") {
		t.Fatal("a first click only selects")
	}
	m = click(t, m, "a1") // clicking the selected row = enter
	if !strings.Contains(screen(m), "Commits › a1 › Files") {
		t.Errorf("clicking the selected row should drill in:\n%s", screen(m))
	}
}

func TestWheelScrollsContentAndMovesLists(t *testing.T) {
	r := &fakeRunner{out: map[string]string{"git log": "a1\nb2\n", "git show a1": numbered("line", 100)}}
	m := start(t, contentUIDef, r)
	x, y := at(t, m, "line 5")
	m = mouse(t, m, x, y, tea.MouseButtonWheelDown)
	if got := mainLines(t, m)[0]; got == "line 0" {
		t.Errorf("wheel over content should scroll it, first line still %q", got)
	}
	x, y = at(t, m, "a1")
	m = mouse(t, m, x, y, tea.MouseButtonWheelDown)
	if c := m.(Model).eng.View("commits").Cursor; c != 1 {
		t.Errorf("wheel over a list should move its cursor, cursor %d", c)
	}
}

func TestClickSelectOpensPickerAndChooses(t *testing.T) {
	m := start(t, selectsUIDef, selectsRunner())
	m = click(t, m, "/etc") // the select: opens its picker
	if !m.(Model).eng.PickerOpen() {
		t.Fatal("clicking a select should open its picker")
	}
	m = click(t, m, "/usr/share") // a choice in the picker
	m = click(t, m, "/usr/share") // selected choice again = choose
	if m.(Model).eng.PickerOpen() || !strings.Contains(screen(m), "doc") {
		t.Errorf("clicking the selected choice should choose it:\n%s", screen(m))
	}
}

func TestClicksOutsideAPopupAreIgnored(t *testing.T) {
	m := start(t, enterUIDef, enterRunner())
	m = key(t, m, "enter")
	m = key(t, m, "j")
	m = key(t, m, "enter") // diff popup over the UI
	before := focusedOf(m)
	m = mouse(t, m, 0, 0, tea.MouseButtonLeft) // top-left: outside the popup
	if focusedOf(m) != before {
		t.Errorf("a click outside the popup changed focus to %q", focusedOf(m))
	}
}
