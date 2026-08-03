// templ.go
//
// Generates all the .templ views of the admin panel application: per-resource
// list/detail/form views, per-page widget views, the shared layout
// (base.templ with sidebar/topbar), and reusable components
// (renderers.templ with field renderers, search bar, sort icon and
// pagination). All views declare package views.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// prefixImports rewrites bare "internal/..." import paths in generated source
// so they are module-qualified, matching the module name written to go.mod.
// Params: code (generated Go or templ source), moduleImport (the module-
// qualified import path to substitute).
// Returns: the source with the bare import rewritten.
func prefixImports(code string, moduleImport string) string {
	if strings.Contains(code, "fmt.") && !strings.Contains(code, `"fmt"`) {
		code = strings.Replace(code, `import "internal/viewmodels"`, "import (\n    \"fmt\"\n    \"internal/viewmodels\"\n)", 1)
	}
	return strings.ReplaceAll(code, `"internal/viewmodels"`, fmt.Sprintf("%q", moduleImport))
}

// generateViews runs all templ generation steps: one view set per resource,
// one view per page, then the layout and component views.
// Returns: an error if any step fails.
func (g *Generator) generateViews() error {
	for _, r := range g.Config.Resources {
		if err := g.generateResourceViews(r); err != nil {
			return err
		}
	}
	if len(g.Config.Pages) > 0 {
		if err := g.generatePageWidgets(); err != nil {
			return err
		}
	}
	for _, p := range g.Config.Pages {
		if err := g.generatePageViews(p); err != nil {
			return err
		}
	}
	if err := g.generateLayoutViews(); err != nil {
		return err
	}
	if err := g.generateComponentViews(); err != nil {
		return err
	}
	return nil
}

// generateResourceViews writes the list/detail/form templ files for a single
// resource into internal/views/resources/{resource}/, one per declared section.
// The shared renderer components (renderBadge, searchBar, pagination, etc.) are
// emitted into the same directory so every resource view package is
// self-contained.
// Params: r (the resource definition).
// Returns: an error if any templ file fails to write.
func (g *Generator) generateResourceViews(r types.Resource) error {
	viewDir := filepath.Join(g.OutDir, "internal/views/resources", strings.ToLower(r.Name))
	if err := os.WriteFile(filepath.Join(viewDir, "renderers.templ"), []byte(renderersSource()), 0644); err != nil {
		return err
	}
	if r.List != nil {
		if err := g.generateListTempl(viewDir, r); err != nil {
			return err
		}
	}
	if r.Detail != nil {
		if err := g.generateDetailTempl(viewDir, r); err != nil {
			return err
		}
	}
	if r.Form != nil {
		if err := g.generateFormTempl(viewDir, r); err != nil {
			return err
		}
	}
	if r.Card != nil {
		if err := g.generateCardTempl(viewDir, r); err != nil {
			return err
		}
	}
	return nil
}

// renderCell returns the templ expression used to display a cell value based
// on its field type, delegating to the matching renderer component in
// renderers.templ (badge, boolean, email, image, file, datetime, date,
// select, relation, json, float) or emitting the raw value for plain types.
// Params: fieldType (the column/field type), expr (the templ expression that
// yields the value, e.g. `item["name"]`).
// Returns: the templ expression string for the cell.
func renderCell(fieldType, expr string) string {
	switch fieldType {
	case "badge":
		return fmt.Sprintf(`@renderBadge(%s, "")`, expr)
	case "boolean":
		return fmt.Sprintf(`@renderBoolean(%s)`, expr)
	case "email":
		return fmt.Sprintf(`@renderEmail(%s)`, expr)
	case "image":
		return fmt.Sprintf(`@renderImage(%s)`, expr)
	case "file":
		return fmt.Sprintf(`@renderFile(%s)`, expr)
	case "datetime":
		return fmt.Sprintf(`@renderDateTime(%s)`, expr)
	case "date":
		return fmt.Sprintf(`@renderDate(%s)`, expr)
	case "select":
		return fmt.Sprintf(`@renderSelect(%s)`, expr)
	case "relation":
		return fmt.Sprintf(`@renderRelation(%s)`, expr)
	case "json":
		return fmt.Sprintf(`@renderJSON(%s)`, expr)
	case "float":
		return fmt.Sprintf(`@renderFloat(%s)`, expr)
	case "gps":
		return fmt.Sprintf(`@renderGPS(%s)`, expr)
	case "integer", "string", "text", "password":
		return fmt.Sprintf(`{ fmt.Sprintf("%%v", %s) }`, expr)
	default:
		return fmt.Sprintf(`{ fmt.Sprintf("%%v", %s) }`, expr)
	}
}

