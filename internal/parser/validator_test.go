package parser

import (
	"strings"
	"testing"
)

const hooksYAML = `
version: "1"
panel:
  name: Admin
  path: /admin
sqlc:
  config: sqlc.yaml
resources:
  - name: User
    form:
      create:
        hooks:
          before:
            - name: validate_domain
              fn: ValidateUserDomain
          after:
            - name: notify
              sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"
      delete:
        hooks:
          after:
            - name: audit_delete
              sql: "INSERT INTO audit_log (action) VALUES ('delete')"
    actions:
      - name: deactivate
        query: "UPDATE users SET status = 'inactive' WHERE id = $1"
        hooks:
          before:
            - name: log_deactivate
              fn: LogDeactivate
`

func TestParseHooksValid(t *testing.T) {
	cfg, err := Parse([]byte(hooksYAML))
	if err != nil {
		t.Fatalf("expected valid config with hooks, got error: %v", err)
	}
	if cfg.Resources[0].Form.Create.Hooks == nil {
		t.Fatal("expected create hooks block")
	}
	before := cfg.Resources[0].Form.Create.Hooks.Before
	if len(before) != 1 || before[0].Fn != "ValidateUserDomain" {
		t.Fatalf("unexpected before hooks: %+v", before)
	}
	after := cfg.Resources[0].Form.Create.Hooks.After
	if len(after) != 1 || after[0].SQL == "" {
		t.Fatalf("unexpected after hooks: %+v", after)
	}
	if !cfg.Resources[0].Form.Create.Hooks.HasFn() {
		t.Fatal("expected HasFn to be true")
	}
	if cfg.Resources[0].Actions[0].Hooks == nil {
		t.Fatal("expected action hooks block")
	}
}

func TestParseHookRequiresFnOrSQL(t *testing.T) {
	bad := strings.Replace(hooksYAML, "fn: ValidateUserDomain", "fn: ''", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a hook has neither fn nor sql")
	} else if !strings.Contains(err.Error(), "exactly one of fn or sql") {
		t.Fatalf("expected fn/sql error, got: %v", err)
	}
}

func TestParseHookRejectsBothFnAndSQL(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"`,
		`sql: "SELECT 1"
              fn: Notify`, 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a hook has both fn and sql")
	} else if !strings.Contains(err.Error(), "exactly one of fn or sql") {
		t.Fatalf("expected fn/sql error, got: %v", err)
	}
}

func TestParseHookRequiresName(t *testing.T) {
	bad := strings.Replace(hooksYAML, "name: validate_domain", "name: ''", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a hook has no name")
	} else if !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("expected name error, got: %v", err)
	}
}
