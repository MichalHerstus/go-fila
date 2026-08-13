package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/types"
)

func TestSplitStatements(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"single", "UPDATE t SET x = 1", []string{"UPDATE t SET x = 1"}},
		{"trailing semicolon", "UPDATE t SET x = 1;", []string{"UPDATE t SET x = 1"}},
		{"two statements", "UPDATE t SET x = 1;\nINSERT INTO u (v) VALUES (2);", []string{"UPDATE t SET x = 1", "\nINSERT INTO u (v) VALUES (2)"}},
		{"semicolon in string", "INSERT INTO t (s) VALUES ('a;b');", []string{"INSERT INTO t (s) VALUES ('a;b')"}},
		{"escaped quote", "INSERT INTO t (s) VALUES ('it''s; ok');", []string{"INSERT INTO t (s) VALUES ('it''s; ok')"}},
		{"line comment semicolon", "UPDATE t SET x = 1; -- note; here\nINSERT INTO u (v) VALUES (2);", []string{"UPDATE t SET x = 1", " -- note; here\nINSERT INTO u (v) VALUES (2)"}},
		{"block comment", "UPDATE t SET x = 1 /* ; */ ;", []string{"UPDATE t SET x = 1 /* ; */ "}},
		{"double-quoted identifier", `CREATE TABLE "a;b" (id INTEGER);`, []string{`CREATE TABLE "a;b" (id INTEGER)`}},
		{"bracket identifier", "SELECT [x;y] FROM t;", []string{"SELECT [x;y] FROM t"}},
		{"empty batch", "", nil},
		{"only comments", "-- hello\n-- world\n", []string{"-- hello\n-- world\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitStatements(tc.sql)
			if len(got) != len(tc.want) {
				t.Fatalf("splitStatements(%q) = %#v, want %#v", tc.sql, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("stmt %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestContainsPlaceholder(t *testing.T) {
	cases := []struct {
		sql  string
		want bool
	}{
		{"UPDATE t SET status='x' WHERE id = $1", true},
		{"UPDATE t SET status='x'", false},
		{"INSERT INTO t (s) VALUES ('$1')", false},
		{"UPDATE t SET x = 1 WHERE id = $2", true},
		{"-- $1 comment\nSELECT 1", false},
		{"/* $1 */ SELECT 1", false},
		{"SELECT [a$1] FROM t", false},
		{"UPDATE t SET x = 1; UPDATE u SET y = $1", true},
	}
	for _, tc := range cases {
		if got := containsPlaceholder(tc.sql); got != tc.want {
			t.Errorf("containsPlaceholder(%q) = %v, want %v", tc.sql, got, tc.want)
		}
	}
}

// procDeclaredConfig returns a sqlite config with a declared procedure backing
// every proc: reference in procConfig (create after-hook, delete before-hook
// and the archive bulk action all reference sp_archive_user).
func procDeclaredConfig() *types.Config {
	cfg := procConfig("sqlite")
	cfg.Procedures = []types.Procedure{
		{
			Name:        "sp_archive_user",
			Description: "Archive the user",
			SQL:         "UPDATE users SET status = 'archived' WHERE id = $1;\nINSERT INTO events (msg) VALUES ('archived');",
		},
	}
	return cfg
}

// TestGenerateProceduresSQLite ensures a sqlite config with declared procedures
// emits the internal/panel/procs package (procs.Exec + statement splitter), the
// sql_procedures DDL + INSERT OR IGNORE seeds, and routes every proc: reference
// through procs.Exec instead of skipping it.
func TestGenerateProceduresSQLite(t *testing.T) {
	dir := t.TempDir()
	g := New(procDeclaredConfig(), dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}
	assertGeneratedGoParses(t, dir)

	procs, err := os.ReadFile(filepath.Join(dir, "internal/panel/procs/procs.go"))
	if err != nil {
		t.Fatalf("read procs.go: %v", err)
	}
	procsStr := string(procs)
	for _, want := range []string{
		"func Exec(db *sql.DB, name string, id int64) error",
		"SELECT body FROM sql_procedures WHERE name = ?",
		"func splitStatements",
		"func containsPlaceholder",
		"tx.Commit()",
	} {
		if !strings.Contains(procsStr, want) {
			t.Errorf("procs.go missing %q\n--- generated:\n%s", want, procsStr)
		}
	}

	mig, err := os.ReadFile(filepath.Join(dir, "sql/migrations/procedures.sql"))
	if err != nil {
		t.Fatalf("read procedures.sql: %v", err)
	}
	migStr := string(mig)
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS sql_procedures (",
		"name TEXT PRIMARY KEY",
		"INSERT OR IGNORE INTO sql_procedures (name, body, description) VALUES ('sp_archive_user', 'UPDATE users SET status = ''archived'' WHERE id = $1;\nINSERT INTO events (msg) VALUES (''archived'');', 'Archive the user');",
	} {
		if !strings.Contains(migStr, want) {
			t.Errorf("procedures.sql missing %q\n--- generated:\n%s", want, migStr)
		}
	}

	create := readResourceFile(t, dir, "user", "create.go")
	for _, want := range []string{
		`procs "`,
		`procs.Exec(db, "sp_archive_user", scope.ID)`,
		"RETURNING id",
	} {
		if !strings.Contains(create, want) {
			t.Errorf("create.go missing %q\n--- generated:\n%s", want, create)
		}
	}

	deleteStr := readResourceFile(t, dir, "user", "delete.go")
	for _, want := range []string{
		`procs "`,
		`procs.Exec(db, "sp_archive_user", scope.ID)`,
	} {
		if !strings.Contains(deleteStr, want) {
			t.Errorf("delete.go missing %q\n--- generated:\n%s", want, deleteStr)
		}
	}

	actions := readResourceFile(t, dir, "user", "actions.go")
	for _, want := range []string{
		`procs "`,
		`procs.Exec(db, "sp_archive_user", int64(id))`,
	} {
		if !strings.Contains(actions, want) {
			t.Errorf("actions.go missing %q\n--- generated:\n%s", want, actions)
		}
	}

	bulk := readResourceFile(t, dir, "user", "bulk.go")
	for _, want := range []string{
		`procs "`,
		`procs.Exec(db, "sp_archive_user", id)`,
	} {
		if !strings.Contains(bulk, want) {
			t.Errorf("bulk.go missing %q\n--- generated:\n%s", want, bulk)
		}
	}
	for _, f := range []string{create, deleteStr, actions, bulk} {
		for _, notWant := range []string{"CALL", "EXEC"} {
			if strings.Contains(f, notWant) {
				t.Errorf("generated file should not contain %q\n--- generated:\n%s", notWant, f)
			}
		}
	}
}

// TestGenerateProceduresNotEmitted guards the byte-identical regression: no
// procedures machinery when the driver is postgres (block ignored) or when the
// driver is sqlite but the procedures block is empty.
func TestGenerateProceduresNotEmitted(t *testing.T) {
	cases := []struct {
		name string
		cfg  *types.Config
	}{
		{"postgres", func() *types.Config {
			cfg := procConfig("postgres")
			cfg.Procedures = []types.Procedure{{Name: "sp_archive_user", SQL: "UPDATE users SET status='archived'"}}
			return cfg
		}()},
		{"sqlite undeclared", procConfig("sqlite")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			g := New(tc.cfg, dir)
			if err := g.Generate(); err != nil {
				t.Fatalf("generate: %v", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "internal/panel/procs/procs.go")); !os.IsNotExist(err) {
				t.Errorf("procs package should not be emitted (stat err = %v)", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "sql/migrations/procedures.sql")); !os.IsNotExist(err) {
				t.Errorf("procedures.sql should not be emitted (stat err = %v)", err)
			}
		})
	}
}

// TestGenerateProcSQLiteDeclaredProcOnlyHook ensures a proc-only hook block on
// sqlite with a declared procedure flips from "emit nothing" to the full
// lifecycle: hooks import, RETURNING id capture and the procs.Exec call.
func TestGenerateProcSQLiteDeclaredProcOnlyHook(t *testing.T) {
	dir := t.TempDir()
	g := New(procDeclaredConfig(), dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}
	assertGeneratedGoParses(t, dir)

	create := readResourceFile(t, dir, "user", "create.go")
	for _, want := range []string{
		`hooks "`,
		`procs "`,
		"RETURNING id",
		`procs.Exec(db, "sp_archive_user", scope.ID)`,
	} {
		if !strings.Contains(create, want) {
			t.Errorf("create.go missing %q\n--- generated:\n%s", want, create)
		}
	}
}
