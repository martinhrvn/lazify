package engine

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

const chainDef = `
panels:
  - {id: branches, source: git branch}
  - id: commits
    source: git log {{branches.line}}
    split: "\t"
    key: .fields.0
  - {id: files, source: "git show {{commits.fields.0}}"}
`

// harness tracks in-flight runs by panel so tests can finish them by name. Content
// panels' runs are tracked under their own ids, like list panels.
type harness struct {
	t        *testing.T
	e        *Engine
	inflight map[string]Run
	cancel   []uint64
	settle   uint64
	ran      []string // commands, in order
}

func newHarness(t *testing.T, src string) *harness {
	h := &harness{t: t, e: New(mustDef(t, src), nil), inflight: map[string]Run{}}
	h.apply(h.e.Start())
	return h
}

func (h *harness) apply(fx Effects) {
	for _, id := range fx.Cancel {
		h.cancel = append(h.cancel, id)
		for p, r := range h.inflight {
			if r.ID == id {
				delete(h.inflight, p)
			}
		}
	}
	for _, r := range fx.Runs {
		slot := r.Panel
		if r.Mark {
			slot += ":mark"
		}
		if old, ok := h.inflight[slot]; ok {
			h.t.Fatalf("%s started run %q while %q still in flight and not cancelled", slot, r.Req.Cmd, old.Req.Cmd)
		}
		h.inflight[slot] = r
		h.ran = append(h.ran, r.Req.Cmd)
	}
	if fx.Settle != 0 {
		h.settle = fx.Settle
	}
}

// finish completes the in-flight run of panel with output out.
func (h *harness) finish(panel, out string) {
	h.t.Helper()
	r, ok := h.inflight[panel]
	if !ok {
		h.t.Fatalf("no run in flight for %s (in flight: %v)", panel, h.inflight)
	}
	delete(h.inflight, panel)
	h.apply(h.e.Finished(r.ID, []byte(out), nil))
}

func (h *harness) cmd(panel string) string {
	h.t.Helper()
	r, ok := h.inflight[panel]
	if !ok {
		h.t.Fatalf("no run in flight for %s", panel)
	}
	return r.Req.Cmd
}

func (h *harness) move(panel string, delta int) {
	h.apply(h.e.FocusPanel(panel))
	h.apply(h.e.Move(delta))
}

func (h *harness) doSettle() { h.apply(h.e.Settle(h.settle)) }

func TestDependentRunsWhenParentLoads(t *testing.T) {
	h := newHarness(t, chainDef)
	if _, ok := h.inflight["commits"]; ok {
		t.Fatal("commits ran before branches had a selection")
	}
	h.finish("branches", "main\nfeature\n")
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("commits cmd = %q", got)
	}
	h.finish("commits", "a1\tfix\nb2\tadd\n")
	if got := h.cmd("files"); got != "git show a1" {
		t.Errorf("files cmd = %q", got)
	}
}

func TestMoveIsDebounced(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\n")
	h.finish("commits", "a1\tfix\n")
	h.finish("files", "x.go\n")

	h.move("branches", 1)
	if len(h.inflight) != 0 {
		t.Fatalf("move ran immediately: %v", h.inflight)
	}
	if h.settle == 0 {
		t.Fatal("move on a panel with dependents should ask to settle")
	}
	if v := h.e.View("commits"); !v.Stale || !v.Loading || len(v.Lines) != 1 {
		t.Errorf("commits should show old rows as stale while pending: %+v", v)
	}
	if v := h.e.View("files"); !v.Stale || !v.Loading {
		t.Errorf("files should be stale too: %+v", v)
	}

	h.doSettle()
	if got := h.cmd("commits"); got != "git log feature" {
		t.Errorf("commits cmd = %q", got)
	}
	if _, ok := h.inflight["files"]; ok {
		t.Error("files must wait for commits to finish")
	}
	h.finish("commits", "c3\tother\n")
	if got := h.cmd("files"); got != "git show c3" {
		t.Errorf("files cmd = %q", got)
	}
}

func TestOnlyLatestSettleCounts(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\nfix\n")
	h.finish("commits", "a1\tx\n")
	h.finish("files", "f\n")

	h.move("branches", 1)
	first := h.settle
	h.move("branches", 1)
	h.apply(h.e.Settle(first))
	if len(h.inflight) != 0 {
		t.Fatalf("superseded settle ran: %v", h.inflight)
	}
	h.doSettle()
	if got := h.cmd("commits"); got != "git log fix" {
		t.Errorf("commits cmd = %q", got)
	}
}

func TestMoveWithoutDependentsNeedsNoSettle(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\n")
	h.finish("commits", "a1\tx\n")
	h.finish("files", "f1\nf2\n")
	h.move("files", 1)
	if h.settle != 0 {
		t.Errorf("leaf move asked to settle")
	}
	h.move("files", 1) // already at the end: no change
	h.move("branches", 5)
	if h.settle != 0 {
		t.Errorf("move that didn't change the cursor asked to settle")
	}
}

func TestMoveCancelsInFlightDependents(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\n")
	stuck := h.inflight["commits"].ID
	h.move("branches", 1)
	if !slices.Contains(h.cancel, stuck) {
		t.Errorf("in-flight commits run %d not cancelled: %v", stuck, h.cancel)
	}
	// A late result for the cancelled run is ignored.
	h.apply(h.e.Finished(stuck, []byte("zz\tlate\n"), nil))
	if v := h.e.View("commits"); len(v.Lines) != 0 {
		t.Errorf("cancelled result applied: %+v", v)
	}
}

