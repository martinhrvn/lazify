package engine

import (
	"slices"
	"time"
)

// Auto-refresh: panels with `refresh: <interval>` re-run on a timer the
// caller keeps (see RefreshIntervals). A refresh is quiet — rows aren't
// dimmed while it runs — so a live panel doesn't flicker.

// RefreshIntervals lists the panels that refresh on their own.
func (e *Engine) RefreshIntervals() map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, p := range e.def.Panels {
		if p.Refresh > 0 {
			out[p.ID] = p.Refresh
		}
	}
	return out
}

// AutoRefresh re-runs panel id if it refreshes on its own, is on screen and
// is idle (not running, waiting for inputs or blocked). The cursor stays on
// the same row by key, and dependents re-run only if its selection changed.
func (e *Engine) AutoRefresh(id string) Effects {
	ps := e.panels[id]
	if ps == nil || ps.def.Refresh <= 0 || !e.visible(id) ||
		ps.runID != 0 || ps.pending || ps.blocked != "" || ps.def.Values != nil {
		return e.take()
	}
	if k, ok := e.render(ps); ok {
		e.start(ps, k)
		ps.stale = false
	}
	return e.take()
}

// visible reports whether panel id is drawn: the top of a slot's active tab,
// or in a popup.
func (e *Engine) visible(id string) bool {
	if slices.ContainsFunc(e.popups, func(p popupLevel) bool { return p.id == id }) {
		return true
	}
	return slices.ContainsFunc(e.TopLevel(), func(slot string) bool { return e.Top(slot) == id })
}
