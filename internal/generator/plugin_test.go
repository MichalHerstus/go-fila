package generator

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-fila/go-fila/internal/types"
	pluginapi "github.com/go-fila/go-fila/pkg/plugin"
)

// pluginBaseConfig returns a minimal config with a Customer resource that has
// a delete form action, used to test hook-attachment merge.
func pluginBaseConfig() *types.Config {
	return &types.Config{
		Version: "1",
		Panel:   types.Panel{ID: "admin", Path: "/admin", Name: "Admin"},
		Resources: []types.Resource{
			{
				Name:  "Customer",
				Label: "Customer",
				Form: &types.FormConfig{
					Delete: &types.FormAction{Query: "DeleteCustomer"},
				},
			},
		},
	}
}

// auditManifest returns a plugin manifest exercising every merge path.
func auditManifest() pluginapi.Manifest {
	return pluginapi.Manifest{
		Resources: []pluginapi.Resource{
			{Name: "AuditLog", Label: "Audit Logs", Table: "audit_log",
				List: &pluginapi.ListConfig{
					Query: "ListAuditLogs", CountQuery: "CountAuditLogs",
					Columns: []pluginapi.Column{{Name: "id", Label: "ID", Type: "integer"}},
				}},
		},
		Pages: []pluginapi.Page{
			{Name: "AuditOverview", Widgets: []pluginapi.Widget{
				{Type: "stat", Label: "Entries", Query: "SELECT COUNT(*) FROM audit_log"},
			}},
		},
		Navigation: []pluginapi.NavigationGroup{
			{Group: "Audit", Icon: "clock", Sort: 90, Items: []pluginapi.NavigationItem{
				{Resource: "AuditLog"},
			}},
		},
		HookAttachments: []pluginapi.HookAttachment{
			{Resource: "Customer", Action: "delete", When: "after",
				Hook: pluginapi.Hook{Name: "audit_customer_delete", SQL: "INSERT INTO audit_log (record_id) VALUES ($1)"}},
		},
		SQLFiles: map[string]string{
			"migrations/audit_schema.sql": "CREATE TABLE audit_log (id INTEGER PRIMARY KEY);",
			"queries/audit.sql":           "-- name: ListAuditLogs :many\nSELECT * FROM audit_log;",
		},
	}
}

func TestMergeManifest(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sql/queries"), 0755)
	g := New(pluginBaseConfig(), dir)
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, auditManifest()); err != nil {
		t.Fatalf("mergeManifest: %v", err)
	}

	cfg := g.Config
	if len(cfg.Resources) != 2 || cfg.Resources[1].Name != "AuditLog" {
		t.Fatalf("expected AuditLog appended, got %+v", cfg.Resources)
	}
	if len(cfg.Pages) != 1 || cfg.Pages[0].Name != "AuditOverview" {
		t.Fatalf("expected AuditOverview page appended, got %+v", cfg.Pages)
	}
	if len(cfg.Navigation) != 1 || cfg.Navigation[0].Group != "Audit" {
		t.Fatalf("expected Audit nav group appended, got %+v", cfg.Navigation)
	}

	cust := cfg.Resources[0]
	if cust.Form.Delete.Hooks == nil || len(cust.Form.Delete.Hooks.After) != 1 {
		t.Fatalf("expected after-delete hook attached, got %+v", cust.Form.Delete.Hooks)
	}
	if got := cust.Form.Delete.Hooks.After[0].SQL; got != "INSERT INTO audit_log (record_id) VALUES ($1)" {
		t.Fatalf("unexpected hook SQL: %s", got)
	}

	// SQL files written.
	for _, name := range []string{"migrations/audit_schema.sql", "queries/audit.sql"} {
		if _, err := os.Stat(filepath.Join(dir, "sql", name)); err != nil {
			t.Errorf("sql file %s not written: %v", name, err)
		}
	}
}

func TestMergeManifestSQLNoOverwrite(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "sql", "queries", "audit.sql")
	os.MkdirAll(filepath.Dir(dst), 0755)
	os.WriteFile(dst, []byte("existing content"), 0644)

	g := New(pluginBaseConfig(), dir)
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, auditManifest()); err != nil {
		t.Fatalf("mergeManifest: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "existing content" {
		t.Fatal("existing SQL file was overwritten by the plugin")
	}
}

