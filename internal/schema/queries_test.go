package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/types"
)

func TestParseQueries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "user.sql"), []byte(`
-- name: ListUsers :many
SELECT u.*, r.name as role_name
FROM users u
LEFT JOIN roles r ON r.id = u.role_id
ORDER BY u.created_at DESC;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: CreateUser :one
INSERT INTO users (name, email) VALUES ($1, $2) RETURNING *;
`), 0644)
	os.WriteFile(filepath.Join(dir, "role.sql"), []byte(`
--name: ListRoles :many
SELECT id, name FROM roles ORDER BY name;
`), 0644)

	qs := ParseQueries(dir)
	if len(qs) != 4 {
		t.Fatalf("want 4 queries, got %d", len(qs))
	}
	l := qs["ListUsers"]
	if l.Variant != ":many" {
		t.Errorf("variant: %q", l.Variant)
	}
	if !contains(l.Body, "role_name") {
		t.Errorf("body missing role_name alias")
	}
	if len(l.SelectCols) != 1 || l.SelectCols[0] != "role_name" {
		t.Errorf("ListUsers SelectCols: %v", l.SelectCols)
	}
	if len(qs["CountUsers"].SelectCols) != 0 {
		t.Errorf("COUNT should yield no select cols")
	}
	if len(qs["CreateUser"].SelectCols) != 0 {
		t.Errorf("INSERT should yield no select cols")
	}
	if r := qs["ListRoles"]; len(r.SelectCols) != 2 || r.SelectCols[0] != "id" || r.SelectCols[1] != "name" {
		t.Errorf("ListRoles SelectCols: %v", r.SelectCols)
	}
}

func TestSelectColumns(t *testing.T) {
	cases := []struct {
		sql  string
		want []string
	}{
		{`SELECT t.id, t.name FROM t;`, []string{"id", "name"}},
		{`SELECT id, name AS label FROM t`, []string{"id", "label"}},
		{`SELECT COUNT(*), MAX(x) FROM t`, nil},
		{`SELECT r.name, u.* FROM u JOIN r`, []string{"name"}},
		{`SELECT COALESCE(role_name, '') as role_name FROM users`, []string{"role_name"}},
	}
	for _, c := range cases {
		got := SelectColumns(c.sql)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.sql, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v want %v", c.sql, got, c.want)
				break
			}
		}
	}
}

func TestCollectReferences(t *testing.T) {
	cfg := &types.Config{
		Resources: []types.Resource{
			{
				Name: "User",
				List: &types.ListConfig{Query: "ListUsers", CountQuery: "CountUsers",
					Columns: []types.Column{{Name: "id"}, {Name: "email"}}},
				Detail: &types.DetailConfig{Query: "GetUser",
					Fields: []types.Field{{Name: "name"}, {Name: "role_id", OptionsQuery: "ListRoles"}}},
				Card: &types.CardConfig{
					Fields:      []types.Field{{Name: "email"}},
					KanbanField: "status",
					Searchable:  []string{"email"},
					DefaultSort: "-created_at",
				},
				Form: &types.FormConfig{
					Create: &types.FormAction{Query: "CreateUser"},
					Update: &types.FormAction{Query: "UpdateUser", PopulateQuery: "GetUser"},
				},
				Actions: []types.Action{{Name: "archive", Query: "ArchiveUser"}},
			},
		},
		Pages: []types.Page{{
			Name: "Dashboard",
			Widgets: []types.Widget{
				{Type: "stat", Label: "x", Query: "CountUsers"},
				{Type: "chart", Label: "y", Query: "ChartData", Chart: &types.ChartConfig{Type: "line"}},
				{Type: "stats_grid", Widgets: []types.Widget{{Type: "stat", Label: "z", Query: "CountUsers"}}},
			},
		}},
	}
	refs := CollectReferences(cfg)
	wantQueries := []string{"ListUsers", "CountUsers", "GetUser", "ListRoles",
		"CreateUser", "UpdateUser", "ArchiveUser", "ChartData"}
	got := map[string]bool{}
	for _, q := range refs.Queries {
		got[q.Name] = true
	}
	for _, w := range wantQueries {
		if !got[w] {
			t.Errorf("missing query ref %s", w)
		}
	}
	if len(got) != len(wantQueries) {
		t.Errorf("unexpected extra refs: %v", got)
	}
	if refs.Tables["User"] != "users" {
		t.Errorf("table for User: %q", refs.Tables["User"])
	}
	// Columns is the deduplicated summary: id, email (list+card), status,
	// created_at, name, role_id = 6 unique names.
	if len(refs.Columns["User"]) != 6 {
		t.Errorf("columns for User: %v", refs.Columns["User"])
	}
	// ColumnRefs pins each reference to its section + index.
	wantRefs := []ColumnRef{
		{"id", "list.columns", 0},
		{"email", "list.columns", 1},
		{"email", "card.fields", 0},
		{"email", "card.searchable", 0},
		{"status", "card.kanban_field", 0},
		{"created_at", "card.default_sort", 0},
		{"name", "detail.fields", 0},
		{"role_id", "detail.fields", 1},
	}
	gotRefs := refs.ColumnRefs["User"]
	if len(gotRefs) != len(wantRefs) {
		t.Fatalf("ColumnRefs for User: got %d, want %d: %+v", len(gotRefs), len(wantRefs), gotRefs)
	}
	for i, w := range wantRefs {
		if gotRefs[i] != w {
			t.Errorf("ColumnRefs[%d] = %+v, want %+v", i, gotRefs[i], w)
		}
	}
}

