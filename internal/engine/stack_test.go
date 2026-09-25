package engine

import (
	"reflect"
	"slices"
	"testing"
)

const stackDef = `
panels:
  - id: commits
    source: git log
    split: "\t"
    key: .fields.0
    enter: files
  - id: files
    source: "git show --name-only {{commits.fields.0}}"
    enter: {panel: diff, popup: true}
  - id: diff
    content: {default: {tabs: [{name: Diff, cmd: "git show {{commits.fields.0}} -- {{files.line}}"}]}}
  - id: tags
    source: git tag
    enter: {panel: tagfiles, popup: {width: 60, height: 50}}
  - id: tagfiles
    source: "git show --name-only {{tags.line}}"
    enter: {panel: tagtail, popup: full}
  - id: tagtail
    content: {default: {tabs: [{name: T, cmd: "tail -f {{tagfiles.line}}", mode: stream}]}}
  - id: main
    content:
      commits: {tabs: [{name: C, cmd: "git show {{.fields.0}}"}]}
      files: {tabs: [{name: F, cmd: "cat {{.line}}"}]}
`

func loadedStack(t *testing.T) *harness {
	h := newHarness(t, stackDef)
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	h.finish("tags", "v1\n")
	h.finish("main", "show a1\n")
	return h
}

func (h *harness) enter() { h.apply(h.e.Enter()) }

func (h *harness) back() bool {
	fx, ok := h.e.Back()
	h.apply(fx)
	return ok
}

func TestEnterDrillsDown(t *testing.T) {
	h := loadedStack(t)
	if _, ok := h.inflight["files"]; ok {
		t.Fatal("enter target ran before being entered")
	}
	h.move("commits", 1)
	h.doSettle()
	h.finish("main", "show b2\n")
	h.enter()
	if got := h.cmd("files"); got != "git show --name-only b2" {
		t.Errorf("files cmd = %q", got)
	}
	if h.e.Focused() != "files" {
		t.Errorf("focused = %q", h.e.Focused())
	}
	// files is now the active panel: main shows its entry once files has a row.
	h.finish("files", "x.go\ny.go\n")
	if got := h.cmd("main"); got != "cat x.go" {
		t.Errorf("main cmd = %q", got)
	}
	if got := h.e.Crumbs("commits"); !reflect.DeepEqual(got, []string{"commits", "b2\tadd", "files"}) {
		t.Errorf("crumbs = %q", got)
	}
}

func TestBackRestoresParent(t *testing.T) {
	h := loadedStack(t)
	h.move("commits", 1)
	h.doSettle()
	h.finish("main", "show b2\n")
	h.enter()
	stuck := h.inflight["files"].ID
	if !h.back() {
		t.Fatal("back reported nothing to close")
	}
	if !slices.Contains(h.cancel, stuck) {
		t.Error("leaving a level must cancel its runs")
	}
	if h.e.Focused() != "commits" || h.e.View("commits").Cursor != 1 {
		t.Errorf("focused %q cursor %d", h.e.Focused(), h.e.View("commits").Cursor)
	}
	if got := h.e.Crumbs("commits"); !reflect.DeepEqual(got, []string{"commits"}) {
		t.Errorf("crumbs = %q", got)
	}
	if h.back() {
		t.Error("nothing left to close")
	}
}

func TestReenterUsesCache(t *testing.T) {
	h := loadedStack(t)
	h.enter()
	h.finish("files", "x.go\n")
	h.finish("main", "cat x.go\n")
	h.back()
	n := len(h.ran)
	h.enter()
	if len(h.ran) != n {
		t.Errorf("re-entering the same row ran %v", h.ran[n:])
	}
	if v := h.e.View("files"); !reflect.DeepEqual(v.Lines, []string{"x.go"}) || v.Loading || v.Cursor != 0 {
		t.Errorf("files = %+v", v)
	}
}

