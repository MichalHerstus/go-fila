// handler.go
//
// Generates the HTTP handlers for each resource in the admin panel
// (internal/panel/resources/{resource}/): list with dynamic WHERE/ORDER BY/
// LIMIT, detail via SQLC, create/update with raw SQL, delete, named actions
// and CSV export. It also holds shared helpers for building column/field
// definitions, scanning rows, and converting snake_case to PascalCase.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// generateResource writes all handler files for a single resource into its
// package directory: list.go, detail.go, create.go/update.go/delete.go,
// export.go and actions.go, depending on which sections the resource declares.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error if any handler file fails to write.
func (g *Generator) generateResource(r types.Resource) error {
	dir := filepath.Join(g.OutDir, "internal/panel/resources", strings.ToLower(r.Name))

	if r.List != nil {
		if err := g.generateListHandler(dir, r); err != nil {
			return err
		}
	}
	if r.Card != nil {
		if err := g.generateCardHandler(dir, r); err != nil {
			return err
		}
	}
	if r.Detail != nil {
		if err := g.generateDetailHandler(dir, r); err != nil {
			return err
		}
	}
	if r.Form != nil {
		if err := g.generateFormHandlers(dir, r); err != nil {
			return err
		}
	}
	if r.List != nil {
		if err := g.generateCSVHandler(dir, r); err != nil {
			return err
		}
	}
	if len(r.Actions) > 0 {
		if err := g.generateActionHandler(dir, r); err != nil {
			return err
		}
	}
	if hasBulkActions(r) {
		if err := g.generateBulkHandler(dir, r); err != nil {
			return err
		}
	}
	return nil
}

// hasBulkActions reports whether any action on the resource declares the bulk
// flag, which enables the bulk route and the bulk UI on the list view.
// Params: r (the resource definition).
// Returns: true when at least one action is bulk-enabled.
func hasBulkActions(r types.Resource) bool {
	for _, a := range r.Actions {
		if a.Bulk {
			return true
		}
	}
	return false
}

// hasActionPolicies reports whether any custom action on the resource declares
// a policy, which wraps the action and bulk routes in auth.ActionRBACMiddleware.
// Params: r (the resource definition).
// Returns: true when at least one action carries a role policy.
func hasActionPolicies(r types.Resource) bool {
	for _, a := range r.Actions {
		if a.Policy != "" {
			return true
		}
	}
	return false
}

// colDefsStr renders the []viewmodels.ColumnDef literal for a list of
// columns, filling in the label (defaults to the column name), field type,
// sortable/searchable flags and the static options map (nil when empty).
// Params: cols (list columns from the YAML config).
// Returns: a comma-joined Go source string for the column defs.
func colDefsStr(cols []types.Column) string {
	var defs []string
	for _, c := range cols {
		label := c.Label
		if label == "" {
			label = c.Name
		}
		opts := "nil"
		if len(c.Options) > 0 {
			opts = "map[string]string{"
			for k, v := range c.Options {
				opts += fmt.Sprintf("%q: %q, ", k, v)
			}
			opts += "}"
		}
		defs = append(defs, fmt.Sprintf("{Name: %q, Label: %q, FieldType: %q, Sortable: %t, Searchable: %t, Options: %s}", c.Name, label, c.Type, c.Sortable, c.Searchable, opts))
	}
	return strings.Join(defs, ",\n")
}

// fieldDefsStr renders the []viewmodels.ColumnDef literal for a list of form
// or detail fields, defaulting the label to the field name and the field type
// to "string".
// Params: fields (field definitions from the YAML config).
// Returns: a comma-joined Go source string for the field defs.
func fieldDefsStr(fields []types.Field) string {
	var defs []string
	for _, f := range fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		opts := "nil"
		if len(f.Options) > 0 {
			opts = "map[string]string{"
			for k, v := range f.Options {
				opts += fmt.Sprintf("%q: %q, ", k, v)
			}
			opts += "}"
		}
		ft := f.Type
		if ft == "" {
			ft = "string"
		}
		defs = append(defs, fmt.Sprintf("{Name: %q, Label: %q, FieldType: %q, Options: %s}", f.Name, label, ft, opts))
	}
	return strings.Join(defs, ",\n")
}

// fieldDefsFromDetail renders the field defs for a detail view; it is a thin
// wrapper around fieldDefsStr kept for readability at call sites.
// Params: fields (detail field definitions).
// Returns: the rendered Go source string.
func fieldDefsFromDetail(fields []types.Field) string {
	return fieldDefsStr(fields)
}

// tableName derives the SQL table name for a resource: the explicit "table"
// override when set, otherwise the lowercase resource name plus a plural "s"
// (e.g. "User" -> "users"). Introspected projects emit "table" whenever the
// convention does not match the real table (e.g. "Zamestnanec" -> table
// "Zamestnanec", not "zamestnanecs").
// Params: r (the resource definition).
// Returns: the SQL table name.
func tableName(r types.Resource) string {
	if r.Table != "" {
		return r.Table
	}
	return strings.ToLower(r.Name) + "s"
}

// idColumn returns the name of the row-key column for a resource: the explicit
// "id_column" override when set, otherwise "id". Row maps in list/card/detail
// views are keyed by the real column name, so MSSQL tables with an "ID" column
// must emit id_column to keep View/Edit/action links working.
// Params: r (the resource definition).
// Returns: the row-key column name.
func idColumn(r types.Resource) string {
	if r.IDColumn != "" {
		return r.IDColumn
	}
	return "id"
}

// resourceTitle returns the page title used for a resource view: the YAML
// label when set, falling back to the resource name.
// Params: r (the resource definition).
// Returns: the display title string.
func resourceTitle(r types.Resource) string {
	if r.Label != "" {
		return r.Label
	}
	return r.Name
}

// fkLabelJoin describes a LEFT JOIN a generated list/card/export handler must
// emit so an FK label column ({fk}_label) can select the foreign table's label
// column. It mirrors the JOIN the introspector writes into the SQLC list/detail
// queries.
type fkLabelJoin struct {
	colName    string
	selectPart string
	fromPart   string
}

// labelJoins reconstructs the LEFT JOINs the SQLC queries use for FK label
// columns: for every view column named "{fk}_label" with a matching relation
// form field (options_query "List{Foreign}", options_value, options_label) it
// produces the aliased SELECT fragment and the JOIN clause. Columns without a
// matching relation field are skipped so the emitted SQL keeps the historical
// (unjoined) behavior.
// Params: r (the resource definition), colNames (the view's column/field names).
// Returns: the join specs, possibly empty.
func (g *Generator) labelJoins(r types.Resource, colNames []string) []fkLabelJoin {
	var joins []fkLabelJoin
	for _, c := range colNames {
		if !strings.HasSuffix(c, "_label") {
			continue
		}
		base := strings.TrimSuffix(c, "_label")
		f := relationFormField(r, base)
		if f == nil || f.OptionsQuery == "" {
			continue
		}
		foreignName := strings.TrimPrefix(f.OptionsQuery, "List")
		var foreign *types.Resource
		for i := range g.Config.Resources {
			if g.Config.Resources[i].Name == foreignName {
				foreign = &g.Config.Resources[i]
				break
			}
		}
		if foreign == nil {
			continue
		}
		ftable := tableName(*foreign)
		joins = append(joins, fkLabelJoin{
			colName:    c,
			selectPart: fmt.Sprintf("f_%s.%s AS %s", ftable, f.OptionsLabel, c),
			fromPart:   fmt.Sprintf("LEFT JOIN %s f_%s ON f_%s.%s = t.%s", ftable, ftable, ftable, f.OptionsValue, base),
		})
	}
	return joins
}

