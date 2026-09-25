package def

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoadGitExample(t *testing.T) {
	d, err := Load("../../examples/git-lite.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "git-lite" {
		t.Errorf("Name = %q", d.Name)
	}
	if got := ids(d.Panels); !reflect.DeepEqual(got, []string{"status", "branches", "commits", "files", "tags", "main"}) {
		t.Errorf("panels = %v", got)
	}
	commits, files := d.Panel("commits"), d.Panel("files")
	if commits.Children != "files" || files.Parent != "commits" {
		t.Errorf("children/parent = %q/%q", commits.Children, files.Parent)
	}
	if !reflect.DeepEqual(commits.Deps, []string{"branches"}) {
		t.Errorf("commits deps = %v", commits.Deps)
	}
	if !reflect.DeepEqual(files.Deps, []string{"commits"}) {
		t.Errorf("files deps = %v", files.Deps)
	}
	if !reflect.DeepEqual(d.Order, []string{"status", "branches", "commits", "files", "tags", "main"}) {
		t.Errorf("order = %v", d.Order)
	}
	if !reflect.DeepEqual(topLevel(d), []string{"status", "branches", "commits", "tags", "main"}) {
		t.Errorf("top level = %v", topLevel(d))
	}
	if !reflect.DeepEqual(commits.Key, []string{"fields", "0"}) {
		t.Errorf("key = %v", commits.Key)
	}
	if commits.Title != "Commits" || d.Panel("files").Title != "Files" {
		t.Error("titles not set")
	}
	if d.Timeout != 30*time.Second {
		t.Errorf("default timeout = %v", d.Timeout)
	}
	if d.Layout.Focus != "equal" || d.Panel("status").Size.Kind != Fit || d.Panel("tags").Side != "right" {
		t.Errorf("layout = %+v, status size = %+v, tags side = %q", d.Layout, d.Panel("status").Size, d.Panel("tags").Side)
	}
	acts := d.Panel("branches").Actions
	if len(acts) != 3 || acts[0].Key != "space" || acts[0].Mode != "background" || acts[1].Prompt == "" || !acts[2].Confirm {
		t.Errorf("branch actions = %+v %+v %+v", acts[0], acts[1], acts[2])
	}
	if len(d.Actions) != 2 || d.Actions[0].Key != "R" || d.Actions[1].Mode != "interactive" {
		t.Errorf("global actions = %+v", d.Actions)
	}
	if tabs := d.Panel("main").Content["commits"].Tabs; len(tabs) != 1 || tabs[0].Mode != "once" || tabs[0].Format != "text" {
		t.Errorf("tabs = %+v", tabs)
	}
}

func TestLoadECSExample(t *testing.T) {
	d, err := Load("../../examples/ecs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.ContextOrder, []string{"profile", "region"}) {
		t.Errorf("context order = %v", d.ContextOrder)
	}
	if d.Context["region"].Default != "eu-west-1" || len(d.Context["region"].Values) != 3 {
		t.Errorf("region = %+v", d.Context["region"])
	}
	if d.Context["profile"].Source == nil {
		t.Error("profile source not parsed")
	}
	if d.Panel("services").Refresh != 10*time.Second {
		t.Errorf("refresh = %v", d.Panel("services").Refresh)
	}
	if !reflect.DeepEqual(d.Panel("tasks").Deps, []string{"clusters", "services"}) {
		t.Errorf("tasks deps = %v", d.Panel("tasks").Deps)
	}
	if n := len(d.Panel("services").Columns); n != 3 {
		t.Errorf("columns = %d", n)
	}
	if d.Panel("main").Content["tasks"].Tabs[0].Mode != "stream" {
		t.Error("stream mode not parsed")
	}
	if d.Panel("tasks").Actions[0].Mode != "interactive" || !d.Panel("services").Actions[0].Confirm {
		t.Error("action mode/confirm not parsed")
	}
	if d.Env["AWS_PROFILE"] == nil {
		t.Error("env not parsed")
	}
}

