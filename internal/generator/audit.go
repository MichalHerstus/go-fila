// audit.go
//
// Implements the generator-implicit audit log (SPEC_future_enhancement.md,
// Phase D2). When the config has `audit: {enabled: true}`, the generator
// augments the config with a list-only AuditLog resource and an "Audit Log"
// navigation group, emits the audit_log table DDL into sql/migrations, and
// weaves an INSERT INTO audit_log into every create/update/delete/action
// handler. The operation and its audit insert run inside one transaction so a
// failed audit write rolls the operation back.
package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MichalHerstus/yaga/internal/types"
)

// auditResourceName is the name of the augmented list-only audit resource.
const auditResourceName = "AuditLog"

// auditEnabled reports whether the config enables the audit log.
// Returns: true when the audit block exists and is enabled.
func (g *Generator) auditEnabled() bool {
	return g.Config.Audit != nil && g.Config.Audit.Enabled
}

// auditTable returns the audit_log table name, honoring the audit.table
// override and defaulting to "audit_log".
// Returns: the table name.
func (g *Generator) auditTable() string {
	if g.Config.Audit != nil && g.Config.Audit.Table != "" {
		return g.Config.Audit.Table
	}
	return "audit_log"
}

// auditFor returns the audit config when auditing applies to the given
// resource (enabled and not excluded). The generated AuditLog resource itself
// is never audited (it is list-only anyway). Returns nil otherwise.
// Params: r (the resource definition).
// Returns: the audit config, or nil when auditing is off or excluded.
func (g *Generator) auditFor(r types.Resource) *types.AuditConfig {
	if !g.auditEnabled() || r.Name == auditResourceName {
		return nil
	}
	for _, ex := range g.Config.Audit.ExcludeResources {
		if ex == r.Name {
			return nil
		}
	}
	return g.Config.Audit
}

// auditAnyResource reports whether any configured resource is audited
// (enabled and not excluded). Gates the emitted UserID middleware helper so a
// config that enables audit but excludes every resource still produces
// byte-identical auth output.
// Returns: true when at least one resource carries an audit insert.
func (g *Generator) auditAnyResource() bool {
	if !g.auditEnabled() {
		return false
	}
	for _, r := range g.Config.Resources {
		if g.auditFor(r) != nil {
			return true
		}
	}
	return false
}

// applyAudit augments the config with the list-only AuditLog resource and the
// "Audit Log" navigation group when audit is enabled. Called during Generate
// after plugins so the augmented resource flows through the whole pipeline
// (router, handlers, views, dirs). No-op when audit is disabled or a resource
// named AuditLog already exists.
func (g *Generator) applyAudit() {
	if !g.auditEnabled() {
		return
	}
	for _, r := range g.Config.Resources {
		if r.Name == auditResourceName {
			return
		}
	}
	cols := []types.Column{
		{Name: "id", Label: "ID"},
		{Name: "user_name", Label: "User"},
		{Name: "table_name", Label: "Table"},
		{Name: "action", Label: "Action"},
		{Name: "row_id", Label: "Row"},
	}
	if g.Config.Audit.IncludeValues {
		cols = append(cols, types.Column{Name: "values_json", Label: "Values", Type: "json"})
	}
	cols = append(cols, types.Column{Name: "created_at", Label: "Created", Type: "datetime"})
	res := types.Resource{
		Name:  auditResourceName,
		Label: "Audit Log",
		Table: g.auditTable(),
		List: &types.ListConfig{
			Columns:     cols,
			PerPage:     20,
			DefaultSort: "-created_at",
		},
	}
	if g.Config.Audit.Policy != "" {
		res.Policies = &types.Policy{ViewAny: g.Config.Audit.Policy}
	}
	g.Config.Resources = append(g.Config.Resources, res)
	g.Config.Navigation = append(g.Config.Navigation, types.NavigationGroup{
		Group: "Audit Log",
		Items: []types.NavigationItem{{Resource: auditResourceName}},
	})
}

// generateAuditSchema writes the audit_log table DDL into the out dir's
// sql/migrations directory so the user can migrate it. The DDL is driver-aware
// (postgres/mssql share the postgres dialect, sqlite gets its own) and is
// never executed against the DB — it is input for the user's migrations. When
// a migration file in the out dir already declares the audit table (the
// user's own DDL, or e.g. a schema captured from a live DB), nothing is
// emitted — no sqlc query file is produced (D11), the audit list/count
// queries run as raw SQL inside the generated list handler.
// Returns: an error on write failure.
func (g *Generator) generateAuditSchema() error {
	if !g.auditEnabled() {
		return nil
	}
	if g.auditTableInMigrations() {
		return nil
	}
	dir := filepath.Join(g.OutDir, "sql", "migrations")
	table := g.auditTable()
	path := filepath.Join(dir, strings.ToLower(table)+".sql")
	var ddl string
	if g.isSQLite() {
		ddl = fmt.Sprintf(`CREATE TABLE %s (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT,
    user_name TEXT,
    table_name TEXT NOT NULL,
    action TEXT NOT NULL,
    row_id TEXT,
    values_json TEXT,
    created_at DATETIME DEFAULT (datetime('now'))
);
`, g.quoteIdent(table))
	} else {
		ddl = fmt.Sprintf(`CREATE TABLE %s (
    id BIGSERIAL PRIMARY KEY,
    user_id TEXT,
    user_name TEXT,
    table_name TEXT NOT NULL,
    action TEXT NOT NULL,
    row_id TEXT,
    values_json JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
`, g.quoteIdent(table))
	}
	return os.WriteFile(path, []byte(ddl), 0644)
}

