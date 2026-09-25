// Package def loads and validates lazify definition files.
package def

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/martinhrvn/lazify/internal/rows"
	"github.com/martinhrvn/lazify/internal/tmpl"
	"gopkg.in/yaml.v3"
)

// DefaultTimeout applies to non-stream commands unless `timeout:` overrides it.
const DefaultTimeout = 30 * time.Second

// ReservedKeys are bound by the engine and cannot be used by actions.
var ReservedKeys = []string{
	"j", "k", "up", "down", "tab", "shift+tab",
	"1", "2", "3", "4", "5", "6", "7", "8", "9",
	"enter", "esc", "[", "]", "ctrl+d", "ctrl+u", "J", "K",
	"/", "r", "?", "q", "ctrl+c", "o",
}

// Definition is a validated app definition.
type Definition struct {
	ID      string // how the app is run: `lazify <id>`
	Name    string
	File    string
	Timeout time.Duration
	Env     map[string]*tmpl.Template
	Panels  []*Panel
	Actions []*Action // global: available whatever is focused
	Layout  Layout
	// EnvDeps are the panels the env reads (sorted); every other list panel
	// depends on them.
	EnvDeps []string
	// Order lists panel ids so that every panel comes after the panels it depends on.
	Order []string
}

// Layout controls how panels are arranged around the main view.
type Layout struct {
	Focus      string // expand | equal
	LeftWidth  int    // percent of terminal width
	RightWidth int    // percent; only used when a panel is on the right
}

// SizeKind says how a panel claims height in its column.
type SizeKind int

const (
	Flex  SizeKind = iota // share the remaining height by weight N
	Fit                   // as tall as its content
	Fixed                 // exactly N lines
)

// Size is a panel's height rule.
type Size struct {
	Kind SizeKind
	N    int
}

// Panel is a list fed by a shell command.
type Panel struct {
	ID      string
	Title   string
	Source  *tmpl.Template
	Rows    string
	Split   string
	Parser  *rows.Parser
	Label   *tmpl.Template // nil when Columns are used
	Columns []Column
	Key     []string // path into the row; nil = use the rendered label
	Enter   *Enter   // what Enter opens for the selected row; nil = nothing
	Parent  string   // set on the panel another panel's Enter opens
	TabOf   string   // the panel whose slot this one is a tab in
	Values  []string // rows written in the definition instead of a source
	Select  string   // "popup" or "inline": its selection is chosen in a picker; "" = not a select
	Default string   // a select's initial choice (key, else label)
	Refresh time.Duration
	Mark    *Mark  // which rows to highlight as current; nil = none
	Side    string // left | center | right
	Size    Size
	// Content makes this a content panel: what to show, keyed by the active
	// list panel's id or "default". Nil for list panels.
	Content map[string]*Content
	// Actions apply while this panel is focused; they win over a global with
	// the same key.
	Actions []*Action
	// Deps are the panels whose selection this panel's source needs, in
	// declaration order. A drill-in child also depends on its parent.
	Deps []string
}

// Mark highlights rows: those whose Path value is truthy, or those whose Match
// (default: key, else label) is among the output lines of Source.
type Mark struct {
	Path   []string
	Source *tmpl.Template
	Match  *tmpl.Template
}

// Enter is what pressing Enter on a row opens: the target panel, either in
// place of this one (drill down) or in a popup of Width×Height percent.
type Enter struct {
	Panel         string
	Popup         bool
	Full          bool
	Width, Height int
}

// Column is one aligned column of a panel's rows.
type Column struct {
	Title string
	Value *tmpl.Template
}

// Content is what a content panel shows for one active panel: a set of tabs.
type Content struct {
	Tabs []*Tab
}

// Tab is one tab of a content panel.
type Tab struct {
	Name   string
	Cmd    *tmpl.Template
	Mode   string   // once | stream
	Format string   // text | json
	Deps   []string // panels referenced in Cmd (besides the row itself)
}

// Action is a key bound on a panel.
type Action struct {
	Key     string
	Desc    string
	Cmd     *tmpl.Template
	Prompt  string
	Confirm bool
	Mode    string // background | interactive
	Refresh []string
}

// IsSelect reports whether p is a select panel.
func (p *Panel) IsSelect() bool { return p.Select != "" }

// IsContent reports whether p is a content panel (as opposed to a list panel).
func (p *Panel) IsContent() bool { return p.Content != nil }

