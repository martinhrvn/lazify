package def

import (
	"strings"
	"testing"
)

const styleDef = `
panels:
  - id: tasks
    source: x
    rows: .[]
    columns:
      - title: Status
        value: "{{.lastStatus}}"
        style:
          RUNNING: {icon: check, color: ok}
          PROVISIONING: {spinner: true, color: warn}
          STOPPED: {icon: cross, color: error, text: false, bold: true}
          "*": dim
      - {title: Started, value: "{{.startedAt}}", format: ago}
      - title: Health
        value: "{{.running}}/{{.desired}}"
        format: bar
        style: {value: "{{.health}}", map: {degraded: red}}
    row_style: {value: "{{.desiredStatus}}", map: {STOPPED: dim}}
  - id: branches
    source: y
    format: basename
    style: {value: "{{.state}}", map: {gone: {icon: warn, color: warn}}}
`

func TestStyles(t *testing.T) {
	d, err := Parse([]byte(styleDef), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cols := d.Panel("tasks").Columns
	st := cols[0].Style
	if st == nil || st.Value != nil {
		t.Fatalf("status style = %+v (a bare map keys on the column's own value)", st)
	}
	if got := st.Map["RUNNING"]; got != (Deco{Icon: "check", Color: "ok"}) {
		t.Errorf("RUNNING = %+v", got)
	}
	if got := st.Map["PROVISIONING"]; got != (Deco{Spinner: true, Color: "warn"}) {
		t.Errorf("PROVISIONING = %+v", got)
	}
	if got := st.Map["STOPPED"]; got != (Deco{Icon: "cross", Color: "error", HideText: true, Bold: true}) {
		t.Errorf("STOPPED = %+v", got)
	}
	if st.Default == nil || *st.Default != (Deco{Color: "dim"}) {
		t.Errorf("* = %+v", st.Default)
	}
	if cols[1].Format != "ago" || cols[2].Format != "bar" {
		t.Errorf("formats = %q %q", cols[1].Format, cols[2].Format)
	}
	if hs := cols[2].Style; hs.Value == nil || hs.Value.Source() != "{{.health}}" || hs.Map["degraded"] != (Deco{Color: "red"}) {
		t.Errorf("health style = %+v", hs)
	}
	if rs := d.Panel("tasks").RowStyle; rs == nil || rs.Map["STOPPED"] != (Deco{Color: "dim"}) {
		t.Errorf("row style = %+v", rs)
	}
	b := d.Panel("branches")
	if b.Format != "basename" || b.Style == nil || b.Style.Map["gone"] != (Deco{Icon: "warn", Color: "warn"}) {
		t.Errorf("label style/format = %+v %q", b.Style, b.Format)
	}
}

func TestLookup(t *testing.T) {
	d, _ := Parse([]byte(styleDef), "t.yaml")
	st := d.Panel("tasks").Columns[0].Style
	if deco, ok := st.Lookup("RUNNING"); !ok || deco.Icon != "check" {
		t.Errorf("RUNNING = %+v %v", deco, ok)
	}
	if deco, ok := st.Lookup("WHATEVER"); !ok || deco.Color != "dim" {
		t.Errorf("fallback = %+v %v", deco, ok)
	}
	health := d.Panel("tasks").Columns[2].Style
	if _, ok := health.Lookup("fine"); ok {
		t.Error("no match and no * means no decoration")
	}
}

func TestStyleErrors(t *testing.T) {
	base := "panels:\n  - id: a\n    source: x\n    rows: .[]\n"
	tests := []struct{ name, src, want string }{
		{"unknown icon", "columns: [{title: S, value: v, style: {X: {icon: rocket}}}]", `unknown icon "rocket"`},
		{"unknown colour", "columns: [{title: S, value: v, style: {X: purple}}]", `unknown colour "purple"`},
		{"unknown field", "columns: [{title: S, value: v, style: {X: {icon: check, blink: true}}}]", `unknown field "blink"`},
		{"icon and spinner", "columns: [{title: S, value: v, style: {X: {icon: check, spinner: true}}}]", "icon and spinner"},
		{"unknown format", "columns: [{title: S, value: v, format: shout}]", `unknown format "shout"`},
		{"row_style bare map", "row_style: {STOPPED: dim}", "row_style needs {value: ..., map: ...}"},
		{"label style bare map", "style: {gone: red}", "style needs {value: ..., map: ...}"},
		{"style value bad ref", "row_style: {value: '{{nope.x}}', map: {a: red}}", `unknown panel "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(base+"    "+tt.src+"\n"), "t.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant %q", err, tt.want)
			}
		})
	}
	_, err := Parse([]byte("panels:\n  - {id: a, source: x}\n  - {id: m, format: ago, content: {a: {tabs: [{name: n, cmd: c}]}}}"), "t.yaml")
	if err == nil || !strings.Contains(err.Error(), "format only applies to list panels") {
		t.Errorf("content panel format: %v", err)
	}
}
