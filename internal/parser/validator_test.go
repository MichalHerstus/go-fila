package parser

import (
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/types"
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
	} else if !strings.Contains(err.Error(), "exactly one of fn, sql or proc") {
		t.Fatalf("expected fn/sql/proc error, got: %v", err)
	}
}

func TestParseHookRejectsBothFnAndSQL(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"`,
		`sql: "SELECT 1"
              fn: Notify`, 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a hook has both fn and sql")
	} else if !strings.Contains(err.Error(), "exactly one of fn, sql or proc") {
		t.Fatalf("expected fn/sql/proc error, got: %v", err)
	}
}

func TestParseProcHookValid(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"`,
		`proc: sp_archive_user`, 1)
	cfg, err := Parse([]byte(bad))
	if err != nil {
		t.Fatalf("expected proc hook to parse, got: %v", err)
	}
	after := cfg.Resources[0].Form.Create.Hooks.After
	if len(after) != 1 || after[0].Proc != "sp_archive_user" || after[0].SQL != "" {
		t.Fatalf("unexpected proc hook: %+v", after)
	}
}

func TestParseHookRejectsFnAndProc(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"`,
		`proc: sp_archive_user
              fn: Notify`, 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a hook has both fn and proc")
	} else if !strings.Contains(err.Error(), "exactly one of fn, sql or proc") {
		t.Fatalf("expected fn/sql/proc error, got: %v", err)
	}
}

func TestParseActionRejectsQueryAndProc(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`query: "UPDATE users SET status = 'inactive' WHERE id = $1"`,
		`query: "UPDATE users SET status = 'inactive' WHERE id = $1"
        proc: sp_deactivate`, 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when an action has both query and proc")
	} else if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got: %v", err)
	}
}

func TestParseActionProcValid(t *testing.T) {
	bad := strings.Replace(hooksYAML,
		`query: "UPDATE users SET status = 'inactive' WHERE id = $1"`,
		`proc: sp_deactivate`, 1)
	cfg, err := Parse([]byte(bad))
	if err != nil {
		t.Fatalf("expected proc action to parse, got: %v", err)
	}
	a := cfg.Resources[0].Actions[0]
	if a.Proc != "sp_deactivate" || a.Query != "" {
		t.Fatalf("unexpected proc action: %+v", a)
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

const pluginsYAML = `
version: "1"
panel:
  name: Admin
  path: /admin
resources:
  - name: User
plugins:
  - name: audit
    source: ./plugins/audit
    config:
      retention_days: 90
`

func TestParsePluginsValid(t *testing.T) {
	cfg, err := Parse([]byte(pluginsYAML))
	if err != nil {
		t.Fatalf("expected valid config with plugins, got error: %v", err)
	}
	if len(cfg.Plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(cfg.Plugins))
	}
	if cfg.Plugins[0].Name != "audit" || cfg.Plugins[0].Source != "./plugins/audit" {
		t.Fatalf("unexpected plugin: %+v", cfg.Plugins[0])
	}
	if cfg.Plugins[0].Config["retention_days"] != 90 {
		t.Fatalf("unexpected plugin config: %+v", cfg.Plugins[0].Config)
	}
}

func TestParsePluginRequiresName(t *testing.T) {
	bad := strings.Replace(pluginsYAML, "name: audit", "name: ''", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a plugin has no name")
	} else if !strings.Contains(err.Error(), "plugins[0].name is required") {
		t.Fatalf("expected plugin name error, got: %v", err)
	}
}

func TestParsePluginRequiresSource(t *testing.T) {
	bad := strings.Replace(pluginsYAML, "source: ./plugins/audit", "source: ''", 1)
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error when a plugin has no source")
	} else if !strings.Contains(err.Error(), "plugins[0].source is required") {
		t.Fatalf("expected plugin source error, got: %v", err)
	}
}

func TestParsePluginRejectsDuplicateNames(t *testing.T) {
	bad := pluginsYAML + "\n  - name: audit\n    source: ./other\n"
	if _, err := Parse([]byte(bad)); err == nil {
		t.Fatal("expected error for duplicate plugin names")
	} else if !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate-name error, got: %v", err)
	}
}

// TestValidateAllReportsEveryProblem verifies ValidateAll collects all
// structural problems while Validate still returns only the first.
func TestValidateAllReportsEveryProblem(t *testing.T) {
	cfg := &types.Config{
		Version:   "1",
		Panel:     types.Panel{Name: "Admin", Path: "/admin"},
		Resources: []types.Resource{{Name: "User"}},
	}
	// Break it in several ways at once.
	cfg.Version = ""
	cfg.Panel.Name = ""
	cfg.Panel.Path = ""
	cfg.Plugins = []types.PluginConfig{{}, {}}

	errs := ValidateAll(cfg)
	want := []string{
		"version is required",
		"panel.name is required",
		"panel.path is required",
		"plugins[0].name is required",
		"plugins[0].source is required",
		"plugins[1].name is required",
		"plugins[1].source is required",
		`plugins[1].name "" is duplicated`,
	}
	if len(errs) != len(want) {
		t.Fatalf("ValidateAll returned %d errors, want %d: %v", len(errs), len(want), errs)
	}
	for i, w := range want {
		if errs[i].Error() != w {
			t.Errorf("errs[%d] = %q, want %q", i, errs[i].Error(), w)
		}
	}
	// Validate keeps the old single-first-error contract used by Parse/save.
	if got := Validate(cfg); got == nil || got.Error() != "version is required" {
		t.Errorf("Validate should return the first error, got %v", got)
	}
}

const auditYAML = `
version: "1"
panel:
  name: Admin
  path: /admin
resources:
  - name: User
    list:
      columns:
        - name: name
audit:
  enabled: true
  table: custom_audit
  include_values: true
  exclude_resources: [User]
`

func TestParseAuditValid(t *testing.T) {
	cfg, err := Parse([]byte(auditYAML))
	if err != nil {
		t.Fatalf("expected valid config with audit, got error: %v", err)
	}
	if cfg.Audit == nil || !cfg.Audit.Enabled {
		t.Fatal("audit block must be parsed")
	}
	if cfg.Audit.Table != "custom_audit" {
		t.Errorf("audit.table = %q, want custom_audit", cfg.Audit.Table)
	}
	if !cfg.Audit.IncludeValues {
		t.Error("audit.include_values must be true")
	}
	if len(cfg.Audit.ExcludeResources) != 1 || cfg.Audit.ExcludeResources[0] != "User" {
		t.Errorf("audit.exclude_resources = %v, want [User]", cfg.Audit.ExcludeResources)
	}
}

func TestParseAuditDefaultsTable(t *testing.T) {
	cfg, err := Parse([]byte(strings.ReplaceAll(auditYAML, "table: custom_audit", "")))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Audit.Table != "audit_log" {
		t.Errorf("default audit.table = %q, want audit_log", cfg.Audit.Table)
	}
}

func TestParseAuditRejectsUnknownExcludedResource(t *testing.T) {
	yaml := strings.ReplaceAll(auditYAML, "exclude_resources: [User]", "exclude_resources: [Nope]")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("expected error for unknown audit.exclude_resources resource")
	}
}