// Tabs lists the panels sharing owner's slot: the owner, then its tabs in
// declaration order.
func (d *Definition) Tabs(owner string) []string {
	tabs := []string{owner}
	for _, p := range d.Panels {
		if p.TabOf == owner {
			tabs = append(tabs, p.ID)
		}
	}
	return tabs
}

// Panel returns the panel with the given id, or nil.
func (d *Definition) Panel(id string) *Panel {
	for _, p := range d.Panels {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// Dependents returns the panels that directly depend on id, in declaration order.
func (d *Definition) Dependents(id string) []string {
	var out []string
	for _, p := range d.Panels {
		if slices.Contains(p.Deps, id) {
			out = append(out, p.ID)
		}
	}
	return out
}

// Error is one validation problem, positioned in the source file.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// Errors collects every problem found in a definition.
type Errors []Error

func (es Errors) Error() string {
	msgs := make([]string, len(es))
	for i, e := range es {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "\n")
}

// Load reads and validates a definition file.
func Load(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data, path)
}

type rawDef struct {
	ID      string            `yaml:"id"`
	Name    string            `yaml:"name"`
	Timeout string            `yaml:"timeout"`
	Context yaml.Node         `yaml:"context"` // removed; kept to explain the migration
	Env     map[string]string `yaml:"env"`
	Panels  []rawPanel        `yaml:"panels"`
	Detail  yaml.Node         `yaml:"detail"`  // removed; kept to explain the migration
	Actions yaml.Node         `yaml:"actions"` // list of global actions
	Layout  rawLayout         `yaml:"layout"`
}

type rawLayout struct {
	Focus      string `yaml:"focus"`
	LeftWidth  int    `yaml:"left_width"`
	RightWidth int    `yaml:"right_width"`
}

type rawPanel struct {
	ID       string                `yaml:"id"`
	Title    string                `yaml:"title"`
	Source   string                `yaml:"source"`
	Rows     string                `yaml:"rows"`
	Split    string                `yaml:"split"`
	Label    string                `yaml:"label"`
	Columns  []rawColumn           `yaml:"columns"`
	Key      string                `yaml:"key"`
	Children string                `yaml:"children"` // renamed to enter; kept to explain
	Enter    yaml.Node             `yaml:"enter"`
	TabOf    string                `yaml:"tab_of"`
	Values   []string              `yaml:"values"`
	Select   yaml.Node             `yaml:"select"`
	Default  string                `yaml:"default"`
	Refresh  string                `yaml:"refresh"`
	Size     string                `yaml:"size"`
	Side     string                `yaml:"side"`
	Content  map[string]rawContent `yaml:"content"`
	Actions  []rawAction           `yaml:"actions"`
	Mark     yaml.Node             `yaml:"mark"`
}

type rawColumn struct {
	Title string `yaml:"title"`
	Value string `yaml:"value"`
}

type rawContent struct {
	Tabs []rawTab `yaml:"tabs"`
}

type rawTab struct {
	Name   string `yaml:"name"`
	Cmd    string `yaml:"cmd"`
	Mode   string `yaml:"mode"`
	Format string `yaml:"format"`
}

type rawAction struct {
	Key     string   `yaml:"key"`
	Desc    string   `yaml:"desc"`
	Cmd     string   `yaml:"cmd"`
	Prompt  string   `yaml:"prompt"`
	Confirm bool     `yaml:"confirm"`
	Mode    string   `yaml:"mode"`
	Refresh []string `yaml:"refresh"`
}

var idRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

var yamlLineRe = regexp.MustCompile(`^(?:yaml: )?line (\d+): (.*)$`)

var unknownFieldRe = regexp.MustCompile(`^field (\S+) not found in type def\.raw(\w+)$`)

var rawTypeNames = map[string]string{
	"Def": "definition", "File": "definition", "Ctx": "context", "Panel": "panel", "Column": "column",
	"Content": "content", "Tab": "tab", "Action": "action", "Layout": "layout",
}

func yamlErrors(file string, err error) Errors {
	var errs Errors
	lines := []string{err.Error()}
	if te, ok := err.(*yaml.TypeError); ok {
		lines = te.Errors
	}
	for _, l := range lines {
		if m := yamlLineRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			var n int
			fmt.Sscan(m[1], &n)
			msg := m[2]
			if u := unknownFieldRe.FindStringSubmatch(msg); u != nil {
				msg = fmt.Sprintf("unknown field %q in %s", u[1], rawTypeNames[u[2]])
			}
			errs = append(errs, Error{File: file, Line: n, Msg: msg})
		} else {
			errs = append(errs, Error{File: file, Msg: l})
		}
	}
	return errs
}

