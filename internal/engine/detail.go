package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// maxStreamLines bounds a stream tab's buffer; older lines are dropped.
const maxStreamLines = 10000

// DetailView is what the UI needs to draw the main view.
type DetailView struct {
	// ID changes whenever different content is shown (panel, tab or command),
	// so the UI knows when to reset scrolling.
	ID     string
	Tabs   []string
	Active int
	Lines  []string
	// Loading: waiting for output. Stale: Lines are for a previous selection.
	Loading, Stale bool
	// Live: a stream is running. Ended: the stream has exited.
	Live, Ended bool
	Err         string
	Empty       string // why there is nothing to show
	// For panels without tabs: the selected row, shown as-is.
	Row    any
	HasRow bool
}

// detailState is the main view's content for the focused panel's active tab.
type detailState struct {
	panel   string
	tab     int
	cmd     string // command whose output is in lines ("" = none)
	lines   []string
	partial string // incomplete last line of a stream
	runID   uint64
	runCmd  string
	stream  bool
	json    bool
	pending bool // waiting for inputs or the debounce
	stale   bool
	force   bool // next evaluation must re-run, bypassing the cache (refresh)
	ended   bool
	err     string
	empty   string
}

func (e *Engine) tabs(panel string) []*def.Tab {
	if d := e.def.Detail[panel]; d != nil {
		return d.Tabs
	}
	return nil
}

// NextTab shows the focused panel's next detail tab.
func (e *Engine) NextTab() Effects { return e.shiftTab(1) }

// PrevTab shows the focused panel's previous detail tab.
func (e *Engine) PrevTab() Effects { return e.shiftTab(-1) }

func (e *Engine) shiftTab(delta int) Effects {
	p := e.Focused()
	if n := len(e.tabs(p)); n > 1 {
		e.tabIdx[p] = ((e.tabIdx[p]+delta)%n + n) % n
		e.evaluateDetail(true)
	}
	return e.take()
}

// evaluateDetail brings the main view up to date with the focused panel's
// selection. With run=false, cache misses are left pending (cursor movement).
func (e *Engine) evaluateDetail(run bool) {
	panel := e.Focused()
	tabs := e.tabs(panel)
	d := &e.detail
	if d.panel != panel || d.tab != e.tabIdx[panel] || len(tabs) == 0 {
		e.cancelDetail()
		*d = detailState{panel: panel, tab: e.tabIdx[panel]}
	}
	if len(tabs) == 0 {
		return
	}
	tab := tabs[d.tab]

	for _, dep := range append([]string{panel}, tab.Deps...) {
		dp := e.panels[dep]
		if dp.runID != 0 || dp.pending {
			e.cancelDetail()
			d.pending, d.empty = true, ""
			d.stale = len(d.lines) > 0
			return
		}
		if _, ok := e.selection(dep); !ok {
			e.cancelDetail()
			*d = detailState{panel: panel, tab: d.tab, empty: "no selection in " + dep}
			return
		}
	}
	sel, _ := e.selection(panel)
	cmd, err := tab.Cmd.Render(e.resolver(sel), tmpl.Shell)
	if err != nil {
		e.cancelDetail()
		d.pending, d.err = false, err.Error()
		return
	}
	d.empty = ""
	stream := tab.Mode == "stream"
	if !d.force {
		switch {
		case d.runID != 0 && d.runCmd == cmd:
			return // already running it
		case d.cmd == cmd:
			e.cancelDetail()
			d.pending, d.stale = false, false
			return
		}
	}
	e.cancelDetail()
	if cached, ok := e.dcache[cmd]; ok && !stream && !d.force {
		d.cmd, d.lines, d.err, d.pending, d.stale = cmd, cached, "", false, false
		return
	}
	if !run {
		d.pending = true
		d.stale = len(d.lines) > 0
		return
	}

	e.nextID++
	d.runID, d.runCmd, d.stream, d.json = e.nextID, cmd, stream, tab.Format == "json"
	d.pending, d.force, d.ended, d.err = false, false, false, ""
	timeout := e.def.Timeout
	if stream {
		d.cmd, d.lines, d.partial, d.stale = cmd, nil, "", false
		timeout = 0
	} else {
		d.stale = len(d.lines) > 0
	}
	e.fx.Runs = append(e.fx.Runs, Run{
		ID: d.runID, Panel: panel, Detail: true, Stream: stream,
		Req: runner.Request{Cmd: cmd, Env: e.env, Timeout: timeout},
	})
}

