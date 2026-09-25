package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// withColors turns on ANSI output for the test (lipgloss strips styles when
// not writing to a terminal, which would hide styling bugs).
func withColors(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// TestStyledTitleKeepsBorderColour: a title containing its own styled parts
// (an underlined active tab) must stay in the border colour throughout. The
// inner parts end with resets, and underline is rendered per character, so
// wrapping naively left only the first letter coloured.
func TestStyledTitleKeepsBorderColour(t *testing.T) {
	withColors(t)
	border := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	open := strings.TrimSuffix(border.Render("x"), "x\x1b[0m") // the border colour's SGR
	title := styleTabActive.Render("Branches") + " │ Remotes"

	top := strings.Split(box(title, nil, 40, 1, false), "\n")[0]
	if ansi.Strip(top) != "╭─ Branches │ Remotes ─────────────────╮" {
		t.Fatalf("title text = %q", ansi.Strip(top))
	}
	// Walk the raw line: every visible title character must be drawn while the
	// border colour is on (set since the last reset).
	coloured, col := false, 0
	for i := 0; i < len(top); {
		if strings.HasPrefix(top[i:], "\x1b[") {
			end := i + strings.IndexByte(top[i:], 'm') + 1
			switch seq := top[i:end]; {
			case seq == "\x1b[0m":
				coloured = false
			case strings.Contains(open, seq) || seq == open:
				coloured = true
			}
			i = end
			continue
		}
		r := []rune(top[i:])[0]
		if col >= 3 && col < 3+len([]rune("Branches │ Remotes")) && !coloured {
			t.Fatalf("title character %q at column %d is not in the border colour: %q", r, col, top)
		}
		i += len(string(r))
		col++
	}
}
