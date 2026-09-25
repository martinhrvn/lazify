package ui

import (
	"reflect"
	"testing"

	"github.com/martinhrvn/lazify/internal/def"
)

var (
	flex  = def.Size{Kind: def.Flex, N: 1}
	flex2 = def.Size{Kind: def.Flex, N: 2}
	fit   = def.Size{Kind: def.Fit}
)

func fixed(n int) def.Size { return def.Size{Kind: def.Fixed, N: n} }

func TestHeights(t *testing.T) {
	tests := []struct {
		name  string
		avail int
		focus string
		slots []slot
		want  []int
	}{
		{
			name: "expand: focused takes the rest, others shrink to content", avail: 30, focus: "expand",
			slots: []slot{{size: flex, content: 3}, {size: flex, content: 50, focused: true}, {size: flex, content: 50}},
			want:  []int{3, 20, 7},
		},
		{
			name: "expand: focus in other column splits equally", avail: 30, focus: "expand",
			slots: []slot{{size: flex, content: 50}, {size: flex, content: 50}, {size: flex, content: 50}},
			want:  []int{10, 10, 10},
		},
		{
			name: "equal ignores focus, leftover goes to the top", avail: 20, focus: "equal",
			slots: []slot{{size: flex, content: 50}, {size: flex, content: 50, focused: true}, {size: flex}},
			want:  []int{7, 7, 6},
		},
		{
			name: "lazygit: fit status + equal rest", avail: 31, focus: "equal",
			slots: []slot{{size: fit, content: 1, focused: true}, {size: flex}, {size: flex}, {size: flex}},
			want:  []int{1, 10, 10, 10},
		},
		{
			name: "fit is capped at a fair share", avail: 20, focus: "equal",
			slots: []slot{{size: fit, content: 100}, {size: flex}},
			want:  []int{10, 10},
		},
		{
			name: "fit stays small when focused in expand mode", avail: 20, focus: "expand",
			slots: []slot{{size: fit, content: 2, focused: true}, {size: flex, content: 30}, {size: flex, content: 30}},
			want:  []int{2, 9, 9},
		},
		{
			name: "empty fit still gets a line", avail: 10, focus: "equal",
			slots: []slot{{size: fit}, {size: flex}},
			want:  []int{1, 9},
		},
		{
			name: "fixed", avail: 20, focus: "equal",
			slots: []slot{{size: fixed(5)}, {size: flex}},
			want:  []int{5, 15},
		},
		{
			name: "weights", avail: 30, focus: "equal",
			slots: []slot{{size: flex}, {size: flex2}},
			want:  []int{10, 20},
		},
		{
			name: "no flex: last panel absorbs the leftover", avail: 20, focus: "equal",
			slots: []slot{{size: fit, content: 2}, {size: fixed(3)}},
			want:  []int{2, 18},
		},
		{
			name: "overflow shrinks the largest first", avail: 10, focus: "equal",
			slots: []slot{{size: fixed(8)}, {size: fixed(4)}, {size: flex}},
			want:  []int{5, 4, 1},
		},
		{
			name: "too short for everything: one line each", avail: 2, focus: "equal",
			slots: []slot{{size: flex}, {size: flex}, {size: flex}},
			want:  []int{1, 1, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := heights(tt.avail, tt.slots, tt.focus)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("heights = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHeightsAlwaysFill(t *testing.T) {
	sizes := []def.Size{flex, flex2, fit, fixed(1), fixed(7)}
	for avail := 3; avail < 60; avail++ {
		for _, focus := range []string{"expand", "equal"} {
			for f := range 3 {
				var slots []slot
				for i := range 3 {
					slots = append(slots, slot{size: sizes[(avail+i)%len(sizes)], content: avail * i % 17, focused: i == f})
				}
				got := heights(avail, slots, focus)
				sum := 0
				for _, h := range got {
					if h < 1 {
						t.Fatalf("avail %d %s: height < 1: %v", avail, focus, got)
					}
					sum += h
				}
				if sum != avail {
					t.Fatalf("avail %d %s slots %+v: sum %d (%v)", avail, focus, slots, sum, got)
				}
			}
		}
	}
}
