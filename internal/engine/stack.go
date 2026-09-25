package engine

import (
	"slices"
	"strings"
)

// Drill-down and popups. Each top-level slot has a stack of panels entered
// with Enter (drawn in the slot, top of the stack visible); popups form one
// more stack drawn over everything. Esc pops the top level.

// popupLevel is one entry of the popup stack.
type popupLevel struct {
	id            string
	width, height int // percent of the screen
	full          bool
}

// PopupView describes the open popup.
type PopupView struct {
	ID            string
	Width, Height int
	Full          bool
	Crumbs        []string
}

// slot is the focused top-level slot.
func (e *Engine) slot() string { return e.TopLevel()[e.focus] }

// Focused returns the panel that has focus: the top popup, else the top of the
// focused slot's drill-down stack.
func (e *Engine) Focused() string {
	if n := len(e.popups); n > 0 {
		return e.popups[n-1].id
	}
	return e.Top(e.slot())
}

// Top returns the panel shown in slot: its deepest entered level.
func (e *Engine) Top(slot string) string {
	if st := e.stacks[slot]; len(st) > 0 {
		return st[len(st)-1]
	}
	return slot
}

// isOpen reports whether an Enter target is currently shown.
func (e *Engine) isOpen(id string) bool {
	if slices.ContainsFunc(e.popups, func(p popupLevel) bool { return p.id == id }) {
		return true
	}
	for _, st := range e.stacks {
		if slices.Contains(st, id) {
			return true
		}
	}
	return false
}

// EnterTarget returns the title of what Enter opens from the focused panel.
func (e *Engine) EnterTarget() (string, bool) {
	p := e.def.Panel(e.Focused())
	if p.Enter == nil || p.IsContent() {
		return "", false
	}
	return e.def.Panel(p.Enter.Panel).Title, true
}

// CanGoBack reports whether Esc has a level or popup to close.
func (e *Engine) CanGoBack() bool {
	return len(e.popups) > 0 || len(e.stacks[e.slot()]) > 0
}

// Enter opens the focused panel's Enter target for its selected row: in the
// same slot (drill down) or in a popup. Inside a popup, a drill target
// replaces the popup's content.
func (e *Engine) Enter() Effects {
	p := e.def.Panel(e.Focused())
	if p.Enter == nil || p.IsContent() {
		return e.take()
	}
	if _, ok := e.selection(p.ID); !ok {
		return e.take()
	}
	target := p.Enter.Panel
	switch {
	case p.Enter.Popup:
		e.popups = append(e.popups, popupLevel{id: target, width: p.Enter.Width, height: p.Enter.Height, full: p.Enter.Full})
	case len(e.popups) > 0:
		top := e.popups[len(e.popups)-1]
		e.popups = append(e.popups, popupLevel{id: target, width: top.width, height: top.height, full: top.full})
	default:
		e.stacks[e.slot()] = append(e.stacks[e.slot()], target)
	}

	ps := e.panels[target]
	ps.cursor, ps.moved, ps.jumped = 0, false, false
	if !ps.def.IsContent() {
		e.active = target
	}
	e.evaluate(ps, true)
	e.evaluateContent(true)
	return e.take()
}

// Back closes the top popup, else the focused slot's deepest level. ok is
// false when there was nothing to close.
func (e *Engine) Back() (Effects, bool) {
	var closed string
	if n := len(e.popups); n > 0 {
		closed, e.popups = e.popups[n-1].id, e.popups[:n-1]
	} else if st := e.stacks[e.slot()]; len(st) > 0 {
		closed, e.stacks[e.slot()] = st[len(st)-1], st[:len(st)-1]
	} else {
		return e.take(), false
	}

	ps := e.panels[closed]
	e.cancel(ps)
	e.cancelMark(ps)
	*ps = panelState{def: ps.def} // reopening re-evaluates (the cache makes it instant)
	if v := e.views[closed]; v != nil {
		e.cancelView(v)
		*v = viewState{}
	}
	if f := e.Focused(); !e.IsContent(f) {
		e.active = f
	}
	e.evaluateContent(true)
	return e.take(), true
}

// Crumbs is the path shown as slot's title: each level's title, separated by
// the label of the row it was entered from.
func (e *Engine) Crumbs(slot string) []string {
	return e.crumbs(append([]string{slot}, e.stacks[slot]...))
}

func (e *Engine) crumbs(path []string) []string {
	var out []string
	for i, id := range path {
		out = append(out, e.def.Panel(id).Title)
		if i < len(path)-1 {
			out = append(out, e.selectedLabel(id))
		}
	}
	return out
}

// selectedLabel is how panel id's selected row is displayed.
func (e *Engine) selectedLabel(id string) string {
	v := e.View(id)
	switch {
	case v.Cursor < len(v.Lines):
		return v.Lines[v.Cursor]
	case v.Cursor < len(v.Columns):
		return strings.Join(v.Columns[v.Cursor], " ")
	}
	return ""
}

// Popup describes the top popup, if one is open. Its crumbs run from the
// panel that opened the first popup.
func (e *Engine) Popup() (PopupView, bool) {
	n := len(e.popups)
	if n == 0 {
		return PopupView{}, false
	}
	top := e.popups[n-1]
	path := []string{e.Top(e.slot())}
	for _, p := range e.popups {
		path = append(path, p.id)
	}
	return PopupView{ID: top.id, Width: top.width, Height: top.height, Full: top.full, Crumbs: e.crumbs(path)[2:]}, true
}