// relationFormField returns the relation-typed form field with the given name,
// searching the create then the update form action. nil when absent.
// Params: r (the resource definition), name (the field name to find).
// Returns: the matching field, or nil.
func relationFormField(r types.Resource, name string) *types.Field {
	var fields []types.Field
	if r.Form != nil {
		if r.Form.Create != nil {
			fields = append(fields, r.Form.Create.Fields...)
		}
		if r.Form.Update != nil {
			fields = append(fields, r.Form.Update.Fields...)
		}
	}
	for i := range fields {
		if fields[i].Name == name && fields[i].Type == "relation" {
			return &fields[i]
		}
	}
	return nil
}

// listSelectFrom renders the SELECT column list and FROM fragment for the raw
// list/card/export queries. When the view has FK label columns backed by
// relation form fields, real columns are qualified with the "t" alias, label
// columns select the joined foreign table's column, and the FROM carries the
// LEFT JOINs; colPrefix ("t.") and tableRef ("{table} t") are returned so the
// WHERE/ORDER BY clauses stay unambiguous. Without joins the historical
// unqualified fragments are returned so generated output stays unchanged.
// Params: r (the resource definition), tName (the SQL table name), colNames
// (the view's column/field names).
// Returns: the SELECT list, the FROM fragment (alias + JOINs), the column
// prefix, the table reference, and whether any JOINs were emitted.
func (g *Generator) listSelectFrom(r types.Resource, tName string, colNames []string) (selectFrag, fromFrag, colPrefix, tableRef string, hasJoins bool) {
	joins := g.labelJoins(r, colNames)
	if len(joins) == 0 {
		return strings.Join(colNames, ", "), tName, "", tName, false
	}
	labelCols := map[string]bool{}
	for _, j := range joins {
		labelCols[j.colName] = true
	}
	sel := make([]string, 0, len(colNames))
	for _, c := range colNames {
		if labelCols[c] {
			continue
		}
		sel = append(sel, "t."+c)
	}
	for _, j := range joins {
		sel = append(sel, j.selectPart)
	}
	fromParts := []string{tName + " t"}
	for _, j := range joins {
		fromParts = append(fromParts, j.fromPart)
	}
	return strings.Join(sel, ", "), strings.Join(fromParts, " "), "t.", tName + " t", true
}

// generateListHandler writes list.go for a resource: a List(db) handler that
// reads page/search/sort/order query parameters, builds a dynamic WHERE/ORDER
// BY/LIMIT query against the plural table name, counts the total rows for
// pagination, scans the listed columns and renders the resource list view.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateListHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	tName := tableName(r)

	var searchCols []string
	var sortableCols []string
	var colNames []string
	for _, c := range r.List.Columns {
		colNames = append(colNames, c.Name)
		if c.Searchable {
			searchCols = append(searchCols, c.Name)
		}
		if c.Sortable {
			sortableCols = append(sortableCols, c.Name)
		}
	}

	selectFrag, fromFrag, colPrefix, _, _ := g.listSelectFrom(r, tName, colNames)

	perPage := r.List.PerPage
	if perPage < 1 {
		perPage = 20
	}

	var sb strings.Builder

	// Package declaration and imports
	sb.WriteString(fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "math"
    "net/http"
    "strconv"
    "strings"

    %q
    %q
    auth %q
    httperr %q
    layoutviews %q
)

func List(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        page, _ := strconv.Atoi(r.URL.Query().Get("page"))
        if page < 1 {
            page = 1
        }
        perPage := %d
        offset := (page - 1) * perPage

        search := r.URL.Query().Get("search")
        sort := r.URL.Query().Get("sort")
        order := r.URL.Query().Get("order")
        if order == "" {
            order = "asc"
        }

        if sort == "" {`+func() string {
		sortField := strings.TrimPrefix(r.List.DefaultSort, "-")
		sortOrder := "asc"
		if r.List.DefaultSort != sortField {
			sortOrder = "desc"
		}
		return fmt.Sprintf(`
            sort = %q
            order = %q`, sortField, sortOrder)
	}()+`
        }

        if order != "asc" && order != "desc" {
            order = "asc"
        }

        validSorts := map[string]bool{`, pkgName,
		g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		g.moduleImport("internal/panel/auth"), g.moduleImport("internal/panel/httperr"), g.moduleImport("internal/views/layout"), perPage))

	// Valid sort columns
	for i, c := range sortableCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%q: true", c))
	}
	sb.WriteString(`}
        if sort != "" && !validSorts[sort] {
            sort = ""
        }

`)
	searchableColsLiteral := quoteList(searchCols, colPrefix)

	// Build the args + WHERE/ORDER/LIMIT construction. Sqlite binds ? args
	// positionally in SQL text order, so search args must come before the
	// LIMIT/OFFSET args; postgres uses numbered $N so order does not matter.
	var listCore string
	if g.isSQLite() {
		listCore = fmt.Sprintf(`        var args []interface{}

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, col+" LIKE ?")
                args = append(args, "%%"+search+"%%")
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " LIMIT ? OFFSET ?"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        fullArgs = append(fullArgs, perPage, offset)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()

`, searchableColsLiteral, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	} else if g.isMSSQL() {
		listCore = fmt.Sprintf(`        var args []interface{}
        args = append(args, perPage, offset)
        argIdx := 3

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, fmt.Sprintf("%%s LIKE $%%d", col, argIdx))
                args = append(args, "%%"+search+"%%")
                argIdx++
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }
        if orderSQL == "" {
            orderSQL = " ORDER BY (SELECT NULL)"
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()

`, searchableColsLiteral, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	} else {
		listCore = fmt.Sprintf(`        var args []interface{}
        args = append(args, perPage, offset)
        argIdx := 3

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, fmt.Sprintf("%%s ILIKE $%%d", col, argIdx))
                args = append(args, "%%"+search+"%%")
                argIdx++
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " LIMIT $1 OFFSET $2"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()

