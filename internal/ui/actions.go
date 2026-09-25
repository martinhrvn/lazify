package ui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/engine"
	"github.com/martinhrvn/lazify/internal/runner"
)

// modal is an input mode that captures keys before anything else.
type modal int

const (
	modalNone modal = iota
	modalHelp
	modalConfirm
	modalPrompt
)

type toastKind int

const (
	toastRunning toastKind = iota
	toastOK
	toastFail
)

// toast is the status-line message about the last action.
type toast struct {
	text string
	kind toastKind
	seq  int // identifies it for its expiry tick
}

var (
	styleOK      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleRunning = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

type actionDoneMsg struct {
	ref engine.ActionRef
	err error
}

type toastExpireMsg struct{ seq int }

func newPrompt() textinput.Model {
	in := textinput.New()
	in.Cursor.SetMode(cursor.CursorStatic) // a blinking cursor would tick forever
	return in
}

// trigger starts ref: asks for input or confirmation first when the action
// wants it.
func (m Model) trigger(ref engine.ActionRef) (Model, tea.Cmd) {
	if ref.Action.Prompt != "" {
		m.modal, m.pending = modalPrompt, ref
		m.input.Prompt = ref.Action.Prompt + ": "
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	}
	req, err := m.eng.RenderAction(ref, "")
	if err != nil {
		return m.fail(ref, err), nil
	}
	if ref.Action.Confirm {
		m.modal, m.pending, m.pendingReq = modalConfirm, ref, req
		return m, nil
	}
	return m.execute(ref, req)
}

// execute runs a rendered action: interactive ones take over the terminal,
// background ones run with a status-line toast.
func (m Model) execute(ref engine.ActionRef, req runner.Request) (Model, tea.Cmd) {
	done := func(err error) tea.Msg { return actionDoneMsg{ref: ref, err: err} }
	if ref.Action.Mode == "interactive" {
		return m, m.execProcess(runner.Interactive(req), done)
	}
	m.setToast(toastRunning, "⟳ "+ref.Desc()+"…")
	r := m.runner
	return m, func() tea.Msg {
		_, err := r.Run(context.Background(), req)
		return done(err)
	}
}

func (m Model) actionDone(msg actionDoneMsg) (Model, tea.Cmd) {
	if msg.err != nil {
		return m.fail(msg.ref, msg.err), nil
	}
	seq := m.setToast(toastOK, "✓ "+msg.ref.Desc())
	cmds := []tea.Cmd{m.apply(m.eng.ActionDone(msg.ref))}
	if m.toastTTL > 0 {
		cmds = append(cmds, tea.Tick(m.toastTTL, func(time.Time) tea.Msg { return toastExpireMsg{seq: seq} }))
	}
	return m, tea.Batch(cmds...)
}

// fail shows an action's error until dismissed; only its first line fits.
func (m Model) fail(ref engine.ActionRef, err error) Model {
	first, _, _ := strings.Cut(err.Error(), "\n")
	m.setToast(toastFail, "✗ "+ref.Desc()+": "+first)
	return m
}

func (m *Model) setToast(kind toastKind, text string) int {
	m.toastSeq++
	m.toast = &toast{text: text, kind: kind, seq: m.toastSeq}
	return m.toastSeq
}

// modalKey handles keys while help, a confirmation or a prompt is open.
func (m Model) modalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	switch m.modal {
	case modalHelp:
		switch k {
		case "esc", "?", "q":
			m.modal = modalNone
		case "j", "down":
			m.helpVP.scroll(1, len(m.helpLines()), m.helpVP.height)
		case "k", "up":
			m.helpVP.scroll(-1, len(m.helpLines()), m.helpVP.height)
		}
		return m, nil
	case modalConfirm:
		m.modal = modalNone
		if k == "y" {
			return m.execute(m.pending, m.pendingReq)
		}
		return m, nil
	case modalPrompt:
		switch k {
		case "esc":
			m.modal = modalNone
			m.input.Blur()
			return m, nil
		case "enter":
			m.modal = modalNone
			m.input.Blur()
			req, err := m.eng.RenderAction(m.pending, m.input.Value())
			if err != nil {
				return m.fail(m.pending, err), nil
			}
			return m.execute(m.pending, req)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// navHints are the built-in keys, for the hint bar and help.
var navHints = [][2]string{
	{"j/k", "move"}, {"tab", "next panel"}, {"1-9", "focus panel"}, {"r", "refresh"},
	{"[/]", "switch tab"}, {"J/K", "scroll content"}, {"ctrl+d/u", "page content"},
	{"enter", "open (drill down / popup)"}, {"esc", "back / close"}, {"?", "help"}, {"q", "quit"},
}

// statusLine is the bottom line: an open prompt or confirmation, otherwise
// the last action's toast followed by as many key hints as fit and "? more".
func (m Model) statusLine() string {
	switch m.modal {
	case modalPrompt:
		return ansi.Truncate(m.input.View(), m.width, "…")
	case modalConfirm:
		q := fmt.Sprintf("%s: %s? [y/N]", m.pending.Desc(), m.pendingReq.Cmd)
		return ansi.Truncate(styleRunning.Render(q), m.width, "…")
	}

	var left string
	if m.toast != nil {
		style := map[toastKind]lipgloss.Style{toastRunning: styleRunning, toastOK: styleOK, toastFail: styleErr}[m.toast.kind]
		left = style.Render(m.toast.text) + "  "
	}
	more := styleHint.Render("? more")
	avail := m.width - ansi.StringWidth(left) - ansi.StringWidth(more) - 3

	var hints []string
	used := 0
	add := func(k, desc string) bool {
		h := k + " " + desc
		if used+ansi.StringWidth(h)+3 > avail {
			return false
		}
		hints = append(hints, h)
		used += ansi.StringWidth(h) + 3
		return true
	}
	if title, ok := m.eng.EnterTarget(); ok {
		add("enter", title)
	}
	if m.eng.CanGoBack() {
		add("esc", "back")
	}
	for _, b := range m.eng.Bindings() {
		if !b.Overridden && !add(b.Key, b.Desc) {
			break
		}
	}
	nav := slices.Clone(navHints[:4])
	if m.eng.IsContent(m.eng.Focused()) {
		nav[0] = [2]string{"j/k", "scroll"}
	}
	for _, h := range nav {
		if !add(h[0], h[1]) {
			break
		}
	}
	line := left + styleHint.Render(strings.Join(hints, " · "))
	if w := m.width - ansi.StringWidth(more); ansi.StringWidth(line) > w-1 {
		line = ansi.Truncate(line, max(0, w-1), "…")
	}
	return padRight(line, m.width-ansi.StringWidth(more)) + more
}
