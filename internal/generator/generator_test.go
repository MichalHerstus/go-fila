package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/types"
)

// hookConfig returns a minimal config exercising hooks on create, delete and a
// custom action, plus a fn hook and a sql hook.
func hookConfig() *types.Config {
	return &types.Config{
		Version: "1",
		Panel:   types.Panel{ID: "admin", Path: "/admin", Name: "Admin"},
		Resources: []types.Resource{
			{
				Name:  "User",
				Label: "User",
				List: &types.ListConfig{
					Columns: []types.Column{{Name: "name", Label: "Name"}},
				},
				Form: &types.FormConfig{
					Create: &types.FormAction{
						Fields: []types.Field{
							{Name: "name", Type: "text"},
							{Name: "email", Type: "email"},
						},
						Hooks: &types.Hooks{
							Before: []types.Hook{{Name: "validate_domain", Fn: "ValidateUserDomain"}},
							After:  []types.Hook{{Name: "notify", SQL: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"}},
						},
					},
					Delete: &types.FormAction{
						Hooks: &types.Hooks{
							After: []types.Hook{{Name: "audit_delete", SQL: "INSERT INTO audit_log (action) VALUES ('delete')"}},
						},
					},
				},
				Actions: []types.Action{
					{
						Name:  "deactivate",
						Query: "UPDATE users SET status = 'inactive' WHERE id = $1",
						Hooks: &types.Hooks{
							Before: []types.Hook{{Name: "log_deactivate", Fn: "LogDeactivate"}},
						},
					},
				},
			},
		},
	}
}

func TestGenerateHooks(t *testing.T) {
	dir := t.TempDir()
	g := New(hookConfig(), dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}

	create, err := os.ReadFile(filepath.Join(dir, "internal/panel/resources/user", "create.go"))
	if err != nil {
		t.Fatalf("read create.go: %v", err)
	}
	createStr := string(create)
	for _, want := range []string{
		`RETURNING id`,
		`hooks.Scope{`,
		`Action: "create"`,
		`hooks.ValidateUserDomain(r.Context(), db, scope)`,
		`db.QueryRowContext(r.Context(), query+" RETURNING id", vals...)`,
		`scope.ID = newID`,
		`db.ExecContext(r.Context(), "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')", scope.ID)`,
	} {
		if !strings.Contains(createStr, want) {
			t.Errorf("create.go missing %q", want)
		}
	}
	for _, notWant := range []string{`db.ExecContext(r.Context(), query, vals...)`} {
		if strings.Contains(createStr, notWant) {
			t.Errorf("create.go should not contain %q when hooks are declared", notWant)
		}
	}

	del, err := os.ReadFile(filepath.Join(dir, "internal/panel/resources/user", "delete.go"))
	if err != nil {
		t.Fatalf("read delete.go: %v", err)
	}
	delStr := string(del)
	for _, want := range []string{
		`hooks "`,
		`Action: "delete"`,
		`db.ExecContext(r.Context(), "INSERT INTO audit_log (action) VALUES ('delete')", scope.ID)`,
	} {
		if !strings.Contains(delStr, want) {
			t.Errorf("delete.go missing %q", want)
		}
	}

	actions, err := os.ReadFile(filepath.Join(dir, "internal/panel/resources/user", "actions.go"))
	if err != nil {
		t.Fatalf("read actions.go: %v", err)
	}
	if !strings.Contains(string(actions), "hooks.LogDeactivate(r.Context(), db, scope)") {
		t.Error("actions.go missing fn hook call")
	}

	hooksGo, err := os.ReadFile(filepath.Join(dir, "internal/hooks", "hooks.go"))
	if err != nil {
		t.Fatalf("read hooks.go: %v", err)
	}
	hooksStr := string(hooksGo)
	for _, want := range []string{
		`type Scope struct`,
		`func ValidateUserDomain(ctx context.Context, db *sql.DB, s Scope) error { return nil }`,
		`func LogDeactivate(ctx context.Context, db *sql.DB, s Scope) error { return nil }`,
	} {
		if !strings.Contains(hooksStr, want) {
			t.Errorf("hooks.go missing %q", want)
		}
	}
}

func TestGenerateNoHooksRegression(t *testing.T) {
	cfg := hookConfig()
	cfg.Resources[0].Form.Create.Hooks = nil
	cfg.Resources[0].Form.Delete.Hooks = nil
	cfg.Resources[0].Actions[0].Hooks = nil

	dir := t.TempDir()
	g := New(cfg, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}

	create, err := os.ReadFile(filepath.Join(dir, "internal/panel/resources/user", "create.go"))
	if err != nil {
		t.Fatalf("read create.go: %v", err)
	}
	createStr := string(create)
	if strings.Contains(createStr, "RETURNING") {
		t.Error("hookless create.go must not use RETURNING")
	}
	if !strings.Contains(createStr, "db.ExecContext(r.Context(), query, vals...)") {
		t.Error("hookless create.go should keep ExecContext")
	}

	if _, err := os.Stat(filepath.Join(dir, "internal/hooks", "hooks.go")); !os.IsNotExist(err) {
		t.Error("hooks.go should not be generated without fn hooks")
	}
}
