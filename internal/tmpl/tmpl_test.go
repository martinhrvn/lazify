package tmpl

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// mapResolver resolves refs from a scope → value map; "" is the current row.
type mapResolver map[string]any

func (m mapResolver) Resolve(r Ref) (any, bool) {
	root, ok := m[r.Scope]
	if !ok {
		return nil, false
	}
	return Lookup(root, r.Path)
}

func TestParseRefs(t *testing.T) {
	tests := []struct {
		in   string
		want []Ref
	}{
		{"plain text", nil},
		{"{{.}}", []Ref{{Scope: ""}}},
		{"{{.name}}", []Ref{{Scope: "", Path: []string{"name"}}}},
		{"{{ .fields.0 }}", []Ref{{Scope: "", Path: []string{"fields", "0"}}}},
		{"{{branches.line}}", []Ref{{Scope: "branches", Path: []string{"line"}}}},
		{"{{ctx.region}}", []Ref{{Scope: "ctx", Path: []string{"region"}}}},
		{"{{input}}", []Ref{{Scope: "input"}}},
		{"{{clusters}}", []Ref{{Scope: "clusters"}}},
		{"a {{.x}} b {{p.y.z}} c", []Ref{
			{Scope: "", Path: []string{"x"}},
			{Scope: "p", Path: []string{"y", "z"}},
		}},
		{`docker ps --format '\{{.Names}}'`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			tp, err := Parse(tt.in)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got := tp.Refs(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Refs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"{{",
		"{{.x",
		"{{}}",
		"{{ }}",
		"{{.x | upper}}",
		"{{a..b}}",
		"{{.x.}}",
		"{{input.x}}",
		"{{ctx}}",
		"{{if .x}}",
	} {
		t.Run(in, func(t *testing.T) {
			if _, err := Parse(in); err == nil {
				t.Errorf("Parse(%q) succeeded, want error", in)
			}
		})
	}
}

func TestRenderDisplay(t *testing.T) {
	res := mapResolver{
		"": map[string]any{
			"name":   "web",
			"count":  float64(3),
			"ratio":  1.5,
			"ok":     true,
			"nested": map[string]any{"a": "b"},
			"fields": []any{"a1b2", "fix bug"},
			"nil":    nil,
		},
		"ctx": map[string]any{"region": "eu-west-1"},
	}
	tests := []struct{ in, want string }{
		{"{{.name}}", "web"},
		{"{{.count}}/{{.ratio}}", "3/1.5"},
		{"{{.ok}}", "true"},
		{"{{.nested}}", `{"a":"b"}`},
		{"{{.fields.0}} {{.fields.1}}", "a1b2 fix bug"},
		{"[{{.missing}}]", "[]"},
		{"[{{.nil}}]", "[]"},
		{"[{{.fields.9}}]", "[]"},
		{"{{ctx.region}}", "eu-west-1"},
		{`lit \{{.name}}`, "lit {{.name}}"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := MustParse(tt.in).Render(res, Display)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderShellQuotes(t *testing.T) {
	res := mapResolver{"": map[string]any{
		"safe":  "my-branch/v1.2",
		"space": "fix bug",
		"quote": "it's",
		"evil":  "x; rm -rf ~",
		"empty": "",
		"n":     float64(42),
	}}
	tests := []struct{ in, want string }{
		{"git checkout {{.safe}}", "git checkout my-branch/v1.2"},
		{"echo {{.space}}", "echo 'fix bug'"},
		{"echo {{.quote}}", `echo 'it'\''s'`},
		{"echo {{.evil}}", "echo 'x; rm -rf ~'"},
		{"echo {{.empty}}", "echo ''"},
		{"head -n {{.n}}", "head -n 42"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := MustParse(tt.in).Render(res, Shell)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderShellMissingIsError(t *testing.T) {
	res := mapResolver{"": map[string]any{"nil": nil}}
	for _, in := range []string{"echo {{.missing}}", "echo {{.nil}}", "echo {{other.x}}"} {
		_, err := MustParse(in).Render(res, Shell)
		if err == nil {
			t.Errorf("%q: want error for missing field", in)
			continue
		}
		var me *MissingError
		if !errors.As(err, &me) {
			t.Errorf("%q: error %v is not *MissingError", in, err)
		}
	}
	_, err := MustParse("echo {{other.x}}").Render(res, Shell)
	if !strings.Contains(err.Error(), "other.x") {
		t.Errorf("error %q should name the reference", err)
	}
}

func TestRefString(t *testing.T) {
	tests := []struct {
		ref  Ref
		want string
	}{
		{Ref{}, "."},
		{Ref{Path: []string{"a", "0"}}, ".a.0"},
		{Ref{Scope: "p", Path: []string{"x"}}, "p.x"},
		{Ref{Scope: "input"}, "input"},
	}
	for _, tt := range tests {
		if got := tt.ref.String(); got != tt.want {
			t.Errorf("%#v.String() = %q, want %q", tt.ref, got, tt.want)
		}
	}
}
