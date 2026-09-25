package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/engine"
)

// region is where a panel's box was drawn, recorded by View for mouse
// hit-testing. x, y, w, h are the box's outer bounds.
type region struct {
	id, slot   string // the panel drawn, and the slot it sits in ("" in a popup)
	popup      bool
	x, y, w, h int
	picker     bool // the popup is a select's picker (rows are PickerView's)
}

func (r region) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// hit returns the region under (x, y); popups are drawn last, so they win.
func (m Model) hit(x, y int) (region, bool) {
	rs := *m.regions
	for i := len(rs) - 1; i >= 0; i-- {
		if rs[i].contains(x, y) {
			return rs[i], true
		}
	}
	return region{}, false
}

// listGeometry mirrors listLines: how many fixed lines (errors, column
// header) precede the rows, and the index of the first visible row.
func listGeometry(v engine.PanelView, h int) (head, offset int) {
	if v.Blocked != "" {
		return h, 0 // no rows
	}
	if v.Err != "" {
		head = min(len(strings.Split(v.Err, "\n")), max(1, h/2))
	}
	if len(v.Headers) > 0 {
		head++
	}
	return head, max(0, v.Cursor-max(1, h-head)+1)
}

const wheelLines = 3 // content lines scrolled per wheel notch

// mouse handles clicks and the wheel: a click focuses the panel under it and
// selects the clicked row; clicking the selected row is Enter.
func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -1
	case tea.MouseButtonWheelDown:
		wheel = 1
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}
	switch m.modal {
	case modalNone:
	case modalHelp:
		if wheel != 0 {
			m.helpVP.scroll(wheel, len(m.helpLines()), m.helpVP.height)
		}
		return m, nil
	default:
		return m, nil // prompts and confirmations want the keyboard
	}

	r, ok := m.hit(msg.X, msg.Y)
	_, popupOpen := m.eng.Popup()
	if !ok || (popupOpen && !r.popup) || (m.eng.PickerOpen() && r.id != m.eng.Focused()) {
		return m, nil // outside everything, or outside the open popup / picker
	}

	if m.eng.IsContent(r.id) {
		if wheel != 0 {
			m.scrollContent(r.id, wheel*wheelLines, false)
			return m, nil
		}
		if r.slot != "" {
			return m, m.apply(m.eng.FocusPanel(r.slot))
		}
		return m, nil
	}

	var cmds []tea.Cmd
	if r.slot != "" && m.eng.Focused() != r.id {
		if m.eng.IsSelect(r.slot) && wheel == 0 {
			return m, m.apply(m.eng.Pick(r.slot)) // a click on a select opens its picker
		}
		cmds = append(cmds, m.apply(m.eng.FocusPanel(r.slot))) // then select the clicked row below
	}
	if wheel != 0 {
		return m, tea.Batch(append(cmds, m.apply(m.eng.Move(wheel)))...)
	}

	v := m.eng.View(r.id)
	if r.picker {
		v = m.eng.PickerView(r.id)
	}
	head, offset := listGeometry(v, r.h-2)
	row := msg.Y - (r.y + 1) - head + offset
	if row < 0 || row >= len(v.Lines)+len(v.Columns) {
		return m, tea.Batch(cmds...)
	}
	if row == v.Cursor && len(cmds) == 0 {
		return m, m.apply(m.eng.Enter()) // clicking the selected row
	}
	return m, tea.Batch(append(cmds, m.apply(m.eng.Move(row-v.Cursor)))...)
}
