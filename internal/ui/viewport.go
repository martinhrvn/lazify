package ui

// viewport is the scroll state of a text view. In follow mode it sticks to the
// bottom as content grows (tail -f); scrolling up pauses following and
// scrolling back to the bottom resumes it.
type viewport struct {
	offset int
	follow bool
	height int // lines shown at the last render, for paging
}

// reset starts a new piece of content: at the bottom and following for a
// stream, at the top otherwise.
func (v *viewport) reset(follow bool) { *v = viewport{follow: follow, height: v.height} }

// scroll moves by delta lines within content of n lines shown h at a time.
func (v *viewport) scroll(delta, n, h int) {
	bottom := max(0, n-h)
	if v.follow {
		v.offset = bottom
	}
	v.offset = max(0, min(bottom, v.offset+delta))
	switch {
	case delta < 0:
		v.follow = false
	case v.offset == bottom:
		v.follow = true // reached the bottom
	}
}

// window returns the lines visible at height h.
func (v *viewport) window(lines []string, h int) []string {
	v.height = h
	bottom := max(0, len(lines)-h)
	if v.follow || v.offset > bottom {
		v.offset = bottom
	}
	return lines[v.offset:min(len(lines), v.offset+h)]
}
