package rows

import (
	"reflect"
	"strings"
	"testing"
)

func TestLines(t *testing.T) {
	p, err := New("", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Parse([]byte("main\n\n  feature/x  \nlast"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		map[string]any{"line": "main"},
		map[string]any{"line": "  feature/x  "},
		map[string]any{"line": "last"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestLinesStripsCRAndBlankWhitespaceLines(t *testing.T) {
	p, _ := New("", "")
	got, _ := p.Parse([]byte("a\r\n   \r\nb\r\n"))
	want := []Row{map[string]any{"line": "a"}, map[string]any{"line": "b"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestSplit(t *testing.T) {
	p, _ := New("", "\t")
	got, _ := p.Parse([]byte("a1b2\tfix bug\tx\nc3d4\tadd"))
	want := []Row{
		map[string]any{"line": "a1b2\tfix bug\tx", "fields": []any{"a1b2", "fix bug", "x"}},
		map[string]any{"line": "c3d4\tadd", "fields": []any{"c3d4", "add"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestJQ(t *testing.T) {
	p, err := New(".clusters[]", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Parse([]byte(`{"clusters":[{"name":"a","n":1},{"name":"b","n":2}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{
		map[string]any{"name": "a", "n": float64(1)},
		map[string]any{"name": "b", "n": float64(2)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestJQMultipleDocuments(t *testing.T) {
	p, _ := New(".", "")
	got, err := p.Parse([]byte("{\"a\":1}\n{\"a\":2}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %#v", len(got), got)
	}
}

func TestJQNormalisesNumbers(t *testing.T) {
	p, _ := New(".[] | {n: length}", "")
	got, err := p.Parse([]byte(`["ab","abc"]`))
	if err != nil {
		t.Fatal(err)
	}
	want := []Row{map[string]any{"n": float64(2)}, map[string]any{"n": float64(3)}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestJQCompileError(t *testing.T) {
	if _, err := New(".foo[", ""); err == nil {
		t.Error("want compile error")
	}
}

func TestJQInvalidJSONShowsOutput(t *testing.T) {
	p, _ := New(".", "")
	_, err := p.Parse([]byte("An error occurred (AccessDenied)"))
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Errorf("want error quoting the output, got %v", err)
	}
}

func TestJQRuntimeError(t *testing.T) {
	p, _ := New(".a[]", "")
	if _, err := p.Parse([]byte(`{"a": 5}`)); err == nil {
		t.Error("want runtime error iterating a number")
	}
}

func TestJQEmptyOutputIsNoRows(t *testing.T) {
	p, _ := New(".[]", "")
	got, err := p.Parse([]byte("  \n"))
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no rows, no error", got, err)
	}
}
