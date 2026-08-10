package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// drawPrimitive renders p (with the given rect) onto a simulation screen and
// returns the visible text per row plus the drawable cells.
func drawPrimitive(t *testing.T, p tview.Primitive, w, h int) ([]string, []tcell.SimCell) {
	t.Helper()
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(w, h)
	if r, ok := p.(interface{ SetRect(x, y, width, height int) }); ok {
		r.SetRect(0, 0, w, h)
	}
	p.Draw(screen)
	screen.Show()
	contents, sw, _ := screen.GetContents()
	lines := make([]string, h)
	for y := 0; y < h; y++ {
		var b strings.Builder
		for x := 0; x < sw; x++ {
			b.WriteRune(rune(contents[y*sw+x].Bytes[0]))
		}
		lines[y] = b.String()
	}
	return lines, contents
}

// TestSyncSimpleView renders the Sync screen and verifies it is the simple
// list form: schema tables, query definitions, the missing-reference summary
// and the action buttons are all visible in the TextView.
func TestSyncSimpleView(t *testing.T) {
	dir := t.TempDir()
	migrations := filepath.Join(dir, "sql", "migrations")
	queries := filepath.Join(dir, "sql", "queries")
	if err := os.MkdirAll(migrations, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(queries, 0755); err != nil {
		t.Fatal(err)
	}
	schemaSQL := `CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  email TEXT NOT NULL,
  name TEXT
);
CREATE TABLE roles (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL
);`
	if err := os.WriteFile(filepath.Join(migrations, "001.sql"), []byte(schemaSQL), 0644); err != nil {
		t.Fatal(err)
	}
	rolesQ := "-- name: ListRoles :many\nSELECT id, name FROM roles ORDER BY name;\n"
	if err := os.WriteFile(filepath.Join(queries, "roles.sql"), []byte(rolesQ), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig()
	cfg.SQLC.SchemaDir = "./sql/migrations"
	cfg.SQLC.QueriesDir = "./sql/queries"
	e := New(cfg, filepath.Join(dir, "go-fila.yaml"))

	lines, _ := drawPrimitive(t, e.syncPage(), 110, 30)
	text := strings.Join(lines, "\n")
	for _, want := range []string{"Schema", "users", "roles", "Queries", "ListRoles", "missing queries", "missing tables", "inline SQL", "Generate missing queries", "Refresh", "Back"} {
		if !strings.Contains(text, want) {
			t.Errorf("sync view missing %q in render:\n%s", want, text)
		}
	}
}

// TestPreviewGridColors verifies only the grid chrome is light blue while the
// content text stays white.
func TestPreviewGridColors(t *testing.T) {
	e := New(testConfig(), "testdata/go-fila.yaml")
	tv := tview.NewTextView().SetDynamicColors(true)
	tv.SetText(mockFrame(e, "  [::b]Users[::-]\n  [::d]widgets[-:-:-]\n"))
	lines, cells := drawPrimitive(t, tv, 90, 24)

	// Locate the topbar row (the line containing "Admin").
	topbar := -1
	for i, l := range lines {
		if strings.Contains(l, "Admin") {
			topbar = i
			break
		}
	}
	if topbar < 0 {
		t.Fatal("topbar not rendered")
	}
	for x := 0; x < 10; x++ {
		c := cells[topbar*90+x]
		ch := rune(c.Bytes[0])
		fg, _, _ := c.Style.Decompose()
		switch x {
		case 0: // leading grid border │
			if ch == '│' && fg != tcell.ColorLightBlue {
				t.Errorf("grid border at col %d fg = %v, want light blue", x, fg)
			}
		case 2: // first content char (A of Admin)
			if ch == 'A' && fg != tcell.ColorWhite {
				t.Errorf("content text at col %d fg = %v, want white", x, fg)
			}
		}
	}
}
