package editor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/go-fila/go-fila/internal/parser"
	"github.com/go-fila/go-fila/internal/schema"
	"github.com/go-fila/go-fila/internal/types"
	"github.com/rivo/tview"
	"gopkg.in/yaml.v3"
)

// finding is one problem reported by the Validate screen. goTo, when set,
// navigates to the exact editor page where the problem can be fixed.
type finding struct {
	kind   string // "error" | "warning"
	label  string
	detail string
	goTo   func()
}

// runValidation runs the full health check over the in-memory config: the
// structural validator (parser.ValidateAll against a YAML copy so defaults are
// not injected into the live config) plus the schema/queries analysis from the
// sync tool. Every schema finding carries a goTo that jumps to the relevant
// editor page (and highlights the offending column/field row).
func (e *Editor) runValidation() []finding {
	var out []finding

	data, err := yaml.Marshal(e.cfg)
	if err != nil {
		return []finding{{kind: "error", label: "yaml.Marshal failed", detail: err.Error()}}
	}
	var copyCfg types.Config
	if err := yaml.Unmarshal(data, &copyCfg); err != nil {
		return []finding{{kind: "error", label: "yaml.Unmarshal failed", detail: err.Error()}}
	}
	for _, verr := range parser.ValidateAll(&copyCfg) {
		out = append(out, finding{kind: "error", label: verr.Error(), goTo: e.structuralGoTo(verr.Error())})
	}

	rep := e.analyze()
	if rep.err != "" {
		return append(out, finding{kind: "error", label: rep.err})
	}
	for _, t := range rep.missingTabs {
		t := t
		parts := strings.SplitN(t, "->", 2)
		resName := strings.TrimSpace(parts[0])
		table := resName
		if len(parts) == 2 {
			table = strings.TrimSpace(parts[1])
		}
		out = append(out, finding{
			kind:   "error",
			label:  fmt.Sprintf("%s: table %q not found in schema", resName, table),
			detail: "Add the table to sql/migrations or fix the resource's table:",
			goTo:   e.resourceGoTo(resName),
		})
	}
	for _, m := range rep.missingCols {
		m := m
		out = append(out, finding{
			kind:   "warning",
			label:  fmt.Sprintf("%s.%s.%s: not a column of the resource's table", m.resource, m.ref.Section, m.ref.Column),
			detail: "Rename to an existing column, or add it to the table in sql/migrations",
			goTo:   e.columnGoTo(m.resource, m.ref),
		})
	}
	for _, q := range rep.missingQ {
		q := q
		resource := strings.SplitN(q.Origin, ".", 2)[0]
		f := finding{
			kind:   "error",
			label:  fmt.Sprintf("missing query %q (%s)", q.Name, q.Origin),
			detail: "Add the SQLC query to sql/queries, or generate it from the Sync screen",
		}
		if idx := e.resourceIdxByName(resource); idx >= 0 {
			f.goTo = e.queryGoTo(idx, q.Name)
		}
		out = append(out, f)
	}
	for _, fk := range rep.fkTargets {
		fk := fk
		out = append(out, finding{
			kind:   "warning",
			label:  "missing FK target query " + fk,
			detail: "Add it from the Sync screen (Generate missing queries)",
			goTo:   func() { e.showPage("Sync", e.syncPage()) },
		})
	}
	return out
}

var (
	resourceIdxRe = regexp.MustCompile(`resources\[(\d+)\]`)
	pageIdxRe     = regexp.MustCompile(`pages\[(\d+)\]`)
)

// structuralGoTo maps a parser.Validate error message to the editor page where
// the offending value is edited (when the message carries an index or a known
// section prefix); nil when the problem is not localizable.
func (e *Editor) structuralGoTo(msg string) func() {
	if m := resourceIdxRe.FindStringSubmatch(msg); m != nil {
		if idx, err := strconv.Atoi(m[1]); err == nil && idx >= 0 && idx < len(e.cfg.Resources) {
			return e.resourceGoTo(e.cfg.Resources[idx].Name)
		}
	}
	if m := pageIdxRe.FindStringSubmatch(msg); m != nil {
		if idx, err := strconv.Atoi(m[1]); err == nil && idx >= 0 && idx < len(e.cfg.Pages) {
			return func() { e.showPage(e.pagePath(idx), e.pagePage(idx)) }
		}
	}
	if strings.HasPrefix(msg, "panel.") {
		return func() { e.showPage("Panel", e.panelPage()) }
	}
	if strings.HasPrefix(msg, "at least one resource or page") {
		return func() { e.showPage("Resources", e.resourcesPage()) }
	}
	return nil
}

