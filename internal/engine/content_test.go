package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

const contentDef = `
panels:
  - {id: branches, source: git branch}
  - id: commits
    source: git log {{branches.line}}
    split: "\t"
    key: .fields.0
  - {id: plain, source: ls}
  - id: main
    content:
      branches:
        tabs:
          - {name: JSON, cmd: "echo {{.}}", format: json}
      commits:
        tabs:
          - {name: Diff, cmd: "git show {{.fields.0}}"}
          - {name: Stat, cmd: "git show --stat {{.fields.0}}"}
          - {name: Tail, cmd: "tail -f {{.fields.0}}", mode: stream}
`

// loaded returns a harness with branches and commits loaded and commits focused,
// with its Diff tab for a1 finished.
func loaded(t *testing.T) *harness {
	h := newHarness(t, contentDef)
	h.finish("plain", "x\n")
	h.finish("branches", "main\nfeature\n")
	h.finish("main", `{"line":"main"}`)
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	h.focus("commits")
	h.finish("main", "diff a1\n")
	return h
}

func (h *harness) focus(panel string) { h.apply(h.e.FocusPanel(panel)) }

func (h *harness) finishErr(slot string, err error) {
	h.t.Helper()
	r, ok := h.inflight[slot]
	if !ok {
		h.t.Fatalf("no run in flight for %s", slot)
	}
	delete(h.inflight, slot)
	h.apply(h.e.Finished(r.ID, nil, err))
}

func (h *harness) stream(data string) {
	h.t.Helper()
	r, ok := h.inflight["main"]
	if !ok || !r.Stream {
		h.t.Fatalf("no stream in flight: %+v", r)
	}
	h.e.StreamData(r.ID, []byte(data))
}

func TestContentRunsForFocusedSelection(t *testing.T) {
	h := newHarness(t, contentDef)
	if _, ok := h.inflight["main"]; ok {
		t.Fatal("detail ran before its panel loaded")
	}
	if v := h.e.ContentView("main"); !v.Loading || !reflect.DeepEqual(v.Tabs, []string{"JSON"}) {
		t.Errorf("detail view before load = %+v", v)
	}
	h.finish("branches", "main\nfeature\n")
	if got := h.cmd("main"); got != `echo '{"line":"main"}'` {
		t.Errorf("detail cmd = %q", got)
	}
	h.finish("main", `{"line":"main"}`)
	v := h.e.ContentView("main")
	if !reflect.DeepEqual(v.Lines, []string{"{", `  "line": "main"`, "}"}) || v.Loading {
		t.Errorf("json not pretty-printed: %+v", v)
	}
}

func TestContentInvalidJSONShownRaw(t *testing.T) {
	h := newHarness(t, contentDef)
	h.finish("branches", "main\n")
	h.finish("main", "not json\n")
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Lines, []string{"not json"}) {
		t.Errorf("lines = %q", v.Lines)
	}
}

func TestContentFollowsFocusImmediately(t *testing.T) {
	h := newHarness(t, contentDef)
	h.finish("branches", "main\n")
	stuck := h.inflight["main"].ID
	h.finish("commits", "a1\tfix\n")
	h.focus("commits")
	if !slices.Contains(h.cancel, stuck) {
		t.Error("previous panel's detail run not cancelled on focus change")
	}
	if got := h.cmd("main"); got != "git show a1" {
		t.Errorf("detail cmd = %q", got)
	}
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Tabs, []string{"Diff", "Stat", "Tail"}) || v.Active != 0 {
		t.Errorf("tabs = %+v", v)
	}
}

func TestOnlyVisibleTabRunsAndTabsAreCached(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	if got := h.cmd("main"); got != "git show --stat a1" {
		t.Errorf("stat cmd = %q", got)
	}
	if v := h.e.ContentView("main"); v.Active != 1 {
		t.Errorf("active = %d", v.Active)
	}
	h.finish("main", "stat a1\n")
	n := len(h.ran)
	h.apply(h.e.PrevTab())
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Lines, []string{"diff a1"}) || v.Loading {
		t.Errorf("diff tab not from cache: %+v", v)
	}
	if len(h.ran) != n {
		t.Errorf("ran %v", h.ran[n:])
	}
	h.apply(h.e.PrevTab()) // wraps to Tail
	if v := h.e.ContentView("main"); v.Active != 2 {
		t.Errorf("prev from 0 should wrap, active = %d", v.Active)
	}
}

func TestTabIsRememberedPerPanel(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	h.finish("main", "stat a1\n")
	h.focus("branches")
	h.focus("commits")
	if v := h.e.ContentView("main"); v.Active != 1 || v.Lines[0] != "stat a1" {
		t.Errorf("tab not remembered: %+v", v)
	}
}

