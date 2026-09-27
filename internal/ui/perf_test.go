package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// raceEnabled: the race detector makes timings meaningless.
var raceEnabled bool

// A cursor move redraws only what is on screen: with a big list and a big
// details output it must stay instant (commands are debounced; drawing is not).
func TestMoveIsCheap(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing test")
	}
	var rowsJSON, detail strings.Builder
	rowsJSON.WriteString("[")
	for i := range 3000 {
		if i > 0 {
			rowsJSON.WriteString(",")
		}
		fmt.Fprintf(&rowsJSON, `{"name":"pkg-%d","size":%d,"current":%v}`, i, i*1000, i == 0)
	}
	rowsJSON.WriteString("]")
	for i := range 5000 {
		fmt.Fprintf(&detail, "\x1b[32m[A.]\x1b[0m  #%d  package-%d  1.2.%d\n", i, i, i)
	}
	src := `
panels:
  - id: list
    source: list
    rows: .[]
    key: .name
    columns:
      - {title: Size, value: "{{.size}}", format: bytes}
      - {title: Name, value: "{{.name}}"}
    row_style: {value: "{{.current}}", map: {"true": {icon: check, color: ok}}}
  - id: main
    content: {default: {tabs: [{name: D, cmd: "detail {{.name}}"}]}}
`
	out := map[string]string{"list": rowsJSON.String(), "detail 'pkg-0'": detail.String()}
	m := start(t, src, &fakeRunner{out: out})
	_ = m.View()
	begin := time.Now()
	const n = 20
	for range n {
		m = key(t, m, "j")
		_ = m.View()
	}
	per := time.Since(begin) / n
	t.Logf("move + redraw: %v", per)
	if per > 4*time.Millisecond {
		t.Errorf("a cursor move takes %v; it should feel instant (< 4ms)", per)
	}
}
