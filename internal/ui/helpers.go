package ui

import (
	"slices"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/martinhrvn/lazify/internal/def"
)

// Built-in helpers that styles can put in front of a value.
var icons = map[string]string{
	"check": "✓", "cross": "✗", "warn": "⚠", "info": "ℹ", "dot": "●",
	"circle": "○", "pause": "⏸", "clock": "◷", "up": "↑", "down": "↓",
}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// colors maps style colour names (def.Colors) to terminal colours; the
// semantic names are what a theme would remap.
var colors = map[string]lipgloss.Style{
	"ok": fg("2"), "warn": fg("3"), "error": fg("1"), "info": fg("4"),
	"dim": lipgloss.NewStyle().Faint(true), "accent": fg("5"),
	"red": fg("1"), "green": fg("2"), "yellow": fg("3"), "blue": fg("4"),
	"magenta": fg("5"), "cyan": fg("6"), "gray": fg("8"), "white": fg("7"),
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

// decoStyle is the lipgloss style of a decoration (zero: none).
func decoStyle(d def.Deco) (lipgloss.Style, bool) {
	st, ok := colors[d.Color]
	if !ok {
		st = lipgloss.NewStyle()
	}
	if d.Bold {
		st, ok = st.Bold(true), true
	}
	return st, ok
}

// decorate renders text with its decoration: a helper (icon or spinner frame)
// in front, unless text is hidden, and the colour.
func decorate(text string, d def.Deco, frame int) string {
	text = withHelper(text, d, frame)
	if st, ok := decoStyle(d); ok {
		return st.Render(text)
	}
	return text
}

// withHelper is text with its decoration's helper (icon or spinner frame) but
// no colour: what decorate draws, and how wide it is.
func withHelper(text string, d def.Deco, frame int) string {
	helper := icons[d.Icon]
	if d.Spinner {
		helper = spinnerFrames[frame%len(spinnerFrames)]
	}
	switch {
	case helper != "" && d.HideText:
		return helper
	case helper != "":
		return helper + " " + text
	}
	return text
}

// hasSpinner reports whether any decoration in the view animates.
func hasSpinner(decos ...[]def.Deco) bool {
	for _, ds := range decos {
		for _, d := range ds {
			if d.Spinner {
				return true
			}
		}
	}
	return false
}

const (
	spinEvery   = 100 * time.Millisecond
	redrawEvery = 30 * time.Second
)

// spinMsg advances spinners; redrawMsg repaints relative times.
type (
	spinMsg   struct{}
	redrawMsg struct{}
)

// spinnerShown reports whether anything on screen animates: a spinner
// decoration, or a panel still loading.
func (m Model) spinnerShown() bool {
	var ids []string
	for _, slot := range m.eng.TopLevel() {
		ids = append(ids, m.eng.Top(slot))
	}
	if pv, ok := m.eng.Popup(); ok {
		ids = append(ids, pv.ID)
	}
	for _, id := range ids {
		if m.eng.IsContent(id) {
			if v := m.eng.ContentView(id); v.Loading && len(v.Lines) == 0 {
				return true
			}
			continue
		}
		v := m.eng.View(id)
		if v.Loading && len(v.Lines)+len(v.Columns) == 0 {
			return true
		}
		if hasSpinner(v.LineDeco, v.RowDeco) || slices.ContainsFunc(v.CellDeco, func(row []def.Deco) bool { return hasSpinner(row) }) {
			return true
		}
	}
	return false
}

// usesAgo reports whether the definition shows relative times.
func usesAgo(d *def.Definition) bool {
	for _, p := range d.Panels {
		if p.Format == "ago" || slices.ContainsFunc(p.Columns, func(c def.Column) bool { return c.Format == "ago" }) {
			return true
		}
	}
	return false
}
