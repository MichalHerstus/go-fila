package editor

import (
	"sort"
	"strings"

	"github.com/MichalHerstus/yaga/internal/schema"
	"github.com/MichalHerstus/yaga/internal/types"
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
			e.showPage(e.resPath(i), e.resourcePage(i))
		},
		remove: func(i int) {
			e.cfg.Resources = append(e.cfg.Resources[:i], e.cfg.Resources[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("Resources", spec)
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
		e.addButton(f, "List", func() { e.showPage(e.resListPath(idx), e.listPage(idx)) })
		e.addButton(f, "Card", func() { e.showPage(e.resCardPath(idx), e.cardPage(idx)) })
		e.addButton(f, "Detail", func() { e.showPage(e.resDetailPath(idx), e.detailPage(idx)) })
		e.addButton(f, "Form", func() { e.showPage(e.resFormPath(idx), e.formPage(idx)) })
		e.head(f, "Behavior")
		e.yesno(f, "Import CSV", r.ImportCSV, func(v bool) { r.ImportCSV = v })
		e.addButton(f, "Actions", func() { e.showPage(e.resActionsPath(idx), e.actionsPage(idx)) })
		e.addButton(f, "Policies", func() { e.showPage(e.resPoliciesPath(idx), e.policiesPage(idx)) })
		e.head(f, "Queries")
		e.addButton(f, "SQL queries", func() { e.showPage(e.resSQLPath(idx), e.sqlQueriesPage(idx)) })
	})
}

// sqlQueriesForResource collects the SQLC query names a resource references
// (list/count/detail/form queries, populate queries and field options_query),
// deduplicated and sorted.
func sqlQueriesForResource(r *types.Resource) []string {
	var names []string
	seen := map[string]bool{}
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" || seen[n] {
			return
		}
		seen[n] = true
		names = append(names, n)
	}
	addFields := func(fs []types.Field) {
		for _, f := range fs {
			add(f.OptionsQuery)
		}
	}
	if r.List != nil {
		add(r.List.Query)
		add(r.List.CountQuery)
	}
	if r.Detail != nil {
		add(r.Detail.Query)
		addFields(r.Detail.Fields)
	}
	if r.Card != nil {
		addFields(r.Card.Fields)
	}
	formActions := []*types.FormAction{nil, nil, nil}
	if r.Form != nil {
		formActions[0], formActions[1], formActions[2] = r.Form.Create, r.Form.Update, r.Form.Delete
	}
	for _, fa := range formActions {
		if fa == nil {
			continue
		}
		add(fa.Query)
		add(fa.PopulateQuery)
		addFields(fa.Fields)
	}
	sort.Strings(names)
	return names
}

