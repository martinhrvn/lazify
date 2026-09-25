package def

import (
	"reflect"
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
	if got := ids(d.Panels); !reflect.DeepEqual(got, []string{"status", "branches", "commits", "files", "tags"}) {
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
	if !reflect.DeepEqual(d.Order, []string{"status", "branches", "commits", "files", "tags"}) {
		t.Errorf("order = %v", d.Order)
	}
	if !reflect.DeepEqual(topLevel(d), []string{"status", "branches", "commits", "tags"}) {
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
	acts := d.Actions["branches"]
	if len(acts) != 1 || acts[0].Key != "space" || acts[0].Mode != "background" {
		t.Errorf("actions = %+v", acts)
	}
	if tabs := d.Detail["commits"].Tabs; len(tabs) != 1 || tabs[0].Mode != "once" || tabs[0].Format != "text" {
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
	if d.Detail["tasks"].Tabs[0].Mode != "stream" {
		t.Error("stream mode not parsed")
	}
	if d.Actions["tasks"][0].Mode != "interactive" || !d.Actions["services"][0].Confirm {
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
		{"detail unknown panel", "panels: [{id: a, source: x}]\ndetail:\n  b: {tabs: [{name: n, cmd: c}]}", "t.yaml:3: detail: unknown panel \"b\""},
		{"tab without cmd", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n}]}", "cmd is required"},
		{"tab bad mode", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n, cmd: c, mode: loop}]}", "mode"},
		{"tab bad format", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n, cmd: c, format: xml}]}", "format"},
		{"json stream", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n, cmd: c, mode: stream, format: json}]}", "format json is only supported with mode once"},
		{"tab unknown ref", "panels: [{id: a, source: x}]\ndetail:\n  a: {tabs: [{name: n, cmd: 'c {{z.q}}'}]}", "unknown panel \"z\""},
		{"action unknown panel", "panels: [{id: a, source: x}]\nactions:\n  b: [{key: x, cmd: c}]", "actions: unknown panel \"b\""},
		{"action no key", "panels: [{id: a, source: x}]\nactions:\n  a: [{cmd: c}]", "key is required"},
		{"action reserved key", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: q, cmd: c}]", "key \"q\" is reserved"},
		{"action dup key", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: x, cmd: c}, {key: x, cmd: d}]", "duplicate key \"x\""},
		{"action bad mode", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: x, cmd: c, mode: loud}]", "mode"},
		{"action input without prompt", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: x, cmd: 'c {{input}}'}]", "prompt"},
		{"action refresh unknown", "panels: [{id: a, source: x}]\nactions:\n  a: [{key: x, cmd: c, refresh: [z]}]", "refresh: unknown panel \"z\""},
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

func TestValidDetailAndActionRefs(t *testing.T) {
	src := `
context:
  region: {values: [a, b], default: a}
env:
  R: "{{ctx.region}}"
panels:
  - {id: a, source: "x {{ctx.region}}", children: b}
  - {id: b, source: "x {{a.y}}"}
detail:
  b:
    tabs: [{name: n, cmd: "c {{.q}} {{a.y}} {{ctx.region}}"}]
actions:
  b:
    - {key: s, cmd: "c {{.q}} {{input}}", prompt: Count, refresh: [a, b]}
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
		{"bad side", "panels:\n  - id: a\n    source: x\n    side: top", "t.yaml:4: panel a: side must be left or right"},
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
	if got := d.Detail["files"].Tabs[0].Deps; !reflect.DeepEqual(got, []string{"commits"}) {
		t.Errorf("files tab deps = %v", got)
	}
	if got := d.Detail["commits"].Tabs[0].Deps; got != nil {
		t.Errorf("commits tab deps = %v, want none (only row refs)", got)
	}
}
