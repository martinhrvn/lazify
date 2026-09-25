package engine

import (
	"slices"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// ActionRef is an action together with where it was bound.
type ActionRef struct {
	Action *def.Action
	Panel  string // owning panel; "" for a global action
}

// Binding describes one action key for hints and help.
type Binding struct {
	Key        string
	Desc       string
	Global     bool
	Overridden bool // a global shadowed by the focused panel's action
}

// ActionFor returns the action bound to key: the focused panel's own action
// wins over a global one with the same key.
func (e *Engine) ActionFor(key string) (ActionRef, bool) {
	focused := e.def.Panel(e.Focused())
	for _, a := range focused.Actions {
		if a.Key == key {
			return ActionRef{Action: a, Panel: focused.ID}, true
		}
	}
	for _, a := range e.def.Actions {
		if a.Key == key {
			return ActionRef{Action: a}, true
		}
	}
	return ActionRef{}, false
}

// Bindings lists the focused panel's actions, then the global ones.
func (e *Engine) Bindings() []Binding {
	var out []Binding
	own := map[string]bool{}
	for _, a := range e.def.Panel(e.Focused()).Actions {
		out = append(out, Binding{Key: a.Key, Desc: describe(a)})
		own[a.Key] = true
	}
	for _, a := range e.def.Actions {
		out = append(out, Binding{Key: a.Key, Desc: describe(a), Global: true, Overridden: own[a.Key]})
	}
	return out
}

// Desc is the action's description, or its command when it has none.
func (r ActionRef) Desc() string { return describe(r.Action) }

func describe(a *def.Action) string {
	if a.Desc != "" {
		return a.Desc
	}
	return a.Cmd.Source()
}

// RenderAction renders ref's command. Its row ({{.x}}) is the owning list
// panel's selection; for globals and content panels' actions it is the active
// panel's. input is the answer to the action's prompt.
func (e *Engine) RenderAction(ref ActionRef, input string) (runner.Request, error) {
	rowPanel := e.active
	if ref.Panel != "" && !e.IsContent(ref.Panel) {
		rowPanel = ref.Panel
	}
	row, _ := e.selection(rowPanel)
	base := e.resolver(row)
	cmd, err := ref.Action.Cmd.Render(resolverFunc(func(r tmpl.Ref) (any, bool) {
		if r.Scope == tmpl.ScopeInput {
			return input, true
		}
		return base.Resolve(r)
	}), tmpl.Shell)
	if err != nil {
		return runner.Request{}, err
	}
	req := runner.Request{Cmd: cmd, Env: e.envFor(), Timeout: e.def.Timeout}
	if ref.Action.Mode == "interactive" {
		req.Timeout = 0
	}
	return req, nil
}

// ActionDone reacts to a successful action: whatever was cached may be out of
// date, so caches are dropped, the action's refresh panels (by default its
// own list panel) re-run once their inputs settle, and content panels re-run.
func (e *Engine) ActionDone(ref ActionRef) Effects {
	clear(e.cache)
	clear(e.dcache)
	clear(e.mcache)
	refresh := ref.Action.Refresh
	if len(refresh) == 0 && ref.Panel != "" && !e.IsContent(ref.Panel) {
		refresh = []string{ref.Panel}
	}
	for _, id := range e.def.Order { // inputs first, so dependents wait for them
		if !slices.Contains(refresh, id) {
			continue
		}
		ps := e.panels[id]
		if ps.def.IsContent() || ps.blocked != "" {
			continue
		}
		ps.cmd = "" // force a re-run even if the command is unchanged
		e.cancel(ps)
		e.evaluate(ps, true)
	}
	for _, id := range e.contentPanels() {
		e.refreshContent(id)
	}
	return e.take()
}