// auditTableInMigrations reports whether any .sql file in the out dir's
// sql/migrations directory already declares the audit table via CREATE TABLE
// (the user's own DDL, or e.g. the demo schema). Matching is case-insensitive
// on the table name and handles the optional "IF NOT EXISTS" and quoted names.
// Returns: true when the table is already declared somewhere.
func (g *Generator) auditTableInMigrations() bool {
	dir := filepath.Join(g.OutDir, "sql", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	target := strings.ToLower(g.auditTable())
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if containsCreateTable(string(b), target) {
			return true
		}
	}
	return false
}

// containsCreateTable reports whether sql contains a CREATE TABLE statement
// for the given lower-cased table name. Handles the optional "IF NOT EXISTS"
// and double-quoted identifiers; matches on the identifier word following
// CREATE TABLE.
// Params: sql (the migration text), table (lower-cased table name).
// Returns: true when a matching CREATE TABLE is found.
func containsCreateTable(sql, table string) bool {
	s := strings.ToLower(sql)
	idx := 0
	for {
		i := strings.Index(s[idx:], "create table")
		if i < 0 {
			return false
		}
		rest := strings.TrimLeft(s[idx+i+len("create table"):], " \t\r\n")
		rest = strings.TrimPrefix(rest, "if not exists ")
		rest = strings.TrimLeft(rest, " \t\r\n")
		if strings.HasPrefix(rest, `"`) {
			rest = rest[1:]
			end := strings.Index(rest, `"`)
			if end >= 0 && rest[:end] == table {
				return true
			}
		} else {
			end := strings.IndexAny(rest, " \t\r\n(")
			if end < 0 {
				end = len(rest)
			}
			if rest[:end] == table {
				return true
			}
		}
		idx += i + len("create table")
	}
}

// auditTxBeginStr renders the BeginTx/defer Rollback prologue that wraps an
// audited operation and its audit insert in one transaction.
// Params: indent (Go source indentation for the handler body).
// Returns: the Go source prologue.
func auditTxBeginStr(indent string) string {
	return fmt.Sprintf(`%stx, err := db.BeginTx(r.Context(), nil)
%sif err != nil {
%s    httperr.Internal(w, err)
%s    return
%s}
%sdefer tx.Rollback()
`, indent, indent, indent, indent, indent, indent)
}

// auditTxCommitStr renders the tx.Commit epilogue (the audit insert runs
// before it; any after-hooks run after it, against db).
// Params: indent (Go source indentation for the handler body).
// Returns: the Go source epilogue.
func auditTxCommitStr(indent string) string {
	return fmt.Sprintf(`%sif err := tx.Commit(); err != nil {
%s    httperr.Internal(w, err)
%s    return
%s}
`, indent, indent, indent, indent)
}

// auditInsertStr renders the Go source that appends one audit_log row inside
// the audit transaction, using the tx executor. rowID is a string expression
// for the affected record key; valuesArg is the values_json SQL argument (a
// string expression, or "" for NULL).
// Params: r (the resource being audited), action (the operation name), rowID
// (string expression for the record key), valuesArg (string expression or ""),
// indent (Go source indentation).
// Returns: the Go source for the audit INSERT.
func (g *Generator) auditInsertStr(r types.Resource, action, rowID, valuesArg, indent string) string {
	stmt := fmt.Sprintf("INSERT INTO %s (user_id, user_name, table_name, action, row_id, values_json) VALUES ($1, $2, $3, $4, $5, $6)", g.quoteIdent(g.auditTable()))
	return fmt.Sprintf(`%sif _, err := tx.ExecContext(r.Context(), %q, auth.UserID(r), auth.UserName(r), %q, %q, %s, %s); err != nil {
%s    httperr.Internal(w, err)
%s    return
%s}`, indent, stmt, tableName(r), action, rowID, valuesArg, indent, indent, indent)
}

// auditValuesStr renders a `var valuesJSON []byte` declaration and a
// json.Marshal of the vals slice as a map, used as the audit values_json
// argument. colNames must be the form columns in vals order.
// Params: colNames (form column names in vals order), indent (Go source
// indentation for the handler body).
// Returns: the Go source declaration.
func auditValuesStr(colNames []string, indent string) string {
	return fmt.Sprintf(`%svar valuesJSON []byte
%svaluesJSON, _ = json.Marshal(map[string]interface{}{
%s        },
%s)`, indent, indent, scopeValuesStr(colNames), indent)
}
