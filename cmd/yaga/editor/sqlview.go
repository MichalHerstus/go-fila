package editor

import (
	"strings"

	"github.com/MichalHerstus/yaga/internal/schema"
	"github.com/rivo/tview"
)

// sqlViewer owns the parsed SQLC query map of one page plus its live read-only
// SQL rows. Reload re-reads the query files from disk (e.g. after editing them
// in an external editor) and re-renders every row on the page.
type sqlViewer struct {
	e    *Editor
	qs   map[string]schema.Query
	rows []func()
}

// newSQLViewer parses the SQLC query files once when a page is built.
func (e *Editor) newSQLViewer() *sqlViewer {
	return &sqlViewer{e: e, qs: schema.ParseQueries(e.queriesDir())}
}

// addRow appends a read-only SQL manifest row to the form. The label is drawn
// in the shared label column (empty indents the body to the value column);
// name is evaluated on every render so editing the query name updates the body
// live. The returned func re-renders the row.
func (v *sqlViewer) addRow(f *tview.Form, label string, name func() string) func() {
	tv := tview.NewTextView().SetDynamicColors(true).SetWrap(true).SetScrollable(false)
	tv.SetLabel(label)
	f.AddFormItem(tv)
	refresh := func() {
		body := sqlManifest(v.qs, name())
		tv.SetSize(sqlRowHeight(body), 0)
		tv.SetText(body)
	}
	v.rows = append(v.rows, refresh)
	refresh()
	return refresh
}

// reloadButton adds the page's "Reload SQL query" button: re-parses the query
// files from disk and re-renders all SQL rows. It is a read action — the
// config is not marked modified.
func (v *sqlViewer) reloadButton(f *tview.Form) {
	v.e.addButton(f, "Reload SQL query", func() {
		v.qs = schema.ParseQueries(v.e.queriesDir())
		for _, refresh := range v.rows {
			refresh()
		}
		v.e.toast("Reloaded SQL queries")
	})
}

// sqlManifest renders the bracketed SQL body of a query name, or a status line
// when the name is empty or does not resolve to a query file. The body is
// escaped so bracket-containing SQL (e.g. MSSQL [id]) is shown literally.
func sqlManifest(qs map[string]schema.Query, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "[::d]no query selected[-:-:-]"
	}
	q, ok := qs[name]
	if !ok {
		return "[yellow]query " + name + " not found (use Sync > Generate missing queries)[-:-:-]"
	}
	return "[::d]" + tview.Escape("["+q.Body+"]") + "[-:-:-]"
}

// sqlRowHeight estimates the terminal rows a manifest needs (wrapped at ~60
// cells, capped so a huge body cannot starve the rest of the form). Applied
// via SetSize on every render; Form.Draw relayouts from GetFieldHeight each
// frame, so short bodies take one row and long ones grow.
func sqlRowHeight(text string) int {
	lines := tview.TaggedStringWidth(text)/60 + 1
	if lines > 4 {
		lines = 4
	}
	if lines < 1 {
		lines = 1
	}
	return lines
}
