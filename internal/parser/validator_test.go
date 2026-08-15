package parser

import (
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/types"
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

const csvResourceYAML = `
version: "1"
panel:
  name: Admin
  path: /admin
resources:
  - name: User
    list:
      columns:
        - name: name
        - name: email
      export: [name, email]
    form:
      create:
        fields:
          - name: name
          - name: email
    import_csv: true
`

func TestParseCsvImportValid(t *testing.T) {
	cfg, err := Parse([]byte(csvResourceYAML))
	if err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
	if !cfg.Resources[0].ImportCSV {
		t.Error("import_csv must parse true")
	}
	if len(cfg.Resources[0].List.Export) != 2 || cfg.Resources[0].List.Export[0] != "name" {
		t.Errorf("list.export = %v, want [name email]", cfg.Resources[0].List.Export)
	}
}

func TestParseCsvExportRejectsUnknownColumn(t *testing.T) {
	yaml := strings.ReplaceAll(csvResourceYAML, "export: [name, email]", "export: [name, nope]")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("expected error for unknown list.export column")
	}
}

func TestParseCsvImportRequiresCreate(t *testing.T) {
	yaml := strings.ReplaceAll(csvResourceYAML, "form:\n      create:\n        fields:\n          - name: name\n          - name: email\n", "")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("expected error for import_csv without a create form")
	}
}

const proceduresYAML = `
version: "1"
panel:
  name: Admin
  path: /admin
connections:
  default:
    driver: sqlite
    dsn: ./data/app.db
sqlc:
  config: sqlc.yaml
resources:
  - name: User
    form:
      create:
        hooks:
          after:
            - name: archive_create
              proc: sp_archive_user
    actions:
      - name: archive
        proc: sp_archive_user
procedures:
  - name: sp_archive_user
    description: Archive the user
    sql: |
      UPDATE users SET status = 'archived' WHERE id = $1;
      INSERT INTO events (msg) VALUES ('user archived');
`

func TestParseProceduresValid(t *testing.T) {
	cfg, err := Parse([]byte(proceduresYAML))
	if err != nil {
		t.Fatalf("expected valid procedures config, got: %v", err)
	}
	if len(cfg.Procedures) != 1 || cfg.Procedures[0].Name != "sp_archive_user" {
		t.Fatalf("unexpected procedures: %+v", cfg.Procedures)
	}
	if !strings.Contains(cfg.Procedures[0].SQL, "UPDATE users") || !strings.Contains(cfg.Procedures[0].SQL, "INSERT INTO events") {
		t.Fatalf("procedure body not preserved: %+v", cfg.Procedures[0].SQL)
	}
}

func TestParseProceduresRejectsUndeclaredRefOnSqlite(t *testing.T) {
	yaml := strings.ReplaceAll(proceduresYAML, "\nprocedures:", "\n#procedures:")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("expected error for undeclared proc reference on sqlite")
	} else if !strings.Contains(err.Error(), "undeclared procedure") {
		t.Fatalf("expected undeclared-procedure error, got: %v", err)
	}
}

func TestParseProceduresIgnoredOnPostgres(t *testing.T) {
	yaml := strings.ReplaceAll(proceduresYAML, "driver: sqlite", "driver: postgres")
	if _, err := Parse([]byte(yaml)); err != nil {
		t.Fatalf("expected no error for undeclared proc reference on postgres, got: %v", err)
	}
}

func TestParseProceduresRejectsDuplicateNames(t *testing.T) {
	yaml := strings.ReplaceAll(proceduresYAML,
		"  - name: sp_archive_user\n",
		"  - name: sp_archive_user\n  - name: sp_archive_user\n")
	if _, err := Parse([]byte(yaml)); err == nil {
		t.Fatal("expected error for duplicate procedure names")
	} else if !strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("expected duplicate-name error, got: %v", err)
	}
}

func TestValidateClampsColumnCounts(t *testing.T) {
	cfg := &types.Config{
		Version: "1",
		Panel:   types.Panel{Name: "Admin", Path: "/admin"},
		Resources: []types.Resource{
			{Name: "Card", Card: &types.CardConfig{Columns: 30, Rows: 3}},
		},
		Pages: []types.Page{{
			Name: "Dash",
			Widgets: []types.Widget{
				{Type: "stats_grid", Columns: 99, Widgets: []types.Widget{{Type: "stats_grid", Columns: -5}}},
			},
		}},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("Validate must not fail on clamps (warnings only), got: %v", err)
	}
	if cfg.Resources[0].Card.Columns != 12 {
		t.Errorf("card.columns clamped to 12, got %d", cfg.Resources[0].Card.Columns)
	}
	if cfg.Pages[0].Widgets[0].Columns != 12 {
		t.Errorf("stats_grid columns clamped to 12, got %d", cfg.Pages[0].Widgets[0].Columns)
	}
	if cfg.Pages[0].Widgets[0].Widgets[0].Columns != 1 {
		t.Errorf("nested stats_grid columns clamped to 1, got %d", cfg.Pages[0].Widgets[0].Widgets[0].Columns)
	}
}

func TestValidateMaxContentWidthAllowlist(t *testing.T) {
	mkCfg := func(width string) *types.Config {
		return &types.Config{Version: "1", Panel: types.Panel{Name: "A", Path: "/a",
			Layout: types.Layout{MaxContentWidth: width}},
			Resources: []types.Resource{{Name: "User"}}}
	}

	valid := mkCfg("7xl")
	errs := ValidateAll(valid)
	if len(errs) != 0 {
		t.Fatalf("valid max_content_width must produce no findings, got: %v", errs)
	}
	if valid.Panel.Layout.MaxContentWidth != "7xl" {
		t.Errorf("7xl kept, got %q", valid.Panel.Layout.MaxContentWidth)
	}

	bad := mkCfg("9xl")
	errs = ValidateAll(bad)
	if len(errs) != 1 {
		t.Fatalf("unknown max_content_width produces exactly one warning, got %d: %v", len(errs), errs)
	}
	if _, ok := errs[0].(Warning); !ok {
		t.Errorf("expected Warning type, got %T: %v", errs[0], errs[0])
	}
	if bad.Panel.Layout.MaxContentWidth != "none" {
		t.Errorf("unknown max_content_width falls back to none, got %q", bad.Panel.Layout.MaxContentWidth)
	}
}

