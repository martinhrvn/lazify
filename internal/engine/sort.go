package engine

import (
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Sorting orders a list panel's rows by one column (a plain list: its label)
// while the app runs. It is a view of the rows: the command's order is kept
// (src) and comes back when unsorted, the cursor stays on its row, and
// nothing re-runs. Columns sort by their value before formatting, so an
// `ago` column sorts by time and `bytes` by size.

// sortColumns is how many columns panel ps can sort by (a plain list: 1).
func sortColumns(ps *panelState) int {
	if n := len(ps.def.Columns); n > 0 {
		return n
	}
	return 1
}

// SortNext sorts the focused list panel by the next (delta 1) or previous
// (-1) column, passing through unsorted, ascending.
func (e *Engine) SortNext(delta int) Effects {
	ps := e.panels[e.Focused()]
	if !sortable(ps) {
		return e.take()
	}
	n := sortColumns(ps) + 1 // "unsorted" (0) and the columns (1…n)
	i := ((ps.sortBy+delta)%n + n) % n
	e.resort(ps, i, false)
	return e.take()
}

// ReverseSort flips the focused list panel's sort direction.
func (e *Engine) ReverseSort() Effects {
	if ps := e.panels[e.Focused()]; sortable(ps) && ps.sortBy > 0 {
		e.resort(ps, ps.sortBy, !ps.sortDesc)
	}
	return e.take()
}

// SortBy sorts panel id by column col; the column it is already sorted by
// flips direction.
func (e *Engine) SortBy(id string, col int) Effects {
	ps := e.panels[id]
	if ps == nil || !sortable(ps) || col < 0 || col >= sortColumns(ps) {
		return e.take()
	}
	e.resort(ps, col+1, col+1 == ps.sortBy && !ps.sortDesc)
	return e.take()
}

func sortable(ps *panelState) bool {
	return ps != nil && !ps.def.IsContent() && !ps.def.IsSelect()
}

// resort orders ps's rows by column by-1 (0: the command's order), keeping the
// cursor on its row.
func (e *Engine) resort(ps *panelState, by int, desc bool) {
	ps.sortBy, ps.sortDesc = by, desc
	key, had := e.rowKey(ps, ps.cursor)
	ps.rows = e.sorted(ps, ps.src)
	ps.rendered = nil
	if had {
		for i := range ps.rows {
			if k, _ := e.rowKey(ps, i); k == key {
				ps.cursor = i
				break
			}
		}
	}
	e.refilter(ps)
}

// sorted returns rs in ps's sort order (rs itself when unsorted).
func (e *Engine) sorted(ps *panelState, rs []rows.Row) []rows.Row {
	if ps.sortBy == 0 || len(rs) < 2 {
		return rs
	}
	keys := make([]string, len(rs))
	for i, row := range rs {
		keys[i] = e.sortKey(ps, row)
	}
	idx := make([]int, len(rs))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(a, b int) int {
		c := 0
		switch {
		case sortLess(keys[a], keys[b]):
			c = -1
		case sortLess(keys[b], keys[a]):
			c = 1
		}
		if ps.sortDesc {
			c = -c
		}
		return c
	})
	out := make([]rows.Row, len(rs))
	for i, j := range idx {
		out[i] = rs[j]
	}
	return out
}

// sortKey is row's value in ps's sort column, before formatting.
func (e *Engine) sortKey(ps *panelState, row rows.Row) string {
	t := ps.def.Label
	if len(ps.def.Columns) > 0 {
		t = ps.def.Columns[ps.sortBy-1].Value
	}
	if t == nil {
		return tmpl.Format(row)
	}
	s, _ := t.Render(e.resolver(row), tmpl.Display)
	return s
}

// sortLess orders two values: as numbers when both are, else as times when
// both are, else as text — case-insensitive, with runs of digits compared as
// numbers (file2 before file10).
func sortLess(a, b string) bool {
	x, errA := strconv.ParseFloat(strings.TrimSpace(a), 64)
	y, errB := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if errA == nil && errB == nil {
		return x < y
	}
	if ta, okA := parseTime(a); okA {
		if tb, okB := parseTime(b); okB {
			return ta.Before(tb)
		}
	}
	return naturalLess(strings.ToLower(a), strings.ToLower(b))
}

func naturalLess(a, b string) bool {
	for a != "" && b != "" {
		ra, rb := []rune(a), []rune(b)
		if unicode.IsDigit(ra[0]) && unicode.IsDigit(rb[0]) {
			na, restA := digits(a)
			nb, restB := digits(b)
			if len(na) != len(nb) { // no leading zeros compared: longer is bigger
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = restA, restB
			continue
		}
		if ra[0] != rb[0] {
			return ra[0] < rb[0]
		}
		a, b = string(ra[1:]), string(rb[1:])
	}
	return len(a) < len(b)
}

// digits splits s into its leading run of digits (without leading zeros) and
// the rest.
func digits(s string) (string, string) {
	i := strings.IndexFunc(s, func(r rune) bool { return !unicode.IsDigit(r) })
	if i < 0 {
		i = len(s)
	}
	n := strings.TrimLeft(s[:i], "0")
	return n, s[i:]
}