`, searchableColsLiteral, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	}
	sb.WriteString(listCore)

	sb.WriteString(`        var items []map[string]interface{}
        for rows.Next() {
            ` + scanFields(colNames, true) + `
            items = append(items, item)
        }
        if !totalSet {
            total = int64(page * perPage)
        }

        totalPages := int(math.Ceil(float64(total) / float64(perPage)))

        vd := &viewmodels.ListData{
            Items:      items,
            Page:       page,
            PerPage:    perPage,
            Total:      int(total),
            TotalPages: totalPages,
            Search:     search,
            Sort:       sort,
            Order:      order,
            Columns: []viewmodels.ColumnDef{
                ` + colDefsStr(r.List.Columns) + `,
            },
            Resource:  ` + fmt.Sprintf("%q", r.Name) + `,
            PanelPath: ` + fmt.Sprintf("%q", g.Config.Panel.Path) + `,
            CSRFToken: auth.CSRFToken(r, w),
        }

        ` + fmt.Sprintf("layoutviews.Base(%q, %q, viewmodels.DefaultTheme(), auth.UserName(r), auth.CSRFToken(r, w), views.%sList(vd)).Render(r.Context(), w)", resourceTitle(r), g.Config.Panel.Path, r.Name) + `
    }
}
`)

	return os.WriteFile(filepath.Join(dir, "list.go"), []byte(sb.String()), 0644)
}

// validSortsMapStr renders the Go source for a map of sortable column names
// (all mapping to true), used to whitelist sort parameters in the generated
// list handler.
// Params: cols (names of the sortable columns).
// Returns: the Go literal string for the map.
func validSortsMapStr(cols []string) string {
	var parts []string
	for _, c := range cols {
		parts = append(parts, fmt.Sprintf("%q: true", c))
	}
	return strings.Join(parts, ", ")
}

// scanFields generates the Go source that scans a database row into a
// map[string]interface{}: it declares one interface{} variable per column,
// appends their addresses to a scan slice and populates the item map. When
// withTotal is true the emitted code additionally scans the trailing
// COUNT(*) OVER() AS _total column into the outer `total` variable.
// Params: cols (the column names to scan), withTotal (also scan _total).
// Returns: the multi-line Go source string to inline in the generated handler.
func scanFields(cols []string, withTotal bool) string {
	var scans []string
	scans = append(scans, `        item := make(map[string]interface{})`)
	scans = append(scans, `        var scanArgs []interface{}`)
	for _, c := range cols {
		scans = append(scans, fmt.Sprintf(`        var val_%s interface{}`, c))
		scans = append(scans, fmt.Sprintf(`        scanArgs = append(scanArgs, &val_%s)`, c))
	}
	if withTotal {
		scans = append(scans, `        var totalVal interface{}`)
		scans = append(scans, `        scanArgs = append(scanArgs, &totalVal)`)
	}
	scans = append(scans, `        if err := rows.Scan(scanArgs...); err != nil {`)
	scans = append(scans, `            httperr.Internal(w, err)`)
	scans = append(scans, `            return`)
	scans = append(scans, `        }`)
	if withTotal {
		scans = append(scans, `        switch tv := totalVal.(type) {`)
		scans = append(scans, `        case int64:`)
		scans = append(scans, `            total = tv`)
		scans = append(scans, `        case float64:`)
		scans = append(scans, `            total = int64(tv)`)
		scans = append(scans, `        }`)
		scans = append(scans, `        totalSet = true`)
	}
	for _, c := range cols {
		scans = append(scans, fmt.Sprintf(`        item[%q] = val_%s`, c, c))
	}
	return strings.Join(scans, "\n")
}

// quoteList renders a comma-separated list of double-quoted Go string literals
// for the given words, each optionally prefixed (the prefix is used to qualify
// searchable columns with the table alias when the list/card query has FK LEFT
// JOINs).
// Params: words (the strings to quote), prefix (optional column prefix).
// Returns: a comma-separated list of quoted Go literals.
func quoteList(words []string, prefix string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = fmt.Sprintf("%q", prefix+w)
	}
	return strings.Join(q, ", ")
}

// generateCardHandler writes card.go for a resource: a Cards(db) handler that
// paginates and searches the resource exactly like the list handler (LIMIT =
// card Rows * Columns) scanning the card fields. When KanbanField names a
// select field, the fetched rows are grouped into columns keyed by that field's
// option values and rendered as a kanban board; otherwise the rows render as a
// Columns x Rows card grid.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateCardHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	tName := tableName(r)
	card := r.Card
	panelPath := g.Config.Panel.Path

	var fieldNames []string
	var searchCols []string
	for _, f := range card.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	for _, s := range card.Searchable {
		searchCols = append(searchCols, s)
	}
	selectFrag, fromFrag, colPrefix, _, _ := g.listSelectFrom(r, tName, fieldNames)
	searchable := quoteList(searchCols, colPrefix)

	kanban := card.KanbanField != ""
	perPage, rows, cols := card.Rows*card.Columns, card.Rows, card.Columns

	sortStmt := ""
	if card.DefaultSort != "" {
		sortField := strings.TrimPrefix(card.DefaultSort, "-")
		sortOrder := "asc"
		if card.DefaultSort != sortField {
			sortOrder = "desc"
		}
		sortStmt = fmt.Sprintf(`        if sort == "" {
            sort = %q
            order = %q
        }
`, sortField, sortOrder)
	}

	var queryCore string
	if g.isSQLite() {
		queryCore = fmt.Sprintf(`        var args []interface{}

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, col+" LIKE ?")
                args = append(args, "%%"+search+"%%")
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " LIMIT ? OFFSET ?"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        fullArgs = append(fullArgs, perPage, offset)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()
`, searchable, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	} else if g.isMSSQL() {
		queryCore = fmt.Sprintf(`        var args []interface{}
        args = append(args, perPage, offset)
        argIdx := 3

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, fmt.Sprintf("%%s LIKE $%%d", col, argIdx))
                args = append(args, "%%"+search+"%%")
                argIdx++
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }
        if orderSQL == "" {
            orderSQL = " ORDER BY (SELECT NULL)"
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()
`, searchable, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	} else {
		queryCore = fmt.Sprintf(`        var args []interface{}
        args = append(args, perPage, offset)
        argIdx := 3

        var whereClauses []string
        if search != "" {
            searchableCols := []string{%s}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, fmt.Sprintf("%%s ILIKE $%%d", col, argIdx))
                args = append(args, "%%"+search+"%%")
                argIdx++
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %%s%%s %%s", %s, sort, order)
        }

        var total int64
        totalSet := false
        dataQuery := "SELECT %s, COUNT(*) OVER() AS _total FROM %s" + whereSQL + orderSQL + " LIMIT $1 OFFSET $2"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()
`, searchable, fmt.Sprintf("%q", colPrefix), selectFrag, fromFrag)
	}

	kanbanCode := ""
	kanbanColumnsExpr := "nil"
	if kanban {
		var optKeys []string
		var optLabelMap []string
		for _, f := range card.Fields {
			if f.Name == card.KanbanField {
				for k := range f.Options {
					optKeys = append(optKeys, k)
				}
				sort.Strings(optKeys)
				for _, k := range optKeys {
					optLabelMap = append(optLabelMap, fmt.Sprintf("%q: %q", k, f.Options[k]))
				}
			}
		}
		kanbanCode = fmt.Sprintf(`        var kanbanColumns []viewmodels.CardColumnData
        bucket := map[string]*viewmodels.CardColumnData{}
        var bucketOrder []string
        {
            optLabels := map[string]string{%s}
            for _, k := range []string{%s} {
                bucket[k] = &viewmodels.CardColumnData{Key: k, Label: optLabels[k]}
                bucketOrder = append(bucketOrder, k)
            }
        }
        for _, item := range items {
            key := viewmodels.OptionValue(item[%q])
            if bucket[key] == nil {
                bucket[key] = &viewmodels.CardColumnData{Key: key, Label: key}
                bucketOrder = append(bucketOrder, key)
            }
            bucket[key].Items = append(bucket[key].Items, item)
        }
        for _, k := range bucketOrder {
            kanbanColumns = append(kanbanColumns, *bucket[k])
        }
`, strings.Join(optLabelMap, ", "), quoteList(optKeys, ""), card.KanbanField)
		kanbanColumnsExpr = "kanbanColumns"
	}

	itemsAssignment := `        for rows.Next() {
            ` + scanFields(fieldNames, true) + `
            items = append(items, item)
        }
        if !totalSet {
            total = int64(page * perPage)
        }
