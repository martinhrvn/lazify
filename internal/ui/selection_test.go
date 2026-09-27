package ui

import (
	"strings"
	"testing"
)

const selectionDef = `
panels:
  - {id: region, select: true, values: [eu, us]}
  - {id: branches, source: branches}
  - {id: commits, source: "log {{branches.line}}"}
`

// Unfocused lists still show their selected row (dimmer than the focused
// panel's cursor), so you can see what the other panels follow.
func TestInactiveSelectionShows(t *testing.T) {
	withColors(t)
	r := &fakeRunner{out: map[string]string{"branches": "main\nfeature\n", "log feature": "f1\nf2\n"}}
	m := start(t, selectionDef, r)
	m = key(t, m, "2") // branches
	m = key(t, m, "j") // feature
	m = key(t, m, "3") // commits: branches loses focus
	raw := m.View()
	inactive := lineWith(raw, "feature")
	if !strings.Contains(inactive, "\x1b[48;5;") || strings.Contains(inactive, "\x1b[7m") {
		t.Errorf("unfocused selection should have a background, not the reverse cursor: %q", inactive)
	}
	if l := lineWith(raw, "main"); strings.Contains(l, "\x1b[48;5;") {
		t.Errorf("other rows stay plain: %q", l)
	}
	if l := lineWith(raw, "f1"); !strings.Contains(l, "\x1b[7m") {
		t.Errorf("the focused panel keeps its cursor: %q", l)
	}
	if l := lineWith(raw, "eu"); strings.Contains(l, "\x1b[48;5;") || strings.Contains(l, "\x1b[7m") {
		t.Errorf("a select shows just its choice, unhighlighted: %q", l)
	}
}
