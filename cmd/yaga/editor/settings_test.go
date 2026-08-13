package editor

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MichalHerstus/yaga/internal/parser"
	"github.com/MichalHerstus/yaga/internal/types"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// TestFeaturePagesNoPanic builds the new-feature editors (audit, procedures,
// plugins, auth rate limit) with and without pre-existing blocks, ensuring the
// lazy-allocation closures never nil-panic.
func TestFeaturePagesNoPanic(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	if p := e.auditPage(); p == nil {
		t.Error("auditPage: nil primitive")
	}
	if p := e.proceduresPage(); p == nil {
		t.Error("proceduresPage: nil primitive")
	}
	if p := e.pluginsPage(); p == nil {
		t.Error("pluginsPage: nil primitive")
	}
	if p := e.authPage(); p == nil {
		t.Error("authPage: nil primitive")
	}

	cfg := testConfig()
	cfg.Procedures = []types.Procedure{{Name: "sp_archive_user", Description: "Archive a user", SQL: "UPDATE users SET active = 0 WHERE id = $1;"}}
	cfg.Plugins = []types.PluginConfig{{Name: "audit", Source: "./plugins/audit"}}
	cfg.Audit = &types.AuditConfig{Enabled: true, Table: "audit_log"}
	cfg.Auth.Login.RateLimit = &types.LoginRateLimit{MaxAttempts: 5, WindowSeconds: 300}
	e = New(cfg, "testdata/yaga.yaml")

	if p := e.auditPage(); p == nil {
		t.Error("auditPage (populated): nil primitive")
	}
	if p := e.proceduresPage(); p == nil {
		t.Error("proceduresPage (populated): nil primitive")
	} else if l, ok := p.(*tview.List); !ok || l.GetItemCount() != 1 {
		t.Errorf("proceduresPage (populated): want 1 row, got %T/%d", p, listCount(l))
	}
	if p := e.procedurePage(0); p == nil {
		t.Error("procedurePage: nil primitive")
	}
	if p := e.pluginsPage(); p == nil {
		t.Error("pluginsPage (populated): nil primitive")
	} else if l, ok := p.(*tview.List); !ok || l.GetItemCount() != 1 {
		t.Errorf("pluginsPage (populated): want 1 row, got %T/%d", p, listCount(l))
	}
	if p := e.pluginPage(0); p == nil {
		t.Error("pluginPage: nil primitive")
	}
	if p := e.authPage(); p == nil {
		t.Error("authPage (rate limit): nil primitive")
	}
}

func listCount(l *tview.List) int {
	if l == nil {
		return -1
	}
	return l.GetItemCount()
}

// TestNavNewFeaturePaths verifies the cd-navigation resolves the audit,
// procedures and plugins screens (including a procedure item).
func TestNavNewFeaturePaths(t *testing.T) {
	cfg := testConfig()
	cfg.Procedures = []types.Procedure{{Name: "sp_archive_user", SQL: "UPDATE users SET active = 0 WHERE id = $1;"}}
	cfg.Plugins = []types.PluginConfig{{Name: "audit", Source: "./plugins/audit"}}
	e := New(cfg, "testdata/yaga.yaml")

	cases := []struct{ in, want string }{
		{"Audit", "Audit"},
		{"Audit/Excluded Resources", "Audit/Excluded Resources"},
		{"Procedures", "Procedures"},
		{"Procedures/sp_archive_user", "Procedures/sp_archive_user"},
		{"Plugins", "Plugins"},
		{"Plugins/audit", "Plugins/audit"},
	}
	for _, c := range cases {
		tg, err := e.resolvePath(c.in)
		if err != nil {
			t.Fatalf("resolvePath(%q): %v", c.in, err)
		}
		if tg.name != c.want {
			t.Fatalf("resolvePath(%q) = %q, want %q", c.in, tg.name, c.want)
		}
	}
	if _, err := e.resolvePath("Procedures/Nope"); err == nil {
		t.Error("resolvePath(Procedures/Nope) should fail")
	}
}

// TestNewFeatureMenuEntries verifies Audit / Procedures / Plugins are reachable
// from the left navigation (not just via the cd dialog).
func TestNewFeatureMenuEntries(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.app = tview.NewApplication()
	e.buildShell()
	got := map[string]bool{}
	for i := 0; i < e.nav.GetItemCount(); i++ {
		if main, _ := e.nav.GetItemText(i); main != "" {
			got[main] = true
		}
	}
	for _, want := range []string{"Audit", "Procedures", "Plugins"} {
		if !got[want] {
			t.Errorf("%s not present in the editor navigation", want)
		}
	}
}

