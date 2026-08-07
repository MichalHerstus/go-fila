// queries.go
//
// Parses SQLC-annotated query files (sql/queries/*.sql) into Query records and
// best-effort extracts the SELECT output columns of each query. Used by the
// editor's SQL <-> YAML sync tool to validate that YAML query references exist
// and that YAML column/field names match query projections.
package schema

import (
	"os"
	"path/filepath"
	"strings"
)

// Query is one SQLC-annotated query parsed from a queries/*.sql file.
type Query struct {
	Name       string   // SQLC function name (-- name: ...)
	Variant    string   // :one | :many | :exec | :execrows ...
	Body       string   // the SQL body without the annotation
	File       string   // base name of the containing .sql file
	SelectCols []string // best-effort SELECT output column aliases
}

// ParseQueries scans every .sql file in dir and returns a map of query name to
// Query. Files that fail to read are skipped.
func ParseQueries(dir string) map[string]Query {
	out := make(map[string]Query)
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return out
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		parseQueryFile(string(data), filepath.Base(f), out)
	}
	return out
}

// parseQueryFile extracts all annotated queries from a single file's text.
func parseQueryFile(text, file string, out map[string]Query) {
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		name, variant, ok := parseAnnotation(trimmed)
		if !ok {
			continue
		}
		q := Query{Name: name, Variant: variant, File: file}
		var body strings.Builder
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if _, _, isAnn := parseAnnotation(next); isAnn {
				break
			}
			if next == "" || strings.HasPrefix(next, "--") {
				continue
			}
			body.WriteString(next)
			body.WriteString(" ")
		}
		q.Body = strings.TrimSpace(body.String())
		q.SelectCols = SelectColumns(q.Body)
		out[name] = q
	}
}

// parseAnnotation parses a "-- name: X :variant" comment line.
func parseAnnotation(line string) (name, variant string, ok bool) {
	rest := strings.TrimSpace(line)
	if !strings.HasPrefix(rest, "--") {
		return "", "", false
	}
	rest = strings.TrimSpace(strings.TrimPrefix(rest, "--"))
	if !strings.HasPrefix(strings.ToLower(rest), "name:") {
		return "", "", false
	}
	rest = strings.TrimSpace(rest[5:])
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	name = fields[0]
	if len(fields) > 1 {
		variant = fields[1]
	}
	return name, variant, true
}

// SelectColumns extracts the output column aliases from a SELECT statement's
// projection list, stopping at the FROM clause at parenthesis depth 0.
func SelectColumns(sql string) []string {
	upper := strings.ToUpper(sql)
	sel := findKeywordAtDepth(upper, "SELECT", 0)
	if sel < 0 {
		return nil
	}
	from := findKeywordAtDepth(upper, "FROM", sel+6)
	if from < 0 {
		from = len(sql)
	}
	proj := sql[sel+6 : from]
	var cols []string
	for _, part := range splitTopLevel(proj) {
		col := projectionAlias(part)
		if col != "" && col != "*" {
			cols = append(cols, col)
		}
	}
	return cols
}

// findKeywordAtDepth finds a whole-word keyword at parenthesis depth 0, starting
// at index from. sql must be upper-cased for matching.
func findKeywordAtDepth(upper, kw string, from int) int {
	depth := 0
	quote := rune(0)
	for i := from; i < len(upper); i++ {
		r := rune(upper[i])
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
			continue
		case '(':
			depth++
			continue
		case ')':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}
		if strings.HasPrefix(upper[i:], kw) {
			before := byte(' ')
			if i > 0 {
				before = upper[i-1]
			}
			after := byte(' ')
			if i+len(kw) < len(upper) {
				after = upper[i+len(kw)]
			}
			if !isIdentByte(before) && !isIdentByte(after) {
				return i
			}
		}
	}
	return -1
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// projectionAlias returns the effective column name for one projection item,
// honoring "AS alias" and stripping table qualifiers. Function calls without an
// alias (e.g. COUNT(*)) yield "".
func projectionAlias(part string) string {
	p := strings.TrimSpace(part)
	if p == "" {
		return ""
	}
	if idx := lastIndexFold(p, " AS "); idx >= 0 {
		return cleanIdent(p[idx+4:])
	}
	fields := strings.Fields(p)
	if len(fields) == 0 {
		return ""
	}
	last := fields[len(fields)-1]
	// bare function call without alias, e.g. "COUNT(*)" or "COUNT(*)::bigint"
	if strings.Contains(last, "(") && !strings.Contains(last, ".") {
		return ""
	}
	if idx := strings.LastIndexByte(last, '.'); idx >= 0 {
		last = last[idx+1:]
	}
	return cleanIdent(last)
}

// lastIndexFold returns the last index of needle in s (case-insensitive), or -1.
func lastIndexFold(s, needle string) int {
	l := strings.ToLower(s)
	n := strings.ToLower(needle)
	return strings.LastIndex(l, n)
}
