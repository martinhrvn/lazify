package ui

import (
	"reflect"
	"testing"
)

func lines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = string(rune('a' + i%26))
	}
	return out
}

func TestViewportClamps(t *testing.T) {
	var v viewport
	v.scroll(-5, 20, 5)
	if v.offset != 0 {
		t.Errorf("offset = %d", v.offset)
	}
	v.scroll(100, 20, 5)
	if v.offset != 15 {
		t.Errorf("offset = %d, want 15 (last page)", v.offset)
	}
	v.scroll(3, 4, 5) // content shorter than the view
	if v.offset != 0 {
		t.Errorf("offset = %d", v.offset)
	}
}

func TestViewportWindow(t *testing.T) {
	v := viewport{offset: 2}
	if got := v.window(lines(6), 3); !reflect.DeepEqual(got, []string{"c", "d", "e"}) {
		t.Errorf("window = %v", got)
	}
	v.offset = 10 // content shrank
	if got := v.window(lines(6), 3); !reflect.DeepEqual(got, []string{"d", "e", "f"}) {
		t.Errorf("window after shrink = %v", got)
	}
}

func TestViewportFollow(t *testing.T) {
	var v viewport
	v.reset(true)
	if got := v.window(lines(10), 3); !reflect.DeepEqual(got, []string{"h", "i", "j"}) {
		t.Errorf("follow window = %v", got)
	}
	// New lines keep it at the bottom.
	if got := v.window(lines(12), 3); !reflect.DeepEqual(got, []string{"j", "k", "l"}) {
		t.Errorf("follow window = %v", got)
	}
	v.scroll(-2, 12, 3)
	if v.follow {
		t.Error("scrolling up should stop following")
	}
	if got := v.window(lines(20), 3); !reflect.DeepEqual(got, []string{"h", "i", "j"}) {
		t.Errorf("paused window should not move: %v", got)
	}
	v.scroll(100, 20, 3)
	if !v.follow {
		t.Error("reaching the bottom should resume following")
	}
}

func TestViewportResetOnce(t *testing.T) {
	v := viewport{offset: 7, follow: true}
	v.reset(false)
	if v.offset != 0 || v.follow {
		t.Errorf("reset = %+v", v)
	}
}