func TestTimeout(t *testing.T) {
	d, err := Parse([]byte("timeout: 5s\npanels: [{id: a, source: x}]"), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Timeout != 5*time.Second {
		t.Errorf("timeout = %v", d.Timeout)
	}
}

func TestNameDefaultsToFileName(t *testing.T) {
	d, err := Parse([]byte("panels: [{id: a, source: x}]"), "/x/lazyecs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "lazyecs" {
		t.Errorf("Name = %q", d.Name)
	}
}

func TestOrderIsTopological(t *testing.T) {
	src := `
panels:
  - {id: c, source: "x {{b.x}}"}
  - {id: b, source: "x {{a.x}}"}
  - {id: a, source: x}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Order, []string{"a", "b", "c"}) {
		t.Errorf("order = %v", d.Order)
	}
	if !reflect.DeepEqual(d.Dependents("a"), []string{"b"}) {
		t.Errorf("dependents(a) = %v", d.Dependents("a"))
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{"yaml syntax", "panels: [", "t.yaml:1"},
		{"unknown field", "panels:\n  - id: a\n    source: x\n    sauce: y", "t.yaml:4"},
		{"no panels", "name: x", "no panels"},
		{"missing id", "panels:\n  - source: x", "t.yaml:2: panel: id is required"},
		{"bad id", "panels:\n  - {id: 'a b', source: x}", "invalid id"},
		{"reserved id", "panels:\n  - {id: ctx, source: x}", "reserved"},
		{"duplicate id", "panels:\n  - {id: a, source: x}\n  - {id: a, source: y}", "t.yaml:3: panel a: duplicate id"},
		{"missing source", "panels:\n  - id: a", "panel a: source is required"},
		{"bad template", "panels:\n  - {id: a, source: 'x {{.y'}", "panel a: source: unclosed"},
		{"unknown panel ref", "panels:\n  - id: a\n    source: x {{nope.y}}", "t.yaml:3: panel a: source: unknown panel \"nope\""},
		{"self ref", "panels:\n  - {id: a, source: 'x {{a.y}}'}", "references itself"},
		{"row ref in source", "panels:\n  - {id: a, source: 'x {{.y}}'}", "{{.y}} in source"},
		{"input in source", "panels:\n  - {id: a, source: 'x {{input}}'}", "{{input}}"},
		{"unknown ctx", "panels:\n  - {id: a, source: 'x {{ctx.region}}'}", "unknown context \"region\""},
		{"cycle", "panels:\n  - {id: a, source: 'x {{b.y}}'}\n  - {id: b, source: 'x {{a.y}}'}", "cycle: a → b → a"},
		{"bad jq", "panels:\n  - {id: a, source: x, rows: '.['}", "panel a: rows"},
		{"split with rows", "panels:\n  - {id: a, source: x, rows: '.', split: ','}", "split"},
		{"label and columns", "panels:\n  - {id: a, source: x, label: y, columns: [{title: t, value: v}]}", "label and columns"},
		{"column without value", "panels:\n  - {id: a, source: x, columns: [{title: t}]}", "value is required"},
		{"bad key", "panels:\n  - {id: a, source: x, key: 'foo'}", "key"},
		{"bad refresh", "panels:\n  - {id: a, source: x, refresh: soon}", "refresh"},
		{"unknown child", "panels:\n  - {id: a, source: x, children: b}", "children: unknown panel \"b\""},
		{"self child", "panels:\n  - {id: a, source: x, children: a}", "children"},
		{"two parents", "panels:\n  - {id: a, source: x, children: c}\n  - {id: b, source: x, children: c}\n  - {id: c, source: x}", "already a child of a"},
		{"content unknown panel", "panels:\n  - {id: a, source: x}\n  - id: m\n    content:\n      b: {tabs: [{name: n, cmd: c}]}", "t.yaml:5: panel m: content: unknown panel \"b\""},
		{"content key is content panel", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {n: {tabs: [{name: t, cmd: c}]}}}\n  - {id: n, content: {a: {tabs: [{name: t, cmd: c}]}}}", "panel m: content: n is a content panel"},
		{"tab without cmd", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {a: {tabs: [{name: n}]}}}", "cmd is required"},
		{"tab bad mode", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {a: {tabs: [{name: n, cmd: c, mode: loop}]}}}", "mode"},
		{"tab bad format", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {a: {tabs: [{name: n, cmd: c, format: xml}]}}}", "format"},
		{"json stream", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {a: {tabs: [{name: n, cmd: c, mode: stream, format: json}]}}}", "format json is only supported with mode once"},
		{"tab unknown ref", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {a: {tabs: [{name: n, cmd: 'c {{z.q}}'}]}}}", "unknown panel \"z\""},
		{"source and content", "panels:\n  - {id: a, source: x, content: {default: {tabs: [{name: n, cmd: c}]}}}", "panel a: use either source or content, not both"},
		{"list field on content", "panels:\n  - {id: a, source: x}\n  - {id: m, rows: '.', content: {a: {tabs: [{name: n, cmd: c}]}}}", "panel m: rows only applies to list panels"},
		{"children on content", "panels:\n  - {id: a, source: x}\n  - {id: m, children: a, content: {a: {tabs: [{name: n, cmd: c}]}}}", "panel m: children only applies to list panels"},
		{"content panel as child", "panels:\n  - {id: a, source: x, children: m}\n  - {id: m, content: {a: {tabs: [{name: n, cmd: c}]}}}", "children: m is a content panel"},
		{"ref to content panel", "panels:\n  - {id: m, content: {default: {tabs: [{name: n, cmd: c}]}}}\n  - {id: a, source: 'x {{m.y}}'}", "m is a content panel and has no rows"},
		{"default id reserved", "panels:\n  - {id: default, source: x}", "panel default: id is reserved"},
		{"old detail key", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n, cmd: c}]}", "t.yaml:2: detail: was replaced by content panels"},
		{"mark bad path", "panels:\n  - id: a\n    source: x\n    mark: current", "t.yaml:4: panel a: mark: must be a row path like .current or {source: ..., match: ...}"},
		{"mark no source", "panels:\n  - id: a\n    source: x\n    mark: {match: '{{.line}}'}", "panel a: mark: source is required"},
		{"mark unknown field", "panels:\n  - id: a\n    source: x\n    mark: {source: y, when: z}", "t.yaml:4: panel a: mark: unknown field \"when\""},
		{"mark source row ref", "panels:\n  - id: a\n    source: x\n    mark: {source: 'y {{.line}}'}", "{{.line}} in source"},
		{"mark source self", "panels:\n  - id: a\n    source: x\n    mark: {source: 'y {{a.line}}'}", "references itself"},
		{"mark bad match", "panels:\n  - id: a\n    source: x\n    mark: {source: y, match: '{{.x'}", "panel a: mark: match: unclosed"},
		{"mark on content", "panels:\n  - {id: a, source: x}\n  - {id: m, mark: .x, content: {a: {tabs: [{name: n, cmd: c}]}}}", "panel m: mark only applies to list panels"},
		{"bad side", "panels:\n  - {id: a, source: x, side: middle}", "side must be left, center or right"},
		{"old actions map", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: x, cmd: c}]", "t.yaml:2: actions: panel actions now live in the panel"},
		{"action no key", "panels:\n  - id: a\n    source: x\n    actions: [{cmd: c}]", "panel a: action: key is required"},
		{"action reserved key", "panels:\n  - id: a\n    source: x\n    actions: [{key: q, cmd: c}]", "key \"q\" is reserved"},
		{"action dup key", "panels:\n  - id: a\n    source: x\n    actions: [{key: x, cmd: c}, {key: x, cmd: d}]", "duplicate key \"x\""},
		{"action bad mode", "panels:\n  - id: a\n    source: x\n    actions: [{key: x, cmd: c, mode: loud}]", "mode"},
		{"action input without prompt", "panels:\n  - id: a\n    source: x\n    actions: [{key: x, cmd: 'c {{input}}'}]", "prompt"},
		{"action refresh unknown", "panels:\n  - id: a\n    source: x\n    actions: [{key: x, cmd: c, refresh: [z]}]", "refresh: unknown panel \"z\""},
		{"global no cmd", "actions:\n  - {key: x}\npanels: [{id: a, source: x}]", "t.yaml:2: global action \"x\": cmd is required"},
		{"global dup key", "actions:\n  - {key: x, cmd: c}\n  - {key: x, cmd: d}\npanels: [{id: a, source: x}]", "t.yaml:3: global action \"x\": duplicate key"},
		{"global reserved", "actions: [{key: tab, cmd: c}]\npanels: [{id: a, source: x}]", "key \"tab\" is reserved"},
		{"ctx source and values", "context:\n  r: {source: x, values: [a]}\npanels: [{id: a, source: x}]", "exactly one of source or values"},
		{"ctx neither", "context:\n  r: {default: a}\npanels: [{id: a, source: x}]", "exactly one of source or values"},
		{"ctx source refs panel", "context:\n  r: {source: 'x {{a.b}}'}\npanels: [{id: a, source: x}]", "context r"},
		{"env refs panel", "env:\n  X: '{{a.b}}'\npanels: [{id: a, source: x}]", "env X"},
		{"bad timeout", "timeout: forever\npanels: [{id: a, source: x}]", "timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "t.yaml")
			if err == nil {
				t.Fatalf("want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q\ndoes not contain %q", err, tt.want)
			}
		})
	}
}

func TestReportsAllErrors(t *testing.T) {
	src := "panels:\n  - {id: a}\n  - {id: b}"
	_, err := Parse([]byte(src), "t.yaml")
	errs, ok := err.(Errors)
	if !ok || len(errs) != 2 {
		t.Fatalf("want 2 errors, got %v", err)
	}
}

func TestValidContentAndActionRefs(t *testing.T) {
	src := `
context:
  region: {values: [a, b], default: a}
env:
  R: "{{ctx.region}}"
panels:
  - {id: a, source: "x {{ctx.region}}", children: b}
  - id: b
    source: "x {{a.y}}"
    actions:
      - {key: s, cmd: "c {{.q}} {{input}}", prompt: Count, refresh: [a, b]}
      - {key: F, desc: Override, cmd: "c {{.q}}"}
  - id: main
    content:
      b:
        tabs: [{name: n, cmd: "c {{.q}} {{a.y}} {{ctx.region}}"}]
      default:
        tabs: [{name: row, cmd: "echo {{.}}", format: json}]
actions:
  - {key: F, desc: Fetch, cmd: "fetch {{ctx.region}} {{.q}}"}
`
	if _, err := Parse([]byte(src), "t.yaml"); err != nil {
		t.Fatal(err)
	}
}

func ids(ps []*Panel) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func topLevel(d *Definition) []string {
	var out []string
	for _, p := range d.Panels {
		if p.Parent == "" {
			out = append(out, p.ID)
		}
	}
	return out
}

func TestUnknownFieldIsFriendlyAndValidationContinues(t *testing.T) {
	src := "panels:\n  - id: a\n    sauce: y\n    source: 'x {{nope.x}}'"
	_, err := Parse([]byte(src), "t.yaml")
	if err == nil {
		t.Fatal("want error")
	}
	msg := err.Error()
	for _, want := range []string{`t.yaml:3: unknown field "sauce" in panel`, `unknown panel "nope"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q\ndoes not contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "rawPanel") {
		t.Errorf("error leaks Go type names: %q", msg)
	}
}

