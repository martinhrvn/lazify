package engine

import (
	"reflect"
	"testing"
)

const tabsDef = `
panels:
  - {id: branches, title: Branches, source: git branch, enter: commits}
  - {id: remotes, title: Remotes, tab_of: branches, source: git branch -r, enter: rcommits}
  - {id: tags, title: Tags, tab_of: branches, source: git tag}
  - {id: commits, source: "git log {{branches.line}}"}
  - {id: rcommits, source: "git log {{remotes.line}}"}
  - {id: files, source: ls}
  - id: main
    content:
      branches: {tabs: [{name: Log, cmd: "log {{.line}}"}, {name: Graph, cmd: "graph {{.line}}"}]}
      tags: {tabs: [{name: Show, cmd: "show {{.line}}"}]}
`

func loadedTabs(t *testing.T) *harness {
	h := newHarness(t, tabsDef)
	h.finish("branches", "main\n")
	h.finish("remotes", "origin/main\n")
	h.finish("tags", "v1\n")
	h.finish("files", "a\n")
	h.finish("main", "log main\n")
	return h
}

func TestTabsRunAndShareASlot(t *testing.T) {
	h := newHarness(t, tabsDef)
	for _, p := range []string{"branches", "remotes", "tags"} {
		if _, ok := h.inflight[p]; !ok {
			t.Errorf("%s should run at start (hidden tabs run too)", p)
		}
	}
	if got := h.e.TopLevel(); !reflect.DeepEqual(got, []string{"branches", "files", "main"}) {
		t.Errorf("top level = %v", got)
	}
	if got := h.e.Tabs("branches"); !reflect.DeepEqual(got, []string{"branches", "remotes", "tags"}) {
		t.Errorf("tabs = %v", got)
	}
}

func TestSwitchPanelTabs(t *testing.T) {
	h := loadedTabs(t)
	h.apply(h.e.NextTab())
	if h.e.Focused() != "remotes" || h.e.ActiveTab("branches") != "remotes" {
		t.Errorf("after ]: focused %q", h.e.Focused())
	}
	h.apply(h.e.NextTab())
	if h.e.Focused() != "tags" {
		t.Errorf("focused %q", h.e.Focused())
	}
	// Content follows the shown tab: main has an entry for tags.
	if got := h.cmd("main"); got != "show v1" {
		t.Errorf("main = %q", got)
	}
	h.finish("main", "tag v1\n")
	h.apply(h.e.NextTab()) // wraps
	if h.e.Focused() != "branches" {
		t.Errorf("] should wrap to branches, got %q", h.e.Focused())
	}
	h.apply(h.e.PrevTab())
	if h.e.Focused() != "tags" {
		t.Errorf("[ from the first tab should wrap to the last, got %q", h.e.Focused())
	}
	// The slot remembers its tab when focus leaves and comes back.
	h.focus("files")
	h.apply(h.e.FocusPanel("branches"))
	if h.e.Focused() != "tags" {
		t.Errorf("slot forgot its tab: %q", h.e.Focused())
	}
}

func TestContentTabsStillSwitchWhenContentFocused(t *testing.T) {
	h := loadedTabs(t)
	h.focus("main")
	h.apply(h.e.NextTab())
	if v := h.e.ContentView("main"); v.Active != 1 {
		t.Errorf("] on main should switch its content tab: %+v", v)
	}
	if h.e.ActiveTab("branches") != "branches" {
		t.Error("] on main must not switch panel tabs")
	}
	// An untabbed list panel falls back to the content tabs, as before.
	h.focus("files")
	if fx := h.e.NextTab(); h.e.Focused() != "files" || len(fx.Runs) > 1 {
		t.Errorf("focused %q", h.e.Focused())
	}
}

func TestDrillDownIsPerTab(t *testing.T) {
	h := loadedTabs(t)
	h.apply(h.e.NextTab()) // remotes
	h.enter()
	if got := h.cmd("rcommits"); got != "git log origin/main" {
		t.Fatalf("rcommits = %q", got)
	}
	h.finish("rcommits", "r1\n")
	if got := h.e.Crumbs("branches"); !reflect.DeepEqual(got, []string{"Remotes", "origin/main", "rcommits"}) {
		t.Errorf("crumbs = %q", got)
	}
	h.apply(h.e.NextTab()) // tags: not drilled
	if h.e.Focused() != "tags" || h.e.CanGoBack() {
		t.Errorf("tags: focused %q, can go back %v", h.e.Focused(), h.e.CanGoBack())
	}
	h.apply(h.e.PrevTab()) // back to remotes: still drilled in
	if h.e.Focused() != "rcommits" {
		t.Errorf("remotes' drill-down lost: focused %q", h.e.Focused())
	}
	h.back()
	if h.e.Focused() != "remotes" {
		t.Errorf("esc should pop the visible tab's level, focused %q", h.e.Focused())
	}
}

func TestFocusMemberSelectsItsTab(t *testing.T) {
	h := loadedTabs(t)
	h.focus("files")
	h.apply(h.e.FocusPanel("tags"))
	if h.e.Focused() != "tags" || h.e.ActiveTab("branches") != "tags" {
		t.Errorf("focused %q", h.e.Focused())
	}
}
