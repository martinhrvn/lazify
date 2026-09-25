// Package tmpl implements lazify's deliberately tiny template language:
// literal text with {{ref}} substitutions and nothing else.
//
// A ref is one of:
//
//	.            the whole current row
//	.a.0.b       a field of the current row
//	panel.a.b    a field of another panel's selection (bare `panel` = whole row)
//	ctx.name     a context value
//	input        the value typed into an action prompt
//
// `\{{` produces a literal `{{`, e.g. for `docker ps --format '\{{.Names}}'`.
package tmpl

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Scopes with special meaning. Any other non-empty scope is a panel id.
const (
	ScopeRow   = ""
	ScopeCtx   = "ctx"
	ScopeInput = "input"
)

// Ref is a parsed reference.
type Ref struct {
	Scope string
	Path  []string
}

func (r Ref) String() string {
	path := strings.Join(r.Path, ".")
	if r.Scope == ScopeRow {
		return "." + path
	}
	if path == "" {
		return r.Scope
	}
	return r.Scope + "." + path
}

// Resolver supplies the value for a ref. ok=false means the value is missing.
type Resolver interface {
	Resolve(Ref) (value any, ok bool)
}

// Mode selects how substituted values are inserted.
type Mode int

const (
	// Display inserts values raw; missing values render empty.
	Display Mode = iota
	// Shell shell-quotes values; missing values are an error.
	Shell
)

// MissingError is returned when rendering a command whose ref has no value.
type MissingError struct{ Ref Ref }

func (e *MissingError) Error() string { return "missing field " + e.Ref.String() }

// Template is a parsed template.
type Template struct {
	src   string
	parts []part
}

type part struct {
	lit   string
	ref   Ref
	isRef bool
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$|^[0-9]+$`)

// Parse parses a template string.
func Parse(s string) (*Template, error) {
	t := &Template{src: s}
	var lit strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], `\{{`):
			lit.WriteString("{{")
			i += 3
		case strings.HasPrefix(s[i:], "{{"):
			end := strings.Index(s[i+2:], "}}")
			if end < 0 {
				return nil, fmt.Errorf("unclosed {{ at offset %d", i)
			}
			ref, err := parseRef(s[i+2 : i+2+end])
			if err != nil {
				return nil, fmt.Errorf("at offset %d: %w", i, err)
			}
			if lit.Len() > 0 {
				t.parts = append(t.parts, part{lit: lit.String()})
				lit.Reset()
			}
			t.parts = append(t.parts, part{ref: ref, isRef: true})
			i += 2 + end + 2
		default:
			lit.WriteByte(s[i])
			i++
		}
	}
	if lit.Len() > 0 {
		t.parts = append(t.parts, part{lit: lit.String()})
	}
	return t, nil
}

// MustParse is Parse that panics on error; for tests and constants.
func MustParse(s string) *Template {
	t, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return t
}

func parseRef(raw string) (Ref, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Ref{}, fmt.Errorf("empty reference {{%s}}", raw)
	}
	if s == "." {
		return Ref{Scope: ScopeRow}, nil
	}
	var ref Ref
	var segs []string
	if strings.HasPrefix(s, ".") {
		ref.Scope = ScopeRow
		segs = strings.Split(s[1:], ".")
	} else {
		all := strings.Split(s, ".")
		ref.Scope, segs = all[0], all[1:]
		if !identRe.MatchString(ref.Scope) {
			return Ref{}, fmt.Errorf("invalid reference {{%s}}: templates only support field references like .field or panel.field", raw)
		}
	}
	for _, seg := range segs {
		if !identRe.MatchString(seg) {
			return Ref{}, fmt.Errorf("invalid reference {{%s}}: templates only support field references like .field or panel.field", raw)
		}
	}
	ref.Path = segs
	if len(ref.Path) == 0 {
		ref.Path = nil
	}
	switch {
	case ref.Scope == ScopeInput && ref.Path != nil:
		return Ref{}, fmt.Errorf("invalid reference {{%s}}: input has no fields", raw)
	case ref.Scope == ScopeCtx && len(ref.Path) != 1:
		return Ref{}, fmt.Errorf("invalid reference {{%s}}: use ctx.<name>", raw)
	}
	return ref, nil
}

// Source returns the original template text.
func (t *Template) Source() string { return t.src }

// Refs returns the template's references in order of appearance.
func (t *Template) Refs() []Ref {
	var refs []Ref
	for _, p := range t.parts {
		if p.isRef {
			refs = append(refs, p.ref)
		}
	}
	return refs
}

// Render substitutes every ref using r.
func (t *Template) Render(r Resolver, mode Mode) (string, error) {
	var b strings.Builder
	for _, p := range t.parts {
		if !p.isRef {
			b.WriteString(p.lit)
			continue
		}
		v, ok := r.Resolve(p.ref)
		if !ok || v == nil {
			if mode == Shell {
				return "", &MissingError{Ref: p.ref}
			}
			continue
		}
		s := Format(v)
		if mode == Shell {
			s = Quote(s)
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// Format turns a JSON-ish value into its textual form: strings as-is,
// scalars in JSON notation, objects and arrays as compact JSON.
func Format(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

var shellSafeRe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// Quote shell-quotes s for POSIX sh, leaving obviously safe words bare.
func Quote(s string) string {
	if shellSafeRe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Lookup walks path through nested maps and slices (numeric segments index slices).
func Lookup(v any, path []string) (any, bool) {
	for _, seg := range path {
		switch x := v.(type) {
		case map[string]any:
			next, ok := x[seg]
			if !ok {
				return nil, false
			}
			v = next
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(x) {
				return nil, false
			}
			v = x[i]
		default:
			return nil, false
		}
	}
	return v, true
}