type validator struct {
	file string
	root *yaml.Node
	errs Errors
}

func (v *validator) errorf(line int, format string, args ...any) {
	v.errs = append(v.errs, Error{File: v.file, Line: line, Msg: fmt.Sprintf(format, args...)})
}

// line returns the line of the node at path (string keys, int indexes),
// falling back to the deepest node found.
func (v *validator) line(path ...any) int {
	n := v.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	line := n.Line
	for _, p := range path {
		var next *yaml.Node
		switch key := p.(type) {
		case string:
			if n.Kind == yaml.MappingNode {
				for i := 0; i+1 < len(n.Content); i += 2 {
					if n.Content[i].Value == key {
						line = n.Content[i].Line
						next = n.Content[i+1]
						break
					}
				}
			}
		case int:
			if n.Kind == yaml.SequenceNode && key < len(n.Content) {
				next = n.Content[key]
				line = next.Line
			}
		}
		if next == nil {
			return line
		}
		n = next
	}
	return line
}

// mapKeys returns the keys of the mapping at path (string keys, int indexes)
// in file order.
func (v *validator) mapKeys(path ...any) []string {
	n := v.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, p := range path {
		var next *yaml.Node
		switch key := p.(type) {
		case string:
			for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == key {
					next = n.Content[i+1]
				}
			}
		case int:
			if n.Kind == yaml.SequenceNode && key < len(n.Content) {
				next = n.Content[key]
			}
		}
		if next == nil {
			return nil
		}
		n = next
	}
	var keys []string
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

// refRules says which kinds of reference a template may contain.
type refRules struct {
	row, input, panels bool
	self               string // panel that must not be referenced (its own source)
}

func (v *validator) template(line int, what, src string, d *Definition, r refRules) (*tmpl.Template, []string) {
	t, err := tmpl.Parse(src)
	if err != nil {
		v.errorf(line, "%s: %v", what, err)
		return nil, nil
	}
	var deps []string
	for _, ref := range t.Refs() {
		switch ref.Scope {
		case tmpl.ScopeRow:
			if !r.row {
				v.errorf(line, "%s: {{%s}} in source has no row to refer to; use panel.field", what, ref)
			}
		case tmpl.ScopeInput:
			if !r.input {
				v.errorf(line, "%s: {{input}} is only available in actions with a prompt", what)
			}
		case tmpl.ScopeCtx:
			v.errorf(line, "%s: {{%s}}: context was replaced by select panels; use {{%s.line}}", what, ref, ref.Path[0])
		default:
			switch {
			case !r.panels:
				v.errorf(line, "%s: {{%s}}: only ctx references are allowed here", what, ref)
			case ref.Scope == r.self:
				v.errorf(line, "%s: panel references itself ({{%s}})", what, ref)
			case d.Panel(ref.Scope) == nil:
				v.errorf(line, "%s: unknown panel %q", what, ref.Scope)
			case d.Panel(ref.Scope).IsContent():
				v.errorf(line, "%s: {{%s}}: %s is a content panel and has no rows", what, ref, ref.Scope)
			case !slices.Contains(deps, ref.Scope):
				deps = append(deps, ref.Scope)
			}
		}
	}
	return t, deps
}

