package plugin

import (
	"encoding/json"
	"testing"
)

func TestPanelBuilder(t *testing.T) {
	p := NewPanel()
	if err := p.AddResource(Resource{Name: "A"}); err != nil {
		t.Fatalf("AddResource: %v", err)
	}
	if err := p.AddResource(Resource{Name: "A"}); err == nil {
		t.Fatal("expected duplicate resource error")
	}
	if err := p.AddResource(Resource{}); err == nil {
		t.Fatal("expected error for unnamed resource")
	}

	if err := p.AddPage(Page{Name: "P"}); err != nil {
		t.Fatalf("AddPage: %v", err)
	}
	if err := p.AddPage(Page{Name: "P"}); err == nil {
		t.Fatal("expected duplicate page error")
	}
	if err := p.AddPage(Page{}); err == nil {
		t.Fatal("expected error for unnamed page")
	}

	p.AddNavigationGroup(NavigationGroup{Group: "G"})
	p.AddSQLFile("queries/q.sql", "SELECT 1;")
	p.AddSQLFile("migrations/s.sql", "CREATE TABLE t (id INTEGER);")

	if err := p.AddHookToResource("A", "create", "after", Hook{SQL: "SELECT 1"}); err != nil {
		t.Fatalf("AddHookToResource: %v", err)
	}
	if err := p.AddHookToResource("A", "create", "sideways", Hook{SQL: "SELECT 1"}); err == nil {
		t.Fatal("expected error for invalid when")
	}

	if err := p.AddHookSource("audit_hooks.go", "package hooks\n"); err != nil {
		t.Fatalf("AddHookSource: %v", err)
	}
	if err := p.AddHookSource("sub/hooks.go", "x"); err == nil {
		t.Fatal("expected error for hook source with a directory")
	}
	if err := p.AddHookSource("hooks.go", "x"); err == nil {
		t.Fatal("expected error for reserved hook source name")
	}
	if err := p.AddHookSource("hooks.txt", "x"); err == nil {
		t.Fatal("expected error for non-go hook source")
	}

	m := p.Manifest()
	if len(m.Resources) != 1 || len(m.Pages) != 1 || len(m.Navigation) != 1 {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.Pages[0].Path != "/P" {
		t.Fatalf("expected default page path /P, got %q", m.Pages[0].Path)
	}
	if len(m.SQLFiles) != 2 || len(m.HookAttachments) != 1 || len(m.HookSources) != 1 {
		t.Fatalf("unexpected manifest extras: %+v", m)
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	p := NewPanel()
	p.AddResource(Resource{
		Name:  "AuditLog",
		Label: "Audit Logs",
		List: &ListConfig{
			Query:      "ListAuditLogs",
			CountQuery: "CountAuditLogs",
			Columns:    []Column{{Name: "id", Label: "ID", Type: "integer", Sortable: true}},
		},
		Form: &FormConfig{
			Create: &FormAction{
				Query:  "CreateAuditLog",
				Fields: []Field{{Name: "message", Type: "text", Required: true}},
			},
		},
		Actions: []Action{{Name: "prune", Query: "DELETE FROM audit_log WHERE created_at < $1"}},
	})
	p.AddHookToResource("AuditLog", "create", "after", Hook{Name: "h", SQL: "SELECT 1"})
	p.AddHookSource("audit_hooks.go", "package hooks\nfunc LogCustomerCreated(ctx context.Context, db *sql.DB, s Scope) error { return nil }\n")

	// The manifest must round-trip through JSON using Go field names, which is
	// how the loader decodes the plugin subprocess output.
	data, err := json.Marshal(p.Manifest())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Manifest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Resources) != 1 || out.Resources[0].Name != "AuditLog" {
		t.Fatalf("round-trip resource mismatch: %+v", out.Resources)
	}
	if out.Resources[0].List.Query != "ListAuditLogs" {
		t.Fatalf("round-trip list mismatch: %+v", out.Resources[0].List)
	}
	if out.Resources[0].Form.Create.Query != "CreateAuditLog" {
		t.Fatalf("round-trip form mismatch: %+v", out.Resources[0].Form)
	}
	if len(out.Resources[0].Actions) != 1 || out.Resources[0].Actions[0].Name != "prune" {
		t.Fatalf("round-trip actions mismatch: %+v", out.Resources[0].Actions)
	}
	if len(out.HookAttachments) != 1 || out.HookAttachments[0].Hook.SQL != "SELECT 1" {
		t.Fatalf("round-trip hook mismatch: %+v", out.HookAttachments)
	}
	if len(out.HookSources) != 1 || out.HookSources["audit_hooks.go"] == "" {
		t.Fatalf("round-trip hook source mismatch: %+v", out.HookSources)
	}
}

func TestValidateSQLFileName(t *testing.T) {
	for _, ok := range []string{"queries/a.sql", "migrations/a.sql"} {
		if err := ValidateSQLFileName(ok); err != nil {
			t.Errorf("%s should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"a.sql", "schema.sql", "queries/a.txt"} {
		if err := ValidateSQLFileName(bad); err == nil {
			t.Errorf("%s should be invalid", bad)
		}
	}
}
