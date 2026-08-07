package editor

import (
	"fmt"

	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// columnsPage manages a resource's list columns.
func (e *Editor) columnsPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	l := ensureList(r)
	spec := listSpec{
		title: "List columns",
		labels: func() []string {
			out := make([]string, len(l.Columns))
			for i, c := range l.Columns {
				out[i] = c.Name
			}
			return out
		},
		sub: func(i int) string {
			c := l.Columns[i]
			return fmt.Sprintf("%s  sort=%v search=%v", c.Label, c.Sortable, c.Searchable)
		},
		add: func() {
			l.Columns = append(l.Columns, types.Column{Name: "new_column", Type: "string", Label: "New Column"})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage("columns/"+fmt.Sprint(i), e.columnPage(idx, i))
		},
		remove: func(i int) {
			l.Columns = append(l.Columns[:i], l.Columns[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("columns", spec)
}

// columnPage edits a single list column.
func (e *Editor) columnPage(idx, cidx int) tview.Primitive {
	c := &e.cfg.Resources[idx].List.Columns[cidx]
	return e.formShell("Column: "+c.Name, func(f *tview.Form) {
		e.str(f, "Name", c.Name, func(v string) { c.Name = v })
		e.str(f, "Label", c.Label, func(v string) { c.Label = v })
		e.pick(f, "Type", fieldTypeOptions, c.Type, func(v string) { c.Type = v })
		e.yesno(f, "Sortable", c.Sortable, func(v bool) { c.Sortable = v })
		e.yesno(f, "Searchable", c.Searchable, func(v bool) { c.Searchable = v })
		f.AddButton("Options", func() {
			e.showPage("column-options/"+fmt.Sprint(cidx), e.stringMapPage("column-options/"+fmt.Sprint(cidx), "Column options", func() map[string]string {
				return c.Options
			}, func(v map[string]string) { c.Options = v }))
		})
	})
}
