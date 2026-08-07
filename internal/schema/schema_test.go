package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseSchemaPostgres(t *testing.T) {
	p := writeTemp(t, "schema.sql", `
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    role_id INT REFERENCES roles(id),
    role_name VARCHAR(100) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);
`)
	tables, err := ParseSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("want 2 tables, got %d", len(tables))
	}
	users := tables[0]
	if users.Name != "users" {
		t.Fatalf("want users, got %s", users.Name)
	}
	if len(users.Columns) != 8 {
		t.Fatalf("want 8 columns, got %d", len(users.Columns))
	}
	id := users.Columns[0]
	if !id.IsPrimaryKey {
		t.Errorf("id should be primary key")
	}
	if users.Columns[1].Nullable {
		t.Errorf("name should be NOT NULL")
	}
	if users.Columns[4].Name != "role_id" {
		t.Fatalf("want role_id, got %s", users.Columns[4].Name)
	}
	fks := users.Columns[4].FKs
	if len(fks) != 1 || fks[0].ForeignTable != "roles" || fks[0].ForeignColumn != "id" {
		t.Errorf("bad FK on role_id: %+v", fks)
	}
	if users.Columns[5].Default != "'user'" {
		t.Errorf("bad default on role_name: %q", users.Columns[5].Default)
	}
}

func TestParseSchemaSQLite(t *testing.T) {
	p := writeTemp(t, "schema.sql", `
CREATE TABLE products (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title TEXT NOT NULL,
    price REAL,
    active BOOLEAN NOT NULL DEFAULT 1,
    created_at TEXT DEFAULT (datetime('now'))
);
`)
	tables, err := ParseSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("want 1 table, got %d", len(tables))
	}
	pt := tables[0]
	if !pt.Columns[0].IsPrimaryKey {
		t.Errorf("id should be PK")
	}
	if pt.Columns[1].Nullable {
		t.Errorf("title should be NOT NULL")
	}
	if pt.Columns[2].Type != "REAL" {
		t.Errorf("price type: %q", pt.Columns[2].Type)
	}
	if pt.Columns[4].Default != "(datetime('now'))" {
		t.Errorf("created_at default: %q", pt.Columns[4].Default)
	}
}

func TestParseSchemaTableConstraintPK(t *testing.T) {
	p := writeTemp(t, "schema.sql", `
CREATE TABLE pairs (
    a INT,
    b INT,
    PRIMARY KEY (a, b)
);
`)
	tables, err := ParseSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 {
		t.Fatalf("want 1 table")
	}
	cols := tables[0].Columns
	if !cols[0].IsPrimaryKey || !cols[1].IsPrimaryKey {
		t.Errorf("composite PK not applied: %+v", cols)
	}
}

func TestParseSchemaSkipComments(t *testing.T) {
	p := writeTemp(t, "schema.sql", `
-- note: users -- tricky
CREATE TABLE users (
    id SERIAL PRIMARY KEY
    -- comment inside
);
/* block
   comment */
CREATE TABLE logs (
    id SERIAL PRIMARY KEY
);
`)
	tables, err := ParseSchema(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 2 {
		t.Fatalf("want 2 tables, got %d", len(tables))
	}
}
