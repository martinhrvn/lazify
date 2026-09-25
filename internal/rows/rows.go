// Package rows turns command output into rows: either JSON values produced by
// a jq expression, or one {line, fields} object per non-blank output line.
package rows

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"

	"github.com/itchyny/gojq"
)

// Row is one list item: a JSON value (usually map[string]any).
type Row = any

// Parser converts command output into rows.
type Parser struct {
	code  *gojq.Code
	split string
}

// New builds a parser. A non-empty rowsExpr selects jq mode; otherwise output
// is read line by line and, if split is set, each line also gets `fields`.
func New(rowsExpr, split string) (*Parser, error) {
	p := &Parser{split: split}
	if rowsExpr == "" {
		return p, nil
	}
	q, err := gojq.Parse(rowsExpr)
	if err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	p.code, err = gojq.Compile(q)
	if err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return p, nil
}

// Parse converts output into rows.
func (p *Parser) Parse(out []byte) ([]Row, error) {
	if p.code == nil {
		return p.lines(out), nil
	}
	return p.jq(out)
}

func (p *Parser) lines(out []byte) []Row {
	var rows []Row
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		row := map[string]any{"line": line}
		if p.split != "" {
			parts := strings.Split(line, p.split)
			fields := make([]any, len(parts))
			for i, f := range parts {
				fields[i] = f
			}
			row["fields"] = fields
		}
		rows = append(rows, row)
	}
	return rows
}

func (p *Parser) jq(out []byte) ([]Row, error) {
	dec := json.NewDecoder(bytes.NewReader(out))
	var rows []Row
	for {
		var doc any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				return rows, nil
			}
			return nil, fmt.Errorf("invalid JSON output: %w\n%s", err, head(out, 5))
		}
		iter := p.code.Run(doc)
		for {
			v, ok := iter.Next()
			if !ok {
				break
			}
			if err, isErr := v.(error); isErr {
				var halt *gojq.HaltError
				if errors.As(err, &halt) && halt.Value() == nil {
					break
				}
				return nil, fmt.Errorf("rows: %w", err)
			}
			rows = append(rows, normalise(v))
		}
	}
}

// normalise converts gojq's int/*big.Int results to float64 so rows look the
// same as values decoded straight from JSON.
func normalise(v any) any {
	switch x := v.(type) {
	case int:
		return float64(x)
	case *big.Int:
		f, _ := new(big.Float).SetInt(x).Float64()
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = normalise(e)
		}
	case []any:
		for i, e := range x {
			x[i] = normalise(e)
		}
	}
	return v
}

func head(out []byte, n int) string {
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", n+1)
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return strings.Join(lines, "\n")
}
