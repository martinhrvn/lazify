package engine

import (
	"sort"
	"strings"

	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Select panels are list panels whose selection is chosen in a picker (a
// popup) rather than following the cursor. They are how definitions express
// context such as an AWS profile or region: other panels and the env read
// their choice like any selection ({{region.line}}).

// envFor renders the env for a command about to run. A variable whose
// references have no value yet (a select still loading) is left unset.
func (e *Engine) envFor() map[string]string {
	env := map[string]string{}
	r := e.resolver(nil)
	for k, t := range e.def.Env {
		ready := true
		for _, ref := range t.Refs() {
			if v, ok := r.Resolve(ref); !ok || v == nil {
				ready = false
			}
		}
		if ready {
			env[k], _ = t.Render(r, tmpl.Display)
		}
	}
	return env
}

// key identifies a command's result: the same command under a different env
// (another profile or region) is a different result.
func (e *Engine) key(cmd string) string {
	env := e.envFor()
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k + "=" + env[k] + "\x01")
	}
	return b.String() + "\x00" + cmd
}

// cmdOf returns the command part of a key.
func cmdOf(key string) string {
	_, cmd, _ := strings.Cut(key, "\x00")
	return cmd
}

// IsSelect reports whether id is a select panel.
func (e *Engine) IsSelect(id string) bool {
	p := e.def.Panel(id)
	return p != nil && p.Select
}

// PickerOpen reports whether the top popup is a select's picker.
func (e *Engine) PickerOpen() bool {
	n := len(e.popups)
	return n > 0 && e.popups[n-1].picker
}

// Pick opens select panel id's picker over whatever has focus; closing it
// (enter to choose, esc to cancel) leaves focus where it was.
func (e *Engine) Pick(id string) Effects {
	if e.IsSelect(id) {
		e.openPicker(id)
	}
	return e.take()
}

func (e *Engine) openPicker(id string) {
	ps := e.panels[id]
	ps.pick = ps.cursor
	e.popups = append(e.popups, popupLevel{id: id, picker: true})
}

// choose commits the picker's choice: the select's selection changes and
// everything reading it (dependents, content, the env) follows.
func (e *Engine) choose() {
	top := e.popups[len(e.popups)-1]
	e.popups = e.popups[:len(e.popups)-1]
	ps := e.panels[top.id]
	if ps.pick == ps.cursor || ps.pick >= len(ps.rows) {
		return
	}
	ps.cursor, ps.moved = ps.pick, true
	e.propagate(ps.def.ID, true)
	e.evaluateContent(true)
}

// valueRows are the rows of a panel written with `values:`.
func valueRows(ps *panelState) []rows.Row {
	rs, _ := ps.def.Parser.Parse([]byte(strings.Join(ps.def.Values, "\n")))
	return rs
}

// initialChoice picks the row a panel starts on, once rows are known and the
// user hasn't moved: --set, else a select's default (matched against the
// row's key or label), else its mark.
func (e *Engine) initialChoice(ps *panelState) {
	if ps.jumped || ps.moved || len(ps.rows) == 0 {
		return
	}
	want, ok := e.set[ps.def.ID]
	if !ok && ps.def.Select {
		want = ps.def.Default
	}
	if want == "" {
		e.jumpToMark(ps)
		return
	}
	ps.jumped = true
	for i := range ps.rows {
		k, _ := e.rowKey(ps, i)
		if k == want || e.rowLabel(ps, i) == want {
			if i != ps.cursor {
				ps.cursor = i
				e.propagate(ps.def.ID, true)
			}
			return
		}
	}
}

// rowLabel is row i as displayed (label, or its columns joined).
func (e *Engine) rowLabel(ps *panelState, i int) string {
	r := e.resolver(ps.rows[i])
	if ps.def.Label != nil {
		s, _ := ps.def.Label.Render(r, tmpl.Display)
		return s
	}
	var cells []string
	for _, c := range ps.def.Columns {
		s, _ := c.Value.Render(r, tmpl.Display)
		cells = append(cells, s)
	}
	return strings.Join(cells, " ")
}