`

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "math"
    "net/http"
    "strconv"
    "strings"

    %q
    %q
    auth %q
    httperr %q
    layoutviews %q
)

func Cards(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        page, _ := strconv.Atoi(r.URL.Query().Get("page"))
        if page < 1 {
            page = 1
        }
        perPage := %d
        offset := (page - 1) * perPage

        search := r.URL.Query().Get("search")
        sort := r.URL.Query().Get("sort")
        order := r.URL.Query().Get("order")
        if order == "" {
            order = "asc"
        }

%s
        if order != "asc" && order != "desc" {
            order = "asc"
        }
%s
        var items []map[string]interface{}
%s
        totalPages := int(math.Ceil(float64(total) / float64(perPage)))

%s
        vd := &viewmodels.CardData{
            Items:          items,
            Page:           page,
            PerPage:        perPage,
            Total:          int(total),
            TotalPages:     totalPages,
            Search:         search,
            Sort:           sort,
            Order:          order,
            Fields: []viewmodels.ColumnDef{
%s,
            },
            Columns:       %d,
            Rows:          %d,
            Kanban:        %t,
            KanbanField:   %q,
            KanbanColumns: %s,
            Resource:      %q,
            PanelPath:     %q,
        }

        layoutviews.Base(%q, %q, viewmodels.DefaultTheme(), auth.UserName(r), auth.CSRFToken(r, w), views.%sCards(vd)).Render(r.Context(), w)
    }
}
`, pkgName,
		g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		g.moduleImport("internal/panel/auth"), g.moduleImport("internal/panel/httperr"), g.moduleImport("internal/views/layout"),
		perPage,
		sortStmt,
		queryCore,
		itemsAssignment,
		kanbanCode,
		fieldDefsStr(card.Fields),
		cols, rows, kanban, card.KanbanField,
		kanbanColumnsExpr,
		r.Name, panelPath, resourceTitle(r), panelPath, r.Name)

	return os.WriteFile(filepath.Join(dir, "card.go"), []byte(code), 0644)
}

// generateDetailHandler writes detail.go for a resource: a Detail(db) handler
// that parses the :id path parameter, calls the SQLC detail query, maps the
// returned struct fields into a map and renders the detail view.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateDetailHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	queryName := r.Detail.Query
	if queryName == "" {
		queryName = "GetByID"
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"

    %q
    %q
    %q
    auth %q
    httperr %q
    layoutviews %q
)

func Detail(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

        item, err := data.New(db).%s(r.Context(), %s(id))
        if err != nil {
            httperr.NotFound(w, err)
            return
        }

        itemMap := map[string]interface{}{
%s        }

        vd := &viewmodels.DetailData{
            Item: itemMap,
            Fields: []viewmodels.ColumnDef{
                %s,
            },
            Resource:  %q,
            PanelPath: %q,
            CSRFToken: auth.CSRFToken(r, w),
        }

        layoutviews.Base(%q, %q, viewmodels.DefaultTheme(), auth.UserName(r), auth.CSRFToken(r, w), views.%sDetail(vd)).Render(r.Context(), w)
    }
}
`, pkgName,
		g.moduleImport("internal/data"), g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		g.moduleImport("internal/panel/auth"), g.moduleImport("internal/panel/httperr"), g.moduleImport("internal/views/layout"),
		queryName,
		g.idGoTypeForResource(r),
		detailFieldMap(r.Detail.Fields),
		fieldDefsFromDetail(r.Detail.Fields),
		r.Name,
		g.Config.Panel.Path,
		resourceTitle(r),
		g.Config.Panel.Path,
		r.Name)

	return os.WriteFile(filepath.Join(dir, "detail.go"), []byte(code), 0644)
}

// detailFieldMap generates the entries of the itemMap literal in the detail
// handler, mapping each snake_case field name to the corresponding PascalCase
// field of the SQLC result struct.
// Params: fields (detail field definitions).
// Returns: the indented Go source lines mapping field names to struct fields.
func detailFieldMap(fields []types.Field) string {
	var entries []string
	for _, f := range fields {
		goName := snakeToPascal(f.Name)
		entries = append(entries, fmt.Sprintf("            %q: item.%s,", f.Name, goName))
	}
	return strings.Join(entries, "\n")
}

// snakeToPascal converts a column name to the PascalCase struct field name
// that sqlc generates. sqlc lowercases the whole identifier, splits only on
// underscores, and maps the "id" segment to "ID" (e.g. "user_role_id" ->
// "UserRoleID", "CeleJmeno" -> "Celejmeno", "ZamestnanecID" -> "Zamestnanecid").
// Params: s (a column name, e.g. from a YAML field definition).
// Returns: the PascalCase variant.
func snakeToPascal(s string) string {
	parts := strings.Split(strings.ToLower(s), "_")
	for i, p := range parts {
		if p == "id" {
			parts[i] = "ID"
		} else if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// generateFormHandlers dispatches to the create, update and delete handler
// generators based on which form sections the resource declares.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error if any generated handler fails.
func (g *Generator) generateFormHandlers(dir string, r types.Resource) error {
	if r.Form.Create != nil {
		if err := g.generateCreateHandler(dir, r); err != nil {
			return err
		}
	}
	if r.ImportCSV {
		if err := g.generateImportHandler(dir, r); err != nil {
			return err
		}
	}
	if r.Form.Update != nil {
		if err := g.generateUpdateHandler(dir, r); err != nil {
			return err
		}
	}
	if r.Form.Delete != nil {
		if err := g.generateDeleteHandler(dir, r); err != nil {
			return err
		}
	}
	return nil
}

// generateDeleteHandler writes delete.go: a Delete(db) handler that parses the
// :id path parameter, runs "DELETE FROM {table} WHERE id = $1" via ExecContext
// and redirects back to the resource list.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateDeleteHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	listPath := fmt.Sprintf("%s/%s", g.Config.Panel.Path, pkgName)
	tName := tableName(r)

	hooksImport := ""
	if g.hookBlockEmits(r.Form.Delete.Hooks) {
		hooksImport = fmt.Sprintf("    hooks %q\n", g.moduleImport("internal/hooks"))
	}
	procsImport := ""
	if g.hookUsesProc(r.Form.Delete.Hooks) {
		procsImport = g.procImport()
	}
	authImport := ""
	if g.auditFor(r) != nil {
		authImport = fmt.Sprintf("    auth %q\n", g.moduleImport("internal/panel/auth"))
	}

	hasHooks := g.hookBlockEmits(r.Form.Delete.Hooks)
	middle := ""
	if hasHooks {
		middle += fmt.Sprintf(`        scope := hooks.Scope{
            Table:  %q,
            Action: "delete",
            ID:     int64(id),
        }
`, tName)
		middle += g.hookCallsStr(r.Form.Delete.Hooks.Before, "scope", "        ") + "\n"
	}
	if g.auditFor(r) != nil {
		middle += auditTxBeginStr("        ")
		middle += fmt.Sprintf(`        _, err = tx.ExecContext(r.Context(), "DELETE FROM %s WHERE %s = $1", int64(id))
        if err != nil {
            httperr.Internal(w, err)
            return
        }
`, tName, idColumn(r))
		middle += g.auditInsertStr(r, "delete", "strconv.FormatInt(int64(id), 10)", `""`, "        ") + "\n"
		middle += auditTxCommitStr("        ")
	} else {
		middle += fmt.Sprintf(`        _, err = db.ExecContext(r.Context(), "DELETE FROM %s WHERE %s = $1", int64(id))
        if err != nil {
            httperr.Internal(w, err)
            return
        }
`, tName, idColumn(r))
	}
	if hasHooks {
		middle += g.hookCallsStr(r.Form.Delete.Hooks.After, "scope", "        ") + "\n"
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"
    httperr %q
%s%s%s)

func Delete(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

%s
        http.Redirect(w, r, %q, http.StatusFound)
    }
}
`, pkgName, g.moduleImport("internal/panel/httperr"), authImport, hooksImport, procsImport, middle, listPath)

	return os.WriteFile(filepath.Join(dir, "delete.go"), []byte(code), 0644)
}

