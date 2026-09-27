package engine

import "testing"

// Hidden panel tabs don't run until they are shown — unless something on
// screen reads them.
const lazyDef = `
panels:
  - {id: branches, source: branches}
  - {id: tags, source: tags, tab_of: branches}
  - {id: remotes, source: remotes, tab_of: branches}
  - {id: log, source: "log {{remotes.line}}"}
  - id: main
    content:
      tags: {tabs: [{name: Show, cmd: "show {{.line}}"}]}
      default: {tabs: [{name: Status, cmd: status}]}
actions:
  - {key: R, desc: Fetch, cmd: fetch, refresh: [branches, tags, remotes]}
`

func TestHiddenTabsAreLazy(t *testing.T) {
	h := newHarness(t, lazyDef)
	if _, ok := h.inflight["tags"]; ok {
		t.Error("tags is a hidden tab and nothing shown reads it: it must not run yet")
	}
	if _, ok := h.inflight["remotes"]; !ok {
		t.Error("remotes is hidden but log (on screen) reads it: it must run")
	}
	h.finish("branches", "main\n")
	h.finish("remotes", "origin/main\n")
	h.finish("log", "l\n")
	h.finish("main", "st\n")

	h.focus("branches")
	h.apply(h.e.NextTab()) // branches → tags
	if got := h.cmd("tags"); got != "tags" {
		t.Errorf("showing tags should run it: %q", got)
	}
	h.finish("tags", "v1\n")
	if got := h.cmd("main"); got != "show v1" {
		t.Errorf("main follows the shown tab: %q", got)
	}
	h.finish("main", "tag v1\n")
	h.apply(h.e.PrevTab())    // back to branches: loaded, nothing to run
	if len(h.inflight) != 0 { // main shows branches' (cached) status again
		t.Errorf("in flight after switching back: %v", h.inflight)
	}
}

func TestHiddenTabSkipsRefresh(t *testing.T) {
	h := newHarness(t, lazyDef)
	h.finish("branches", "main\n")
	h.finish("remotes", "origin/main\n")
	h.finish("log", "l\n")
	h.finish("main", "st\n")
	h.focus("branches")
	h.apply(h.e.NextTab())
	h.finish("tags", "v1\n")
	h.finish("main", "tag v1\n")
	h.apply(h.e.PrevTab())
	// An action refreshing everything re-runs what is shown; tags waits.
	h.apply(h.e.ActionDone(h.action("R")))
	h.doSettle()
	if _, ok := h.inflight["tags"]; ok {
		t.Error("a hidden tab is refreshed when shown, not now")
	}
	h.apply(h.e.NextTab())
	if got := h.cmd("tags"); got != "tags" {
		t.Errorf("tags re-runs when shown again: %q", got)
	}
}
