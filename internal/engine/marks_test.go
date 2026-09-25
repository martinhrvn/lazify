package engine

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

const marksDef = `
panels:
  - id: branches
    source: git branch
    mark: {source: git branch --show-current}
    actions: [{key: space, desc: Checkout, cmd: "git checkout {{.line}}", refresh: [branches]}]
  - {id: commits, source: "git log {{branches.line}}"}
`

func TestPathMarkTruthiness(t *testing.T) {
	src := `
panels:
  - {id: heads, source: x, split: "\t", mark: .fields.0}
  - {id: js, source: y, rows: ".[]", mark: .c}
`
	h := newHarness(t, src)
	h.finish("heads", "*\tmain\n \tfeature\n\tx\n")
	if got := h.e.View("heads").Marked; !reflect.DeepEqual(got, []bool{true, false, false}) {
		t.Errorf("heads marked = %v", got)
	}
	h.finish("js", `[{"c":true},{"c":false},{"c":0},{"c":1},{"c":"yes"},{"c":null},{}]`)
	if got := h.e.View("js").Marked; !reflect.DeepEqual(got, []bool{true, false, false, true, true, false, false}) {
		t.Errorf("js marked = %v", got)
	}
	if _, ok := h.inflight["heads:mark"]; ok {
		t.Error("path marks need no command")
	}
}

func TestCommandMarkRunsWithPanelAndJumpsOnce(t *testing.T) {
	h := newHarness(t, marksDef)
	if got := h.cmd("branches:mark"); got != "git branch --show-current" {
		t.Fatalf("mark cmd = %q", got)
	}
	h.finish("branches", "feature\nmain\n")
	early := h.inflight["commits"]
	if early.Req.Cmd != "git log feature" {
		t.Fatalf("commits = %q", early.Req.Cmd)
	}
	h.finish("branches:mark", "main\n")
	v := h.e.View("branches")
	if !reflect.DeepEqual(v.Marked, []bool{false, true}) {
		t.Errorf("marked = %v", v.Marked)
	}
	// The cursor starts on the current branch, and commits follows it.
	if v.Cursor != 1 {
		t.Errorf("cursor = %d, want 1 (main)", v.Cursor)
	}
	if !slices.Contains(h.cancel, early.ID) {
		t.Error("commits for the first row should be cancelled after the jump")
	}
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("commits = %q", got)
	}
}

func TestMarksBeforeRowsAlsoJump(t *testing.T) {
	h := newHarness(t, marksDef)
	h.finish("branches:mark", "main\n")
	h.finish("branches", "feature\nmain\n")
	if v := h.e.View("branches"); v.Cursor != 1 {
		t.Errorf("cursor = %d", v.Cursor)
	}
	if got := h.cmd("commits"); got != "git log main" {
		t.Errorf("commits = %q", got)
	}
}

func TestNoJumpAfterUserMoved(t *testing.T) {
	h := newHarness(t, marksDef)
	h.finish("branches", "a\nb\nmain\n")
	h.move("branches", 1)
	h.finish("branches:mark", "main\n")
	if v := h.e.View("branches"); v.Cursor != 1 {
		t.Errorf("cursor = %d, want to stay on b", v.Cursor)
	}
}

func TestMarkRerunsOnRefreshAndAfterActions(t *testing.T) {
	h := newHarness(t, marksDef)
	h.finish("branches", "feature\nmain\n")
	h.finish("branches:mark", "main\n")
	h.finish("commits", "a1\n")
	h.apply(h.e.Refresh())
	if got := h.cmd("branches:mark"); got != "git branch --show-current" {
		t.Errorf("refresh should re-run the mark, got %q", got)
	}
	h.finish("branches", "feature\nmain\n")
	h.finish("branches:mark", "main\n")

	h.move("branches", -1) // feature
	h.doSettle()
	h.finish("commits", "b2\n")
	h.apply(h.e.ActionDone(h.action("space")))
	h.finish("branches", "feature\nmain\n")
	h.finish("branches:mark", "feature\n")
	v := h.e.View("branches")
	if !reflect.DeepEqual(v.Marked, []bool{true, false}) || v.Cursor != 0 {
		t.Errorf("after checkout: marked %v cursor %d", v.Marked, v.Cursor)
	}
}

func TestMarkFailureOnlyDropsMarks(t *testing.T) {
	h := newHarness(t, marksDef)
	h.finish("branches", "feature\nmain\n")
	h.finishErr("branches:mark", errors.New("exit status 128: not a repo"))
	v := h.e.View("branches")
	if slices.Contains(v.Marked, true) || v.MarkErr == "" || v.Err != "" || len(v.Lines) != 2 {
		t.Errorf("view = %+v", v)
	}
}

func TestMarkMatch(t *testing.T) {
	src := `
panels:
  - {id: byKey, source: x, split: "\t", key: .fields.0, mark: {source: echo a1}}
  - {id: byMatch, source: y, split: "\t", mark: {source: echo fix, match: "{{.fields.1}}"}}
`
	h := newHarness(t, src)
	h.finish("byKey", "b2\tadd\na1\tfix\n")
	h.finish("byKey:mark", "a1\n")
	if got := h.e.View("byKey").Marked; !reflect.DeepEqual(got, []bool{false, true}) {
		t.Errorf("by key = %v", got)
	}
	h.finish("byMatch", "b2\tadd\na1\tfix\n")
	h.finish("byMatch:mark", "  fix  \n")
	if got := h.e.View("byMatch").Marked; !reflect.DeepEqual(got, []bool{false, true}) {
		t.Errorf("by match (trimmed) = %v", got)
	}
}

func TestMarkCachedWithRows(t *testing.T) {
	src := `
panels:
  - {id: branches, source: git branch}
  - id: commits
    source: "git log {{branches.line}}"
    mark: {source: "git rev-parse {{branches.line}}"}
`
	h := newHarness(t, src)
	h.finish("branches", "main\nfeature\n")
	h.finish("commits", "a1\na0\n")
	h.finish("commits:mark", "a0\n")
	h.move("branches", 1)
	h.doSettle()
	h.finish("commits", "b2\n")
	h.finish("commits:mark", "b2\n")
	n := len(h.ran)
	h.move("branches", -1) // back to main: rows and marks both cached
	h.doSettle()
	if len(h.ran) != n {
		t.Errorf("cached revisit ran %v", h.ran[n:])
	}
	if got := h.e.View("commits").Marked; !reflect.DeepEqual(got, []bool{false, true}) {
		t.Errorf("cached marks = %v", got)
	}
}

func TestActionsDropCachedMarks(t *testing.T) {
	h := newHarness(t, marksDef)
	h.finish("branches", "feature\nmain\n")
	h.finish("branches:mark", "main\n")
	h.apply(h.e.ActionDone(h.action("space")))
	if len(h.e.mcache) != 0 {
		t.Errorf("mark cache survived an action: %v", h.e.mcache)
	}
}