// generateListTempl writes list.templ for a resource: a table with sortable
// headers, per-row action forms (view/edit/custom actions/delete), a create
// and CSV export button, the search bar and pagination.
// Params: dir (view directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateListTempl(dir string, r types.Resource) error {
	cols := r.List.Columns
	templName := r.Name + "List"
	resLabel := r.Label
	resLower := strings.ToLower(r.Name)
	panelPath := g.Config.Panel.Path

	var headers strings.Builder
	var cells strings.Builder

	for _, c := range cols {
		label := c.Label
		if label == "" {
			label = c.Name
		}
		if c.Sortable {
			headers.WriteString(fmt.Sprintf(`            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                <a href={ templ.SafeURL(fmt.Sprintf("?sort=%%s&order=%%s", %q, sortOrder(data.Sort, %q, data.Order))) } class="flex items-center gap-1 hover:text-gray-700">
                    %s
                    @sortIcon(data.Sort, %q, data.Order)
                </a>
            </th>
`, c.Name, c.Name, label, c.Name))
		} else {
			headers.WriteString(fmt.Sprintf(`            <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">%s</th>
`, label))
		}
		rendered := renderCell(c.Type, fmt.Sprintf(`item[%q]`, c.Name))
		cells.WriteString(fmt.Sprintf(`            <td class="px-6 py-4 whitespace-nowrap text-sm">%s</td>
`, rendered))
	}

	var extraActions string
	for _, a := range r.Actions {
		extraActions += fmt.Sprintf(`                <form action={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/action/%%s", %q, %q, item["id"], %q)) } method="POST" class="inline">
                    <button type="submit" class="text-indigo-600 hover:text-indigo-900 text-sm mr-2">%s</button>
                </form>
`, panelPath, resLower, a.Name, a.Label)
	}
	if r.Form != nil && r.Form.Delete != nil {
		extraActions += fmt.Sprintf(`                <form action={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/delete", %q, %q, item["id"])) } method="POST" class="inline" onsubmit="return confirm('Delete?')">
                    <button type="submit" class="text-red-600 hover:text-red-900 text-sm">Delete</button>
                </form>
`, panelPath, resLower)
	}

	actionsCol := fmt.Sprintf(`            <td class="px-6 py-4 whitespace-nowrap text-right text-sm font-medium">
                <a href={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v", %q, %q, item["id"])) } class="text-indigo-600 hover:text-indigo-900 mr-3">View</a>
                <a href={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/edit", %q, %q, item["id"])) } class="text-indigo-600 hover:text-indigo-900 mr-3">Edit</a>
%s            </td>
`, panelPath, resLower, panelPath, resLower, extraActions)

	createBtn := fmt.Sprintf(`<a href="%s/%s/new" class="bg-indigo-600 text-white px-4 py-2 rounded-lg text-sm hover:bg-indigo-700">Create %s</a>`, panelPath, resLower, resLabel)
	exportBtn := fmt.Sprintf(`<a href="?export=csv" class="text-gray-600 hover:text-gray-900 px-4 py-2 text-sm">Export CSV</a>`)

	cardBtn := ""
	if r.Card != nil {
		cardBtn = fmt.Sprintf(`<a href="%s/%s/cards" class="text-gray-600 hover:text-gray-900 px-4 py-2 text-sm">Cards</a>`, panelPath, resLower)
	}

	headerBtns := createBtn + " " + exportBtn
	if cardBtn != "" {
		headerBtns = createBtn + " " + cardBtn + " " + exportBtn
	}

	code := fmt.Sprintf(`package views

import "internal/viewmodels"

templ %s(data *viewmodels.ListData) {
    <div class="p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold text-gray-900">%s</h1>
            <div class="flex gap-2 items-center">
                %s
            </div>
        </div>

        @searchBar(data.Search, data.Resource)

        <div class="bg-white shadow rounded-lg overflow-hidden">
            <table class="min-w-full divide-y divide-gray-200">
                <thead class="bg-gray-50">
                    <tr>
%s                        <th class="px-6 py-3 text-right text-xs font-medium text-gray-500 uppercase tracking-wider">Actions</th>
                    </tr>
                </thead>
                <tbody class="bg-white divide-y divide-gray-200">
                    for _, item := range data.Items {
                    <tr class="hover:bg-gray-50">
%s%s                    </tr>
                    }
                </tbody>
            </table>

            @pagination(data.Page, data.TotalPages, data.Total, data.Search, data.Sort, data.Order)
        </div>
    </div>
}
`, templName, resLabel, headerBtns, headers.String(), cells.String(), actionsCol)
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(dir, "list.templ"), []byte(code), 0644)
}

