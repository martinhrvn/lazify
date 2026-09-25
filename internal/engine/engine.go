// Package engine is lazify's state machine: panels, selections and which
// commands to run. It does no I/O — callers execute the returned Runs and
// report back with Finished — so it is tested without a shell or a TUI.
package engine

import (
	"maps"
	"slices"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Run asks the caller to execute a command and report it via Finished(ID, ...).
type Run struct {
	ID    uint64
	Panel string
	Req   runner.Request
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
	Stale   bool   // rows shown are from a previous run
	Err     string // last run's error
	Blocked string // why the panel cannot run yet
}

type panelState struct {
	def     *def.Panel
	rows    []rows.Row
	cursor  int
	runID   uint64 // in-flight run; 0 = none
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
	focus  int // index into TopLevel()
	nextID uint64
}

// New creates an engine. ctx overrides context defaults (e.g. from --set).
func New(d *def.Definition, ctx map[string]string) *Engine {
	e := &Engine{def: d, ctx: map[string]string{}, panels: map[string]*panelState{}}
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
	}
	return e
}

// Start returns the runs for every panel that can run without a selection.
func (e *Engine) Start() []Run {
	var runs []Run
	for _, id := range e.def.Order {
		if r, ok := e.run(e.panels[id]); ok {
			runs = append(runs, r)
		}
	}
	return runs
}

// run renders a panel's source and marks it loading; ok=false if it is blocked.
func (e *Engine) run(ps *panelState) (Run, bool) {
	for _, dep := range ps.def.Deps {
		if _, ok := e.selection(dep); !ok {
			ps.blocked = "no selection in " + dep
			return Run{}, false
		}
	}
	ps.blocked = ""
	cmd, err := ps.def.Source.Render(e.resolver(nil), tmpl.Shell)
	if err != nil {
		ps.err = err.Error()
		return Run{}, false
	}
	e.nextID++
	ps.runID = e.nextID
	ps.stale = len(ps.rows) > 0
	return Run{
		ID:    ps.runID,
		Panel: ps.def.ID,
		Req:   runner.Request{Cmd: cmd, Env: e.env, Timeout: e.def.Timeout},
	}, true
}

// Finished applies a run's result. Results of superseded runs are ignored.
func (e *Engine) Finished(id uint64, stdout []byte, runErr error) []Run {
	var ps *panelState
	for _, p := range e.panels {
		if p.runID == id && id != 0 {
			ps = p
		}
	}
	if ps == nil {
		return nil
	}
	ps.runID = 0
	if runErr != nil {
		ps.err = runErr.Error()
		return nil
	}
	parsed, err := ps.def.Parser.Parse(stdout)
	if err != nil {
		ps.err = err.Error()
		return nil
	}
	prevKey, hadPrev := e.rowKey(ps, ps.cursor)
	ps.rows, ps.err, ps.stale, ps.cursor = parsed, "", false, 0
	if hadPrev {
		for i := range ps.rows {
			if k, _ := e.rowKey(ps, i); k == prevKey {
				ps.cursor = i
				break
			}
		}
	}
	return nil
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

// Move moves the focused panel's cursor by delta, clamped to its rows.
func (e *Engine) Move(delta int) []Run {
	ps := e.panels[e.Focused()]
	if len(ps.rows) == 0 {
		return nil
	}
	ps.cursor = max(0, min(len(ps.rows)-1, ps.cursor+delta))
	return nil
}

// Refresh re-runs the focused panel.
func (e *Engine) Refresh() []Run {
	if r, ok := e.run(e.panels[e.Focused()]); ok {
		return []Run{r}
	}
	return nil
}

// TopLevel lists the panels that own a slot (not drill-in children) in visual
// order: the left column top to bottom, then the right column.
func (e *Engine) TopLevel() []string {
	var left, right []string
	for _, p := range e.def.Panels {
		switch {
		case p.Parent != "":
		case p.Side == "right":
			right = append(right, p.ID)
		default:
			left = append(left, p.ID)
		}
	}
	return append(left, right...)
}

// Focused returns the id of the focused panel.
func (e *Engine) Focused() string { return e.TopLevel()[e.focus] }

// FocusNext focuses the next top-level panel, wrapping around.
func (e *Engine) FocusNext() { e.focus = (e.focus + 1) % len(e.TopLevel()) }

// FocusPrev focuses the previous top-level panel, wrapping around.
func (e *Engine) FocusPrev() {
	n := len(e.TopLevel())
	e.focus = (e.focus - 1 + n) % n
}

// FocusPanel focuses a top-level panel by id.
func (e *Engine) FocusPanel(id string) {
	if i := slices.Index(e.TopLevel(), id); i >= 0 {
		e.focus = i
	}
}

// View renders a panel for display.
func (e *Engine) View(id string) PanelView {
	ps := e.panels[id]
	v := PanelView{
		ID:      id,
		Title:   ps.def.Title,
		Cursor:  ps.cursor,
		Loading: ps.runID != 0,
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
