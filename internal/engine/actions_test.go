package engine

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

const actionsDef = `
actions:
  - {key: R, desc: Fetch, cmd: "git fetch {{.line}}", refresh: [branches]}
  - {key: space, desc: Global space, cmd: echo global}
  - {key: S, desc: Shell, cmd: sh, mode: interactive}
panels:
  - id: branches
    source: git branch
    actions:
      - {key: space, desc: Checkout, cmd: "git checkout {{.line}}", refresh: [branches, commits]}
      - {key: n, desc: New, prompt: Name, cmd: "git checkout -b {{input}}"}
      - {key: x, cmd: "rm {{.line}}"}
  - {id: commits, source: "git log {{branches.line}}"}
  - id: main
    content: {default: {tabs: [{name: S, cmd: git status}]}}
    actions: [{key: y, desc: Copy, cmd: "echo {{.line}}"}]
`

func loadedActions(t *testing.T) *harness {
	h := newHarness(t, actionsDef)
	h.finish("main", "clean\n")
	h.finish("branches", "main\nfeature\n")
	h.finish("commits", "a1 fix\n")
	return h
}

func (h *harness) action(key string) ActionRef {
	h.t.Helper()
	ref, ok := h.e.ActionFor(key)
	if !ok {
		h.t.Fatalf("no action for %q", key)
	}
	return ref
}

func (h *harness) render(key, input string) string {
	h.t.Helper()
	req, err := h.e.RenderAction(h.action(key), input)
	if err != nil {
		h.t.Fatalf("render %q: %v", key, err)
	}
	return req.Cmd
}

func TestPanelActionsOverrideGlobals(t *testing.T) {
	h := loadedActions(t)
	if ref := h.action("space"); ref.Action.Desc != "Checkout" || ref.Panel != "branches" {
		t.Errorf("space on branches = %+v", ref)
	}
	if ref := h.action("R"); ref.Action.Desc != "Fetch" || ref.Panel != "" {
		t.Errorf("R = %+v", ref)
	}
	h.focus("commits")
	if ref := h.action("space"); ref.Action.Desc != "Global space" {
		t.Errorf("space on commits should be the global, got %q", ref.Action.Desc)
	}
	if _, ok := h.e.ActionFor("n"); ok {
		t.Error("branches' n must not apply on commits")
	}
}

func TestBindings(t *testing.T) {
	h := loadedActions(t)
	want := []Binding{
		{Key: "space", Desc: "Checkout"},
		{Key: "n", Desc: "New"},
		{Key: "x", Desc: "rm {{.line}}"},
		{Key: "R", Desc: "Fetch", Global: true},
		{Key: "space", Desc: "Global space", Global: true, Overridden: true},
		{Key: "S", Desc: "Shell", Global: true},
	}
	if got := h.e.Bindings(); !reflect.DeepEqual(got, want) {
		t.Errorf("bindings =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRenderActionRows(t *testing.T) {
	h := loadedActions(t)
	h.move("branches", 1)
	if got := h.render("space", ""); got != "git checkout feature" {
		t.Errorf("checkout = %q", got)
	}
	h.doSettle()
	h.finish("commits", "b2 feat\n")
	h.focus("commits")
	// Globals use the active panel's row.
	if got := h.render("R", ""); got != "git fetch 'b2 feat'" {
		t.Errorf("global on commits = %q", got)
	}
	// A content panel's action also uses the active (last list) panel's row.
	h.focus("main")
	if got := h.render("y", ""); got != "echo 'b2 feat'" {
		t.Errorf("content panel action = %q", got)
	}
}

func TestRenderActionInputIsQuoted(t *testing.T) {
	h := loadedActions(t)
	if got := h.render("n", "feat x; rm -rf ~"); got != `git checkout -b 'feat x; rm -rf ~'` {
		t.Errorf("cmd = %q", got)
	}
}

func TestRenderActionMissingField(t *testing.T) {
	h := newHarness(t, actionsDef)
	h.finish("branches", "")
	_, err := h.e.RenderAction(h.action("space"), "")
	var me *tmpl.MissingError
	if !errors.As(err, &me) {
		t.Errorf("err = %v, want missing field", err)
	}
}

func TestRenderActionTimeouts(t *testing.T) {
	h := loadedActions(t)
	bg, _ := h.e.RenderAction(h.action("space"), "")
	sh, _ := h.e.RenderAction(h.action("S"), "")
	if bg.Timeout != def.DefaultTimeout || sh.Timeout != 0 {
		t.Errorf("timeouts: background %v, interactive %v", bg.Timeout, sh.Timeout)
	}
}

func TestActionDoneRefreshesListedPanelsAndClearsCaches(t *testing.T) {
	h := loadedActions(t)
	h.move("branches", 1)
	h.doSettle()
	h.finish("commits", "b2 feat\n")
	n := len(h.ran)
	h.apply(h.e.ActionDone(h.action("space")))
	got := h.ran[n:]
	slices.Sort(got)
	if want := []string{"git branch", "git status"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after checkout ran %v, want %v", got, want)
	}
	// commits is refreshed too, but waits for branches (its input) first.
	if v := h.e.View("commits"); !v.Loading || !v.Stale {
		t.Errorf("commits should wait with stale rows: %+v", v)
	}
	h.finish("branches", "main\nfeature\n")
	if got := h.cmd("commits"); got != "git log feature" {
		t.Errorf("commits after branches = %q", got)
	}
	h.finish("commits", "b2 feat\n")
	h.finish("main", "clean\n")
	// main's commits were cached before the action; the cache is gone now.
	h.move("branches", -1)
	h.doSettle()
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("stale cache served after action, commits in flight: %q", got)
	}
}

func TestActionDoneDefaultRefresh(t *testing.T) {
	h := loadedActions(t)
	n := len(h.ran)
	h.apply(h.e.ActionDone(h.action("x"))) // panel action without refresh: its own panel
	if got := h.ran[n:]; !slices.Contains(got, "git branch") || slices.Contains(got, "git log main") {
		t.Errorf("default refresh ran %v", got)
	}
	h.finish("branches", "main\nfeature\n")
	h.finish("main", "clean\n")
	h.focus("commits")
	n = len(h.ran)
	h.apply(h.e.ActionDone(h.action("space"))) // global without refresh: content only
	if got := h.ran[n:]; !reflect.DeepEqual(got, []string{"git status"}) {
		t.Errorf("global without refresh ran %v", got)
	}
}