func TestLayoutDefaults(t *testing.T) {
	d, err := Parse([]byte("panels: [{id: a, source: x}]"), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Layout != (Layout{Focus: "expand", LeftWidth: 40, RightWidth: 25}) {
		t.Errorf("layout = %+v", d.Layout)
	}
	p := d.Panel("a")
	if p.Side != "left" || p.Size != (Size{Kind: Flex, N: 1}) {
		t.Errorf("side/size = %q/%+v", p.Side, p.Size)
	}

	d, err = Parse([]byte("panels: [{id: a, source: x}, {id: b, source: y, side: right}]"), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Layout.LeftWidth != 30 || d.Layout.RightWidth != 25 {
		t.Errorf("with right panels, layout = %+v", d.Layout)
	}
}

func TestLayoutParsing(t *testing.T) {
	src := `
layout: {focus: equal, left_width: 35, right_width: 20}
panels:
  - {id: a, source: x, size: fit}
  - {id: b, source: x, size: 5}
  - {id: c, source: x, size: 3fr, side: right}
  - {id: d, source: x, size: 1fr, side: left}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Layout != (Layout{Focus: "equal", LeftWidth: 35, RightWidth: 20}) {
		t.Errorf("layout = %+v", d.Layout)
	}
	want := map[string]Size{"a": {Kind: Fit}, "b": {Kind: Fixed, N: 5}, "c": {Kind: Flex, N: 3}, "d": {Kind: Flex, N: 1}}
	for id, s := range want {
		if got := d.Panel(id).Size; got != s {
			t.Errorf("%s size = %+v, want %+v", id, got, s)
		}
	}
	if d.Panel("c").Side != "right" {
		t.Errorf("c side = %q", d.Panel("c").Side)
	}
}

func TestLayoutValidation(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"bad side", "panels:\n  - id: a\n    source: x\n    side: top", "t.yaml:4: panel a: side must be left, center or right"},
		{"bad size", "panels:\n  - id: a\n    source: x\n    size: big", "t.yaml:4: panel a: size"},
		{"zero size", "panels: [{id: a, source: x, size: 0}]", "size"},
		{"zero fr", "panels: [{id: a, source: x, size: 0fr}]", "size"},
		{"child side", "panels:\n  - {id: a, source: x, children: b}\n  - {id: b, source: x, side: right}", "panel b: side/size not allowed on a drill-in child"},
		{"child size", "panels:\n  - {id: a, source: x, children: b}\n  - {id: b, source: x, size: fit}", "not allowed on a drill-in child"},
		{"bad focus", "layout: {focus: grow}\npanels: [{id: a, source: x}]", "t.yaml:1: layout: focus must be expand or equal"},
		{"narrow", "layout: {left_width: 5}\npanels: [{id: a, source: x}]", "left_width"},
		{"wide", "layout: {right_width: 85}\npanels: [{id: a, source: x}]", "right_width"},
		{"too wide together", "layout: {left_width: 60, right_width: 40}\npanels: [{id: a, source: x}]", "leave at least 10% for main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "t.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %v\ndoes not contain %q", err, tt.want)
			}
		})
	}
}

func TestTabDeps(t *testing.T) {
	d, err := Load("../../examples/git-lite.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Panel("main").Content["files"].Tabs[0].Deps; !reflect.DeepEqual(got, []string{"commits"}) {
		t.Errorf("files tab deps = %v", got)
	}
	if got := d.Panel("main").Content["commits"].Tabs[0].Deps; got != nil {
		t.Errorf("commits tab deps = %v, want none (only row refs)", got)
	}
}

func TestContentPanels(t *testing.T) {
	src := `
panels:
  - {id: branches, source: git branch}
  - id: main
    title: Main
    content:
      branches:
        tabs: [{name: Log, cmd: "git log {{.line}}"}]
      default:
        tabs: [{name: Status, cmd: git status}]
  - id: log
    side: right
    size: fit
    content:
      default:
        tabs: [{name: Tail, cmd: tail -F x.log, mode: stream}]
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	main, log, branches := d.Panel("main"), d.Panel("log"), d.Panel("branches")
	if !main.IsContent() || !log.IsContent() || branches.IsContent() {
		t.Fatalf("kinds: main %v log %v branches %v", main.IsContent(), log.IsContent(), branches.IsContent())
	}
	if main.Side != "center" || log.Side != "right" || branches.Side != "left" {
		t.Errorf("sides = %q %q %q", main.Side, log.Side, branches.Side)
	}
	if log.Size.Kind != Fit {
		t.Errorf("log size = %+v", log.Size)
	}
	if got := main.Content["branches"].Tabs[0].Cmd.Source(); got != "git log {{.line}}" {
		t.Errorf("branches entry = %q", got)
	}
	if main.Content["default"] == nil || log.Content["default"].Tabs[0].Mode != "stream" {
		t.Error("default entries not parsed")
	}
	if main.Source != nil || main.Parser != nil || main.Label != nil {
		t.Error("content panel should have no list fields")
	}
	if slices.Contains(d.Order, "main") && len(main.Deps) != 0 {
		t.Errorf("content panel deps = %v", main.Deps)
	}
}

func TestListPanelCanBeCentered(t *testing.T) {
	d, err := Parse([]byte("panels: [{id: a, source: x, side: center}]"), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if d.Panel("a").Side != "center" {
		t.Errorf("side = %q", d.Panel("a").Side)
	}
}

func TestGlobalAndPanelActions(t *testing.T) {
	src := `
actions:
  - {key: R, desc: Fetch, cmd: git fetch, refresh: [a]}
  - {key: space, desc: Global space, cmd: echo}
panels:
  - id: a
    source: x
    actions:
      - {key: space, desc: Checkout, cmd: "git checkout {{.line}}"}
      - {key: e, desc: Shell, cmd: sh, mode: interactive, confirm: true}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Actions) != 2 || d.Actions[0].Key != "R" || d.Actions[0].Refresh[0] != "a" {
		t.Errorf("globals = %+v", d.Actions)
	}
	acts := d.Panel("a").Actions
	if len(acts) != 2 || acts[0].Key != "space" || acts[1].Mode != "interactive" || !acts[1].Confirm {
		t.Errorf("panel actions = %+v", acts)
	}
}

func TestMark(t *testing.T) {
	src := `
panels:
  - {id: z, source: x}
  - id: a
    source: x
    mark: .current
  - id: b
    source: x
    key: .fields.0
    mark: {source: "git branch --show-current {{a.name}}"}
  - id: c
    source: x
    mark: {source: y, match: "{{.fields.1}}"}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if m := d.Panel("a").Mark; m == nil || !reflect.DeepEqual(m.Path, []string{"current"}) || m.Source != nil {
		t.Errorf("path mark = %+v", m)
	}
	b := d.Panel("b")
	if b.Mark == nil || b.Mark.Source.Source() != "git branch --show-current {{a.name}}" || b.Mark.Match != nil {
		t.Errorf("command mark = %+v", b.Mark)
	}
	if !reflect.DeepEqual(b.Deps, []string{"a"}) {
		t.Errorf("mark source deps should join the panel's: %v", b.Deps)
	}
	if m := d.Panel("c").Mark; m.Match == nil || m.Match.Source() != "{{.fields.1}}" {
		t.Errorf("match = %+v", m)
	}
	if d.Panel("z").Mark != nil {
		t.Error("panel without mark has one")
	}
}