func (v *validator) build(raw *rawDef) *Definition {
	d := &Definition{
		Name:    raw.Name,
		File:    v.file,
		Timeout: DefaultTimeout,
		Env:     map[string]*tmpl.Template{},
	}
	if raw.Timeout != "" {
		t, err := time.ParseDuration(raw.Timeout)
		if err != nil || t <= 0 {
			v.errorf(v.line("timeout"), "timeout: invalid duration %q", raw.Timeout)
		} else {
			d.Timeout = t
		}
	}

	if raw.Context.Kind != 0 {
		v.errorf(v.line("context"), "context: was replaced by select panels: a panel with select: true and values: or source: (referenced as {{id.line}})")
	}

	if len(raw.Panels) == 0 {
		v.errorf(v.line("panels"), "no panels defined")
		return d
	}

	// Pass 1: ids, so references can be checked regardless of order.
	for i, rp := range raw.Panels {
		line := v.line("panels", i)
		switch {
		case rp.ID == "":
			v.errorf(line, "panel: id is required")
			continue
		case !idRe.MatchString(rp.ID):
			v.errorf(line, "panel %s: invalid id (use letters, digits, _ and -)", rp.ID)
			continue
		case rp.ID == tmpl.ScopeCtx || rp.ID == tmpl.ScopeInput || rp.ID == "default":
			v.errorf(line, "panel %s: id is reserved", rp.ID)
			continue
		case d.Panel(rp.ID) != nil:
			v.errorf(line, "panel %s: duplicate id", rp.ID)
			continue
		}
		title := rp.Title
		if title == "" {
			title = rp.ID
		}
		p := &Panel{ID: rp.ID, Title: title}
		if rp.Content != nil {
			p.Content = map[string]*Content{}
		}
		d.Panels = append(d.Panels, p)
	}

	// Env may reference panels (select panels, in practice), so it is checked
	// once all ids are known.
	for _, name := range sortedKeys(raw.Env) {
		t, deps := v.template(v.line("env", name), "env "+name, raw.Env[name], d, refRules{panels: true})
		d.Env[name] = t
		for _, dep := range deps {
			if !slices.Contains(d.EnvDeps, dep) {
				d.EnvDeps = append(d.EnvDeps, dep)
			}
		}
	}
	slices.Sort(d.EnvDeps)

	// Pass 2: everything else.
	built := map[string]bool{}
	for i, rp := range raw.Panels {
		p := d.Panel(rp.ID)
		if p == nil || built[p.ID] {
			continue // invalid id, or the duplicate of an already-built panel
		}
		built[p.ID] = true
		at := func(field string) int { return v.line("panels", i, field) }
		what := "panel " + p.ID

		if p.IsContent() {
			v.contentPanel(d, p, rp, i)
		} else {
			v.listPanel(d, p, rp, i)
		}

		p.Side = rp.Side
		if p.Side == "" {
			p.Side = "left"
			if p.IsContent() {
				p.Side = "center"
			}
		}
		if p.Side != "left" && p.Side != "center" && p.Side != "right" {
			v.errorf(at("side"), "%s: side must be left, center or right, got %q", what, rp.Side)
		}
		p.Size = Size{Kind: Flex, N: 1}
		if p.IsSelect() {
			p.Size = Size{Kind: Fit} // a select shows one line: its choice
		}
		if rp.Size != "" {
			if s, ok := parseSize(rp.Size); ok {
				p.Size = s
			} else {
				v.errorf(at("size"), "%s: size must be fit, a line count or <n>fr, got %q", what, rp.Size)
			}
		}
	}
	// Children depend on their parent even without referencing it.
	for _, p := range d.Panels {
		if p.Parent != "" && !slices.Contains(p.Deps, p.Parent) {
			p.Deps = append(p.Deps, p.Parent)
		}
	}
	// Tabs share their owner's slot. Checked after Enter targets are known.
	rawTabOf := map[string]string{}
	for _, rp := range raw.Panels {
		rawTabOf[rp.ID] = rp.TabOf
	}
	for i, rp := range raw.Panels {
		if p := d.Panel(rp.ID); p != nil && rp.TabOf != "" && !p.IsContent() {
			v.tabOf(d, p, rp, i, rawTabOf)
		}
	}
	// A drill-in child is shown in its parent's slot, so it has no layout of its own.
	for i, rp := range raw.Panels {
		if p := d.Panel(rp.ID); p != nil && p.Parent != "" && (rp.Side != "" || rp.Size != "") {
			v.errorf(v.line("panels", i), "panel %s: side/size not allowed on a drill-in child (it uses %s's slot)", p.ID, p.Parent)
		}
	}
	v.spreadEnvDeps(d)
	v.order(d)
	v.layout(d, raw.Layout)

	if raw.Detail.Kind != 0 {
		v.errorf(v.line("detail"), "detail: was replaced by content panels — move each entry under a panel's content: (see docs/design.md)")
	}

	// Actions last: they may refresh any panel.
	if raw.Actions.Kind == yaml.MappingNode {
		v.errorf(v.line("actions"), "actions: panel actions now live in the panel (panels[].actions); top-level actions: is a list of global actions")
	} else if raw.Actions.Kind != 0 {
		var globals []rawAction
		if err := raw.Actions.Decode(&globals); err != nil {
			v.errorf(v.line("actions"), "actions: %v", err)
		}
		d.Actions = v.actions(d, globals, "global action", "actions")
	}
	for i, rp := range raw.Panels {
		if p := d.Panel(rp.ID); p != nil && p.Actions == nil && len(rp.Actions) > 0 {
			p.Actions = v.actions(d, rp.Actions, "panel "+p.ID+": action", "panels", i, "actions")
		}
	}
	return d
}