// resourceIdxByName returns the index of a resource by its name, or -1.
func (e *Editor) resourceIdxByName(name string) int {
	for i, r := range e.cfg.Resources {
		if r.Name == name {
			return i
		}
	}
	return -1
}

// resourceGoTo opens the resource editor page for the named resource.
func (e *Editor) resourceGoTo(name string) func() {
	return func() {
		idx := e.resourceIdxByName(name)
		if idx < 0 {
			return
		}
		e.showPage(e.resPath(idx), e.resourcePage(idx))
	}
}

// columnGoTo jumps to the editor page containing the referenced column/field
// and highlights the offending row.
func (e *Editor) columnGoTo(resource string, ref schema.ColumnRef) func() {
	return func() {
		name, prim := e.sectionJump(resource, ref)
		if prim == nil {
			return
		}
		e.showPage(name, prim)
	}
}

// sectionJump builds the editor page for a column reference's section and
// preselects the offending row, returning the page name + primitive. Sections
// that are edited as single fields (default_sort, kanban_field) open the
// parent list/card page without a row focus.
func (e *Editor) sectionJump(resource string, ref schema.ColumnRef) (string, tview.Primitive) {
	idx := e.resourceIdxByName(resource)
	if idx < 0 {
		return "", nil
	}
	name := e.resPath(idx)
	var prim tview.Primitive
	switch ref.Section {
	case "list.columns":
		name = e.resColumnsPath(idx)
		prim = e.columnsPage(idx)
	case "card.fields":
		name = e.resCardFieldsPath(idx)
		prim = e.cardFieldsPage(idx)
	case "detail.fields":
		name = e.resDetailFieldsPath(idx)
		prim = e.detailFieldsPage(idx)
	case "list.default_sort":
		name = e.resListPath(idx)
		prim = e.listPage(idx)
	case "card.searchable", "card.kanban_field", "card.default_sort":
		name = e.resCardPath(idx)
		prim = e.cardPage(idx)
	default:
		if strings.HasPrefix(ref.Section, "form.") {
			which := strings.TrimPrefix(ref.Section, "form.")
			which = strings.TrimSuffix(which, ".fields")
			name = e.resFormWhichPath(idx, which) + "/Fields"
			prim = e.formFieldsPage(idx, which)
		}
	}
	if prim == nil {
		return "", nil
	}
	if list, ok := prim.(*tview.List); ok && ref.Index >= 0 && ref.Index < list.GetItemCount() {
		list.SetCurrentItem(ref.Index)
	}
	return name, prim
}

// queryGoTo opens the resource's SQL-queries page and focuses the row for the
// missing query (when the resource references it).
func (e *Editor) queryGoTo(idx int, qname string) func() {
	return func() {
		prim := e.sqlQueriesPage(idx)
		e.showPage(e.resSQLPath(idx), prim)
		if list, ok := prim.(*tview.List); ok {
			for i := 0; i < list.GetItemCount(); i++ {
				if main, _ := list.GetItemText(i); main == qname {
					list.SetCurrentItem(i)
					break
				}
			}
		}
	}
}

// validatePage renders the validation results: one list row per problem,
// Enter jumps to the fix location. Mirrors the Sync screen layout (list + a
// row of buttons).
func (e *Editor) validatePage() tview.Primitive {
	fs := e.runValidation()

	list := tview.NewList().ShowSecondaryText(true)
	list.SetBorder(true).SetBorderColor(colBorder)
	list.SetMainTextColor(colText)
	list.SetSecondaryTextColor(colMuted)
	list.SetSelectedFocusOnly(true)

	errs, warns := 0, 0
	for _, f := range fs {
		if f.kind == "warning" {
			warns++
		} else {
			errs++
		}
	}
	if len(fs) == 0 {
		list.SetTitle("Validation")
		list.AddItem("[green]No problems found[-:-:-]", "The config is valid and every referenced table/column/query exists", 0, nil)
	} else {
		list.SetTitle(fmt.Sprintf("Validation (%d error(s), %d warning(s))", errs, warns))
		for _, f := range fs {
			f := f
			color := "red"
			if f.kind == "warning" {
				color = "yellow"
			}
			list.AddItem(fmt.Sprintf("[%s]%s[-:-:-]", color, f.label), f.detail, 0, func() {
				if f.goTo != nil {
					f.goTo()
				}
			})
		}
	}

	buttons := tview.NewForm()
	buttons.SetBorder(false)
	buttons.SetButtonBackgroundColor(colAccent)
	buttons.SetButtonTextColor(tcell.ColorWhite)
	e.addButton(buttons, "Refresh", func() {
		e.refreshPage("Validate", e.validatePage())
	})
	e.backButton(buttons)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(list, 0, 1, true)
	flex.AddItem(buttons, 3, 0, false)
	return flex
}
