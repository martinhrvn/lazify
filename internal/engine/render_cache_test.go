package engine

import (
	"testing"
	"time"
)

// Drawing a panel must not re-render every row on every frame: rows are
// rendered once and reused until something they show can have changed.
func TestRowsRenderOnce(t *testing.T) {
	src := `
panels:
  - id: pkgs
    source: pkgs
    rows: .[]
    columns:
      - {title: Name, value: "{{.name}}"}
      - {title: Built, value: "{{.at}}", format: ago}
  - id: other
    source: other
    style: {value: "{{pkgs.name}}", map: {a: ok}}   # reads another panel's selection
  - id: main
    content: {default: {tabs: [{name: D, cmd: "d {{.name}}"}]}}
`
	h := newHarness(t, src)
	clock := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	h.e.now = func() time.Time { return clock }
	h.finish("pkgs", `[{"name":"a","at":"2026-09-25T11:59:00Z"},{"name":"b","at":"2026-09-25T11:58:00Z"},{"name":"c","at":"2026-09-25T11:57:00Z"}]`)
	h.finish("other", "x\ny\n")

	renders := func(f func()) int {
		before := h.e.rowRenders
		f()
		return h.e.rowRenders - before
	}
	h.e.View("pkgs")
	if n := renders(func() { h.e.View("pkgs"); h.e.View("pkgs") }); n != 0 {
		t.Errorf("redrawing re-rendered %d rows", n)
	}
	if n := renders(func() { h.move("pkgs", 1); h.e.View("pkgs") }); n != 0 {
		t.Errorf("moving the cursor re-rendered %d rows", n)
	}
	if n := renders(func() { clock = clock.Add(400 * time.Millisecond); h.e.View("pkgs") }); n != 0 {
		t.Errorf("ago only changes by the second: re-rendered %d rows", n)
	}
	if n := renders(func() { clock = clock.Add(time.Second); h.e.View("pkgs") }); n != 3 {
		t.Errorf("a new second should re-render the ago rows once: %d", n)
	}
	if got := h.e.View("pkgs").Columns[0][1]; got != "1m ago" {
		t.Errorf("ago = %q", got)
	}
	// A panel whose style reads pkgs' selection re-renders when it moves.
	h.e.View("other")
	h.move("pkgs", 1)
	if n := renders(func() { h.e.View("other") }); n != 2 {
		t.Errorf("other should re-render after pkgs moved: %d", n)
	}
	// New rows are rendered afresh.
	h.apply(h.e.Refresh())
	h.focus("other")
	h.apply(h.e.Refresh())
	h.finish("other", "z\n")
	if v := h.e.View("other"); len(v.Lines) != 1 || v.Lines[0] != "z" {
		t.Errorf("other = %+v", v.Lines)
	}
}
