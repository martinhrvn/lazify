// Command lazify turns a YAML definition into a lazygit-style TUI.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/martinhrvn/lazify/internal/def"
	"github.com/martinhrvn/lazify/internal/runner"
	"github.com/martinhrvn/lazify/internal/ui"
)

const usage = `usage:
  lazify <file.yaml|name> [--set ctx=value]...   run a definition
  lazify lint <file.yaml|name>...                 validate definitions
  lazify list                                     list definitions in %s
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
		return list(stdout, stderr, cfgDir)
	}

	path, err := resolve(a.positional[0], cfgDir)
	if err != nil {
		fmt.Fprintln(stderr, "lazify:", err)
		return 2
	}
	d, err := def.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for k := range a.set {
		if _, ok := d.Context[k]; !ok {
			fmt.Fprintf(stderr, "lazify: --set: unknown context %q\n", k)
			return 2
		}
	}
	p := tea.NewProgram(ui.New(d, runner.Shell{}, a.set), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, "lazify:", err)
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

// resolve maps a file path or a definition name to a file.
func resolve(nameOrPath, cfgDir string) (string, error) {
	if _, err := os.Stat(nameOrPath); err == nil {
		return nameOrPath, nil
	}
	if !strings.ContainsRune(nameOrPath, filepath.Separator) {
		for _, ext := range []string{".yaml", ".yml"} {
			p := filepath.Join(cfgDir, nameOrPath+ext)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("no definition %q (not a file, not in %s)", nameOrPath, cfgDir)
}

func lint(names []string, stdout, stderr io.Writer, cfgDir string) int {
	if len(names) == 0 {
		fmt.Fprintln(stderr, "lazify lint: no definitions given")
		return 2
	}
	code := 0
	for _, n := range names {
		path, err := resolve(n, cfgDir)
		if err == nil {
			_, err = def.Load(path)
		}
		if err != nil {
			fmt.Fprintln(stderr, err)
			code = 1
			continue
		}
		fmt.Fprintf(stdout, "%s: ok\n", path)
	}
	return code
}

func list(stdout, stderr io.Writer, cfgDir string) int {
	entries, err := os.ReadDir(cfgDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "lazify:", err)
		return 1
	}
	var names []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.IsDir() && (ext == ".yaml" || ext == ".yml") {
			names = append(names, strings.TrimSuffix(e.Name(), ext))
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintln(stdout, n)
	}
	return 0
}
