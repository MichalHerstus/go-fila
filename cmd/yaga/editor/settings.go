package editor

import (
	"strings"

	"github.com/MichalHerstus/yaga/internal/types"
	"github.com/rivo/tview"
)

// auditCfg lazily allocates the top-level audit block on first write.
func (e *Editor) auditCfg() *types.AuditConfig {
	if e.cfg.Audit == nil {
		e.cfg.Audit = &types.AuditConfig{}
	}
	return e.cfg.Audit
}

// auditPage edits the generator-implicit audit log block (D2).
func (e *Editor) auditPage() tview.Primitive {
	a := &types.AuditConfig{}
	if e.cfg.Audit != nil {
		*a = *e.cfg.Audit
	}
	return e.formShell("Audit", func(f *tview.Form) {
		e.yesno(f, "Enabled", a.Enabled, func(v bool) { e.auditCfg().Enabled = v })
		e.str(f, "Table", a.Table, func(v string) { e.auditCfg().Table = v })
		e.yesno(f, "Include values", a.IncludeValues, func(v bool) { e.auditCfg().IncludeValues = v })
		e.str(f, "Policy", a.Policy, func(v string) { e.auditCfg().Policy = v })
		e.addButton(f, "Excluded resources", func() {
			path := "Audit/Excluded Resources"
			e.showPage(path, e.stringListPage(path, "Audit excluded resources",
				func() []string { return e.auditCfg().ExcludeResources },
				func(v []string) { e.auditCfg().ExcludeResources = v }))
		})
	})
}

// proceduresPage manages the sqlite stored-procedure definitions (D6).
func (e *Editor) proceduresPage() tview.Primitive {
	spec := listSpec{
		title: "Procedures",
		labels: func() []string {
			out := make([]string, len(e.cfg.Procedures))
			for i, p := range e.cfg.Procedures {
				out[i] = p.Name
			}
			return out
		},
		sub: func(i int) string {
			p := e.cfg.Procedures[i]
			first := ""
			for _, l := range strings.Split(p.SQL, "\n") {
				if strings.TrimSpace(l) != "" {
					first = strings.TrimSpace(l)
					break
				}
			}
			return first
		},
		add: func() {
			e.cfg.Procedures = append(e.cfg.Procedures, types.Procedure{Name: "sp_new_procedure"})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage(e.procedurePath(i), e.procedurePage(i))
		},
		remove: func(i int) {
			e.cfg.Procedures = append(e.cfg.Procedures[:i], e.cfg.Procedures[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("Procedures", spec)
}

// procedurePath returns the canonical path of the i-th procedure screen.
func (e *Editor) procedurePath(i int) string {
	return "Procedures/" + segName(e.cfg.Procedures[i].Name, i)
}

// procedurePage edits one stored-procedure definition.
func (e *Editor) procedurePage(i int) tview.Primitive {
	p := &e.cfg.Procedures[i]
	return e.formShell("Procedure: "+p.Name, func(f *tview.Form) {
		e.str(f, "Name", p.Name, func(v string) { p.Name = v })
		e.str(f, "Description", p.Description, func(v string) { p.Description = v })
		e.long(f, "SQL", p.SQL, func(v string) { p.SQL = v })
	})
}

// procedureIdxBySeg resolves a procedure path segment to its index.
func (e *Editor) procedureIdxBySeg(seg string) int {
	labels := make([]string, len(e.cfg.Procedures))
	for i, p := range e.cfg.Procedures {
		labels[i] = segName(p.Name, i)
	}
	return findSeg(labels, seg)
}

// pluginsPage manages the plugin declarations (D5).
func (e *Editor) pluginsPage() tview.Primitive {
	spec := listSpec{
		title: "Plugins",
		labels: func() []string {
			out := make([]string, len(e.cfg.Plugins))
			for i, pl := range e.cfg.Plugins {
				out[i] = pl.Name
			}
			return out
		},
		sub: func(i int) string { return e.cfg.Plugins[i].Source },
		add: func() {
			e.cfg.Plugins = append(e.cfg.Plugins, types.PluginConfig{Name: "my_plugin", Source: "./plugins/my_plugin"})
			e.markModified()
		},
		edit: func(i int) {
			e.showPage(e.pluginPath(i), e.pluginPage(i))
		},
		remove: func(i int) {
			e.cfg.Plugins = append(e.cfg.Plugins[:i], e.cfg.Plugins[i+1:]...)
			e.markModified()
		},
	}
	return e.recordList("Plugins", spec)
}

// pluginPath returns the canonical path of the i-th plugin screen.
func (e *Editor) pluginPath(i int) string {
	return "Plugins/" + segName(e.cfg.Plugins[i].Name, i)
}

// pluginPage edits one plugin declaration.
func (e *Editor) pluginPage(i int) tview.Primitive {
	p := &e.cfg.Plugins[i]
	return e.formShell("Plugin: "+p.Name, func(f *tview.Form) {
		e.str(f, "Name", p.Name, func(v string) { p.Name = v })
		e.str(f, "Source", p.Source, func(v string) { p.Source = v })
	})
}

// pluginIdxBySeg resolves a plugin path segment to its index.
func (e *Editor) pluginIdxBySeg(seg string) int {
	labels := make([]string, len(e.cfg.Plugins))
	for i, pl := range e.cfg.Plugins {
		labels[i] = segName(pl.Name, i)
	}
	return findSeg(labels, seg)
}