// generateDetailTempl writes detail.templ for a resource: a read-only table
// of the detail fields plus action buttons (edit, custom actions, delete).
// Params: dir (view directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateDetailTempl(dir string, r types.Resource) error {
	resName := r.Name
	templName := resName + "Detail"
	resLower := strings.ToLower(resName)
	panelPath := g.Config.Panel.Path

	var rows strings.Builder
	for _, f := range r.Detail.Fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		rendered := renderCell(f.Type, fmt.Sprintf(`data.Item[%q]`, f.Name))
		rows.WriteString(fmt.Sprintf(`                <tr>
                    <td class="px-6 py-4 whitespace-nowrap text-sm font-medium text-gray-500 w-1/4">%s</td>
                    <td class="px-6 py-4 whitespace-nowrap text-sm text-gray-900">%s</td>
                </tr>
`, label, rendered))
	}

	var actionBtns strings.Builder
	for _, a := range r.Actions {
		actionBtns.WriteString(fmt.Sprintf(`                <form action={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/action/%%s", %q, %q, data.Item["id"], %q)) } method="POST" class="inline">
                    <button type="submit" class="%s px-4 py-2 rounded-lg text-sm hover:opacity-90">%s</button>
                </form>
`, panelPath, resLower, a.Name, actionColor(a.Color), a.Label))
	}
	if r.Form != nil && r.Form.Delete != nil {
		actionBtns.WriteString(fmt.Sprintf(`                <form action={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/delete", %q, %q, data.Item["id"])) } method="POST" class="inline" onsubmit="return confirm('Delete this %s?')">
                    <button type="submit" class="bg-red-600 text-white px-4 py-2 rounded-lg text-sm hover:bg-red-700">Delete</button>
                </form>
`, panelPath, resLower, resName))
	}

	code := fmt.Sprintf(`package views

import "internal/viewmodels"

templ %s(data *viewmodels.DetailData) {
    <div class="p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold text-gray-900">%s Details</h1>
            <div class="flex gap-2">
                <a href="%s/%s" class="text-gray-600 hover:text-gray-900 px-4 py-2">Back</a>
                <a href={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/edit", %q, %q, data.Item["id"])) } class="bg-indigo-600 text-white px-4 py-2 rounded-lg text-sm hover:bg-indigo-700">Edit</a>
%s            </div>
        </div>

        <div class="bg-white shadow rounded-lg overflow-hidden">
            <table class="min-w-full divide-y divide-gray-200">
                <tbody class="divide-y divide-gray-200">
%s                </tbody>
            </table>
        </div>
    </div>
}
`, templName, resName, panelPath, resLower, panelPath, resLower, actionBtns.String(), rows.String())
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(dir, "detail.templ"), []byte(code), 0644)
}

// actionColor maps a semantic action color (success, danger, warning,
// primary, info or any of their aliases) to the Tailwind button classes used
// on action buttons. Unknown colors fall back to gray.
// Params: c (the configured color name).
// Returns: the Tailwind class string for the button.
func actionColor(c string) string {
	switch c {
	case "success", "green":
		return "bg-green-600 text-white"
	case "danger", "red":
		return "bg-red-600 text-white"
	case "warning", "yellow":
		return "bg-yellow-500 text-white"
	case "primary", "indigo":
		return "bg-indigo-600 text-white"
	case "info", "blue":
		return "bg-blue-600 text-white"
	default:
		return "bg-gray-600 text-white"
	}
}

