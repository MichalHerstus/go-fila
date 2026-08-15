package editor

import (
	"testing"
	"time"

	"github.com/MichalHerstus/yaga/internal/schema"
	"github.com/MichalHerstus/yaga/internal/types"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// setUsersSchema attaches a minimal users table (id, email) to the config's
// captured `schema:` block so runValidation can resolve the resource's table.
func setUsersSchema(cfg *types.Config) {
	cfg.Schema = &types.Schema{
		Tables: []types.SchemaTable{{
			Name: "users",
			PK:   "id",
			Columns: []types.SchemaColumn{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "string"},
			},
		}},
	}
}

// TestValidatePageBuilds ensures the Validate screen builds without panic both
// on a clean config and on one with schema findings.
func TestValidatePageBuilds(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	if p := e.validatePage(); p == nil {
		t.Error("validatePage returned nil")
	}

	cfg := testConfig()
	setUsersSchema(cfg)
	cfg.Resources[0].List.Columns = append(cfg.Resources[0].List.Columns, types.Column{Name: "role_id"})
	e = New(cfg, "testdata/yaga.yaml")
	if p := e.validatePage(); p == nil {
		t.Error("validatePage returned nil with findings")
	}
}

// TestRunValidationFindsBadColumns verifies that a column missing from the
// captured schema block is reported as a navigable warning for every section
// it is referenced from (list, card), and that invoking goTo lands on the
// right editor page.
func TestRunValidationFindsBadColumns(t *testing.T) {
	cfg := testConfig()
	setUsersSchema(cfg)
	cfg.Resources[0].List.Columns = append(cfg.Resources[0].List.Columns, types.Column{Name: "role_id"})
	cfg.Resources[0].Card = &types.CardConfig{Fields: []types.Field{{Name: "missing_card"}}}
	e := New(cfg, "testdata/yaga.yaml")

	fs := e.runValidation()
	var listCol, cardCol *finding
	for i := range fs {
		switch fs[i].label {
		case "User.list.columns.role_id: not a column of the resource's table":
			listCol = &fs[i]
		case "User.card.fields.missing_card: not a column of the resource's table":
			cardCol = &fs[i]
		}
	}
	if listCol == nil || listCol.kind != "warning" || listCol.goTo == nil {
		t.Fatalf("expected navigable list.columns warning, got %+v", fs)
	}
	if cardCol == nil || cardCol.kind != "warning" || cardCol.goTo == nil {
		t.Fatalf("expected navigable card.fields warning, got %+v", fs)
	}

	e.app = tview.NewApplication()
	e.buildShell()
	listCol.goTo()
	if got := e.history[len(e.history)-1]; got != "Resources/User/List/Columns" {
		t.Errorf("list finding should jump to the columns page, got %q", got)
	}
	cardCol.goTo()
	if got := e.history[len(e.history)-1]; got != "Resources/User/Card/Fields" {
		t.Errorf("card finding should jump to the card-fields page, got %q", got)
	}
}

// TestSectionJumpFocusesOffendingRow verifies sectionJump maps every section to
// the right page and preselects the offending column/field row.
func TestSectionJumpFocusesOffendingRow(t *testing.T) {
	cfg := testConfig()
	cfg.Resources[0].List.Columns = append(cfg.Resources[0].List.Columns, types.Column{Name: "role_id"})
	cfg.Resources[0].Card = &types.CardConfig{Fields: []types.Field{{Name: "a"}, {Name: "b"}}}
	cfg.Resources[0].Form.Update = &types.FormAction{Fields: []types.Field{{Name: "x"}}}
	e := New(cfg, "testdata/yaga.yaml")

	name, prim := e.sectionJump("User", schema.ColumnRef{Column: "role_id", Section: "list.columns", Index: 1})
	if name != "Resources/User/List/Columns" {
		t.Errorf("list.columns jump name = %q", name)
	}
	if l, ok := prim.(*tview.List); !ok || l.GetCurrentItem() != 1 {
		t.Errorf("list.columns should focus row 1, got %T", prim)
	}

	name, prim = e.sectionJump("User", schema.ColumnRef{Column: "b", Section: "card.fields", Index: 1})
	if name != "Resources/User/Card/Fields" {
		t.Errorf("card.fields jump name = %q", name)
	}
	if l, ok := prim.(*tview.List); !ok || l.GetCurrentItem() != 1 {
		t.Errorf("card.fields should focus row 1, got %T", prim)
	}

	name, prim = e.sectionJump("User", schema.ColumnRef{Column: "x", Section: "form.update.fields", Index: 0})
	if name != "Resources/User/Form/Update/Fields" {
		t.Errorf("form.update.fields jump name = %q", name)
	}
	if l, ok := prim.(*tview.List); !ok || l.GetCurrentItem() != 0 {
		t.Errorf("form.update.fields should focus row 0, got %T", prim)
	}

	name, prim = e.sectionJump("User", schema.ColumnRef{Column: "created_at", Section: "card.default_sort", Index: 0})
	if name != "Resources/User/Card" {
		t.Errorf("card.default_sort jump name = %q", name)
	}
	if _, ok := prim.(*tview.Form); !ok {
		t.Errorf("card.default_sort should open a form page, got %T", prim)
	}

	if _, prim = e.sectionJump("NoSuchResource", schema.ColumnRef{Section: "list.columns"}); prim != nil {
		t.Errorf("unknown resource should yield no page, got %T", prim)
	}
}

// TestValidateMenuEntry verifies the Validate screen is reachable from the left
// navigation (present in the nav list under its label).
func TestValidateMenuEntry(t *testing.T) {
	e := New(testConfig(), "testdata/yaga.yaml")
	e.app = tview.NewApplication()
	e.buildShell()
	found := false
	for i := 0; i < e.nav.GetItemCount(); i++ {
		if main, _ := e.nav.GetItemText(i); main == "Validate (Ctrl+V)" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Validate not present in the editor navigation")
	}
}

// TestValidateGlobalShortcut verifies Ctrl+V opens the Validate screen from
// anywhere (global key in capture, like Ctrl+S/Ctrl+Q).
func TestValidateGlobalShortcut(t *testing.T) {
	screen := newSimScreen(t)
	e := New(testConfig(), "testdata/yaga.yaml")
	e.SetScreen(screen)

	go func() {
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlV, 0, tcell.ModNone)
		time.Sleep(150 * time.Millisecond)
		screen.InjectKey(tcell.KeyCtrlQ, 0, tcell.ModNone)
	}()

	if _, err := e.Run(); err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(e.history) == 0 || e.history[len(e.history)-1] != "Validate" {
		t.Errorf("expected Validate to be the front page, history: %v", e.history)
	}
}
