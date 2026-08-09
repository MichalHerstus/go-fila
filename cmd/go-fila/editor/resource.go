package editor

import (
	"fmt"

	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// resourcesPage manages the CRUD resources.
func (e *Editor) resourcesPage() tview.Primitive {
	spec := listSpec{
		title: "Resources",
		labels: func() []string {
			out := make([]string, len(e.cfg.Resources))
			for i, r := range e.cfg.Resources {
				out[i] = r.Name
			}
			return out
		},
		sub: func(i int) string {
			r := e.cfg.Resources[i]
			extra := ""
			if r.Table != "" {
				extra = " table=" + r.Table
			}
			if r.IDType != "" {
				extra += " id=" + r.IDType
			}
			return r.Label + extra
		},
		add: func() {
			e.cfg.Resources = append(e.cfg.Resources, types.Resource{Name: "NewResource"})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage("resources/"+fmt.Sprint(i), e.resourcePage(i))
		},
		remove: func(i int) {
			e.cfg.Resources = append(e.cfg.Resources[:i], e.cfg.Resources[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("resources", spec)
}

// resourcePage edits a single resource and its sections.
func (e *Editor) resourcePage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	return e.formShell("Resource: "+r.Name, func(f *tview.Form) {
		e.str(f, "Name", r.Name, func(v string) { r.Name = v })
		e.str(f, "Label", r.Label, func(v string) { r.Label = v })
		e.pick(f, "Icon", iconOptions, r.Icon, func(v string) { r.Icon = v })
		e.str(f, "Group", r.Group, func(v string) { r.Group = v })
		e.str(f, "Table", r.Table, func(v string) { r.Table = v })
		e.pick(f, "ID type", idTypeOptions, r.IDType, func(v string) { r.IDType = v })
		e.str(f, "ID column", r.IDColumn, func(v string) { r.IDColumn = v })
		e.head(f, "Views")
		f.AddButton("List", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/list", e.listPage(idx)) })
		f.AddButton("Card", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/card", e.cardPage(idx)) })
		f.AddButton("Detail", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/detail", e.detailPage(idx)) })
		f.AddButton("Form", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/form", e.formPage(idx)) })
		e.head(f, "Behavior")
		f.AddButton("Actions", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/actions", e.actionsPage(idx)) })
		f.AddButton("Policies", func() { e.showPage("resources/"+fmt.Sprint(idx)+"/policies", e.policiesPage(idx)) })
	})
}

// ensureList lazily allocates the resource list config.
func ensureList(r *types.Resource) *types.ListConfig {
	if r.List == nil {
		r.List = &types.ListConfig{}
	}
	return r.List
}

// listPage edits a resource list view.
func (e *Editor) listPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	l := ensureList(r)
	return e.formShell("List: "+r.Name, func(f *tview.Form) {
		e.str(f, "Query", l.Query, func(v string) { l.Query = v })
		e.str(f, "Count query", l.CountQuery, func(v string) { l.CountQuery = v })
		e.num(f, "Per page", l.PerPage, func(v int) { l.PerPage = v })
		e.str(f, "Default sort", l.DefaultSort, func(v string) { l.DefaultSort = v })
		f.AddButton("Columns", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/columns", e.columnsPage(idx))
		})
	})
}

// cardPage edits a resource card/kanban view.
func (e *Editor) cardPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	if r.Card == nil {
		r.Card = &types.CardConfig{Columns: 4, Rows: 4}
	}
	c := r.Card
	kanbanOpts := func() []Option {
		out := make([]Option, 0, len(c.Fields))
		for _, fld := range c.Fields {
			out = append(out, Option{Label: fld.Name, Value: fld.Name})
		}
		return out
	}
	return e.formShell("Card: "+r.Name, func(f *tview.Form) {
		e.num(f, "Columns", c.Columns, func(v int) { c.Columns = v })
		e.num(f, "Rows", c.Rows, func(v int) { c.Rows = v })
		e.str(f, "Default sort", c.DefaultSort, func(v string) { c.DefaultSort = v })
		e.pick(f, "Kanban field", kanbanOpts(), c.KanbanField, func(v string) { c.KanbanField = v })
		f.AddButton("Fields", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/card-fields", e.cardFieldsPage(idx))
		})
		f.AddButton("Searchable", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/card-searchable", e.stringListPage("resources/"+fmt.Sprint(idx)+"/card-searchable", "Card searchable", func() []string {
				return c.Searchable
			}, func(v []string) { c.Searchable = v }))
		})
	})
}

// detailPage edits a resource detail view.
func (e *Editor) detailPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	if r.Detail == nil {
		r.Detail = &types.DetailConfig{}
	}
	d := r.Detail
	return e.formShell("Detail: "+r.Name, func(f *tview.Form) {
		e.str(f, "Query", d.Query, func(v string) { d.Query = v })
		f.AddButton("Params", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/detail-params", e.stringMapPage("resources/"+fmt.Sprint(idx)+"/detail-params", "Detail params", func() map[string]string {
				return d.Params
			}, func(v map[string]string) { d.Params = v }))
		})
		f.AddButton("Fields", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/detail-fields", e.detailFieldsPage(idx))
		})
	})
}

