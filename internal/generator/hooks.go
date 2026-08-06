// hooks.go
//
// Generates the shared internal/hooks package and the Go source snippets that
// invoke lifecycle hooks from the resource handlers. Each declared fn hook
// gets a compile-ready stub in internal/hooks/hooks.go that the user fills in;
// sql hooks are inlined by the handler generators as db.ExecContext calls.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// generateHooks writes internal/hooks/hooks.go into the output directory when
// at least one fn hook is declared anywhere in the config. The file defines
// the Scope struct shared by all hook calls plus one stub per unique fn hook
// name. Nothing is written when only sql hooks (or no hooks) are declared.
// Returns: an error on write failure.
func (g *Generator) generateHooks() error {
	fns := g.collectFnHooks()
	if len(fns) == 0 {
		return nil
	}

	var stubs strings.Builder
	for _, fn := range fns {
		stubs.WriteString(fmt.Sprintf("\n// %s - one stub per declared fn hook; user implements.\n", fn))
		stubs.WriteString(fmt.Sprintf("func %s(ctx context.Context, db *sql.DB, s Scope) error { return nil }\n", fn))
	}

	code := fmt.Sprintf(`package hooks

import (
    "context"
    "database/sql"
)

type Scope struct {
    ID     int64
    Table  string
    Action string // create|update|delete|<actionName>
    Values map[string]interface{}
}
%s`, stubs.String())

	dir := filepath.Join(g.OutDir, "internal/hooks")
	return os.WriteFile(filepath.Join(dir, "hooks.go"), []byte(code), 0644)
}

// collectFnHooks walks every resource in config order, gathering the unique,
// non-empty fn hook names from the form actions (create/update/delete) and the
// custom actions. Order is preserved and duplicates are dropped.
// Returns: the deduplicated list of fn hook names.
func (g *Generator) collectFnHooks() []string {
	seen := map[string]bool{}
	var fns []string
	for _, r := range g.Config.Resources {
		if r.Form != nil {
			for _, fa := range []*types.FormAction{r.Form.Create, r.Form.Update, r.Form.Delete} {
				if fa == nil || fa.Hooks == nil {
					continue
				}
				for _, h := range fa.Hooks.Before {
					if h.Fn != "" && !seen[h.Fn] {
						seen[h.Fn] = true
						fns = append(fns, h.Fn)
					}
				}
				for _, h := range fa.Hooks.After {
					if h.Fn != "" && !seen[h.Fn] {
						seen[h.Fn] = true
						fns = append(fns, h.Fn)
					}
				}
			}
		}
		for _, a := range r.Actions {
			if a.Hooks == nil {
				continue
			}
			for _, h := range a.Hooks.Before {
				if h.Fn != "" && !seen[h.Fn] {
					seen[h.Fn] = true
					fns = append(fns, h.Fn)
				}
			}
			for _, h := range a.Hooks.After {
				if h.Fn != "" && !seen[h.Fn] {
					seen[h.Fn] = true
					fns = append(fns, h.Fn)
				}
			}
		}
	}
	return fns
}

// hookCallsStr renders the Go source that runs a before/after hook list against
// a Scope variable. fn hooks call the generated stub in the hooks package; sql
// hooks are inlined as db.ExecContext binding the current scope id ($1). A hook
// error aborts the request with a 500 response.
// Params: hooks (the before or after list), scopeVar (name of the Scope var),
// indent (the leading whitespace for each emitted line).
// Returns: the Go source lines (empty when the list is empty).
func hookCallsStr(hooks []types.Hook, scopeVar, indent string) string {
	var lines []string
	for _, h := range hooks {
		if h.Fn != "" {
			lines = append(lines, fmt.Sprintf(`%sif err := hooks.%s(r.Context(), db, %s); err != nil {
%s    http.Error(w, err.Error(), http.StatusInternalServerError)
%s    return
%s}`, indent, h.Fn, scopeVar, indent, indent, indent))
		} else if h.SQL != "" {
			lines = append(lines, fmt.Sprintf(`%sif _, err := db.ExecContext(r.Context(), %q, %s.ID); err != nil {
%s    http.Error(w, err.Error(), http.StatusInternalServerError)
%s    return
%s}`, indent, h.SQL, scopeVar, indent, indent, indent))
		}
	}
	return strings.Join(lines, "\n")
}

// scopeValuesStr renders the Values map literal for a create/update Scope,
// referencing the handler's vals slice by index so password/file expressions
// are not duplicated. The id column is skipped (it is not part of the form
// params).
// Params: cols (form column names in vals order).
// Returns: the "key: vals[n]," entries (empty for an empty cols list).
func scopeValuesStr(cols []string) string {
	var entries []string
	for i, c := range cols {
		entries = append(entries, fmt.Sprintf("            %q: vals[%d],", c, i))
	}
	return strings.Join(entries, "\n")
}

// returningClause returns the SQL fragment appended to the create query to
// capture the inserted row key: " RETURNING <id>" for postgres/sqlite and the
// T-SQL " OUTPUT INSERTED.<id>" for mssql (which has no RETURNING clause).
// Params: r (the resource definition; used for its id column override).
// Returns: the SQL fragment including its leading space.
func (g *Generator) returningClause(r types.Resource) string {
	col := idColumn(r)
	if g.isMSSQL() {
		return " OUTPUT INSERTED." + col
	}
	return " RETURNING " + col
}
