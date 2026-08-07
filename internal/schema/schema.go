// schema.go
//
// Parses CREATE TABLE statements out of sqlc schema files (sql/migrations/*.sql)
// into a lightweight Table/Column model. This is a file-level, heuristic parser
// used by the editor's SQL <-> YAML sync tool: it understands sqlite and
// postgres dialects well enough to extract table/column names, primary keys,
// nullability, defaults and inline foreign keys. It is NOT a full SQL parser.
package schema

import (
	"os"
	"regexp"
	"strings"
)

// FK describes an inline REFERENCES constraint declared on a column.
type FK struct {
	Column        string // the column that holds the reference
	ForeignTable  string
	ForeignColumn string
}

// Column is a single column of a parsed table.
type Column struct {
	Name         string
	Type         string
	Nullable     bool
	Default      string
	IsPrimaryKey bool
	FKs          []FK
}

// Table is a parsed CREATE TABLE statement.
type Table struct {
	Name    string
	Columns []Column
}

// FKs returns the foreign keys declared on the table's columns.
func (t Table) FKs() []FK {
	var out []FK
	for _, c := range t.Columns {
		for _, fk := range c.FKs {
			out = append(out, FK{
				Column:        c.Name,
				ForeignTable:  fk.ForeignTable,
				ForeignColumn: fk.ForeignColumn,
			})
		}
	}
	return out
}

