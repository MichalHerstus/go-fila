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
	return nil
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

// tableName derives the default SQL table name for a resource: the lowercase
// resource name plus a plural "s" (e.g. "User" -> "users").
// Params: resourceName (PascalCase resource name from the YAML config).
// Returns: the plural lowercase table name.
func tableName(resourceName string) string {
	return strings.ToLower(resourceName) + "s"
}

// generateListHandler writes list.go for a resource: a List(db) handler that
// reads page/search/sort/order query parameters, builds a dynamic WHERE/ORDER
// BY/LIMIT query against the plural table name, counts the total rows for
// pagination, scans the listed columns and renders the resource list view.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateListHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	tName := tableName(r.Name)

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

	colsJoin := strings.Join(colNames, ", ")

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
)

func List(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        page, _ := strconv.Atoi(r.URL.Query().Get("page"))
        if page < 1 {
            page = 1
        }
        perPage := 20
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

        validSorts := map[string]bool{`, pkgName,
		g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName)))

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
	searchableColsLiteral := ""
	for i, sc := range searchCols {
		if i > 0 {
			searchableColsLiteral += ", "
		}
		searchableColsLiteral += fmt.Sprintf("%q", sc)
	}

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
            orderSQL = fmt.Sprintf(" ORDER BY %%s %%s", sort, order)
        }

        countQuery := "SELECT COUNT(*) FROM %s" + whereSQL
        var total int64
        if err := db.QueryRowContext(r.Context(), countQuery, args...).Scan(&total); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        dataQuery := "SELECT %s FROM %s" + whereSQL + orderSQL + " LIMIT ? OFFSET ?"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        fullArgs = append(fullArgs, perPage, offset)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer rows.Close()

`, searchableColsLiteral, tName, colsJoin, tName)
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
            orderSQL = fmt.Sprintf(" ORDER BY %%s %%s", sort, order)
        }

        countQuery := "SELECT COUNT(*) FROM %s" + whereSQL
        var total int64
        if err := db.QueryRowContext(r.Context(), countQuery, args[2:]...).Scan(&total); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        dataQuery := "SELECT %s FROM %s" + whereSQL + orderSQL + " LIMIT $1 OFFSET $2"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer rows.Close()

`, searchableColsLiteral, tName, colsJoin, tName)
	}
	sb.WriteString(listCore)

	sb.WriteString(`        var items []map[string]interface{}
        for rows.Next() {
            ` + scanFields(colNames) + `
            items = append(items, item)
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
        }

        ` + fmt.Sprintf("views.%sList(vd).Render(r.Context(), w)", r.Name) + `
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
// appends their addresses to a scan slice and populates the item map.
// Params: cols (the column names to scan).
// Returns: the multi-line Go source string to inline in the generated handler.
func scanFields(cols []string) string {
	var scans []string
	scans = append(scans, `        item := make(map[string]interface{})`)
	scans = append(scans, `        var scanArgs []interface{}`)
	for _, c := range cols {
		scans = append(scans, fmt.Sprintf(`        var val_%s interface{}`, c))
		scans = append(scans, fmt.Sprintf(`        scanArgs = append(scanArgs, &val_%s)`, c))
	}
	scans = append(scans, `        if err := rows.Scan(scanArgs...); err != nil {`)
	scans = append(scans, `            http.Error(w, err.Error(), http.StatusInternalServerError)`)
	scans = append(scans, `            return`)
	scans = append(scans, `        }`)
	for _, c := range cols {
		scans = append(scans, fmt.Sprintf(`        item[%q] = val_%s`, c, c))
	}
	return strings.Join(scans, "\n")
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
            http.Error(w, err.Error(), http.StatusNotFound)
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
        }

        views.%sDetail(vd).Render(r.Context(), w)
    }
}
`, pkgName,
		g.moduleImport("internal/data"), g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		queryName,
		g.idGoType(),
		detailFieldMap(r.Detail.Fields),
		fieldDefsFromDetail(r.Detail.Fields),
		r.Name,
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

// snakeToPascal converts a snake_case string to PascalCase so it matches the
// field names that sqlc generates. The "id" segment is special-cased to "ID"
// (e.g. "user_role_id" -> "UserRoleID").
// Params: s (the snake_case input, e.g. a column name).
// Returns: the PascalCase variant.
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
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
	tName := tableName(r.Name)

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"
)

func Delete(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

        _, err = db.ExecContext(r.Context(), "DELETE FROM %s WHERE id = $1", int64(id))
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        http.Redirect(w, r, %q, http.StatusFound)
    }
}
`, pkgName, tName, listPath)

	return os.WriteFile(filepath.Join(dir, "delete.go"), []byte(code), 0644)
}

// generateCSVHandler writes export.go: an ExportCSV(db) handler that selects
// all list columns ordered by the first column and streams them as an
// attachment CSV file using encoding/csv.
// Params: dir (resource package directory), r (the resource definition).
// Returns: an error on write failure.
func (g *Generator) generateCSVHandler(dir string, r types.Resource) error {
	pkgName := strings.ToLower(r.Name)
	tName := tableName(r.Name)

	var colNames []string
	for _, c := range r.List.Columns {
		colNames = append(colNames, c.Name)
	}
	colsJoin := strings.Join(colNames, ", ")

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "encoding/csv"
    "net/http"
)

