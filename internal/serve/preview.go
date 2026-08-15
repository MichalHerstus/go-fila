// preview.go
//
// Implements the wedit dashboard preview (GET /preview). It renders a faithful
// HTML mock of a generated admin page (custom pages and resource list views)
// from the in-memory config: the same Tailwind classes, brand colors, fonts,
// sidebar width, sticky topbar and dark-mode handling the generated app uses,
// plus the vendored Chart.js for chart widgets. The mock is served inside an
// iframe by the SPA's Preview tab; the stylesheet and Chart.js bundle are
// served from the generator's embedded assets so the preview and the generated
// dashboard stay byte-identical.
package serve

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"sort"
	"strings"

	"github.com/MichalHerstus/yaga/internal/generator"
	"github.com/MichalHerstus/yaga/internal/types"
)

// previewView lists the kinds of preview that can be rendered.
const (
	previewPage     = "page"
	previewResource = "resource"
)

// handlePreview renders the dashboard mock for ?view=page|resource&page=..&resource=..
// with an optional ?theme=auto|light|dark (auto = follow panel.theme.dark_mode).
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	q := r.URL.Query()
	theme := q.Get("theme")
	if theme != "light" && theme != "dark" {
		theme = "auto"
	}

	var body string
	switch q.Get("view") {
	case previewResource:
		body = resourcePreviewBody(cfg, q.Get("resource"))
	default:
		body = pagePreviewBody(cfg, q.Get("page"))
	}

	doc := previewDoc(cfg, theme, body)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(doc))
}

// handlePreviewStyles serves the pre-built Tailwind stylesheet the generated
// app ships at static/css/styles.css, so the preview uses the exact compiled
// classes of the real dashboard.
func (s *Server) handlePreviewStyles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	_, _ = w.Write(generator.StylesCSS())
}

// handlePreviewChart serves the vendored Chart.js bundle the generated app
// ships at static/js/chart.js.
func (s *Server) handlePreviewChart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write(generator.ChartUmdJS())
}

