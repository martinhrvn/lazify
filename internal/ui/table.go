package ui

import (
	"slices"

	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/engine"
)

// Table geometry, shared by drawing (listLines) and the mouse (a click on a
// column title sorts by it).

const colGap = 2 // spaces between columns (alignRow)

// sortMark is the indicator after the sorted column's title (or a list's).
func sortMark(v engine.PanelView) string {
	switch {
	case v.SortCol < 0:
		return ""
	case v.SortDesc:
		return " ▼"
	}
	return " ▲"
}

// headers are v's column titles, the sorted one marked.
func headers(v engine.PanelView) []string {
	hs := slices.Clone(v.Headers)
	if v.SortCol >= 0 && v.SortCol < len(hs) {
		hs[v.SortCol] += sortMark(v)
	}
	return hs
}

func cellDeco(v engine.PanelView, i, j int) def.Deco {
	if i < len(v.CellDeco) && j < len(v.CellDeco[i]) {
		return v.CellDeco[i][j]
	}
	return def.Deco{}
}

// tableWidths are v's column widths as drawn: every row (so they don't jump
// while scrolling), titles with the sort mark, cells with their helpers.
func (m Model) tableWidths(v engine.PanelView) []int {
	hs := headers(v)
	widths := make([]int, len(hs))
	for j, h := range hs {
		widths[j] = ansi.StringWidth(h)
	}
	for i, row := range v.Columns {
		for j, c := range row {
			if j < len(widths) {
				widths[j] = max(widths[j], ansi.StringWidth(withHelper(c, cellDeco(v, i, j), m.frame)))
			}
		}
	}
	return widths
}

// tableIndent is the width in front of the first column: the mark gutter and
// the row-icon gutter, when the panel has them.
func tableIndent(v engine.PanelView) int {
	n := 0
	if v.Marked != nil {
		n += 2
	}
	if slices.ContainsFunc(v.RowDeco, func(d def.Deco) bool { return d.Icon != "" || d.Spinner }) {
		n += 2
	}
	return n
}

// columnAt is the column at display column x of a table row (0 = the box's
// inner left edge), or -1 left of the first; the last column runs to the edge.
func (m Model) columnAt(v engine.PanelView, x int) int {
	x -= tableIndent(v)
	if x < 0 {
		return -1
	}
	widths := m.tableWidths(v)
	for j, w := range widths {
		if x < w+colGap || j == len(widths)-1 {
			return j
		}
		x -= w + colGap
	}
	return -1
}
