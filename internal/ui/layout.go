package ui

import "github.com/martinhrvn/lazify/internal/def"

// slot is one panel in a column, as far as height allocation is concerned.
type slot struct {
	size    def.Size
	content int // lines the panel would like to show
	focused bool
}

// heights splits avail inner lines between the slots of one column. The result
// sums to avail whenever avail >= len(slots); every slot gets at least 1 line.
//
//   - Fixed slots get N lines; Fit slots get their content, capped at a fair share.
//   - Flex slots share the rest by weight. With focus "expand" and a focused
//     flex slot, the other flex slots shrink to their content and the focused one
//     takes whatever is left.
func heights(avail int, slots []slot, focus string) []int {
	n := len(slots)
	if n == 0 {
		return nil
	}
	fair := max(1, avail/n)
	hs := make([]int, n)
	var flex []int
	focusedFlex := -1
	rest := avail
	for i, s := range slots {
		switch s.size.Kind {
		case def.Fixed:
			hs[i] = s.size.N
		case def.Fit:
			hs[i] = min(max(1, s.content), fair)
		default:
			flex = append(flex, i)
			if s.focused {
				focusedFlex = i
			}
			continue
		}
		rest -= hs[i]
	}

	switch {
	case len(flex) == 0:
		hs[n-1] += max(0, rest)
	case focus == "expand" && focusedFlex >= 0:
		share := max(1, rest/(len(flex)+1))
		used := 0
		for _, i := range flex {
			if i != focusedFlex {
				hs[i] = min(max(1, slots[i].content), share)
				used += hs[i]
			}
		}
		hs[focusedFlex] = max(1, rest-used)
	default:
		weight := 0
		for _, i := range flex {
			weight += slots[i].size.N
		}
		used := 0
		for _, i := range flex {
			hs[i] = max(1, max(0, rest)*slots[i].size.N/weight)
			used += hs[i]
		}
		for k := 0; used < rest; k++ {
			hs[flex[k%len(flex)]]++
			used++
		}
	}

	// Too tall: take lines from the largest slots until it fits (or all are 1).
	total := 0
	for _, h := range hs {
		total += h
	}
	for total > avail {
		big := 0
		for i, h := range hs {
			if h > hs[big] {
				big = i
			}
		}
		if hs[big] <= 1 {
			break
		}
		hs[big]--
		total--
	}
	return hs
}