// previewDoc assembles the full HTML document around the mock body: the same
// shell as the generated base.templ (sidebar/topbar/brand vars/fonts/dark
// mode) plus the generated app's theme toggle + Chart.js auto-render scripts.
func previewDoc(cfg *types.Config, theme string, body string) string {
	panel := cfg.Panel
	panelName := panel.Name
	if panelName == "" {
		panelName = "admin"
	}
	primary := panel.Brand.Colors.Primary
	if primary == "" {
		primary = "#6366f1"
	}
	secondary := panel.Brand.Colors.Secondary
	if secondary == "" {
		secondary = "#8b5cf6"
	}

	var fonts strings.Builder
	if panel.Theme.Font.Family != "" {
		fmt.Fprintf(&fonts, "body { font-family: %s; }\n", escCSS(panel.Theme.Font.Family))
	}
	if panel.Theme.Font.Mono != "" {
		fmt.Fprintf(&fonts, "code, pre { font-family: %s; }\n", escCSS(panel.Theme.Font.Mono))
	}

	sticky := "relative"
	if panel.Layout.Topbar.Sticky {
		sticky = "sticky top-0 z-10"
	}
	sidebarWidth := panel.Layout.Sidebar.Width
	if sidebarWidth <= 0 {
		sidebarWidth = 256
	}
	maxW := cfg.Panel.Layout.MaxContentWidth
	if maxW == "" {
		maxW = "none"
	}

	// Dark-mode init: mirror the generated Base layout. auto follows the
	// config; a forced theme bypasses the saved preference on first load but
	// toggling still writes localStorage (so a later auto view honors it).
	darkInit := "var html = document.documentElement;\n"
	switch theme {
	case "light":
		darkInit += "html.classList.remove('dark');\n"
	case "dark":
		darkInit += "html.classList.add('dark');\n"
	default:
		def := "false"
		if panel.Theme.DarkMode {
			def = "true"
		}
		darkInit += "var saved = localStorage.getItem('yaga-theme');\n" +
			"if (saved === 'dark') { html.classList.add('dark'); }\n" +
			"else if (saved === 'light') { html.classList.remove('dark'); }\n" +
			"else if (" + def + ") { html.classList.add('dark'); }\n"
	}

	var b strings.Builder
	b.WriteString(`<!DOCTYPE html>
<html lang="en" class="preview">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Preview</title>
<link href="/preview/styles.css" rel="stylesheet">
<script src="/preview/chart.js"></script>
<style>
:root {
  --brand-primary: ` + primary + `;
  --brand-secondary: ` + secondary + `;
  --brand-primary-rgb: ` + generator.HexChannels(primary) + `;
  --brand-secondary-rgb: ` + generator.HexChannels(secondary) + `;
}
` + fonts.String() + `
</style>
</head>
<body class="bg-gray-50 dark:bg-gray-900">
<div class="flex h-screen">
  <aside id="app-sidebar" data-width="` + fmt.Sprint(sidebarWidth) + `" class="bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 shadow-md h-screen overflow-y-auto shrink-0">
    <div class="p-4 border-b border-gray-200 dark:border-gray-700">
      <h1 class="text-xl font-bold text-gray-900 dark:text-gray-100">` + escHTML(panelName) + `</h1>
    </div>
    <nav class="mt-2">
` + previewSidebar(cfg) + `    </nav>
  </aside>
  <div class="flex-1 flex flex-col min-w-0">
    <header class="bg-white dark:bg-gray-800 shadow-sm px-6 py-3 flex items-center justify-between ` + sticky + `">
      <div class="flex items-center gap-4">
        <button class="text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200" onclick="toggleTheme()" title="Toggle dark mode">
          <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364 6.364l-.707-.707M6.343 6.343l-.707-.707m12.728 0l-.707.707M6.343 17.657l-.707.707M16 12a4 4 0 11-8 0 4 4 0 018 0z" /></svg>
        </button>
        <span class="text-sm text-gray-500 dark:text-gray-400">preview</span>
      </div>
      <div class="flex items-center gap-4">
        <span class="text-sm text-gray-700 dark:text-gray-300">admin</span>
        <span class="text-sm text-gray-600 dark:text-gray-400">Logout</span>
      </div>
    </header>
    <main class="flex-1 overflow-y-auto p-6">
      <div class="max-w-` + escAttr(maxW) + ` mx-auto">
` + body + `      </div>
    </main>
  </div>
</div>
<script>
function toggleTheme() {
  var html = document.documentElement;
  html.classList.toggle('dark');
  localStorage.setItem('yaga-theme', html.classList.contains('dark') ? 'dark' : 'light');
}
(function() { ` + darkInit + ` })();
(function() {
  var aside = document.getElementById('app-sidebar');
  if (aside) { aside.style.width = aside.getAttribute('data-width') + 'px'; }
})();
document.addEventListener('DOMContentLoaded', function() {
  var rootStyle = getComputedStyle(document.documentElement);
  var brand = (rootStyle.getPropertyValue('--brand-primary') || '#6366f1').trim();
  function hexToRgba(hex, alpha) {
    var m = /^#([0-9a-fA-F]{6})$/.exec(hex);
    if (!m) { return 'rgba(99, 102, 241, ' + alpha + ')'; }
    var n = parseInt(m[1], 16);
    return 'rgba(' + ((n >> 16) & 255) + ', ' + ((n >> 8) & 255) + ', ' + (n & 255) + ', ' + alpha + ')';
  }
  document.querySelectorAll('canvas[data-chart-type]').forEach(function(canvas) {
    var ctx = canvas.getContext('2d');
    var type = canvas.dataset.chartType;
    var labels = JSON.parse(canvas.dataset.labels || '[]');
    var values = JSON.parse(canvas.dataset.values || '[]');
    new Chart(ctx, {
      type: type,
      data: {
        labels: labels,
        datasets: [{
          label: canvas.parentElement.querySelector('h3')?.textContent || '',
          data: values,
          borderColor: brand,
          backgroundColor: hexToRgba(brand, 0.2),
        }]
      }
    });
  });
});
</script>
</body>
</html>`)
	return b.String()
}

