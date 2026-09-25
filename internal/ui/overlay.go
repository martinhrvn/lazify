package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// overlay paints fg centered over bg (both multi-line strings of width w).
func overlay(bg, fg string, w int) string {
	base := strings.Split(bg, "\n")
	top := strings.Split(fg, "\n")
	fw := 0
	for _, l := range top {
		fw = max(fw, ansi.StringWidth(l))
	}
	x := max(0, (w-fw)/2)
	y := max(0, (len(base)-len(top))/2)
	for i, l := range top {
		if y+i >= len(base) {
			break
		}
		b := padRight(base[y+i], w)
		base[y+i] = ansi.Cut(b, 0, x) + padRight(l, fw) + ansi.Cut(b, x+fw, w)
	}
	return strings.Join(base, "\n")
}