func TestTableNameFor(t *testing.T) {
	if TableNameFor(types.Resource{Name: "User"}) != "users" {
		t.Error("pluralize failed")
	}
	if TableNameFor(types.Resource{Name: "User", Table: "accounts"}) != "accounts" {
		t.Error("explicit table failed")
	}
}

func TestGenerateQueriesAndYAML(t *testing.T) {
	tables := ParseSchemaBytes([]byte(`CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    customer_id INT REFERENCES customers(id),
    total DECIMAL(10,2),
    created_at TIMESTAMPTZ DEFAULT NOW()
);
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL
);`))
	qs := GenerateQueries(tables, "postgres")
	if _, ok := qs["orders.sql"]; !ok {
		t.Fatal("no orders.sql")
	}
	if _, ok := qs["customers_options.sql"]; !ok {
		t.Fatal("no customers_options.sql")
	}
	yaml := GenerateResourceYAML(tables[0], tables, "postgres")
	for _, want := range []string{"name: Order", "list:", "form:", "options_query: ListCustomers"} {
		if !contains(yaml, want) {
			t.Errorf("yaml missing %q", want)
		}
	}
}

func TestRawBody(t *testing.T) {
	const file = "-- name: ListUsers :many\nSELECT u.id, u.name\nFROM users u\nORDER BY u.id;\n\n-- name: CountUsers :one\nSELECT COUNT(*) FROM users;\n"
	qs := ParseQueriesForFile(file, "users.sql")
	lu, ok := qs["ListUsers"]
	if !ok {
		t.Fatal("ListUsers missing")
	}
	if want := "SELECT u.id, u.name\nFROM users u\nORDER BY u.id;"; lu.RawBody != want {
		t.Errorf("RawBody = %q, want %q", lu.RawBody, want)
	}
	if !strings.Contains(lu.Body, "FROM users u") || strings.Contains(lu.Body, "\n") {
		t.Errorf("Body must stay collapsed single-line, got %q", lu.Body)
	}
	if lu.File != "users.sql" {
		t.Errorf("File = %q", lu.File)
	}
}

func TestRewriteQueryBody(t *testing.T) {
	const file = "-- name: ListUsers :many\nSELECT u.id, u.name\nFROM users u;\n\n-- name: CountUsers :one\nSELECT COUNT(*) FROM users;\n"
	rewritten := RewriteQueryBody(file, "ListUsers", "SELECT id, email FROM users;")
	for _, want := range []string{"-- name: ListUsers :many", "SELECT id, email FROM users;", "-- name: CountUsers :one", "SELECT COUNT(*) FROM users;"} {
		if !strings.Contains(rewritten, want) {
			t.Errorf("rewritten file missing %q:\n%s", want, rewritten)
		}
	}
	if strings.Contains(rewritten, "u.name") {
		t.Errorf("old body leaked into rewrite:\n%s", rewritten)
	}
	if RewriteQueryBody(file, "MissingQuery", "SELECT 1") != file {
		t.Error("rewriting an unknown query must leave the file unchanged")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