// previewSidebar renders the sorted navigation groups and their items using
// the exact href/label rules of the generated generateLayoutViews.
func previewSidebar(cfg *types.Config) string {
	sortedNav := make([]types.NavigationGroup, len(cfg.Navigation))
	copy(sortedNav, cfg.Navigation)
	sort.Slice(sortedNav, func(i, j int) bool {
		return sortedNav[i].Sort < sortedNav[j].Sort
	})

	var b strings.Builder
	if len(sortedNav) > 0 {
		for _, ng := range sortedNav {
			icon := navIconSVG(ng.Icon)
			fmt.Fprintf(&b, "      <div class=\"px-4 py-2 text-xs font-semibold text-gray-500 dark:text-gray-400 uppercase tracking-wider mt-4\">%s%s</div>\n", icon, escHTML(ng.Group))
			for _, item := range ng.Items {
				if item.Resource != "" {
					label := item.Resource
					if item.Label != "" {
						label = item.Label
					}
					fmt.Fprintf(&b, "      <a href=\"/preview?view=resource&resource=%s\" class=\"block px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-brand-primary/10 hover:text-brand-primary mx-2 rounded-md\">%s</a>\n", escAttr(item.Resource), escHTML(label))
				} else if item.Page != "" {
					label := item.Page
					if item.Label != "" {
						label = item.Label
					}
					fmt.Fprintf(&b, "      <a href=\"/preview?view=page&page=%s\" class=\"block px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-brand-primary/10 hover:text-brand-primary mx-2 rounded-md\">%s</a>\n", escAttr(item.Page), escHTML(label))
				} else if item.Type == "link" {
					target := ""
					if item.OpensInNewTab {
						target = ` target="_blank"`
					}
					label := item.URL
					if item.Label != "" {
						label = item.Label
					}
					fmt.Fprintf(&b, "      <a href=\"%s\"%s class=\"block px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-brand-primary/10 hover:text-brand-primary mx-2 rounded-md\">%s</a>\n", escAttr(item.URL), target, escHTML(label))
				}
			}
		}
	} else {
		for _, r := range cfg.Resources {
			fmt.Fprintf(&b, "      <a href=\"/preview?view=resource&resource=%s\" class=\"block px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-brand-primary/10 hover:text-brand-primary mx-2 rounded-md\">%s</a>\n", escAttr(r.Name), escHTML(r.Name))
		}
	}
	return b.String()
}

// pagePreviewBody renders one custom page's widget mock.
func pagePreviewBody(cfg *types.Config, pageName string) string {
	var page *types.Page
	for i := range cfg.Pages {
		if cfg.Pages[i].Name == pageName {
			page = &cfg.Pages[i]
			break
		}
	}
	var b strings.Builder
	b.WriteString("      <div class=\"p-6\">\n")
	if page == nil {
		b.WriteString("        <p class=\"text-gray-500 dark:text-gray-400\">Select a page from the Preview tab toolbar.</p>\n")
		b.WriteString("      </div>\n")
		return b.String()
	}
	fmt.Fprintf(&b, "        <h1 class=\"text-2xl font-bold mb-6 text-gray-900 dark:text-gray-100\">%s</h1>\n", escHTML(page.Name))
	for _, w := range page.Widgets {
		b.WriteString(previewWidget(w))
	}
	b.WriteString("      </div>\n")
	return b.String()
}

// previewWidget renders one widget card mirroring the generated widget templ,
// with deterministic fake data in place of DB results.
func previewWidget(w types.Widget) string {
	switch w.Type {
	case "stats_grid":
		cols := w.Columns
		if cols <= 0 || cols > 12 {
			cols = 4
		}
		var b strings.Builder
		fmt.Fprintf(&b, "      <div class=\"grid grid-cols-1 md:grid-cols-2 lg:grid-cols-%d gap-4 mb-6\">\n", cols)
		if len(w.Widgets) == 0 {
			b.WriteString("        <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg p-6\"><p class=\"text-sm text-gray-500 dark:text-gray-400\">Empty stats grid</p></div>\n")
		}
		for _, sw := range w.Widgets {
			b.WriteString(previewStat(sw, false))
		}
		b.WriteString("      </div>\n")
		return b.String()
	case "stat":
		return previewStat(w, true)
	case "chart":
		return previewChart(w)
	case "table":
		return previewTable(w)
	case "list":
		return previewList(w)
	default: // html and unknown
		return previewHTML()
	}
}

