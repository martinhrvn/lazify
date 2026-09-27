package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Options: values the user sets while the app runs, read by commands as
// {{opt.x}} (their owner's) or {{panel.opt.x}} (a list panel's). An owner is
// a list panel id or a content entry (EntryOwner). Only values the user set
// are stored; defaults are resolved when read, so "-14d" follows the clock.

// EntryOwner names the options of content panel view's entry (a list panel
// id or "default").
func EntryOwner(view, entry string) string { return view + "\x00" + entry }

// OptionField is one option as the form shows it.
type OptionField struct {
	Option  *def.Option
	Value   string   // current value (a date as YYYY-MM-DD, a toggle as true/false)
	Default string   // the default, resolved the same way
	Choices []string // for a choice: its values, or its source's lines ("" = any)
	Loading bool     // the choices' command is running
	Err     string   // the choices' command failed
}

// OptionGroup is the options of one owner, titled for the form.
type OptionGroup struct {
	Owner  string
	Title  string
	Fields []OptionField
}

// ownerOptions returns the options declared for owner.
func (e *Engine) ownerOptions(owner string) []*def.Option {
	if view, entry, ok := strings.Cut(owner, "\x00"); ok {
		if p := e.def.Panel(view); p != nil && p.Content[entry] != nil {
			return p.Content[entry].Options
		}
		return nil
	}
	if p := e.def.Panel(owner); p != nil {
		return p.Options
	}
	return nil
}