// generateFormTempl writes form.templ for a resource: a shared create/update
// form rendered from the create fields (when present) or update fields. It
// emits the appropriate input widget per field type, adds a multipart enctype
// when any field is a file/image, and shows validation hints for required
// fields.
// Params: dir (view directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateFormTempl(dir string, r types.Resource) error {
	templName := r.Name + "Form"
	resLabel := r.Label
	resLower := strings.ToLower(r.Name)
	panelPath := g.Config.Panel.Path

	formFields := r.Form.Create
	isCreate := formFields != nil
	if !isCreate {
		formFields = r.Form.Update
	}
	if formFields == nil {
		formFields = &types.FormAction{}
	}

	var inputs strings.Builder

	for _, f := range formFields.Fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		if isCreate && len(f.Visible) > 0 {
			hasCreate := false
			for _, v := range f.Visible {
				if v == "create" {
					hasCreate = true
					break
				}
			}
			if !hasCreate {
				continue
			}
		}

		inputs.WriteString(fmt.Sprintf(`            <div>
                <label for="%s" class="block text-sm font-medium text-gray-700 mb-1">%s</label>
`, f.Name, label))

		switch f.Type {
		case "text":
			inputs.WriteString(fmt.Sprintf(`                <textarea id="%s" name="%s" rows="3" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2">{ fmt.Sprintf("%%v", data.Item[%q]) }</textarea>
`, f.Name, f.Name, f.Name))
		case "password":
			inputs.WriteString(fmt.Sprintf(`                <input type="password" id="%s" name="%s" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name))
		case "email":
			inputs.WriteString(fmt.Sprintf(`                <input type="email" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name, f.Name))
		case "select":
			inputs.WriteString(fmt.Sprintf(`                <select id="%s" name="%s" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2">
                    <option value="">Select...</option>
                    for _, fd := range data.Fields {
                        if fd.Name == %q {
                            for key, label := range fd.Options {
                                <option value={ key } if viewmodels.OptionValue(data.Item[%q]) == key { selected }>{ label }</option>
                            }
                        }
                    }
                </select>
`, f.Name, f.Name, f.Name, f.Name))
		case "boolean":
			inputs.WriteString(fmt.Sprintf(`                <input type="checkbox" id="%s" name="%s" value="true" class="rounded border-gray-300 text-indigo-600 focus:ring-indigo-500"
                    if fmt.Sprintf("%%v", data.Item[%q]) == "true" {
                        checked
                    }
                />
`, f.Name, f.Name, f.Name))
		case "integer", "float":
			inputs.WriteString(fmt.Sprintf(`                <input type="number" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name, f.Name))
		case "datetime":
			inputs.WriteString(fmt.Sprintf(`                <input type="datetime-local" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name, f.Name))
		case "date":
			inputs.WriteString(fmt.Sprintf(`                <input type="date" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name, f.Name))
		case "file":
			inputs.WriteString(fmt.Sprintf(`                <input type="file" id="%s" name="%s" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name))
		case "image":
			inputs.WriteString(fmt.Sprintf(`                <input type="file" id="%s" name="%s" accept="image/*" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name))
		case "badge":
			inputs.WriteString(fmt.Sprintf(`                <input type="text" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" placeholder="badge value" />
`, f.Name, f.Name, f.Name))
		case "relation":
			inputs.WriteString(fmt.Sprintf(`                <input type="text" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" placeholder="related ID" />
`, f.Name, f.Name, f.Name))
		case "json":
			inputs.WriteString(fmt.Sprintf(`                <textarea id="%s" name="%s" rows="5" class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2 font-mono text-xs">{ fmt.Sprintf("%%v", data.Item[%q]) }</textarea>
`, f.Name, f.Name, f.Name))
		case "gps":
			inputs.WriteString(fmt.Sprintf(`                <input type="text" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" placeholder="lat, lng" />
`, f.Name, f.Name, f.Name))
		default:
			inputs.WriteString(fmt.Sprintf(`                <input type="text" id="%s" name="%s" value={ fmt.Sprintf("%%v", data.Item[%q]) } class="w-full rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
`, f.Name, f.Name, f.Name))
		}

		if f.Required {
			inputs.WriteString(`                <p class="text-xs text-red-500 mt-1">Required</p>
`)
		}
		if f.Validation != nil {
			if f.Validation.Min > 0 || f.Validation.Max > 0 {
				inputs.WriteString(fmt.Sprintf(`                <p class="text-xs text-gray-500 mt-1">Min: %d, Max: %d</p>
`, f.Validation.Min, f.Validation.Max))
			}
		}

		inputs.WriteString("            </div>\n")
	}

	hasFile := false
	for _, f := range formFields.Fields {
		if f.Type == "file" || f.Type == "image" {
			hasFile = true
			break
		}
	}

	enctype := ""
	if hasFile {
		enctype = ` enctype="multipart/form-data"`
	}

	actionPath := fmt.Sprintf("%s/%s", panelPath, resLower)
	listPath := fmt.Sprintf("%s/%s", panelPath, resLower)

	code := fmt.Sprintf(`package views

import "internal/viewmodels"

templ %s(data *viewmodels.FormData) {
    <div class="p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold text-gray-900">
                if data.IsCreate {
                    Create %s
                } else {
                    Edit %s
                }
            </h1>
            <a href="%s" class="text-gray-600 hover:text-gray-900 px-4 py-2">Back</a>
        </div>

        <div class="bg-white shadow rounded-lg p-6">
            <form action="%s" method="POST"%s class="space-y-6">
%s                <div class="flex justify-end pt-4">
                    <button type="submit" class="bg-indigo-600 text-white px-6 py-2 rounded-lg text-sm hover:bg-indigo-700">
                        if data.IsCreate {
                            Create
                        } else {
                            Update
                        }
                    </button>
                </div>
            </form>
        </div>
    </div>
}
`, templName, resLabel, resLabel, listPath, actionPath, enctype, inputs.String())
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(dir, "form.templ"), []byte(code), 0644)
}

// generateCardTempl writes cards.templ for a resource: a card grid view (or a
// kanban board when data.Kanban is true). Each card renders the configured
// fields stacked vertically with per-field renderers; the grid uses Tailwind's
// responsive columns so `data.Columns` cards fit per row. Grid mode shows the
// shared search bar and pagination; kanban mode renders columns side by side
// with the search bar only.
// Params: dir (view directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateCardTempl(dir string, r types.Resource) error {
	fields := r.Card.Fields
	templName := r.Name + "Cards"
	resLabel := r.Label
	resLower := strings.ToLower(r.Name)
	panelPath := g.Config.Panel.Path

	var cardBody strings.Builder
	for _, f := range fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		rendered := renderCell(f.Type, fmt.Sprintf(`item[%q]`, f.Name))
		cardBody.WriteString(fmt.Sprintf(`                    <div class="mb-2">
                        <span class="block text-xs font-medium text-gray-500">%s</span>
                        %s
                    </div>
`, label, rendered))
	}

	actions := fmt.Sprintf(`                    <div class="flex gap-2 border-t pt-3 mt-3">
                        <a href={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v", %q, %q, item["id"])) } class="text-indigo-600 hover:text-indigo-900 text-sm">View</a>
                        <a href={ templ.SafeURL(fmt.Sprintf("%%s/%%s/%%v/edit", %q, %q, item["id"])) } class="text-indigo-600 hover:text-indigo-900 text-sm">Edit</a>
                    </div>
`, panelPath, resLower, panelPath, resLower)

	kanbanField := ""
	if r.Card.KanbanField != "" {
		kanbanField = r.Card.KanbanField
	}

	// Header button: a "Create" link to the create form when configured,
	// otherwise a "View List" link back to the resource list.
	headerBtnURL := fmt.Sprintf("%s/%s", panelPath, resLower)
	headerBtnLabel := "View List"
	if r.Form != nil && r.Form.Create != nil {
		headerBtnURL = fmt.Sprintf("%s/%s/new", panelPath, resLower)
		headerBtnLabel = fmt.Sprintf("Create %s", resLabel)
	}
	headerBtn := fmt.Sprintf(`<a href="%s" class="bg-indigo-600 text-white px-4 py-2 rounded-lg text-sm hover:bg-indigo-700">%s</a>`, headerBtnURL, headerBtnLabel)

	gridView := fmt.Sprintf(`                <div class="grid gap-4 grid-cols-1 sm:grid-cols-2 lg:grid-cols-%d">
                    for _, item := range data.Items {
                        <div class="bg-white shadow rounded-lg p-4 border border-gray-200">
%s
%s                    </div>
                    }
                </div>
`, r.Card.Columns, cardBody.String(), actions)

	kanbanView := ""
	if kanbanField != "" {
		kanbanView = fmt.Sprintf(`                <div class="flex gap-4 overflow-x-auto pb-4">
                    for _, col := range data.KanbanColumns {
                        <div class="w-72 flex-shrink-0 bg-gray-50 rounded-lg p-3 border border-gray-200">
                            <div class="flex items-center justify-between mb-3">
                                <span class="text-sm font-medium text-gray-700">{ col.Label }</span>
                                <span class="text-xs text-gray-500">{ fmt.Sprintf("%%d", len(col.Items)) }</span>
                            </div>
                            for _, item := range col.Items {
                                <div class="bg-white shadow rounded-lg p-4 mb-3 border border-gray-200">
%s
%s                                </div>
                            }
                        </div>
                    }
                </div>
`, cardBody.String(), actions)
	}

	code := fmt.Sprintf(`package views

import "internal/viewmodels"

templ %s(data *viewmodels.CardData) {
    <div class="p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold text-gray-900">%s</h1>
            <div class="flex gap-2 items-center">
                <a href="%s/%s" class="text-gray-600 hover:text-gray-900 px-4 py-2 text-sm">Back</a>
                %s
            </div>
        </div>

        @searchBar(data.Search, data.Resource)

        %s

        if !data.Kanban {
            @pagination(data.Page, data.TotalPages, data.Total, data.Search, data.Sort, data.Order)
        }
    </div>
}
`, templName, resLabel, panelPath, resLower, headerBtn, gridView)
	if kanbanView != "" {
		code = fmt.Sprintf(`package views

import "internal/viewmodels"

templ %s(data *viewmodels.CardData) {
    <div class="p-6">
        <div class="flex items-center justify-between mb-6">
            <h1 class="text-2xl font-bold text-gray-900">%s</h1>
            <div class="flex gap-2 items-center">
                <a href="%s/%s" class="text-gray-600 hover:text-gray-900 px-4 py-2 text-sm">Back</a>
                %s
            </div>
        </div>

        @searchBar(data.Search, data.Resource)

        if data.Kanban {
%s        } else {
%s        }
    </div>
}
`, templName, resLabel, panelPath, resLower, headerBtn, kanbanView, gridView)
	}
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(dir, "cards.templ"), []byte(code), 0644)
}

// generatePageWidgets writes internal/views/pages/widgets.templ containing the
// templ components shared by every page view (widget, statWidget, iconSVG).
// The shared file is written once regardless of how many pages are configured;
// per-page view files only declare their page template and reference these
// components. The stats_grid column count is baked in from the first page that
// declares a stats_grid widget (defaulting to 4).
// Returns: an error on write failure.
func (g *Generator) generatePageWidgets() error {
	viewDir := filepath.Join(g.OutDir, "internal/views/pages")
	gridCols := 4
	for _, p := range g.Config.Pages {
		if c := g.detectGridColumns(p.Widgets); c != 4 {
			gridCols = c
			break
		}
	}
	code := fmt.Sprintf(`package views

import (
    "internal/viewmodels"
    "fmt"
)

templ widget(w viewmodels.WidgetData) {
    switch w.Type {
    case "stats_grid":
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-%d gap-4 mb-6">
            for _, sw := range w.SubWidgets {
                @statWidget(sw)
            }
        </div>
    case "stat":
        @statWidget(w)
    case "chart":
        <div class="bg-white shadow rounded-lg p-6 mb-6">
            <h3 class="text-lg font-semibold mb-4">{ w.Label }</h3>
            <canvas id={ fmt.Sprintf("chart-%%s", w.Label) } class="w-full h-64"
                data-chart-type={ w.ChartType }
                data-labels={ w.ChartLabelsJSON }
                data-values={ w.ChartValuesJSON }>
            </canvas>
        </div>
    case "table":
        <div class="bg-white shadow rounded-lg overflow-hidden mb-6">
            <div class="px-6 py-4 border-b">
                <h3 class="text-lg font-semibold">{ w.Label }</h3>
            </div>
            <table class="min-w-full divide-y divide-gray-200">
                <thead class="bg-gray-50">
                    <tr>
                        for _, col := range w.TableColumns {
                        <th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase tracking-wider">{ col }</th>
                        }
                    </tr>
                </thead>
                <tbody class="bg-white divide-y divide-gray-200">
                    for _, row := range w.TableRows {
                    <tr class="hover:bg-gray-50">
                        for _, col := range w.TableColumns {
                        <td class="px-6 py-4 whitespace-nowrap text-sm">{ fmt.Sprintf("%%v", row[col]) }</td>
                        }
                    </tr>
                    }
                </tbody>
            </table>
        </div>
    case "list":
        <div class="bg-white shadow rounded-lg p-6 mb-6">
            <h3 class="text-lg font-semibold mb-4">{ w.Label }</h3>
            <ul class="divide-y divide-gray-200">
                for _, row := range w.TableRows {
                <li class="py-3 flex items-center justify-between">
                    <span class="text-sm text-gray-900">{ fmt.Sprintf("%%v", row["label"]) }</span>
                    <span class="text-sm text-gray-500">{ fmt.Sprintf("%%v", row["value"]) }</span>
                </li>
                }
            </ul>
        </div>
    case "html":
        <div class="bg-white shadow rounded-lg p-6 mb-6">
            @templ.Raw(string(w.Value))
        </div>
    }
}

templ statWidget(w viewmodels.WidgetData) {
    <div class="bg-white shadow rounded-lg p-6">
        <div class="flex items-center justify-between">
            <div>
                if w.Icon != "" {
                <div class="w-10 h-10 rounded-lg bg-indigo-100 flex items-center justify-center mb-3">
                    @iconSVG(w.Icon)
                </div>
                }
                <p class="text-sm text-gray-500">{ w.Label }</p>
                <p class="text-2xl font-bold">
                    if w.Prefix != "" {
                        <span class="text-lg">{ w.Prefix }</span>
                    }
                    @templ.Raw(string(w.Value))
                    if w.Suffix != "" {
                        <span class="text-lg">{ w.Suffix }</span>
                    }
                </p>
            </div>
        </div>
    </div>
}

templ iconSVG(name string) {
    switch name {
    case "users":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z" />
        </svg>
    case "chart":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z" />
        </svg>
    case "dollar":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
    case "check":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
        </svg>
    case "cog":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
        </svg>
    case "bell":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
        </svg>
    case "home":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6" />
        </svg>
    case "mail":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
    case "lock":
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 15v2m-6 4h12a2 2 0 002-2v-6a2 2 0 00-2-2H6a2 2 0 00-2 2v6a2 2 0 002 2zm10-10V7a4 4 0 00-8 0v4h8z" />
        </svg>
    default:
        <svg class="w-6 h-6 text-indigo-600" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
    }
}
`, gridCols)
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(viewDir, "widgets.templ"), []byte(code), 0644)
}

// generatePageViews writes one templ view per page into internal/views/pages.
// Each file only declares its page template; the shared widget/statWidget/
// iconSVG components live in widgets.templ (see generatePageWidgets).
// Params: p (the page definition).
// Returns: an error on write failure.
func (g *Generator) generatePageViews(p types.Page) error {
	viewDir := filepath.Join(g.OutDir, "internal/views/pages")
	panelID := g.Config.Panel.ID

	capitalID := strings.ToUpper(panelID[:1]) + panelID[1:]
	templName := capitalID + p.Name
	code := fmt.Sprintf(`package views

import (
    "internal/viewmodels"
)

templ %s(data *viewmodels.PageData) {
    <div class="p-6">
        <h1 class="text-2xl font-bold mb-6">{ data.Name }</h1>
        for _, w := range data.Widgets {
            @widget(w)
        }
    </div>
}
`, templName)
	code = prefixImports(code, g.moduleImport("internal/viewmodels"))

	return os.WriteFile(filepath.Join(viewDir, p.Name+".templ"), []byte(code), 0644)
}

// detectGridColumns finds the column count of the first stats_grid widget on a
// page, used to size the generated grid layout. Defaults to 4 when no
// stats_grid widget declares columns.
// Params: widgets (the page's widget list).
// Returns: the number of grid columns to render.
func (g *Generator) detectGridColumns(widgets []types.Widget) int {
	for _, w := range widgets {
		if w.Type == "stats_grid" && w.Columns > 0 {
			return w.Columns
		}
	}
	return 4
}

// generateLayoutViews writes base.templ into internal/views/layout: the Base
// layout document (with the Chart.js CDN script and auto-rendering JS), the
// sidebar with the navigation groups sorted by their sort value, the topbar
// with the logout link, and the iconNav SVG helper.
// Returns: an error on write failure.
func (g *Generator) generateLayoutViews() error {
	dir := filepath.Join(g.OutDir, "internal/views/layout")
	panelPath := g.Config.Panel.Path
	panelName := g.Config.Panel.Name

	sortedNav := make([]types.NavigationGroup, len(g.Config.Navigation))
	copy(sortedNav, g.Config.Navigation)
	sort.Slice(sortedNav, func(i, j int) bool {
		return sortedNav[i].Sort < sortedNav[j].Sort
	})

	var sidebarNav strings.Builder
	for _, ng := range sortedNav {
		sidebarNav.WriteString(fmt.Sprintf(`            <div class="px-4 py-2 text-xs font-semibold text-gray-500 uppercase tracking-wider mt-4">@iconNav(%q) %s</div>
`, ng.Icon, ng.Group))
		for _, item := range ng.Items {
			if item.Resource != "" {
				label := item.Resource
				sidebarNav.WriteString(fmt.Sprintf(`            <a href="%s/%s" class="block px-4 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-600 mx-2 rounded-md">%s</a>
`, panelPath, strings.ToLower(item.Resource), label))
			}
			if item.Page != "" {
				sidebarNav.WriteString(fmt.Sprintf(`            <a href="%s/%s" class="block px-4 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-600 mx-2 rounded-md">%s</a>
`, panelPath, item.Page, item.Page))
			}
			if item.Type == "link" {
				target := ""
				if item.OpensInNewTab {
					target = ` target="_blank"`
				}
				sidebarNav.WriteString(fmt.Sprintf(`            <a href="%s"%s class="block px-4 py-2 text-sm text-gray-700 hover:bg-indigo-50 hover:text-indigo-600 mx-2 rounded-md">%s</a>
`, item.URL, target, item.Label))
			}
		}
	}

	code := fmt.Sprintf(`package views

templ Base(title string, panelPath string, children templ.Component) {
    <!DOCTYPE html>
    <html lang="en">
    <head>
        <meta charset="UTF-8" />
        <meta name="viewport" content="width=device-width, initial-scale=1.0" />
        <title>{ title }</title>
        <link href="/static/css/styles.css" rel="stylesheet" />
        <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    </head>
    <body class="bg-gray-50">
        <div class="flex h-screen">
            @Sidebar(panelPath)
            <div class="flex-1 flex flex-col">
                @Topbar(panelPath)
                <main class="flex-1 overflow-y-auto p-6">
                    @children
                </main>
            </div>
        </div>
        <script>
            function toggleSidebar() {
                var sidebar = document.querySelector('aside');
                sidebar.style.display = sidebar.style.display === 'none' ? '' : 'none';
            }
            // Auto-render Chart.js canvases
            document.addEventListener('DOMContentLoaded', function() {
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
                                borderColor: '#6366f1',
                                backgroundColor: 'rgba(99, 102, 241, 0.2)',
                            }]
                        }
                    });
                });
            });
        </script>
    </body>
    </html>
}

