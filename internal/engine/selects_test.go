package engine

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

const selectsDef = `
env:
  AWS_PROFILE: "{{profile.line}}"
actions:
  - {key: X, desc: Reboot, cmd: "reboot {{region.line}}"}
panels:
  - {id: profile, select: true, source: aws configure list-profiles, default: dev}
  - {id: region, select: true, values: [eu-west-1, us-east-1]}
  - id: clusters
    source: "list-clusters {{region.line}}"
    mark: {source: current-cluster}
  - id: main
    content:
      clusters: {tabs: [{name: C, cmd: "describe {{.line}} {{region.line}}"}]}
`

// loadedSelects: profile chose "dev", clusters and main have loaded.
func loadedSelects(t *testing.T) *harness {
	h := newHarness(t, selectsDef)
	h.finish("profile", "default\ndev\nprod\n")
	h.finish("clusters", "web\napi\n")
	h.finish("clusters:mark", "api\n")
	h.finish("main", "desc api\n")
	return h
}

func TestValuesPanelNeedsNoRun(t *testing.T) {
	h := newHarness(t, selectsDef)
	if _, ok := h.inflight["region"]; ok {
		t.Error("a values panel must not run anything")
	}
	if v := h.e.View("region"); !reflect.DeepEqual(v.Lines, []string{"eu-west-1"}) {
		t.Errorf("select shows only its choice, got %q", v.Lines)
	}
	// clusters reads region, but also the env (profile), so it waits for profile.
	if _, ok := h.inflight["clusters"]; ok {
		t.Error("clusters ran before the env's profile was chosen")
	}
}

func TestSelectDefaultAndEnv(t *testing.T) {
	h := newHarness(t, selectsDef)
	h.finish("profile", "default\ndev\nprod\n")
	if v := h.e.View("profile"); !reflect.DeepEqual(v.Lines, []string{"dev"}) {
		t.Errorf("default choice = %q", v.Lines)
	}
	r := h.inflight["clusters"]
	if r.Req.Cmd != "list-clusters eu-west-1" || r.Req.Env["AWS_PROFILE"] != "dev" {
		t.Errorf("clusters run = %q env %v", r.Req.Cmd, r.Req.Env)
	}
	if m := h.inflight["clusters:mark"]; m.Req.Env["AWS_PROFILE"] != "dev" {
		t.Errorf("mark env = %v", m.Req.Env)
	}
}

func TestSetOverridesDefault(t *testing.T) {
	h := &harness{t: t, inflight: map[string]Run{}}
	h.e = New(mustDef(t, selectsDef), map[string]string{"profile": "prod", "region": "us-east-1"})
	h.apply(h.e.Start())
	h.finish("profile", "default\ndev\nprod\n")
	if got := h.cmd("clusters"); got != "list-clusters us-east-1" || h.inflight["clusters"].Req.Env["AWS_PROFILE"] != "prod" {
		t.Errorf("clusters = %q env %v", got, h.inflight["clusters"].Req.Env)
	}
}

func TestPickerCommitsOnlyOnEnter(t *testing.T) {
	h := loadedSelects(t)
	before := h.e.Focused()
	h.apply(h.e.Pick("region"))
	if h.e.Focused() != "region" || !h.e.PickerOpen() {
		t.Fatalf("picker not open: focused %q", h.e.Focused())
	}
	if v := h.e.View("region"); !reflect.DeepEqual(v.Lines, []string{"eu-west-1", "us-east-1"}) || v.Cursor != 0 {
		t.Errorf("picker view = %+v", v)
	}
	n := len(h.ran)
	h.apply(h.e.Move(1))
	if len(h.ran) != n || h.e.View("clusters").Stale {
		t.Error("moving in the picker must not change anything yet")
	}
	h.enter()
	if h.e.PickerOpen() || h.e.Focused() != before {
		t.Errorf("after enter: picker open %v, focused %q (was %q)", h.e.PickerOpen(), h.e.Focused(), before)
	}
	if got := h.cmd("clusters"); got != "list-clusters us-east-1" {
		t.Errorf("clusters = %q", got)
	}
	if v := h.e.View("region"); !reflect.DeepEqual(v.Lines, []string{"us-east-1"}) {
		t.Errorf("region shows %q", v.Lines)
	}
}