// actions validates one list of actions (global, or one panel's) found at path.
func (v *validator) actions(d *Definition, raws []rawAction, what string, path ...any) []*Action {
	var out []*Action
	seen := map[string]bool{}
	for ai, ra := range raws {
		al := v.line(append(slices.Clone(path), ai)...)
		w := fmt.Sprintf("%s %q", what, ra.Key)
		a := &Action{Key: ra.Key, Desc: ra.Desc, Prompt: ra.Prompt, Confirm: ra.Confirm,
			Mode: or(ra.Mode, "background"), Refresh: ra.Refresh}
		switch {
		case ra.Key == "":
			v.errorf(al, "%s: key is required", what)
		case slices.Contains(ReservedKeys, ra.Key):
			v.errorf(al, "%s: key %q is reserved", w, ra.Key)
		case seen[ra.Key]:
			v.errorf(al, "%s: duplicate key %q", w, ra.Key)
		}
		seen[ra.Key] = true
		if ra.Cmd == "" {
			v.errorf(al, "%s: cmd is required", w)
		} else {
			a.Cmd, _ = v.template(al, w, ra.Cmd, d, refRules{row: true, panels: true, input: true})
			if a.Cmd != nil && a.Prompt == "" && slices.ContainsFunc(a.Cmd.Refs(), func(r tmpl.Ref) bool {
				return r.Scope == tmpl.ScopeInput
			}) {
				v.errorf(al, "%s: uses {{input}} but has no prompt", w)
			}
		}
		if a.Mode != "background" && a.Mode != "interactive" {
			v.errorf(al, "%s: mode must be background or interactive, got %q", w, a.Mode)
		}
		for _, r := range ra.Refresh {
			if d.Panel(r) == nil {
				v.errorf(al, "%s: refresh: unknown panel %q", w, r)
			}
		}
		out = append(out, a)
	}
	return out
}

// layout validates the layout block and fills in defaults.
func (v *validator) layout(d *Definition, rl rawLayout) {
	hasRight := slices.ContainsFunc(d.Panels, func(p *Panel) bool { return p.Side == "right" })
	d.Layout = Layout{Focus: or(rl.Focus, "expand"), LeftWidth: rl.LeftWidth, RightWidth: rl.RightWidth}
	if d.Layout.Focus != "expand" && d.Layout.Focus != "equal" {
		v.errorf(v.line("layout", "focus"), "layout: focus must be expand or equal, got %q", rl.Focus)
	}
	if d.Layout.LeftWidth == 0 {
		d.Layout.LeftWidth = 40
		if hasRight {
			d.Layout.LeftWidth = 30
		}
	}
	if d.Layout.RightWidth == 0 {
		d.Layout.RightWidth = 25
	}
	for _, w := range []struct {
		name string
		val  int
	}{{"left_width", d.Layout.LeftWidth}, {"right_width", d.Layout.RightWidth}} {
		if w.val < 10 || w.val > 80 {
			v.errorf(v.line("layout", w.name), "layout: %s must be between 10 and 80 (percent), got %d", w.name, w.val)
		}
	}
	if d.Layout.LeftWidth+d.Layout.RightWidth > 90 {
		v.errorf(v.line("layout"), "layout: left_width + right_width must leave at least 10%% for main")
	}
}

