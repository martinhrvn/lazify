package engine

import (
	"reflect"
	"testing"
	"time"
)

const refreshDef = `
panels:
  - {id: services, source: list-services, key: .line, refresh: 10s, enter: detail}
  - {id: detail, source: "describe {{services.line}}", refresh: 5s}
  - {id: tasks, source: "list-tasks {{services.line}}"}
  - {id: hidden, source: hidden-tab, tab_of: services, refresh: 3s}
  - {id: other, source: other}
`

func loadedRefresh(t *testing.T) *harness {
	h := newHarness(t, refreshDef)
	h.finish("services", "api\nweb\n")
	h.finish("hidden", "x\n")
	h.finish("other", "o\n")
	h.finish("tasks", "t1\n")
	return h
}

func TestRefreshIntervals(t *testing.T) {
	h := newHarness(t, refreshDef)
	want := map[string]time.Duration{"services": 10 * time.Second, "detail": 5 * time.Second, "hidden": 3 * time.Second}
	if got := h.e.RefreshIntervals(); !reflect.DeepEqual(got, want) {
		t.Errorf("intervals = %v", got)
	}
}

func TestAutoRefreshRerunsQuietly(t *testing.T) {
	h := loadedRefresh(t)
	h.move("services", 1) // web
	h.doSettle()
	h.finish("tasks", "t2\n")
	h.apply(h.e.AutoRefresh("services"))
	if got := h.cmd("services"); got != "list-services" {
		t.Fatalf("services = %q", got)
	}
	if v := h.e.View("services"); v.Stale {
		t.Error("an auto-refresh must not dim the rows (it would flicker)")
	}
	h.finish("services", "api\nweb\nnew\n")
	v := h.e.View("services")
	if len(v.Lines) != 3 || v.Cursor != 1 {
		t.Errorf("rows %q cursor %d, want the cursor kept on web", v.Lines, v.Cursor)
	}
	if _, ok := h.inflight["tasks"]; ok {
		t.Error("dependents re-run only when the selection's command changes")
	}
}

func TestAutoRefreshSkips(t *testing.T) {
	h := loadedRefresh(t)
	for _, id := range []string{"hidden", "detail", "other"} {
		if fx := h.e.AutoRefresh(id); len(fx.Runs) != 0 {
			t.Errorf("%s: hidden tab, closed Enter target or no refresh must not run: %v", id, fx.Runs)
		}
	}
	h.apply(h.e.AutoRefresh("services"))
	if fx := h.e.AutoRefresh("services"); len(fx.Runs) != 0 {
		t.Error("must not start a second run while one is in flight")
	}
	h.finish("services", "api\nweb\n")
	h.enter() // services → detail (drill): detail is shown now, services is not
	h.finish("detail", "d\n")
	if fx := h.e.AutoRefresh("services"); len(fx.Runs) != 0 {
		t.Error("services is covered by its drill-down level")
	}
	h.apply(h.e.AutoRefresh("detail"))
	if got := h.cmd("detail"); got != "describe api" {
		t.Errorf("detail = %q", got)
	}
}

func TestAutoRefreshKeepsDependentStreams(t *testing.T) {
	src := `
panels:
  - {id: services, source: list-services, key: .line, refresh: 10s}
  - {id: tasks, source: "list-tasks {{services.line}}", key: .line}
  - id: logs
    content: {default: {tabs: [{name: Logs, cmd: "tail {{.line}}", mode: stream}]}}
`
	h := newHarness(t, src)
	h.finish("services", "api\nweb\n")
	h.finish("tasks", "t1\nt2\n")
	h.focus("tasks")
	h.doSettle()
	stream := h.inflight["logs"].ID
	h.cancel = nil
	h.apply(h.e.AutoRefresh("services"))
	h.doSettle()
	if len(h.cancel) != 0 {
		t.Errorf("a quiet refresh must not stop what depends on it: cancelled %v", h.cancel)
	}
	h.finish("services", "api\nweb\nnew\n")
	h.doSettle()
	if r, ok := h.inflight["logs"]; !ok || r.ID != stream {
		t.Errorf("the log stream restarted (it would re-read its history): %+v", r)
	}
	if _, ok := h.inflight["tasks"]; ok {
		t.Error("tasks re-ran although its command is unchanged")
	}
	if v := h.e.ContentView("logs"); v.Stale {
		t.Errorf("logs = %+v", v)
	}
	if v := h.e.View("tasks"); v.Stale || v.Loading {
		t.Errorf("tasks must not flicker during a parent's refresh: %+v", v)
	}
}