func findOption(opts []*def.Option, id string) *def.Option {
	for _, o := range opts {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func (e *Engine) optionDefault(o *def.Option) string {
	if o.Type == "date" {
		if t, ok := def.ParseDate(o.Default, e.now()); ok {
			return t.Format(def.DateLayout)
		}
	}
	return o.Default
}

func (e *Engine) optionValue(owner string, o *def.Option) string {
	if v, ok := e.opts[owner][o.ID]; ok {
		return v
	}
	return e.optionDefault(o)
}

// optionArg is what {{opt.x}} renders: with a flag, an optional argument
// (flag+value, the flag alone for a toggle that is on, nothing when unset).
func optionArg(o *def.Option, v string) any {
	switch {
	case o.Flag == "":
		return v
	case o.Type == "toggle" && v == "true":
		return tmpl.Arg(o.Flag)
	case o.Type == "toggle" || v == "":
		return tmpl.Arg("")
	}
	return tmpl.Arg(o.Flag + v)
}

// panelOption resolves {{panel.opt.x}}: ok=false when ref isn't one.
func (e *Engine) panelOption(ref tmpl.Ref) (any, bool) {
	if len(ref.Path) != 2 || ref.Path[0] != tmpl.ScopeOpt {
		return nil, false
	}
	p := e.def.Panel(ref.Scope)
	if p == nil {
		return nil, false
	}
	if o := findOption(p.Options, ref.Path[1]); o != nil {
		return optionArg(o, e.optionValue(p.ID, o)), true
	}
	return nil, false
}

// optResolver is resolver(row) that also answers {{opt.x}} for owner.
func (e *Engine) optResolver(owner string, row rows.Row) tmpl.Resolver {
	base := e.resolver(row)
	return resolverFunc(func(r tmpl.Ref) (any, bool) {
		if r.Scope == tmpl.ScopeOpt {
			if o := findOption(e.ownerOptions(owner), r.Path[0]); o != nil {
				return optionArg(o, e.optionValue(owner, o)), true
			}
			return nil, false
		}
		return base.Resolve(r)
	})
}

// needsSelection reports whether t needs panel dep's selection: false only
// when t reads nothing of dep but its options ({{dep.opt.x}}), so it doesn't
// wait for dep's rows. A dep t doesn't mention (a drill parent, the env's
// selects) is needed.
func needsSelection(t *tmpl.Template, dep string) bool {
	if t == nil {
		return true
	}
	onlyOpts := false
	for _, r := range t.Refs() {
		if r.Scope != dep {
			continue
		}
		if len(r.Path) == 0 || r.Path[0] != tmpl.ScopeOpt {
			return true
		}
		onlyOpts = true
	}
	return !onlyOpts
}

// OptionSummary lists owner's options that differ from their defaults, as
// shown in titles: id=value, or just id for a toggle that is on.
func (e *Engine) optionSummary(owner string) []string {
	var out []string
	for _, o := range e.ownerOptions(owner) {
		v, d := e.optionValue(owner, o), e.optionDefault(o)
		switch {
		case v == d:
		case o.Type == "toggle" && v == "true":
			out = append(out, o.ID)
		case o.Type == "toggle":
			out = append(out, o.ID+"=off")
		case v == "":
			out = append(out, o.ID+"=any")
		default:
			out = append(out, o.ID+"="+v)
		}
	}
	return out
}

// SetOptions sets owner's options (id → value; ones not given are kept) and
// re-runs what reads them. Values are checked first: nothing changes if one
// is invalid. Dates may be relative (-1w) and are stored as YYYY-MM-DD.
func (e *Engine) SetOptions(owner string, values map[string]string) (Effects, error) {
	opts := e.ownerOptions(owner)
	next := map[string]string{}
	for id, v := range e.opts[owner] {
		next[id] = v
	}
	for id, v := range values {
		o := findOption(opts, id)
		switch {
		case o == nil:
			return e.take(), fmt.Errorf("no option %q", id)
		case o.Type == "date" && v == "" && o.Flag != "": // an optional date, unset
		case o.Type == "date":
			t, ok := def.ParseDate(v, e.now())
			if !ok {
				return e.take(), fmt.Errorf("%s: %q is not a date (YYYY-MM-DD, today, -3d, -2w)", o.Title, v)
			}
			v = t.Format(def.DateLayout)
		case o.Type == "toggle" && v != "true" && v != "false":
			return e.take(), fmt.Errorf("%s: %q is not true or false", o.Title, v)
		case o.Type == "choice" && len(o.Values) > 0 && !slices.Contains(o.Values, v):
			return e.take(), fmt.Errorf("%s: %q is not one of %s", o.Title, v, strings.Join(o.Values, ", "))
		}
		next[id] = v
	}
	e.opts[owner] = next
	if ps := e.panels[owner]; ps != nil {
		e.evaluate(ps, true)
		e.propagate(owner, true)
	}
	e.evaluateContent(true)
	return e.take(), nil
}

// OptionsInEffect lists the options the form (o) edits: the focused list
// panel's, then those of the entry each content panel shows for it. With a
// content panel focused, just its entry's.
func (e *Engine) OptionsInEffect() []OptionGroup {
	var out []OptionGroup
	add := func(owner, title string) {
		opts := e.ownerOptions(owner)
		if len(opts) == 0 {
			return
		}
		g := OptionGroup{Owner: owner, Title: title}
		for _, o := range opts {
			f := OptionField{Option: o, Value: e.optionValue(owner, o), Default: e.optionDefault(o)}
			switch c := e.choices[owner+"\x00"+o.ID]; {
			case len(o.Values) > 0:
				f.Choices = o.Values
			case c != nil:
				f.Choices, f.Loading, f.Err = c.values, c.runID != 0, c.err
			}
			g.Fields = append(g.Fields, f)
		}
		out = append(out, g)
	}
	focused := e.Focused()
	views := e.contentPanels()
	if e.IsContent(focused) {
		views = []string{focused}
	} else {
		add(focused, e.def.Panel(focused).Title)
	}
	for _, id := range views {
		entry, _ := e.entry(id)
		title := e.def.Panel(id).Title + " · " + entry
		if p := e.def.Panel(entry); p != nil {
			title = e.def.Panel(id).Title + " · " + p.Title
		}
		add(EntryOwner(id, entry), title)
	}
	return out
}

// choiceState is a choice option's list from its source command.
type choiceState struct {
	runID  uint64
	values []string
	err    string
}

// OpenOptions starts the commands that list choices for the options in
// effect (a form is about to show them).
func (e *Engine) OpenOptions() Effects {
	for _, g := range e.OptionsInEffect() {
		for _, f := range g.Fields {
			o := f.Option
			if o.Source == nil {
				continue
			}
			key := g.Owner + "\x00" + o.ID
			if c := e.choices[key]; c != nil && c.runID != 0 {
				e.fx.Cancel = append(e.fx.Cancel, c.runID)
			}
			c := &choiceState{}
			e.choices[key] = c
			cmd, err := o.Source.Render(e.resolver(nil), tmpl.Shell)
			if err != nil {
				c.err = err.Error()
				continue
			}
			e.nextID++
			c.runID = e.nextID
			owner, _, _ := strings.Cut(g.Owner, "\x00")
			e.fx.Runs = append(e.fx.Runs, Run{
				ID: c.runID, Panel: owner, Choices: true,
				Req: runner.Request{Cmd: cmd, Env: e.envFor(""), Timeout: e.def.Timeout},
			})
		}
	}
	return e.take()
}

// choicesFinished takes a choice command's output; false if id isn't one.
func (e *Engine) choicesFinished(id uint64, stdout []byte, runErr error) bool {
	for key, c := range e.choices {
		if id == 0 || c.runID != id {
			continue
		}
		c.runID = 0
		if runErr != nil {
			c.err = runErr.Error()
			return true
		}
		c.values = []string{""} // (any)
		if o := e.choiceOption(key); o != nil && o.Default != "" {
			c.values = nil
		}
		for _, l := range strings.Split(strings.TrimRight(string(stdout), "\n"), "\n") {
			if l != "" && !slices.Contains(c.values, l) {
				c.values = append(c.values, l)
			}
		}
		return true
	}
	return false
}

func (e *Engine) choiceOption(key string) *def.Option {
	i := strings.LastIndex(key, "\x00")
	return findOption(e.ownerOptions(key[:i]), key[i+1:])
}
