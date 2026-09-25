package engine

import (
	"strings"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// renderedRow is one row as displayed: formatted text plus the decorations
// its styles chose.
type renderedRow struct {
	cells    []string
	cellDeco []def.Deco
	line     string
	lineDeco def.Deco
	rowDeco  def.Deco
}

// renderRow renders a row's label or columns. Styles look at the value
// before formatting (the data decides); the formatter only changes how it
// reads.
func (e *Engine) renderRow(ps *panelState, row rows.Row) renderedRow {
	r := e.resolver(row)
	var rr renderedRow
	now := e.now()
	if len(ps.def.Columns) > 0 {
		for _, c := range ps.def.Columns {
			raw, _ := c.Value.Render(r, tmpl.Display)
			rr.cells = append(rr.cells, Format(c.Format, raw, now))
			rr.cellDeco = append(rr.cellDeco, lookup(c.Style, raw, r))
		}
	} else {
		raw, _ := ps.def.Label.Render(r, tmpl.Display)
		rr.line = Format(ps.def.Format, raw, now)
		rr.lineDeco = lookup(ps.def.Style, raw, r)
	}
	rr.rowDeco = lookup(ps.def.RowStyle, "", r)
	return rr
}

// lookup finds the decoration for a value: the style's own value template if
// it has one, else own.
func lookup(s *def.Style, own string, r tmpl.Resolver) def.Deco {
	if s == nil {
		return def.Deco{}
	}
	key := own
	if s.Value != nil {
		key, _ = s.Value.Render(r, tmpl.Display)
	}
	d, _ := s.Lookup(key)
	return d
}

// rowText is row i as displayed (formatted label, or columns joined): what a
// filter matches.
func (e *Engine) rowText(ps *panelState, i int) string {
	rr := e.renderRow(ps, ps.rows[i])
	if rr.cells != nil {
		return strings.Join(rr.cells, " ")
	}
	return rr.line
}