// generateCSVHandler writes export.go: an ExportCSV(db) handler that selects
// the exported columns ordered by the first column and streams them as an
// attachment CSV file using encoding/csv. When list.export is set it exports
// only that subset (with Label headers); otherwise all list columns with
// raw column-name headers (historical behavior).
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateCSVHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	tName := tableName(r)

	exportCols := r.List.Columns
	useLabels := false
	if len(r.List.Export) > 0 {
		var filtered []types.Column
		for _, want := range r.List.Export {
			for _, c := range r.List.Columns {
				if c.Name == want {
					filtered = append(filtered, c)
					break
				}
			}
		}
		exportCols = filtered
		useLabels = true
	}

	var colNames []string
	for _, c := range exportCols {
		colNames = append(colNames, c.Name)
	}
	selectFrag, fromFrag, _, _, _ := g.listSelectFrom(r, tName, colNames)

	headerCode := `        out := make([]string, len(cols))
        for i, c := range cols {
            out[i] = csvSafe(c)
        }
        wr.Write(out)
`
	if useLabels {
		var labels []string
		for _, c := range exportCols {
			lbl := c.Label
			if lbl == "" {
				lbl = c.Name
			}
			labels = append(labels, fmt.Sprintf("csvSafe(%q)", lbl))
		}
		headerCode = fmt.Sprintf("        wr.Write([]string{%s})\n", strings.Join(labels, ", "))
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "encoding/csv"
    "net/http"
    httperr %q
)

func ExportCSV(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        query := "SELECT %s FROM %s ORDER BY 1"
        rows, err := db.QueryContext(r.Context(), query)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer rows.Close()

        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", "attachment; filename=%s_export.csv")
        wr := csv.NewWriter(w)
        defer wr.Flush()

        cols, _ := rows.Columns()
%s
        vals := make([]string, len(cols))
        ptrs := make([]interface{}, len(cols))
        for i := range vals {
            ptrs[i] = &vals[i]
        }
        for rows.Next() {
            rows.Scan(ptrs...)
            for i := range vals {
                vals[i] = csvSafe(vals[i])
            }
            wr.Write(vals)
        }
    }
}

// csvSafe neutralizes spreadsheet formula injection by prefixing a single
// quote when a value begins with a formula trigger character (=, +, -, @, tab
// or carriage return), which Excel/Sheets would otherwise evaluate.
func csvSafe(s string) string {
    if len(s) > 0 {
        switch s[0] {
        case '=', '+', '-', '@', '\t', '\r':
            return "'" + s
        }
    }
    return s
}
`, pkgName, g.moduleImport("internal/panel/httperr"), selectFrag, fromFrag, tName, headerCode)

	return os.WriteFile(filepath.Join(dir, "export.go"), []byte(code), 0644)
}

// actionExecSQL returns the SQL text an action executes at request time: the
// raw Query when set, otherwise the driver-appropriate stored procedure call
// when Proc is set (and the driver is not sqlite). Empty when the action has
// neither (a sqlite proc-only action, or an action that only runs hooks).
// Params: a (the action definition).
// Returns: the SQL to execute, or "".
func (g *Generator) actionExecSQL(a types.Action) string {
	if a.Query != "" {
		return a.Query
	}
	if a.Proc != "" && !g.isSQLite() {
		return g.procSQL(a.Proc)
	}
	return ""
}

// generateActionHandler writes actions.go: an Action(db) handler that parses
// the :id and :action path parameters and switches over the configured action
// names, executing each action's SQL via ExecContext, then redirecting to the
// resource list. Unknown action names return 404.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateActionHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	listPath := fmt.Sprintf("%s/%s", g.Config.Panel.Path, pkgName)
	tName := tableName(r)

	hasHooks := false
	hasProcs := false
	auditCfg := g.auditFor(r)
	auditAny := false
	var dispatch []string
	for _, a := range r.Actions {
		useHooks := g.hookBlockEmits(a.Hooks)
		if useHooks {
			hasHooks = true
		}
		if g.hookUsesProc(a.Hooks) {
			hasProcs = true
		}
		exec := g.actionExecSQL(a)
		procExec := g.actionProcExec(a, "int64(id)")
		if procExec != "" {
			hasProcs = true
		}
		auditAction := auditCfg != nil && exec != "" && procExec == ""
		if auditAction {
			auditAny = true
		}
		var body []string
		if useHooks {
			body = append(body, fmt.Sprintf(`            scope := hooks.Scope{
                Table:  %q,
                Action: %q,
                ID:     int64(id),
            }`, tName, a.Name))
			if before := g.hookCallsStr(a.Hooks.Before, "scope", "            "); before != "" {
				body = append(body, before)
			}
		}
		if auditAction {
			body = append(body, auditTxBeginStr("            "))
			body = append(body, fmt.Sprintf(`            _, err = tx.ExecContext(r.Context(), %q, int64(id))
            if err != nil {
                httperr.Internal(w, err)
                return
            }`, exec))
			body = append(body, g.auditInsertStr(r, a.Name, "strconv.FormatInt(int64(id), 10)", `""`, "            "))
			body = append(body, auditTxCommitStr("            "))
		} else if procExec != "" {
			body = append(body, procExec)
		} else if exec != "" {
			body = append(body, fmt.Sprintf(`            _, err := db.ExecContext(r.Context(), %q, int64(id))
            if err != nil {
                httperr.Internal(w, err)
                return
            }`, exec))
		}
		if useHooks {
			if after := g.hookCallsStr(a.Hooks.After, "scope", "            "); after != "" {
				body = append(body, after)
			}
		}
		dispatch = append(dispatch, fmt.Sprintf(`    case %q:
        {
%s
        }
`, a.Name, strings.Join(body, "\n")))
	}

	hooksImport := ""
	if hasHooks {
		hooksImport = fmt.Sprintf("    hooks %q\n", g.moduleImport("internal/hooks"))
	}
	procsImport := ""
	if hasProcs {
		procsImport = g.procImport()
	}
	authImport := ""
	if auditAny {
		authImport = fmt.Sprintf("    auth %q\n", g.moduleImport("internal/panel/auth"))
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"
    httperr %q
%s%s%s)

func Action(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        actionName := r.PathValue("action")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

        switch actionName {
%s    default:
            http.Error(w, "unknown action", http.StatusNotFound)
            return
        }

        http.Redirect(w, r, %q, http.StatusFound)
    }
}
`, pkgName, g.moduleImport("internal/panel/httperr"), authImport, hooksImport, procsImport, strings.Join(dispatch, "\n"), listPath)

	return os.WriteFile(filepath.Join(dir, "actions.go"), []byte(code), 0644)
}

// generateBulkHandler writes bulk.go: a Bulk(db) handler that parses the
// :action path parameter and the repeated "ids" form values, switching over the
// configured bulk actions and executing each action's SQL once per selected id,
// then redirecting to the resource list. Unknown action names return 404.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateBulkHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	listPath := fmt.Sprintf("%s/%s", g.Config.Panel.Path, pkgName)

	hasExec := false
	hasProcs := false
	for _, a := range r.Actions {
		if !a.Bulk {
			continue
		}
		if g.actionExecSQL(a) != "" {
			hasExec = true
		}
		if g.actionProcExec(a, "id") != "" {
			hasProcs = true
		}
	}

	var dispatch []string
	for _, a := range r.Actions {
		if !a.Bulk {
			continue
		}
		executor := "db"
		if hasExec {
			executor = "tx"
		}
		if procExec := g.actionProcExec(a, "id"); procExec != "" {
			dispatch = append(dispatch, fmt.Sprintf(`    case %q:
        for _, id := range ids {
%s
        }
`, a.Name, procExec))
		} else if exec := g.actionExecSQL(a); exec != "" {
			dispatch = append(dispatch, fmt.Sprintf(`    case %q:
        for _, id := range ids {
            _, err := %s.ExecContext(r.Context(), %q, id)
            if err != nil {
                httperr.Internal(w, err)
                return
            }
        }
`, a.Name, executor, exec))
		} else {
			dispatch = append(dispatch, fmt.Sprintf(`    case %q:
        for _, id := range ids {
            _ = id
        }
`, a.Name))
		}
	}

	txCode := ""
	if hasExec {
		txCode = fmt.Sprintf(`
        tx, err := db.BeginTx(r.Context(), nil)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
        defer tx.Rollback()
`)
	}
	commitCode := ""
	if hasExec {
		commitCode = `
        if err := tx.Commit(); err != nil {
            httperr.Internal(w, err)
            return
        }
`
	}
	procsImport := ""
	if hasProcs {
		procsImport = g.procImport()
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"
    httperr %q
%s)