// stripComments removes -- line comments and /* */ block comments. It is a
// best-effort pass; string literals containing "--" are rare in schema DDL.
func stripComments(src string) string {
	var b strings.Builder
	inBlock := false
	lines := strings.Split(src, "\n")
	for _, line := range lines {
		if inBlock {
			if idx := strings.Index(line, "*/"); idx >= 0 {
				b.WriteString(line[idx+2:])
				b.WriteString("\n")
				inBlock = false
			}
			continue
		}
		if idx := strings.Index(line, "/*"); idx >= 0 {
			b.WriteString(line[:idx])
			if end := strings.Index(line[idx+2:], "*/"); end >= 0 {
				b.WriteString(line[idx+2+end+2:])
				b.WriteString("\n")
			} else {
				inBlock = true
			}
			continue
		}
		if idx := strings.Index(line, "--"); idx >= 0 {
			b.WriteString(line[:idx])
			b.WriteString("\n")
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

var createTableRe = regexp.MustCompile(`(?i)create\s+table\s+(?:if\s+not\s+exists\s+)?([A-Za-z0-9_.]+)\s*\(`)

// parseSchemaBytes parses all CREATE TABLE statements in the given SQL text.
func parseSchemaBytes(data []byte) []Table {
	text := stripComments(string(data))
	var tables []Table
	pos := 0
	for pos < len(text) {
		loc := createTableRe.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		name := text[pos+loc[2] : pos+loc[3]]
		open := pos + loc[1] - 1
		body, next, ok := readBalanced(text, open)
		if !ok {
			break
		}
		tables = append(tables, parseTableBody(cleanIdent(name), body))
		pos = next
	}
	return tables
}

// ParseSchema reads one or more SQL files and returns all tables they define.
func ParseSchema(paths ...string) ([]Table, error) {
	var data []byte
	for _, p := range paths {
		d, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		data = append(data, d...)
		data = append(data, '\n')
	}
	return ParseSchemaBytes(data), nil
}

// ParseSchemaBytes parses all CREATE TABLE statements in the given SQL text.
func ParseSchemaBytes(data []byte) []Table {
	return parseSchemaBytes(data)
}

// readBalanced returns the substring between the parenthesis at position start
// and its matching close paren, plus the index just past it.
func readBalanced(s string, start int) (string, int, bool) {
	if start >= len(s) || s[start] != '(' {
		return "", 0, false
	}
	depth := 0
	quote := rune(0)
	for i := start; i < len(s); i++ {
		r := rune(s[i])
		if quote != 0 {
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start+1 : i], i + 1, true
			}
		}
	}
	return "", 0, false
}

// splitTopLevel splits a string on commas at parenthesis depth 0, preserving
// quoted strings.
func splitTopLevel(s string) []string {
	var parts []string
	var cur strings.Builder
	depth := 0
	quote := rune(0)
	for _, r := range s {
		if quote != 0 {
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
			cur.WriteRune(r)
		case '(':
			depth++
			cur.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case ',':
			if depth == 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}

// tokenize splits a definition segment on whitespace while keeping quoted
// strings and parenthesised groups as single tokens.
func tokenize(s string) []string {
	var tokens []string
	var cur strings.Builder
	quote := rune(0)
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if quote != 0 {
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"', '`':
			quote = r
			cur.WriteRune(r)
		case '(':
			depth++
			cur.WriteRune(r)
		case ')':
			if depth > 0 {
				depth--
			}
			cur.WriteRune(r)
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				flush()
			} else {
				cur.WriteRune(r)
			}
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// attrKeywords are SQL tokens that end a column type / a DEFAULT value.
var attrKeywords = map[string]bool{
	"PRIMARY": true, "NOT": true, "NULL": true, "UNIQUE": true,
	"DEFAULT": true, "REFERENCES": true, "COLLATE": true, "CHECK": true,
	"CONSTRAINT": true, "AUTOINCREMENT": true, "GENERATED": true,
	"IDENTITY": true, "COMMENT": true, "KEY": true, "INDEX": true,
	"USING": true, "AS": true,
}

func isAttrKeyword(tok string) bool {
	return attrKeywords[strings.ToUpper(strings.TrimSpace(tok))]
}

// extractParenIdents returns the comma-separated identifiers inside a "(...)"
// token.
func extractParenIdents(s string) []string {
	open := strings.IndexByte(s, '(')
	close := strings.LastIndexByte(s, ')')
	if open < 0 || close <= open {
		return nil
	}
	var out []string
	for _, p := range splitTopLevel(s[open+1 : close]) {
		p = cleanIdent(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// cleanIdent strips quote characters from an identifier.
func cleanIdent(s string) string {
	return strings.Trim(strings.TrimSpace(s), "`\"[]")
}

// splitReference parses a REFERENCES target that may appear as "roles" (with a
// separate column group token), "roles(id)", or "roles (id)".
func splitReference(tok string) (table, col string) {
	t := cleanIdent(tok)
	if idx := strings.IndexByte(t, '('); idx >= 0 {
		table = cleanIdent(t[:idx])
		if ids := extractParenIdents(t); len(ids) > 0 {
			col = ids[0]
		}
		return table, col
	}
	return t, ""
}

// parseTableBody turns the text between the parens of CREATE TABLE into a Table.
func parseTableBody(name, body string) Table {
	t := Table{Name: name}
	for _, seg := range splitTopLevel(body) {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		toks := tokenize(seg)
		if len(toks) == 0 {
			continue
		}
		switch {
		case isConstraint(toks):
			applyConstraint(&t, toks)
		default:
			c := parseColumnSegment(seg)
			if c.Name != "" {
				t.Columns = append(t.Columns, c)
			}
		}
	}
	return t
}

// isConstraint reports whether a definition segment is a table constraint
// rather than a column definition.
func isConstraint(toks []string) bool {
	first := strings.ToUpper(toks[0])
	switch first {
	case "CONSTRAINT", "PRIMARY", "FOREIGN", "UNIQUE", "CHECK", "INDEX":
		return true
	}
	if first == "KEY" && len(toks) > 1 && strings.HasPrefix(toks[1], "(") {
		return true
	}
	return false
}

// applyConstraint updates the table for PRIMARY KEY (...) and FOREIGN KEY (...)
// REFERENCES ... table constraints.
func applyConstraint(t *Table, toks []string) {
	upper := make([]string, len(toks))
	for i, tk := range toks {
		upper[i] = strings.ToUpper(tk)
	}
	for i := 0; i < len(upper); i++ {
		switch upper[i] {
		case "PRIMARY":
			if i+1 < len(upper) && upper[i+1] == "KEY" && i+2 < len(toks) {
				for _, c := range extractParenIdents(toks[i+2]) {
					if col := findColumn(t, c); col != nil {
						col.IsPrimaryKey = true
					}
				}
			}
		case "FOREIGN":
			if i+1 < len(upper) && upper[i+1] == "KEY" && i+2 < len(toks) {
				cols := extractParenIdents(toks[i+2])
				// scan forward for REFERENCES tab (col)
				for j := i + 2; j < len(upper); j++ {
					if upper[j] == "REFERENCES" && j+1 < len(toks) {
						ft, fc := splitReference(toks[j+1])
						if fc == "" && j+2 < len(toks) {
							if ids := extractParenIdents(toks[j+2]); len(ids) > 0 {
								fc = ids[0]
							}
						}
						for _, c := range cols {
							if col := findColumn(t, c); col != nil {
								col.FKs = append(col.FKs, FK{Column: col.Name, ForeignTable: ft, ForeignColumn: fc})
							}
						}
					}
				}
			}
		}
	}
}

func findColumn(t *Table, name string) *Column {
	for i := range t.Columns {
		if strings.EqualFold(t.Columns[i].Name, name) {
			return &t.Columns[i]
		}
	}
	return nil
}

// parseColumnSegment parses a single column definition segment.
func parseColumnSegment(seg string) Column {
	c := Column{Nullable: true}
	toks := tokenize(seg)
	if len(toks) == 0 {
		return c
	}
	c.Name = cleanIdent(toks[0])
	i := 1
	var typeParts []string
	for i < len(toks) && !isAttrKeyword(toks[i]) {
		typeParts = append(typeParts, cleanIdent(toks[i]))
		i++
	}
	c.Type = strings.Join(typeParts, " ")

	for i < len(toks) {
		k := strings.ToUpper(toks[i])
		switch {
		case k == "PRIMARY" && i+1 < len(toks) && strings.ToUpper(toks[i+1]) == "KEY":
			c.IsPrimaryKey = true
			i += 2
		case k == "NOT" && i+1 < len(toks) && strings.ToUpper(toks[i+1]) == "NULL":
			c.Nullable = false
			i += 2
		case k == "NULL":
			c.Nullable = true
			i++
		case k == "DEFAULT":
			i++
			var d []string
			for i < len(toks) && !isAttrKeyword(toks[i]) {
				d = append(d, toks[i])
				i++
			}
			c.Default = strings.Join(d, " ")
		case k == "REFERENCES":
			i++
			if i < len(toks) {
				ft, fc := splitReference(toks[i])
				i++
				if fc == "" && i < len(toks) {
					if ids := extractParenIdents(toks[i]); len(ids) > 0 {
						fc = ids[0]
						i++
					}
				}
				c.FKs = append(c.FKs, FK{Column: c.Name, ForeignTable: ft, ForeignColumn: fc})
			}
		default:
			i++
		}
	}
	return c
}
