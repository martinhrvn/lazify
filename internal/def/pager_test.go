package def

import (
	"strings"
	"testing"
)

func TestActionPagerAndFloat(t *testing.T) {
	src := `
panels:
  - id: tasks
    source: x
    actions:
      - {key: f, desc: Follow, cmd: "tail -f log", pager: true}
      - {key: b, desc: Bat, cmd: "tail -f log", pager: "bat --paging=always"}
      - {key: s, desc: Shell, cmd: sh, mode: interactive, float: false}
      - {key: e, desc: Edit, cmd: vi, mode: interactive}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][3]string{ // mode, pager, float
		"f": {"interactive", DefaultPager, "true"},
		"b": {"interactive", "bat --paging=always", "true"},
		"s": {"interactive", "", "false"},
		"e": {"interactive", "", "true"},
	}
	for _, a := range d.Panel("tasks").Actions {
		got := [3]string{a.Mode, a.Pager, map[bool]string{true: "true", false: "false"}[a.Float]}
		if got != want[a.Key] {
			t.Errorf("%s: mode/pager/float = %v, want %v", a.Key, got, want[a.Key])
		}
	}
}

func TestActionPagerErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"background pager", "panels:\n  - id: a\n    source: x\n    actions: [{key: f, cmd: y, pager: true, mode: background}]", "pager needs the terminal: leave out mode (or use interactive)"},
		{"background float", "panels:\n  - id: a\n    source: x\n    actions: [{key: f, cmd: y, float: true}]", "float only applies to interactive and pager actions"},
		{"bad pager", "panels:\n  - id: a\n    source: x\n    actions: [{key: f, cmd: y, pager: [a]}]", "pager must be true or a command"},
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
