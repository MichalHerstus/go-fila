package editor

import (
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// fieldsListPage manages a []types.Field collection (form, card or detail
// fields) with a shared editor. name is the canonical path of the fields list
// screen; field sub-pages extend it.
func (e *Editor) fieldsListPage(name, title string, get func() *[]types.Field) tview.Primitive {
	spec := listSpec{
		title: title,
		labels: func() []string {
			fs := *get()
			out := make([]string, len(fs))
			for i, fld := range fs {
				out[i] = fld.Name
			}
			return out
		},
		sub: func(i int) string {
			fs := *get()
			return fs[i].Type
		},
		add: func() {
			fs := append(*get(), types.Field{Name: "new_field", Label: "New Field", Type: "string"})
			*get() = fs
			e.markModified()
		},
		edit: func(i int) {
			fs := *get()
			e.showPage(name+"/"+segName(fs[i].Name, i), e.fieldPage(name, get, i))
		},
		remove: func(i int) {
			fs := *get()
			fs = append(fs[:i], fs[i+1:]...)
			*get() = fs
			e.markModified()
		},
	}
	return e.recordList(name, spec)
}

// fieldPage edits a single field definition. name is the canonical fields-list
// path; the field's own path (with /Validation, /Options, /Visible children) is
// derived from it.
func (e *Editor) fieldPage(name string, get func() *[]types.Field, idx int) tview.Primitive {
	fs := *get()
	fld := &fs[idx]
	qc := e.newSQLViewer()
	fieldPath := name + "/" + segName(fld.Name, idx)
	return e.formShell("Field: "+fld.Name, func(f *tview.Form) {
		e.str(f, "Name", fld.Name, func(v string) { fld.Name = v })
		e.str(f, "Label", fld.Label, func(v string) { fld.Label = v })
		e.pick(f, "Type", fieldTypeOptions, fld.Type, func(v string) { fld.Type = v })
		e.yesno(f, "Required", fld.Required, func(v bool) { fld.Required = v })
		var renderOpt func()
		e.str(f, "Options query", fld.OptionsQuery, func(v string) { fld.OptionsQuery = v; renderOpt() })
		renderOpt = qc.addRow(f, "", func() string { return fld.OptionsQuery })
		e.str(f, "Options value", fld.OptionsValue, func(v string) { fld.OptionsValue = v })
		e.str(f, "Options label", fld.OptionsLabel, func(v string) { fld.OptionsLabel = v })
		qc.reloadButton(f)
		e.addButton(f, "Validation", func() {
			if fld.Validation == nil {
				fld.Validation = &types.Validation{}
			}
			e.showPage(fieldPath+"/Validation", e.validationPage(fieldPath, fld.Validation))
		})
		e.addButton(f, "Options", func() {
			optsPath := fieldPath + "/Options"
			e.showPage(optsPath, e.stringMapPage(optsPath, "Field options", func() map[string]string {
				return fld.Options
			}, func(v map[string]string) { fld.Options = v }))
		})
		e.addButton(f, "Visible", func() {
			visPath := fieldPath + "/Visible"
			e.showPage(visPath, e.tagsPage(visPath, "Field visible in", visibleOptions, func() []string {
				return fld.Visible
			}, func(v []string) { fld.Visible = v }))
		})
	})
}

// validationPage edits a field's min/max validation.
func (e *Editor) validationPage(fieldPath string, v *types.Validation) tview.Primitive {
	return e.formShell("Validation", func(f *tview.Form) {
		e.num(f, "Min", v.Min, func(x int) { v.Min = x })
		e.num(f, "Max", v.Max, func(x int) { v.Max = x })
	})
}

// cardFieldsPage edits the card view fields of a resource.
func (e *Editor) cardFieldsPage(idx int) tview.Primitive {
	c := e.cfg.Resources[idx].Card
	return e.fieldsListPage(e.resCardFieldsPath(idx), "Card fields", func() *[]types.Field {
		return &c.Fields
	})
}

// detailFieldsPage edits the detail view fields of a resource.
func (e *Editor) detailFieldsPage(idx int) tview.Primitive {
	d := e.cfg.Resources[idx].Detail
	return e.fieldsListPage(e.resDetailFieldsPath(idx), "Detail fields", func() *[]types.Field {
		return &d.Fields
	})
}

// formFieldsPage edits the form fields of one form action.
func (e *Editor) formFieldsPage(idx int, which string) tview.Primitive {
	r := &e.cfg.Resources[idx]
	var fa *types.FormAction
	switch which {
	case "create":
		fa = r.Form.Create
	case "update":
		fa = r.Form.Update
	case "delete":
		fa = r.Form.Delete
	}
	title := "Create fields"
	if which == "update" {
		title = "Update fields"
	} else if which == "delete" {
		title = "Delete fields"
	}
	return e.fieldsListPage(e.resFormWhichPath(idx, which)+"/Fields", title, func() *[]types.Field {
		return &fa.Fields
	})
}
