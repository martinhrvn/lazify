package engine

import "testing"

const focusDef = `
panels:
  - {id: clusters, source: list-clusters, key: .line, default: jobs, enter: {focus: services}}
  - {id: services, source: "list-services {{clusters.line}}", enter: {focus: next}}
  - {id: tasks, source: "list-tasks {{services.line}}"}
`

func TestDefaultPicksTheInitialRow(t *testing.T) {
	h := newHarness(t, focusDef)
	h.finish("clusters", "web\njobs\n")
	if got := h.cmd("services"); got != "list-services jobs" {
		t.Errorf("services = %q, want the default cluster", got)
	}
	h2 := &harness{t: t, inflight: map[string]Run{}}
	h2.e = New(mustDef(t, focusDef), map[string]string{"clusters": "web"})
	h2.apply(h2.e.Start())
	h2.finish("clusters", "web\njobs\n")
	if got := h2.cmd("services"); got != "list-services web" {
		t.Errorf("--set should win over default: %q", got)
	}
}

func TestEnterMovesFocus(t *testing.T) {
	h := newHarness(t, focusDef)
	h.finish("clusters", "web\njobs\n")
	h.finish("services", "api\n")
	if title, ok := h.e.EnterTarget(); !ok || title != "services" {
		t.Errorf("enter target = %q %v", title, ok)
	}
	h.enter()
	if h.e.Focused() != "services" {
		t.Fatalf("enter should focus services, got %q", h.e.Focused())
	}
	if title, _ := h.e.EnterTarget(); title != "next panel" {
		t.Errorf("hint for focus: next = %q", title)
	}
	h.enter()
	if h.e.Focused() != "tasks" {
		t.Errorf("enter: {focus: next} should focus tasks, got %q", h.e.Focused())
	}
}
