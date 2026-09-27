package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Search in a content panel (/ while it is focused), like less: matches are
// highlighted in place, n/N jump between the lines that have them. Plain
// text, case-insensitive unless the query has an upper-case letter.

type contentSearch struct {
	query string
	cur   int // line (in the body) of the current match; -1 = none yet
}

var (
	styleMatch    = lipgloss.NewStyle().Background(lipgloss.Color("3")).Foreground(lipgloss.Color("0"))
	styleMatchCur = lipgloss.NewStyle().Background(lipgloss.Color("6")).Foreground(lipgloss.Color("0")).Bold(true)
)

// activeSearch returns content panel id's search, if it has a query.
func (m Model) activeSearch(id string) *contentSearch {
	if s := m.search[id]; s != nil && s.query != "" {
		return s
	}
	return nil
}

// fold prepares text and query for matching: smart case.
func fold(text, query string) (string, string) {
	if strings.IndexFunc(query, unicode.IsUpper) >= 0 {
		return text, query
	}
	if lt := strings.ToLower(text); len(lt) == len(text) { // byte offsets must still line up
		return lt, strings.ToLower(query)
	}
	return text, query
}

// matchLines lists the body lines containing query.
func matchLines(body []string, query string) []int {
	var out []int
	for i, l := range body {
		if hay, needle := fold(ansi.Strip(l), query); strings.Contains(hay, needle) {
			out = append(out, i)
		}
	}
	return out
}

// jumpMatch moves content panel id's current match: dir 0 = the first at or
// below the top of the view, 1/-1 = the next/previous (wrapping).
func (m Model) jumpMatch(id string, dir int) {
	s := m.activeSearch(id)
	if s == nil {
		return
	}
	_, _, body, _ := m.contentParts(id)
	ms := matchLines(body, s.query)
	if len(ms) == 0 {
		s.cur = -1
		return
	}
	vp := m.viewport(id)
	i := 0
	switch dir {
	case 0:
		i = slices.IndexFunc(ms, func(l int) bool { return l >= vp.offset })
	case 1:
		i = slices.IndexFunc(ms, func(l int) bool { return l > s.cur })
	case -1:
		i = len(ms) - 1
		for i >= 0 && ms[i] >= s.cur {
			i--
		}
		if i < 0 {
			i = len(ms) - 1
		}
	}
	if i < 0 {
		i = 0 // wrap to the first
	}
	s.cur = ms[i]
	vp.show(s.cur, len(body), max(1, vp.height))
}

// searchTitle is what a content panel's title shows about its search.
func (m Model) searchTitle(id string, body []string) string {
	s := m.activeSearch(id)
	if s == nil {
		return ""
	}
	ms := matchLines(body, s.query)
	switch i := slices.Index(ms, s.cur); {
	case len(ms) == 0:
		return styleDim.Render(" /" + s.query + " (no match)")
	case i < 0:
		return styleDim.Render(fmt.Sprintf(" /%s %d", s.query, len(ms)))
	default:
		return styleDim.Render(fmt.Sprintf(" /%s %d/%d", s.query, i+1, len(ms)))
	}
}

// highlight marks query's occurrences in line, keeping its other styling.
func highlight(line, query string, current bool) string {
	plain := ansi.Strip(line)
	hay, needle := fold(plain, query)
	if needle == "" || !strings.Contains(hay, needle) {
		return line
	}
	st := styleMatch
	if current {
		st = styleMatchCur
	}
	var b strings.Builder
	col, from := 0, 0
	for {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			break
		}
		start, end := from+i, from+i+len(needle)
		a, z := ansi.StringWidth(plain[:start]), ansi.StringWidth(plain[:end])
		b.WriteString(ansi.Cut(line, col, a))
		b.WriteString(st.Render(plain[start:end]))
		col, from = z, end
	}
	b.WriteString(ansi.Cut(line, col, ansi.StringWidth(plain)))
	return b.String()
}

// searchKey handles keys while the search prompt is open: typing searches as
// you go, enter keeps it, esc clears it.
func (m Model) searchKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	id := m.eng.Focused()
	switch msg.String() {
	case "enter":
		m.modal = modalNone
		m.input.Blur()
		return m, nil
	case "esc":
		m.modal = modalNone
		m.input.Blur()
		delete(m.search, id)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	s := m.search[id]
	if s == nil {
		s = &contentSearch{cur: -1}
		m.search[id] = s
	}
	s.query, s.cur = m.input.Value(), -1
	m.jumpMatch(id, 0)
	return m, cmd
}
