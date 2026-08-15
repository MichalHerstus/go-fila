package serve

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MichalHerstus/yaga/internal/types"
)

// previewCfg builds a config with one custom page (stat + chart widgets), one
// resource, and a navigation group carrying an HTML-ish label used by the
// escaping tests.
func previewCfg() *types.Config {
	cfg := testConfig()
	cfg.Pages = []types.Page{
		{
			Name: "Dashboard",
			Path: "/dashboard",
			Widgets: []types.Widget{
				{Type: "stat", Label: "Total revenue", Icon: "dollar", Prefix: "$"},
				{Type: "chart", Label: "Orders by month", Chart: &types.ChartConfig{Type: "bar"}},
			},
		},
	}
	cfg.Navigation = []types.NavigationGroup{
		{
			Group: "Main",
			Sort:  1,
			Items: []types.NavigationItem{
				{Type: "resource", Resource: "User"},
				{Type: "page", Page: "Dashboard"},
				{Type: "link", URL: "https://example.com", Label: "<script>alert(1)</script>", OpensInNewTab: true},
			},
		},
	}
	return cfg
}

func TestPreviewPage(t *testing.T) {
	s, _ := setupServer(t, previewCfg())
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page&page=Dashboard&theme=light", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<html lang=\"en\" class=\"preview\"",
		"Total revenue",
		"$",
		"text-2xl font-bold",
		"Orders by month",
		`data-chart-type="bar"`,
		`<canvas`,
		"/preview/styles.css",
		"/preview/chart.js",
		"--brand-primary: #6366f1;",
		"--brand-primary-rgb: 99 102 241;",
		"html.classList.remove('dark');",
		"data-width=\"256\"",
		"max-w-none",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page preview missing %q", want)
		}
	}
}

func TestPreviewResource(t *testing.T) {
	s, _ := setupServer(t, previewCfg())
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=resource&resource=User&theme=auto", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"User",
		">id</th>",
		">email</th>",
		"Search",
		"View",
		"Edit",
		"Delete",
		"Showing page 1 of 2",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("resource preview missing %q", want)
		}
	}
}

func TestPreviewThemeForced(t *testing.T) {
	s, _ := setupServer(t, previewCfg())

	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page&theme=dark", nil)
	if !strings.Contains(rec.Body.String(), "html.classList.add('dark');") {
		t.Errorf("dark force did not add the dark class: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "localStorage.getItem('yaga-theme')") {
		t.Errorf("forced dark still reads the saved preference")
	}

	rec, _ = get(t, s.Handler(), "GET", "/preview?view=page&theme=light", nil)
	if !strings.Contains(rec.Body.String(), "html.classList.remove('dark');") {
		t.Errorf("light force did not remove the dark class: %s", rec.Body.String())
	}
}

func TestPreviewThemeAutoFromConfig(t *testing.T) {
	cfg := previewCfg()
	cfg.Panel.Theme.DarkMode = true
	s, _ := setupServer(t, cfg)
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page&theme=auto", nil)
	if !strings.Contains(rec.Body.String(), "else if (true) { html.classList.add('dark'); }") {
		t.Errorf("auto dark mode did not default to dark: %s", rec.Body.String())
	}
}

func TestPreviewEscapesNavLabel(t *testing.T) {
	s, _ := setupServer(t, previewCfg())
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page", nil)
	body := rec.Body.String()
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("nav label was not HTML-escaped")
	}
	if strings.Contains(body, "alert(1)</a>") || strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("raw unescaped nav label leaked into the document")
	}
}

// TestPreviewSidebarLinks verifies the preview sidebar navigates within the
// /preview endpoint instead of emitting real dashboard routes that the wedit
// server would 404 (the generated app's /admin/... handlers do not exist here).
func TestPreviewSidebarLinks(t *testing.T) {
	s, _ := setupServer(t, previewCfg())
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page", nil)
	body := rec.Body.String()
	for _, want := range []string{
		`href="/preview?view=resource&resource=User"`,
		`href="/preview?view=page&page=Dashboard"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("preview sidebar missing %q", want)
		}
	}
	if strings.Contains(body, `href="/admin/user"`) || strings.Contains(body, `href="/admin/dashboard"`) {
		t.Errorf("preview sidebar leaked a real dashboard route (would 404)")
	}
}

func TestPreviewNoTarget(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	rec, _ := get(t, s.Handler(), "GET", "/preview?view=page", nil)
	if !strings.Contains(rec.Body.String(), "Select a page from the Preview tab toolbar.") {
		t.Errorf("missing no-target fallback: %s", rec.Body.String())
	}
	rec, _ = get(t, s.Handler(), "GET", "/preview?view=resource", nil)
	if !strings.Contains(rec.Body.String(), "Select a resource from the Preview tab toolbar.") {
		t.Errorf("missing resource no-target fallback: %s", rec.Body.String())
	}
}

func TestPreviewAssets(t *testing.T) {
	s, _ := setupServer(t, previewCfg())

	rec, _ := get(t, s.Handler(), "GET", "/preview/styles.css", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("styles status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("styles content-type = %q, want text/css", ct)
	}
	if !strings.Contains(rec.Body.String(), "--brand-primary") && len(rec.Body.String()) < 1000 {
		t.Errorf("styles.css looks empty/short (len=%d)", rec.Body.Len())
	}

	rec, _ = get(t, s.Handler(), "GET", "/preview/chart.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("chart status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("chart content-type = %q, want application/javascript", ct)
	}
	if len(rec.Body.String()) < 100000 {
		t.Errorf("chart.js unexpectedly small: %d bytes", rec.Body.Len())
	}
}

func TestPreviewReflectsConfigPut(t *testing.T) {
	s, _ := setupServer(t, testConfig())
	// Round-trip the served (YAML-keyed) config through the API, then add a
	// resource — this mirrors exactly what the SPA does before loading the
	// preview iframe.
	_, first := get(t, s.Handler(), "GET", "/api/config", nil)
	tree := first["config"].(map[string]interface{})
	resources := tree["resources"].([]interface{})
	resources = append(resources, map[string]interface{}{"name": "Order", "table": "orders"})
	tree["resources"] = resources
	payload, _ := json.Marshal(tree)

	rec, _ := get(t, s.Handler(), "PUT", "/api/config", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/config = %d: %s", rec.Code, rec.Body.String())
	}

	rec, _ = get(t, s.Handler(), "GET", "/preview?view=resource&resource=Order", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Order") {
		t.Errorf("preview did not reflect the PUT config")
	}
}
