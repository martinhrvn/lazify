package def

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LoadFile reads a definition file and replaces its `import:` entries with the
// apps of the files they name (relative to the importing file; `~/` is the home
// directory). The returned error is for the file itself; a bad import becomes
// an app carrying the error, so it does not hide the other apps.
func LoadFile(path string) ([]App, error) {
	return load(path, nil)
}

// load reads path; stack is the chain of files importing it, for cycles.
func load(path string, stack []string) ([]App, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	apps, err := ParseFile(data, path)
	abs, _ := filepath.Abs(path)
	stack = append(slices.Clip(stack), abs)
	var out []App
	for _, a := range apps {
		if a.Import == "" {
			out = append(out, a)
			continue
		}
		out = append(out, loadImport(a, stack)...)
	}
	return out, err
}

func loadImport(entry App, stack []string) []App {
	fail := func(msg string) []App {
		return []App{{ID: "import " + entry.Import, File: entry.File, Line: entry.Line,
			Err: Errors{{File: entry.File, Line: entry.Line, Msg: "apps: import " + entry.Import + ": " + msg}}}}
	}
	target, err := importPath(entry.File, entry.Import)
	if err != nil {
		return fail(err.Error())
	}
	if i := slices.Index(stack, target); i >= 0 {
		var chain []string
		for _, p := range append(stack[i:], target) {
			chain = append(chain, filepath.Base(p))
		}
		if i > 0 { // show where the chain started
			chain = append([]string{filepath.Base(stack[0])}, chain...)
		}
		return fail("import cycle: " + strings.Join(chain, " → "))
	}
	apps, err := load(target, stack)
	var es Errors
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fail("no such file")
	case errors.As(err, &es): // the imported file's own errors, at their lines
		return append(apps, App{ID: "import " + entry.Import, File: entry.File, Line: entry.Line, Err: es})
	case err != nil:
		return fail(err.Error())
	}
	return apps
}

// importPath resolves an import against the importing file, as an absolute path.
func importPath(from, path string) (string, error) {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("no home directory: %w", err)
		}
		path = filepath.Join(home, rest)
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(from), path)
	}
	return filepath.Abs(path)
}
