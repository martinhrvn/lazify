// Package def loads and validates lazify definition files.
package def

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
	"/", "r", "c", "?", "q", "ctrl+c", "o",
}

// Definition is a validated app definition.
type Definition struct {
	Name         string
	File         string
	Timeout      time.Duration
	Context      map[string]*ContextVar
	ContextOrder []string
	Env          map[string]*tmpl.Template
	Panels       []*Panel
	Detail       map[string]*Detail
	Actions      map[string][]*Action
	Layout       Layout
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

// ContextVar is an app-wide switchable value.
type ContextVar struct {
	Name    string
	Source  *tmpl.Template // one of Source/Values
	Values  []string
	Default string
}

// Panel is a list fed by a shell command.
type Panel struct {
	ID       string
	Title    string
	Source   *tmpl.Template
	Rows     string
	Split    string
	Parser   *rows.Parser
	Label    *tmpl.Template // nil when Columns are used
	Columns  []Column
	Key      []string // path into the row; nil = use the rendered label
	Children string
	Parent   string // set on the panel named by another panel's Children
	Refresh  time.Duration
	Side     string // left | right
	Size     Size
	// Deps are the panels whose selection this panel's source needs, in
	// declaration order. A drill-in child also depends on its parent.
	Deps []string
}

// Column is one aligned column of a panel's rows.
type Column struct {
	Title string
	Value *tmpl.Template
}

// Detail holds the main-view tabs for a panel.
type Detail struct {
	Tabs []*Tab
}

// Tab is one detail tab.
type Tab struct {
	Name   string
	Cmd    *tmpl.Template
	Mode   string // once | stream
	Format string // text | json
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
	Name    string                 `yaml:"name"`
	Timeout string                 `yaml:"timeout"`
	Context map[string]rawCtx      `yaml:"context"`
	Env     map[string]string      `yaml:"env"`
	Panels  []rawPanel             `yaml:"panels"`
	Detail  map[string]rawDetail   `yaml:"detail"`
	Actions map[string][]rawAction `yaml:"actions"`
	Layout  rawLayout              `yaml:"layout"`
}

type rawLayout struct {
	Focus      string `yaml:"focus"`
	LeftWidth  int    `yaml:"left_width"`
	RightWidth int    `yaml:"right_width"`
}

type rawCtx struct {
	Source  string   `yaml:"source"`
	Values  []string `yaml:"values"`
	Default string   `yaml:"default"`
}

type rawPanel struct {
	ID       string      `yaml:"id"`
	Title    string      `yaml:"title"`
	Source   string      `yaml:"source"`
	Rows     string      `yaml:"rows"`
	Split    string      `yaml:"split"`
	Label    string      `yaml:"label"`
	Columns  []rawColumn `yaml:"columns"`
	Key      string      `yaml:"key"`
	Children string      `yaml:"children"`
	Refresh  string      `yaml:"refresh"`
	Size     string      `yaml:"size"`
	Side     string      `yaml:"side"`
}

type rawColumn struct {
	Title string `yaml:"title"`
	Value string `yaml:"value"`
}

type rawDetail struct {
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
	"Def": "definition", "Ctx": "context", "Panel": "panel", "Column": "column",
	"Detail": "detail", "Tab": "tab", "Action": "action", "Layout": "layout",
}

// Parse validates a definition from YAML. file is used in error messages.
func Parse(data []byte, file string) (*Definition, error) {
	var raw rawDef
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	var typeErrs Errors
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		// Type errors (unknown fields, wrong kinds) still leave a usable partial
		// decode, so keep validating to report everything at once.
		if _, ok := err.(*yaml.TypeError); !ok {
			return nil, yamlErrors(file, err)
		}
		typeErrs = yamlErrors(file, err)
	}
	var root yaml.Node
	_ = yaml.Unmarshal(data, &root)

	v := &validator{file: file, root: &root, errs: typeErrs}
	d := v.build(&raw)
	if len(v.errs) > 0 {
		return nil, v.errs
	}
	return d, nil
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

// mapKeys returns a mapping's keys in file order.
func (v *validator) mapKeys(path ...any) []string {
	n := v.root
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	for _, p := range path {
		var next *yaml.Node
		for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == p {
				next = n.Content[i+1]
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
			if _, ok := d.Context[ref.Path[0]]; !ok {
				v.errorf(line, "%s: unknown context %q", what, ref.Path[0])
			}
		default:
			switch {
			case !r.panels:
				v.errorf(line, "%s: {{%s}}: only ctx references are allowed here", what, ref)
			case ref.Scope == r.self:
				v.errorf(line, "%s: panel references itself ({{%s}})", what, ref)
			case d.Panel(ref.Scope) == nil:
				v.errorf(line, "%s: unknown panel %q", what, ref.Scope)
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
		Context: map[string]*ContextVar{},
		Env:     map[string]*tmpl.Template{},
		Detail:  map[string]*Detail{},
		Actions: map[string][]*Action{},
	}
	if d.Name == "" {
		d.Name = strings.TrimSuffix(filepath.Base(v.file), filepath.Ext(v.file))
	}
	if raw.Timeout != "" {
		t, err := time.ParseDuration(raw.Timeout)
		if err != nil || t <= 0 {
			v.errorf(v.line("timeout"), "timeout: invalid duration %q", raw.Timeout)
		} else {
			d.Timeout = t
		}
	}

	// Context first: everything else may reference it.
	d.ContextOrder = v.mapKeys("context")
	for _, name := range d.ContextOrder {
		d.Context[name] = &ContextVar{Name: name}
	}
	for _, name := range d.ContextOrder {
		rc, cv, line := raw.Context[name], d.Context[name], v.line("context", name)
		if (rc.Source == "") == (len(rc.Values) == 0) {
			v.errorf(line, "context %s: needs exactly one of source or values", name)
		}
		if rc.Source != "" {
			cv.Source, _ = v.template(line, "context "+name, rc.Source, d, refRules{})
		}
		cv.Values, cv.Default = rc.Values, rc.Default
		if cv.Default == "" && len(cv.Values) > 0 {
			cv.Default = cv.Values[0]
		}
	}
	for _, name := range sortedKeys(raw.Env) {
		d.Env[name], _ = v.template(v.line("env", name), "env "+name, raw.Env[name], d, refRules{})
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
		case rp.ID == tmpl.ScopeCtx || rp.ID == tmpl.ScopeInput:
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
		d.Panels = append(d.Panels, &Panel{ID: rp.ID, Title: title})
	}

	// Pass 2: everything else.
	for i, rp := range raw.Panels {
		p := d.Panel(rp.ID)
		if p == nil || p.Source != nil {
			continue // invalid id, or the duplicate of an already-built panel
		}
		at := func(field string) int { return v.line("panels", i, field) }
		what := "panel " + p.ID

		if rp.Source == "" {
			v.errorf(v.line("panels", i), "%s: source is required", what)
		} else {
			p.Source, p.Deps = v.template(at("source"), what+": source", rp.Source, d,
				refRules{panels: true, self: p.ID})
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

		p.Side = or(rp.Side, "left")
		if p.Side != "left" && p.Side != "right" {
			v.errorf(at("side"), "%s: side must be left or right, got %q", what, rp.Side)
		}
		p.Size = Size{Kind: Flex, N: 1}
		if rp.Size != "" {
			if s, ok := parseSize(rp.Size); ok {
				p.Size = s
			} else {
				v.errorf(at("size"), "%s: size must be fit, a line count or <n>fr, got %q", what, rp.Size)
			}
		}

		if rp.Children != "" {
			child := d.Panel(rp.Children)
			switch {
			case child == nil:
				v.errorf(at("children"), "%s: children: unknown panel %q", what, rp.Children)
			case child == p:
				v.errorf(at("children"), "%s: children: a panel cannot drill into itself", what)
			case child.Parent != "":
				v.errorf(at("children"), "%s: children: %s is already a child of %s", what, child.ID, child.Parent)
			default:
				p.Children, child.Parent = child.ID, p.ID
			}
		}
	}
	// Children depend on their parent even without referencing it.
	for _, p := range d.Panels {
		if p.Parent != "" && !slices.Contains(p.Deps, p.Parent) {
			p.Deps = append(p.Deps, p.Parent)
		}
	}
	// A drill-in child is shown in its parent's slot, so it has no layout of its own.
	for i, rp := range raw.Panels {
		if p := d.Panel(rp.ID); p != nil && p.Parent != "" && (rp.Side != "" || rp.Size != "") {
			v.errorf(v.line("panels", i), "panel %s: side/size not allowed on a drill-in child (it uses %s's slot)", p.ID, p.Parent)
		}
	}
	v.order(d)
	v.layout(d, raw.Layout)

	for _, id := range v.mapKeys("detail") {
		line := v.line("detail", id)
		if d.Panel(id) == nil {
			v.errorf(line, "detail: unknown panel %q", id)
			continue
		}
		det := &Detail{}
		for ti, rt := range raw.Detail[id].Tabs {
			tl := v.line("detail", id, "tabs", ti)
			what := fmt.Sprintf("detail %s: tab %q", id, rt.Name)
			tab := &Tab{Name: rt.Name, Mode: or(rt.Mode, "once"), Format: or(rt.Format, "text")}
			if tab.Name == "" {
				v.errorf(tl, "detail %s: tab %d: name is required", id, ti+1)
			}
			if rt.Cmd == "" {
				v.errorf(tl, "%s: cmd is required", what)
			} else {
				tab.Cmd, _ = v.template(tl, what, rt.Cmd, d, refRules{row: true, panels: true})
			}
			if tab.Mode != "once" && tab.Mode != "stream" {
				v.errorf(tl, "%s: mode must be once or stream, got %q", what, tab.Mode)
			}
			if tab.Format != "text" && tab.Format != "json" {
				v.errorf(tl, "%s: format must be text or json, got %q", what, tab.Format)
			}
			det.Tabs = append(det.Tabs, tab)
		}
		d.Detail[id] = det
	}

	for _, id := range v.mapKeys("actions") {
		if d.Panel(id) == nil {
			v.errorf(v.line("actions", id), "actions: unknown panel %q", id)
			continue
		}
		seen := map[string]bool{}
		for ai, ra := range raw.Actions[id] {
			al := v.line("actions", id, ai)
			what := fmt.Sprintf("action %s %q", id, ra.Key)
			a := &Action{Key: ra.Key, Desc: ra.Desc, Prompt: ra.Prompt, Confirm: ra.Confirm,
				Mode: or(ra.Mode, "background"), Refresh: ra.Refresh}
			switch {
			case ra.Key == "":
				v.errorf(al, "actions %s: key is required", id)
			case slices.Contains(ReservedKeys, ra.Key):
				v.errorf(al, "%s: key %q is reserved", what, ra.Key)
			case seen[ra.Key]:
				v.errorf(al, "%s: duplicate key %q", what, ra.Key)
			}
			seen[ra.Key] = true
			if ra.Cmd == "" {
				v.errorf(al, "%s: cmd is required", what)
			} else {
				a.Cmd, _ = v.template(al, what, ra.Cmd, d, refRules{row: true, panels: true, input: true})
				if a.Cmd != nil && a.Prompt == "" && slices.ContainsFunc(a.Cmd.Refs(), func(r tmpl.Ref) bool {
					return r.Scope == tmpl.ScopeInput
				}) {
					v.errorf(al, "%s: uses {{input}} but has no prompt", what)
				}
			}
			if a.Mode != "background" && a.Mode != "interactive" {
				v.errorf(al, "%s: mode must be background or interactive, got %q", what, a.Mode)
			}
			for _, r := range ra.Refresh {
				if d.Panel(r) == nil {
					v.errorf(al, "%s: refresh: unknown panel %q", what, r)
				}
			}
			d.Actions[id] = append(d.Actions[id], a)
		}
	}
	return d
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
