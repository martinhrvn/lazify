package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/engine"
)

// overlay paints fg centered over bg (both multi-line strings of width w).
func overlay(bg, fg string, w int) string {
	base := strings.Split(bg, "\n")
	top := strings.Split(fg, "\n")
	fw := 0
	for _, l := range top {
		fw = max(fw, ansi.StringWidth(l))
	}
	x := max(0, (w-fw)/2)
	y := max(0, (len(base)-len(top))/2)
	for i, l := range top {
		if y+i >= len(base) {
			break
		}
		b := padRight(base[y+i], w)
		base[y+i] = ansi.Cut(b, 0, x) + padRight(l, fw) + ansi.Cut(b, x+fw, w)
	}
	return strings.Join(base, "\n")
}

// panelBox renders panel id at inner height h inside a box of width w: its
// title (crumbs joined, the last one replaced by a content panel's tab bar)
// and its lines.
func (m Model) panelBox(id string, crumbs []string, w, h int) (string, []string) {
	var title string
	var lines []string
	if m.eng.IsContent(id) {
		title, lines = m.contentBox(id, h)
	} else {
		title, lines = m.eng.View(id).Title, m.panelLines(id, w-2, h)
		if len(crumbs) > 0 {
			title = crumbs[len(crumbs)-1] // the slot's tab bar when not drilled in
		}
	}
	if len(crumbs) > 1 {
		title = strings.Join(crumbs[:len(crumbs)-1], " › ") + " › " + title
	}
	return title, lines
}

// popupBox renders the open popup, sized in percent of the screen body.
func (m Model) popupBox(pv engine.PopupView, bodyH int) string {
	w := max(10, m.width*pv.Width/100)
	h := max(3, bodyH*pv.Height/100)
	if pv.Full {
		w, h = m.width, bodyH
	}
	title, lines := m.panelBox(pv.ID, pv.Crumbs, w, h-2)
	return box(title, lines, w, h-2, true)
}

// tabBar renders a slot's panel tabs, the shown one highlighted.
func (m Model) tabBar(tabs []string, active string) string {
	parts := make([]string, len(tabs))
	for i, id := range tabs {
		parts[i] = m.def.Panel(id).Title
		if id == active {
			parts[i] = styleTabActive.Render(parts[i])
		}
	}
	return strings.Join(parts, " │ ")
}