templ iconNav(name string) {
    switch name {
    case "users":
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z"/></svg>
    case "chart":
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"/></svg>
    case "cog":
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"/><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"/></svg>
    case "home":
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-6 0a1 1 0 001-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 001 1m-6 0h6"/></svg>
    case "mail":
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3 8l7.89 5.26a2 2 0 002.22 0L21 8M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"/></svg>
    default:
        <svg class="w-4 h-4 inline mr-1" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"/></svg>
    }
}

templ Sidebar(panelPath string) {
    <aside class="w-64 bg-white shadow-md h-screen overflow-y-auto shrink-0">
        <div class="p-4 border-b">
            <h1 class="text-xl font-bold">%s</h1>
        </div>
        <nav class="mt-2">
%s        </nav>
    </aside>
}

templ Topbar(panelPath string) {
    <header class="bg-white shadow-sm px-6 py-3 flex items-center justify-between sticky top-0 z-10">
        <div class="flex items-center gap-4">
            <button class="text-gray-500 hover:text-gray-700" onclick="toggleSidebar()">
                <svg class="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
                </svg>
            </button>
        </div>
        <div class="flex items-center gap-4">
            <a href="%s/logout" class="text-sm text-gray-600 hover:text-gray-900">Logout</a>
        </div>
    </header>
}
`, panelName, sidebarNav.String(), panelPath)

	return os.WriteFile(filepath.Join(dir, "base.templ"), []byte(code), 0644)
}

// generateComponentViews writes renderers.templ into internal/views/components:
// the shared field renderers (badge, boolean, email, image, file, datetime,
// date, select, relation, json, float), the search bar, sort icon, sortOrder
// helper and pagination component.
// Returns: an error on write failure.
func (g *Generator) generateComponentViews() error {
	dir := filepath.Join(g.OutDir, "internal/views/components")
	return os.WriteFile(filepath.Join(dir, "renderers.templ"), []byte(renderersSource()), 0644)
}

// renderersSource returns the templ source for the shared field renderers and
// utility components (search bar, sort icon, sortOrder helper, pagination).
// The same source is emitted into every resource view directory so each view
// package is self-contained.
// Returns: the templ source as a string.
func renderersSource() string {
	return `package views

