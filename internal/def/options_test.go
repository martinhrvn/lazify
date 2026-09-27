package def

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

const optionsDef = `
panels:
  - id: branches
    source: git branch
  - id: commits
    source: git log {{opt.since}} {{opt.author}} {{opt.merges}} -n {{opt.max}} {{opt.grep}} {{branches.line}}
    options:
      - {id: since, title: Since, type: date, default: -14d, flag: --since=}
      - {id: author, title: Author, source: "git log --format=%an {{branches.line}}", flag: --author=}
      - {id: merges, title: No merges, type: toggle, flag: --no-merges}
      - {id: max, title: Max, values: [50, 200, 1000], default: 200}
      - {id: grep, flag: --grep=}
    actions:
      - {key: x, desc: Export, cmd: "git log {{opt.since}} > out"}
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

func TestOptions(t *testing.T) {
	d, err := Parse([]byte(optionsDef), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	opts := d.Panel("commits").Options
	var got []string
	for _, o := range opts {
		got = append(got, o.ID+":"+o.Type+":"+o.Default)
	}
	want := []string{"since:date:-14d", "author:choice:", "merges:toggle:false", "max:choice:200", "grep:text:"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("options = %v, want %v", got, want)
	}
	if opts[0].Title != "Since" || opts[4].Title != "grep" || opts[1].Source == nil || opts[1].Flag != "--author=" {
		t.Errorf("titles/source/flag: %+v %+v %+v", opts[0], opts[4], opts[1])
	}
	if !reflect.DeepEqual(opts[3].Values, []string{"50", "200", "1000"}) {
		t.Errorf("values = %v", opts[3].Values)
	}
	// {{commits.opt.since}} is a dependency on commits, like any panel field.
	if !reflect.DeepEqual(d.Panel("stats").Deps, []string{"commits"}) {
		t.Errorf("stats deps = %v", d.Panel("stats").Deps)
	}
	if o := d.Panel("main").Content["commits"].Options; len(o) != 1 || o[0].Default != "3" {
		t.Errorf("entry options = %+v", o)
	}
	if o := d.Panel("main").Content["branches"].Options; len(o) != 0 {
		t.Errorf("branches entry has no options: %+v", o)
	}
}

func TestOptionErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"unknown opt", "panels:\n  - id: a\n    source: x {{opt.nope}}\n    options: [{id: since}]",
			`t.yaml:3: panel a: source: {{opt.nope}}: unknown option "nope" (have: since)`},
		{"no options here", "panels:\n  - id: a\n    source: x {{opt.since}}",
			"t.yaml:3: panel a: source: {{opt.since}}: this panel has no options"},
		{"other panel's option", "panels:\n  - id: a\n    source: x\n    options: [{id: since}]\n  - id: b\n    source: y {{a.opt.until}}",
			`t.yaml:6: panel b: source: {{a.opt.until}}: panel a has no option "until"`},
		{"duplicate", "panels:\n  - id: a\n    source: x\n    options: [{id: s}, {id: s}]", `t.yaml:4: panel a: option "s": duplicate id`},
		{"bad type", "panels:\n  - id: a\n    source: x\n    options: [{id: s, type: number}]", "t.yaml:4: panel a: option \"s\": type must be text, choice, toggle or date"},
		{"toggle flag", "panels:\n  - id: a\n    source: x\n    options: [{id: s, type: toggle}]", "toggle needs a flag"},
		{"bad date", "panels:\n  - id: a\n    source: x\n    options: [{id: s, type: date, default: soon}]", `default "soon" is not a date (YYYY-MM-DD, today, yesterday, -3d, -2w)`},
		{"date default", "panels:\n  - id: a\n    source: x\n    options: [{id: s, type: date}]", "a date without a default needs a flag"},
		{"values and source", "panels:\n  - id: a\n    source: x\n    options: [{id: s, values: [a], source: y}]", "use either values or source"},
		{"default not a value", "panels:\n  - id: a\n    source: x\n    options: [{id: s, values: [a, b], default: c}]", `default "c" is not one of the values`},
		{"select", "panels:\n  - id: a\n    select: true\n    values: [x]\n    options: [{id: s}]", "t.yaml:5: panel a: options are not supported on select panels"},
		{"content panel", "panels:\n  - {id: a, source: x}\n  - id: m\n    options: [{id: s}]\n    content: {default: {tabs: [{name: T, cmd: y}]}}", "t.yaml:4: panel m: options go under each content entry"},
		{"reserved id", "panels:\n  - {id: opt, source: x}", "panel opt: id is reserved"},
		{"env", "env:\n  X: '{{opt.s}}'\npanels:\n  - {id: a, source: x, options: [{id: s}]}", "env X: {{opt.s}}: options belong to a panel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "t.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant %q", err, tt.want)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	now := time.Date(2026, 9, 27, 15, 4, 0, 0, time.Local)
	for in, want := range map[string]string{
		"2026-01-02": "2026-01-02", "today": "2026-09-27", "yesterday": "2026-09-26",
		"-3d": "2026-09-24", "-2w": "2026-09-13", "+1d": "2026-09-28",
	} {
		got, ok := ParseDate(in, now)
		if !ok || got.Format(DateLayout) != want {
			t.Errorf("ParseDate(%q) = %v %v, want %s", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "soon", "2026-13-01", "-d", "3d"} {
		if _, ok := ParseDate(in, now); ok {
			t.Errorf("ParseDate(%q) should fail", in)
		}
	}
}
