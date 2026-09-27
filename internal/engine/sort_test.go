package engine

import (
	"reflect"
	"testing"
)

const sortDef = `
panels:
  - id: svc
    source: svc
    rows: .[]
    key: .name
    columns:
      - {title: Name, value: "{{.name}}"}
      - {title: Mem, value: "{{.mem}}", format: bytes}
      - {title: Up, value: "{{.at}}", format: ago}
  - id: logs
    source: "logs {{svc.name}}"
  - id: branches
    source: branches
`

const svcRows = `[
  {"name":"web2","mem":9000,"at":"2026-09-25T10:00:00+02:00"},
  {"name":"web10","mem":10000,"at":"2026-09-25T09:30:00Z"},
  {"name":"db","mem":512,"at":"2026-09-25T11:00:00+02:00"}]`

func sortHarness(t *testing.T) *harness {
	h := newHarness(t, sortDef)
	h.finish("svc", svcRows)
	h.finish("logs", "l\n")
	h.finish("branches", "main\nfeature/b\nFix-a\n")
	h.focus("svc")
	return h
}

func names(h *harness) []string {
	var out []string
	for _, row := range h.e.View("svc").Columns {
		out = append(out, row[0])
	}
	return out
}

func TestSortCycles(t *testing.T) {
	h := sortHarness(t)
	steps := []struct {
		apply func() Effects
		want  []string
		col   int
		desc  bool
	}{
		{func() Effects { return h.e.SortNext(1) }, []string{"db", "web2", "web10"}, 0, false},  // natural: web2 < web10
		{func() Effects { return h.e.ReverseSort() }, []string{"web10", "web2", "db"}, 0, true}, // ~
		{func() Effects { return h.e.SortNext(1) }, []string{"db", "web2", "web10"}, 1, false},  // Mem: numeric, not "10.0 KiB" text
		{func() Effects { return h.e.SortNext(1) }, []string{"web2", "db", "web10"}, 2, false},  // Up: 08:00Z, 09:00Z, 09:30Z → by time, not text
		{func() Effects { return h.e.SortNext(1) }, []string{"web2", "web10", "db"}, -1, false}, // wraps to unsorted: command order
		{func() Effects { return h.e.SortNext(-1) }, []string{"web2", "db", "web10"}, 2, false}, // < goes back to the last column
	}
	for i, s := range steps {
		if fx := s.apply(); len(fx.Runs) != 0 {
			t.Errorf("step %d: sorting ran %v", i, fx.Runs)
		}
		v := h.e.View("svc")
		if got := names(h); !reflect.DeepEqual(got, s.want) || v.SortCol != s.col || v.SortDesc != s.desc {
			t.Errorf("step %d: %v col %d desc %v; want %v col %d desc %v", i, got, v.SortCol, v.SortDesc, s.want, s.col, s.desc)
		}
	}
}

func TestSortKeepsCursorFilterAndRefresh(t *testing.T) {
	h := sortHarness(t)
	h.move("svc", 1) // web10
	h.doSettle()
	h.finish("logs", "l10\n")
	h.apply(h.e.SortNext(1)) // by name: db, web2, web10
	if v := h.e.View("svc"); names(h)[v.Cursor] != "web10" {
		t.Errorf("cursor on %q, want it kept on web10", names(h)[v.Cursor])
	}
	if _, ok := h.inflight["logs"]; ok {
		t.Error("the selection didn't change: nothing re-runs")
	}
	h.apply(h.e.SetFilter("web"))
	if v := h.e.View("svc"); len(v.Columns) != 2 || v.Columns[0][0] != "web2" {
		t.Errorf("filtered sorted rows = %v", v.Columns)
	}
	h.apply(h.e.SetFilter(""))
	// A refresh keeps the sort.
	h.apply(h.e.Refresh())
	h.finish("svc", `[{"name":"zz","mem":1,"at":"2026-09-25T10:00:00Z"},{"name":"aa","mem":2,"at":"2026-09-25T10:00:00Z"}]`)
	if got := names(h); !reflect.DeepEqual(got, []string{"aa", "zz"}) {
		t.Errorf("after refresh: %v", got)
	}
}

func TestSortByAndLabels(t *testing.T) {
	h := sortHarness(t)
	h.apply(h.e.SortBy("svc", 1))
	h.apply(h.e.SortBy("svc", 1)) // same column again: reversed
	if v := h.e.View("svc"); v.SortCol != 1 || !v.SortDesc || names(h)[0] != "web10" {
		t.Errorf("SortBy twice: %v col %d desc %v", names(h), v.SortCol, v.SortDesc)
	}
	h.apply(h.e.SortBy("svc", 0)) // another column: ascending
	if v := h.e.View("svc"); v.SortCol != 0 || v.SortDesc {
		t.Errorf("SortBy other: col %d desc %v", v.SortCol, v.SortDesc)
	}
	// A plain list sorts by its label, case-insensitively.
	h.focus("branches")
	h.apply(h.e.SortNext(1))
	if got := h.e.View("branches").Lines; !reflect.DeepEqual(got, []string{"feature/b", "Fix-a", "main"}) {
		t.Errorf("branches = %v", got)
	}
	h.apply(h.e.SortNext(1)) // one "column": next is unsorted
	if got := h.e.View("branches").Lines; !reflect.DeepEqual(got, []string{"main", "feature/b", "Fix-a"}) {
		t.Errorf("branches unsorted = %v", got)
	}
}

func TestNaturalLess(t *testing.T) {
	for _, c := range [][2]string{{"a", "B"}, {"file2", "file10"}, {"v1.9", "v1.10"}, {"", "a"}} {
		if !sortLess(c[0], c[1]) || sortLess(c[1], c[0]) {
			t.Errorf("%q should sort before %q", c[0], c[1])
		}
	}
	if !sortLess("9", "10") || !sortLess("-1.5", "2") {
		t.Error("numbers compare as numbers")
	}
}