func ExportCSV(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        query := "SELECT %s FROM %s ORDER BY 1"
        rows, err := db.QueryContext(r.Context(), query)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer rows.Close()

        w.Header().Set("Content-Type", "text/csv")
        w.Header().Set("Content-Disposition", "attachment; filename=%s_export.csv")
        wr := csv.NewWriter(w)
        defer wr.Flush()

        cols, _ := rows.Columns()
        wr.Write(cols)

        vals := make([]string, len(cols))
        ptrs := make([]interface{}, len(cols))
        for i := range vals {
            ptrs[i] = &vals[i]
        }
        for rows.Next() {
            rows.Scan(ptrs...)
            wr.Write(vals)
        }
    }
}
`, pkgName, colsJoin, tName, tName)

	return os.WriteFile(filepath.Join(dir, "export.go"), []byte(code), 0644)
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

	var dispatch []string
	for _, a := range r.Actions {
		dispatch = append(dispatch, fmt.Sprintf(`    case %q:
        _, err := db.ExecContext(r.Context(), %q, int64(id))
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }`, a.Name, a.Query))
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "net/http"
    "strconv"
)

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
`, pkgName, strings.Join(dispatch, "\n"), listPath)

	return os.WriteFile(filepath.Join(dir, "actions.go"), []byte(code), 0644)
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
	tName := tableName(r.Name)

	paramFields := r.Form.Create.Fields

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
		} else {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q)", f.Name))
		}
	}

	bcryptImport := ""
	if hasPassword {
		bcryptImport = `    "golang.org/x/crypto/bcrypt"
`
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

    ext := filepath.Ext(header.Filename)
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
`
	}

	preHashCode := strings.Join(preHashLines, "\n")

	formParseCode := "r.ParseForm()"
	if hasFile {
		formParseCode = "r.ParseMultipartForm(32 << 20)"
	}

	placeholders := make([]string, len(colNames))
	for i := range colNames {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "net/http"
    "strings"
%s%s
    %q
    %q
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
            }
            views.%sForm(vd).Render(r.Context(), w)
            return
        }

        if err := %s; err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }

%s
        cols := []string{%s}
        vals := []interface{}{%s}
        placeholders := make([]string, len(cols))
        for i := range cols {
            placeholders[i] = fmt.Sprintf("$%%d", i+1)
        }
        query := fmt.Sprintf("INSERT INTO %%s (%%s) VALUES (%%s)", %q, strings.Join(cols, ", "), strings.Join(placeholders, ", "))
        _, err := db.ExecContext(r.Context(), query, vals...)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        http.Redirect(w, r, %q, http.StatusFound)
    }
}
%s`, pkgName,
		bcryptImport,
		fileImport,
		g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		optLoadCode,
		formFieldDefsWithOpts(paramFields, optVars),
		listPath,
		r.Name,
		g.Config.Panel.Path,
		r.Name,
		formParseCode,
		preHashCode,
		colsLiteral(colNames),
		strings.Join(valExprs, ", "),
		tName,
		listPath,
		uploadHelper)

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
	for _, f := range fields {
		if f.OptionsQuery == "" {
			continue
		}
		varName := f.Name + "Opts"
		optVars[f.Name] = varName
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
		defs = append(defs, fmt.Sprintf("{Name: %q, Label: %q, FieldType: %q, Options: %s}", f.Name, label, f.Type, opts))
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
	tName := tableName(r.Name)
	populateQuery := r.Form.Update.PopulateQuery
	if populateQuery == "" {
		populateQuery = "GetByID"
	}

	paramFields := r.Form.Update.Fields

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
		} else {
			valExprs = append(valExprs, fmt.Sprintf("r.FormValue(%q)", f.Name))
		}
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

    ext := filepath.Ext(header.Filename)
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

	code := fmt.Sprintf(`package %s

import (
    "database/sql"
    "fmt"
    "net/http"
    "strconv"
    "strings"
%s
    %q
    %q
    %q
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
                http.Error(w, err.Error(), http.StatusNotFound)
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
                Action:    %q,
                Method:    "POST",
                Resource:  %q,
                PanelPath: %q,
                IsCreate:  false,
            }
            views.%sForm(vd).Render(r.Context(), w)
            return
        }

        if err := %s; err != nil {
            http.Error(w, "invalid form", http.StatusBadRequest)
            return
        }

        cols := []string{%s}
        vals := []interface{}{%s}
        setClauses := make([]string, len(cols))
        for i, col := range cols {
            setClauses[i] = fmt.Sprintf("%%s = $%%d", col, i+1)
        }
        vals = append(vals, int64(id))
        query := fmt.Sprintf("UPDATE %%s SET %%s WHERE id = $%%d", %q, strings.Join(setClauses, ", "), len(cols)+1)
        _, err = db.ExecContext(r.Context(), query, vals...)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        http.Redirect(w, r, %q, http.StatusFound)
    }
}
%s`, pkgName,
		fileImport,
		g.moduleImport("internal/data"), g.moduleImport("internal/viewmodels"), g.moduleImport("internal/views/resources/"+pkgName),
		populateQuery,
		g.idGoType(),
		strings.Join(populateFields, "\n"),
		optLoadCode,
		formFieldDefsWithOpts(paramFields, optVars),
		fmt.Sprintf("%s/%s/%%d", g.Config.Panel.Path, pkgName),
		r.Name,
		g.Config.Panel.Path,
		r.Name,
		formParseCode,
		colsLiteral(colNames),
		strings.Join(valExprs, ", "),
		tName,
		listPath,
		uploadHelper)

	return os.WriteFile(filepath.Join(dir, "update.go"), []byte(code), 0644)
}
