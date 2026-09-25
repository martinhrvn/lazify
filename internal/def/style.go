package def

import (
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"

	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Styles map a value to a decoration — a built-in helper (icon or spinner)
// and a colour — through a lookup table: no expressions. Anything that needs
// logic is computed as a category in `rows:` (jq) and then looked up.

// Icons are the built-in icon helpers (glyphs live in the UI).
var Icons = []string{"check", "cross", "warn", "info", "dot", "circle", "pause", "clock", "up", "down"}

// Colors are the colour names a decoration may use: semantic ones first.
var Colors = []string{"ok", "warn", "error", "info", "dim", "accent",
	"red", "green", "yellow", "blue", "magenta", "cyan", "gray", "white"}

// Formats are the built-in value formatters.
var Formats = []string{"ago", "duration", "bytes", "basename", "bar"}

// Deco is how a value is decorated.
type Deco struct {
	Icon     string
	Spinner  bool
	HideText bool // show only the helper
	Bold     bool
	Color    string
}

// Style maps a value (Value rendered for the row, or the element's own value
// when nil) to a decoration; Default applies to anything unlisted ("*").
type Style struct {
	Value   *tmpl.Template
	Map     map[string]Deco
	Default *Deco
}

// Lookup returns the decoration for value v, if any.
func (s *Style) Lookup(v string) (Deco, bool) {
	if d, ok := s.Map[v]; ok {
		return d, true
	}
	if s.Default != nil {
		return *s.Default, true
	}
	return Deco{}, false
}

// style parses a style node: {value, map}, or — when bare is allowed (a
// column styling its own value) — the map itself.
func (v *validator) style(n yaml.Node, line int, what string, d *Definition, bare bool) *Style {
	if n.Kind != yaml.MappingNode {
		v.errorf(line, "%s: must be a map of value → style", what)
		return nil
	}
	s := &Style{Map: map[string]Deco{}}
	entries := &n
	keys := mapKeysOf(&n)
	if len(keys) > 0 && !slices.ContainsFunc(keys, func(k string) bool { return k != "value" && k != "map" }) {
		if valueNode := mapValue(&n, "value"); valueNode != nil {
			s.Value, _ = v.template(valueNode.Line, what+": value", valueNode.Value, d, refRules{row: true, panels: true})
		}
		entries = mapValue(&n, "map")
		if entries == nil || entries.Kind != yaml.MappingNode || s.Value == nil && !bare {
			v.errorf(line, "%s needs {value: ..., map: ...}", what)
			return nil
		}
	} else if !bare {
		v.errorf(line, "%s needs {value: ..., map: ...} (value: the template to look up)", what)
		return nil
	}
	for i := 0; i+1 < len(entries.Content); i += 2 {
		key, val := entries.Content[i], entries.Content[i+1]
		deco, ok := v.deco(*val, fmt.Sprintf("%s: %q", what, key.Value))
		if !ok {
			continue
		}
		if key.Value == "*" {
			s.Default = &deco
		} else {
			s.Map[key.Value] = deco
		}
	}
	return s
}

// deco parses a decoration: a colour name, or {icon|spinner, color, bold, text}.
func (v *validator) deco(n yaml.Node, what string) (Deco, bool) {
	var d Deco
	ok := true
	color := func(c string, line int) {
		if !slices.Contains(Colors, c) {
			v.errorf(line, "%s: unknown colour %q (use one of %v)", what, c, Colors)
			ok = false
		}
		d.Color = c
	}
	switch n.Kind {
	case yaml.ScalarNode:
		color(n.Value, n.Line)
		return d, ok
	case yaml.MappingNode:
	default:
		v.errorf(n.Line, "%s: must be a colour or {icon, color, ...}", what)
		return d, false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, val := n.Content[i], n.Content[i+1]
		switch k.Value {
		case "icon":
			if !slices.Contains(Icons, val.Value) {
				v.errorf(val.Line, "%s: unknown icon %q (use one of %v)", what, val.Value, Icons)
				ok = false
			}
			d.Icon = val.Value
		case "spinner":
			d.Spinner = val.Value == "true"
		case "color", "colour":
			color(val.Value, val.Line)
		case "bold":
			d.Bold = val.Value == "true"
		case "text":
			d.HideText = val.Value == "false"
		default:
			v.errorf(k.Line, "%s: unknown field %q (use icon, spinner, color, bold, text)", what, k.Value)
			ok = false
		}
	}
	if d.Icon != "" && d.Spinner {
		v.errorf(n.Line, "%s: use either icon and spinner, not both", what)
		ok = false
	}
	return d, ok
}

// format validates a format name.
func (v *validator) format(name string, line int, what string) string {
	if name != "" && !slices.Contains(Formats, name) {
		v.errorf(line, "%s: unknown format %q (use one of %v)", what, name, Formats)
	}
	return name
}
