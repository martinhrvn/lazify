package def

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/martinhrvn/lazify/internal/tmpl"
)

// Options are knobs the user sets while the app runs (a date, an author, a
// log window), edited in a form (o) and read by commands as {{opt.id}}. They
// belong to where the command lives: a list panel (its source and actions) or
// one content entry (its tabs). Other panels read a list panel's options as
// {{panel.opt.id}}.

// DateLayout is how date options are written and passed to commands.
const DateLayout = "2006-01-02"

// Option is one option.
type Option struct {
	ID      string
	Title   string
	Type    string         // text | choice | toggle | date
	Default string         // toggle: "true"/"false"; date: as written (may be relative)
	Values  []string       // choice: the choices
	Source  *tmpl.Template // choice: a command listing the choices, one per line
	Flag    string         // rendered as flag+value when set, nothing when empty
}

type rawOption struct {
	ID      string   `yaml:"id"`
	Title   string   `yaml:"title"`
	Type    string   `yaml:"type"`
	Default string   `yaml:"default"`
	Values  []string `yaml:"values"`
	Source  string   `yaml:"source"`
	Flag    string   `yaml:"flag"`
}

var relDateRe = regexp.MustCompile(`^([+-])(\d+)([dw])$`)

// ParseDate reads a date option: YYYY-MM-DD, today, yesterday or relative to
// now (-3d, -2w, +1d).
func ParseDate(s string, now time.Time) (time.Time, bool) {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch s {
	case "today":
		return day, true
	case "yesterday":
		return day.AddDate(0, 0, -1), true
	}
	if m := relDateRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[2])
		if m[3] == "w" {
			n *= 7
		}
		if m[1] == "-" {
			n = -n
		}
		return day.AddDate(0, 0, n), true
	}
	t, err := time.ParseInLocation(DateLayout, s, now.Location())
	return t, err == nil
}

// options parses the options at path (a list panel's, or a content entry's).
func (v *validator) options(raw []rawOption, what string, d *Definition, path ...any) []*Option {
	var out []*Option
	for i, ro := range raw {
		line := v.line(append(slices.Clone(path), i)...)
		w := fmt.Sprintf("%s: option %q", what, ro.ID)
		if !idRe.MatchString(ro.ID) {
			v.errorf(line, "%s: option: id is required (letters, digits, _ and -)", what)
			continue
		}
		if slices.ContainsFunc(out, func(o *Option) bool { return o.ID == ro.ID }) {
			v.errorf(line, "%s: duplicate id", w)
			continue
		}
		o := &Option{ID: ro.ID, Title: or(ro.Title, ro.ID), Type: ro.Type, Default: ro.Default, Values: ro.Values, Flag: ro.Flag}
		if o.Type == "" {
			o.Type = "text"
			if len(ro.Values) > 0 || ro.Source != "" {
				o.Type = "choice"
			}
		}
		switch o.Type {
		case "text":
		case "choice":
			switch {
			case len(ro.Values) > 0 && ro.Source != "":
				v.errorf(line, "%s: use either values or source", w)
			case len(ro.Values) > 0 && o.Default == "":
				o.Default = ro.Values[0]
			case len(ro.Values) > 0 && !slices.Contains(ro.Values, o.Default):
				v.errorf(line, "%s: default %q is not one of the values", w, o.Default)
			case ro.Source != "":
				o.Source, _ = v.template(line, w+": source", ro.Source, d, refRules{panels: true})
			case len(ro.Values) == 0:
				v.errorf(line, "%s: a choice needs values or a source", w)
			}
		case "toggle":
			if o.Flag == "" {
				v.errorf(line, "%s: a toggle needs a flag (what it adds to the command when on)", w)
			}
			switch o.Default {
			case "":
				o.Default = "false"
			case "true", "false":
			default:
				v.errorf(line, "%s: a toggle's default is true or false", w)
			}
		case "date":
			if o.Default == "" {
				if o.Flag == "" {
					v.errorf(line, "%s: a date without a default needs a flag (unset, it adds nothing)", w)
				}
			} else if _, ok := ParseDate(o.Default, time.Now()); !ok {
				v.errorf(line, "%s: default %q is not a date (YYYY-MM-DD, today, yesterday, -3d, -2w)", w, o.Default)
			}
		default:
			v.errorf(line, "%s: type must be text, choice, toggle or date, got %q", w, o.Type)
			continue
		}
		if o.Type != "choice" && (len(ro.Values) > 0 || ro.Source != "") {
			v.errorf(line, "%s: values and source are for choices, not %s", w, o.Type)
		}
		out = append(out, o)
	}
	return out
}

// findOption returns option id of opts, if any.
func findOption(opts []*Option, id string) *Option {
	for _, o := range opts {
		if o.ID == id {
			return o
		}
	}
	return nil
}

func optionIDs(opts []*Option) string {
	var ids []string
	for _, o := range opts {
		ids = append(ids, o.ID)
	}
	return strings.Join(ids, ", ")
}
