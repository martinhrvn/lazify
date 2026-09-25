// Package engine is lazify's state machine: panels, selections and which
// commands to run. It does no I/O — callers carry out the returned Effects and
// report back with Finished and Settle — so it is tested without a shell or a TUI.
//
// Reactivity: a panel whose source references other panels re-renders its
// command whenever their selections change. Results are cached by rendered
// command, so revisiting a selection is instant; cache misses caused by cursor
// movement run only after the caller waits Debounce and calls Settle.
package engine

import (
	"maps"
	"slices"
	"time"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Debounce is how long callers should wait after a Settle request.
const Debounce = 150 * time.Millisecond

// Run asks the caller to execute a command and report it via Finished(ID, ...).
type Run struct {
	ID     uint64
	Panel  string // the panel it is for (list or content panel)
	Stream bool   // long-running: use Runner.Stream and report chunks via StreamData
	Req    runner.Request
}

// Effects is what the caller must do after an engine call.
type Effects struct {
	Runs   []Run
	Cancel []uint64 // runs whose results are no longer wanted; kill them
	Settle uint64   // non-zero: call Settle(Settle) after Debounce
}

// PanelView is what the UI needs to draw a panel.
type PanelView struct {
	ID      string
	Title   string
	Lines   []string   // rendered labels (when the panel has no columns)
	Headers []string   // column titles (when it has columns)
	Columns [][]string // per row, rendered column values
	Cursor  int
	Loading bool
	Stale   bool   // rows shown are not (yet) for the current selection
	Err     string // last run's error
	Blocked string // why the panel cannot run
}

type panelState struct {
	def     *def.Panel
	rows    []rows.Row
	cursor  int
	cmd     string // command that produced rows ("" = none)
	runID   uint64 // in-flight run; 0 = none
	runCmd  string // command of the in-flight run
	pending bool   // needs a run once its inputs settle / the debounce ends
	stale   bool
	err     string
	blocked string
}

// Engine holds the app state for one definition.
type Engine struct {
	def    *def.Definition
	ctx    map[string]string
	env    map[string]string
	panels map[string]*panelState
	// cache maps a rendered command to its rows. The env is fixed for the
	// engine's lifetime, so the command alone is the key.
	cache    map[string][]rows.Row
	views    map[string]*viewState // per content panel
	tabIdx   map[string]int        // active tab per (content panel, entry)
	active   string                // last focused list panel: what content panels show
	dcache   map[string][]string   // once-tab output by rendered command
	focus    int                   // index into TopLevel()
	nextID   uint64
	settleID uint64
	fx       Effects // accumulated by the current call
}

// New creates an engine. ctx overrides context defaults (e.g. from --set).
func New(d *def.Definition, ctx map[string]string) *Engine {
	e := &Engine{
		def:    d,
		ctx:    map[string]string{},
		panels: map[string]*panelState{},
		cache:  map[string][]rows.Row{},
		tabIdx: map[string]int{},
		dcache: map[string][]string{},
		views:  map[string]*viewState{},
	}
	for name, cv := range d.Context {
		e.ctx[name] = cv.Default
	}
	maps.Copy(e.ctx, ctx)
	e.env = map[string]string{}
	for k, t := range d.Env {
		e.env[k], _ = t.Render(e.resolver(nil), tmpl.Display)
	}
	for _, p := range d.Panels {
		e.panels[p.ID] = &panelState{def: p}
		if p.IsContent() {
			e.views[p.ID] = &viewState{}
		}
	}
	for _, id := range e.TopLevel() {
		if !e.IsContent(id) {
			e.active = id
			break
		}
	}
	return e
}

// take returns and resets the effects accumulated by the current call.
func (e *Engine) take() Effects {
	fx := e.fx
	e.fx = Effects{}
	return fx
}

// Start runs every panel that can run; the rest wait for their inputs.
func (e *Engine) Start() Effects {
	for _, id := range e.def.Order {
		e.evaluate(e.panels[id], true)
	}
	e.evaluateContent(true)
	return e.take()
}

// Finished applies a run's result. Results of superseded runs are ignored.
func (e *Engine) Finished(id uint64, stdout []byte, runErr error) Effects {
	for _, v := range e.views {
		if id != 0 && id == v.runID {
			e.contentFinished(v, stdout, runErr)
			return e.take()
		}
	}
	var ps *panelState
	for _, p := range e.panels {
		if id != 0 && p.runID == id {
			ps = p
		}
	}
	if ps == nil {
		return e.take()
	}
	cmd := ps.runCmd
	ps.runID, ps.runCmd = 0, ""
	if runErr != nil {
		ps.err = runErr.Error()
	} else if parsed, err := ps.def.Parser.Parse(stdout); err != nil {
		ps.err = err.Error()
	} else {
		e.cache[cmd] = parsed
		e.setRows(ps, cmd, parsed)
	}
	e.propagate(ps.def.ID, true)
	e.evaluateContent(true)
	return e.take()
}

// Settle runs what cursor movement left pending, unless a newer move superseded it.
func (e *Engine) Settle(id uint64) Effects {
	if id == 0 || id != e.settleID {
		return e.take()
	}
	for _, pid := range e.def.Order {
		e.evaluate(e.panels[pid], true)
	}
	e.evaluateContent(true)
	return e.take()
}

// Move moves the focused panel's cursor by delta, clamped to its rows.
// Dependents are served from the cache at once; misses wait for Settle.
func (e *Engine) Move(delta int) Effects {
	ps := e.panels[e.Focused()]
	if ps.def.IsContent() || len(ps.rows) == 0 {
		return e.take() // content panels scroll in the UI
	}
	cursor := max(0, min(len(ps.rows)-1, ps.cursor+delta))
	if cursor == ps.cursor {
		return e.take()
	}
	ps.cursor = cursor
	hasDeps := len(e.def.Dependents(ps.def.ID)) > 0
	if hasDeps {
		e.propagate(ps.def.ID, false)
	}
	e.evaluateContent(false)
	if hasDeps || e.anyContent() {
		e.settleID++
		e.fx.Settle = e.settleID
	}
	return e.take()
}

// Refresh re-runs the focused panel, bypassing the cache. A list panel also
// re-runs what the content panels show for it; a content panel re-runs its tab.
func (e *Engine) Refresh() Effects {
	ps := e.panels[e.Focused()]
	if ps.def.IsContent() {
		e.refreshContent(ps.def.ID)
		return e.take()
	}
	if ps.blocked != "" || ps.pending {
		return e.take()
	}
	if cmd, ok := e.render(ps); ok {
		e.start(ps, cmd)
	}
	for _, id := range e.contentPanels() {
		e.refreshContent(id)
	}
	return e.take()
}

// propagate re-evaluates every panel downstream of id, in dependency order.
func (e *Engine) propagate(id string, run bool) {
	down := map[string]bool{id: true}
	for _, pid := range e.def.Order {
		if down[pid] {
			continue
		}
		p := e.panels[pid]
		if slices.ContainsFunc(p.def.Deps, func(d string) bool { return down[d] }) {
			down[pid] = true
			e.evaluate(p, run)
		}
	}
}

// evaluate brings one panel up to date with its inputs' selections. With
// run=false, cache misses are left pending instead of started.
func (e *Engine) evaluate(ps *panelState, run bool) {
	if ps.def.Parent != "" || ps.def.IsContent() {
		return // drill-in children run only when opened; content panels have no source
	}
	for _, dep := range ps.def.Deps {
		d := e.panels[dep]
		if d.runID != 0 || d.pending {
			e.cancel(ps)
			ps.pending, ps.blocked = true, ""
			ps.stale = len(ps.rows) > 0
			return
		}
		if _, ok := e.selection(dep); !ok {
			e.cancel(ps)
			*ps = panelState{def: ps.def, blocked: "no selection in " + dep}
			return
		}
	}
	ps.blocked = ""
	cmd, ok := e.render(ps)
	if !ok {
		return
	}
	switch {
	case ps.runID != 0 && ps.runCmd == cmd:
		return // already running it
	case ps.cmd == cmd && ps.cmd != "":
		e.cancel(ps)
		ps.pending, ps.stale = false, false
		return
	}
	e.cancel(ps)
	if cached, ok := e.cache[cmd]; ok {
		ps.pending = false
		e.setRows(ps, cmd, cached)
		return
	}
	if !run {
		ps.pending = true
		ps.stale = len(ps.rows) > 0
		return
	}
	e.start(ps, cmd)
}

// render renders a panel's source; a failure is recorded as the panel's error.
func (e *Engine) render(ps *panelState) (string, bool) {
	cmd, err := ps.def.Source.Render(e.resolver(nil), tmpl.Shell)
	if err != nil {
		e.cancel(ps)
		ps.pending, ps.err = false, err.Error()
		return "", false
	}
	return cmd, true
}

// start begins running cmd for ps, replacing any in-flight run.
func (e *Engine) start(ps *panelState, cmd string) {
	e.cancel(ps)
	e.nextID++
	ps.runID, ps.runCmd, ps.pending = e.nextID, cmd, false
	ps.stale = len(ps.rows) > 0
	e.fx.Runs = append(e.fx.Runs, Run{
		ID:    ps.runID,
		Panel: ps.def.ID,
		Req:   runner.Request{Cmd: cmd, Env: e.env, Timeout: e.def.Timeout},
	})
}

// cancel drops ps's in-flight run, if any.
func (e *Engine) cancel(ps *panelState) {
	if ps.runID != 0 {
		e.fx.Cancel = append(e.fx.Cancel, ps.runID)
		ps.runID, ps.runCmd = 0, ""
	}
}

// setRows shows rows produced by cmd, keeping the cursor on the same key.
func (e *Engine) setRows(ps *panelState, cmd string, rs []rows.Row) {
	prevKey, hadPrev := e.rowKey(ps, ps.cursor)
	ps.rows, ps.cmd, ps.err, ps.stale, ps.cursor = rs, cmd, "", false, 0
	if hadPrev {
		for i := range ps.rows {
			if k, _ := e.rowKey(ps, i); k == prevKey {
				ps.cursor = i
				break
			}
		}
	}
}

// rowKey is the stable identity of row i: its key path, else its label.
func (e *Engine) rowKey(ps *panelState, i int) (string, bool) {
	if i < 0 || i >= len(ps.rows) {
		return "", false
	}
	row := ps.rows[i]
	if ps.def.Key != nil {
		v, _ := tmpl.Lookup(row, ps.def.Key)
		return tmpl.Format(v), true
	}
	if ps.def.Label != nil {
		s, _ := ps.def.Label.Render(e.resolver(row), tmpl.Display)
		return s, true
	}
	return tmpl.Format(row), true
}

// Selection returns the row under the cursor of panel id.
func (e *Engine) Selection(id string) (rows.Row, bool) { return e.selection(id) }

// selection returns the row under the cursor of panel id.
func (e *Engine) selection(id string) (rows.Row, bool) {
	ps := e.panels[id]
	if ps == nil || ps.cursor >= len(ps.rows) {
		return nil, false
	}
	return ps.rows[ps.cursor], true
}

// TopLevel lists the panels that own a slot (not drill-in children) in visual
// order: the left column top to bottom, then the center, then the right.
func (e *Engine) TopLevel() []string {
	var left, center, right []string
	for _, p := range e.def.Panels {
		switch {
		case p.Parent != "":
		case p.Side == "right":
			right = append(right, p.ID)
		case p.Side == "center":
			center = append(center, p.ID)
		default:
			left = append(left, p.ID)
		}
	}
	return slices.Concat(left, center, right)
}

// Focused returns the id of the focused panel.
func (e *Engine) Focused() string { return e.TopLevel()[e.focus] }

// FocusNext focuses the next top-level panel, wrapping around.
func (e *Engine) FocusNext() Effects { return e.setFocus(e.focus + 1) }

// FocusPrev focuses the previous top-level panel, wrapping around.
func (e *Engine) FocusPrev() Effects { return e.setFocus(e.focus - 1) }

// FocusPanel focuses a top-level panel by id.
func (e *Engine) FocusPanel(id string) Effects {
	if i := slices.Index(e.TopLevel(), id); i >= 0 {
		return e.setFocus(i)
	}
	return e.take()
}

// setFocus focuses TopLevel()[i] (wrapping). Focusing a list panel makes it the
// active one, and content panels switch to it at once.
func (e *Engine) setFocus(i int) Effects {
	n := len(e.TopLevel())
	e.focus = ((i % n) + n) % n
	if id := e.Focused(); !e.IsContent(id) {
		e.active = id
	}
	e.evaluateContent(true)
	return e.take()
}

// View renders a panel for display.
func (e *Engine) View(id string) PanelView {
	ps := e.panels[id]
	v := PanelView{
		ID:      id,
		Title:   ps.def.Title,
		Cursor:  ps.cursor,
		Loading: ps.runID != 0 || ps.pending,
		Stale:   ps.stale,
		Err:     ps.err,
		Blocked: ps.blocked,
	}
	for _, c := range ps.def.Columns {
		v.Headers = append(v.Headers, c.Title)
	}
	for _, row := range ps.rows {
		r := e.resolver(row)
		if len(ps.def.Columns) > 0 {
			cells := make([]string, len(ps.def.Columns))
			for i, c := range ps.def.Columns {
				cells[i], _ = c.Value.Render(r, tmpl.Display)
			}
			v.Columns = append(v.Columns, cells)
		} else {
			s, _ := ps.def.Label.Render(r, tmpl.Display)
			v.Lines = append(v.Lines, s)
		}
	}
	return v
}

// resolver resolves refs against row (for `.x`), panel selections and ctx.
func (e *Engine) resolver(row rows.Row) tmpl.Resolver {
	return resolverFunc(func(ref tmpl.Ref) (any, bool) {
		var root any
		switch ref.Scope {
		case tmpl.ScopeRow:
			if row == nil {
				return nil, false
			}
			root = row
		case tmpl.ScopeCtx:
			v, ok := e.ctx[ref.Path[0]]
			return v, ok
		case tmpl.ScopeInput:
			return nil, false
		default:
			sel, ok := e.selection(ref.Scope)
			if !ok {
				return nil, false
			}
			root = sel
		}
		return tmpl.Lookup(root, ref.Path)
	})
}

type resolverFunc func(tmpl.Ref) (any, bool)

func (f resolverFunc) Resolve(r tmpl.Ref) (any, bool) { return f(r) }