func Bulk(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        actionName := r.PathValue("action")

        if err := r.ParseForm(); err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }

        ids := make([]int64, 0)
        for _, raw := range r.Form["ids"] {
            if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
                ids = append(ids, id)
            }
        }

%s
        switch actionName {
%s    default:
            http.Error(w, "unknown action", http.StatusNotFound)
            return
        }
%s
        http.Redirect(w, r, %q, http.StatusFound)
    }
}
`, pkgName, g.moduleImport("internal/panel/httperr"), procsImport, txCode, strings.Join(dispatch, "\n"), commitCode, listPath)

	return os.WriteFile(filepath.Join(dir, "bulk.go"), []byte(code), 0644)
}

// generateCreateHandler writes create.go: a Create(db) handler serving the
// create form on GET and inserting a new row on POST. It builds the INSERT
// statement from the create form fields, bcrypt-hashes password fields, saves
// uploaded files (file/image fields) via the saveUploadedFile helper and loads
// dynamic select options declared with options_query.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateCreateHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	listPath := fmt.Sprintf("%s/%s", g.Config.Panel.Path, pkgName)
	tName := tableName(r)

	paramFields := r.Form.Create.Fields
	create := r.Form.Create

	var optLoadCode string
	var optVars map[string]string
	queryDir := g.Config.SQLC.QueriesDir
	if queryDir == "" {
		queryDir = filepath.Join(g.OutDir, "sql", "queries")
	} else if !filepath.IsAbs(queryDir) {
		queryDir = filepath.Join(g.OutDir, queryDir)
	}
	optVars, optLoadCode = buildOptionsLoader(queryDir, paramFields)

	var colNames []string
	var valExprs []string
	var preHashLines []string
	hasPassword := false
	hasFile := false
	for _, f := range paramFields {
		colNames = append(colNames, f.Name)
		if f.Type == "password" {
			hasPassword = true
			valExprs = append(valExprs, fmt.Sprintf("string(%sBytes)", f.Name))
			preHashLines = append(preHashLines, fmt.Sprintf(`        %sBytes, _ := bcrypt.GenerateFromPassword([]byte(r.FormValue(%q)), bcrypt.DefaultCost)`, f.Name, f.Name))
		} else if f.Type == "file" || f.Type == "image" {
			hasFile = true
			valExprs = append(valExprs, fmt.Sprintf("saveUploadedFile(r, %q)", f.Name))
		} else if f.Type == "boolean" {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q) == \"true\"", f.Name))
		} else {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q)", f.Name))
		}
	}

	// buildCreateParams is the shared INSERT-value constructor used by both the
	// Create POST and the CSV import handler: it maps a field-name -> value map
	// onto the create column order, bcrypt-hashes password fields and coerces
	// booleans. File/image fields are rejected (uploads are request-bound, the
	// CSV path cannot carry them), so the create POST only uses it when the
	// resource has no such fields (legacy inline construction otherwise).
	usesBuildParams := !hasFile
	needBuildParams := !hasFile || r.ImportCSV

	var buildParamsCode string
	var formMapCode string
	if needBuildParams {
		if hasFile {
			buildParamsCode = `func buildCreateParams(m map[string]string) ([]interface{}, error) {
    return nil, fmt.Errorf("file/image uploads are not supported in CSV import")
}
`
		} else {
			var bpPre []string
			var bpVals []string
			for _, f := range paramFields {
				if f.Type == "password" {
					bpPre = append(bpPre, fmt.Sprintf(`    %sBytes, err := bcrypt.GenerateFromPassword([]byte(m[%q]), bcrypt.DefaultCost)
    if err != nil {
        return nil, err
    }`, f.Name, f.Name))
					bpVals = append(bpVals, fmt.Sprintf("string(%sBytes)", f.Name))
				} else if f.Type == "boolean" {
					bpVals = append(bpVals, fmt.Sprintf("m[%q] == \"true\"", f.Name))
				} else {
					bpVals = append(bpVals, fmt.Sprintf("m[%q]", f.Name))
				}
			}
			buildParamsCode = fmt.Sprintf(`func buildCreateParams(m map[string]string) ([]interface{}, error) {
%s
    vals := []interface{}{%s}
    return vals, nil
}
`, strings.Join(bpPre, "\n"), strings.Join(bpVals, ", "))
		}
		if usesBuildParams {
			var entries []string
			for _, f := range paramFields {
				entries = append(entries, fmt.Sprintf("%q: r.FormValue(%q)", f.Name, f.Name))
			}
			formMapCode = fmt.Sprintf(`        vals, err := buildCreateParams(map[string]string{%s})
        if err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }
`, strings.Join(entries, ", "))
		}
	}

	bcryptImport := ""
	if hasPassword {
		bcryptImport = `    "golang.org/x/crypto/bcrypt"
`
	}

	hooksImport := ""
	if g.hookBlockEmits(create.Hooks) {
		hooksImport = fmt.Sprintf("    hooks %q\n", g.moduleImport("internal/hooks"))
	}
	procsImport := ""
	if g.hookUsesProc(create.Hooks) {
		procsImport = g.procImport()
	}

	fileImport := ""
	uploadHelper := ""
	if hasFile {
		fileImport = `    "io"
    "os"
    "path/filepath"
    "time"
`
		uploadHelper = `
func saveUploadedFile(r *http.Request, fieldName string) string {
    file, header, err := r.FormFile(fieldName)
    if err != nil {
        return ""
    }
    defer file.Close()

    ext := strings.ToLower(filepath.Ext(header.Filename))
    if !safeUploadExt(ext) {
        return ""
    }

    head := make([]byte, 512)
    n, _ := io.ReadFull(file, head)
    detected := http.DetectContentType(head[:n])
    if n > 0 && (detected == "text/html" || detected == "image/svg+xml") {
        return ""
    }
    if _, err := file.Seek(0, io.SeekStart); err != nil {
        return ""
    }

    dir := "static/uploads/" + fieldName
    os.MkdirAll(dir, 0755)
    outPath := dir + "/" + fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
    out, err := os.Create(outPath)
    if err != nil {
        return ""
    }
    defer out.Close()
    io.Copy(out, file)
    return "/" + outPath
}

var safeUploadExts = map[string]bool{
    ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
    ".pdf": true, ".txt": true, ".csv": true, ".zip": true,
    ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
}

func safeUploadExt(ext string) bool {
    return safeUploadExts[ext]
}
`
	}

	preHashCode := strings.Join(preHashLines, "\n")

	formParseCode := "r.ParseForm()"
	if hasFile {
		formParseCode = "r.ParseMultipartForm(32 << 20)"
	}

	valsLine := ""
	if !usesBuildParams {
		valsLine = fmt.Sprintf("        vals := []interface{}{%s}\n", strings.Join(valExprs, ", "))
	}
	postCode := fmt.Sprintf(`        cols := []string{%s}