// sqlQueriesPage lists the resource's SQLC query definitions; Enter opens the
// SQL editor for the selected query.
func (e *Editor) sqlQueriesPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	qdir := e.queriesDir()
	qs := schema.ParseQueries(qdir)
	names := sqlQueriesForResource(r)

	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetBorderColor(colBorder).SetTitle("SQL queries — " + r.Name + "  (enter: edit, esc: back)")
	list.SetMainTextColor(colText)
	list.SetSecondaryTextColor(colMuted)
	for _, name := range names {
		qname := name
		sub := "not found in " + qdir
		if q, ok := qs[qname]; ok {
			sub = q.File
		}
		list.AddItem(qname, sub, 0, func() {
			e.showPage(e.resSQLQueryPath(idx, qname), e.sqlEditPage(qname))
		})
	}
	if len(names) == 0 {
		list.AddItem("(no SQLC queries referenced)", "", 0, nil)
	}
	return list
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
	qc := e.newSQLViewer()
	return e.formShell("List: "+r.Name, func(f *tview.Form) {
		var renderQ, renderCQ func()
		e.str(f, "Query", l.Query, func(v string) { l.Query = v; renderQ() })
		renderQ = qc.addRow(f, "", func() string { return l.Query })
		e.str(f, "Count query", l.CountQuery, func(v string) { l.CountQuery = v; renderCQ() })
		renderCQ = qc.addRow(f, "", func() string { return l.CountQuery })
		e.num(f, "Per page", l.PerPage, func(v int) { l.PerPage = v })
		e.str(f, "Default sort", l.DefaultSort, func(v string) { l.DefaultSort = v })
		e.addButton(f, "Export", func() {
			path := e.resListPath(idx) + "/Export"
			e.showPage(path, e.stringListPage(path, "Export columns", func() []string {
				return l.Export
			}, func(v []string) { l.Export = v }))
		})
		qc.reloadButton(f)
		e.addButton(f, "Columns", func() {
			e.showPage(e.resColumnsPath(idx), e.columnsPage(idx))
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
	qc := e.newSQLViewer()
	return e.formShell("Card: "+r.Name, func(f *tview.Form) {
		e.num(f, "Columns", c.Columns, func(v int) { c.Columns = v })
		e.num(f, "Rows", c.Rows, func(v int) { c.Rows = v })
		e.str(f, "Default sort", c.DefaultSort, func(v string) { c.DefaultSort = v })
		e.pick(f, "Kanban field", kanbanOpts(), c.KanbanField, func(v string) { c.KanbanField = v })
		// The generated card reuses the list query; show it for reference.
		qc.addRow(f, "Query (List)", func() string {
			if r.List != nil {
				return r.List.Query
			}
			return ""
		})
		qc.reloadButton(f)
		e.addButton(f, "Fields", func() {
			e.showPage(e.resCardFieldsPath(idx), e.cardFieldsPage(idx))
		})
		e.addButton(f, "Searchable", func() {
			path := e.resCardPath(idx) + "/Searchable"
			e.showPage(path, e.stringListPage(path, "Card searchable", func() []string {
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
	qc := e.newSQLViewer()
	return e.formShell("Detail: "+r.Name, func(f *tview.Form) {
		var renderQ func()
		e.str(f, "Query", d.Query, func(v string) { d.Query = v; renderQ() })
		renderQ = qc.addRow(f, "", func() string { return d.Query })
		qc.reloadButton(f)
		e.addButton(f, "Params", func() {
			path := e.resDetailPath(idx) + "/Params"
			e.showPage(path, e.stringMapPage(path, "Detail params", func() map[string]string {
				return d.Params
			}, func(v map[string]string) { d.Params = v }))
		})
		e.addButton(f, "Fields", func() {
			e.showPage(e.resDetailFieldsPath(idx), e.detailFieldsPage(idx))
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
		e.addButton(f, "Create", func() {
			e.ensureFormAction(r, "create")
			e.showPage(e.resFormWhichPath(idx, "create"), e.formActionPage(idx, "create"))
		})
		e.addButton(f, "Update", func() {
			e.ensureFormAction(r, "update")
			e.showPage(e.resFormWhichPath(idx, "update"), e.formActionPage(idx, "update"))
		})
		e.addButton(f, "Delete", func() {
			e.ensureFormAction(r, "delete")
			e.showPage(e.resFormWhichPath(idx, "delete"), e.formActionPage(idx, "delete"))
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
	qc := e.newSQLViewer()
	base := e.resFormWhichPath(idx, which)
	return e.formShell(label+" form: "+r.Name, func(f *tview.Form) {
		var renderQ, renderPQ func()
		e.str(f, "Query", fa.Query, func(v string) { fa.Query = v; renderQ() })
		renderQ = qc.addRow(f, "", func() string { return fa.Query })
		e.str(f, "Populate query", fa.PopulateQuery, func(v string) { fa.PopulateQuery = v; renderPQ() })
		renderPQ = qc.addRow(f, "", func() string { return fa.PopulateQuery })
		qc.reloadButton(f)
		e.addButton(f, "Populate params", func() {
			path := base + "/Params"
			e.showPage(path, e.stringMapPage(path, label+" populate params", func() map[string]string {
				return fa.PopulateParams
			}, func(v map[string]string) { fa.PopulateParams = v }))
		})
		e.addButton(f, "Fields", func() {
			e.showPage(base+"/Fields", e.formFieldsPage(idx, which))
		})
		e.addButton(f, "Hooks", func() {
			ensureHooks(fa)
			hooksPath := base + "/Hooks"
			e.showPage(hooksPath, e.hooksPage(hooksPath, &fa.Hooks, label))
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