func TestContentMoveUsesCacheOrWaitsForSettle(t *testing.T) {
	h := loaded(t)
	h.move("commits", 1)
	if _, ok := h.inflight["main"]; ok {
		t.Fatal("detail ran before settle")
	}
	if h.settle == 0 {
		t.Fatal("move should request settle for the detail")
	}
	if v := h.e.ContentView("main"); !v.Loading || !v.Stale || v.Lines[0] != "diff a1" {
		t.Errorf("pending detail should show stale content: %+v", v)
	}
	h.doSettle()
	if got := h.cmd("main"); got != "git show b2" {
		t.Errorf("detail cmd = %q", got)
	}
	h.finish("main", "diff b2\n")
	n := len(h.ran)
	h.move("commits", -1)
	if v := h.e.ContentView("main"); v.Lines[0] != "diff a1" || v.Loading || v.Stale {
		t.Errorf("revisit not from cache: %+v", v)
	}
	h.doSettle()
	if len(h.ran) != n {
		t.Errorf("ran %v", h.ran[n:])
	}
}

func TestContentMoveCancelsInFlight(t *testing.T) {
	h := loaded(t)
	h.move("commits", 1)
	h.doSettle()
	stuck := h.inflight["main"].ID
	h.move("commits", -1) // back to a1: cached, so b2's run is no longer wanted
	if !slices.Contains(h.cancel, stuck) {
		t.Errorf("in-flight detail run not cancelled")
	}
	h.apply(h.e.Finished(stuck, []byte("late\n"), nil))
	if v := h.e.ContentView("main"); v.Lines[0] != "diff a1" {
		t.Errorf("late result applied: %+v", v)
	}
}

func TestContentWaitsForItsPanel(t *testing.T) {
	h := newHarness(t, contentDef)
	h.finish("branches", "main\n")
	h.finish("main", "{}")
	h.focus("commits") // commits still loading
	if _, ok := h.inflight["main"]; ok {
		t.Fatal("detail ran while its panel was loading")
	}
	if v := h.e.ContentView("main"); !v.Loading {
		t.Errorf("detail should be loading: %+v", v)
	}
	h.finish("commits", "a1\tfix\n")
	if got := h.cmd("main"); got != "git show a1" {
		t.Errorf("detail cmd = %q", got)
	}
}

func TestContentEmptyPanel(t *testing.T) {
	h := newHarness(t, contentDef)
	h.finish("branches", "")
	if v := h.e.ContentView("main"); v.Empty == "" || v.Loading {
		t.Errorf("empty panel detail = %+v", v)
	}
}

func TestContentErrorsAreNotCached(t *testing.T) {
	h := newHarness(t, contentDef)
	h.finish("branches", "main\nfeature\n")
	h.finish("main", "{}")
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	h.focus("commits")
	h.finishErr("main", errors.New("exit status 128: bad object"))
	if v := h.e.ContentView("main"); v.Err != "exit status 128: bad object" || v.Loading {
		t.Errorf("error not shown: %+v", v)
	}
	h.move("commits", 1)
	h.doSettle()
	h.finish("main", "diff b2\n")
	h.move("commits", -1)
	h.doSettle()
	if got := h.cmd("main"); got != "git show a1" {
		t.Errorf("errored detail should re-run, got %q", got)
	}
}

func TestPanelWithoutEntryShowsNothing(t *testing.T) {
	h := loaded(t)
	h.focus("plain")
	v := h.e.ContentView("main")
	if len(v.Tabs) != 0 || len(v.Lines) != 0 || v.Empty != "nothing for plain" {
		t.Errorf("view = %+v", v)
	}
	if _, ok := h.inflight["main"]; ok {
		t.Error("no-tab panel ran a detail command")
	}
}

func TestRefreshRerunsContent(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.Refresh())
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	if got := h.cmd("main"); got != "git show a1" {
		t.Errorf("refresh should re-run the detail, got %q", got)
	}
	if v := h.e.ContentView("main"); !v.Stale || v.Lines[0] != "diff a1" {
		t.Errorf("old detail should stay visible while refreshing: %+v", v)
	}
}

func TestOutputCleanup(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	h.finish("main", "a\tb\r\nprogress 10%\rprogress 100%\n\x1b[31mred\x1b[0m\n")
	want := []string{"a    b", "progress 100%", "\x1b[31mred\x1b[0m"}
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Lines, want) {
		t.Errorf("lines = %q, want %q", v.Lines, want)
	}
}

func openTail(t *testing.T) *harness {
	h := loaded(t)
	h.apply(h.e.PrevTab()) // Diff → Tail (wraps)
	return h
}

