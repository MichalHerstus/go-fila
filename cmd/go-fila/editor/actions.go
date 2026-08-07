package editor

import (
	"fmt"

	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// actionsPage manages a resource's custom row actions.
func (e *Editor) actionsPage(idx int) tview.Primitive {
	r := &e.cfg.Resources[idx]
	spec := listSpec{
		title: "Actions",
		labels: func() []string {
			out := make([]string, len(r.Actions))
			for i, a := range r.Actions {
				out[i] = a.Name
			}
			return out
		},
		sub: func(i int) string {
			a := r.Actions[i]
			return fmt.Sprintf("%s  bulk=%v", a.Label, a.Bulk)
		},
		add: func() {
			r.Actions = append(r.Actions, types.Action{Name: "new_action", Label: "New action"})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage("actions/"+fmt.Sprint(i), e.actionPage(idx, i))
		},
		remove: func(i int) {
			r.Actions = append(r.Actions[:i], r.Actions[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("actions", spec)
}

// actionPage edits a single custom action.
func (e *Editor) actionPage(idx, aidx int) tview.Primitive {
	a := &e.cfg.Resources[idx].Actions[aidx]
	return e.formShell("Action: "+a.Name, func(f *tview.Form) {
		e.str(f, "Name", a.Name, func(v string) { a.Name = v })
		e.str(f, "Label", a.Label, func(v string) { a.Label = v })
		e.pick(f, "Icon", iconOptions, a.Icon, func(v string) { a.Icon = v })
		e.pick(f, "Color", actionColorOptions, a.Color, func(v string) { a.Color = v })
		e.yesno(f, "Requires confirmation", a.RequiresConfirmation, func(v bool) { a.RequiresConfirmation = v })
		e.yesno(f, "Bulk action", a.Bulk, func(v bool) { a.Bulk = v })
		e.long(f, "Query", a.Query, func(v string) { a.Query = v })
		f.AddButton("Hooks", func() {
			if a.Hooks == nil {
				a.Hooks = &types.Hooks{}
			}
			e.showPage("actions/"+fmt.Sprint(aidx)+"/hooks", e.hooksPage(&a.Hooks, a.Name))
		})
	})
}
