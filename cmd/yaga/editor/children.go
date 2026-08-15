package editor

import (
	"github.com/MichalHerstus/yaga/internal/types"
	"github.com/rivo/tview"
)

// childrenPage manages the `children:` master-detail sections of a header
// resource (D14): each entry references a child resource plus an optional FK
// column and display-column overrides.
func (e *Editor) childrenPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	spec := listSpec{
		title: "Children",
		labels: func() []string {
			out := make([]string, len(r.Children))
			for i, ch := range r.Children {
				label := ch.Name
				if label == "" {
					label = ch.Resource
				}
				out[i] = label
			}
			return out
		},
		sub: func(i int) string {
			return r.Children[i].Resource
		},
		add: func() {
			r.Children = append(r.Children, types.ChildResource{Name: "Lines", Resource: ""})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage(e.resChildrenPath(idx)+"/"+segName(r.Children[i].Name, i), e.childResourcePage(idx, i))
		},
		remove: func(i int) {
			r.Children = append(r.Children[:i], r.Children[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList(e.resChildrenPath(idx), spec)
}

// childResourcePage edits one ChildResource entry: the section heading, the
// child resource name, the optional FK column, and the optional display-column
// overrides.
func (e *Editor) childResourcePage(idx, ci int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	ch := &r.Children[ci]
	path := e.resChildrenPath(idx) + "/" + segName(ch.Name, ci)
	return e.formShell("Child: "+ch.Name, func(f *tview.Form) {
		e.str(f, "Name", ch.Name, func(v string) { ch.Name = v })
		e.str(f, "Resource", ch.Resource, func(v string) { ch.Resource = v })
		e.str(f, "FK column", ch.Column, func(v string) { ch.Column = v })
		e.addButton(f, "Columns", func() {
			e.showPage(path+"/Columns", e.childColumnsPage(ch))
		})
	})
}

// childColumnsPage edits the optional display columns of a child section. When
// empty the generator defaults to the child resource's list columns.
func (e *Editor) childColumnsPage(ch *types.ChildResource) tview.Primitive {
	spec := listSpec{
		title: "Child columns",
		labels: func() []string {
			out := make([]string, len(ch.Columns))
			for i, c := range ch.Columns {
				out[i] = c.Name
			}
			return out
		},
		sub: func(i int) string { return ch.Columns[i].Type },
		add: func() {
			ch.Columns = append(ch.Columns, types.Column{Name: "new_column", Type: "string", Label: "New Column"})
			e.markModified()
		},
		edit: func(i int) {
			c := &ch.Columns[i]
			e.showPage("child-col-"+segName(c.Name, i), e.formShell("Column: "+c.Name, func(f *tview.Form) {
				e.str(f, "Name", c.Name, func(v string) { c.Name = v })
				e.str(f, "Label", c.Label, func(v string) { c.Label = v })
				e.pick(f, "Type", fieldTypeOptions, c.Type, func(v string) { c.Type = v })
			}))
		},
		remove: func(i int) {
			ch.Columns = append(ch.Columns[:i], ch.Columns[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("child-columns", spec)
}