import (
    "fmt"
    "time"
)

// --- Field Renderers ---

templ renderBadge(value interface{}, color string) {
    if value != nil {
        {{ text := fmt.Sprintf("%v", value) }}
        {{ c := color }}
        if c == "" {
            {{ c = "gray" }}
        }
        <span class={ fmt.Sprintf("inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium bg-%s-100 text-%s-800", c, c) }>{ text }</span>
    }
}

templ renderBoolean(value interface{}) {
    if value != nil {
        if b, ok := value.(bool); ok && b {
            <span class="text-green-600">
                <svg class="w-5 h-5 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                </svg>
            </span>
        } else {
            <span class="text-red-600">
                <svg class="w-5 h-5 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                </svg>
            </span>
        }
    }
}

templ renderEmail(value interface{}) {
    if value != nil {
        {{ email := fmt.Sprintf("%v", value) }}
        <a href={ templ.SafeURL(fmt.Sprintf("mailto:%s", email)) } class="text-indigo-600 hover:text-indigo-900 underline">{ email }</a>
    }
}

templ renderImage(value interface{}) {
    if value != nil {
        {{ src := fmt.Sprintf("%v", value) }}
        <img src={ src } alt="" class="w-10 h-10 rounded-full object-cover" />
    }
}

templ renderFile(value interface{}) {
    if value != nil {
        {{ name := fmt.Sprintf("%v", value) }}
        <a href={ templ.SafeURL(name) } class="text-indigo-600 hover:text-indigo-900 underline" download>{ name }</a>
    }
}