// TestEditorSavePreservesFeatureBlocks drives the real editor on a config that
// carries audit/procedures/plugins blocks and asserts a Ctrl+S save keeps them
// in the written YAML and still parses (regression guard for the D6 save
// dead-end: the editor must never drop these top-level blocks).
func TestEditorSavePreservesFeatureBlocks(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/yaga.yaml"

	cfg := testConfig()
	cfg.Audit = &types.AuditConfig{
		Enabled:          true,
		Table:            "audit_log",
		IncludeValues:    true,
		Policy:           "admin",
		ExcludeResources: []string{"User"},
	}
	cfg.Procedures = []types.Procedure{{
		Name:        "sp_archive_user",
		Description: "Archive a user",
		SQL:         "UPDATE users SET active = 0 WHERE id = $1;",
	}}
	cfg.Plugins = []types.PluginConfig{{Name: "audit", Source: "./plugins/audit"}}

	screen := newSimScreen(t)
	e := New(cfg, path)
	e.SetScreen(screen)
	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlS, 0, tcell.ModNone) // save
		time.Sleep(200 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit (saved, no confirm)
	}()

	saved, err := e.Run()
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if !saved {
		t.Fatal("expected save")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	for _, want := range []string{
		"audit:", "enabled: true", "audit_log", "include_values: true", "User",
		"procedures:", "sp_archive_user", "Archive a user", "$1",
		"plugins:", "./plugins/audit",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("saved config missing %q", want)
		}
	}
	if _, err := parser.ParseFile(path); err != nil {
		t.Errorf("saved config does not re-parse: %v", err)
	}
}

// TestEditorProceduresScreenViaDialog drives the real event loop: open the cd
// dialog, type a procedure path, Enter to navigate to the new Procedures
// screen, then quit. Verifies the new screen is reachable end-to-end.
func TestEditorProceduresScreenViaDialog(t *testing.T) {
	cfg := testConfig()
	cfg.Procedures = []types.Procedure{{Name: "sp_archive_user", SQL: "UPDATE users SET active = 0 WHERE id = $1;"}}
	screen := newSimScreen(t)
	e := New(cfg, "testdata/yaga.yaml")
	e.SetScreen(screen)

	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlP, 0, tcell.ModNone)
		time.Sleep(150 * time.Millisecond)
		for _, r := range "Procedures/sp_archive_user" {
			screen.InjectKey(tcell.KeyRune, r, tcell.ModNone)
			time.Sleep(5 * time.Millisecond)
		}
		time.Sleep(80 * time.Millisecond)
		screen.InjectKey(tcell.KeyEnter, 0, tcell.ModNone)
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
	}()

	if _, err := e.Run(); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if cur := e.currentPath(); cur != "Procedures/sp_archive_user" {
		t.Fatalf("current path = %q, want Procedures/sp_archive_user", cur)
	}
}

// TestStructuralGoToProcedures verifies an undeclared-procedure validation
// error jumps straight to the Procedures screen (fixing the D6 save dead-end).
func TestStructuralGoToProcedures(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.pages = tview.NewPages()
	e.app = tview.NewApplication()
	e.buildShell()
	gt := e.structuralGoTo(`resources[0] (User) form.create references undeclared procedure "sp_x" - add a matching procedures: entry`)
	if gt == nil {
		t.Fatal("expected goTo for undeclared procedure error")
	}
	gt()
	if cur := e.currentPath(); cur != "Procedures" {
		t.Fatalf("current path = %q, want Procedures", cur)
	}
}

// TestAuditPageAllocatesOnWrite verifies merely viewing the Audit page with no
// audit block does not mutate the config, and that toggling Enabled allocates.
func TestAuditPageAllocatesOnWrite(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	// Snapshot the page state by invoking the builder; the config must stay nil.
	e.auditPage()
	if e.cfg.Audit != nil {
		t.Fatal("auditPage should not allocate cfg.Audit on view")
	}
	// Simulate a write through the same lazy path.
	e.auditCfg().Enabled = true
	if e.cfg.Audit == nil || !e.cfg.Audit.Enabled {
		t.Fatal("auditCfg should allocate on write")
	}
}
