// Package ui is the Bubble Tea front end. It owns layout, keys and command
// execution, and delegates all state decisions to the engine.
package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/engine"
	"github.com/martinhrvn/lazify/internal/runner"
)

var (
	colorFocus     = lipgloss.Color("2")
	colorBorder    = lipgloss.Color("8")
	styleCursor    = lipgloss.NewStyle().Reverse(true)
	styleDim       = lipgloss.NewStyle().Faint(true)
	styleErr       = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleHeader    = lipgloss.NewStyle().Bold(true)
	styleHint      = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleTabActive = lipgloss.NewStyle().Bold(true).Underline(true)
	styleLive      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

// Model is the root Bubble Tea model.
type Model struct {
	def      *def.Definition
	eng      *engine.Engine
	runner   runner.Runner
	cancels  map[uint64]context.CancelFunc
	streams  map[uint64]<-chan streamEvent
	vp       *viewport // main view scroll; pointer so View can apply follow
	vpID     string    // DetailView.ID the viewport belongs to
	debounce time.Duration
	width    int
	height   int
}

type settleMsg struct{ id uint64 }

type finishedMsg struct {
	id     uint64
	stdout []byte
	err    error
}

// New creates the model. ctx overrides context defaults.
func New(d *def.Definition, r runner.Runner, ctx map[string]string) Model {
	return Model{
		def:      d,
		eng:      engine.New(d, ctx),
		runner:   r,
		cancels:  map[uint64]context.CancelFunc{},
		streams:  map[uint64]<-chan streamEvent{},
		vp:       &viewport{},
		debounce: engine.Debounce,
	}
}

func (m Model) Init() tea.Cmd { return m.apply(m.eng.Start()) }

// apply carries out engine effects: kills cancelled runs, starts new ones
// (reporting back with finishedMsg or streamMsg) and schedules the debounced settle.
func (m Model) apply(fx engine.Effects) tea.Cmd {
	for _, id := range fx.Cancel {
		if cancel, ok := m.cancels[id]; ok {
			cancel()
			delete(m.cancels, id)
			delete(m.streams, id)
		}
	}
	var cmds []tea.Cmd
	if fx.Settle != 0 {
		cmds = append(cmds, tea.Tick(m.debounce, func(time.Time) tea.Msg { return settleMsg{id: fx.Settle} }))
	}
	for _, r := range fx.Runs {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancels[r.ID] = cancel
		if r.Stream {
			cmds = append(cmds, m.startStream(ctx, r))
			continue
		}
		cmds = append(cmds, func() tea.Msg {
			res, err := m.runner.Run(ctx, r.Req)
			return finishedMsg{id: r.ID, stdout: res.Stdout, err: err}
		})
	}
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := m.update(msg)
	next.syncViewport()
	return next, cmd
}

func (m Model) update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case finishedMsg:
		if cancel, ok := m.cancels[msg.id]; ok {
			cancel()
			delete(m.cancels, msg.id)
		}
		return m, m.apply(m.eng.Finished(msg.id, msg.stdout, msg.err))
	case settleMsg:
		return m, m.apply(m.eng.Settle(msg.id))
	case streamMsg:
		m.eng.StreamData(msg.id, msg.data)
		if msg.done {
			if cancel, ok := m.cancels[msg.id]; ok {
				cancel()
				delete(m.cancels, msg.id)
			}
			delete(m.streams, msg.id)
			return m, m.apply(m.eng.Finished(msg.id, nil, msg.err))
		}
		if ch, ok := m.streams[msg.id]; ok {
			return m, waitStream(msg.id, ch)
		}
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "q", "ctrl+c":
		for _, cancel := range m.cancels {
			cancel()
		}
		return m, tea.Quit
	case "j", "down":
		return m, m.apply(m.eng.Move(1))
	case "k", "up":
		return m, m.apply(m.eng.Move(-1))
	case "tab":
		return m, m.apply(m.eng.FocusNext())
	case "shift+tab":
		return m, m.apply(m.eng.FocusPrev())
	case "]":
		return m, m.apply(m.eng.NextTab())
	case "[":
		return m, m.apply(m.eng.PrevTab())
	case "J":
		m.scrollMain(1)
	case "K":
		m.scrollMain(-1)
	case "ctrl+d":
		m.scrollMain(m.mainHeight() / 2)
	case "ctrl+u":
		m.scrollMain(-m.mainHeight() / 2)
	case "r":
		return m, m.apply(m.eng.Refresh())
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < len(m.eng.TopLevel()) {
			return m, m.apply(m.eng.FocusPanel(m.eng.TopLevel()[i]))
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

// mainView draws the focused panel's detail: a tab bar in the title and the
// active tab's output, scrolled by the viewport.
func (m Model) mainView(w, h int) string {
	inner := max(0, h-2)
	title, head, body, stale := m.mainContent()
	shown := m.vp.window(body, max(0, inner-len(head)))
	lines := slices.Clone(head)
	for _, l := range shown {
		if stale {
			l = styleDim.Render(ansi.Strip(l))
		}
		lines = append(lines, l)
	}
	return clip(box(title, lines, w, inner, false), w, h)
}

// mainContent splits the main view into its title, fixed head lines (errors,
// placeholders) and the scrollable body.
func (m Model) mainContent() (title string, head, body []string, stale bool) {
	v := m.eng.DetailView()
	if len(v.Tabs) == 0 {
		if !v.HasRow {
			return "Main", []string{styleDim.Render(v.Empty)}, nil, false
		}
		b, _ := json.MarshalIndent(v.Row, "", "  ")
		return "Main", nil, strings.Split(string(b), "\n"), false
	}

	tabs := make([]string, len(v.Tabs))
	for i, t := range v.Tabs {
		if i == v.Active {
			tabs[i] = styleTabActive.Render(t)
		} else {
			tabs[i] = t
		}
	}
	title = strings.Join(tabs, " │ ")
	switch {
	case v.Live:
		title += styleLive.Render(" ● live")
	case v.Ended:
		title += styleDim.Render(" ended")
	}

	if v.Err != "" {
		errLines := strings.Split(strings.TrimSpace(v.Err), "\n")
		for _, l := range errLines[:min(len(errLines), 5)] {
			head = append(head, styleErr.Render(l))
		}
	}
	if len(v.Lines) == 0 {
		switch {
		case v.Loading:
			head = append(head, styleDim.Render("loading…"))
		case v.Empty != "":
			head = append(head, styleDim.Render(v.Empty))
		case v.Err == "" && !v.Live:
			head = append(head, styleDim.Render("(no output)"))
		}
	}
	return title, head, v.Lines, v.Stale
}

// mainHeight is the number of content lines inside the main box.
func (m Model) mainHeight() int { return max(1, m.height-1-2) }

func (m Model) scrollMain(delta int) {
	_, head, body, _ := m.mainContent()
	m.vp.scroll(delta, len(body), max(1, m.mainHeight()-len(head)))
}

// syncViewport resets scrolling when the main view shows different content:
// streams start following the tail, everything else starts at the top.
func (m *Model) syncViewport() {
	if v := m.eng.DetailView(); v.ID != m.vpID {
		m.vpID = v.ID
		m.vp.reset(v.Live)
	}
}

func (m Model) statusLine() string {
	hints := []string{"j/k move", "tab focus", "[/] detail tab", "J/K ctrl+d/u scroll", "r refresh", "q quit"}
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
	ts := bs
	if focused {
		ts = bs.Bold(true)
	}
	// Style the pieces separately: a title with its own ANSI styling (the tab
	// bar) would otherwise reset the colour of the border after it.
	top := bs.Render("╭─") + ts.Render(title) +
		bs.Render(strings.Repeat("─", max(0, innerW-1-ansi.StringWidth(title)))+"╮")

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
