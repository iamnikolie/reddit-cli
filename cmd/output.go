package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/iamnikolie/reddit-cli/internal/archive"
	"github.com/iamnikolie/reddit-cli/internal/render"
)

func format() string {
	if jsonOutput {
		return "json"
	}
	if outputFormat == "" {
		return "md"
	}
	return outputFormat
}

// getPath resolves a dotted path ("a.b") inside decoded JSON.
func getPath(v any, path string) (any, bool) {
	if m, ok := v.(map[string]any); ok {
		if x, ok := m[path]; ok {
			return x, true
		}
	}
	cur := v
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func project(row map[string]any, fields []string) map[string]any {
	out := make(map[string]any, len(fields))
	for _, f := range fields {
		if v, ok := getPath(row, f); ok {
			out[f] = v
		}
	}
	return out
}

func projectAll(rows []map[string]any, cols []string) []map[string]any {
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = project(r, cols)
	}
	return out
}

func activeFields(defaults []string) []string {
	if len(fieldsFlag) > 0 {
		return fieldsFlag
	}
	return defaults
}

// writeJSON encodes v as compact UTF-8 without HTML escaping.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

func writeRaw(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return writeJSON(stdout, v)
}

// emitRows prints rows in the active non-md format (json/table/csv/tsv).
// JSON keeps every field unless --fields projects it.
func emitRows(rows []archive.Row, defaults []string) error {
	cols := activeFields(defaults)
	switch format() {
	case "json":
		if len(fieldsFlag) == 0 {
			if rows == nil {
				rows = []archive.Row{}
			}
			return writeJSON(stdout, rows)
		}
		return writeJSON(stdout, projectAll(rows, fieldsFlag))
	case "csv":
		render.Separated(stdout, projectAll(rows, cols), cols, ",")
	case "tsv":
		render.Separated(stdout, projectAll(rows, cols), cols, "\t")
	default:
		render.Table(stdout, projectAll(rows, cols), cols)
	}
	return nil
}

// truncate collapses whitespace and cuts s to n runes (0 = unlimited).
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// readArg returns the literal text, or reads stdin when it is "-".
func readArg(s string) (string, error) {
	if s != "-" {
		return s, nil
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// partialOK downgrades an archive.Partial (some subreddits failed, rows
// arrived from the rest) to a stderr note and returns any other error as is.
func partialOK(err error) error {
	var p *archive.Partial
	if errors.As(err, &p) {
		note("(warning: %s)", p.Error())
		return nil
	}
	return err
}

// note prints an advisory line on stderr (never in JSON mode).
func note(format string, args ...any) {
	if jsonOutput || outputFormat == "json" {
		return
	}
	fmt.Fprintf(stderr, format+"\n", args...)
}
