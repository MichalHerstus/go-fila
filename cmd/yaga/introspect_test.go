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
