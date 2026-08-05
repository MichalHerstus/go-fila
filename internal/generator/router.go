// router.go
//
// Generates the router for the admin panel application (internal/panel/router.go)
// and the HTTP handler for each custom page (internal/panel/pages).
// The router wires chi middleware, static file serving, login/logout routes,
// per-resource CRUD routes (with optional RBAC wrapping) and page routes under
// the configured panel path.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// generateRouter writes internal/panel/router.go. It builds the import list
// (database/sql, chi, auth, pages and one resource package per resource), then
// emits the NewRouter function with middleware, static file handling, the
// login/logout endpoints, CRUD routes per resource and page routes. Resources
// with policies get their routes wrapped in auth.RBACMiddleware; action
// routes are always plain POST. Returns an error on write failure.
func (g *Generator) generateRouter() error {
	var importPaths []string
	importPaths = append(importPaths, `"database/sql"`, `"net/http"`)
	importPaths = append(importPaths, `"github.com/go-chi/chi/v5"`, `"github.com/go-chi/chi/v5/middleware"`)
	importPaths = append(importPaths, fmt.Sprintf("%q", g.moduleImport("internal/panel/auth")))
	importPaths = append(importPaths, fmt.Sprintf("%q", g.moduleImport("internal/panel/pages")))

	for _, r := range g.Config.Resources {
		name := strings.ToLower(r.Name)
		importPaths = append(importPaths, fmt.Sprintf("%q", g.moduleImport("internal/panel/resources/"+name)))
	}

	code := "package panel\n\nimport (\n"
	for _, ip := range importPaths {
		code += "\t" + ip + "\n"
	}
	code += ")\n\n"

	code += `func NewRouter(db *sql.DB) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(auth.SessionMiddleware)

	fileServer := http.FileServer(http.Dir("static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))
	r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir("static/uploads"))))

	panelPath := "` + g.Config.Panel.Path + `"

	r.Route(panelPath, func(r chi.Router) {
		r.Get("/login", auth.LoginHandler(db))
		r.Post("/login", auth.LoginHandler(db))

		r.Group(func(r chi.Router) {
			r.Use(auth.AuthMiddleware)
`

	for _, res := range g.Config.Resources {
		name := strings.ToLower(res.Name)
		rbacPrefix := func(action string) string {
			if res.Policies != nil {
				return fmt.Sprintf("r.With(auth.RBACMiddleware(%q, %q)).", name, action)
			}
			return "r."
		}
		if res.List != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s\", %s.List(db))\n", rbacPrefix("view_any"), name, name)
		}
		if res.Card != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s/cards\", %s.Cards(db))\n", rbacPrefix("view_any"), name, name)
		}
		if res.Form != nil && res.Form.Create != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s/new\", %s.Create(db))\n", rbacPrefix("create"), name, name)
			code += fmt.Sprintf("\t\t%sPost(\"/%s/new\", %s.Create(db))\n", rbacPrefix("create"), name, name)
		}
		if res.Detail != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s/{id}\", %s.Detail(db))\n", rbacPrefix("view"), name, name)
		}
		if res.Form != nil && res.Form.Update != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s/{id}/edit\", %s.Update(db))\n", rbacPrefix("update"), name, name)
			code += fmt.Sprintf("\t\t%sPost(\"/%s/{id}\", %s.Update(db))\n", rbacPrefix("update"), name, name)
		}
		if res.Form != nil && res.Form.Delete != nil {
			code += fmt.Sprintf("\t\t%sPost(\"/%s/{id}/delete\", %s.Delete(db))\n", rbacPrefix("delete"), name, name)
		}
		if len(res.Actions) > 0 {
			code += fmt.Sprintf("\t\tr.Post(\"/%s/{id}/action/{action}\", %s.Action(db))\n", name, name)
		}
		if hasBulkActions(res) {
			code += fmt.Sprintf("\t\tr.Post(\"/%s/bulk/{action}\", %s.Bulk(db))\n", name, name)
		}
		if res.List != nil {
			code += fmt.Sprintf("\t\t%sGet(\"/%s/export/csv\", %s.ExportCSV(db))\n", rbacPrefix("view_any"), name, name)
		}
	}

	for _, p := range g.Config.Pages {
		capID := strings.ToUpper(g.Config.Panel.ID[:1]) + g.Config.Panel.ID[1:]
		handlerName := fmt.Sprintf("pages.%s%s(db)", capID, p.Name)
		if p.Default {
			code += fmt.Sprintf("\t\tr.Get(\"/\", %s)\n", handlerName)
		}
		path := p.Path
		if path == "" {
			path = "/" + p.Name
		}
		if path != "/" {
			code += fmt.Sprintf("\t\tr.Get(\"%s\", %s)\n", path, handlerName)
		}
	}

	code += fmt.Sprintf("\t\tr.Get(\"/logout\", auth.LogoutHandler(db))\n")
	code += "\t\tr.Post(\"/logout\", auth.LogoutHandler(db))\n"

	code += `		})
	})

	return r
}
`

	dir := filepath.Join(g.OutDir, "internal/panel")
	return os.WriteFile(filepath.Join(dir, "router.go"), []byte(code), 0644)
}

