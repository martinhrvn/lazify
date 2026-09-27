package ui

import (
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/engine"
)

// The options form (o): every option in effect, grouped by owner. Edits stay
// in the form until enter applies them; esc drops them.

type optionsForm struct {
	groups []engine.OptionGroup
	fields []formField
	cur    int
	err    string
}

type formField struct {
	group int
	opt   *def.Option
	value string
	input textinput.Model // text and date fields
}

var styleSection = lipgloss.NewStyle().Bold(true)

// openOptions opens the form, or says there is nothing to set.
func (m Model) openOptions() (Model, tea.Cmd) {
	groups := m.eng.OptionsInEffect()
	if len(groups) == 0 {
		return m.notify("no options here")
	}
	f := &optionsForm{groups: groups}
	for gi, g := range groups {
		for _, of := range g.Fields {
			ff := formField{group: gi, opt: of.Option, value: of.Value}
			if of.Option.Type == "text" || of.Option.Type == "date" {
				ff.input = newPrompt()
				ff.input.Prompt = ""
				ff.input.SetValue(of.Value)
			}
			f.fields = append(f.fields, ff)
		}
	}
	m.form = f
	m.modal = modalOptions
	m.focusField()
	return m, m.apply(m.eng.OpenOptions())
}

// notify shows a short informational toast.
func (m Model) notify(text string) (Model, tea.Cmd) {
	seq := m.setToast(toastRunning, text)
	if m.toastTTL == 0 {
		return m, nil
	}
	return m, tea.Tick(m.toastTTL, func(time.Time) tea.Msg { return toastExpireMsg{seq: seq} })
}

func (m Model) focusField() {
	for i := range m.form.fields {
		f := &m.form.fields[i]
		switch {
		case f.opt.Type != "text" && f.opt.Type != "date": // no text input
		case i == m.form.cur:
			f.input.Focus()
		default:
			f.input.Blur()
		}
	}
}

// choices are field i's choices as the engine knows them now (a source's
// arrive after the form opens).
func (m Model) choices(i int) (choices []string, loading bool, errText string) {
	f := m.form.fields[i]
	g := m.form.groups[f.group]
	for _, cur := range m.eng.OptionsInEffect() {
		if cur.Owner != g.Owner {
			continue
		}
		for _, of := range cur.Fields {
			if of.Option == f.opt {
				return of.Choices, of.Loading, of.Err
			}
		}
	}
	return f.opt.Values, false, ""
}

func (m Model) optionsKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	form := m.form
	f := &form.fields[form.cur]
	typed := f.opt.Type == "text" || f.opt.Type == "date"
	switch k := msg.String(); {
	case k == "esc":
		m.modal, m.form = modalNone, nil
		return m, nil
	case k == "enter":
		return m.applyOptions()
	case k == "tab" || k == "down" || (!typed && k == "j"):
		form.cur = (form.cur + 1) % len(form.fields)
		m.focusField()
		return m, nil
	case k == "shift+tab" || k == "up" || (!typed && k == "k"):
		form.cur = (form.cur + len(form.fields) - 1) % len(form.fields)
		m.focusField()
		return m, nil
	case k == "ctrl+r": // back to the default
		for _, g := range m.eng.OptionsInEffect() {
			for _, of := range g.Fields {
				if of.Option == f.opt && g.Owner == form.groups[f.group].Owner {
					f.value = of.Default
					f.input.SetValue(of.Default)
				}
			}
		}
		form.err = ""
		return m, nil
	}
	switch f.opt.Type {
	case "toggle":
		if k := msg.String(); k == "space" || k == " " || k == "left" || k == "right" || k == "h" || k == "l" {
			f.value = map[string]string{"true": "false"}[f.value]
			if f.value == "" {
				f.value = "true"
			}
		}
	case "choice":
		choices, _, _ := m.choices(form.cur)
		step := map[string]int{"right": 1, "l": 1, "space": 1, " ": 1, "left": -1, "h": -1}[msg.String()]
		if step != 0 && len(choices) > 0 {
			i := slices.Index(choices, f.value)
			f.value = choices[((i+step)%len(choices)+len(choices))%len(choices)]
		}
	case "date":
		if d := map[string]int{"[": -1, "]": 1}[msg.String()]; d != 0 {
			v := f.input.Value()
			if v == "" {
				v = "today"
			}
			if t, ok := def.ParseDate(v, time.Now()); ok {
				f.input.SetValue(t.AddDate(0, 0, d).Format(def.DateLayout))
			}
			f.value = f.input.Value()
			return m, nil
		}
		fallthrough
	case "text":
		var cmd tea.Cmd
		f.input, cmd = f.input.Update(msg)
		f.value = f.input.Value()
		return m, cmd
	}
	return m, nil
}

// applyOptions sets every group's values; on an error the form stays open.
func (m Model) applyOptions() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	for gi, g := range m.form.groups {
		values := map[string]string{}
		for _, f := range m.form.fields {
			if f.group == gi {
				values[f.opt.ID] = f.value
			}
		}
		fx, err := m.eng.SetOptions(g.Owner, values)
		cmds = append(cmds, m.apply(fx))
		if err != nil {
			m.form.err = err.Error()
			return m, tea.Batch(cmds...)
		}
	}
	m.modal, m.form = modalNone, nil
	return m, tea.Batch(cmds...)
}

// optionsBox renders the form.
func (m Model) optionsBox() string {
	form := m.form
	labelW := 0
	for _, f := range form.fields {
		labelW = max(labelW, ansi.StringWidth(f.opt.Title))
	}
	var lines []string
	for i, f := range form.fields {
		if i == 0 || form.fields[i-1].group != f.group {
			if i > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, " "+styleSection.Render(form.groups[f.group].Title))
		}
		marker := "   "
		if i == form.cur {
			marker = " › "
		}
		lines = append(lines, marker+padRight(f.opt.Title, labelW)+"  "+m.fieldText(i))
	}
	field := map[string]string{
		"choice": "←/→ choose", "toggle": "space toggle", "text": "type to edit",
		"date": "type a date (today, -3d, -2w) · [/] ±1 day",
	}[form.fields[form.cur].opt.Type]
	lines = append(lines, "", " "+styleDim.Render(field),
		" "+styleDim.Render("tab next · enter apply · esc cancel · ctrl+r default"))
	if form.err != "" {
		lines = append(lines, " "+styleErr.Render(form.err))
	}
	w := 0
	for _, l := range lines {
		w = max(w, ansi.StringWidth(l)+2)
	}
	w = min(max(w, 40), m.width)
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w-2, "…")
	}
	return box("Options", lines, w, len(lines), true)
}

// fieldText is field i's value as the form shows it.
func (m Model) fieldText(i int) string {
	f := m.form.fields[i]
	cur := i == m.form.cur
	switch f.opt.Type {
	case "toggle":
		if f.value == "true" {
			return "[x]"
		}
		return "[ ]"
	case "choice":
		choices, loading, errText := m.choices(i)
		v := f.value
		if v == "" {
			v = "(any)"
		}
		switch {
		case errText != "":
			return v + styleErr.Render("  "+errText)
		case loading:
			return v + styleDim.Render("  loading…")
		case len(choices) == 0:
			return v
		}
		if cur {
			return "‹ " + styleCursor.Render(v) + " ›"
		}
		return "‹ " + v + " ›"
	}
	if cur {
		return f.input.View()
	}
	if f.value == "" {
		return styleDim.Render("(empty)")
	}
	return f.value
}

// optionTitle is a panel title's suffix listing options away from defaults.
func optionTitle(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	return styleDim.Render(" " + strings.Join(opts, " "))
}