%s        placeholders := make([]string, len(cols))
        for i := range cols {
            placeholders[i] = fmt.Sprintf("$%%d", i+1)
        }
        query := fmt.Sprintf("INSERT INTO %%s (%%s) VALUES (%%s)", %q, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
`, colsLiteral(colNames), valsLine, tName)
	hasHooks := g.hookBlockEmits(create.Hooks)
	audit := g.auditFor(r)
	jsonImport := ""
	if audit != nil && audit.IncludeValues {
		jsonImport = "    \"encoding/json\"\n"
	}
	if hasHooks || audit != nil {
		if hasHooks {
			postCode += fmt.Sprintf(`        scope := hooks.Scope{
            Table:  %q,
            Action: "create",
            Values: map[string]interface{}{
%s        },
        }
`, tName, scopeValuesStr(colNames))
			postCode += g.hookCallsStr(create.Hooks.Before, "scope", "        ") + "\n"
		}
		if audit != nil {
			postCode += auditTxBeginStr("        ")
			postCode += fmt.Sprintf(`        var newID int64
        if err := tx.QueryRowContext(r.Context(), query+%q, vals...).Scan(&newID); err != nil {
            httperr.Internal(w, err)
            return
        }
`, g.returningClause(r))
			if hasHooks {
				postCode += "        scope.ID = newID\n"
			}
			valuesArg := `""`
			if audit.IncludeValues {
				postCode += auditValuesStr(colNames, "        ") + "\n"
				valuesArg = "string(valuesJSON)"
			}
			postCode += g.auditInsertStr(r, "create", `fmt.Sprintf("%d", newID)`, valuesArg, "        ") + "\n"
			postCode += auditTxCommitStr("        ")
		} else {
			postCode += fmt.Sprintf(`        var newID int64
        if err := db.QueryRowContext(r.Context(), query+%q, vals...).Scan(&newID); err != nil {
            httperr.Internal(w, err)
            return
        }
        scope.ID = newID
`, g.returningClause(r))
		}
		if hasHooks {
			postCode += g.hookCallsStr(create.Hooks.After, "scope", "        ") + "\n"
		}
	} else {
		execAssign := ":="
		if usesBuildParams {
			execAssign = "="
		}
		postCode += fmt.Sprintf(`        _, err %s db.ExecContext(r.Context(), query, vals...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
`, execAssign)
	}

	preInsertCode := preHashCode
	if usesBuildParams {
		preInsertCode = formMapCode
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "net/http"
    "strings"
    %s%s%s%s%s
    %q
    %q
    auth %q
    httperr %q
    layoutviews %q
)

func Create(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if r.Method == http.MethodGet {
            %s
            vd := &viewmodels.FormData{
                Item:      make(map[string]interface{}),
                Fields: []viewmodels.ColumnDef{
                    %s,
                },
                Action:    %q,
                Method:    "POST",
                Resource:  %q,
                PanelPath: %q,
                IsCreate:  true,
                CSRFToken: auth.CSRFToken(r, w),
            }
            layoutviews.Base(%q, %q, viewmodels.DefaultTheme(), auth.UserName(r), auth.CSRFToken(r, w), views.%sForm(vd)).Render(r.Context(), w)
            return
        }

        if err := %s; err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }

%s
%s
        http.Redirect(w, r, %q, http.StatusFound)
    }
}
%s`, pkgName,
		jsonImport,
		bcryptImport,
		fileImport,
		hooksImport,
		procsImport,
		g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		g.moduleImport("internal/panel/auth"), g.moduleImport("internal/panel/httperr"), g.moduleImport("internal/views/layout"),
		optLoadCode,
		formFieldDefsWithOpts(paramFields, optVars),
		fmt.Sprintf("%s/%s/new", g.Config.Panel.Path, pkgName),
		r.Name,
		g.Config.Panel.Path,
		resourceTitle(r),
		g.Config.Panel.Path,
		r.Name,
		formParseCode,
		preInsertCode,
		postCode,
		listPath,
		uploadHelper+buildParamsCode)

	return os.WriteFile(filepath.Join(dir, "create.go"), []byte(code), 0644)
}

// buildOptionsLoader generates code to load dynamic select options from DB.
// Returns: fieldName→goVarName map, and the code to load them at request time.
// Params: queryDir (directory with the .sql files used to resolve each
// options_query), fields (the form fields to inspect).
// Returns: optVars (map from field name to the generated Go variable holding
// its options) and loadCode (Go source that fills those variables at request
// time by running "SELECT value, label FROM (rawSQL) AS _opt").
func buildOptionsLoader(queryDir string, fields []types.Field) (optVars map[string]string, loadCode string) {
	optVars = make(map[string]string)
	var loads []string
	loaded := map[string]string{}
	for _, f := range fields {
		if f.OptionsQuery == "" {
			continue
		}
		if varName, ok := loaded[f.OptionsQuery]; ok {
			optVars[f.Name] = varName
			continue
		}
		varName := f.Name + "Opts"
		optVars[f.Name] = varName
		loaded[f.OptionsQuery] = varName
		rawSQL := findSQLCQuery(queryDir, f.OptionsQuery)
		if rawSQL == "" {
			rawSQL = f.OptionsQuery
		}
		optField := f.OptionsValue
		if optField == "" {
			optField = "id"
		}
		optLabel := f.OptionsLabel
		if optLabel == "" {
			optLabel = "name"
		}
		loads = append(loads, fmt.Sprintf(`        %s := map[string]string{}
        { optRows, err := db.QueryContext(r.Context(), "SELECT %s, %s FROM ("+%q+") AS _opt"); if err == nil { defer optRows.Close(); for optRows.Next() { var val, label interface{}; if err := optRows.Scan(&val, &label); err == nil { %s[fmt.Sprintf("%%v", val)] = fmt.Sprintf("%%v", label) } } } }`, varName, optField, optLabel, rawSQL, varName))
	}
	return optVars, strings.Join(loads, "\n")
}

// formFieldDefsWithOpts renders the []viewmodels.ColumnDef literal for form
// fields, wiring each field's Options to the runtime-loaded variable when the
// field has an options_query, or to the inline static options map otherwise.
// Params: fields (form field definitions), optVars (map from field name to the
// generated options variable, as returned by buildOptionsLoader).
// Returns: the comma-joined Go source string for the field defs.
func formFieldDefsWithOpts(fields []types.Field, optVars map[string]string) string {
	var defs []string
	for _, f := range fields {
		label := f.Label
		if label == "" {
			label = f.Name
		}
		opts := "nil"
		if varName, ok := optVars[f.Name]; ok {
			opts = varName
		} else if len(f.Options) > 0 {
			opts = "map[string]string{"
			for k, v := range f.Options {
				opts += fmt.Sprintf("%q: %q, ", k, v)
			}
			opts += "}"
		}
		picker := false
		if _, ok := optVars[f.Name]; ok && (f.Type == "select" || f.Type == "relation") {
			picker = true
		}
		defs = append(defs, fmt.Sprintf("{Name: %q, Label: %q, FieldType: %q, Picker: %t, Options: %s}", f.Name, label, f.Type, picker, opts))
	}
	return strings.Join(defs, ",\n")
}

// colsLiteral renders a list of double-quoted Go string literals for the given
// column names, used to build the cols []string literals in the generated
// create/update handlers.
// Params: cols (the column names).
// Returns: a comma-separated list of quoted Go literals.
func colsLiteral(cols []string) string {
	var q []string
	for _, c := range cols {
		q = append(q, fmt.Sprintf("%q", c))
	}
	return strings.Join(q, ", ")
}