// generatePage writes one page handler per configured Page. The handler
// initializes an empty widget list, runs the DB queries declared by each
// widget (stat, chart, table, stats_grid), packs them into a viewmodels.PageData
// and renders the matching templ page view.
// Params: p (the Page definition to generate a handler for).
// Returns: an error on write failure.
func (g *Generator) generatePage(p types.Page) error {
	dir := filepath.Join(g.OutDir, "internal/panel/pages")
	name := p.Name
	panelID := g.Config.Panel.ID
	panelPath := g.Config.Panel.Path
	capitalID := strings.ToUpper(panelID[:1]) + panelID[1:]
	handlerName := capitalID + name
	viewName := capitalID + name

	var widgetInit []string
	for i, w := range p.Widgets {
		switch w.Type {
		case "stat":
			var valExpr string
			if w.Query != "" {
				valExpr = fmt.Sprintf(`template.HTML(fmt.Sprintf("%%s%%d%%s", %q, count%d, %q))`, w.Prefix, i, "")
			} else {
				valExpr = `"0"`
			}
			widgetInit = append(widgetInit, fmt.Sprintf(`
        var count%d int64
        if q := %q; q != "" {
            _ = db.QueryRowContext(r.Context(), q).Scan(&count%d)
        }
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "stat",
            Label: %q,
            Color: %q,
            Icon: %q,
            Value: %s,
        })`, i, w.Query, i, w.Label, w.Color, w.Icon, valExpr))
		case "chart":
			widgetInit = append(widgetInit, fmt.Sprintf(`
        {
        var chartLabels []string
        var chartValues []float64
        if q := %q; q != "" {
            rows, err := db.QueryContext(r.Context(), q)
            if err == nil {
                defer rows.Close()
                for rows.Next() {
                    var label string
                    var val float64
                    if err := rows.Scan(&label, &val); err == nil {
                        chartLabels = append(chartLabels, label)
                        chartValues = append(chartValues, val)
                    }
                }
            }
        }
        chartLabelsJSON, _ := json.Marshal(chartLabels)
        chartValuesJSON, _ := json.Marshal(chartValues)
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "chart",
            Label: %q,
            ChartType: %q,
            ChartLabelsJSON: string(chartLabelsJSON),
            ChartValuesJSON: string(chartValuesJSON),
        })
        }`, w.Query, w.Label, w.Chart.Type))
		case "table":
			colList := "[]string{"
			for _, col := range w.DataColumns {
				colList += fmt.Sprintf("%q, ", col)
			}
			colList += "}"
			widgetInit = append(widgetInit, fmt.Sprintf(`
        {
        var tableRows []map[string]interface{}
        if q := %q; q != "" {
            dataRows, err := db.QueryContext(r.Context(), q)
            if err == nil {
                defer dataRows.Close()
                dCols, _ := dataRows.Columns()
                for dataRows.Next() {
                    vals := make([]interface{}, len(dCols))
                    valPtrs := make([]interface{}, len(dCols))
                    for i := range vals {
                        valPtrs[i] = &vals[i]
                    }
                    if err := dataRows.Scan(valPtrs...); err == nil {
                        row := make(map[string]interface{})
                        for i, col := range dCols {
                            row[col] = vals[i]
                        }
                        tableRows = append(tableRows, row)
                    }
                }
            }
        }
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "table",
            Label: %q,
            TableColumns: %s,
            TableRows: tableRows,
        })
        }`, w.Query, w.Label, colList))
		case "stats_grid":
			widgetInit = append(widgetInit, fmt.Sprintf(`
        var subWidgets%d []viewmodels.WidgetData`, i))
			for _, sw := range w.Widgets {
				widgetInit = append(widgetInit, fmt.Sprintf(`
        {
            var subCount int64
            if q := %q; q != "" {
                _ = db.QueryRowContext(r.Context(), q).Scan(&subCount)
            }
            subWidgets%d = append(subWidgets%d, viewmodels.WidgetData{
                Type: "stat",
                Label: %q,
                Color: %q,
                Icon: %q,
                Value: template.HTML(fmt.Sprintf("%%d", subCount)),
            })
        }`, sw.Query, i, i, sw.Label, sw.Color, sw.Icon))
			}
			widgetInit = append(widgetInit, fmt.Sprintf(`
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "stats_grid",
            SubWidgets: subWidgets%d,
        })`, i))
		case "list":
			widgetInit = append(widgetInit, fmt.Sprintf(`
        {
        var listRows []map[string]interface{}
        if q := %q; q != "" {
            listQueryRows, err := db.QueryContext(r.Context(), q)
            if err == nil {
                defer listQueryRows.Close()
                listCols, _ := listQueryRows.Columns()
                for listQueryRows.Next() {
                    listVals := make([]interface{}, len(listCols))
                    listPtrs := make([]interface{}, len(listCols))
                    for i := range listVals {
                        listPtrs[i] = &listVals[i]
                    }
                    if err := listQueryRows.Scan(listPtrs...); err == nil {
                        listRow := make(map[string]interface{})
                        for i, col := range listCols {
                            listRow[col] = listVals[i]
                        }
                        listRows = append(listRows, listRow)
                    }
                }
            }
        }
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "list",
            Label: %q,
            TableRows: listRows,
        })
        }`, w.Query, w.Label))
		case "html":
			widgetInit = append(widgetInit, fmt.Sprintf(`
        {
        var htmlVal string
        if q := %q; q != "" {
            _ = db.QueryRowContext(r.Context(), q).Scan(&htmlVal)
        }
        widgets = append(widgets, viewmodels.WidgetData{
            Type: "html",
            Label: %q,
            Value: template.HTML(htmlVal),
        })
        }`, w.Query, w.Label))
		}
	}

	jsonImport := ""
	for _, w := range p.Widgets {
		if w.Type == "chart" {
			jsonImport = `    "encoding/json"
`
			break
		}
	}

	code := fmt.Sprintf(`package pages

import (
    "database/sql"
%s    "fmt"
    "html/template"
    "net/http"

    %q
    layoutviews %q
    pageviews %q
)

func %s(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var widgets []viewmodels.WidgetData

        %s

        pd := &viewmodels.PageData{
            Name:      %q,
            PanelID:   %q,
            PanelPath: %q,
            Widgets:   widgets,
        }

        err := layoutviews.Base(pd.Name, pd.PanelPath, viewmodels.DefaultTheme(), pageviews.%s(pd)).Render(r.Context(), w)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
    }
}
`, jsonImport, g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/layout"), g.moduleImport("internal/views/pages"),
		handlerName, strings.Join(widgetInit, "\n"), name, panelID, panelPath, viewName)

	return os.WriteFile(filepath.Join(dir, name+".go"), []byte(code), 0644)
}
