package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// maxStreamLines bounds a stream tab's buffer; older lines are dropped.
const maxStreamLines = 10000

// ContentView is what the UI needs to draw a content panel.
type ContentView struct {
	// ID changes whenever different content is shown (active panel, tab or
	// command), so the UI knows when to reset scrolling.
	ID     string
	Title  string
	Tabs   []string
	Active int
	Lines  []string
	// Loading: waiting for output. Stale: Lines are for a previous selection.
	Loading, Stale bool
	// Live: a stream is running. Ended: the stream has exited.
	Live, Ended bool
	Err         string
	Empty       string // why there is nothing to show
}

// viewState is what one content panel shows: the active tab of its entry for
// the active list panel.
type viewState struct {
	active  string // list panel whose selection is the row ({{.x}})
	entry   string // content key in use: the active panel's id or "default"
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

// IsContent reports whether id is a content panel.
func (e *Engine) IsContent(id string) bool {
	p := e.def.Panel(id)
	return p != nil && p.IsContent()
}

// contentPanels lists the visible content panels: top-level ones in screen
// order, then content panels opened with Enter.
func (e *Engine) contentPanels() []string {
	var ids []string
	for _, p := range e.def.Panels {
		if p.IsContent() && (p.Parent == "" || e.isOpen(p.ID)) {
			ids = append(ids, p.ID)
		}
	}
	top := e.TopLevel()
	slices.SortStableFunc(ids, func(a, b string) int {
		return rank(top, a) - rank(top, b)
	})
	return ids
}

// ContentPanels lists the visible content panels (including open Enter targets).
func (e *Engine) ContentPanels() []string { return e.contentPanels() }

// rank orders top-level panels by screen position, others after them.
func rank(top []string, id string) int {
	if i := slices.Index(top, id); i >= 0 {
		return i
	}
	return len(top)
}

// anyContent reports whether some content panel shows something for the
// active panel's selection.
func (e *Engine) anyContent() bool {
	for _, id := range e.contentPanels() {
		if _, tabs := e.entry(id); len(tabs) > 0 {
			return true
		}
	}
	return false
}

// Target is the content panel that tab switching and J/K scrolling act on: the
// focused panel if it is a content panel, otherwise the first one ("" if none).
func (e *Engine) Target() string {
	if f := e.Focused(); e.IsContent(f) {
		return f
	}
	if ids := e.contentPanels(); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// entry returns the content key a view uses for the active panel, and its tabs.
func (e *Engine) entry(view string) (string, []*def.Tab) {
	content := e.def.Panel(view).Content
	if c := content[e.active]; c != nil {
		return e.active, c.Tabs
	}
	if c := content["default"]; c != nil {
		return "default", c.Tabs
	}
	return "", nil
}

func tabKey(view, entry string) string { return view + "\x00" + entry }

// NextTab shows the next tab: of the focused slot if it has panel tabs,
// otherwise of the target content panel.
func (e *Engine) NextTab() Effects { return e.shiftTab(1) }

// PrevTab shows the previous tab (see NextTab).
func (e *Engine) PrevTab() Effects { return e.shiftTab(-1) }

func (e *Engine) shiftTab(delta int) Effects {
	// Panel tabs of the focused slot come first; otherwise content tabs.
	if slot := e.slot(); len(e.popups) == 0 && len(e.Tabs(slot)) > 1 {
		n := len(e.Tabs(slot))
		e.slotTab[slot] = ((e.slotTab[slot]+delta)%n + n) % n
		if f := e.Focused(); !e.IsContent(f) {
			e.active = f
		}
		e.evaluateContent(true)
		return e.take()
	}
	view := e.Target()
	if view == "" {
		return e.take()
	}
	entry, tabs := e.entry(view)
	if n := len(tabs); n > 1 {
		k := tabKey(view, entry)
		e.tabIdx[k] = ((e.tabIdx[k]+delta)%n + n) % n
		e.evaluateContent(true)
	}
	return e.take()
}

// evaluateContent brings every content panel up to date with the active
// panel's selection. With run=false, cache misses are left pending.
func (e *Engine) evaluateContent(run bool) {
	for _, id := range e.contentPanels() {
		e.evaluateView(id, e.views[id], run)
	}
}

func (e *Engine) evaluateView(id string, v *viewState, run bool) {
	entry, tabs := e.entry(id)
	tab := e.tabIdx[tabKey(id, entry)]
	if v.active != e.active || v.entry != entry || v.tab != tab || len(tabs) == 0 {
		e.cancelView(v)
		*v = viewState{active: e.active, entry: entry, tab: tab}
	}
	if len(tabs) == 0 {
		v.empty = "nothing for " + e.def.Panel(e.active).Title
		return
	}
	t := tabs[tab]

	// Wait for, or need a selection in, every panel the command reads from.
	var needs []string
	if slices.ContainsFunc(t.Cmd.Refs(), func(r tmpl.Ref) bool { return r.Scope == tmpl.ScopeRow }) {
		needs = append(needs, e.active)
	}
	needs = append(needs, e.def.EnvDeps...) // every command runs with the env
	for _, dep := range append(needs, t.Deps...) {
		dp := e.panels[dep]
		if dp.runID != 0 || dp.pending {
			e.cancelView(v)
			v.pending, v.empty = true, ""
			v.stale = len(v.lines) > 0
			return
		}
		if _, ok := e.selection(dep); !ok {
			e.cancelView(v)
			*v = viewState{active: v.active, entry: entry, tab: tab, empty: "no selection in " + dep}
			return
		}
	}
	sel, _ := e.selection(e.active)
	cmd, err := t.Cmd.Render(e.resolver(sel), tmpl.Shell)
	if err != nil {
		e.cancelView(v)
		v.pending, v.err = false, err.Error()
		return
	}
	k := e.key(cmd) // results are per env + command
	v.empty = ""
	stream := t.Mode == "stream"
	if !v.force {
		switch {
		case v.runID != 0 && v.runCmd == k:
			return // already running it
		case v.cmd == k:
			e.cancelView(v)
			v.pending, v.stale = false, false
			return
		}
	}
	e.cancelView(v)
	if cached, ok := e.dcache[k]; ok && !stream && !v.force {
		v.cmd, v.lines, v.err, v.pending, v.stale = k, cached, "", false, false
		return
	}
	if !run {
		v.pending = true
		v.stale = len(v.lines) > 0
		return
	}

	e.nextID++
	v.runID, v.runCmd, v.stream, v.json = e.nextID, k, stream, t.Format == "json"
	v.pending, v.force, v.ended, v.err = false, false, false, ""
	timeout := e.def.Timeout
	if stream {
		v.cmd, v.lines, v.partial, v.stale = k, nil, "", false
		timeout = 0
	} else {
		v.stale = len(v.lines) > 0
	}
	e.fx.Runs = append(e.fx.Runs, Run{
		ID: v.runID, Panel: id, Stream: stream,
		Req: runner.Request{Cmd: cmd, Env: e.envFor(), Timeout: timeout},
	})
}

// cancelView drops a view's in-flight run. A killed stream's output no longer
// belongs to a live command, so it is kept only as stale content.
func (e *Engine) cancelView(v *viewState) {
	if v.runID == 0 {
		return
	}
	e.fx.Cancel = append(e.fx.Cancel, v.runID)
	v.runID, v.runCmd = 0, ""
	if v.stream {
		v.cmd = ""
		v.stale = len(v.lines) > 0
	}
}

// refreshContent makes a content panel re-run its tab, bypassing the cache.
func (e *Engine) refreshContent(id string) {
	v := e.views[id]
	if _, tabs := e.entry(id); len(tabs) == 0 {
		return
	}
	e.cancelView(v)
	v.force = true
	e.evaluateView(id, v, true)
}

func (e *Engine) contentFinished(v *viewState, stdout []byte, runErr error) {
	cmd := v.runCmd
	v.runID, v.runCmd = 0, ""
	if v.stream {
		v.ended = true
		if runErr != nil && !errors.Is(runErr, context.Canceled) {
			v.err = runErr.Error()
		}
		return
	}
	if runErr != nil {
		v.err = runErr.Error()
		return
	}
	if v.json {
		var buf bytes.Buffer
		if json.Indent(&buf, bytes.TrimSpace(stdout), "", "  ") == nil {
			stdout = buf.Bytes()
		}
	}
	lines := cleanLines(string(stdout))
	e.dcache[cmd] = lines
	v.cmd, v.lines, v.err, v.stale = cmd, lines, "", false
}

// StreamData appends output from running stream id; other ids are ignored.
func (e *Engine) StreamData(id uint64, chunk []byte) {
	for _, v := range e.views {
		if id != 0 && id == v.runID && v.stream {
			v.appendStream(chunk)
		}
	}
}

func (v *viewState) appendStream(chunk []byte) {
	text := v.partial + string(chunk)
	cut := strings.LastIndexByte(text, '\n')
	if cut < 0 {
		v.partial = text
		return
	}
	v.partial = text[cut+1:]
	v.lines = append(v.lines, cleanLines(text[:cut])...)
	if over := len(v.lines) - maxStreamLines; over > 0 {
		v.lines = append([]string(nil), v.lines[over:]...)
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

// ContentView renders content panel id for display.
func (e *Engine) ContentView(id string) ContentView {
	v := e.views[id]
	_, tabs := e.entry(id)
	cv := ContentView{
		ID:      id + "\x00" + v.active + "\x00" + v.entry + "\x00" + v.cmd,
		Title:   e.def.Panel(id).Title,
		Active:  v.tab,
		Lines:   v.lines,
		Loading: v.pending || (v.runID != 0 && !v.stream),
		Stale:   v.stale,
		Live:    v.runID != 0 && v.stream,
		Ended:   v.ended,
		Err:     v.err,
		Empty:   v.empty,
	}
	if v.partial != "" {
		cv.Lines = append(append([]string(nil), v.lines...), cleanLines(v.partial)...)
	}
	for _, t := range tabs {
		cv.Tabs = append(cv.Tabs, t.Name)
	}
	return cv
}