func TestEnterNeedsTargetAndSelection(t *testing.T) {
	h := newHarness(t, stackDef)
	h.finish("tags", "")
	h.focus("tags")
	if fx := h.e.Enter(); len(fx.Runs) != 0 || h.e.Focused() != "tags" {
		t.Error("enter without a selection should do nothing")
	}
	h.focus("main")
	if fx := h.e.Enter(); len(fx.Runs) != 0 || h.e.Focused() != "main" {
		t.Error("enter on a panel without enter: should do nothing")
	}
}

func TestPopups(t *testing.T) {
	h := loadedStack(t)
	h.focus("tags")
	if _, ok := h.e.EnterTarget(); !ok {
		t.Error("tags should offer enter")
	}
	h.enter()
	if got := h.cmd("tagfiles"); got != "git show --name-only v1" {
		t.Errorf("popup list cmd = %q", got)
	}
	pv, ok := h.e.Popup()
	if !ok || pv.ID != "tagfiles" || pv.Width != 60 || pv.Height != 50 {
		t.Errorf("popup = %+v %v", pv, ok)
	}
	if h.e.Focused() != "tagfiles" {
		t.Errorf("focused = %q", h.e.Focused())
	}
	// Focus can't leave an open popup.
	h.apply(h.e.FocusNext())
	if h.e.Focused() != "tagfiles" {
		t.Errorf("tab moved focus out of a popup to %q", h.e.Focused())
	}
	h.finish("tagfiles", "a.txt\nb.txt\n")
	h.apply(h.e.Move(1))
	if h.e.View("tagfiles").Cursor != 1 {
		t.Error("j should move inside the popup")
	}
	// A popup on top of a popup: a content panel streaming for the popup's row.
	h.enter()
	if r := h.inflight["tagtail"]; !r.Stream || r.Req.Cmd != "tail -f b.txt" {
		t.Errorf("nested popup run = %+v", r)
	}
	if pv, _ := h.e.Popup(); pv.ID != "tagtail" || !pv.Full {
		t.Errorf("nested popup = %+v", pv)
	}
	stream := h.inflight["tagtail"].ID
	h.back()
	if !slices.Contains(h.cancel, stream) {
		t.Error("closing a popup must stop its stream")
	}
	if pv, _ := h.e.Popup(); pv.ID != "tagfiles" || h.e.Focused() != "tagfiles" {
		t.Errorf("after one esc: popup %+v focused %q", pv, h.e.Focused())
	}
	h.back()
	if _, ok := h.e.Popup(); ok || h.e.Focused() != "tags" {
		t.Errorf("after two esc: focused %q", h.e.Focused())
	}
}

func TestContentPopupUsesOpenersRow(t *testing.T) {
	h := loadedStack(t)
	h.enter() // commits → files (drill)
	h.finish("files", "x.go\n")
	h.finish("main", "cat x.go\n")
	h.enter() // files → diff popup
	if got := h.cmd("diff"); got != "git show a1 -- x.go" {
		t.Errorf("diff cmd = %q", got)
	}
	if h.e.Target() != "diff" {
		t.Errorf("scroll/tab target should be the popup, got %q", h.e.Target())
	}
	h.finish("diff", "+line\n")
	if v := h.e.ContentView("diff"); v.Lines[0] != "+line" {
		t.Errorf("diff view = %+v", v)
	}
	h.back()
	if h.e.Focused() != "files" {
		t.Errorf("esc from the popup should return to files, got %q", h.e.Focused())
	}
}

func TestThreeDrillLevels(t *testing.T) {
	src := `
panels:
  - {id: a, source: a, enter: b}
  - {id: b, source: "b {{a.line}}", enter: c}
  - {id: c, source: "c {{b.line}}"}
`
	h := newHarness(t, src)
	h.finish("a", "1\n")
	h.enter()
	h.finish("b", "2\n")
	h.enter()
	if got := h.cmd("c"); got != "c 2" {
		t.Errorf("c cmd = %q", got)
	}
	if got := h.e.Crumbs("a"); !reflect.DeepEqual(got, []string{"a", "1", "b", "2", "c"}) {
		t.Errorf("crumbs = %q", got)
	}
	h.back()
	h.back()
	if h.e.Focused() != "a" || h.back() {
		t.Errorf("focused %q after two esc", h.e.Focused())
	}
}
