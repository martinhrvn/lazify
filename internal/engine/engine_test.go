package engine

import (
	"errors"
	"reflect"
	"testing"

	"github.com/martinhrvn/lazify/internal/def"
)

func mustDef(t *testing.T, src string) *def.Definition {
	t.Helper()
	d, err := def.Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const gitDef = `
panels:
  - id: branches
    source: git branch
  - id: commits
    source: git log {{branches.line}}
  - id: tags
    title: Tags
    source: git tag
    key: .line
`

func TestStartRunsRootPanels(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	runs := e.Start().Runs
	if got := panelsOf(runs); !reflect.DeepEqual(got, []string{"branches", "tags"}) {
		t.Fatalf("runs = %v", got)
	}
	if runs[0].Req.Cmd != "git branch" {
		t.Errorf("cmd = %q", runs[0].Req.Cmd)
	}
	if v := e.View("branches"); !v.Loading {
		t.Error("branches should be loading")
	}
	if v := e.View("commits"); v.Blocked != "" || !v.Loading {
		t.Errorf("commits should be waiting for branches, got %+v", v)
	}
}

func TestFinishedFillsRows(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	runs := e.Start().Runs
	e.Finished(runs[0].ID, []byte("main\nfeature\n"), nil)
	v := e.View("branches")
	if v.Loading || !reflect.DeepEqual(v.Lines, []string{"main", "feature"}) || v.Cursor != 0 {
		t.Errorf("view = %+v", v)
	}
}

func TestFinishedError(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	runs := e.Start().Runs
	e.Finished(runs[0].ID, nil, errors.New("exit status 128: not a git repository"))
	if v := e.View("branches"); v.Err != "exit status 128: not a git repository" || v.Loading {
		t.Errorf("view = %+v", v)
	}
}

func TestErrorKeepsOldRowsAsStale(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	e.Finished(e.Start().Runs[0].ID, []byte("main\n"), nil)
	run := e.Refresh().Runs
	if v := e.View("branches"); !v.Stale || len(v.Lines) != 1 {
		t.Errorf("while refreshing: %+v", v)
	}
	e.Finished(run[0].ID, nil, errors.New("boom"))
	if v := e.View("branches"); !v.Stale || v.Err != "boom" || len(v.Lines) != 1 {
		t.Errorf("after error: %+v", v)
	}
}

func TestParseErrorIsShown(t *testing.T) {
	e := New(mustDef(t, "panels: [{id: a, source: x, rows: '.[]'}]"), nil)
	e.Finished(e.Start().Runs[0].ID, []byte("not json"), nil)
	if v := e.View("a"); v.Err == "" {
		t.Errorf("want parse error, view = %+v", v)
	}
}

func TestStaleResultIgnored(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	first := e.Start().Runs[0]
	second := e.Refresh().Runs[0]
	e.Finished(first.ID, []byte("old\n"), nil)
	if v := e.View("branches"); len(v.Lines) != 0 || !v.Loading {
		t.Errorf("superseded result applied: %+v", v)
	}
	e.Finished(second.ID, []byte("new\n"), nil)
	if v := e.View("branches"); !reflect.DeepEqual(v.Lines, []string{"new"}) {
		t.Errorf("view = %+v", v)
	}
}

func TestMoveClamps(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	e.Finished(e.Start().Runs[0].ID, []byte("a\nb\nc\n"), nil)
	e.Move(1)
	e.Move(1)
	e.Move(5)
	if c := e.View("branches").Cursor; c != 2 {
		t.Errorf("cursor = %d", c)
	}
	e.Move(-10)
	if c := e.View("branches").Cursor; c != 0 {
		t.Errorf("cursor = %d", c)
	}
}

func TestCursorRestoredByKeyOnRefresh(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	runs := e.Start().Runs
	e.Finished(runs[1].ID, []byte("v1\nv2\nv3\n"), nil)
	e.FocusPanel("tags")
	e.Move(1) // v2
	e.Finished(e.Refresh().Runs[0].ID, []byte("v0\nv1\nv2\nv3\n"), nil)
	if c := e.View("tags").Cursor; c != 2 {
		t.Errorf("cursor = %d, want 2 (v2)", c)
	}
	e.Finished(e.Refresh().Runs[0].ID, []byte("v9\n"), nil)
	if c := e.View("tags").Cursor; c != 0 {
		t.Errorf("cursor = %d, want 0 when key gone", c)
	}
}

func TestFocusCyclesTopLevelPanels(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	if e.Focused() != "branches" {
		t.Fatalf("initial focus = %q", e.Focused())
	}
	e.FocusNext()
	e.FocusNext()
	if e.Focused() != "tags" {
		t.Errorf("focus = %q", e.Focused())
	}
	e.FocusNext()
	if e.Focused() != "branches" {
		t.Errorf("focus should wrap, got %q", e.Focused())
	}
	e.FocusPrev()
	if e.Focused() != "tags" {
		t.Errorf("focus = %q", e.Focused())
	}
}

func TestTopLevelExcludesChildren(t *testing.T) {
	d := mustDef(t, "panels:\n  - {id: a, source: x, children: b}\n  - {id: b, source: 'y {{a.line}}'}")
	if got := New(d, nil).TopLevel(); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("top level = %v", got)
	}
}