// parseSize parses `fit`, `<n>` (fixed lines) or `<n>fr` (flex weight).
func parseSize(s string) (Size, bool) {
	if s == "fit" {
		return Size{Kind: Fit}, true
	}
	kind, num := Fixed, s
	if n, ok := strings.CutSuffix(s, "fr"); ok {
		kind, num = Flex, n
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 {
		return Size{}, false
	}
	return Size{Kind: kind, N: n}, true
}

// listPanel validates the fields of a list panel: a source and how its rows
// are parsed, displayed and drilled into.
func (v *validator) listPanel(d *Definition, p *Panel, rp rawPanel, i int) {
	at := func(field string) int { return v.line("panels", i, field) }
	what := "panel " + p.ID

	switch {
	case rp.Source != "" && len(rp.Values) > 0:
		v.errorf(at("values"), "%s: use either source or values, not both", what)
	case len(rp.Values) > 0:
		p.Values = rp.Values
	case rp.Source == "":
		v.errorf(v.line("panels", i), "%s: source is required (or values, or content for a content panel)", what)
	default:
		p.Source, p.Deps = v.template(at("source"), what+": source", rp.Source, d,
			refRules{panels: true, self: p.ID})
	}

	p.Default = rp.Default
	switch mode := rp.Select.Value; {
	case rp.Select.Kind == 0 || mode == "false":
	case mode == "true" || mode == "popup":
		p.Select = "popup"
	case mode == "inline":
		p.Select = "inline"
	default:
		v.errorf(at("select"), "%s: select must be true, popup or inline, got %q", what, mode)
	}
	if rp.Default != "" && !p.IsSelect() {
		v.errorf(at("default"), "%s: default only applies with select: true", what)
	}

	p.Rows, p.Split = rp.Rows, rp.Split
	if rp.Rows != "" && rp.Split != "" {
		v.errorf(at("split"), "%s: split only applies to line output, not with rows", what)
	}
	if parser, err := rows.New(rp.Rows, rp.Split); err != nil {
		v.errorf(at("rows"), "%s: %v", what, err)
	} else {
		p.Parser = parser
	}

	if rp.Label != "" && len(rp.Columns) > 0 {
		v.errorf(at("label"), "%s: use either label and columns, not both", what)
	}
	display := refRules{row: true, panels: true}
	if rp.Label != "" {
		p.Label, _ = v.template(at("label"), what+": label", rp.Label, d, display)
	}
	for ci, rc := range rp.Columns {
		line := v.line("panels", i, "columns", ci)
		if rc.Value == "" {
			v.errorf(line, "%s: column %d: value is required", what, ci+1)
			continue
		}
		t, _ := v.template(line, what+": column", rc.Value, d, display)
		p.Columns = append(p.Columns, Column{Title: rc.Title, Value: t})
	}
	if p.Label == nil && len(p.Columns) == 0 {
		if rp.Rows == "" {
			p.Label = tmpl.MustParse("{{.line}}")
		} else {
			p.Label = tmpl.MustParse("{{.}}")
		}
	}

	if rp.Key != "" {
		p.Key = v.keyPath(at("key"), what, rp.Key)
	}
	if rp.Refresh != "" {
		r, err := time.ParseDuration(rp.Refresh)
		if err != nil || r <= 0 {
			v.errorf(at("refresh"), "%s: refresh: invalid duration %q", what, rp.Refresh)
		}
		p.Refresh = r
	}

	if rp.Mark.Kind != 0 {
		p.Mark = v.mark(d, p, rp.Mark, at("mark"))
	}

	if rp.Children != "" {
		v.errorf(at("children"), "%s: children: was renamed to enter: (drill down), or use enter: {panel: %s, popup: true}", what, rp.Children)
	}
	if rp.Enter.Kind != 0 {
		p.Enter = v.enter(d, p, rp.Enter, at("enter"))
	}
}

// contentPanel validates a content panel: for each active list panel (or
// "default"), the tabs to show.
func (v *validator) contentPanel(d *Definition, p *Panel, rp rawPanel, i int) {
	what := "panel " + p.ID
	if rp.Source != "" {
		v.errorf(v.line("panels", i, "source"), "%s: use either source or content, not both", what)
	}
	for field, set := range map[string]bool{
		"rows": rp.Rows != "", "split": rp.Split != "", "label": rp.Label != "",
		"columns": len(rp.Columns) > 0, "key": rp.Key != "", "children": rp.Children != "", "enter": rp.Enter.Kind != 0, "tab_of": rp.TabOf != "", "select": rp.Select.Kind != 0, "default": rp.Default != "", "values": len(rp.Values) > 0,
		"refresh": rp.Refresh != "", "mark": rp.Mark.Kind != 0,
	} {
		if set {
			v.errorf(v.line("panels", i, field), "%s: %s only applies to list panels", what, field)
		}
	}

	for _, key := range v.mapKeys("panels", i, "content") {
		line := v.line("panels", i, "content", key)
		if key != "default" {
			switch src := d.Panel(key); {
			case src == nil:
				v.errorf(line, "%s: content: unknown panel %q", what, key)
				continue
			case src.IsContent():
				v.errorf(line, "%s: content: %s is a content panel; use the id of a list panel", what, key)
				continue
			}
		}
		c := &Content{}
		for ti, rt := range rp.Content[key].Tabs {
			tl := v.line("panels", i, "content", key, "tabs", ti)
			tw := fmt.Sprintf("%s: content %s: tab %q", what, key, rt.Name)
			tab := &Tab{Name: rt.Name, Mode: or(rt.Mode, "once"), Format: or(rt.Format, "text")}
			if tab.Name == "" {
				v.errorf(tl, "%s: content %s: tab %d: name is required", what, key, ti+1)
			}
			if rt.Cmd == "" {
				v.errorf(tl, "%s: cmd is required", tw)
			} else {
				tab.Cmd, tab.Deps = v.template(tl, tw, rt.Cmd, d, refRules{row: true, panels: true})
			}
			if tab.Mode != "once" && tab.Mode != "stream" {
				v.errorf(tl, "%s: mode must be once or stream, got %q", tw, tab.Mode)
			}
			if tab.Format != "text" && tab.Format != "json" {
				v.errorf(tl, "%s: format must be text or json, got %q", tw, tab.Format)
			} else if tab.Format == "json" && tab.Mode == "stream" {
				v.errorf(tl, "%s: format json is only supported with mode once", tw)
			}
			c.Tabs = append(c.Tabs, tab)
		}
		p.Content[key] = c
	}
}

// keyPath parses a row path such as `.fields.0`.
func (v *validator) keyPath(line int, what, key string) []string {
	t, err := tmpl.Parse("{{" + key + "}}")
	if err != nil || len(t.Refs()) != 1 || t.Refs()[0].Scope != tmpl.ScopeRow {
		v.errorf(line, "%s: key must be a row path like .id or .fields.0, got %q", what, key)
		return nil
	}
	return t.Refs()[0].Path
}

// order computes a topological order of panels, reporting cycles.
func (v *validator) order(d *Definition) {
	const (
		unvisited = iota
		visiting
		done
	)
	state := map[string]int{}
	var stack []string
	var visit func(p *Panel) bool
	visit = func(p *Panel) bool {
		switch state[p.ID] {
		case done:
			return true
		case visiting:
			start := slices.Index(stack, p.ID)
			cycle := append(slices.Clone(stack[start:]), p.ID)
			v.errorf(v.line("panels"), "dependency cycle: %s", strings.Join(cycle, " → "))
			return false
		}
		state[p.ID] = visiting
		stack = append(stack, p.ID)
		for _, dep := range p.Deps {
			if !visit(d.Panel(dep)) {
				return false
			}
		}
		stack = stack[:len(stack)-1]
		state[p.ID] = done
		d.Order = append(d.Order, p.ID)
		return true
	}
	for _, p := range d.Panels {
		if !visit(p) {
			return
		}
	}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mark validates a panel's mark: a row path, or {source, match}.
func (v *validator) mark(d *Definition, p *Panel, n yaml.Node, line int) *Mark {
	what := "panel " + p.ID + ": mark"
	if n.Kind == yaml.ScalarNode {
		t, err := tmpl.Parse("{{" + n.Value + "}}")
		if err != nil || len(t.Refs()) != 1 || t.Refs()[0].Scope != tmpl.ScopeRow || n.Value == "." {
			v.errorf(line, "%s: must be a row path like .current or {source: ..., match: ...}, got %q", what, n.Value)
			return nil
		}
		return &Mark{Path: t.Refs()[0].Path}
	}
	if n.Kind != yaml.MappingNode {
		v.errorf(line, "%s: must be a row path like .current or {source: ..., match: ...}", what)
		return nil
	}
	var src, match string
	for i := 0; i+1 < len(n.Content); i += 2 {
		switch k, val := n.Content[i].Value, n.Content[i+1].Value; k {
		case "source":
			src = val
		case "match":
			match = val
		default:
			v.errorf(n.Content[i].Line, "%s: unknown field %q (use source and match)", what, k)
		}
	}
	m := &Mark{}
	if src == "" {
		v.errorf(line, "%s: source is required", what)
	} else {
		var deps []string
		m.Source, deps = v.template(line, what+": source", src, d, refRules{panels: true, self: p.ID})
		for _, dep := range deps {
			if !slices.Contains(p.Deps, dep) {
				p.Deps = append(p.Deps, dep)
			}
		}
	}
	if match != "" {
		m.Match, _ = v.template(line, what+": match", match, d, refRules{row: true, panels: true})
	}
	return m
}

// enter validates a panel's enter: a panel id (drill down), or
// {panel, popup: true | full | {width, height}}.
func (v *validator) enter(d *Definition, p *Panel, n yaml.Node, line int) *Enter {
	what := "panel " + p.ID + ": enter"
	e := &Enter{}
	switch n.Kind {
	case yaml.ScalarNode:
		e.Panel = n.Value
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, val := n.Content[i], n.Content[i+1]
			switch k.Value {
			case "panel":
				e.Panel = val.Value
			case "popup":
				v.popup(e, val, what)
			default:
				v.errorf(k.Line, "%s: unknown field %q (use panel and popup)", what, k.Value)
			}
		}
	default:
		v.errorf(line, "%s: must be a panel id or {panel: ..., popup: ...}", what)
		return nil
	}

	target := d.Panel(e.Panel)
	switch {
	case e.Panel == "":
		v.errorf(line, "%s: panel is required", what)
	case target == nil:
		v.errorf(line, "%s: unknown panel %q", what, e.Panel)
	case target == p:
		v.errorf(line, "%s: a panel cannot enter itself", what)
	case target.Parent != "":
		v.errorf(line, "%s: %s is already entered from %s", what, target.ID, target.Parent)
	default:
		target.Parent = p.ID
		return e
	}
	return nil
}

func (v *validator) popup(e *Enter, n *yaml.Node, what string) {
	bad := func() { v.errorf(n.Line, "%s: popup must be true, full or {width, height} (percent)", what) }
	switch {
	case n.Kind == yaml.ScalarNode && n.Value == "false":
	case n.Kind == yaml.ScalarNode && n.Value == "true":
		e.Popup, e.Width, e.Height = true, 80, 80
	case n.Kind == yaml.ScalarNode && n.Value == "full":
		e.Popup, e.Full, e.Width, e.Height = true, true, 100, 100
	case n.Kind == yaml.MappingNode:
		e.Popup, e.Width, e.Height = true, 80, 80
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, val := n.Content[i], n.Content[i+1]
			pct, err := strconv.Atoi(val.Value)
			if k.Value != "width" && k.Value != "height" {
				v.errorf(k.Line, "%s: popup: unknown field %q (use width and height)", what, k.Value)
				continue
			}
			if err != nil || pct < 10 || pct > 100 {
				v.errorf(val.Line, "%s: popup %s must be between 10 and 100 (percent), got %q", what, k.Value, val.Value)
				continue
			}
			if k.Value == "width" {
				e.Width = pct
			} else {
				e.Height = pct
			}
		}
	default:
		bad()
	}
}