// previewStat renders a stat card; wrap controls the outer mb-6 so grid items
// (which carry their own gap) do not double the margin.
func previewStat(w types.Widget, wrap bool) string {
	label := w.Label
	if label == "" {
		label = w.Type
	}
	icon := previewIconSVG(w.Icon)
	mb := ""
	if wrap {
		mb = " mb-6"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "      <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg p-6%s\">\n", mb)
	b.WriteString("        <div class=\"flex items-center justify-between\">\n")
	b.WriteString("          <div>\n")
	if w.Icon != "" {
		b.WriteString("            <div class=\"w-10 h-10 rounded-lg bg-brand-primary/10 flex items-center justify-center mb-3\">\n")
		b.WriteString(icon)
		b.WriteString("            </div>\n")
	}
	fmt.Fprintf(&b, "            <p class=\"text-sm text-gray-500 dark:text-gray-400\">%s</p>\n", escHTML(label))
	b.WriteString("            <p class=\"text-2xl font-bold text-gray-900 dark:text-gray-100\">\n")
	if w.Prefix != "" {
		fmt.Fprintf(&b, "              <span class=\"text-lg\">%s</span>\n", escHTML(w.Prefix))
	}
	fmt.Fprintf(&b, "              %s\n", fakeNumber(label))
	b.WriteString("            </p>\n")
	b.WriteString("          </div>\n")
	b.WriteString("        </div>\n")
	b.WriteString("      </div>\n")
	return b.String()
}

// previewChart renders a chart card with fake labels/values consumed by the
// shared Chart.js auto-render script.
func previewChart(w types.Widget) string {
	label := w.Label
	if label == "" {
		label = "Chart"
	}
	chartType := "line"
	if w.Chart != nil && w.Chart.Type != "" {
		chartType = w.Chart.Type
	}
	labels := fakeLabels(label)
	values := fakeValues(label)
	labelsJSON, _ := json.Marshal(labels)
	valuesJSON, _ := json.Marshal(values)
	var b strings.Builder
	b.WriteString("      <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg p-6 mb-6\">\n")
	fmt.Fprintf(&b, "        <h3 class=\"text-lg font-semibold mb-4 text-gray-900 dark:text-gray-100\">%s</h3>\n", escHTML(label))
	fmt.Fprintf(&b, "        <canvas id=\"chart-%s\" class=\"w-full h-64\" data-chart-type=\"%s\" data-labels=\"%s\" data-values=\"%s\"></canvas>\n",
		escAttr(label), escAttr(chartType), escAttr(string(labelsJSON)), escAttr(string(valuesJSON)))
	b.WriteString("      </div>\n")
	return b.String()
}

// previewTable renders a table card with header cells from data_columns and a
// few fake rows.
func previewTable(w types.Widget) string {
	label := w.Label
	if label == "" {
		label = "Table"
	}
	cols := w.DataColumns
	if len(cols) == 0 {
		cols = []string{"value"}
	}
	var b strings.Builder
	b.WriteString("      <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg overflow-hidden mb-6\">\n")
	b.WriteString("        <div class=\"px-6 py-4 border-b border-gray-200 dark:border-gray-700\">\n")
	fmt.Fprintf(&b, "          <h3 class=\"text-lg font-semibold text-gray-900 dark:text-gray-100\">%s</h3>\n", escHTML(label))
	b.WriteString("        </div>\n")
	b.WriteString("        <table class=\"min-w-full divide-y divide-gray-200 dark:divide-gray-700\">\n")
	b.WriteString("          <thead class=\"bg-gray-50 dark:bg-gray-900\">\n")
	b.WriteString("            <tr>\n")
	for _, c := range cols {
		fmt.Fprintf(&b, "              <th class=\"px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider\">%s</th>\n", escHTML(c))
	}
	b.WriteString("            </tr>\n")
	b.WriteString("          </thead>\n")
	b.WriteString("          <tbody class=\"bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700\">\n")
	for i := 1; i <= 4; i++ {
		b.WriteString("            <tr class=\"hover:bg-gray-50 dark:hover:bg-gray-700/50\">\n")
		for ci, c := range cols {
			fmt.Fprintf(&b, "              <td class=\"px-6 py-4 whitespace-nowrap text-sm text-gray-900 dark:text-gray-100\">%s</td>\n", fakeCell(c, i, ci))
		}
		b.WriteString("            </tr>\n")
	}
	b.WriteString("          </tbody>\n")
	b.WriteString("        </table>\n")
	b.WriteString("      </div>\n")
	return b.String()
}