func TestMergeManifestMissingHookTarget(t *testing.T) {
	g := New(pluginBaseConfig(), t.TempDir())
	m := auditManifest()
	m.HookAttachments[0].Resource = "Missing"
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, m); err == nil {
		t.Fatal("expected error for missing hook target resource")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got: %v", err)
	}
}

func TestMergeManifestMissingAction(t *testing.T) {
	g := New(pluginBaseConfig(), t.TempDir())
	m := auditManifest()
	m.HookAttachments[0].Action = "create"
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, m); err == nil {
		t.Fatal("expected error for missing hook target action")
	}
}

func TestMergeManifestRejectsFnHook(t *testing.T) {
	g := New(pluginBaseConfig(), t.TempDir())
	m := auditManifest()
	m.HookAttachments[0].Hook = pluginapi.Hook{Name: "h", Fn: "MyHook"}
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, m); err == nil {
		t.Fatal("expected error for fn hook from plugin")
	} else if !strings.Contains(err.Error(), "M5") {
		t.Fatalf("expected M5 rejection message, got: %v", err)
	}
}

func TestMergeManifestRejectsDuplicateResource(t *testing.T) {
	cfg := pluginBaseConfig()
	cfg.Resources = append(cfg.Resources, types.Resource{Name: "AuditLog"})
	g := New(cfg, t.TempDir())
	m := auditManifest()
	m.HookAttachments = nil
	if err := g.mergeManifest(types.PluginConfig{Name: "audit"}, m); err == nil {
		t.Fatal("expected error for duplicate resource name")
	}
}

// skipIfNoGo skips the test when the go toolchain is unavailable.
func skipIfNoGo(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
}

