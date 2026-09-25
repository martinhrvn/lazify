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
	picker        bool // a select's picker (see Pick)
	inline        bool // …drawn in the select's own box, not as a dialog
	width, height int  // percent of the screen
	full          bool
}

// PopupView describes the open popup.
type PopupView struct {
	ID            string
	Picker        bool // a select's picker: sized to its choices
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

// FocusedSlot is the top-level slot that has focus (under any popup).
func (e *Engine) FocusedSlot() string { return e.slot() }

// Tabs lists the panels sharing slot (the slot's own panel first).
func (e *Engine) Tabs(slot string) []string { return e.def.Tabs(slot) }

// ActiveTab returns the panel tab shown in slot.
func (e *Engine) ActiveTab(slot string) string { return e.Tabs(slot)[e.slotTab[slot]] }

// Top returns the panel shown in slot: the deepest entered level of its
// active tab. Drill-down stacks belong to tabs, so they survive tab switches.
func (e *Engine) Top(slot string) string {
	tab := e.ActiveTab(slot)
	if st := e.stacks[tab]; len(st) > 0 {
		return st[len(st)-1]
	}
	return tab
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
	switch f := p.Enter.Focus; {
	case f == "next":
		return "next panel", true
	case f != "":
		return e.def.Panel(f).Title, true
	}
	return e.def.Panel(p.Enter.Panel).Title, true
}

// CanGoBack reports whether Esc has a level or popup to close.
func (e *Engine) CanGoBack() bool {
	return len(e.popups) > 0 || len(e.stacks[e.ActiveTab(e.slot())]) > 0
}

// Enter opens the focused panel's Enter target for its selected row: in the
// same slot (drill down) or in a popup. Inside a popup, a drill target
// replaces the popup's content.
func (e *Engine) Enter() Effects {
	if e.PickerOpen() {
		e.choose()
		return e.take()
	}
	if f := e.Focused(); e.IsSelect(f) {
		e.openPicker(f)
		return e.take()
	}
	p := e.def.Panel(e.Focused())
	if p.Enter == nil || p.IsContent() {
		return e.take()
	}
	switch f := p.Enter.Focus; { // enter: {focus: …} moves focus instead of opening
	case f == "next":
		return e.FocusNext()
	case f != "":
		return e.FocusPanel(f)
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
		tab := e.ActiveTab(e.slot())
		e.stacks[tab] = append(e.stacks[tab], target)
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
	if e.PickerOpen() {
		e.closePicker() // cancel: the choice stays as it was
		return e.take(), true
	}
	if ps := e.panels[e.Focused()]; ps.filter != "" {
		e.setFilter("") // esc clears a filter before leaving a level
		return e.take(), true
	}
	var closed string
	if n := len(e.popups); n > 0 {
		closed, e.popups = e.popups[n-1].id, e.popups[:n-1]
	} else if tab := e.ActiveTab(e.slot()); len(e.stacks[tab]) > 0 {
		st := e.stacks[tab]
		closed, e.stacks[tab] = st[len(st)-1], st[:len(st)-1]
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
	tab := e.ActiveTab(slot)
	return e.crumbs(append([]string{tab}, e.stacks[tab]...))
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
	if top.inline {
		return PopupView{}, false // drawn in place
	}
	if top.picker {
		return PopupView{ID: top.id, Picker: true, Crumbs: []string{e.def.Panel(top.id).Title}}, true
	}
	return PopupView{ID: top.id, Width: top.width, Height: top.height, Full: top.full, Crumbs: e.crumbs(path)[2:]}, true
}