func TestValidateWarningsNotBlocking(t *testing.T) {
	cfg := &types.Config{Version: "1", Panel: types.Panel{Name: "A", Path: "/a",
		Layout: types.Layout{MaxContentWidth: "bogus"}},
		Resources: []types.Resource{{Name: "R", Card: &types.CardConfig{Columns: 40}}}}
	if err := Validate(cfg); err != nil {
		t.Fatalf("warnings must not fail Validate, got: %v", err)
	}
	cfg2 := &types.Config{Version: "1", Panel: types.Panel{Name: "A", Path: "/a",
		Layout: types.Layout{MaxContentWidth: "bogus"}},
		Resources: []types.Resource{{Name: "R", Card: &types.CardConfig{Columns: 40}}}}
	all := ValidateAll(cfg2)
	if len(all) != 2 {
		t.Fatalf("expected 2 warnings (card columns + max width), got %d: %v", len(all), all)
	}
	for _, w := range all {
		if _, ok := w.(Warning); !ok {
			t.Errorf("expected Warning type, got %T: %v", w, w)
		}
	}
	if cfg2.Resources[0].Card.Columns != 12 || cfg2.Panel.Layout.MaxContentWidth != "none" {
		t.Errorf("clamps applied: columns=%d max_width=%q", cfg2.Resources[0].Card.Columns, cfg2.Panel.Layout.MaxContentWidth)
	}
}

// TestValidateCopiesWarnings ensures `copies:` problems are non-fatal warnings:
// a copies map on a non-picker field, a copy into an unknown field, and a
// self-copy all surface as warnings while the config still validates.
func TestValidateCopiesWarnings(t *testing.T) {
	cfg := &types.Config{
		Version: "1",
		Panel:   types.Panel{ID: "admin", Path: "/admin", Name: "Admin"},
		Resources: []types.Resource{
			{
				Name: "User",
				Form: &types.FormConfig{
					Create: &types.FormAction{Fields: []types.Field{
						{Name: "city", Type: "string"},
						{Name: "role_id", Type: "relation", OptionsValue: "id", OptionsLabel: "name", Copies: map[string]string{"city": "city"}},
					}},
					Update: &types.FormAction{Fields: []types.Field{
						{Name: "city", Type: "string"},
						{Name: "role_id", Type: "relation", OptionsValue: "id", OptionsLabel: "name", Copies: map[string]string{"nope": "city", "role_id": "id"}},
					}},
				},
			},
		},
	}
	errs := ValidateAll(cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("copies warnings must not block Validate: %v", err)
	}
	var warns []string
	for _, e := range errs {
		if _, ok := e.(Warning); ok {
			warns = append(warns, e.Error())
		}
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "copies into itself") {
		t.Errorf("expected self-copy warning, got:\n%s", joined)
	}
	if !strings.Contains(joined, "not a field of the same form") {
		t.Errorf("expected unknown-target warning, got:\n%s", joined)
	}
}

// TestValidateChildren ensures the master-detail children block is validated:
// an unknown child resource and an explicit FK column missing from the child
// schema are errors; a reverse-FK-derived column resolves silently.
func TestValidateChildren(t *testing.T) {
	schema := &types.Schema{Tables: []types.SchemaTable{
		{Name: "orders", PK: "id", Columns: []types.SchemaColumn{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "customer_name", Type: "string"}}},
		{Name: "order_lines", PK: "id", Columns: []types.SchemaColumn{{Name: "id", Type: "integer", PrimaryKey: true}, {Name: "order_id", Type: "integer"}, {Name: "qty", Type: "integer"}},
			ForeignKeys: []types.SchemaFK{{Column: "order_id", ForeignTable: "orders", ForeignColumn: "id", Label: "customer_name"}}},
	}}
	makeCfg := func(children []types.ChildResource) *types.Config {
		return &types.Config{
			Version: "1", Panel: types.Panel{ID: "admin", Path: "/admin", Name: "Admin"},
			Schema: schema,
			Resources: []types.Resource{
				{Name: "Order", Label: "Orders", Table: "orders", Children: children},
				{Name: "OrderLine", Label: "Order Lines", Table: "order_lines"},
			},
		}
	}

	// Derived FK column resolves silently (no errors).
	if errs := ValidateAll(makeCfg([]types.ChildResource{{Name: "Lines", Resource: "OrderLine"}})); len(errs) != 0 {
		t.Fatalf("derived children must validate, got %v", errs)
	}

	// Unknown child resource is an error.
	found := false
	for _, e := range ValidateAll(makeCfg([]types.ChildResource{{Name: "Lines", Resource: "Nope"}})) {
		if strings.Contains(e.Error(), "not a defined resource") {
			found = true
		}
	}
	if !found {
		t.Error("unknown children.resource must be an error")
	}

	// Explicit FK column missing from the child schema is an error.
	found = false
	for _, e := range ValidateAll(makeCfg([]types.ChildResource{{Name: "Lines", Resource: "OrderLine", Column: "bogus"}})) {
		if strings.Contains(e.Error(), "not a column of order_lines") {
			found = true
		}
	}
	if !found {
		t.Error("children column missing from the child table must be an error")
	}
}
