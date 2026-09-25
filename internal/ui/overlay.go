package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
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
	if pv.Picker { // sized to its choices
		pick := m.eng.PickerView(pv.ID)
		lines := pick.Lines
		w := ansi.StringWidth(pv.Crumbs[0]) + 8
		for _, l := range lines {
			w = max(w, ansi.StringWidth(l)+6)
		}
		w = min(max(w, 30), m.width)
		h := min(max(len(lines), 1)+2, max(3, bodyH*70/100))
		return box(pv.Crumbs[0], m.listLines(pick, true, w-2, h-2), w, h-2, true)
	}
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

// within renders s in style st even when s has styled parts of its own (an
// underlined tab, a coloured "● live"). Each inner part ends with a reset that
// would switch st off for the rest of s — and lipgloss underlines one
// character at a time, so only the first letter kept st — so st is re-applied
// after every reset.
func within(st lipgloss.Style, s string) string {
	const reset = "\x1b[0m"
	open := strings.TrimSuffix(st.Render("x"), "x"+reset)
	if open == "" || open == "x" {
		return st.Render(s) // no styling (e.g. not a terminal)
	}
	return open + strings.ReplaceAll(s, reset, reset+open) + reset
}
