package def

import (
	"strings"
	"testing"
)

func TestRemember(t *testing.T) {
	src := `
panels:
  - {id: region, select: true, values: [a, b]}
  - {id: profile, select: true, values: [x], remember: false}
  - {id: clusters, source: c, remember: true}
  - {id: services, source: "s {{clusters.line}}"}
`
	d, err := Parse([]byte(src), "t.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{"region": true, "profile": false, "clusters": true, "services": false} {
		if got := d.Panel(id).Remember; got != want {
			t.Errorf("%s: remember = %v, want %v", id, got, want)
		}
	}
	if !d.Remembers() {
		t.Error("Remembers() = false")
	}
	d, err = Parse([]byte("panels:\n  - {id: a, source: x}\n  - {id: r, select: true, values: [a], remember: false}"), "t.yaml")
	if err != nil || d.Remembers() {
		t.Errorf("nothing remembers: %v %v", d.Remembers(), err)
	}
}

func TestRememberErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"content", "panels:\n  - {id: a, source: x}\n  - {id: m, content: {default: {tabs: [{name: T, cmd: y}]}}, remember: true}",
			"t.yaml:3: panel m: remember only applies to list panels"},
		{"enter target", "panels:\n  - {id: a, source: x, enter: b}\n  - {id: b, source: y, remember: true}",
			"t.yaml:3: panel b: remember: b is only shown through Enter from a, so its row isn't restored"},
		{"not a bool", "panels:\n  - {id: a, source: x, remember: yes please}", "t.yaml:2: panel a: remember must be true or false"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.src), "t.yaml")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v\nwant %q", err, tt.want)
			}
		})
	}
}