// formPage edits a resource's create/update/delete form actions.
func (e *Editor) formPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	if r.Form == nil {
		r.Form = &types.FormConfig{}
	}
	return e.formShell("Form: "+r.Name, func(f *tview.Form) {
		f.AddButton("Create", func() {
			e.ensureFormAction(r, "create")
			e.showPage("resources/"+fmt.Sprint(idx)+"/form-create", e.formActionPage(idx, "create"))
		})
		f.AddButton("Update", func() {
			e.ensureFormAction(r, "update")
			e.showPage("resources/"+fmt.Sprint(idx)+"/form-update", e.formActionPage(idx, "update"))
		})
		f.AddButton("Delete", func() {
			e.ensureFormAction(r, "delete")
			e.showPage("resources/"+fmt.Sprint(idx)+"/form-delete", e.formActionPage(idx, "delete"))
		})
	})
}

// ensureFormAction lazily allocates one of create/update/delete.
func (e *Editor) ensureFormAction(r *types.Resource, which string) {
	if r.Form == nil {
		r.Form = &types.FormConfig{}
	}
	switch which {
	case "create":
		if r.Form.Create == nil {
			r.Form.Create = &types.FormAction{}
		}
	case "update":
		if r.Form.Update == nil {
			r.Form.Update = &types.FormAction{}
		}
	case "delete":
		if r.Form.Delete == nil {
			r.Form.Delete = &types.FormAction{}
		}
	}
}

// formActionPage edits one form action.
func (e *Editor) formActionPage(idx int, which string) tview.Primitive {
	r := &e.cfg.Resources[idx]
	var fa *types.FormAction
	label := "Create"
	switch which {
	case "create":
		fa = r.Form.Create
	case "update":
		fa = r.Form.Update
		label = "Update"
	case "delete":
		fa = r.Form.Delete
		label = "Delete"
	}
	return e.formShell(label+" form: "+r.Name, func(f *tview.Form) {
		e.str(f, "Query", fa.Query, func(v string) { fa.Query = v })
		e.str(f, "Populate query", fa.PopulateQuery, func(v string) { fa.PopulateQuery = v })
		f.AddButton("Populate params", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/"+which+"-params", e.stringMapPage("resources/"+fmt.Sprint(idx)+"/"+which+"-params", label+" populate params", func() map[string]string {
				return fa.PopulateParams
			}, func(v map[string]string) { fa.PopulateParams = v }))
		})
		f.AddButton("Fields", func() {
			e.showPage("resources/"+fmt.Sprint(idx)+"/"+which+"-fields", e.formFieldsPage(idx, which))
		})
		f.AddButton("Hooks", func() {
			ensureHooks(fa)
			e.showPage("resources/"+fmt.Sprint(idx)+"/"+which+"-hooks", e.hooksPage(&fa.Hooks, label))
		})
	})
}

// ensureHooks lazily allocates a Hooks block on a form action.
func ensureHooks(fa *types.FormAction) {
	if fa.Hooks == nil {
		fa.Hooks = &types.Hooks{}
	}
}

// policiesPage edits the RBAC role lists for a resource.
func (e *Editor) policiesPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	if r.Policies == nil {
		r.Policies = &types.Policy{}
	}
	p := r.Policies
	return e.formShell("Policies: "+r.Name, func(f *tview.Form) {
		e.str(f, "View any", p.ViewAny, func(v string) { p.ViewAny = v })
		e.str(f, "View", p.View, func(v string) { p.View = v })
		e.str(f, "Create", p.Create, func(v string) { p.Create = v })
		e.str(f, "Update", p.Update, func(v string) { p.Update = v })
		e.str(f, "Delete", p.Delete, func(v string) { p.Delete = v })
		e.head(f, "Roles are pipe-separated, e.g. admin|manager")
	})
}
