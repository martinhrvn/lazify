package engine

// Lazy panels: a list panel runs only when it is needed — on screen (the
// shown tab of a slot, an open drill level or popup), or read by something
// needed (a panel's inputs, the env's panels, what a shown content tab reads).
// A hidden tab stays pending until it is shown (see wake).

// needed returns the ids of the panels that should run now.
func (e *Engine) needed() map[string]bool {
	need := map[string]bool{}
	var add func(id string)
	add = func(id string) {
		if need[id] {
			return
		}
		need[id] = true
		if p := e.def.Panel(id); p != nil {
			for _, dep := range p.Deps {
				add(dep)
			}
		}
	}
	for _, p := range e.def.Panels {
		if !p.IsContent() && e.visible(p.ID) {
			add(p.ID)
		}
	}
	for _, id := range e.def.EnvDeps {
		add(id)
	}
	for _, id := range e.contentPanels() {
		if !e.visible(id) {
			continue
		}
		add(e.active)
		if entry, tabs := e.entry(id); len(tabs) > 0 {
			for _, dep := range tabs[e.tabIdx[tabKey(id, entry)]].Deps {
				add(dep)
			}
		}
	}
	return need
}

// isNeeded reports whether panel id should run now (see needed).
func (e *Engine) isNeeded(id string) bool { return e.needed()[id] }

// wake runs the panels that became needed (a tab was shown) and were left
// pending while hidden.
func (e *Engine) wake() {
	need := e.needed()
	for _, id := range e.def.Order {
		if ps := e.panels[id]; need[id] && ps.pending {
			e.evaluate(ps, true)
		}
	}
}
