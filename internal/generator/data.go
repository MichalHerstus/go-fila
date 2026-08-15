// data.go
//
// Emits the generated app's internal/data package (the SQLC replacement under
// D11). Instead of sqlc structs, Get{Resource} returns a map[string]interface{}
// keyed by the raw selected column name, so the detail/update handlers keep
// the existing map-based rendering pipeline (viewmodels.Stringify, row maps).
// The query is derived from the captured `schema:` block: every column of the
// table plus one LEFT JOIN + {fk}_label per foreign key, keyed on the table's
// pk. When a resource's table is not present in the schema block (hand-written
// config without schema), a degraded Get is emitted from the resource's own
// detail/update fields so legacy configs still build.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MichalHerstus/yaga/internal/types"
)

// generateData writes internal/data/data.go: a Querier holding *sql.DB plus
// one Get{query} method per resource that needs a populated read (detail view
// or update-form populate). Returns an error on write failure.
func (g *Generator) generateData() error {
	dir := filepath.Join(g.OutDir, "internal", "data")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	seen := map[string]bool{}
	var methods []string

	for _, r := range g.Config.Resources {
		if r.Detail != nil {
			name := r.Detail.Query
			if name == "" {
				name = "GetByID"
			}
			key := name + "\x00" + tableName(r)
			if !seen[key] {
				seen[key] = true
				methods = append(methods, g.getMethod(name, r))
			}
		}
		if r.Form != nil && r.Form.Update != nil {
			name := r.Form.Update.PopulateQuery
			if name == "" {
				name = "GetByID"
			}
			key := name + "\x00" + tableName(r)
			if !seen[key] {
				seen[key] = true
				methods = append(methods, g.getMethod(name, r))
			}
		}
	}

	if len(methods) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString(`package data

import (
    "context"
    "database/sql"
)

type Querier struct {
    db *sql.DB
}

func New(db *sql.DB) *Querier {
    return &Querier{db: db}
}

`)
	for _, m := range methods {
		b.WriteString(m)
		b.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(dir, "data.go"), []byte(b.String()), 0644)
}

// getMethod emits one Get method whose SQL derives from the schema block (or
// the resource's own fields as a fallback) and scans the first row into a map
// keyed by the selected column names.
func (g *Generator) getMethod(name string, r types.Resource) string {
	sel, from, whereCol := g.getQuerySQL(r)
	idType := g.idGoTypeForResource(r)
	return fmt.Sprintf(`func (q *Querier) %s(ctx context.Context, id %s) (map[string]interface{}, error) {
    rows, err := q.db.QueryContext(ctx, "SELECT %s FROM %s WHERE %s = %s", id)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    cols, err := rows.Columns()
    if err != nil {
        return nil, err
    }
    if !rows.Next() {
        return nil, sql.ErrNoRows
    }
    vals := make([]interface{}, len(cols))
    ptrs := make([]interface{}, len(cols))
    for i := range vals {
        ptrs[i] = &vals[i]
    }
    if err := rows.Scan(ptrs...); err != nil {
        return nil, err
    }
    m := make(map[string]interface{}, len(cols))
    for i, c := range cols {
        m[c] = vals[i]
    }
    return m, nil
}
`, name, idType, embedSQL(sel), embedSQL(from), embedSQL(whereCol), g.placeholder(1))
}

// getQuerySQL builds the SELECT list, FROM fragment and WHERE key column for a
// resource's Get query. Prefers the captured schema block; falls back to the
// resource's detail/update fields + key column when the table is absent.
func (g *Generator) getQuerySQL(r types.Resource) (sel, from, whereCol string) {
	tName := tableName(r)
	whereCol = idColumn(r)

	if st := g.schemaTable(tName); st != nil {
		var cols []string
		for _, c := range st.Columns {
			cols = append(cols, "t."+g.quoteIdent(c.Name))
		}
		fromParts := []string{g.quoteIdent(st.Name) + " t"}
		for _, fk := range st.ForeignKeys {
			cols = append(cols, fmt.Sprintf("f_%s.%s AS %s_label", fk.ForeignTable, g.quoteIdent(fk.Label), fk.Column))
			fromParts = append(fromParts, fmt.Sprintf("LEFT JOIN %s f_%s ON f_%s.%s = t.%s", g.quoteIdent(fk.ForeignTable), fk.ForeignTable, fk.ForeignTable, g.quoteIdent(fk.ForeignColumn), g.quoteIdent(fk.Column)))
		}
		if st.PK != "" {
			whereCol = "t." + g.quoteIdent(st.PK)
		}
		return strings.Join(cols, ", "), strings.Join(fromParts, " "), whereCol
	}

	var cols []string
	seen := map[string]bool{}
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			cols = append(cols, g.quoteIdent(c))
		}
	}
	if r.Detail != nil {
		for _, f := range r.Detail.Fields {
			add(f.Name)
		}
	}
	if r.Form != nil && r.Form.Update != nil {
		for _, f := range r.Form.Update.Fields {
			add(f.Name)
		}
	}
	add(whereCol)
	if len(cols) == 0 {
		return "*", g.quoteIdent(tName), g.quoteIdent(whereCol)
	}
	return strings.Join(cols, ", "), g.quoteIdent(tName), g.quoteIdent(whereCol)
}

// schemaTable returns the schema block entry for a table name, comparing
// exact then case-insensitive (MSSQL PascalCase table names). nil when the
// config carries no schema block or the table is absent.
func (g *Generator) schemaTable(name string) *types.SchemaTable {
	if g.Config.Schema == nil {
		return nil
	}
	for i := range g.Config.Schema.Tables {
		t := &g.Config.Schema.Tables[i]
		if t.Name == name || strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return nil
}