templ renderDateTime(value interface{}) {
    if value != nil {
        if t, ok := value.(time.Time); ok {
            <span class="text-sm text-gray-600">{ t.Format("Jan 02, 2006 15:04") }</span>
        } else {
            <span class="text-sm text-gray-600">{ fmt.Sprintf("%v", value) }</span>
        }
    }
}

templ renderDate(value interface{}) {
    if value != nil {
        if t, ok := value.(time.Time); ok {
            <span class="text-sm text-gray-600">{ t.Format("Jan 02, 2006") }</span>
        } else {
            <span class="text-sm text-gray-600">{ fmt.Sprintf("%v", value) }</span>
        }
    }
}

templ renderSelect(value interface{}) {
    if value != nil {
        {{ text := fmt.Sprintf("%v", value) }}
        <span class="text-sm text-gray-900">{ text }</span>
    }
}

templ renderRelation(value interface{}) {
    if value != nil {
        {{ text := fmt.Sprintf("%v", value) }}
        <a href="#" class="text-indigo-600 hover:text-indigo-900 underline">{ text }</a>
    }
}

templ renderJSON(value interface{}) {
    if value != nil {
        <pre class="text-xs text-gray-600 bg-gray-50 p-2 rounded overflow-x-auto max-w-xs">{ fmt.Sprintf("%v", value) }</pre>
    }
}

