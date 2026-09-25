package engine

import (
	"reflect"
	"testing"
)

const filterDef = `
panels:
  - {id: branches, source: git branch, key: .line}
  - {id: log, source: "git log {{branches.line}}"}
  - {id: region, select: popup, values: [eu-west-1, eu-central-1, us-east-1]}
`

func loadedFilter(t *testing.T) *harness {
	h := newHarness(t, filterDef)
	h.finish("branches", "main\nfeature/login\nfeature/logout\nfix-prod-web\n")
	h.finish("log", "a1\n")
	h.focus("branches")
	return h
}

func (h *harness) filter(text string) { h.apply(h.e.SetFilter(text)) }

func TestFilterNarrowsRows(t *testing.T) {
	h := loadedFilter(t)
	for filter, want := range map[string][]string{
		"feat":     {"feature/login", "feature/logout"},
		"FEAT":     {"feature/login", "feature/logout"},
		"prod web": {"fix-prod-web"}, // every term must appear
		"web prod": {"fix-prod-web"},
		"":         {"main", "feature/login", "feature/logout", "fix-prod-web"},
	} {
		h.filter(filter)
		if got := h.e.View("branches").Lines; !reflect.DeepEqual(got, want) {
			t.Errorf("filter %q: %q, want %q", filter, got, want)
		}
	}
}

func TestFilterMovesAHiddenCursorAndPropagates(t *testing.T) {
	h := loadedFilter(t) // cursor on main
	h.filter("login")
	v := h.e.View("branches")
	if v.Cursor != 0 || v.Lines[0] != "feature/login" || h.e.Filter("branches") != "login" {
		t.Errorf("view = %+v", v)
	}
	if h.settle == 0 {
		t.Fatal("a selection change by filtering should settle like a move")
	}
	h.doSettle()
	if got := h.cmd("log"); got != "git log feature/login" {
		t.Errorf("log = %q", got)
	}
}

func TestFilterKeepsAVisibleSelection(t *testing.T) {
	h := loadedFilter(t)
	h.move("branches", 2) // feature/logout
	h.doSettle()
	h.finish("log", "b2\n")
	n := len(h.ran)
	h.filter("feat")
	h.doSettle()
	if v := h.e.View("branches"); v.Cursor != 1 || len(h.ran) != n {
		t.Errorf("cursor %d, ran %v", v.Cursor, h.ran[n:])
	}
}

func TestFilterWithNoMatches(t *testing.T) {
	h := loadedFilter(t)
	h.filter("zzz")
	if v := h.e.View("branches"); len(v.Lines) != 0 {
		t.Errorf("lines = %q", v.Lines)
	}
	h.doSettle()
	if v := h.e.View("log"); v.Blocked != "no selection in branches" {
		t.Errorf("no match should mean no selection: %+v", v)
	}
}

func TestMoveSkipsHiddenRows(t *testing.T) {
	h := loadedFilter(t)
	h.filter("feat")
	h.apply(h.e.Move(1))
	h.apply(h.e.Move(1)) // clamps at the last visible row
	if sel, _ := h.e.Selection("branches"); sel.(map[string]any)["line"] != "feature/logout" {
		t.Errorf("selection = %v", sel)
	}
}

func TestClearingTheFilterKeepsTheSelection(t *testing.T) {
	h := loadedFilter(t)
	h.filter("feat")
	h.apply(h.e.Move(1)) // feature/logout
	h.filter("")
	if v := h.e.View("branches"); len(v.Lines) != 4 || v.Cursor != 2 {
		t.Errorf("view = %+v", v)
	}
}

func TestFilterReappliedAfterRefresh(t *testing.T) {
	h := loadedFilter(t)
	h.filter("feat")
	h.doSettle()
	h.finish("log", "x\n")
	h.apply(h.e.Refresh())
	h.finish("branches", "main\nfeature/login\nfeature/new\n")
	if got := h.e.View("branches").Lines; !reflect.DeepEqual(got, []string{"feature/login", "feature/new"}) {
		t.Errorf("lines = %q", got)
	}
}

func TestEscClearsTheFilterFirst(t *testing.T) {
	h := loadedFilter(t)
	h.filter("feat")
	if !h.back() || h.e.Filter("branches") != "" {
		t.Error("esc should clear the filter")
	}
	if h.back() {
		t.Error("nothing left for esc")
	}
}

func TestFilterPickerChoices(t *testing.T) {
	h := loadedFilter(t)
	h.apply(h.e.Pick("region"))
	h.filter("central")
	if v := h.e.PickerView("region"); !reflect.DeepEqual(v.Lines, []string{"eu-central-1"}) || v.Cursor != 0 {
		t.Errorf("picker = %+v", v)
	}
	h.enter()
	if got := h.e.View("region").Lines; !reflect.DeepEqual(got, []string{"eu-central-1"}) {
		t.Errorf("chosen = %q", got)
	}
	if h.e.Filter("region") != "" || h.e.Focused() != "branches" {
		t.Errorf("closing the picker clears its filter; focused %q", h.e.Focused())
	}
}
