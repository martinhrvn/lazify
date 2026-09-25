// Package ui is the Bubble Tea front end. It owns layout, keys and command
// execution, and delegates all state decisions to the engine.
package ui

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	styleMark      = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)
)

// Model is the root Bubble Tea model.
type Model struct {
	def      *def.Definition
	eng      *engine.Engine
	runner   runner.Runner
	cancels  map[uint64]context.CancelFunc
	streams  map[uint64]<-chan streamEvent
	vps      map[string]*viewport // per content panel; pointers so View can apply follow
	vpIDs    map[string]string    // ContentView.ID each viewport belongs to
	debounce time.Duration
	toastTTL time.Duration // how long a success toast stays; 0 = until replaced

	// Actions: an open modal, the action it is for, and the last toast.
	modal       modal
	pending     engine.ActionRef
	pendingReq  runner.Request
	input       textinput.Model
	helpVP      *viewport
	toast       *toast
	toastSeq    int
	execProcess func(*exec.Cmd, tea.ExecCallback) tea.Cmd // tea.ExecProcess; swapped in tests
	width       int
	height      int
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
		def:         d,
		eng:         engine.New(d, ctx),
		runner:      r,
		cancels:     map[uint64]context.CancelFunc{},
		streams:     map[uint64]<-chan streamEvent{},
		vps:         map[string]*viewport{},
		vpIDs:       map[string]string{},
		debounce:    engine.Debounce,
		toastTTL:    3 * time.Second,
		input:       newPrompt(),
		helpVP:      &viewport{},
		execProcess: tea.ExecProcess,
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
	next.syncViewports()
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
	case actionDoneMsg:
		return m.actionDone(msg)
	case toastExpireMsg:
		if m.toast != nil && m.toast.seq == msg.seq {
			m.toast = nil
		}
	case tea.KeyMsg:
		if m.modal != modalNone {
			return m.modalKey(msg)
		}
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	if k == " " {
		k = "space" // the name definitions use
	}
	switch k {
	case "q", "ctrl+c":
		for _, cancel := range m.cancels {
			cancel()
		}
		return m, tea.Quit
	case "j", "down":
		if f := m.eng.Focused(); m.eng.IsContent(f) {
			m.scrollContent(f, 1, false)
			return m, nil
		}
		return m, m.apply(m.eng.Move(1))
	case "k", "up":
		if f := m.eng.Focused(); m.eng.IsContent(f) {
			m.scrollContent(f, -1, false)
			return m, nil
		}
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
		m.scrollContent(m.eng.Target(), 1, false)
	case "K":
		m.scrollContent(m.eng.Target(), -1, false)
	case "ctrl+d":
		m.scrollContent(m.eng.Target(), 1, true)
	case "ctrl+u":
		m.scrollContent(m.eng.Target(), -1, true)
	case "r":
		return m, m.apply(m.eng.Refresh())
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < len(m.eng.TopLevel()) {
			id := m.eng.TopLevel()[i]
			if m.eng.IsSelect(id) {
				return m, m.apply(m.eng.Pick(id)) // a select's number opens its picker
			}
			return m, m.apply(m.eng.FocusPanel(id))
		}
	case "?":
		m.modal = modalHelp
		m.helpVP.reset(false)
	case "/":
		if f := m.eng.Focused(); !m.eng.IsContent(f) {
			m.modal = modalFilter
			m.input.Prompt = "/"
			m.input.SetValue(m.eng.Filter(f))
			m.input.Focus()
		}
	case "enter":
		return m, m.apply(m.eng.Enter())
	case "esc":
		if fx, ok := m.eng.Back(); ok {
			return m, m.apply(fx)
		}
		m.toast = nil
	default:
		if ref, ok := m.eng.ActionFor(k); ok {
			return m.trigger(ref)
		}
	}
	return m, nil
}

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}
	bodyH := m.height - 1
	var left, center, right []string
	for _, id := range m.eng.TopLevel() {
		switch m.def.Panel(id).Side {
		case "right":
			right = append(right, id)
		case "center":
			center = append(center, id)
		default:
			left = append(left, id)
		}
	}

	lay := m.def.Layout
	leftW, rightW := 0, 0
	if len(left) > 0 {
		leftW = m.width * lay.LeftWidth / 100
	}
	if len(right) > 0 {
		rightW = m.width * lay.RightWidth / 100
	}
	centerW := m.width - leftW - rightW
	switch {
	case len(center) == 0 && len(left) > 0: // no center: the left column takes its space
		leftW, centerW = leftW+centerW, 0
	case len(center) == 0:
		rightW, centerW = rightW+centerW, 0
	case centerW < 10 && len(left) > 0: // keep the center readable
		leftW, centerW = max(0, leftW-(10-centerW)), 10
	}

	var cols []string
	for _, c := range []struct {
		ids []string
		w   int
	}{{left, leftW}, {center, centerW}, {right, rightW}} {
		if len(c.ids) > 0 && c.w > 0 {
			cols = append(cols, m.column(c.ids, c.w, bodyH))
		}
	}
	view := lipgloss.JoinHorizontal(lipgloss.Top, cols...) + "\n" + m.statusLine()
	if pv, ok := m.eng.Popup(); ok {
		view = overlay(view, m.popupBox(pv, bodyH), m.width)
	}
	if m.modal == modalHelp {
		view = overlay(view, m.helpBox(m.width, m.height), m.width)
	}
	return view
}

