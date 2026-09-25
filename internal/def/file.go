package def

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// App is one app found in a file: either the whole file, or an entry of its
// `apps:` list.
type App struct {
	ID   string
	Name string
	Line int         // where the app starts in the file
	Def  *Definition // nil when Err is set
	Err  error       // this app's validation errors (Errors)
}

// rawFile is a file with one app at the top level, or several under apps.
type rawFile struct {
	rawDef `yaml:",inline"`
	Apps   []rawDef `yaml:"apps"`
}

// ParseFile parses every app in a file. The returned error is for problems
// with the file as a whole (bad YAML, mixed forms); each app carries its own
// validation errors, so one broken app does not hide the others.
func ParseFile(data []byte, file string) ([]App, error) {
	var raw rawFile
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
	var doc yaml.Node
	_ = yaml.Unmarshal(data, &doc)
	root := &doc
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	}

	appsNode := mapValue(root, "apps")
	if appsNode == nil {
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		app := buildApp(file, root, &raw.rawDef, typeErrs, stem)
		return []App{app}, nil
	}
	if keys := mapKeysOf(root); len(keys) > 1 {
		return nil, Errors{{File: file, Line: keyLine(root, "apps"),
			Msg: "apps: use either apps: or a single app's fields at the top level, not both"}}
	}

	var apps []App
	var fileErrs Errors
	for i, rd := range raw.Apps {
		node := appsNode.Content[i]
		end := -1
		if i+1 < len(appsNode.Content) {
			end = appsNode.Content[i+1].Line
		}
		// Type errors belong to the app whose lines they fall in.
		var own Errors
		for _, e := range typeErrs {
			if e.Line >= node.Line && (end < 0 || e.Line < end) {
				own = append(own, e)
			}
		}
		apps = append(apps, buildApp(file, node, &rd, own, ""))
	}
	for _, e := range typeErrs {
		if len(appsNode.Content) == 0 || e.Line < appsNode.Content[0].Line {
			fileErrs = append(fileErrs, e)
		}
	}
	if len(fileErrs) > 0 {
		return apps, fileErrs
	}
	return apps, nil
}

// buildApp validates one app rooted at node. defaultID is used when the app
// has no id (single-app files); "" makes the id required.
func buildApp(file string, node *yaml.Node, raw *rawDef, typeErrs Errors, defaultID string) App {
	v := &validator{file: file, root: node, errs: typeErrs}
	app := App{ID: raw.ID, Line: node.Line}
	switch {
	case app.ID == "" && defaultID == "":
		v.errorf(node.Line, "app: id is required")
	case app.ID == "":
		app.ID = defaultID
	case !idRe.MatchString(app.ID):
		v.errorf(v.line("id"), "app %s: invalid id (use letters, digits, _ and -)", app.ID)
	}
	d := v.build(raw)
	d.ID, d.Name = app.ID, raw.Name
	if d.Name == "" {
		d.Name = app.ID
	}
	app.Name = d.Name
	if len(v.errs) > 0 {
		app.Err = v.errs
	} else {
		app.Def = d
	}
	return app
}

// Parse validates a file holding exactly one app. file is used in error messages.
func Parse(data []byte, file string) (*Definition, error) {
	apps, err := ParseFile(data, file)
	if err != nil {
		return nil, err
	}
	if len(apps) != 1 {
		ids := make([]string, len(apps))
		for i, a := range apps {
			ids[i] = a.ID
		}
		return nil, fmt.Errorf("%s defines %d apps (%s); pick one by id", file, len(apps), strings.Join(ids, ", "))
	}
	return apps[0].Def, apps[0].Err
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func mapKeysOf(n *yaml.Node) []string {
	var keys []string
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

func keyLine(n *yaml.Node, key string) int {
	for i := 0; n.Kind == yaml.MappingNode && i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i].Line
		}
	}
	return n.Line
}
