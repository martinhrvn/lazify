package engine

import (
	"slices"
	"strconv"
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
	rr := e.renderedRows(ps)[i]
	if rr.cells != nil {
		return strings.Join(rr.cells, " ")
	}
	return rr.line
}

// renderedRows returns every row of ps rendered, reusing the last rendering
// until something it shows can have changed: new rows (setRows drops it), the
// selection of a panel its templates read, or — with `ago` — the second.
func (e *Engine) renderedRows(ps *panelState) []renderedRow {
	stamp := e.renderStamp(ps)
	if ps.rendered != nil && ps.renderedFor == stamp {
		return ps.rendered
	}
	out := make([]renderedRow, len(ps.rows))
	for i, row := range ps.rows {
		out[i] = e.renderRow(ps, row)
	}
	e.rowRenders += len(out)
	ps.rendered, ps.renderedFor = out, stamp
	return out
}

// renderStamp identifies what a panel's rendering depends on besides its rows.
func (e *Engine) renderStamp(ps *panelState) string {
	var b strings.Builder
	for _, id := range displayRefs(ps.def) {
		if k, ok := e.rowKey(e.panels[id], e.panels[id].cursor); ok {
			b.WriteString(k)
		}
		b.WriteByte(0)
	}
	if usesAgo(ps.def) {
		b.WriteString(strconv.FormatInt(e.now().Unix(), 10))
	}
	return b.String()
}

// displayRefs lists the other panels a panel's display templates read.
func displayRefs(p *def.Panel) []string {
	var ts []*tmpl.Template
	add := func(t *tmpl.Template) {
		if t != nil {
			ts = append(ts, t)
		}
	}
	styleValue := func(s *def.Style) {
		if s != nil {
			add(s.Value)
		}
	}
	add(p.Label)
	styleValue(p.Style)
	styleValue(p.RowStyle)
	for _, c := range p.Columns {
		add(c.Value)
		styleValue(c.Style)
	}
	var ids []string
	for _, t := range ts {
		for _, r := range t.Refs() {
			switch r.Scope {
			case tmpl.ScopeRow, tmpl.ScopeCtx, tmpl.ScopeInput, p.ID:
			default:
				if !slices.Contains(ids, r.Scope) {
					ids = append(ids, r.Scope)
				}
			}
		}
	}
	return ids
}

func usesAgo(p *def.Panel) bool {
	return p.Format == "ago" || slices.ContainsFunc(p.Columns, func(c def.Column) bool { return c.Format == "ago" })
}
