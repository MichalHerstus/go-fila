package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/schema"
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// testConfig builds a config with one resource, one page and navigation.
func testConfig() *types.Config {
	return &types.Config{
		Version: "1",
		Panel: types.Panel{
			Name: "Admin",
			Path: "/admin",
			Brand: types.Brand{Colors: types.BrandColors{
				Primary: "#6366f1", Secondary: "#8b5cf6",
			}},
			Layout: types.Layout{Sidebar: types.SidebarLayout{
				Collapsible: true, Width: 256, CollapsedWidth: 64,
			}},
		},
		Connections: map[string]types.Connection{
			"default": {Driver: "sqlite", DSN: "file:demo.db"},
		},
		SQLC: types.SQLCConfig{
			Config:     "sqlc.yaml",
			QueriesDir: "./sql/queries",
			SchemaDir:  "./sql/migrations",
			OutputPkg:  "internal/data",
		},
		Auth: types.AuthConfig{Table: "users", Login: types.LoginConfig{
			Fields: []string{"email", "password"}, Redirect: "/admin/dashboard",
		}},
		Navigation: []types.NavigationGroup{{
			Group: "Sales",
			Items: []types.NavigationItem{{Resource: "User", Type: "resource"}},
		}},
		Resources: []types.Resource{{
			Name:  "User",
			Label: "Users",
			List: &types.ListConfig{
				Query:      "ListUsers",
				CountQuery: "CountUsers",
				Columns:    []types.Column{{Name: "id", Label: "ID", Type: "integer", Sortable: true}},
			},
			Detail: &types.DetailConfig{
				Query:  "GetUser",
				Params: map[string]string{"id": "{record.id}"},
				Fields: []types.Field{{Name: "email", Type: "email"}},
			},
			Form: &types.FormConfig{
				Create: &types.FormAction{Query: "CreateUser", Fields: []types.Field{{Name: "email", Type: "email"}}},
			},
			Actions: []types.Action{{Name: "archive", Label: "Archive", Query: "UPDATE users SET archived = 1 WHERE id = ?"}},
		}},
		Pages: []types.Page{{
			Name:    "Dashboard",
			Path:    "/dashboard",
			Default: true,
			Widgets: []types.Widget{
				{Type: "stat", Label: "Users", Query: "SELECT COUNT(*) FROM users"},
				{Type: "chart", Label: "Revenue", Chart: &types.ChartConfig{Type: "line"}},
				{Type: "stats_grid", Label: "Grid", Columns: 2,
					Widgets: []types.Widget{{Type: "stat", Label: "A"}}},
			},
		}},
	}
}

// TestPageBuilders builds every page without running the app, ensuring no
// nil-pointer panics and non-nil primitives.
func TestPageBuilders(t *testing.T) {
	e := New(testConfig(), "testdata/go-fila.yaml")
	builders := map[string]func() tview.Primitive{
		"home":        e.homePage,
		"panel":       e.panelPage,
		"brand":       e.brandPage,
		"layout":      e.layoutPage,
		"theme":       e.themePage,
		"connections": e.connectionsPage,
		"sqlc":        e.sqlcPage,
		"auth":        e.authPage,
		"navigation":  e.navGroupsPage,
		"pages":       e.pagesPage,
		"resources":   e.resourcesPage,
		"validate":    e.validatePage,
		"preview":     e.previewPage,
	}
	for name, build := range builders {
		if p := build(); p == nil {
			t.Errorf("%s: nil primitive", name)
		}
	}
}

// TestResourcePages exercises the nested resource editors.
func TestResourcePages(t *testing.T) {
	e := New(testConfig(), "testdata/go-fila.yaml")
	for _, fn := range []func() tview.Primitive{
		func() tview.Primitive { return e.resourcePage(0) },
		func() tview.Primitive { return e.listPage(0) },
		func() tview.Primitive { return e.columnsPage(0) },
		func() tview.Primitive { return e.cardPage(0) },
		func() tview.Primitive { return e.detailPage(0) },
		func() tview.Primitive { return e.formPage(0) },
		func() tview.Primitive { return e.formActionPage(0, "create") },
		func() tview.Primitive { return e.formFieldsPage(0, "create") },
		func() tview.Primitive { return e.actionsPage(0) },
		func() tview.Primitive { return e.policiesPage(0) },
	} {
		if fn() == nil {
			t.Error("nil primitive")
		}
	}
}

// TestProcPages exercises the hook/action editors with proc-configured items,
// ensuring the three-way kind picker and the Proc field render without panic.
func TestProcPages(t *testing.T) {
	cfg := testConfig()
	cfg.Resources[0].Form.Create.Hooks = &types.Hooks{
		After: []types.Hook{{Name: "archive_created", Proc: "sp_archive_user"}},
	}
	cfg.Resources[0].Actions[0].Proc = "sp_archive_user"
	cfg.Resources[0].Actions[0].Query = ""
	e := New(cfg, "testdata/go-fila.yaml")

	hs := cfg.Resources[0].Form.Create.Hooks
	get := func() *[]types.Hook { return &hs.After }
	if p := e.hookListPage(&cfg.Resources[0].Form.Create.Hooks, false); p == nil {
		t.Error("hookListPage: nil primitive")
	}
	if p := e.hookPage(get, 0); p == nil {
		t.Error("hookPage: nil primitive")
	}
	if p := e.actionsPage(0); p == nil {
		t.Error("actionsPage: nil primitive")
	}
	if p := e.actionPage(0, 0); p == nil {
		t.Error("actionPage: nil primitive")
	}
}

