package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// fkSchemaTables returns the introspected tables for a schema where
// sklad_zasoby.pn references sklad_zbozi.pn (a non-PK column) and sklad_zbozi
// is itself a user-visible resource. This mirrors the real-world case that
// exposed the FK join and options generation bugs.
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

// TestConvertSchemaFKLabel ensures the captured schema block records each FK's
// foreign table + label column (used to derive option SQL at generation time).
func TestConvertSchemaFKLabel(t *testing.T) {
	s := convertSchema(fkSchemaTables(), "postgres")

	if len(s.Tables) != 2 {
		t.Fatalf("expected 2 schema tables, got %d", len(s.Tables))
	}
	zasoby := s.Tables[0]
	if zasoby.Name != "sklad_zasoby" || zasoby.PK != "id" {
		t.Fatalf("sklad_zasoby wrong: %+v", zasoby)
	}
	if len(zasoby.ForeignKeys) != 1 {
		t.Fatalf("expected 1 FK, got %d", len(zasoby.ForeignKeys))
	}
	fk := zasoby.ForeignKeys[0]
	if fk.Column != "pn" || fk.ForeignTable != "sklad_zbozi" || fk.ForeignColumn != "pn" {
		t.Fatalf("FK wrong: %+v", fk)
	}
	// sklad_zbozi has no "name"/"title"/"label" column; the label falls back to
	// its first non-PK string column (pn).
	if fk.Label != "pn" {
		t.Fatalf("FK label should fall back to pn, got %q", fk.Label)
	}
}

// TestConvertSchemaColumnTypes ensures yaga field types come from the DB types
// via mapDBTypeToFieldType.
func TestConvertSchemaColumnTypes(t *testing.T) {
	s := convertSchema(fkSchemaTables(), "postgres")
	zasoby := s.Tables[0]

	got := map[string]string{}
	for _, c := range zasoby.Columns {
		got[c.Name] = c.Type
	}
	if got["mnozstvi"] != "float" {
		t.Fatalf("numeric must map to float, got %q", got["mnozstvi"])
	}
	if got["created_at"] != "datetime" {
		t.Fatalf("timestamp must map to datetime, got %q", got["created_at"])
	}
}

// TestGenerateYAMLHasSchemaBlock ensures the introspected config carries the
// `schema:` block (the sole schema source) and no longer a `sqlc:` block.
func TestGenerateYAMLHasSchemaBlock(t *testing.T) {
	out := generateYAML(fkSchemaTables(), "postgres", "postgres://x/x")

	if !strings.Contains(out, "schema:") {
		t.Fatalf("yaga.yaml must contain a schema: block:\n%s", out)
	}
	if !strings.Contains(out, "  tables:") {
		t.Fatalf("schema block must list tables:\n%s", out)
	}
	if !strings.Contains(out, "  - name: sklad_zasoby") {
		t.Fatalf("schema block must contain sklad_zasoby:\n%s", out)
	}
	if strings.Contains(out, "sqlc:") {
		t.Fatal("yaga.yaml must not contain a sqlc: block")
	}
	if strings.Contains(out, "queries_dir") {
		t.Fatal("yaga.yaml must not reference queries_dir")
	}
}

// TestGenerateYAMLSchemaParses ensures the emitted schema block round-trips
// through the yaml parser used by the generator (Field.OptionsSQL tags parse,
// schema table/FK structure intact).
func TestGenerateYAMLSchemaParses(t *testing.T) {
	out := generateYAML(fkSchemaTables(), "postgres", "postgres://x/x")

	var doc struct {
		Schema struct {
			Tables []struct {
				Name        string `yaml:"name"`
				PK          string `yaml:"pk"`
				ForeignKeys []struct {
					Column        string `yaml:"column"`
					ForeignTable  string `yaml:"foreign_table"`
					ForeignColumn string `yaml:"foreign_column"`
					Label         string `yaml:"label"`
				} `yaml:"foreign_keys"`
			} `yaml:"tables"`
		} `yaml:"schema"`
	}
	if err := yaml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("schema block does not parse: %v\n%s", err, out)
	}
	if len(doc.Schema.Tables) != 2 {
		t.Fatalf("expected 2 schema tables, got %d", len(doc.Schema.Tables))
	}
	if len(doc.Schema.Tables[0].ForeignKeys) != 1 {
		t.Fatalf("expected 1 FK in first table, got %d", len(doc.Schema.Tables[0].ForeignKeys))
	}
	fk := doc.Schema.Tables[0].ForeignKeys[0]
	if fk.Column != "pn" || fk.ForeignTable != "sklad_zbozi" || fk.ForeignColumn != "pn" || fk.Label != "pn" {
		t.Fatalf("FK structure wrong after round-trip: %+v", fk)
	}
}

// TestWriteResourceYAMLRelationField ensures the relation field carries
// options_value/options_label (option SQL is derived from the schema block)
// and no longer an options_query reference.
func TestWriteResourceYAMLRelationField(t *testing.T) {
	var b strings.Builder
	tables := fkSchemaTables()
	writeResourceYAML(&b, tables[0], tables, "postgres")
	out := b.String()

	if strings.Contains(out, "options_query:") {
		t.Fatalf("relation field must not reference options_query, got:\n%s", out)
	}
	if !strings.Contains(out, "options_value: pn") {
		t.Fatalf("options_value must be the referenced foreign column pn:\n%s", out)
	}
	if !strings.Contains(out, "options_label: pn") {
		t.Fatalf("options_label must be the FK target's label column:\n%s", out)
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

// TestConvertSchemaView ensures views are marked with View in the schema block
// and keep their fallback key column.
func TestConvertSchemaView(t *testing.T) {
	s := convertSchema(viewTables(), "postgres")

	summary := s.Tables[1]
	if !summary.View {
		t.Fatal("order_summary must be marked as a view in the schema block")
	}
	if summary.PK != "customer_name" {
		t.Fatalf("view key must fall back to its first column, got %q", summary.PK)
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
