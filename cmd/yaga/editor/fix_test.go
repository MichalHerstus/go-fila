// fix_test.go — the TUI editor's Fix button: repairConfig/autoFix.
package editor

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/MichalHerstus/yaga/internal/types"
	"github.com/gdamore/tcell/v2"
	"gopkg.in/yaml.v3"
)

const emptyFilterYAML = `version: "1.0"
panel:
  id: admin
  path: /admin
  name: My Admin
auth:
  table: users
resources:
  - name: Category
    list:
      filter:
        label: ""
        where: ""
        params: []
`

// fixCfg writes a YAML config to a temp file and parses it WITHOUT validation
// (ParseFile would reject a config broken on purpose).
func fixCfg(t *testing.T, content string) (*types.Config, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "yaga.yaml")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	var cfg types.Config
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg, p
}

func TestRepairConfigFixesEmptyFilter(t *testing.T) {
	cfg, p := fixCfg(t, emptyFilterYAML)
	e := New(cfg, p)
	fixed, remaining, err := e.repairConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining: %v", remaining)
	}
	if want := []string{"resources/Category/list.filter"}; !reflect.DeepEqual(fixed, want) {
		t.Fatalf("fixed = %v, want %v", fixed, want)
	}
	if e.cfg.Resources[0].List.Filter != nil {
		t.Fatal("in-memory filter must be dropped")
	}
	if !e.saved {
		t.Fatal("saved flag must be set")
	}
	if e.modified {
		t.Fatal("modified must be cleared after a fix write")
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "filter") {
		t.Fatalf("write did not drop the filter:\n%s", data)
	}
	bak, err := os.ReadFile(p + ".bak")
	if err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !strings.Contains(string(bak), "filter") {
		t.Fatal("backup must hold the pre-fix state")
	}
}

func TestRepairConfigNothingToFix(t *testing.T) {
	validYAML := `version: "1.0"
panel:
    id: admin
    path: /admin
    name: My Admin
auth:
  table: users
resources:
  - name: Category
`
	cfg, p := fixCfg(t, validYAML)
	e := New(cfg, p)
	fixed, remaining, err := e.repairConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixed) != 0 || len(remaining) != 0 {
		t.Fatalf("fixed=%v remaining=%v", fixed, remaining)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup expected for a no-op repair")
	}
}

func TestRepairConfigLeavesUnfixable(t *testing.T) {
	onlyLabelYAML := `version: "1.0"
panel:
    id: admin
    path: /admin
    name: My Admin
auth:
  table: users
resources:
  - name: Category
    list:
      filter:
        label: "Search"
`
	cfg, p := fixCfg(t, onlyLabelYAML)
	e := New(cfg, p)
	fixed, remaining, err := e.repairConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixed) != 0 {
		t.Fatalf("fixed = %v, want none", fixed)
	}
	if len(remaining) != 1 || !strings.Contains(remaining[0].Error(), "where is required") {
		t.Fatalf("remaining = %v", remaining)
	}
	if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
		t.Fatal("no backup expected when nothing was written")
	}
}

// TestFixButtonWiring drives the real event loop on a simulation screen:
// open Validate (Ctrl+V), press the Fix shortcut (Ctrl+F), then quit. The
// empty list.filter must be gone from the file and a backup written.
func TestFixButtonWiring(t *testing.T) {
	screen := newSimScreen(t)
	cfg, p := fixCfg(t, emptyFilterYAML)
	e := New(cfg, p)
	e.SetScreen(screen)

	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlV, 0, tcell.ModNone) // Validate page
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlF, 0, tcell.ModNone) // Fix
		time.Sleep(250 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone) // quit
	}()

	if _, err := e.Run(); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "filter") {
		t.Fatalf("Fix button did not remove the filter:\n%s", data)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if e.cfg.Resources[0].List.Filter != nil {
		t.Fatal("in-memory filter not dropped")
	}
}
