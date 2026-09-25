package engine

import (
	"strings"

	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// A panel's mark highlights "current" rows: those whose mark path is truthy,
// or whose match value is among the output lines of the mark command. The
// command runs whenever the panel's source runs, and is cached by command.

// startMark runs ps's mark command unless that exact command is already running.
func (e *Engine) startMark(ps *panelState) {
	m := ps.def.Mark
	if m == nil || m.Source == nil {
		return
	}
	cmd, err := m.Source.Render(e.resolver(nil), tmpl.Shell)
	if err != nil {
		ps.marks, ps.markErr = nil, err.Error()
		return
	}
	k := e.key(cmd)
	if ps.markRunID != 0 && ps.markRunCmd == k {
		return
	}
	e.cancelMark(ps)
	e.nextID++
	ps.markRunID, ps.markRunCmd = e.nextID, k
	e.fx.Runs = append(e.fx.Runs, Run{
		ID: ps.markRunID, Panel: ps.def.ID, Mark: true,
		Req: runner.Request{Cmd: cmd, Env: e.envFor(), Timeout: e.def.Timeout},
	})
}

func (e *Engine) cancelMark(ps *panelState) {
	if ps.markRunID != 0 {
		e.fx.Cancel = append(e.fx.Cancel, ps.markRunID)
		ps.markRunID, ps.markRunCmd = 0, ""
	}
}

// cachedMark applies a cached mark result for ps's current inputs, or runs
// the mark command when there is none.
func (e *Engine) cachedMark(ps *panelState) {
	m := ps.def.Mark
	if m == nil || m.Source == nil {
		return
	}
	cmd, err := m.Source.Render(e.resolver(nil), tmpl.Shell)
	if err != nil {
		return
	}
	if set, ok := e.mcache[e.key(cmd)]; ok {
		e.cancelMark(ps)
		ps.marks, ps.markErr = set, ""
		return
	}
	e.startMark(ps)
}

// markFinished stores a mark result; failures just leave the panel unmarked.
func (e *Engine) markFinished(ps *panelState, stdout []byte, runErr error) {
	cmd := ps.markRunCmd
	ps.markRunID, ps.markRunCmd = 0, ""
	if runErr != nil {
		ps.marks, ps.markErr = nil, runErr.Error()
		return
	}
	set := map[string]bool{}
	for line := range strings.SplitSeq(string(stdout), "\n") {
		if l := strings.TrimSpace(line); l != "" {
			set[l] = true
		}
	}
	e.mcache[cmd] = set // cmd is the run's key (env + command)
	ps.marks, ps.markErr = set, ""
	e.initialChoice(ps)
}

// jumpToMark puts the cursor on the first marked row the first time both rows
// and marks are known, unless the user has already moved in the panel.
func (e *Engine) jumpToMark(ps *panelState) {
	if ps.def.Mark == nil || ps.jumped || ps.moved || len(ps.rows) == 0 {
		return
	}
	if ps.def.Mark.Source != nil && ps.marks == nil {
		return // wait for the mark command
	}
	ps.jumped = true
	for i := range ps.rows {
		if e.isMarked(ps, i) {
			if i != ps.cursor {
				ps.cursor = i
				e.propagate(ps.def.ID, true)
			}
			return
		}
	}
}

func (e *Engine) isMarked(ps *panelState, i int) bool {
	m, row := ps.def.Mark, ps.rows[i]
	if m.Source == nil {
		v, _ := tmpl.Lookup(row, m.Path)
		return truthy(v)
	}
	var match string
	if m.Match != nil {
		match, _ = m.Match.Render(e.resolver(row), tmpl.Display)
	} else {
		match, _ = e.rowKey(ps, i)
	}
	return ps.marks[strings.TrimSpace(match)]
}

// truthy: null, false, 0 and blank strings (git's " " for a non-HEAD branch) are false.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return strings.TrimSpace(x) != ""
	}
	return true
}
