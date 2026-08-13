package main

import (
	"strings"
	"testing"
)

// fkSchemaTables returns the introspected tables for a schema where
// sklad_zasoby.pn references sklad_zbozi.pn (a non-PK column) and sklad_zbozi
// is itself a user-visible resource. This mirrors the real-world case that
// exposed the FK join and options_query generation bugs.
func fkSchemaTables() []TableInfo {
	return []TableInfo{
		{
			Name: "sklad_zasoby",
			Columns: []ColumnInfo{
				{Name: "id", DBType: "integer", IsPrimaryKey: true},
				{Name: "pn", DBType: "character varying"},
				{Name: "mnozstvi", DBType: "numeric"},
				{Name: "created_at", DBType: "timestamp without time zone"},
			},
			ForeignKeys: []ForeignKeyInfo{
				{Column: "pn", ForeignTable: "sklad_zbozi", ForeignColumn: "pn"},
			},
		},
		{
			Name: "sklad_zbozi",
			Columns: []ColumnInfo{
				{Name: "id", DBType: "integer", IsPrimaryKey: true},
				{Name: "pn", DBType: "character varying"},
				{Name: "pn_nazev", DBType: "character varying"},
			},
		},
	}
}

// TestGenerateQueriesFKJoinNonPK ensures the FK LEFT JOIN uses the referenced
// foreign column, not the foreign table's primary key.
func TestGenerateQueriesFKJoinNonPK(t *testing.T) {
	queries := generateQueries(fkSchemaTables(), "postgres")

	zasoby := queries["sklad_zasoby.sql"]
	if !strings.Contains(zasoby, "LEFT JOIN sklad_zbozi f_sklad_zbozi ON f_sklad_zbozi.pn = t.pn") {
		t.Fatalf("FK join must reference the foreign column (pn), got:\n%s", zasoby)
	}
	if strings.Contains(zasoby, "f_sklad_zbozi.id = t.pn") {
		t.Fatal("FK join must not use the foreign primary key")
	}
}

// TestGenerateQueriesNoDuplicateListNames ensures a FK target that is itself a
// resource does not get a second options file with a colliding query name.
func TestGenerateQueriesNoDuplicateListNames(t *testing.T) {
	queries := generateQueries(fkSchemaTables(), "postgres")

	if _, ok := queries["sklad_zbozi_options.sql"]; ok {
		t.Fatal("sklad_zbozi is a resource with its own List query; no options file should be generated")
	}

	names := map[string]bool{}
	for _, content := range queries {
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "-- name: ") {
				qn := strings.TrimSpace(strings.TrimPrefix(trimmed, "-- name: "))
				qn = strings.SplitN(qn, " ", 2)[0]
				if names[qn] {
					t.Fatalf("duplicate query name %q across generated files", qn)
				}
				names[qn] = true
			}
		}
	}
	if !names["ListSkladZbozi"] {
		t.Fatal("ListSkladZbozi query missing from generated output")
	}
}

// TestWriteResourceYAMLOptionsQuery ensures the relation field's options_query
// matches the name of the query generateQueries emits for the FK target.
func TestWriteResourceYAMLOptionsQuery(t *testing.T) {
	var b strings.Builder
	tables := fkSchemaTables()
	writeResourceYAML(&b, tables[0], tables, "postgres")
	out := b.String()

	if !strings.Contains(out, "options_query: ListSkladZbozi") {
		t.Fatalf("options_query must be ListSkladZbozi (the generated query), got:\n%s", out)
	}
	if strings.Contains(out, "options_query: ListSkladZbozis") {
		t.Fatal("options_query must not be pluralized")
	}
	if !strings.Contains(out, "options_value: pn") {
		t.Fatalf("options_value must be the referenced foreign column pn:\n%s", out)
	}
}

// viewTables returns tables for a schema where order_summary is a database view
// (no primary key, no foreign keys) alongside a real orders table.
func viewTables() []TableInfo {
	return []TableInfo{
		{
			Name: "orders",
			Columns: []ColumnInfo{
				{Name: "id", DBType: "integer", IsPrimaryKey: true},
				{Name: "customer_name", DBType: "character varying"},
				{Name: "total", DBType: "numeric"},
			},
		},
		{
			Name:   "order_summary",
			IsView: true,
			Columns: []ColumnInfo{
				{Name: "customer_name", DBType: "character varying"},
				{Name: "total", DBType: "numeric"},
				{Name: "created_at", DBType: "timestamp without time zone"},
			},
		},
	}
}