// previewList renders a list widget card (label/value rows).
func previewList(w types.Widget) string {
	label := w.Label
	if label == "" {
		label = "List"
	}
	var b strings.Builder
	b.WriteString("      <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg p-6 mb-6\">\n")
	fmt.Fprintf(&b, "        <h3 class=\"text-lg font-semibold mb-4 text-gray-900 dark:text-gray-100\">%s</h3>\n", escHTML(label))
	b.WriteString("        <ul class=\"divide-y divide-gray-200 dark:divide-gray-700\">\n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&b, "          <li class=\"py-3 flex items-center justify-between\"><span class=\"text-sm text-gray-900 dark:text-gray-100\">%s</span><span class=\"text-sm text-gray-500 dark:text-gray-400\">%s</span></li>\n",
			escHTML(fakeWords(i)), fakeNumber(label+"row"+fmt.Sprint(i)))
	}
	b.WriteString("        </ul>\n")
	b.WriteString("      </div>\n")
	return b.String()
}

// previewHTML renders a placeholder note for raw HTML widgets (content comes
// from the database at runtime, so there is nothing real to show in preview).
func previewHTML() string {
	return `      <div class="bg-white dark:bg-gray-800 shadow rounded-lg p-6 mb-6 text-gray-900 dark:text-gray-100">
        <p class="text-sm text-gray-500 dark:text-gray-400">Raw HTML widget — content is fetched from the database at runtime and is not shown in preview.</p>
      </div>
`
}

