package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/types"
)

// testConfig returns a minimal config that passes parser.ValidateAll.
func testConfig() *types.Config {
	return &types.Config{
		Version: "1.0",
		Panel: types.Panel{
			ID:   "admin",
			Path: "/admin",
			Name: "Admin",
			Brand: types.Brand{
				Logo:   "logo.png",
				Colors: types.BrandColors{Primary: "#6366f1", Secondary: "#8b5cf6"},
			},
		},
		Connections: map[string]types.Connection{
			"primary": {Driver: "sqlite", DSN: "./test.db"},
		},
		SQLC: types.SQLCConfig{
			Config:     "sqlc.yaml",
			QueriesDir: "./sql/queries",
			SchemaDir:  "./sql/migrations",
			OutputPkg:  "data",
		},
		Auth: types.AuthConfig{
			Guard: "password", Provider: "password", Table: "users",
			Login: types.LoginConfig{Fields: []string{"email", "password"}, Redirect: "/admin"},
		},
		Resources: []types.Resource{
			{
				Name:  "User",
				Table: "users",
				List:  &types.ListConfig{Columns: []types.Column{{Name: "id", Type: "integer"}, {Name: "email", Type: "string"}}, PerPage: 20},
			},
		},
	}
}

// setupServer creates a Server rooted at a temp dir with the given sql trees.
func setupServer(t *testing.T, cfg *types.Config) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "yaga.yaml")
	if err := os.WriteFile(configPath, []byte("version: \"1.0\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"sql/queries", "sql/migrations"} {
		if err := os.MkdirAll(filepath.Join(dir, rel), 0755); err != nil {
			t.Fatal(err)
		}
	}
	s := New(cfg, configPath, Options{Port: 0})
	return s, dir
}

func get(t *testing.T, h http.Handler, method, url string, body []byte) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		req = httptest.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]interface{}
	if rec.Header().Get("Content-Type") == "application/json" && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
			t.Fatalf("bad json (%d): %v: %s", rec.Code, err, rec.Body.String())
		}
	}
	return rec, data
}