// generateUpdateHandler writes update.go: an Update(db) handler that renders
// the populated edit form on GET (via the SQLC populate query) and performs a
// raw SQL UPDATE on POST. It builds the SET clauses from the update form
// fields, saves uploaded files, appends the record id as the last placeholder
// and loads dynamic select options. Returns an error on write failure.
// Params: dir (resource package directory), r (the resource definition).
func (g *Generator) generateUpdateHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	listPath := fmt.Sprintf("%s/%s", g.Config.Panel.Path, pkgName)
	tName := tableName(r)
	populateQuery := r.Form.Update.PopulateQuery
	if populateQuery == "" {
		populateQuery = "GetByID"
	}

	paramFields := r.Form.Update.Fields
	update := r.Form.Update

	var optLoadCode string
	var optVars map[string]string
	queryDir := g.Config.SQLC.QueriesDir
	if queryDir == "" {
		queryDir = filepath.Join(g.OutDir, "sql", "queries")
	} else if !filepath.IsAbs(queryDir) {
		queryDir = filepath.Join(g.OutDir, queryDir)
	}
	optVars, optLoadCode = buildOptionsLoader(queryDir, paramFields)

	var colNames []string
	var valExprs []string
	hasFile := false
	for _, f := range paramFields {
		colNames = append(colNames, f.Name)
		if f.Type == "file" || f.Type == "image" {
			hasFile = true
			valExprs = append(valExprs, fmt.Sprintf("saveUploadedFile(r, %q)", f.Name))
		} else if f.Type == "boolean" {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q) == \"true\"", f.Name))
		} else {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q)", f.Name))
		}
	}

	hooksImport := ""
	if g.hookBlockEmits(update.Hooks) {
		hooksImport = fmt.Sprintf("    hooks %q\n", g.moduleImport("internal/hooks"))
	}
	procsImport := ""
	if g.hookUsesProc(update.Hooks) {
		procsImport = g.procImport()
	}

	fileImport := ""
	uploadHelper := ""
	if hasFile {
		fileImport = `    "io"
    "os"
    "path/filepath"
    "time"
`
		uploadHelper = `
func saveUploadedFile(r *http.Request, fieldName string) string {
    file, header, err := r.FormFile(fieldName)
    if err != nil {
        return ""
    }
    defer file.Close()

    ext := strings.ToLower(filepath.Ext(header.Filename))
    if !safeUploadExt(ext) {
        return ""
    }

    head := make([]byte, 512)
    n, _ := io.ReadFull(file, head)
    detected := http.DetectContentType(head[:n])
    if n > 0 && (detected == "text/html" || detected == "image/svg+xml") {
        return ""
    }
    if _, err := file.Seek(0, io.SeekStart); err != nil {
        return ""
    }

    dir := "static/uploads/" + fieldName
    os.MkdirAll(dir, 0755)
    outPath := dir + "/" + fmt.Sprintf("%d%s", time.Now().UnixNano(), ext)
    out, err := os.Create(outPath)
    if err != nil {
        return ""
    }
    defer out.Close()
    io.Copy(out, file)
    return "/" + outPath
}

var safeUploadExts = map[string]bool{
    ".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
    ".pdf": true, ".txt": true, ".csv": true, ".zip": true,
    ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
}

func safeUploadExt(ext string) bool {
    return safeUploadExts[ext]
}
`
	}

	formParseCode := "r.ParseForm()"
	if hasFile {
		formParseCode = "r.ParseMultipartForm(32 << 20)"
	}

	var populateFields []string
	populateFields = append(populateFields, `"id": item.ID,`)
	for _, f := range paramFields {
		goName := snakeToPascal(f.Name)
		populateFields = append(populateFields, fmt.Sprintf("            %q: item.%s,", f.Name, goName))
	}

	postCode := fmt.Sprintf(`        cols := []string{%s}
        vals := []interface{}{%s}
        setClauses := make([]string, len(cols))
        for i, col := range cols {
            setClauses[i] = fmt.Sprintf("%%s = $%%d", col, i+1)
        }
        vals = append(vals, int64(id))
        query := fmt.Sprintf("UPDATE %%s SET %%s WHERE %%s = $%%d", %q, strings.Join(setClauses, ", "), %q, len(cols)+1)
`, colsLiteral(colNames), strings.Join(valExprs, ", "), tName, idColumn(r))
	hasHooks := g.hookBlockEmits(update.Hooks)
	audit := g.auditFor(r)
	jsonImport := ""
	if audit != nil && audit.IncludeValues {
		jsonImport = "    \"encoding/json\"\n"
	}
	if hasHooks {
		postCode += fmt.Sprintf(`        scope := hooks.Scope{
            Table:  %q,
            Action: "update",
            ID:     int64(id),
            Values: map[string]interface{}{
%s        },
        }
`, tName, scopeValuesStr(colNames))
		postCode += g.hookCallsStr(update.Hooks.Before, "scope", "        ") + "\n"
	}
	if audit != nil {
		postCode += auditTxBeginStr("        ")
		postCode += `        _, err = tx.ExecContext(r.Context(), query, vals...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
`
		valuesArg := `""`
		if audit.IncludeValues {
			postCode += auditValuesStr(colNames, "        ") + "\n"
			valuesArg = "string(valuesJSON)"
		}
		postCode += g.auditInsertStr(r, "update", "strconv.FormatInt(int64(id), 10)", valuesArg, "        ") + "\n"
		postCode += auditTxCommitStr("        ")
	} else {
		postCode += `        _, err = db.ExecContext(r.Context(), query, vals...)
        if err != nil {
            httperr.Internal(w, err)
            return
        }
`
	}
	if hasHooks {
		postCode += g.hookCallsStr(update.Hooks.After, "scope", "        ") + "\n"
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "net/http"
    "strconv"
    "strings"
%s%s%s%s
    %q
    %q
    %q
    auth %q
    httperr %q
    layoutviews %q
)

func Update(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

        if r.Method == http.MethodGet {
            item, err := data.New(db).%s(r.Context(), %s(id))
            if err != nil {
                httperr.NotFound(w, err)
                return
            }

            itemMap := map[string]interface{}{
%s            }

            %s
            vd := &viewmodels.FormData{
                Item: itemMap,
                Fields: []viewmodels.ColumnDef{
                    %s,
                },
                Action:    %s,
                Method:    "POST",
                Resource:  %q,
                PanelPath: %q,
                IsCreate:  false,
                CSRFToken: auth.CSRFToken(r, w),
            }
            layoutviews.Base(%q, %q, viewmodels.DefaultTheme(), auth.UserName(r), auth.CSRFToken(r, w), views.%sForm(vd)).Render(r.Context(), w)
            return
        }

        if err := %s; err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }

%s
        http.Redirect(w, r, %q, http.StatusFound)
    }
}
%s`, pkgName,
		jsonImport,
		fileImport,
		hooksImport,
		procsImport,
		g.moduleImport("internal/data"), g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		g.moduleImport("internal/panel/auth"), g.moduleImport("internal/panel/httperr"), g.moduleImport("internal/views/layout"),
		populateQuery,
		g.idGoTypeForResource(r),
		strings.Join(populateFields, "\n"),
		optLoadCode,
		formFieldDefsWithOpts(paramFields, optVars),
		fmt.Sprintf("fmt.Sprintf(\"%%s/%%s/%%d\", %q, %q, id)", g.Config.Panel.Path, pkgName),
		r.Name,
		g.Config.Panel.Path,
		resourceTitle(r),
		g.Config.Panel.Path,
		r.Name,
		formParseCode,
		postCode,
		listPath,
		uploadHelper)

	return os.WriteFile(filepath.Join(dir, "update.go"), []byte(code), 0644)
}