func TestPickerEscCancels(t *testing.T) {
	h := loadedSelects(t)
	h.apply(h.e.Pick("region"))
	h.apply(h.e.Move(1))
	n := len(h.ran)
	if !h.back() {
		t.Fatal("esc should close the picker")
	}
	if len(h.ran) != n || h.e.PickerOpen() {
		t.Errorf("esc changed something: ran %v", h.ran[n:])
	}
	if v := h.e.View("region"); !reflect.DeepEqual(v.Lines, []string{"eu-west-1"}) {
		t.Errorf("region = %q", v.Lines)
	}
	// Re-opening starts on the committed choice again.
	h.apply(h.e.Pick("region"))
	if v := h.e.View("region"); v.Cursor != 0 {
		t.Errorf("picker cursor = %d", v.Cursor)
	}
}

func TestEnvChangeRerunsUnchangedCommands(t *testing.T) {
	h := loadedSelects(t)
	h.apply(h.e.Pick("profile"))
	h.apply(h.e.Move(1)) // prod
	h.enter()
	r := h.inflight["clusters"]
	if r.Req.Cmd != "list-clusters eu-west-1" || r.Req.Env["AWS_PROFILE"] != "prod" {
		t.Errorf("same command must re-run under the new profile: %q %v", r.Req.Cmd, r.Req.Env)
	}
	h.finish("clusters", "db\n")
	h.finish("clusters:mark", "db\n")
	// Switching back to dev is served from the cache (keyed by env + command).
	h.finish("main", "desc db\n")
	n := len(h.ran)
	h.apply(h.e.Pick("profile"))
	h.apply(h.e.Move(-1))
	h.enter()
	for _, c := range h.ran[n:] {
		if strings.HasPrefix(c, "list-clusters") {
			t.Errorf("switching back re-fetched clusters: %v", h.ran[n:])
		}
	}
	if v := h.e.View("clusters"); !reflect.DeepEqual(v.Lines, []string{"web", "api"}) {
		t.Errorf("clusters = %q", v.Lines)
	}
}

func TestSelectsNeverBecomeActive(t *testing.T) {
	h := loadedSelects(t)
	h.focus("clusters")
	h.focus("region") // tab onto a select
	if v := h.e.ContentView("main"); v.Tabs == nil || v.Tabs[0] != "C" {
		t.Errorf("main should still show clusters' content: %+v", v)
	}
	h.enter() // enter on a focused select opens its picker
	if !h.e.PickerOpen() {
		t.Error("enter on a select should open the picker")
	}
}

func TestEnvReachesActionsAndIsUnsetUntilReady(t *testing.T) {
	h := newHarness(t, selectsDef)
	h.focus("clusters")
	req, err := h.e.RenderAction(h.action("X"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, set := req.Env["AWS_PROFILE"]; set {
		t.Errorf("env var should be unset while profile has no choice: %v", req.Env)
	}
	h.finish("profile", "dev\n")
	req, _ = h.e.RenderAction(h.action("X"), "")
	if req.Cmd != "reboot eu-west-1" || req.Env["AWS_PROFILE"] != "dev" {
		t.Errorf("action = %q %v", req.Cmd, req.Env)
	}
}

func TestSelectFailureShowsError(t *testing.T) {
	h := newHarness(t, selectsDef)
	h.finishErr("profile", errors.New("exit status 255: aws not configured"))
	if v := h.e.View("profile"); v.Err == "" {
		t.Errorf("profile = %+v", v)
	}
	if v := h.e.View("clusters"); v.Blocked != "no selection in profile" {
		t.Errorf("clusters = %+v", v)
	}
}
