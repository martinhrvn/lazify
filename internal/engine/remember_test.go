package engine

import (
	"reflect"
	"testing"
)

const rememberDef = `
panels:
  - id: profile
    select: true
    source: profiles
    mark: {source: 'echo "$AWS_PROFILE"'}
  - {id: region, select: true, values: [eu-west-1, eu-central-1], default: eu-central-1}
  - {id: clusters, source: "clusters {{region.line}}", remember: true}
  - {id: services, source: "services {{clusters.line}}"}
`

func recallHarness(t *testing.T, set, recall map[string]string) *harness {
	h := &harness{t: t, inflight: map[string]Run{}}
	h.e = New(mustDef(t, rememberDef), set)
	h.e.Recall(recall)
	h.apply(h.e.Start())
	return h
}

func TestRecallRestoresRows(t *testing.T) {
	h := recallHarness(t, nil, map[string]string{"region": "eu-west-1", "clusters": "api", "services": "db"})
	if got := h.cmd("clusters"); got != "clusters eu-west-1" {
		t.Errorf("recalled region beats default: clusters = %q", got)
	}
	h.finish("clusters", "web\napi\n")
	if got := h.cmd("services"); got != "services api" {
		t.Errorf("recalled cluster: services = %q", got)
	}
	h.finish("services", "cache\ndb\n")
	if c := h.e.View("services").Cursor; c != 0 {
		t.Error("services doesn't remember: recall must not move it")
	}
}

func TestRecallPrecedence(t *testing.T) {
	// --set beats a recalled choice.
	h := recallHarness(t, map[string]string{"region": "eu-central-1"}, map[string]string{"region": "eu-west-1"})
	if got := h.cmd("clusters"); got != "clusters eu-central-1" {
		t.Errorf("--set should win: %q", got)
	}
	// A recalled choice beats the mark, even when the mark answers later.
	h = recallHarness(t, nil, map[string]string{"profile": "prod"})
	h.finish("profile", "dev\nprod\n")
	h.finish("profile:mark", "dev\n")
	if sel, _ := h.e.Selection("profile"); sel.(map[string]any)["line"] != "prod" {
		t.Errorf("profile = %v, want the recalled prod over the dev mark", sel)
	}
	// A recalled row that is gone falls back to the default, then the mark.
	h = recallHarness(t, nil, map[string]string{"region": "us-east-1", "profile": "gone"})
	if got := h.cmd("clusters"); got != "clusters eu-central-1" {
		t.Errorf("gone region should fall back to the default: %q", got)
	}
	h.finish("profile", "dev\nprod\n")
	h.finish("profile:mark", "prod\n")
	if sel, _ := h.e.Selection("profile"); sel.(map[string]any)["line"] != "prod" {
		t.Errorf("gone profile should fall back to the mark: %v", sel)
	}
}

func TestRemembered(t *testing.T) {
	h := recallHarness(t, nil, nil)
	h.finish("profile", "dev\nprod\n")
	h.finish("profile:mark", "dev\n")
	want := map[string]string{"profile": "dev", "region": "eu-central-1"}
	if got := h.e.Remembered(); !reflect.DeepEqual(got, want) {
		t.Errorf("before clusters load: %v, want %v (no selection: left out)", got, want)
	}
	h.finish("clusters", "web\napi\n")
	h.move("clusters", 1)
	h.doSettle()
	h.finish("services", "db\n")
	want["clusters"] = "api"
	if got := h.e.Remembered(); !reflect.DeepEqual(got, want) {
		t.Errorf("remembered = %v, want %v (services doesn't remember)", got, want)
	}
}