// cancelDetail drops the detail's in-flight run. A killed stream's output no
// longer belongs to a live command, so it is kept only as stale content.
func (e *Engine) cancelDetail() {
	d := &e.detail
	if d.runID == 0 {
		return
	}
	e.fx.Cancel = append(e.fx.Cancel, d.runID)
	d.runID, d.runCmd = 0, ""
	if d.stream {
		d.cmd = ""
		d.stale = len(d.lines) > 0
	}
}

// refreshDetail makes the next evaluation re-run the detail, bypassing the cache.
func (e *Engine) refreshDetail() {
	if len(e.tabs(e.Focused())) == 0 {
		return
	}
	e.cancelDetail()
	e.detail.force = true
	e.evaluateDetail(true)
}

func (e *Engine) detailFinished(stdout []byte, runErr error) {
	d := &e.detail
	cmd := d.runCmd
	d.runID, d.runCmd = 0, ""
	if d.stream {
		d.ended = true
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			d.err = runErr.Error()
		}
		return
	}
	if runErr != nil {
		d.err = runErr.Error()
		return
	}
	if d.json {
		var buf bytes.Buffer
		if json.Indent(&buf, bytes.TrimSpace(stdout), "", "  ") == nil {
			stdout = buf.Bytes()
		}
	}
	lines := cleanLines(string(stdout))
	e.dcache[cmd] = lines
	d.cmd, d.lines, d.err, d.stale = cmd, lines, "", false
}

// StreamData appends output from the running stream id; other ids are ignored.
func (e *Engine) StreamData(id uint64, chunk []byte) {
	d := &e.detail
	if id == 0 || id != d.runID || !d.stream {
		return
	}
	text := d.partial + string(chunk)
	cut := strings.LastIndexByte(text, '\n')
	if cut < 0 {
		d.partial = text
		return
	}
	d.partial = text[cut+1:]
	d.lines = append(d.lines, cleanLines(text[:cut])...)
	if over := len(d.lines) - maxStreamLines; over > 0 {
		d.lines = append([]string(nil), d.lines[over:]...)
	}
}

// cleanLines splits output into display lines: carriage-return progress lines
// keep only their final state and tabs become spaces. ANSI codes pass through.
func cleanLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		if j := strings.LastIndexByte(l, '\r'); j >= 0 {
			l = l[j+1:]
		}
		lines[i] = strings.ReplaceAll(l, "\t", "    ")
	}
	return lines
}

// DetailView renders the main view for display.
func (e *Engine) DetailView() DetailView {
	panel := e.Focused()
	d := &e.detail
	tabs := e.tabs(panel)
	if len(tabs) == 0 {
		v := DetailView{ID: panel}
		v.Row, v.HasRow = e.selection(panel)
		if !v.HasRow {
			v.Empty = "no selection"
		}
		return v
	}
	v := DetailView{
		ID:      panel + "\x00" + tabs[d.tab].Name + "\x00" + d.cmd,
		Active:  d.tab,
		Lines:   d.lines,
		Loading: d.pending || (d.runID != 0 && !d.stream),
		Stale:   d.stale,
		Live:    d.runID != 0 && d.stream,
		Ended:   d.ended,
		Err:     d.err,
		Empty:   d.empty,
	}
	if d.partial != "" {
		v.Lines = append(append([]string(nil), d.lines...), cleanLines(d.partial)...)
	}
	for _, t := range tabs {
		v.Tabs = append(v.Tabs, t.Name)
	}
	return v
}
