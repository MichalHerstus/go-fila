package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

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

func fieldDefsFromDetail(fields []types.Field) string {
	return fieldDefsStr(fields)
}

func tableName(resourceName string) string {
	return strings.ToLower(resourceName) + "s"
}

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

    "internal/viewmodels"
    "internal/views/resources/%s"
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

        validSorts := map[string]bool{`, pkgName, pkgName))

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

        var args []interface{}
        args = append(args, perPage, offset)
        argIdx := 3

        var whereClauses []string
        if search != "" {
            searchableCols := []string{`)
	for i, sc := range searchCols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(fmt.Sprintf("%q", sc))
	}
	sb.WriteString(`}
            for _, col := range searchableCols {
                whereClauses = append(whereClauses, fmt.Sprintf("%s ILIKE $%d", col, argIdx))
                args = append(args, "%"+search+"%")
                argIdx++
            }
        }

        whereSQL := ""
        if len(whereClauses) > 0 {
            whereSQL = " WHERE " + strings.Join(whereClauses, " OR ")
        }

        orderSQL := ""
        if sort != "" {
            orderSQL = fmt.Sprintf(" ORDER BY %s %s", sort, order)
        }

        countQuery := "SELECT COUNT(*) FROM ` + tName + `" + whereSQL
        var total int64
        if err := db.QueryRowContext(r.Context(), countQuery, args[2:]...).Scan(&total); err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }

        dataQuery := "SELECT ` + colsJoin + ` FROM ` + tName + `" + whereSQL + orderSQL + " LIMIT $1 OFFSET $2"
        var fullArgs []interface{}
        fullArgs = append(fullArgs, args...)
        rows, err := db.QueryContext(r.Context(), dataQuery, fullArgs...)
        if err != nil {
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer rows.Close()

        var items []map[string]interface{}
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

func validSortsMapStr(cols []string) string {
	var parts []string
	for _, c := range cols {
		parts = append(parts, fmt.Sprintf("%q: true", c))
	}
	return strings.Join(parts, ", ")
}

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

    "internal/data"
    "internal/viewmodels"
    "internal/views/resources/%s"
)

func Detail(db *sql.DB) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        idStr := r.PathValue("id")
        id, err := strconv.Atoi(idStr)
        if err != nil {
            http.Error(w, "invalid id", http.StatusBadRequest)
            return
        }

        item, err := data.%s(db, int64(id))
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
`, pkgName, pkgName,
		queryName,
		detailFieldMap(r.Detail.Fields),
		fieldDefsFromDetail(r.Detail.Fields),
		r.Name,
		g.Config.Panel.Path,
		r.Name)

	return os.WriteFile(filepath.Join(dir, "detail.go"), []byte(code), 0644)
}

func detailFieldMap(fields []types.Field) string {
	var entries []string
	for _, f := range fields {
		goName := snakeToPascal(f.Name)
		entries = append(entries, fmt.Sprintf("            %q: item.%s,", f.Name, goName))
	}
	return strings.Join(entries, "\n")
}

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
    "fmt"
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
    "internal/viewmodels"
    "internal/views/resources/%s"
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
		pkgName,
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
        { optRows, err := db.QueryContext(r.Context(), "SELECT %s, %s FROM ("+%q+") AS _opt"); if err == nil { defer optRows.Close(); for optRows.Next() { var val, label string; if err := optRows.Scan(&val, &label); err == nil { %s[val] = label } } } }`, varName, optField, optLabel, rawSQL, varName))
	}
	return optVars, strings.Join(loads, "\n")
}

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

func colsLiteral(cols []string) string {
	var q []string
	for _, c := range cols {
		q = append(q, fmt.Sprintf("%q", c))
	}
	return strings.Join(q, ", ")
}

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
    "internal/data"
    "internal/viewmodels"
    "internal/views/resources/%s"
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
            item, err := data.%s(db, int64(id))
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
		pkgName,
		populateQuery,
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
