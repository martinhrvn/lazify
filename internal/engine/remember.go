package engine

// Remembered choices: panels with `remember` (selects by default) start on
// the row they were on last time. The caller stores them between runs; the
// engine only takes them in (Recall) and hands them out (Remembered).

// Recall sets the rows to start on, by panel id → row key (or label). Call it
// before Start. Only panels that remember use theirs; --set still wins, and a
// row that is gone falls back to the panel's default, then its mark.
func (e *Engine) Recall(m map[string]string) {
	e.recall = map[string]string{}
	for id, key := range m {
		if p := e.def.Panel(id); p != nil && p.Remember {
			e.recall[id] = key
		}
	}
}

// Remembered returns the row each remembering panel is on, by panel id →
// row key. Panels without a selection (loading, empty, filtered out) are
// left out, so the caller keeps what it had for them.
func (e *Engine) Remembered() map[string]string {
	out := map[string]string{}
	for _, p := range e.def.Panels {
		if !p.Remember {
			continue
		}
		ps := e.panels[p.ID]
		if _, ok := e.selection(p.ID); !ok || ps.blocked != "" {
			continue
		}
		if k, ok := e.rowKey(ps, ps.cursor); ok {
			out[p.ID] = k
		}
	}
	return out
}