func TestStreamRunsWithoutTimeout(t *testing.T) {
	h := openTail(t)
	r := h.inflight["main"]
	if !r.Stream || r.Req.Cmd != "tail -f a1" || r.Req.Timeout != 0 {
		t.Errorf("stream run = %+v", r)
	}
	if v := h.e.ContentView("main"); !v.Live || v.Loading {
		t.Errorf("stream view = %+v", v)
	}
}

func TestStreamAppendsAndJoinsPartialLines(t *testing.T) {
	h := openTail(t)
	h.stream("one\ntw")
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Lines, []string{"one", "tw"}) {
		t.Errorf("lines = %q", v.Lines)
	}
	h.stream("o\nthree\n")
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Lines, []string{"one", "two", "three"}) {
		t.Errorf("lines = %q", v.Lines)
	}
}

func TestStreamCapsLines(t *testing.T) {
	h := openTail(t)
	var b strings.Builder
	for i := range maxStreamLines + 5 {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	h.stream(b.String())
	v := h.e.ContentView("main")
	if len(v.Lines) != maxStreamLines || v.Lines[len(v.Lines)-1] != fmt.Sprintf("line %d", maxStreamLines+4) {
		t.Errorf("got %d lines, last %q", len(v.Lines), v.Lines[len(v.Lines)-1])
	}
}

func TestStreamRestartsOnMoveAndIsNeverCached(t *testing.T) {
	h := openTail(t)
	first := h.inflight["main"].ID
	h.stream("a1 log\n")
	h.move("commits", 1)
	if !slices.Contains(h.cancel, first) {
		t.Fatal("stream not killed on selection change")
	}
	if v := h.e.ContentView("main"); !v.Stale || v.Live {
		t.Errorf("old stream output should be stale: %+v", v)
	}
	h.doSettle()
	if got := h.cmd("main"); got != "tail -f b2" {
		t.Errorf("new stream = %q", got)
	}
	if v := h.e.ContentView("main"); len(v.Lines) != 0 || v.Stale {
		t.Errorf("new stream should start empty: %+v", v)
	}
	h.move("commits", -1)
	h.doSettle()
	if got := h.cmd("main"); got != "tail -f a1" {
		t.Errorf("revisit should restart the stream, got %q", got)
	}
}

func TestStreamEnd(t *testing.T) {
	h := openTail(t)
	h.stream("last\n")
	h.finish("main", "")
	if v := h.e.ContentView("main"); !v.Ended || v.Live || v.Err != "" || v.Lines[0] != "last" {
		t.Errorf("ended stream = %+v", v)
	}
	h2 := openTail(t)
	h2.finishErr("main", errors.New("exit status 1"))
	if v := h2.e.ContentView("main"); !v.Ended || v.Err != "exit status 1" {
		t.Errorf("failed stream = %+v", v)
	}
	h3 := openTail(t)
	h3.finishErr("main", context.Canceled)
	if v := h3.e.ContentView("main"); v.Err != "" {
		t.Errorf("cancellation is not an error: %+v", v)
	}
}

func TestStreamDataForOldStreamIgnored(t *testing.T) {
	h := openTail(t)
	first := h.inflight["main"].ID
	h.move("commits", 1)
	h.doSettle()
	h.e.StreamData(first, []byte("stale\n"))
	if v := h.e.ContentView("main"); len(v.Lines) != 0 {
		t.Errorf("old stream data applied: %q", v.Lines)
	}
}

func TestContentIDChangesWithContent(t *testing.T) {
	h := loaded(t)
	id := h.e.ContentView("main").ID
	h.move("commits", 1)
	if h.e.ContentView("main").ID != id {
		t.Error("ID changed while old content still shown")
	}
	h.doSettle()
	h.finish("main", "diff b2\n")
	if h.e.ContentView("main").ID == id {
		t.Error("ID unchanged after new content")
	}
}

const twoViewsDef = `
panels:
  - {id: status, source: git status}
  - {id: branches, source: git branch}
  - id: main
    content:
      branches:
        tabs:
          - {name: Log, cmd: "git log {{.line}}"}
          - {name: Graph, cmd: "git log --graph {{.line}}"}
      default:
        tabs: [{name: Row, cmd: "echo {{.}}"}]
  - id: log
    side: right
    content:
      default:
        tabs: [{name: Tail, cmd: tail -F app.log, mode: stream}]
`

func TestTwoContentPanelsRunIndependently(t *testing.T) {
	h := newHarness(t, twoViewsDef)
	// log's default needs no row, so it streams straight away.
	if r := h.inflight["log"]; !r.Stream || r.Req.Cmd != "tail -F app.log" {
		t.Fatalf("log run = %+v", r)
	}
	h.finish("status", "## main\n")
	if got := h.cmd("main"); got != `echo '{"line":"## main"}'` {
		t.Errorf("main default cmd = %q", got)
	}
	h.finish("branches", "main\nfeature\n")
	h.focus("branches")
	if got := h.cmd("main"); got != "git log main" {
		t.Errorf("main cmd = %q", got)
	}
	if _, ok := h.inflight["log"]; !ok {
		t.Error("focus change must not disturb the log stream")
	}
	h.e.StreamData(h.inflight["log"].ID, []byte("hello\n"))
	if v := h.e.ContentView("log"); !v.Live || v.Lines[0] != "hello" {
		t.Errorf("log view = %+v", v)
	}
	if v := h.e.ContentView("main"); len(v.Lines) != 0 {
		t.Errorf("stream data leaked into main: %+v", v)
	}
}

func TestDefaultNeedsNoSelectionUnlessItUsesTheRow(t *testing.T) {
	src := `
panels:
  - {id: empty, source: "true"}
  - id: main
    content:
      default:
        tabs: [{name: Status, cmd: git status}]
`
	h := newHarness(t, src)
	h.finish("empty", "")
	if got := h.cmd("main"); got != "git status" {
		t.Errorf("default without row refs should run, got %q", got)
	}
	h2 := newHarness(t, twoViewsDef)
	h2.finish("status", "")
	if v := h2.e.ContentView("main"); v.Empty != "no selection in status" {
		t.Errorf("default using {{.}} with no row: %+v", v)
	}
}

func TestFocusingContentPanelKeepsActive(t *testing.T) {
	h := newHarness(t, twoViewsDef)
	h.finish("status", "## main\n")
	h.finish("main", "row\n")
	h.finish("branches", "main\n")
	h.focus("branches")
	h.finish("main", "log main\n")
	n := len(h.ran)
	h.focus("main")
	if v := h.e.ContentView("main"); v.Lines[0] != "log main" {
		t.Errorf("main should keep showing branches' content: %+v", v)
	}
	if len(h.ran) != n {
		t.Errorf("focusing a content panel ran %v", h.ran[n:])
	}
	if fx := h.e.Move(1); len(fx.Runs) != 0 || fx.Settle != 0 {
		t.Errorf("j on a content panel should do nothing in the engine: %+v", fx)
	}
}

func TestTargetAndTabMemoryPerEntry(t *testing.T) {
	h := newHarness(t, twoViewsDef)
	if got := h.e.Target(); got != "main" {
		t.Errorf("target with a list panel focused = %q, want first content panel", got)
	}
	h.finish("status", "s\n")
	h.finish("main", "row\n")
	h.finish("branches", "main\n")
	h.focus("branches")
	h.apply(h.e.NextTab()) // Graph for branches
	if got := h.cmd("main"); got != "git log --graph main" {
		t.Errorf("cmd = %q", got)
	}
	h.finish("main", "graph\n")
	h.focus("status") // default entry: its own tab index
	if v := h.e.ContentView("main"); v.Active != 0 || v.Tabs[0] != "Row" {
		t.Errorf("status entry = %+v", v)
	}
	h.focus("branches")
	if v := h.e.ContentView("main"); v.Active != 1 {
		t.Errorf("branches entry should remember Graph: %+v", v)
	}
	h.focus("log")
	if got := h.e.Target(); got != "log" {
		t.Errorf("target with log focused = %q", got)
	}
}

func TestRefreshOnContentPanelRerunsOnlyIt(t *testing.T) {
	h := newHarness(t, twoViewsDef)
	h.finish("status", "s\n")
	h.finish("main", "row\n")
	h.focus("main")
	n := len(h.ran)
	h.apply(h.e.Refresh())
	if got := h.ran[n:]; len(got) != 1 || got[0] != `echo '{"line":"s"}'` {
		t.Errorf("refresh on main ran %v", got)
	}
}

func TestNoContentPanels(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\n")
	if h.e.Target() != "" {
		t.Errorf("target = %q", h.e.Target())
	}
	if fx := h.e.NextTab(); len(fx.Runs) != 0 {
		t.Errorf("tab switch without content panels ran %v", fx.Runs)
	}
}

func TestTopLevelOrderIncludesCenter(t *testing.T) {
	src := `
panels:
  - {id: r, source: x, side: right}
  - id: main
    content: {default: {tabs: [{name: n, cmd: c}]}}
  - {id: l, source: x}
  - {id: c2, source: x, side: center}
`
	e := New(mustDef(t, src), nil)
	if got := e.TopLevel(); !reflect.DeepEqual(got, []string{"l", "main", "c2", "r"}) {
		t.Errorf("top level = %v", got)
	}
}