func TestSyncReport(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "sql", "migrations")
	queries := filepath.Join(dir, "sql", "queries")
	if err := os.MkdirAll(migrations, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(queries, 0755); err != nil {
		t.Fatal(err)
	}
	schemaSQL := `CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL,
  role_id INTEGER REFERENCES roles(id)
);
CREATE TABLE roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL
);`
	if err := os.WriteFile(filepath.Join(migrations, "001.sql"), []byte(schemaSQL), 0644); err != nil {
		t.Fatal(err)
	}
	// Only the roles query file exists; ListUsers is missing.
	rolesQ := `-- name: ListRoles :many
SELECT id, name FROM roles ORDER BY name;
`
	if err := os.WriteFile(filepath.Join(queries, "roles.sql"), []byte(rolesQ), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SQLC.SchemaDir = "./sql/migrations"
	cfg.SQLC.QueriesDir = "./sql/queries"
	cfg.Connections["default"] = types.Connection{Driver: "sqlite"}
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))
	rep := e.analyze()
	if rep.err != "" {
		t.Fatalf("analyze error: %s", rep.err)
	}
	if len(rep.tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(rep.tables))
	}
	if !containsString(queryRefNames(rep), "ListUsers") {
		t.Errorf("expected ListUsers flagged as missing, got %v", queryRefNames(rep))
	}
	if containsString(rep.missingTabs, "User -> users") {
		t.Errorf("User -> users should resolve, got %v", rep.missingTabs)
	}
	cfg.Resources = append(cfg.Resources, types.Resource{Name: "Product"})
	e = New(cfg, filepath.Join(dir, "go-fila.yaml"))
	rep = e.analyze()
	if !containsString(rep.missingTabs, "Product -> products") {
		t.Errorf("expected Product -> products flagged, got %v", rep.missingTabs)
	}

	e.generateMissingQueries(rep)
	if _, err := os.Stat(filepath.Join(queries, "users.sql")); err != nil {
		t.Errorf("users.sql was not generated: %v", err)
	}

	e.importResourcesFromSchema(rep)
	found := false
	for _, r := range cfg.Resources {
		if r.Name == "Role" {
			found = true
		}
	}
	if !found {
		t.Error("expected Role resource imported from schema")
	}
}

func queryRefNames(rep *syncReport) []string {
	var out []string
	for _, q := range rep.missingQ {
		out = append(out, q.Name)
	}
	return out
}

// TestSyncInlineSQLSplit verifies that inline SQL references (widget/action
// queries) are reported separately and never counted as missing named queries.
func TestSyncInlineSQLSplit(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "sql", "migrations")
	queries := filepath.Join(dir, "sql", "queries")
	if err := os.MkdirAll(migrations, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(queries, 0755); err != nil {
		t.Fatal(err)
	}
	schemaSQL := `CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL
);`
	if err := os.WriteFile(filepath.Join(migrations, "001.sql"), []byte(schemaSQL), 0644); err != nil {
		t.Fatal(err)
	}
	// Every named query the config references exists.
	usersQ := `-- name: ListUsers :many
SELECT id, email FROM users;
-- name: CountUsers :one
SELECT COUNT(*) FROM users;
-- name: GetUser :one
SELECT id, email FROM users WHERE id = ?;
-- name: CreateUser :one
INSERT INTO users (email) VALUES (?) RETURNING id;
`
	if err := os.WriteFile(filepath.Join(queries, "users.sql"), []byte(usersQ), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SQLC.SchemaDir = "./sql/migrations"
	cfg.SQLC.QueriesDir = "./sql/queries"
	cfg.Connections["default"] = types.Connection{Driver: "sqlite"}
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))
	rep := e.analyze()

	if len(rep.missingQ) != 0 {
		t.Errorf("all named queries exist; got missing %v", queryRefNames(rep))
	}
	// testConfig has one action (UPDATE ...) and one widget (SELECT ...) with
	// inline SQL — they must land in inlineQ (deduped by name), never missingQ.
	if len(rep.inlineQ) != 2 {
		t.Errorf("expected 2 inline SQL refs (action + widget), got %d: %v", len(rep.inlineQ), rep.inlineQ)
	}
	for _, q := range rep.inlineQ {
		if !strings.ContainsAny(q.Name, " \t") {
			t.Errorf("inline ref %q looks like a query name: %+v", q.Name, q)
		}
	}
}

// TestGenerateQueriesProducesValidBody sanity-checks the query output.
func TestGenerateQueriesProducesValidBody(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sql", "migrations"), 0755); err != nil {
		t.Fatal(err)
	}
	schemaSQL := `CREATE TABLE customers (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TIMESTAMP
);`
	mp := filepath.Join(dir, "sql", "migrations", "c.sql")
	if err := os.WriteFile(mp, []byte(schemaSQL), 0644); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "sql", "migrations", "*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	tables, err := schema.ParseSchema(matches...)
	if err != nil {
		t.Fatal(err)
	}
	q := schema.GenerateQueries(tables, "sqlite")
	if q["customers.sql"] == "" {
		t.Fatal("expected customers.sql query content")
	}
	if !strings.Contains(q["customers.sql"], "ListCustomers") {
		t.Error("missing ListCustomers")
	}
}
