package engine

import (
	"slices"
	"strings"
)

// Filtering narrows the rows a list panel shows to those whose displayed
// text contains every term of the filter (case-insensitive). The filter is a
// view: the cursor moves over visible rows only, and a panel whose rows all
// hide has no selection.

// Filter returns panel id's filter ("" = none).
func (e *Engine) Filter(id string) string { return e.panels[id].filter }

// SetFilter filters the focused list panel (or the open picker's choices).
// A hidden selection moves to the first visible row and propagates like a
// cursor move.
func (e *Engine) SetFilter(text string) Effects {
	e.setFilter(text)
	return e.take()
}

func (e *Engine) setFilter(text string) {
	ps := e.panels[e.Focused()]
	if ps.def.IsContent() {
		return
	}
	if e.PickerOpen() {
		ps.filter = text
		e.refilter(ps)
		if vis := ps.visible; !slices.Contains(vis, ps.pick) && len(vis) > 0 {
			ps.pick = vis[0]
		}
		return
	}
	// Filtering never changes the rows, only which are visible, so the
	// selection changed iff the cursor moved or it (dis)appeared.
	cursor := ps.cursor
	_, had := e.selection(ps.def.ID)
	ps.filter = text
	e.refilter(ps)
	if _, has := e.selection(ps.def.ID); has != had || ps.cursor != cursor {
		ps.moved = true
		e.propagate(ps.def.ID, false)
		e.evaluateContent(false)
		e.settleID++
		e.fx.Settle = e.settleID
	}
}

// refilter recomputes the visible rows and moves a hidden cursor to the first
// visible one.
func (e *Engine) refilter(ps *panelState) {
	ps.visible = nil
	if ps.filter == "" {
		return
	}
	terms := strings.Fields(strings.ToLower(ps.filter))
	ps.visible = []int{} // non-nil: a filter is active, even with no matches
	for i := range ps.rows {
		text := strings.ToLower(e.rowText(ps, i))
		if !slices.ContainsFunc(terms, func(t string) bool { return !strings.Contains(text, t) }) {
			ps.visible = append(ps.visible, i)
		}
	}
	if len(ps.visible) > 0 && !slices.Contains(ps.visible, ps.cursor) {
		ps.cursor = ps.visible[0]
	}
}

// step moves index i (into rows) by delta over the visible rows.
func (ps *panelState) step(i, delta int) int {
	if ps.visible == nil {
		return max(0, min(len(ps.rows)-1, i+delta))
	}
	if len(ps.visible) == 0 {
		return i
	}
	p := max(0, slices.Index(ps.visible, i))
	return ps.visible[max(0, min(len(ps.visible)-1, p+delta))]
}

// pos is row i's position among the visible rows (-1 if hidden).
func (ps *panelState) pos(i int) int {
	if ps.visible == nil {
		return i
	}
	return slices.Index(ps.visible, i)
}

// hidden reports whether row i is filtered out.
func (ps *panelState) hidden(i int) bool {
	return ps.visible != nil && !slices.Contains(ps.visible, i)
}