// tabOf validates `tab_of`: p joins the slot of a top-level list panel.
func (v *validator) tabOf(d *Definition, p *Panel, rp rawPanel, i int, rawTabOf map[string]string) {
	line := v.line("panels", i, "tab_of")
	what := "panel " + p.ID + ": tab_of"
	owner := d.Panel(rp.TabOf)
	switch {
	case owner == nil:
		v.errorf(line, "%s: unknown panel %q", what, rp.TabOf)
	case owner == p:
		v.errorf(line, "%s: a panel cannot be its own tab", what)
	case rawTabOf[owner.ID] != "":
		v.errorf(line, "%s: %s is itself a tab of %s", what, owner.ID, rawTabOf[owner.ID])
	case owner.IsContent():
		v.errorf(line, "%s: %s is a content panel (content panels have tabs of their own)", what, owner.ID)
	case owner.Parent != "":
		v.errorf(line, "%s: %s is opened with Enter from %s; tabs need a top-level panel", what, owner.ID, owner.Parent)
	case p.Parent != "":
		v.errorf(line, "%s: a tab cannot be opened with Enter (from %s)", what, p.Parent)
	case rp.Side != "" || rp.Size != "":
		v.errorf(v.line("panels", i), "panel %s: side/size not allowed on a tab (it uses %s's slot)", p.ID, owner.ID)
	default:
		p.TabOf = owner.ID
	}
}

// spreadEnvDeps makes every list panel depend on the panels the env reads,
// since every command runs with the env — except those panels and their own
// inputs, which would otherwise depend on themselves.
func (v *validator) spreadEnvDeps(d *Definition) {
	if len(d.EnvDeps) == 0 {
		return
	}
	upstream := map[string]bool{}
	queue := slices.Clone(d.EnvDeps)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if upstream[id] {
			continue
		}
		upstream[id] = true
		if p := d.Panel(id); p != nil {
			queue = append(queue, p.Deps...)
		}
	}
	for _, p := range d.Panels {
		if p.IsContent() || p.Values != nil || upstream[p.ID] {
			continue // values panels run no command
		}
		for _, dep := range d.EnvDeps {
			if !slices.Contains(p.Deps, dep) {
				p.Deps = append(p.Deps, dep)
			}
		}
	}
}
