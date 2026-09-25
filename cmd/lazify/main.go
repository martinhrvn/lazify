// Command lazify turns a YAML definition into a lazygit-style TUI.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/catalog"
	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/ui"
)

const usage = `usage:
  lazify <id> [--set select=value]...    run the app with that id from %[1]s
  lazify <file.yaml> [id] [--set ...]    run an app from a file (id picks one of several)
  lazify list                            list the apps in %[1]s
  lazify lint [id|file.yaml]...          validate apps (default: everything in %[1]s)

Apps live in any *.yaml / *.yml there: one app per file (id defaults to the
file name) or several under "apps:", each with an id.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, configDir()))
}

func configDir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazify")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "lazify")
}

func run(argv []string, stdout, stderr io.Writer, cfgDir string) int {
	a, err := parseArgs(argv)
	if err != nil || len(a.positional) == 0 {
		if err != nil {
			fmt.Fprintln(stderr, "lazify:", err)
		}
		fmt.Fprintf(stderr, usage, cfgDir)
		return 2
	}

	switch a.positional[0] {
	case "lint":
		return lint(a.positional[1:], stdout, stderr, cfgDir)
	case "list":
		return list(stdout, cfgDir)
	}

	d, err := pick(a.positional, cfgDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := checkSet(d, a.set); err != nil {
		fmt.Fprintln(stderr, "lazify:", err)
		return 2
	}
	p := tea.NewProgram(ui.New(d, runner.Shell{}, a.set), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, "lazify:", err)
		return 1
	}
	return 0
}

// isFile reports whether arg names a definition file rather than an app id.
func isFile(arg string) bool {
	ext := filepath.Ext(arg)
	if !strings.ContainsRune(arg, filepath.Separator) && ext != ".yaml" && ext != ".yml" {
		return false
	}
	st, err := os.Stat(arg)
	return err == nil && !st.IsDir()
}

// pick finds the app to run: `<id>` from the catalog, or `<file> [id]`.
func pick(args []string, cfgDir string) (*def.Definition, error) {
	if !isFile(args[0]) {
		if len(args) > 1 {
			return nil, fmt.Errorf("lazify: unexpected argument %q", args[1])
		}
		return catalog.Scan(cfgDir).Find(args[0])
	}
	file := args[0]
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	apps, err := def.ParseFile(data, file)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, app := range apps {
		ids = append(ids, app.ID)
		if len(args) > 1 && app.ID == args[1] {
			return app.Def, app.Err
		}
	}
	switch {
	case len(args) > 1:
		return nil, fmt.Errorf("no app %q in %s (apps: %s)", args[1], file, strings.Join(ids, ", "))
	case len(apps) == 1:
		return apps[0].Def, apps[0].Err
	}
	return nil, fmt.Errorf("%s defines %d apps (%s); run `lazify %s <id>`", file, len(apps), strings.Join(ids, ", "), file)
}

// lint validates the given apps or files, or everything in the config dir.
func lint(targets []string, stdout, stderr io.Writer, cfgDir string) int {
	code := 0
	report := func(what string, err error) {
		if err != nil {
			fmt.Fprintln(stderr, err)
			code = 1
			return
		}
		fmt.Fprintf(stdout, "%s: ok\n", what)
	}
	if len(targets) == 0 {
		c := catalog.Scan(cfgDir)
		for _, e := range c.Entries() {
			if e.Err == nil && !c.Duplicate(e.ID) {
				report(e.ID, nil)
			}
		}
		for _, p := range c.Problems() {
			report("", p)
		}
		return code
	}
	for _, t := range targets {
		if !isFile(t) {
			_, err := catalog.Scan(cfgDir).Find(t)
			report(t, err)
			continue
		}
		data, err := os.ReadFile(t)
		if err != nil {
			report(t, err)
			continue
		}
		apps, err := def.ParseFile(data, t)
		if err != nil {
			report(t, err)
		}
		for _, app := range apps {
			report(t+": "+app.ID, app.Err)
		}
	}
	return code
}

// list prints the catalog as a table.
func list(stdout io.Writer, cfgDir string) int {
	c := catalog.Scan(cfgDir)
	if len(c.Entries()) == 0 {
		return 0
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tFILE\t")
	for _, e := range c.Entries() {
		status := ""
		switch {
		case e.Err != nil:
			status = "invalid (lazify lint)"
		case c.Duplicate(e.ID):
			status = "duplicate id"
		}
		loc := e.Location()
		if rel, err := filepath.Rel(cfgDir, e.File); err == nil {
			loc = fmt.Sprintf("%s:%d", rel, e.Line)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", e.ID, e.Name, loc, status)
	}
	if err := tw.Flush(); err != nil {
		return 1
	}
	return 0
}

type args struct {
	positional []string
	set        map[string]string
}

func parseArgs(argv []string) (args, error) {
	a := args{set: map[string]string{}}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		var kv string
		switch {
		case arg == "--set":
			if i+1 >= len(argv) {
				return a, errors.New("--set needs ctx=value")
			}
			i++
			kv = argv[i]
		case strings.HasPrefix(arg, "--set="):
			kv = strings.TrimPrefix(arg, "--set=")
		case strings.HasPrefix(arg, "-"):
			return a, fmt.Errorf("unknown flag %s", arg)
		default:
			a.positional = append(a.positional, arg)
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return a, fmt.Errorf("--set %q: want ctx=value", kv)
		}
		a.set[k] = v
	}
	return a, nil
}

// checkSet validates --set: each names a select panel, and for a select with
// written-out values, one of them (a command's choices are only known later).
func checkSet(d *def.Definition, set map[string]string) error {
	for id, val := range set {
		p := d.Panel(id)
		switch {
		case p == nil || !p.Select:
			return fmt.Errorf("--set %s: no select panel %q", id, id)
		case p.Values != nil && !slices.Contains(p.Values, val):
			return fmt.Errorf("--set %s: %q is not one of %s", id, val, strings.Join(p.Values, ", "))
		}
	}
	return nil
}
