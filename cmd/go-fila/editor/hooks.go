package editor

import (
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
)

// hooksPage manages the before/after hook lists of a Hooks block.
func (e *Editor) hooksPage(hooks **types.Hooks, title string) tview.Primitive {
	return e.formShell(title+" / Hooks", func(f *tview.Form) {
		f.AddButton("Before hooks", func() {
			e.showPage("hooks/before", e.hookListPage(hooks, true))
		})
		f.AddButton("After hooks", func() {
			e.showPage("hooks/after", e.hookListPage(hooks, false))
		})
	})
}

// hookListPage edits one of the before/after hook slices.
func (e *Editor) hookListPage(hooks **types.Hooks, before bool) tview.Primitive {
	get := func() *[]types.Hook {
		if *hooks == nil {
			*hooks = &types.Hooks{}
		}
		if before {
			return &(*hooks).Before
		}
		return &(*hooks).After
	}
	title := "After hooks"
	if before {
		title = "Before hooks"
	}
	spec := listSpec{
		title: title,
		labels: func() []string {
			hs := *get()
			out := make([]string, len(hs))
			for i, h := range hs {
				out[i] = h.Name
			}
			return out
		},
		sub: func(i int) string {
			hs := *get()
			if hs[i].Fn != "" {
				return "fn: " + hs[i].Fn
			}
			return "sql: " + hs[i].SQL
		},
		add: func() {
			hs := append(*get(), types.Hook{Name: "hook", Fn: ""})
			*get() = hs
			e.markModified()
		},
		edit: func(i int) {
			e.showPage("hooks/edit", e.hookPage(get, i))
		},
		remove: func(i int) {
			hs := *get()
			hs = append(hs[:i], hs[i+1:]...)
			*get() = hs
			e.markModified()
		},
	}
	return e.recordList("hooks/"+title, spec)
}

// hookPage edits a single hook (name + exactly one of fn/sql).
func (e *Editor) hookPage(get func() *[]types.Hook, idx int) tview.Primitive {
	hs := *get()
	h := &hs[idx]
	return e.formShell("Hook: "+h.Name, func(f *tview.Form) {
		e.str(f, "Name", h.Name, func(v string) { h.Name = v })
		e.yesno(f, "Use Go function", h.Fn != "", func(v bool) {
			if v {
				h.SQL = ""
				if h.Fn == "" {
					h.Fn = "MyHook"
				}
			} else {
				h.Fn = ""
			}
			e.markModified()
		})
		if h.Fn != "" {
			e.str(f, "Function", h.Fn, func(v string) { h.Fn = v })
		} else {
			e.long(f, "SQL", h.SQL, func(v string) { h.SQL = v })
		}
	})
}