// resourcePreviewBody renders a mock of a resource list view: search bar,
// a table over the list columns with fake rows, and pagination.
func resourcePreviewBody(cfg *types.Config, resName string) string {
	var res *types.Resource
	for i := range cfg.Resources {
		if cfg.Resources[i].Name == resName {
			res = &cfg.Resources[i]
			break
		}
	}
	var b strings.Builder
	b.WriteString("      <div class=\"p-6\">\n")
	if res == nil {
		b.WriteString("        <p class=\"text-gray-500 dark:text-gray-400\">Select a resource from the Preview tab toolbar.</p>\n")
		b.WriteString("      </div>\n")
		return b.String()
	}
	title := res.Label
	if title == "" {
		title = res.Name
	}
	fmt.Fprintf(&b, "        <div class=\"flex items-center justify-between mb-6\">\n")
	fmt.Fprintf(&b, "          <h1 class=\"text-2xl font-bold text-gray-900 dark:text-gray-100\">%s</h1>\n", escHTML(title))
	b.WriteString("          <a href=\"#\" class=\"bg-brand-primary text-white px-4 py-2 rounded-md text-sm hover:opacity-90\">+ Add</a>\n")
	b.WriteString("        </div>\n")
	b.WriteString("        <div class=\"mb-4\">\n")
	b.WriteString("          <form method=\"GET\" class=\"flex gap-2\">\n")
	b.WriteString("            <input type=\"text\" name=\"search\" placeholder=\"Search...\" class=\"w-64 rounded-md border-gray-300 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 shadow-sm focus:border-brand-primary focus:ring-brand-primary sm:text-sm border px-3 py-2\" />\n")
	b.WriteString("            <button type=\"submit\" class=\"bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-200 px-4 py-2 rounded-md text-sm hover:bg-gray-200 dark:hover:bg-gray-600 border dark:border-gray-600\">Search</button>\n")
	b.WriteString("          </form>\n")
	b.WriteString("        </div>\n")
	b.WriteString("        <div class=\"bg-white dark:bg-gray-800 shadow rounded-lg overflow-hidden mb-6\">\n")
	b.WriteString("          <table class=\"min-w-full divide-y divide-gray-200 dark:divide-gray-700\">\n")
	b.WriteString("            <thead class=\"bg-gray-50 dark:bg-gray-900\">\n")
	b.WriteString("              <tr>\n")
	cols := resourceListColumns(res)
	for _, c := range cols {
		head := c.Label
		if head == "" {
			head = c.Name
		}
		fmt.Fprintf(&b, "                <th class=\"px-6 py-3 text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider\">%s</th>\n", escHTML(head))
	}
	b.WriteString("                <th class=\"px-6 py-3 text-right\"></th>\n")
	b.WriteString("              </tr>\n")
	b.WriteString("            </thead>\n")
	b.WriteString("            <tbody class=\"bg-white dark:bg-gray-800 divide-y divide-gray-200 dark:divide-gray-700\">\n")
	for i := 1; i <= 4; i++ {
		b.WriteString("              <tr class=\"hover:bg-gray-50 dark:hover:bg-gray-700/50\">\n")
		for _, c := range cols {
			b.WriteString("                <td class=\"px-6 py-4 whitespace-nowrap text-sm text-gray-900 dark:text-gray-100\">" + fakeTypedCell(c.Type, i) + "</td>\n")
		}
		b.WriteString("                <td class=\"px-6 py-4 whitespace-nowrap text-sm text-right\">\n")
		b.WriteString("                  <a href=\"#\" class=\"text-brand-primary hover:text-brand-primary/80 underline mr-3\">View</a>\n")
		b.WriteString("                  <a href=\"#\" class=\"text-brand-primary hover:text-brand-primary/80 underline mr-3\">Edit</a>\n")
		b.WriteString("                  <a href=\"#\" class=\"text-red-600 hover:text-red-800 underline\">Delete</a>\n")
		b.WriteString("                </td>\n")
		b.WriteString("              </tr>\n")
	}
	b.WriteString("            </tbody>\n")
	b.WriteString("          </table>\n")
	b.WriteString("          <div class=\"bg-white dark:bg-gray-800 px-4 py-3 flex items-center justify-between border-t dark:border-gray-700\">\n")
	b.WriteString("            <div class=\"text-sm text-gray-700 dark:text-gray-300\">Showing page 1 of 2 (12 total)</div>\n")
	b.WriteString("            <div class=\"flex gap-1\">\n")
	b.WriteString("              <span class=\"px-3 py-1 border rounded text-sm bg-brand-primary text-white\">1</span>\n")
	b.WriteString("              <a href=\"#\" class=\"px-3 py-1 border rounded text-sm hover:bg-gray-50 dark:hover:bg-gray-700 dark:border-gray-600\">2</a>\n")
	b.WriteString("              <a href=\"#\" class=\"px-3 py-1 border rounded text-sm hover:bg-gray-50 dark:hover:bg-gray-700 dark:border-gray-600\">Next</a>\n")
	b.WriteString("            </div>\n")
	b.WriteString("          </div>\n")
	b.WriteString("        </div>\n")
	b.WriteString("      </div>\n")
	return b.String()
}

// resourceListColumns returns the list columns of a resource, falling back to
// a single id column when none are declared.
func resourceListColumns(res *types.Resource) []types.Column {
	if res != nil && res.List != nil && len(res.List.Columns) > 0 {
		return res.List.Columns
	}
	return []types.Column{{Name: "id", Type: "integer"}}
}

// fakeTypedCell renders a plausible cell value for the given column type.
func fakeTypedCell(t string, row int) string {
	switch t {
	case "integer", "float":
		return fakeNumber(t + fmt.Sprint(row))
	case "boolean":
		if row%2 == 0 {
			return `<span class="text-green-600"><svg class="w-5 h-5 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" /></svg></span>`
		}
		return `<span class="text-red-600"><svg class="w-5 h-5 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" /></svg></span>`
	case "datetime":
		return fmt.Sprintf("Jan %02d, 2026 14:%02d", row, 5+row)
	case "date":
		return fmt.Sprintf("2026-01-%02d", row)
	case "badge":
		return `<span class="inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-gray-100 text-gray-800 dark:bg-gray-900/50 dark:text-gray-300">active</span>`
	case "email":
		return fmt.Sprintf("<a href=\"#\" class=\"text-brand-primary hover:text-brand-primary/80 underline\">user%d@example.com</a>", row)
	case "password":
		return "••••••••"
	case "image":
		return `<span class="inline-block w-10 h-10 rounded-full bg-gray-200 dark:bg-gray-700"></span>`
	case "json":
		return `{ "key": "value" }`
	default:
		return fakeWords(row)
	}
}

