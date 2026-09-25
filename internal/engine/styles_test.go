package engine

import (
	"reflect"
	"testing"
	"time"

	"github.com/martinhrvn/lazify/internal/def"
)

const stylesDef = `
panels:
  - id: tasks
    source: x
    rows: .[]
    columns:
      - title: Status
        value: "{{.status}}"
        style: {RUNNING: {icon: check, color: ok}, STOPPED: {icon: cross, color: error}, "*": dim}
      - {title: Started, value: "{{.started}}", format: ago}
      - title: Tasks
        value: "{{.run}}/{{.want}}"
        format: bar
        style: {value: "{{.health}}", map: {degraded: red}}
    row_style: {value: "{{.desired}}", map: {STOPPED: dim}}
  - id: branches
    source: y
    format: basename
    style: {value: "{{.state}}", map: {gone: {icon: warn}}}
    rows: .[]
    label: "{{.ref}}"
`

func loadedStyles(t *testing.T) *harness {
	h := newHarness(t, stylesDef)
	h.e.now = func() time.Time { return time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC) }
	h.finish("tasks", `[
	  {"status":"RUNNING","started":"2026-09-25T11:57:00Z","run":1,"want":2,"health":"degraded","desired":"RUNNING"},
	  {"status":"PENDING","started":"2026-09-25T11:59:30Z","run":2,"want":2,"health":"ok","desired":"RUNNING"},
	  {"status":"STOPPED","started":"2026-09-25T10:00:00Z","run":0,"want":0,"health":"ok","desired":"STOPPED"}]`)
	h.finish("branches", `[{"ref":"refs/heads/main","state":"ok"},{"ref":"refs/heads/old","state":"gone"}]`)
	return h
}

func TestCellStylesAndFormats(t *testing.T) {
	h := loadedStyles(t)
	v := h.e.View("tasks")
	want := [][]string{
		{"RUNNING", "3m ago", "▰▰▰▰▰▱▱▱▱▱ 1/2"},
		{"PENDING", "30s ago", "▰▰▰▰▰▰▰▰▰▰ 2/2"},
		{"STOPPED", "2h ago", "0/0"}, // bar can't draw n/0: shown raw
	}
	if !reflect.DeepEqual(v.Columns, want) {
		t.Errorf("cells = %q", v.Columns)
	}
	deco := [][]def.Deco{
		{{Icon: "check", Color: "ok"}, {}, {Color: "red"}},
		{{Color: "dim"}, {}, {}}, // "*" fallback; health ok has no entry
		{{Icon: "cross", Color: "error"}, {}, {}},
	}
	if !reflect.DeepEqual(v.CellDeco, deco) {
		t.Errorf("cell decorations = %+v", v.CellDeco)
	}
	if !reflect.DeepEqual(v.RowDeco, []def.Deco{{}, {}, {Color: "dim"}}) {
		t.Errorf("row decorations = %+v", v.RowDeco)
	}
}

func TestLabelStyleAndFormat(t *testing.T) {
	h := loadedStyles(t)
	v := h.e.View("branches")
	if !reflect.DeepEqual(v.Lines, []string{"main", "old"}) {
		t.Errorf("lines = %q", v.Lines)
	}
	if !reflect.DeepEqual(v.LineDeco, []def.Deco{{}, {Icon: "warn"}}) {
		t.Errorf("label decorations = %+v", v.LineDeco)
	}
}

func TestDecorationsFollowTheFilter(t *testing.T) {
	h := loadedStyles(t)
	h.focus("tasks")
	h.filter("stopped")
	v := h.e.View("tasks")
	if len(v.Columns) != 1 || len(v.CellDeco) != 1 || v.CellDeco[0][0].Icon != "cross" || v.RowDeco[0].Color != "dim" {
		t.Errorf("filtered view = %+v", v)
	}
	h.filter("2h ago") // matches the displayed (formatted) text
	if v := h.e.View("tasks"); len(v.Columns) != 1 || v.Columns[0][0] != "STOPPED" {
		t.Errorf("filter on formatted text = %q", v.Columns)
	}
}
