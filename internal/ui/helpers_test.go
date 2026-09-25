package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
)

const helpersUIDef = `
panels:
  - id: tasks
    title: Tasks
    source: x
    rows: .[]
    columns:
      - title: Status
        value: "{{.status}}"
        style:
          RUNNING: {icon: check, color: ok}
          PENDING: {spinner: true, color: warn}
          STOPPED: {icon: cross, color: error, text: false}
      - {title: Name, value: "{{.name}}"}
    row_style: {value: "{{.desired}}", map: {STOPPED: dim}}
`

func helpersRunner() *fakeRunner {
	return &fakeRunner{out: map[string]string{"x": `[
	  {"status":"RUNNING","name":"api","desired":"RUNNING"},
	  {"status":"PENDING","name":"web","desired":"RUNNING"},
	  {"status":"STOPPED","name":"old","desired":"STOPPED"}]`}}
}

func lineWith(s, text string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(ansi.Strip(l), text) {
			return l
		}
	}
	return ""
}

func TestIconsSpinnerAndColours(t *testing.T) {
	withColors(t)
	m := start(t, helpersUIDef, helpersRunner())
	s := screen(m)
	for _, want := range []string{"✓ RUNNING", "⠋ PENDING", "✗ "} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "STOPPED") {
		t.Error("text: false shows only the icon")
	}
	// Alignment holds with icons: Name starts in the same column on every row.
	header, api, old := ansi.Strip(lineWith(s, "Name")), ansi.Strip(lineWith(s, "api")), ansi.Strip(lineWith(s, "old"))
	colOf := func(line, text string) int { return ansi.StringWidth(line[:strings.Index(line, text)]) } // display columns, not bytes
	if col := colOf(header, "Name"); col != colOf(api, "api") || col != colOf(old, "old") {
		t.Errorf("columns misaligned:\n%s\n%s\n%s", header, api, old)
	}
	raw := m.View()
	if !strings.Contains(lineWith(raw, "web"), "\x1b[33m") || !strings.Contains(lineWith(raw, "old"), "\x1b[2m") {
		t.Errorf("expected yellow (warn) on web and dim (row style) on old:\n%q\n%q", lineWith(raw, "web"), lineWith(raw, "old"))
	}
	// The spinner advances on its tick.
	m, _ = m.Update(spinMsg{})
	if !strings.Contains(screen(m), "⠙ PENDING") {
		t.Errorf("spinner should advance:\n%s", screen(m))
	}
}

func TestCursorHighlightSurvivesColouredCells(t *testing.T) {
	withColors(t)
	m := start(t, helpersUIDef, helpersRunner())
	cursor := lineWith(m.View(), "api")                                              // first row, focused
	inner := cursor[strings.Index(cursor, "\x1b[7m"):strings.LastIndex(cursor, "│")] // drop the right border
	inner = inner[:strings.LastIndex(inner, "\x1b[0m")]                              // and the reset ending the line
	for i := strings.Index(inner, "\x1b[0m"); i >= 0; {
		rest := inner[i+len("\x1b[0m"):]
		if !strings.HasPrefix(rest, "\x1b[7m") {
			t.Fatalf("cursor highlight cut short after a coloured cell: %q", inner)
		}
		j := strings.Index(rest, "\x1b[0m")
		if j < 0 {
			break
		}
		i += len("\x1b[0m") + j
	}
}

func TestSpinnerTicksOnlyWhenShown(t *testing.T) {
	for name, tc := range map[string]struct {
		out   string
		ticks bool
	}{
		"spinner on screen": {`[{"status":"PENDING","name":"web","desired":"RUNNING"}]`, true},
		"no spinner":        {`[{"status":"RUNNING","name":"api","desired":"RUNNING"}]`, false},
	} {
		d, err := def.Parse([]byte(helpersUIDef), "t.yaml")
		if err != nil {
			t.Fatal(err)
		}
		model := New(d, &fakeRunner{out: map[string]string{"x": tc.out}}, nil)
		model.debounce, model.toastTTL = 0, 0
		spins := 0
		model.every = func(_ time.Duration, msg tea.Msg) tea.Cmd {
			if _, ok := msg.(spinMsg); ok {
				spins++
			}
			return nil
		}
		var m tea.Model = model
		m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
		drive(t, m, m.Init())
		if (spins > 0) != tc.ticks {
			t.Errorf("%s: spinner ticks scheduled = %d", name, spins)
		}
	}
}