templ renderFloat(value interface{}) {
    if value != nil {
        if f, ok := value.(float64); ok {
            <span class="text-sm text-gray-900">{ fmt.Sprintf("%.2f", f) }</span>
        } else {
            <span class="text-sm text-gray-900">{ fmt.Sprintf("%v", value) }</span>
        }
    }
}

templ renderGPS(value interface{}) {
    if value != nil {
        {{ coords := fmt.Sprintf("%v", value) }}
        <a href={ templ.SafeURL(fmt.Sprintf("https://www.google.com/maps?q=%s", coords)) } target="_blank" rel="noopener noreferrer" class="text-indigo-600 hover:text-indigo-900 underline">{ coords }</a>
    }
}

// --- Utility Components ---

templ searchBar(query string, resource string) {
    <div class="mb-4">
        <form method="GET" class="flex gap-2">
            <input type="text" name="search" value={ query } placeholder="Search..." class="w-64 rounded-md border-gray-300 shadow-sm focus:border-indigo-500 focus:ring-indigo-500 sm:text-sm border px-3 py-2" />
            <button type="submit" class="bg-gray-100 text-gray-700 px-4 py-2 rounded-md text-sm hover:bg-gray-200 border">Search</button>
            if query != "" {
                <a href="?" class="text-gray-500 hover:text-gray-700 px-4 py-2 text-sm">Clear</a>
            }
        </form>
    </div>
}

templ sortIcon(sortField string, field string, order string) {
    if sortField == field {
        if order == "desc" {
            <svg class="w-4 h-4 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7" />
            </svg>
        } else {
            <svg class="w-4 h-4 inline" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
        }
    }
}

func sortOrder(currentSort string, field string, currentOrder string) string {
    if currentSort == field {
        if currentOrder == "asc" {
            return "desc"
        }
    }
    return "asc"
}

templ pagination(page int, totalPages int, total int, search string, sort string, order string) {
    if totalPages > 0 {
        <div class="bg-white px-4 py-3 flex items-center justify-between border-t">
            <div class="text-sm text-gray-700">
                Showing page { fmt.Sprintf("%d", page) } of { fmt.Sprintf("%d", totalPages) } ({ fmt.Sprintf("%d", total) } total)
            </div>
            <div class="flex gap-1">
                if page > 1 {
                    <a href={ templ.SafeURL(fmt.Sprintf("?page=%d&search=%s&sort=%s&order=%s", page-1, search, sort, order)) } class="px-3 py-1 border rounded text-sm hover:bg-gray-50">Previous</a>
                }
                for i := 1; i <= totalPages; i++ {
                    if i == page {
                        <span class="px-3 py-1 border rounded text-sm bg-indigo-600 text-white">{ fmt.Sprintf("%d", i) }</span>
                    } else {
                        <a href={ templ.SafeURL(fmt.Sprintf("?page=%d&search=%s&sort=%s&order=%s", i, search, sort, order)) } class="px-3 py-1 border rounded text-sm hover:bg-gray-50">{ fmt.Sprintf("%d", i) }</a>
                    }
                }
                if page < totalPages {
                    <a href={ templ.SafeURL(fmt.Sprintf("?page=%d&search=%s&sort=%s&order=%s", page+1, search, sort, order)) } class="px-3 py-1 border rounded text-sm hover:bg-gray-50">Next</a>
                }
            </div>
        </div>
    }
}
`
}
