package engine

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const optionsEngineDef = `
env:
  REPO: "{{branches.line}}"
panels:
  - id: branches
    source: git branch
  - id: commits
    source: git log {{opt.since}} {{opt.author}} {{opt.merges}} -n {{opt.max}} {{branches.line}}
    key: .line
    options:
      - {id: since, type: date, default: -14d, flag: --since=}
      - {id: author, source: "git log --format=%an", flag: --author=}
      - {id: merges, type: toggle, flag: --no-merges}
      - {id: max, values: ["50", "200"], default: "200"}
  - id: stats
    source: "git shortlog -s {{commits.opt.since}}"
  - id: main
    content:
      commits:
        options: [{id: context, values: ["3", "10"]}]
        tabs: [{name: Diff, cmd: "git show -U{{opt.context}} {{.line}}"}]
      branches:
        tabs: [{name: Log, cmd: "git log {{.line}}"}]
`

func optionsHarness(t *testing.T) *harness {
	h := &harness{t: t, inflight: map[string]Run{}}
	h.e = New(mustDef(t, optionsEngineDef), nil)
	h.e.now = func() time.Time { return time.Date(2026, 9, 27, 15, 0, 0, 0, time.Local) }
	h.apply(h.e.Start())
	h.finish("branches", "main\n")
	return h
}

func TestOptionDefaultsRender(t *testing.T) {
	h := optionsHarness(t)
	// Unset flag options (author, merges off) leave nothing behind, not ''.
	if got := h.cmd("commits"); got != "git log --since=2026-09-13   -n 200 main" {
		t.Errorf("commits = %q", got)
	}
	// Reading only another panel's option needs no selection there.
	if got := h.cmd("stats"); got != "git shortlog -s --since=2026-09-13" {
		t.Errorf("stats = %q", got)
	}
	h.finish("commits", "a1\nb2\n")
	h.focus("commits")
	if got := h.cmd("main"); got != "git show -U3 a1" {
		t.Errorf("main = %q", got)
	}
	if v := h.e.View("commits"); len(v.Options) != 0 {
		t.Errorf("defaults are not shown in the title: %v", v.Options)
	}
}

func TestSetOptionsReruns(t *testing.T) {
	h := optionsHarness(t)
	h.finish("commits", "a1\nb2\n")
	h.finish("stats", "3 me\n")
	h.finish("main", "diff\n")
	h.focus("commits")
	h.move("commits", 1)
	h.doSettle()
	h.finish("main", "diff b2\n")

	fx, err := h.e.SetOptions("commits", map[string]string{"since": "2026-09-01", "author": "Jane Doe", "merges": "true", "max": "200"})
	if err != nil {
		t.Fatal(err)
	}
	h.apply(fx)
	if got := h.cmd("commits"); got != "git log --since=2026-09-01 '--author=Jane Doe' --no-merges -n 200 main" {
		t.Errorf("commits = %q", got)
	}
	h.finish("commits", "a1\nb2\n")
	if got := h.cmd("stats"); got != "git shortlog -s --since=2026-09-01" {
		t.Errorf("readers of commits.opt re-run: stats = %q", got)
	}
	if v := h.e.View("commits"); !reflect.DeepEqual(v.Options, []string{"since=2026-09-01", "author=Jane Doe", "merges"}) || v.Cursor != 1 {
		t.Errorf("title options %v, cursor %d (kept on b2)", v.Options, v.Cursor)
	}
	// A content entry's options re-run only that entry.
	fx, err = h.e.SetOptions(EntryOwner("main", "commits"), map[string]string{"context": "10"})
	if err != nil {
		t.Fatal(err)
	}
	h.apply(fx)
	if got := h.cmd("main"); got != "git show -U10 b2" {
		t.Errorf("main = %q", got)
	}
	if v := h.e.ContentView("main"); !reflect.DeepEqual(v.Options, []string{"context=10"}) {
		t.Errorf("main options = %v", v.Options)
	}
}

func TestSetOptionsValidates(t *testing.T) {
	h := optionsHarness(t)
	for _, bad := range []map[string]string{{"since": "soon"}, {"max": "7"}, {"merges": "maybe"}, {"nope": "x"}} {
		fx, err := h.e.SetOptions("commits", bad)
		if err == nil || len(fx.Runs) != 0 {
			t.Errorf("SetOptions(%v) = %v, %v; want an error and nothing run", bad, fx.Runs, err)
		}
	}
	// Relative dates are accepted and stored as dates.
	fx, err := h.e.SetOptions("commits", map[string]string{"since": "-1w"})
	if err != nil {
		t.Fatal(err)
	}
	h.apply(fx)
	if got := h.cmd("commits"); !strings.HasPrefix(got, "git log --since=2026-09-20 ") {
		t.Errorf("commits = %q", got)
	}
}

func TestOptionsInEffect(t *testing.T) {
	h := optionsHarness(t)
	h.finish("commits", "a1\n")
	h.focus("commits")
	groups := h.e.OptionsInEffect()
	var got []string
	for _, g := range groups {
		var ids []string
		for _, f := range g.Fields {
			ids = append(ids, f.Option.ID+"="+f.Value)
		}
		got = append(got, g.Title+": "+strings.Join(ids, " "))
	}
	want := []string{"commits: since=2026-09-13 author= merges=false max=200", "main · commits: context=3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups = %q\nwant     %q", got, want)
	}
	h.focus("branches") // no options of its own, and main's branches entry has none
	if g := h.e.OptionsInEffect(); len(g) != 0 {
		t.Errorf("branches: %+v", g)
	}
}

func TestOptionChoicesFromSource(t *testing.T) {
	h := optionsHarness(t)
	h.finish("commits", "a1\n")
	h.focus("commits")
	h.apply(h.e.OpenOptions())
	r, ok := h.inflight["commits:choices"]
	if !ok || r.Req.Cmd != "git log --format=%an" || r.Req.Env["REPO"] != "main" {
		t.Fatalf("choices run = %+v (in flight %v)", r, h.inflight)
	}
	if f := h.e.OptionsInEffect()[0].Fields[1]; !f.Loading {
		t.Errorf("author should be loading: %+v", f)
	}
	h.finish("commits:choices", "Jane\nJoe\n")
	if f := h.e.OptionsInEffect()[0].Fields[1]; f.Loading || !reflect.DeepEqual(f.Choices, []string{"", "Jane", "Joe"}) {
		t.Errorf("author = %+v; want (any) + the command's lines", f)
	}
}

func TestOptionalDate(t *testing.T) {
	h := &harness{t: t, inflight: map[string]Run{}}
	h.e = New(mustDef(t, "panels:\n  - id: log\n    source: git log {{opt.until}}\n    options: [{id: until, type: date, flag: --until=}]"), nil)
	h.apply(h.e.Start())
	if got := h.cmd("log"); got != "git log " {
		t.Errorf("no default: no date: %q", got)
	}
	fx, err := h.e.SetOptions("log", map[string]string{"until": "2026-09-01"})
	if err != nil {
		t.Fatal(err)
	}
	h.apply(fx)
	if got := h.cmd("log"); got != "git log --until=2026-09-01" {
		t.Errorf("log = %q", got)
	}
	fx, err = h.e.SetOptions("log", map[string]string{"until": ""})
	if err != nil {
		t.Fatalf("clearing an optional date: %v", err)
	}
	h.apply(fx)
	if got := h.cmd("log"); got != "git log " {
		t.Errorf("cleared: %q", got)
	}
}
