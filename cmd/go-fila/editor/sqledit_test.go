package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/types"
)

func TestSQLQueriesForResource(t *testing.T) {
	cfg := testConfig()
	names := sqlQueriesForResource(&cfg.Resources[0])
	want := map[string]bool{"ListUsers": true, "CountUsers": true, "GetUser": true, "CreateUser": true}
	if len(names) != len(want) {
		t.Fatalf("got %v", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected query %q", n)
		}
		if len(names) > 1 && names[len(names)-1] < names[len(names)-2] {
			t.Error("names must be sorted")
		}
	}
}

func TestSQLBaseResolution(t *testing.T) {
	dir := t.TempDir()

	// Output-dir layout (init / init --demo): sql lives under admin/.
	adminQ := filepath.Join(dir, "admin", "sql", "queries")
	adminM := filepath.Join(dir, "admin", "sql", "migrations")
	if err := os.MkdirAll(adminQ, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(adminM, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminQ, "users.sql"),
		[]byte("-- name: ListUsers :many\nSELECT id FROM users;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminM, "schema.sql"),
		[]byte("CREATE TABLE users (id integer primary key, name text);"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SQLC.QueriesDir = "./sql/queries"
	cfg.SQLC.SchemaDir = "./sql/migrations"
	cfg.Connections["default"] = types.Connection{Driver: "sqlite"}
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))

	if got := e.queriesDir(); got != adminQ {
		t.Errorf("queriesDir = %q, want %q (resolve through output dir)", got, adminQ)
	}
	rep := e.analyze()
	if len(rep.tables) != 1 || rep.tables[0].Name != "users" {
		t.Errorf("analyze should find users from admin/sql/migrations, got %+v", rep.tables)
	}
	if _, ok := rep.queries["ListUsers"]; !ok {
		t.Errorf("analyze should find ListUsers in admin/sql/queries, got %v", rep.queries)
	}
	for _, q := range rep.missingQ {
		if q.Name == "ListUsers" {
			t.Errorf("ListUsers must not be reported missing: %v", rep.missingQ)
		}
	}

	// Config-dir layout wins over the output dir when it exists (the
	// generator copies configDir/sql into the output dir).
	rootQ := filepath.Join(dir, "sql", "queries")
	if err := os.MkdirAll(rootQ, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootQ, "users.sql"),
		[]byte("-- name: ListUsers :many\nSELECT id, name FROM users;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := e.queriesDir(); got != rootQ {
		t.Errorf("queriesDir = %q, want %q (config dir is authoritative)", got, rootQ)
	}
}

func TestSQLQueryEditSaveFlush(t *testing.T) {
	dir := t.TempDir()
	qdir := filepath.Join(dir, "sql", "queries")
	if err := os.MkdirAll(qdir, 0755); err != nil {
		t.Fatal(err)
	}
	const users = "-- name: ListUsers :many\nSELECT id, name FROM users ORDER BY id;\n\n-- name: CountUsers :one\nSELECT COUNT(*) FROM users;\n"
	if err := os.WriteFile(filepath.Join(qdir, "users.sql"), []byte(users), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SQLC.QueriesDir = "./sql/queries"
	cfg.Connections["default"] = types.Connection{Driver: "sqlite"}
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))

	if e.sqlQueriesPage(0) == nil {
		t.Error("sqlQueriesPage returned nil")
	}
	if e.sqlEditPage("ListUsers") == nil {
		t.Error("sqlEditPage returned nil")
	}

	path, base, ok := e.queryBase("ListUsers", e.queriesDir())
	if !ok {
		t.Fatalf("ListUsers not resolved: %q", path)
	}
	e.stageQueryEdit(path, "ListUsers", base, "SELECT id, email FROM users;")
	if len(e.pendingSQL) != 1 {
		t.Fatalf("expected 1 staged file, got %d", len(e.pendingSQL))
	}
	if !e.modified {
		t.Error("staging should mark the editor modified")
	}

	// A sibling query in the same file must see the staged rewrite.
	_, stagedBase, ok := e.queryBase("CountUsers", e.queriesDir())
	if !ok {
		t.Fatal("CountUsers not resolved")
	}
	if !strings.Contains(stagedBase, "SELECT id, email FROM users;") {
		t.Error("staged edit should be visible when editing a sibling query in the same file")
	}

	e.save()
	content, err := os.ReadFile(filepath.Join(qdir, "users.sql"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(content)
	if !strings.Contains(s, "SELECT id, email FROM users;") {
		t.Errorf("rewritten body not written:\n%s", s)
	}
	if !strings.Contains(s, "ListUsers") || !strings.Contains(s, "CountUsers") {
		t.Errorf("other query blocks lost:\n%s", s)
	}
	if len(e.pendingSQL) != 0 {
		t.Error("pendingSQL should be cleared after save")
	}
	if e.modified {
		t.Error("save should clear modified")
	}
	if _, err := os.Stat(filepath.Join(dir, "go-fila.yaml")); err != nil {
		t.Errorf("config not saved: %v", err)
	}
}

func TestSQLQueryEditRevert(t *testing.T) {
	dir := t.TempDir()
	qdir := filepath.Join(dir, "sql", "queries")
	os.MkdirAll(qdir, 0755)
	const users = "-- name: ListUsers :many\nSELECT id FROM users;\n"
	os.WriteFile(filepath.Join(qdir, "users.sql"), []byte(users), 0644)

	cfg := testConfig()
	cfg.SQLC.QueriesDir = "./sql/queries"
	cfg.Connections["default"] = types.Connection{Driver: "sqlite"}
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))

	path, base, ok := e.queryBase("ListUsers", e.queriesDir())
	if !ok {
		t.Fatal("ListUsers not resolved")
	}
	e.stageQueryEdit(path, "ListUsers", base, "SELECT id, name FROM users;")
	if len(e.pendingSQL) != 1 {
		t.Fatal("edit should stage")
	}
	e.stageQueryEdit(path, "ListUsers", base, "SELECT id FROM users;")
	if len(e.pendingSQL) != 0 {
		t.Error("reverting the edit should unstage it")
	}
}