// writeTinyPlugin writes a compilable plugin module into dir and returns its
// source directory. The plugin registers a single resource named "PlugRes".
func writeTinyPlugin(t *testing.T, dir string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, "plug")
	os.MkdirAll(pluginDir, 0755)

	gomod := "module github.com/example/testplugin\n\ngo 1.26.3\n\nrequire github.com/go-fila/go-fila v0.0.0\n"
	if err := os.WriteFile(filepath.Join(pluginDir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	src := `package plug

import (
    plugin "github.com/go-fila/go-fila/pkg/plugin"
)

type p struct{}

func New() plugin.Plugin { return &p{} }
func (p *p) ID() string  { return "plug" }
func (p *p) Register(pb *plugin.Panel) error {
    return pb.AddResource(plugin.Resource{
        Name:  "PlugRes",
        Label: "Plug Res",
        List:  &plugin.ListConfig{Columns: []plugin.Column{{Name: "id", Type: "integer"}}},
    })
}
func (p *p) Boot(pb *plugin.Panel) error { return nil }
`
	if err := os.WriteFile(filepath.Join(pluginDir, "plug.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}
	return pluginDir
}

func TestLoadPluginsIntegration(t *testing.T) {
	skipIfNoGo(t)

	root := t.TempDir()
	pluginDir := writeTinyPlugin(t, root)

	cfg := pluginBaseConfig()
	cfg.Plugins = []types.PluginConfig{{Name: "testplug", Source: pluginDir}}
	g := New(cfg, filepath.Join(root, "out"))
	g.OutDir = filepath.Join(root, "out")
	os.MkdirAll(g.OutDir, 0755)

	if err := g.loadPlugins(); err != nil {
		t.Fatalf("loadPlugins: %v", err)
	}
	if len(cfg.Resources) != 2 || cfg.Resources[1].Name != "PlugRes" {
		t.Fatalf("expected plugin resource merged, got %+v", cfg.Resources)
	}
}

func TestLoadAuditPluginIntegration(t *testing.T) {
	skipIfNoGo(t)

	root := t.TempDir()
	cfg := pluginBaseConfig()
	auditSource, err := filepath.Abs("../../examples/plugins/audit")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if _, err := os.Stat(auditSource); err != nil {
		t.Skipf("audit plugin example not found at %s", auditSource)
	}

	cfg.Plugins = []types.PluginConfig{{
		Name:   "audit",
		Source: auditSource,
		Config: map[string]any{"table": "audit_log", "retention_days": 30},
	}}

	outDir := filepath.Join(root, "out")
	g := New(cfg, outDir)
	if err := g.Generate(); err != nil {
		t.Fatalf("Generate with audit plugin: %v", err)
	}

	if len(cfg.Resources) != 2 || cfg.Resources[1].Name != "AuditLog" {
		t.Fatalf("expected AuditLog resource merged, got %+v", cfg.Resources)
	}
	if len(cfg.Pages) != 1 || cfg.Pages[0].Name != "AuditOverview" {
		t.Fatalf("expected AuditOverview page merged, got %+v", cfg.Pages)
	}
	if len(cfg.Navigation) != 1 || cfg.Navigation[0].Group != "Audit" {
		t.Fatalf("expected Audit navigation group merged, got %+v", cfg.Navigation)
	}

	// Verify SQL files written
	for _, name := range []string{"migrations/audit_schema.sql", "queries/audit.sql"} {
		if _, err := os.Stat(filepath.Join(outDir, "sql", name)); err != nil {
			t.Errorf("sql file %s not written: %v", name, err)
		}
	}
}

func TestLoadPluginsSkip(t *testing.T) {
	cfg := pluginBaseConfig()
	cfg.Plugins = []types.PluginConfig{{Name: "audit", Source: "./does-not-exist"}}
	g := New(cfg, t.TempDir())
	g.SkipPlugins = true
	if err := g.loadPlugins(); err != nil {
		t.Fatalf("loadPlugins with SkipPlugins should be a no-op, got: %v", err)
	}
	if len(cfg.Resources) != 1 {
		t.Fatal("skip-plugins must not merge anything")
	}
}

func TestResolvePluginSource(t *testing.T) {
	dir := t.TempDir()
	pluginDir := writeTinyPlugin(t, dir)

	mod, local, err := resolvePluginSource(pluginDir)
	if err != nil {
		t.Fatalf("resolvePluginSource: %v", err)
	}
	if mod != "github.com/example/testplugin" {
		t.Fatalf("expected local module path, got %s", mod)
	}
	if local == "" {
		t.Fatal("expected a local dir for a relative source")
	}

	mod2, local2, err := resolvePluginSource("github.com/go-fila/plugin-audit")
	if err != nil {
		t.Fatalf("resolvePluginSource: %v", err)
	}
	if mod2 != "github.com/go-fila/plugin-audit" || local2 != "" {
		t.Fatalf("expected module-path resolution, got %q %q", mod2, local2)
	}

	if _, _, err := resolvePluginSource(""); err == nil {
		t.Fatal("expected error for empty source")
	}
}

func TestGenerateSQLOnlyHooks(t *testing.T) {
	cfg := hookConfig()
	// Turn every fn hook into a sql hook so no stubs are emitted.
	for _, list := range [][]types.Hook{
		cfg.Resources[0].Form.Create.Hooks.Before,
		cfg.Resources[0].Actions[0].Hooks.Before,
	} {
		for i := range list {
			list[i].Fn = ""
			list[i].SQL = "SELECT 1"
		}
	}

	dir := t.TempDir()
	g := New(cfg, dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}

	create, err := os.ReadFile(filepath.Join(dir, "internal/panel/resources/user", "create.go"))
	if err != nil {
		t.Fatalf("read create.go: %v", err)
	}
	if !strings.Contains(string(create), `hooks.Scope{`) {
		t.Error("sql-only hook must still emit hooks.Scope")
	}

	hooksGo, err := os.ReadFile(filepath.Join(dir, "internal/hooks", "hooks.go"))
	if err != nil {
		t.Fatalf("read hooks.go: %v", err)
	}
	hooksStr := string(hooksGo)
	if !strings.Contains(hooksStr, "type Scope struct") {
		t.Error("hooks.go must define Scope for sql-only hooks")
	}
	if strings.Contains(hooksStr, "func ValidateUserDomain") {
		t.Error("no fn stubs should be emitted for sql-only hooks")
	}
	if strings.Contains(hooksStr, "database/sql") {
		t.Error("hooks.go must not import database/sql without fn stubs")
	}
}

func TestGenerateNoPluginsRegression(t *testing.T) {
	dir := t.TempDir()
	g := New(hookConfig(), dir)
	if err := g.Generate(); err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{
		"internal/panel/router.go",
		"internal/panel/resources/user/list.go",
		"internal/hooks/hooks.go",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing expected file %s: %v", want, err)
		}
	}
}
