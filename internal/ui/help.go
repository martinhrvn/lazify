package ui

import (
	"fmt"

	"github.com/charmbracelet/x/ansi"
)

// helpLines lists every key available right now: the focused panel's actions,
// the global ones (marking those it overrides) and the built-in navigation.
func (m Model) helpLines() []string {
	var own, global []string
	for _, b := range m.eng.Bindings() {
		line := fmt.Sprintf("  %-10s %s", b.Key, b.Desc)
		switch {
		case !b.Global:
			own = append(own, line)
		case b.Overridden:
			global = append(global, styleDim.Render(line+" (overridden)"))
		default:
			global = append(global, line)
		}
	}
	var lines []string
	section := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, styleHeader.Render(title))
		lines = append(lines, items...)
	}
	section(m.def.Panel(m.eng.Focused()).Title, own)
	section("Global", global)
	var nav []string
	for _, h := range navHints {
		nav = append(nav, fmt.Sprintf("  %-10s %s", h[0], h[1]))
	}
	section("Navigation", nav)
	return lines
}

// helpBox renders the help popup for a screen of w×h.
func (m Model) helpBox(w, h int) string {
	lines := m.helpLines()
	bw := 4
	for _, l := range lines {
		bw = max(bw, ansi.StringWidth(l)+4)
	}
	bw = min(bw, w-2)
	inner := min(len(lines), max(1, h-4))
	shown := m.helpVP.window(lines, inner)
	pad := make([]string, len(shown))
	for i, l := range shown {
		pad[i] = " " + l
	}
	title := "Keys · esc close"
	if len(lines) > inner {
		title = "Keys · j/k scroll · esc close"
	}
	return box(title, pad, bw, inner, true)
}