func TestLabelsAndColumns(t *testing.T) {
	src := `
panels:
  - id: svc
    source: x
    rows: .[]
    columns:
      - {title: Name, value: "{{.name}}"}
      - {title: Run, value: "{{.run}}/{{.want}}"}
  - id: plain
    source: x
    rows: .[]
    label: "{{.name}} ({{ctx.region}})"
context:
  region: {values: [eu, us]}
`
	e := New(mustDef(t, src), nil)
	runs := e.Start().Runs
	out := []byte(`[{"name":"web","run":1,"want":2},{"name":"api","run":3,"want":3}]`)
	e.Finished(runs[0].ID, out, nil)
	e.Finished(runs[1].ID, out, nil)
	if v := e.View("svc"); !reflect.DeepEqual(v.Columns, [][]string{{"web", "1/2"}, {"api", "3/3"}}) ||
		!reflect.DeepEqual(v.Headers, []string{"Name", "Run"}) {
		t.Errorf("svc view = %+v", v)
	}
	if v := e.View("plain"); !reflect.DeepEqual(v.Lines, []string{"web (eu)", "api (eu)"}) {
		t.Errorf("plain view = %+v", v)
	}
}

func TestContextAndEnv(t *testing.T) {
	src := `
context:
  region: {values: [eu-west-1, us-east-1]}
  profile: {values: [dev, prod], default: prod}
env:
  AWS_REGION: "{{ctx.region}}"
panels:
  - {id: a, source: "aws --profile {{ctx.profile}}"}
`
	e := New(mustDef(t, src), map[string]string{"region": "us-east-1"})
	run := e.Start().Runs[0]
	if run.Req.Cmd != "aws --profile prod" {
		t.Errorf("cmd = %q", run.Req.Cmd)
	}
	if run.Req.Env["AWS_REGION"] != "us-east-1" {
		t.Errorf("env = %v", run.Req.Env)
	}
	if run.Req.Timeout != def.DefaultTimeout {
		t.Errorf("timeout = %v", run.Req.Timeout)
	}
}

func panelsOf(runs []Run) []string {
	var out []string
	for _, r := range runs {
		out = append(out, r.Panel)
	}
	return out
}

func TestSelection(t *testing.T) {
	e := New(mustDef(t, gitDef), nil)
	if _, ok := e.Selection("branches"); ok {
		t.Error("selection before rows")
	}
	e.Finished(e.Start().Runs[0].ID, []byte("a\nb\n"), nil)
	e.Move(1)
	sel, ok := e.Selection("branches")
	if !ok || !reflect.DeepEqual(sel, map[string]any{"line": "b"}) {
		t.Errorf("selection = %v, %v", sel, ok)
	}
}

func TestTopLevelIsVisualOrder(t *testing.T) {
	src := `
panels:
  - {id: stash, source: x, side: right}
  - {id: status, source: x}
  - {id: tags, source: x, side: right}
  - {id: branches, source: x}
`
	e := New(mustDef(t, src), nil)
	if got := e.TopLevel(); !reflect.DeepEqual(got, []string{"status", "branches", "stash", "tags"}) {
		t.Errorf("top level = %v", got)
	}
	if e.Focused() != "status" {
		t.Errorf("initial focus = %q", e.Focused())
	}
}
