package def

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func appIDs(apps []App) []string {
	var out []string
	for _, a := range apps {
		out = append(out, a.ID)
	}
	return out
}

func TestSingleAppFileID(t *testing.T) {
	apps, err := ParseFile([]byte("panels: [{id: a, source: x}]"), "/cfg/ecs.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].ID != "ecs" || apps[0].Name != "ecs" || apps[0].Err != nil {
		t.Fatalf("apps = %+v", apps)
	}
	if apps[0].Def.ID != "ecs" || apps[0].Def.Name != "ecs" {
		t.Errorf("def id/name = %q/%q", apps[0].Def.ID, apps[0].Def.Name)
	}

	apps, _ = ParseFile([]byte("id: aws-ecs\nname: AWS ECS\npanels: [{id: a, source: x}]"), "/cfg/whatever.yaml")
	if apps[0].ID != "aws-ecs" || apps[0].Name != "AWS ECS" {
		t.Errorf("explicit id/name = %+v", apps[0])
	}
}

const multiApps = `apps:
  - id: procs
    panels: [{id: p, source: ps}]
  - id: logs
    name: Log tail
    panels:
      - id: files
        sauce: nope
      - id: other
`

func TestAppsList(t *testing.T) {
	apps, err := ParseFile([]byte(multiApps), "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(appIDs(apps), []string{"procs", "logs"}) {
		t.Fatalf("ids = %v", appIDs(apps))
	}
	if apps[0].Err != nil || apps[0].Def == nil || apps[0].Line != 2 {
		t.Errorf("procs = %+v", apps[0])
	}
	if apps[1].Name != "Log tail" || apps[1].Line != 4 {
		t.Errorf("logs = %+v", apps[1])
	}
	// Errors inside apps[1] keep file line numbers, and don't affect apps[0].
	msg := apps[1].Err.Error()
	for _, want := range []string{
		`config.yaml:8: unknown field "sauce" in panel`,
		"config.yaml:7: panel files: source is required",
		"config.yaml:9: panel other: source is required",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("logs error %q\ndoes not contain %q", msg, want)
		}
	}
}

func TestAppsListErrors(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"mixed", "name: x\napps:\n  - {id: a, panels: [{id: p, source: x}]}", "config.yaml:2: apps: use either apps: or a single app's fields at the top level, not both"},
		{"missing id", "apps:\n  - panels: [{id: p, source: x}]", "config.yaml:2: app: id is required"},
		{"bad id", "apps:\n  - {id: 'my app', panels: [{id: p, source: x}]}", "config.yaml:2: app my app: invalid id"},
		{"syntax", "apps: [", "config.yaml:1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apps, err := ParseFile([]byte(tt.src), "config.yaml")
			var msgs []string
			if err != nil {
				msgs = append(msgs, err.Error())
			}
			for _, a := range apps {
				if a.Err != nil {
					msgs = append(msgs, a.Err.Error())
				}
			}
			if got := strings.Join(msgs, "\n"); !strings.Contains(got, tt.want) {
				t.Errorf("errors %q\ndo not contain %q", got, tt.want)
			}
		})
	}
}

func TestParseRejectsMultiAppFile(t *testing.T) {
	_, err := Parse([]byte("apps:\n  - {id: a, panels: [{id: p, source: x}]}\n  - {id: b, panels: [{id: p, source: x}]}"), "config.yaml")
	if err == nil || !strings.Contains(err.Error(), "2 apps (a, b)") {
		t.Errorf("err = %v", err)
	}
}

func TestUnknownTopLevelFieldIsFriendly(t *testing.T) {
	_, err := Parse([]byte("pannels: []\npanels: [{id: a, source: x}]"), "t.yaml")
	if err == nil || !strings.Contains(err.Error(), `t.yaml:1: unknown field "pannels" in definition`) {
		t.Errorf("err = %v", err)
	}
}

func TestConfigExample(t *testing.T) {
	data, err := os.ReadFile("../../examples/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	apps, err := ParseFile(data, "config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range apps {
		if a.Err != nil {
			t.Errorf("%s: %v", a.ID, a.Err)
		}
	}
	if got := appIDs(apps); !reflect.DeepEqual(got, []string{"procs", "logs"}) {
		t.Errorf("ids = %v", got)
	}
}
