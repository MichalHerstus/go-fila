package editor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
	"github.com/go-fila/go-fila/internal/schema"
	"github.com/rivo/tview"
)

// sqlBase returns the directory that the sqlc paths (sqlc.queries_dir /
// sqlc.schema_dir) are relative to. sqlc runs inside the generated output dir,
// and init/init --demo write sql/{migrations,queries} there (default
// "./admin"), so when the config dir has no sql tree the paths resolve against
// the output dir instead. The config dir is authoritative when it has any sql
// tree (the generator copies configDir/sql into the output dir), with the
// output dir as fallback and the config dir as the final default.
func (e *Editor) sqlBase() string {
	base := filepath.Dir(e.configPath)
	for _, cand := range []string{base, filepath.Join(base, "admin")} {
		if sqlTreeExists(e.cfg.SQLC.QueriesDir, e.cfg.SQLC.SchemaDir, cand) {
			return cand
		}
	}
	return base
}

// sqlTreeExists reports whether either sqlc dir exists under base. Absolute
// paths resolve against nothing in the project and never match.
func sqlTreeExists(queriesDir, schemaDir, base string) bool {
	return sqlRelDir(queriesDir, "./sql/queries", base) ||
		sqlRelDir(schemaDir, "./sql/migrations", base)
}

// sqlRelDir reports whether the (possibly empty, possibly relative) sqlc
// directory rel exists under base.
func sqlRelDir(rel, def, base string) bool {
	if rel == "" {
		rel = def
	}
	return !filepath.IsAbs(rel) && isDir(filepath.Join(base, rel))
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// queriesDir returns the absolute directory of the SQLC query files.
func (e *Editor) queriesDir() string {
	dir := e.cfg.SQLC.QueriesDir
	if dir == "" {
		dir = "./sql/queries"
	}
	return filepath.Join(e.sqlBase(), dir)
}

// queryBase resolves the file backing a query name and its effective text (the
// staged rewrite when one exists, otherwise the on-disk content).
func (e *Editor) queryBase(name, qdir string) (path, text string, ok bool) {
	qs := schema.ParseQueries(qdir)
	q, found := qs[name]
	if !found {
		return "", "", false
	}
	path = filepath.Join(qdir, q.File)
	if staged, ok := e.pendingSQL[path]; ok {
		return path, staged, true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	return path, string(data), true
}

// stageQueryEdit rewrites the query's block in the file text and records it in
// pendingSQL so the next global save flushes it. Reverting to the original text
// removes the staging.
func (e *Editor) stageQueryEdit(path, name, baseText, newSQL string) {
	newText := schema.RewriteQueryBody(baseText, name, newSQL)
	if newText == baseText {
		if e.pendingSQL != nil {
			delete(e.pendingSQL, path)
		}
		return
	}
	if e.pendingSQL == nil {
		e.pendingSQL = map[string]string{}
	}
	e.pendingSQL[path] = newText
	e.markModified()
}

// sqlEditPage provides a full-height multi-line editor for one SQLC query's
// SQL body. Edits are staged and flushed with the global save.
func (e *Editor) sqlEditPage(name string) tview.Primitive {
	qdir := e.queriesDir()
	actions := tview.NewForm()
	actions.SetBorder(false)
	actions.SetButtonBackgroundColor(colAccent)
	actions.SetButtonTextColor(tcell.ColorWhite)

	path, base, ok := e.queryBase(name, qdir)
	if !ok {
		tv := tview.NewTextView().SetDynamicColors(true)
		tv.SetBorder(true).SetBorderColor(colBorder).SetTitle("SQL: " + name)
		fmt.Fprintf(tv, "[yellow]query %q not found in %s[-:-:-]\n\n[::d]Use Sync > Generate missing queries to create missing query files.[-:-:-]", name, qdir)
		e.backButton(actions)
		flex := tview.NewFlex().SetDirection(tview.FlexRow)
		flex.AddItem(tv, 0, 1, true)
		flex.AddItem(actions, 3, 0, false)
		return flex
	}

	qs := schema.ParseQueriesForFile(base, filepath.Base(path))
	raw := ""
	if q, present := qs[name]; present {
		raw = q.RawBody
	}

	ta := tview.NewTextArea()
	ta.SetText(raw, true)
	ta.SetBorder(true).SetBorderColor(colBorder).SetTitle(fmt.Sprintf("SQL: %s   (%s)", name, filepath.Base(path)))
	ta.SetBackgroundColor(colBg)
	ta.SetTextStyle(tcell.StyleDefault.Foreground(colText))

	ta.SetChangedFunc(func() {
		e.stageQueryEdit(path, name, base, ta.GetText())
	})
	e.addButton(actions, "Save", e.save)
	e.backButton(actions)

	flex := tview.NewFlex().SetDirection(tview.FlexRow)
	flex.AddItem(ta, 0, 1, true)
	flex.AddItem(actions, 3, 0, false)
	return flex
}
