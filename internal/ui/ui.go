// Package ui is the Bubble Tea front end. It owns layout, keys and command
// execution, and delegates all state decisions to the engine.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/engine"
	"github.com/martinhrvn/lazify/internal/runner"
)

var (
	colorFocus  = lipgloss.Color("2")
	colorBorder = lipgloss.Color("8")
	styleCursor = lipgloss.NewStyle().Reverse(true)
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleHeader = lipgloss.NewStyle().Bold(true)
	styleHint   = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

// Model is the root Bubble Tea model.
type Model struct {
	def     *def.Definition
	eng     *engine.Engine
	runner  runner.Runner
	cancels map[uint64]context.CancelFunc
	width   int
	height  int
}

type finishedMsg struct {
	id     uint64
	stdout []byte
	err    error
}

// New creates the model. ctx overrides context defaults.
func New(d *def.Definition, r runner.Runner, ctx map[string]string) Model {
	return Model{
		def:     d,
		eng:     engine.New(d, ctx),
		runner:  r,
		cancels: map[uint64]context.CancelFunc{},
	}
}

func (m Model) Init() tea.Cmd { return m.exec(m.eng.Start()) }

// exec turns engine runs into commands that report back with finishedMsg.
func (m Model) exec(runs []engine.Run) tea.Cmd {
	var cmds []tea.Cmd
	for _, r := range runs {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancels[r.ID] = cancel
		cmds = append(cmds, func() tea.Msg {
			res, err := m.runner.Run(ctx, r.Req)
			return finishedMsg{id: r.ID, stdout: res.Stdout, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case finishedMsg:
		if cancel, ok := m.cancels[msg.id]; ok {
			cancel()
			delete(m.cancels, msg.id)
		}
		return m, m.exec(m.eng.Finished(msg.id, msg.stdout, msg.err))
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "q", "ctrl+c":
		for _, cancel := range m.cancels {
			cancel()
		}
		return m, tea.Quit
	case "j", "down":
		return m, m.exec(m.eng.Move(1))
	case "k", "up":
		return m, m.exec(m.eng.Move(-1))
	case "tab":
		m.eng.FocusNext()
	case "shift+tab":
		m.eng.FocusPrev()
	case "r":
		return m, m.exec(m.eng.Refresh())
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < len(m.eng.TopLevel()) {
			m.eng.FocusPanel(m.eng.TopLevel()[i])
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyH := m.height - 1
	var left, right []string
	for _, id := range m.eng.TopLevel() {
		if m.def.Panel(id).Side == "right" {
			right = append(right, id)
		} else {
			left = append(left, id)
		}
	}

	lay := m.def.Layout
	leftW := m.width * lay.LeftWidth / 100
	rightW := 0
	if len(right) > 0 {
		rightW = m.width * lay.RightWidth / 100
	}
	mainW := m.width - leftW - rightW
	if mainW < 10 { // too narrow for main: give its space to the left column
		leftW, mainW = leftW+mainW, 0
	}

	cols := []string{m.column(left, leftW, bodyH)}
	if mainW > 0 {
		cols = append(cols, m.mainView(mainW, bodyH))
	}
	if rightW > 0 {
		cols = append(cols, m.column(right, rightW, bodyH))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" + m.statusLine()
}

// column stacks panels in a box each, sized by the definition's layout rules,
// clipped or padded to exactly h lines.
func (m Model) column(ids []string, w, h int) string {
	if len(ids) == 0 {
		return clip("", w, h)
	}
	slots := make([]slot, len(ids))
	for i, id := range ids {
		slots[i] = slot{
			size:    m.def.Panel(id).Size,
			content: m.contentLines(id),
			focused: id == m.eng.Focused(),
		}
	}
	hs := heights(h-2*len(ids), slots, m.def.Layout.Focus) // each box has 2 border lines
	all := m.eng.TopLevel()
	boxes := make([]string, len(ids))
	for i, id := range ids {
		title := fmt.Sprintf("[%d] %s", slices.Index(all, id)+1, m.eng.View(id).Title)
		boxes[i] = box(title, m.panelLines(id, w-2, hs[i]), w, hs[i], id == m.eng.Focused())
	}
	return clip(lipgloss.JoinVertical(lipgloss.Left, boxes...), w, h)
}

// clip cuts or pads s to exactly h lines of width w.
func clip(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return strings.Join(lines, "\n")
}

func (m Model) contentLines(id string) int {
	v := m.eng.View(id)
	n := max(len(v.Lines), len(v.Columns))
	if len(v.Headers) > 0 {
		n++
	}
	if v.Err != "" {
		n++
	}
	return n
}

// panelLines renders a panel's content, scrolled so the cursor is visible.
func (m Model) panelLines(id string, w, h int) []string {
	v := m.eng.View(id)
	var head, rows []string
	switch {
	case v.Blocked != "":
		return []string{styleDim.Render(v.Blocked)}
	case v.Err != "":
		for _, l := range strings.Split(v.Err, "\n") {
			head = append(head, styleErr.Render(l))
		}
		head = head[:min(len(head), max(1, h/2))]
	case v.Loading && len(v.Lines)+len(v.Columns) == 0:
		return []string{styleDim.Render("loading…")}
	}

	if len(v.Headers) > 0 {
		widths := columnWidths(v.Headers, v.Columns)
		head = append(head, styleHeader.Render(alignRow(v.Headers, widths)))
		for _, cells := range v.Columns {
			rows = append(rows, alignRow(cells, widths))
		}
	} else {
		rows = v.Lines
	}

	avail := max(1, h-len(head))
	offset := max(0, v.Cursor-avail+1)
	out := head
	for i := offset; i < len(rows) && i < offset+avail; i++ {
		line := ansi.Truncate(rows[i], w, "…")
		switch {
		case i == v.Cursor && id == m.eng.Focused():
			line = styleCursor.Render(padRight(line, w))
		case v.Stale:
			line = styleDim.Render(line)
		}
		out = append(out, line)
	}
	if len(rows) == 0 && v.Err == "" && !v.Loading {
		out = append(out, styleDim.Render("(empty)"))
	}
	return out
}

// mainView shows the focused panel's selected row. Detail tabs come in M3.
func (m Model) mainView(w, h int) string {
	var lines []string
	if sel, ok := m.eng.Selection(m.eng.Focused()); ok {
		b, _ := json.MarshalIndent(sel, "", "  ")
		lines = strings.Split(string(b), "\n")
	}
	return clip(box("Main", lines, w, max(0, h-2), false), w, h)
}

func (m Model) statusLine() string {
	hints := []string{"j/k move", "tab focus", "r refresh", "q quit"}
	return ansi.Truncate(styleHint.Render(strings.Join(hints, " · ")), m.width, "…")
}

// box draws lines in a rounded border of total width w and inner height h,
// with the title embedded in the top border.
func box(title string, lines []string, w, h int, focused bool) string {
	color := colorBorder
	if focused {
		color = colorFocus
	}
	bs := lipgloss.NewStyle().Foreground(color)
	innerW := max(0, w-2)

	title = ansi.Truncate(" "+title+" ", max(0, innerW-1), "…")
	top := "╭─" + title + strings.Repeat("─", max(0, innerW-1-ansi.StringWidth(title))) + "╮"
	if focused {
		top = bs.Bold(true).Render(top)
	} else {
		top = bs.Render(top)
	}

	var b strings.Builder
	b.WriteString(top)
	for i := range h {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], innerW, "…")
		}
		b.WriteString("\n" + bs.Render("│") + padRight(line, innerW) + bs.Render("│"))
	}
	b.WriteString("\n" + bs.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return b.String()
}

func padRight(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func columnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = ansi.StringWidth(h)
	}
	for _, r := range rows {
		for i, c := range r {
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
	}
	return widths
}

func alignRow(cells []string, widths []int) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		if i == len(cells)-1 {
			parts[i] = c
		} else {
			parts[i] = padRight(c, widths[i])
		}
	}
	return strings.Join(parts, "  ")
}
