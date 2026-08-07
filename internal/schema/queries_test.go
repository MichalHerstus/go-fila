package schema

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-fila/go-fila/internal/types"
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
	if len(refs.Columns["User"]) != 4 {
		t.Errorf("columns for User: %v", refs.Columns["User"])
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