// fakeCell is a plain-text fake cell for table widgets.
func fakeCell(col string, row int, idx int) string {
	switch {
	case strings.Contains(strings.ToLower(col), "id") || strings.Contains(strings.ToLower(col), "count"):
		return fakeNumber(col + fmt.Sprint(row))
	case strings.Contains(strings.ToLower(col), "date") || strings.Contains(strings.ToLower(col), "time"):
		return fmt.Sprintf("2026-0%d-%02d", idx+1, row)
	case strings.Contains(strings.ToLower(col), "total") || strings.Contains(strings.ToLower(col), "price") || strings.Contains(strings.ToLower(col), "amount"):
		return fmt.Sprintf("$%d.%02d", 1000+row*123, row*17)
	default:
		return fakeWords(row + idx)
	}
}

// fakeWords returns a deterministic filler word.
func fakeWords(seed int) string {
	words := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"}
	extra := []string{" Lorem", " ipsum", " dolor", " sit", " amet", " consectetur"}
	w := words[seed%len(words)]
	if seed >= len(words) {
		w += extra[(seed/len(words))%len(extra)]
	}
	return w
}

// fakeNumber returns a stable 4-6 digit fake number with thousands separators,
// derived from a string seed so a given widget always shows the same value.
func fakeNumber(seed string) string {
	h := 0
	for _, r := range seed {
		h = (h*31 + int(r)) & 0x7fffffff
	}
	v := 1200 + h%888000
	digits := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return b.String()
}

// fakeLabels returns fake month-ish labels for a chart, seeded by the widget
// label so a given chart always shows the same series.
func fakeLabels(seed string) []string {
	base := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug"}
	n := 6
	if h := hashSeed(seed) % 3; h == 1 {
		n = 7
	} else if h == 2 {
		n = 8
	}
	out := make([]string, n)
	copy(out, base[:n])
	return out
}

// fakeValues returns deterministic chart values between 40 and 95.
func fakeValues(seed string) []int {
	n := len(fakeLabels(seed))
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = 40 + (hashSeed(seed+fmt.Sprint(i)) % 56)
	}
	return out
}

func hashSeed(s string) int {
	h := 0
	for _, r := range s {
		h = (h*31 + int(r)) & 0x7fffffff
	}
	return h
}

func escHTML(s string) string { return html.EscapeString(s) }
func escAttr(s string) string { return html.EscapeString(s) }
func escCSS(s string) string  { return strings.ReplaceAll(s, "</", "<\\/") }

// navIconSVG renders a small sidebar group icon (mirrors iconNav).
func navIconSVG(name string) string {
	p := iconPath(name)
	if p == "" {
		return ""
	}
	return `<svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="` + p + `"/></svg>`
}

// previewIconSVG renders a large widget icon (mirrors iconSVG).
func previewIconSVG(name string) string {
	p := iconPath(name)
	if p == "" {
		return ""
	}
	return `              <svg class="w-6 h-6 text-brand-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="` + p + `" /></svg>
`
}

// iconPath returns the SVG path data for a known icon name ("" when unknown).
func iconPath(name string) string {
	switch name {
	case "users":
		return "M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"
	case "chart":
		return "M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
	case "cog":
		return "M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"
	case "home":
		return "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"
	case "clock":
		return "M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"
	case "mail":
		return "M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
	case "dollar":
		return "M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
	case "check":
		return "M5 13l4 4L19 7"
	case "bell":
		return "M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9"
	case "lock":
		return "M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z"
	default:
		return ""
	}
}
