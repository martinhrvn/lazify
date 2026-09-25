package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/def"
)

func TestAutoRefreshTimers(t *testing.T) {
	d, err := def.Parse([]byte("panels:\n  - {id: svc, title: Services, source: list, refresh: 10s}\n  - {id: other, source: other}"), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{out: map[string]string{"list": "api\n", "other": "o\n"}}
	model := New(d, r, nil)
	model.debounce, model.toastTTL = 0, 0
	type timer struct {
		d   time.Duration
		msg tea.Msg
	}
	var timers []timer
	model.every = func(d time.Duration, msg tea.Msg) tea.Cmd {
		timers = append(timers, timer{d, msg})
		return nil
	}
	var m tea.Model = model
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = drive(t, m, m.Init())
	if len(timers) != 1 || timers[0].d != 10*time.Second {
		t.Fatalf("timers after start = %+v", timers)
	}

	r.out["list"] = "api\nweb\n"
	m, cmd := m.Update(timers[0].msg) // the timer fires
	m = drive(t, m, cmd)
	if s := screen(m); !strings.Contains(s, "web") {
		t.Errorf("auto-refresh should show the new rows:\n%s", s)
	}
	if len(timers) != 2 || timers[1].d != 10*time.Second {
		t.Errorf("the timer should be scheduled again: %+v", timers)
	}
}