// column stacks panels in a box each, sized by the definition's layout rules,
// clipped or padded to exactly h lines.
func (m Model) column(ids []string, w, h int) string {
	// Each slot shows the top of its drill-down stack, sized by the slot's rules.
	slots := make([]slot, len(ids))
	for i, id := range ids {
		top := m.eng.Top(id)
		slots[i] = slot{
			size:    m.def.Panel(id).Size,
			content: m.wantLines(top),
			focused: top == m.eng.Focused(),
		}
	}
	hs := heights(h-2*len(ids), slots, m.def.Layout.Focus) // each box has 2 border lines
	all := m.eng.TopLevel()
	boxes := make([]string, len(ids))
	for i, id := range ids {
		num := fmt.Sprintf("[%d] ", slices.Index(all, id)+1)
		crumbs := m.eng.Crumbs(id)
		if tabs := m.eng.Tabs(id); len(tabs) > 1 {
			crumbs[0] = m.tabBar(tabs, m.eng.ActiveTab(id))
		}
		title, lines := m.panelBox(m.eng.Top(id), crumbs, w, hs[i])
		boxes[i] = box(num+title, lines, w, hs[i], m.eng.Top(id) == m.eng.Focused())
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

// wantLines is how many lines panel id would like to show (for `size: fit`).
func (m Model) wantLines(id string) int {
	if m.eng.IsContent(id) {
		_, head, body, _ := m.contentParts(id)
		return len(head) + len(body)
	}
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
	return m.listLines(m.eng.View(id), id == m.eng.Focused(), w, h)
}

// listLines renders a list view; focused shows the cursor.
func (m Model) listLines(v engine.PanelView, focused bool, w, h int) []string {
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

	// Panels with a mark reserve a two-column gutter: "* " for marked rows.
	gutter := func(i int) string { return "" }
	if v.Marked != nil {
		gutter = func(i int) string {
			if i >= 0 && v.Marked[i] {
				return "* "
			}
			return "  "
		}
	}
	if len(v.Headers) > 0 {
		widths := columnWidths(v.Headers, v.Columns)
		head = append(head, styleHeader.Render(gutter(-1)+alignRow(v.Headers, widths)))
		for i, cells := range v.Columns {
			rows = append(rows, gutter(i)+alignRow(cells, widths))
		}
	} else {
		for i, l := range v.Lines {
			rows = append(rows, gutter(i)+l)
		}
	}

	avail := max(1, h-len(head))
	offset := max(0, v.Cursor-avail+1)
	out := head
	for i := offset; i < len(rows) && i < offset+avail; i++ {
		line := ansi.Truncate(rows[i], w, "…")
		switch {
		case i == v.Cursor && focused:
			line = styleCursor.Render(padRight(line, w))
		case v.Stale:
			line = styleDim.Render(line)
		case v.Marked != nil && v.Marked[i]:
			line = styleMark.Render(line)
		}
		out = append(out, line)
	}
	switch {
	case len(rows) == 0 && v.Filter != "":
		out = append(out, styleDim.Render("(no matches)"))
	case len(rows) == 0 && v.Err == "" && !v.Loading:
		out = append(out, styleDim.Render("(empty)"))
	}
	return out
}

// contentBox renders content panel id at inner height h: its title (the tab
// bar) and the visible lines of its output, scrolled by its viewport.
func (m Model) contentBox(id string, h int) (string, []string) {
	title, head, body, stale := m.contentParts(id)
	lines := slices.Clone(head)
	for _, l := range m.viewport(id).window(body, max(0, h-len(head))) {
		if stale {
			l = styleDim.Render(ansi.Strip(l))
		}
		lines = append(lines, l)
	}
	return title, lines
}

// contentParts splits a content panel into its title, fixed head lines
// (errors, placeholders) and the scrollable body.
func (m Model) contentParts(id string) (title string, head, body []string, stale bool) {
	v := m.eng.ContentView(id)
	title = v.Title
	if len(v.Tabs) > 0 {
		tabs := make([]string, len(v.Tabs))
		for i, t := range v.Tabs {
			if i == v.Active {
				tabs[i] = styleTabActive.Render(t)
			} else {
				tabs[i] = t
			}
		}
		title = strings.Join(tabs, " │ ")
	}
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

func (m Model) viewport(id string) *viewport {
	vp, ok := m.vps[id]
	if !ok {
		vp = &viewport{}
		m.vps[id] = vp
	}
	return vp
}

// scrollContent scrolls content panel id; delta is in lines, or in half pages
// when halfPages is set.
func (m Model) scrollContent(id string, delta int, halfPages bool) {
	if id == "" {
		return
	}
	_, _, body, _ := m.contentParts(id)
	vp := m.viewport(id)
	h := max(1, vp.height) // body lines shown at the last render
	if halfPages {
		delta *= max(1, vp.height/2)
	}
	vp.scroll(delta, len(body), h)
}

// syncViewports resets a content panel's scrolling when it shows different
// content: streams start following the tail, everything else starts at the top.
func (m *Model) syncViewports() {
	for _, id := range m.eng.ContentPanels() {
		if v := m.eng.ContentView(id); v.ID != m.vpIDs[id] {
			m.vpIDs[id] = v.ID
			m.viewport(id).reset(v.Live)
		}
	}
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
	top := bs.Render("╭─") + within(ts, title) +
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