// intGridView returns a view whose resolved key column is an integer literal id,
// so it is eligible for a detail ("view form").
func intGridView() TableInfo {
	return TableInfo{
		Name:   "active_customers",
		IsView: true,
		Columns: []ColumnInfo{
			{Name: "id", DBType: "integer"},
			{Name: "name", DBType: "character varying"},
		},
	}
}

// TestWriteResourceYAMLViewReadOnly ensures a text-keyed view is emitted as a
// read-only resource: list + card present, no form section, and no detail (the
// non-integer key column cannot feed the int-casting detail handler).
func TestWriteResourceYAMLViewReadOnly(t *testing.T) {
	var b strings.Builder
	tables := viewTables()
	writeResourceYAML(&b, tables[1], tables, "postgres")
	out := b.String()

	if !strings.Contains(out, "list:") || !strings.Contains(out, "card:") {
		t.Fatalf("view resource must have list/card sections, got:\n%s", out)
	}
	if strings.Contains(out, "form:") {
		t.Fatalf("view resource must not have a form section, got:\n%s", out)
	}
	if strings.Contains(out, "detail:") {
		t.Fatalf("text-keyed view must not emit a detail section, got:\n%s", out)
	}
	if !strings.Contains(out, "id_column: customer_name") {
		t.Fatalf("view key column must fall back to the first column (customer_name), got:\n%s", out)
	}
}

// TestWriteResourceYAMLViewDetail ensures an integer-keyed view still gets the
// detail ("view form") section.
func TestWriteResourceYAMLViewDetail(t *testing.T) {
	var b strings.Builder
	writeResourceYAML(&b, intGridView(), []TableInfo{intGridView()}, "postgres")
	out := b.String()

	if !strings.Contains(out, "detail:") {
		t.Fatalf("integer-keyed view must emit a detail section, got:\n%s", out)
	}
	if !strings.Contains(out, "card:") {
		t.Fatalf("view must emit a card section, got:\n%s", out)
	}
	if strings.Contains(out, "form:") {
		t.Fatalf("view must not emit a form section, got:\n%s", out)
	}
}

// TestGenerateQueriesViewReadOnly ensures views only get read queries
// (List/Count/Get) and no write queries (Create/Update/Delete).
func TestGenerateQueriesViewReadOnly(t *testing.T) {
	queries := generateQueries(viewTables(), "postgres")

	sql := queries["order_summary.sql"]
	if !strings.Contains(sql, "-- name: ListOrderSummary :many") {
		t.Fatalf("view must have a List query:\n%s", sql)
	}
	if !strings.Contains(sql, "-- name: CountOrderSummary :one") {
		t.Fatalf("view must have a Count query:\n%s", sql)
	}
	if !strings.Contains(sql, "-- name: GetOrderSummary :one") {
		t.Fatalf("view must have a Get query:\n%s", sql)
	}
	for _, bad := range []string{
		"CreateOrderSummary", "UpdateOrderSummary", "DeleteOrderSummary",
		"INSERT INTO order_summary", "UPDATE order_summary", "DELETE FROM order_summary",
	} {
		if strings.Contains(sql, bad) {
			t.Fatalf("view must not emit write query %q:\n%s", bad, sql)
		}
	}
	// The Get query must key on the view's fallback key column.
	if !strings.Contains(sql, "WHERE t.customer_name = $1") {
		t.Fatalf("view Get query must key on customer_name:\n%s", sql)
	}
}

// TestGenerateSchemaSQLView ensures schema.sql (sqlc input) carries a synthetic
// CREATE TABLE for a view so sqlc can infer its column types.
func TestGenerateSchemaSQLView(t *testing.T) {
	schema := generateSchemaSQL(viewTables(), false, false, "postgres")
	if !strings.Contains(schema, "CREATE TABLE order_summary (") {
		t.Fatalf("schema.sql must emit a synthetic CREATE TABLE for the view:\n%s", schema)
	}
	if !strings.Contains(schema, "customer_name character varying") {
		t.Fatalf("schema.sql view DDL must include the view columns:\n%s", schema)
	}
}
