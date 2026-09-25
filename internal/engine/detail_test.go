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

const detailDef = `
panels:
  - {id: branches, source: git branch}
  - id: commits
    source: git log {{branches.line}}
    split: "\t"
    key: .fields.0
  - {id: plain, source: ls}
detail:
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
	h := newHarness(t, detailDef)
	h.finish("plain", "x\n")
	h.finish("branches", "main\nfeature\n")
	h.finish("detail", `{"line":"main"}`)
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	h.focus("commits")
	h.finish("detail", "diff a1\n")
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
	r, ok := h.inflight["detail"]
	if !ok || !r.Stream {
		h.t.Fatalf("no stream in flight: %+v", r)
	}
	h.e.StreamData(r.ID, []byte(data))
}

func TestDetailRunsForFocusedSelection(t *testing.T) {
	h := newHarness(t, detailDef)
	if _, ok := h.inflight["detail"]; ok {
		t.Fatal("detail ran before its panel loaded")
	}
	if v := h.e.DetailView(); !v.Loading || !reflect.DeepEqual(v.Tabs, []string{"JSON"}) {
		t.Errorf("detail view before load = %+v", v)
	}
	h.finish("branches", "main\nfeature\n")
	if got := h.cmd("detail"); got != `echo '{"line":"main"}'` {
		t.Errorf("detail cmd = %q", got)
	}
	h.finish("detail", `{"line":"main"}`)
	v := h.e.DetailView()
	if !reflect.DeepEqual(v.Lines, []string{"{", `  "line": "main"`, "}"}) || v.Loading {
		t.Errorf("json not pretty-printed: %+v", v)
	}
}

func TestDetailInvalidJSONShownRaw(t *testing.T) {
	h := newHarness(t, detailDef)
	h.finish("branches", "main\n")
	h.finish("detail", "not json\n")
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Lines, []string{"not json"}) {
		t.Errorf("lines = %q", v.Lines)
	}
}

func TestDetailFollowsFocusImmediately(t *testing.T) {
	h := newHarness(t, detailDef)
	h.finish("branches", "main\n")
	stuck := h.inflight["detail"].ID
	h.finish("commits", "a1\tfix\n")
	h.focus("commits")
	if !slices.Contains(h.cancel, stuck) {
		t.Error("previous panel's detail run not cancelled on focus change")
	}
	if got := h.cmd("detail"); got != "git show a1" {
		t.Errorf("detail cmd = %q", got)
	}
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Tabs, []string{"Diff", "Stat", "Tail"}) || v.Active != 0 {
		t.Errorf("tabs = %+v", v)
	}
}

func TestOnlyVisibleTabRunsAndTabsAreCached(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	if got := h.cmd("detail"); got != "git show --stat a1" {
		t.Errorf("stat cmd = %q", got)
	}
	if v := h.e.DetailView(); v.Active != 1 {
		t.Errorf("active = %d", v.Active)
	}
	h.finish("detail", "stat a1\n")
	n := len(h.ran)
	h.apply(h.e.PrevTab())
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Lines, []string{"diff a1"}) || v.Loading {
		t.Errorf("diff tab not from cache: %+v", v)
	}
	if len(h.ran) != n {
		t.Errorf("ran %v", h.ran[n:])
	}
	h.apply(h.e.PrevTab()) // wraps to Tail
	if v := h.e.DetailView(); v.Active != 2 {
		t.Errorf("prev from 0 should wrap, active = %d", v.Active)
	}
}

func TestTabIsRememberedPerPanel(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	h.finish("detail", "stat a1\n")
	h.focus("branches")
	h.focus("commits")
	if v := h.e.DetailView(); v.Active != 1 || v.Lines[0] != "stat a1" {
		t.Errorf("tab not remembered: %+v", v)
	}
}

func TestDetailMoveUsesCacheOrWaitsForSettle(t *testing.T) {
	h := loaded(t)
	h.move("commits", 1)
	if _, ok := h.inflight["detail"]; ok {
		t.Fatal("detail ran before settle")
	}
	if h.settle == 0 {
		t.Fatal("move should request settle for the detail")
	}
	if v := h.e.DetailView(); !v.Loading || !v.Stale || v.Lines[0] != "diff a1" {
		t.Errorf("pending detail should show stale content: %+v", v)
	}
	h.doSettle()
	if got := h.cmd("detail"); got != "git show b2" {
		t.Errorf("detail cmd = %q", got)
	}
	h.finish("detail", "diff b2\n")
	n := len(h.ran)
	h.move("commits", -1)
	if v := h.e.DetailView(); v.Lines[0] != "diff a1" || v.Loading || v.Stale {
		t.Errorf("revisit not from cache: %+v", v)
	}
	h.doSettle()
	if len(h.ran) != n {
		t.Errorf("ran %v", h.ran[n:])
	}
}

func TestDetailMoveCancelsInFlight(t *testing.T) {
	h := loaded(t)
	h.move("commits", 1)
	h.doSettle()
	stuck := h.inflight["detail"].ID
	h.move("commits", -1) // back to a1: cached, so b2's run is no longer wanted
	if !slices.Contains(h.cancel, stuck) {
		t.Errorf("in-flight detail run not cancelled")
	}
	h.apply(h.e.Finished(stuck, []byte("late\n"), nil))
	if v := h.e.DetailView(); v.Lines[0] != "diff a1" {
		t.Errorf("late result applied: %+v", v)
	}
}

func TestDetailWaitsForItsPanel(t *testing.T) {
	h := newHarness(t, detailDef)
	h.finish("branches", "main\n")
	h.finish("detail", "{}")
	h.focus("commits") // commits still loading
	if _, ok := h.inflight["detail"]; ok {
		t.Fatal("detail ran while its panel was loading")
	}
	if v := h.e.DetailView(); !v.Loading {
		t.Errorf("detail should be loading: %+v", v)
	}
	h.finish("commits", "a1\tfix\n")
	if got := h.cmd("detail"); got != "git show a1" {
		t.Errorf("detail cmd = %q", got)
	}
}

func TestDetailEmptyPanel(t *testing.T) {
	h := newHarness(t, detailDef)
	h.finish("branches", "")
	if v := h.e.DetailView(); v.Empty == "" || v.Loading {
		t.Errorf("empty panel detail = %+v", v)
	}
}

func TestDetailErrorsAreNotCached(t *testing.T) {
	h := newHarness(t, detailDef)
	h.finish("branches", "main\nfeature\n")
	h.finish("detail", "{}")
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	h.focus("commits")
	h.finishErr("detail", errors.New("exit status 128: bad object"))
	if v := h.e.DetailView(); v.Err != "exit status 128: bad object" || v.Loading {
		t.Errorf("error not shown: %+v", v)
	}
	h.move("commits", 1)
	h.doSettle()
	h.finish("detail", "diff b2\n")
	h.move("commits", -1)
	h.doSettle()
	if got := h.cmd("detail"); got != "git show a1" {
		t.Errorf("errored detail should re-run, got %q", got)
	}
}

func TestPanelWithoutTabsShowsRow(t *testing.T) {
	h := loaded(t)
	h.focus("plain")
	v := h.e.DetailView()
	if len(v.Tabs) != 0 || !v.HasRow || !reflect.DeepEqual(v.Row, map[string]any{"line": "x"}) {
		t.Errorf("fallback = %+v", v)
	}
	if _, ok := h.inflight["detail"]; ok {
		t.Error("no-tab panel ran a detail command")
	}
}

func TestRefreshRerunsDetail(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.Refresh())
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	if got := h.cmd("detail"); got != "git show a1" {
		t.Errorf("refresh should re-run the detail, got %q", got)
	}
	if v := h.e.DetailView(); !v.Stale || v.Lines[0] != "diff a1" {
		t.Errorf("old detail should stay visible while refreshing: %+v", v)
	}
}

func TestOutputCleanup(t *testing.T) {
	h := loaded(t)
	h.apply(h.e.NextTab())
	h.finish("detail", "a\tb\r\nprogress 10%\rprogress 100%\n\x1b[31mred\x1b[0m\n")
	want := []string{"a    b", "progress 100%", "\x1b[31mred\x1b[0m"}
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Lines, want) {
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
	r := h.inflight["detail"]
	if !r.Stream || r.Req.Cmd != "tail -f a1" || r.Req.Timeout != 0 {
		t.Errorf("stream run = %+v", r)
	}
	if v := h.e.DetailView(); !v.Live || v.Loading {
		t.Errorf("stream view = %+v", v)
	}
}

func TestStreamAppendsAndJoinsPartialLines(t *testing.T) {
	h := openTail(t)
	h.stream("one\ntw")
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Lines, []string{"one", "tw"}) {
		t.Errorf("lines = %q", v.Lines)
	}
	h.stream("o\nthree\n")
	if v := h.e.DetailView(); !reflect.DeepEqual(v.Lines, []string{"one", "two", "three"}) {
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
	v := h.e.DetailView()
	if len(v.Lines) != maxStreamLines || v.Lines[len(v.Lines)-1] != fmt.Sprintf("line %d", maxStreamLines+4) {
		t.Errorf("got %d lines, last %q", len(v.Lines), v.Lines[len(v.Lines)-1])
	}
}

func TestStreamRestartsOnMoveAndIsNeverCached(t *testing.T) {
	h := openTail(t)
	first := h.inflight["detail"].ID
	h.stream("a1 log\n")
	h.move("commits", 1)
	if !slices.Contains(h.cancel, first) {
		t.Fatal("stream not killed on selection change")
	}
	if v := h.e.DetailView(); !v.Stale || v.Live {
		t.Errorf("old stream output should be stale: %+v", v)
	}
	h.doSettle()
	if got := h.cmd("detail"); got != "tail -f b2" {
		t.Errorf("new stream = %q", got)
	}
	if v := h.e.DetailView(); len(v.Lines) != 0 || v.Stale {
		t.Errorf("new stream should start empty: %+v", v)
	}
	h.move("commits", -1)
	h.doSettle()
	if got := h.cmd("detail"); got != "tail -f a1" {
		t.Errorf("revisit should restart the stream, got %q", got)
	}
}

func TestStreamEnd(t *testing.T) {
	h := openTail(t)
	h.stream("last\n")
	h.finish("detail", "")
	if v := h.e.DetailView(); !v.Ended || v.Live || v.Err != "" || v.Lines[0] != "last" {
		t.Errorf("ended stream = %+v", v)
	}
	h2 := openTail(t)
	h2.finishErr("detail", errors.New("exit status 1"))
	if v := h2.e.DetailView(); !v.Ended || v.Err != "exit status 1" {
		t.Errorf("failed stream = %+v", v)
	}
	h3 := openTail(t)
	h3.finishErr("detail", context.Canceled)
	if v := h3.e.DetailView(); v.Err != "" {
		t.Errorf("cancellation is not an error: %+v", v)
	}
}

func TestStreamDataForOldStreamIgnored(t *testing.T) {
	h := openTail(t)
	first := h.inflight["detail"].ID
	h.move("commits", 1)
	h.doSettle()
	h.e.StreamData(first, []byte("stale\n"))
	if v := h.e.DetailView(); len(v.Lines) != 0 {
		t.Errorf("old stream data applied: %q", v.Lines)
	}
}

func TestDetailIDChangesWithContent(t *testing.T) {
	h := loaded(t)
	id := h.e.DetailView().ID
	h.move("commits", 1)
	if h.e.DetailView().ID != id {
		t.Error("ID changed while old content still shown")
	}
	h.doSettle()
	h.finish("detail", "diff b2\n")
	if h.e.DetailView().ID == id {
		t.Error("ID unchanged after new content")
	}
}