func TestCacheServesRevisitsInstantly(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\n")
	h.finish("commits", "a1\tmain-commit\n")
	h.finish("files", "m.go\n")
	h.move("branches", 1)
	h.doSettle()
	h.finish("commits", "b2\tfeature-commit\n")
	h.finish("files", "f.go\n")
	ran := len(h.ran)

	h.move("branches", -1) // back to main: everything cached
	if v := h.e.View("commits"); v.Stale || v.Loading || v.Lines[0] != "a1\tmain-commit" {
		t.Errorf("commits not served from cache: %+v", v)
	}
	if v := h.e.View("files"); v.Stale || v.Loading || v.Lines[0] != "m.go" {
		t.Errorf("files not served from cache: %+v", v)
	}
	h.doSettle()
	if len(h.ran) != ran {
		t.Errorf("cached revisit ran commands: %v", h.ran[ran:])
	}
}

func TestRefreshBypassesCacheAndUpdatesIt(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\n")
	h.finish("commits", "a1\tx\n")
	h.finish("files", "f\n")

	h.e.FocusPanel("commits")
	h.apply(h.e.Refresh())
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("refresh cmd = %q", got)
	}
	if v := h.e.View("commits"); !v.Stale || !v.Loading {
		t.Errorf("refreshing panel should be stale+loading: %+v", v)
	}
	h.finish("commits", "a0\tnew\na1\tx\n")
	// Cursor stays on a1 (key), so files' command is unchanged: no re-run.
	if _, ok := h.inflight["files"]; ok {
		t.Error("files re-ran although commits' selection did not change")
	}
	if v := h.e.View("commits"); v.Cursor != 1 {
		t.Errorf("cursor = %d, want 1", v.Cursor)
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\n")
	r := h.inflight["commits"]
	delete(h.inflight, "commits")
	h.apply(h.e.Finished(r.ID, nil, errors.New("boom")))
	h.move("branches", 1)
	h.doSettle()
	h.finish("commits", "b2\tx\n")
	h.finish("files", "f\n")
	h.move("branches", -1)
	h.doSettle()
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("errored command should re-run, in flight: %q", got)
	}
}

func TestEmptyParentBlocksAndClearsDependents(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\n")
	h.finish("commits", "a1\tx\n")
	h.finish("files", "f\n")

	h.e.FocusPanel("branches")
	h.apply(h.e.Refresh())
	h.finish("branches", "")
	for _, p := range []string{"commits", "files"} {
		v := h.e.View(p)
		if v.Blocked == "" || len(v.Lines) != 0 || v.Loading {
			t.Errorf("%s should be blocked and empty: %+v", p, v)
		}
	}
	if v := h.e.View("commits"); v.Blocked != "no selection in branches" {
		t.Errorf("commits blocked = %q", v.Blocked)
	}
}

func TestUnchangedCommandDoesNotRerun(t *testing.T) {
	src := `
panels:
  - {id: svc, source: x, rows: ".[]"}
  - {id: logs, source: "logs {{svc.group}}"}
`
	h := newHarness(t, src)
	h.finish("svc", `[{"name":"a","group":"g"},{"name":"b","group":"g"}]`)
	h.finish("logs", "l1\n")
	h.move("svc", 1)
	h.doSettle()
	if len(h.inflight) != 0 {
		t.Errorf("same rendered command re-ran: %v", h.inflight)
	}
	if v := h.e.View("logs"); v.Stale || v.Loading {
		t.Errorf("logs should stay fresh: %+v", v)
	}
}

func TestDrillInChildDoesNotRunAtTopLevel(t *testing.T) {
	src := `
panels:
  - {id: commits, source: git log, children: files}
  - {id: files, source: "git show {{commits.line}}"}
`
	h := newHarness(t, src)
	h.finish("commits", "a1\n")
	if _, ok := h.inflight["files"]; ok {
		t.Error("drill-in child ran without being opened")
	}
}

func TestDiamondRunsOnce(t *testing.T) {
	src := `
panels:
  - {id: a, source: a}
  - {id: b, source: "b {{a.line}}"}
  - {id: c, source: "c {{a.line}}"}
  - {id: d, source: "d {{b.line}} {{c.line}}"}
`
	h := newHarness(t, src)
	h.finish("a", "1\n")
	h.finish("b", "b1\n")
	if _, ok := h.inflight["d"]; ok {
		t.Fatal("d ran before c finished")
	}
	h.finish("c", "c1\n")
	if got := h.cmd("d"); got != "d b1 c1" {
		t.Errorf("d cmd = %q", got)
	}
	if n := countPrefix(h.ran, "d "); n != 1 {
		t.Errorf("d ran %d times", n)
	}
}

func TestSelectionOfDependentsFollowsKeyAcrossParents(t *testing.T) {
	h := newHarness(t, chainDef)
	h.finish("branches", "main\nfeature\n")
	h.finish("commits", "a1\tx\nb2\ty\n")
	h.finish("files", "f\n")
	h.move("commits", 1) // b2
	h.doSettle()
	h.finish("files", "g\n")
	h.move("branches", 1)
	h.doSettle()
	h.finish("commits", "c3\tz\nb2\ty\n") // b2 is also on feature
	if v := h.e.View("commits"); v.Cursor != 1 {
		t.Errorf("cursor = %d, want 1 (b2 kept)", v.Cursor)
	}
	// files already shows b2's rows, so it is up to date without running.
	if _, ok := h.inflight["files"]; ok {
		t.Errorf("files re-ran for an unchanged selection")
	}
	if v := h.e.View("files"); !reflect.DeepEqual(v.Lines, []string{"g"}) || v.Stale || v.Loading {
		t.Errorf("files = %+v, want fresh [g]", v)
	}
}

func countPrefix(ss []string, p string) int {
	n := 0
	for _, s := range ss {
		if len(s) >= len(p) && s[:len(p)] == p {
			n++
		}
	}
	return n
}