func TestConfigGet(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	rec, data := get(t, s.Handler(), "GET", "/api/config", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	cfg := data["config"].(map[string]interface{})
	if cfg["version"] != "1.0" {
		t.Errorf("version = %v", cfg["version"])
	}
	panel := cfg["panel"].(map[string]interface{})
	if panel["name"] != "Admin" || panel["path"] != "/admin" {
		t.Errorf("panel = %v", panel)
	}
	resources := cfg["resources"].([]interface{})
	user := resources[0].(map[string]interface{})
	list := user["list"].(map[string]interface{})
	if list["per_page"] != float64(20) {
		t.Errorf("per_page = %v (want 20)", list["per_page"])
	}
	if _, ok := data["path"]; !ok {
		t.Error("missing path key")
	}
}

func TestConfigPutValid(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	// Round-trip the served config through the API: the numbers must survive.
	_, first := get(t, s.Handler(), "GET", "/api/config", nil)
	cfg := first["config"]
	payload, _ := json.Marshal(cfg)

	rec, data := get(t, s.Handler(), "PUT", "/api/config", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if data["ok"] != true {
		t.Errorf("ok = %v", data["ok"])
	}

	// The in-memory config now has the PUT version: panel.name changed.
	_, check := get(t, s.Handler(), "GET", "/api/config", nil)
	c := check["config"].(map[string]interface{})
	p := c["panel"].(map[string]interface{})
	if p["name"] != "Admin" {
		t.Errorf("panel.name = %v after PUT", p["name"])
	}
}

func TestConfigPutInvalid(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	bad := []byte(`{"panel":{},"resources":[]}`)
	rec, data := get(t, s.Handler(), "PUT", "/api/config", bad)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	errs := data["errors"].([]interface{})
	if len(errs) == 0 {
		t.Error("expected validation errors")
	}
	// The in-memory config must be untouched.
	_, check := get(t, s.Handler(), "GET", "/api/config", nil)
	c := check["config"].(map[string]interface{})
	p := c["panel"].(map[string]interface{})
	if p["name"] != "Admin" {
		t.Errorf("config was mutated by failed PUT: %v", p["name"])
	}
}

func TestSaveWritesYAML(t *testing.T) {
	cfg := testConfig()
	s, dir := setupServer(t, cfg)
	configPath := filepath.Join(dir, "yaga.yaml")

	// Change something via PUT then save.
	_, first := get(t, s.Handler(), "GET", "/api/config", nil)
	tree := first["config"].(map[string]interface{})
	panel := tree["panel"].(map[string]interface{})
	panel["name"] = "Order Management"
	payload, _ := json.Marshal(tree)
	if rec, _ := get(t, s.Handler(), "PUT", "/api/config", payload); rec.Code != 200 {
		t.Fatalf("PUT failed: %s", rec.Body.String())
	}
	if rec, _ := get(t, s.Handler(), "POST", "/api/save", nil); rec.Code != 200 {
		t.Fatalf("save failed: %s", rec.Body.String())
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "name: Order Management") {
		t.Errorf("saved YAML missing renamed panel:\n%s", data)
	}
	if !strings.Contains(string(data), "per_page: 20") {
		t.Errorf("saved YAML missing per_page:\n%s", data)
	}
}

func TestRawRoundTrip(t *testing.T) {
	s, dir := setupServer(t, testConfig())
	rec, data := get(t, s.Handler(), "GET", "/api/raw", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	y := data["yaml"].(string)
	if !strings.Contains(y, "path: /admin") {
		t.Errorf("raw yaml missing panel:\n%s", y)
	}

	// Valid raw YAML replaces the config.
	newYAML := `version: "1.0"
panel:
  id: admin
  path: /admin
  name: Renamed
resources:
  - name: User
`
	rec, data = get(t, s.Handler(), "PUT", "/api/raw", []byte(newYAML))
	if rec.Code != 200 {
		t.Fatalf("raw PUT status = %d: %s", rec.Code, rec.Body.String())
	}
	_, check := get(t, s.Handler(), "GET", "/api/config", nil)
	c := check["config"].(map[string]interface{})
	if c["panel"].(map[string]interface{})["name"] != "Renamed" {
		t.Error("raw PUT did not replace the config")
	}

	// Invalid raw YAML is rejected and the config stays.
	bad := `version: "1.0"
panel: {}
resources: []
`
	rec, data = get(t, s.Handler(), "PUT", "/api/raw", []byte(bad))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad raw PUT status = %d, want 422", rec.Code)
	}
	if len(data["errors"].([]interface{})) == 0 {
		t.Error("expected errors for invalid raw YAML")
	}
	_, check = get(t, s.Handler(), "GET", "/api/config", nil)
	c = check["config"].(map[string]interface{})
	if c["panel"].(map[string]interface{})["name"] != "Renamed" {
		t.Error("invalid raw PUT mutated the config")
	}
	_ = dir
}

func TestValidateFindings(t *testing.T) {
	cfg := testConfig()
	s, dir := setupServer(t, cfg)
	// A schema table with a matching column.
	writeFile(t, filepath.Join(dir, "sql/migrations/users.sql"),
		"-- users\nCREATE TABLE users (\n  id INTEGER PRIMARY KEY,\n  email TEXT\n);\n")

	rec, data := get(t, s.Handler(), "GET", "/api/validate", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	findings := data["findings"].([]interface{})
	// No structural errors for the minimal config.
	for _, f := range findings {
		if f.(map[string]interface{})["kind"] == "error" {
			t.Errorf("unexpected error finding: %v", f)
		}
	}

	// A resource whose table is absent from the schema block produces a
	// missing-table finding; a column ref outside the block's table columns
	// produces a missing-column finding.
	cfg2 := testConfig()
	cfg2.Schema = &types.Schema{
		Tables: []types.SchemaTable{{
			Name: "users",
			PK:   "id",
			Columns: []types.SchemaColumn{
				{Name: "id", Type: "integer", PrimaryKey: true},
				{Name: "email", Type: "string"},
			},
		}},
	}
	// Resource "User" maps to users (present), but its detail field "email" is
	// a real column while list.query exists; so no missing table/column.
	s2, _ := setupServer(t, cfg2)
	rec, data = get(t, s2.Handler(), "GET", "/api/validate", nil)
	findings = data["findings"].([]interface{})
	for _, f := range findings {
		label := f.(map[string]interface{})["label"].(string)
		if strings.Contains(label, "missing table") || strings.Contains(label, "missing column") {
			t.Errorf("unexpected missing ref finding for valid schema: %v", f)
		}
	}

	// A resource referencing a column the schema table lacks.
	cfg3 := testConfig()
	cfg3.Schema = &types.Schema{
		Tables: []types.SchemaTable{{
			Name:    "users",
			PK:      "id",
			Columns: []types.SchemaColumn{{Name: "id", Type: "integer", PrimaryKey: true}},
		}},
	}
	s3, _ := setupServer(t, cfg3)
	rec, data = get(t, s3.Handler(), "GET", "/api/validate", nil)
	findings = data["findings"].([]interface{})
	found := false
	for _, f := range findings {
		if strings.Contains(f.(map[string]interface{})["label"].(string), "missing column") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a missing-column finding, got %v", findings)
	}
}

func TestStaticServesIndex(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	rec, _ := get(t, s.Handler(), "GET", "/", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "WEdit") {
		t.Fatalf("/ did not serve index.html: %d", rec.Code)
	}
	rec, _ = get(t, s.Handler(), "GET", "/static/app.js", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "init();") {
		t.Fatalf("/static/app.js not served: %d", rec.Code)
	}
	rec, _ = get(t, s.Handler(), "GET", "/static/style.css", nil)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Catppuccin") {
		t.Fatalf("/static/style.css not served: %d", rec.Code)
	}
	// Unknown path 404s.
	if rec, _ := get(t, s.Handler(), "GET", "/nope", nil); rec.Code != 404 {
		t.Errorf("unknown path status = %d, want 404", rec.Code)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestFixEndpoint checks that POST /api/fix applies the same repairs as
// `validate --fix` to the in-memory config (no disk write) and reports the
// unfixable errors that remain.
func TestFixEndpoint(t *testing.T) {
	cfg := testConfig()
	cfg.Resources[0].List.Filter = &types.FilterConfig{}
	s, _ := setupServer(t, cfg)

	rec, data := get(t, s.Handler(), "GET", "/api/validate", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	found := false
	for _, f := range data["findings"].([]interface{}) {
		if strings.Contains(f.(map[string]interface{})["label"].(string), "where is required") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a where-required finding first, got %v", data["findings"])
	}

	rec, data = get(t, s.Handler(), "POST", "/api/fix", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if data["changed"] != true {
		t.Fatalf("changed = %v", data["changed"])
	}
	fixed := data["fixed"].([]interface{})
	if len(fixed) != 1 || fixed[0] != "resources/User/list.filter" {
		t.Fatalf("fixed = %v", fixed)
	}
	if errs := data["errors"].([]interface{}); len(errs) != 0 {
		t.Fatalf("errors = %v", errs)
	}

	// The in-memory config now validates cleanly.
	rec, data = get(t, s.Handler(), "GET", "/api/validate", nil)
	for _, f := range data["findings"].([]interface{}) {
		if f.(map[string]interface{})["kind"] == "error" {
			t.Errorf("error finding after fix: %v", f)
		}
	}
}

// TestFixEndpointNoop ensures an already-valid config reports changed=false
// and leaves the config untouched.
func TestFixEndpointNoop(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	rec, data := get(t, s.Handler(), "POST", "/api/fix", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if data["changed"] != false {
		t.Fatalf("changed = %v, want false", data["changed"])
	}
	if f := data["fixed"].([]interface{}); len(f) != 0 {
		t.Fatalf("fixed = %v", f)
	}
}

// TestFixEndpointPartialRepair checks an unfixable error survives while the
// fixable filter is still applied.
func TestFixEndpointPartialRepair(t *testing.T) {
	cfg := testConfig()
	cfg.Resources[0].List.Filter = &types.FilterConfig{}
	cfg.Resources[0].ImportCSV = true
	s, _ := setupServer(t, cfg)

	rec, data := get(t, s.Handler(), "POST", "/api/fix", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if data["changed"] != true {
		t.Fatal("expected changes")
	}
	fixed := data["fixed"].([]interface{})
	if len(fixed) != 1 || fixed[0] != "resources/User/list.filter" {
		t.Fatalf("fixed = %v", fixed)
	}
	if errs := data["errors"].([]interface{}); len(errs) != 1 || !strings.Contains(errs[0].(string), "import_csv") {
		t.Fatalf("errors = %v", errs)
	}
	// After a fix the filter error is gone; only the import_csv error remains.
	_, data = get(t, s.Handler(), "GET", "/api/validate", nil)
	found := false
	for _, f := range data["findings"].([]interface{}) {
		label := f.(map[string]interface{})["label"].(string)
		if strings.Contains(label, "where is required") {
			t.Errorf("filter error should be fixed away: %v", f)
		}
		if strings.Contains(label, "import_csv") {
			found = true
		}
	}
	if !found {
		t.Errorf("import_csv error should remain, got %v", data["findings"])
	}
}
