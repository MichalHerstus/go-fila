// audit.go
//
// Example go-fila plugin: an audit trail. At generation time it contributes an
// AuditLog resource, an AuditOverview page with two stat widgets, an "Audit"
// navigation group, the audit_log schema + queries SQL files, and attaches an
// after-delete SQL hook to the existing "Customer" resource so every customer
// deletion is recorded.
//
// The plugin is driver-aware: go-fila injects the database driver into the
// config map under the reserved "driver" key, and the emitted SQL uses the
// matching dialect (sqlc placeholders and DDL differ per driver).
package audit

import (
	"fmt"

	plugin "github.com/go-fila/go-fila/pkg/plugin"
)

// tableName is the SQL table the audit entries are written to.
const tableName = "audit_log"

// auditPlugin implements plugin.Plugin.
type auditPlugin struct {
	driver        string
	table         string
	retentionDays int
}

// New returns the plugin instance. Declared by convention at the module root.
func New() plugin.Plugin {
	return &auditPlugin{table: tableName, retentionDays: 90}
}

// ID is the plugin's stable identifier.
func (p *auditPlugin) ID() string { return "audit" }

// Configure receives the YAML config map plus the injected "driver" key.
func (p *auditPlugin) Configure(cfg map[string]any) error {
	if d, ok := cfg["driver"].(string); ok && d != "" {
		p.driver = d
	}
	if t, ok := cfg["table"].(string); ok && t != "" {
		p.table = t
	}
	if r, ok := cfg["retention_days"].(float64); ok && r > 0 {
		p.retentionDays = int(r)
	}
	return nil
}

// Register contributes the resource, page, navigation, SQL files and the
// Customer delete hook to the panel.
func (p *auditPlugin) Register(pb *plugin.Panel) error {
	if p.driver == "" {
		return fmt.Errorf("audit plugin: no database driver injected by go-fila")
	}

	if err := pb.AddResource(plugin.Resource{
		Name:  "AuditLog",
		Label: "Audit Logs",
		Table: p.table,
		List: &plugin.ListConfig{
			Query:       "ListAuditLogs",
			CountQuery:  "CountAuditLogs",
			DefaultSort: "-created_at",
			Columns: []plugin.Column{
				{Name: "id", Label: "ID", Type: "integer", Sortable: true},
				{Name: "table_name", Label: "Table", Type: "string"},
				{Name: "record_id", Label: "Record", Type: "integer"},
				{Name: "action", Label: "Action", Type: "badge", Options: map[string]string{
					"create": "success",
					"update": "warning",
					"delete": "danger",
				}},
				{Name: "message", Label: "Message", Type: "string"},
				{Name: "created_at", Label: "Created", Type: "datetime", Sortable: true},
			},
		},
		Detail: &plugin.DetailConfig{
			Query:  "GetAuditLog",
			Params: map[string]string{"id": "{record.id}"},
			Fields: []plugin.Field{
				{Name: "id", Label: "ID", Type: "integer"},
				{Name: "table_name", Label: "Table", Type: "string"},
				{Name: "record_id", Label: "Record", Type: "integer"},
				{Name: "action", Label: "Action", Type: "string"},
				{Name: "message", Label: "Message", Type: "text"},
				{Name: "created_at", Label: "Created", Type: "datetime"},
			},
		},
	}); err != nil {
		return err
	}

	if err := pb.AddPage(plugin.Page{
		Name:    "AuditOverview",
		Default: false,
		Widgets: []plugin.Widget{
			{
				Type:  "stat",
				Label: "Audit Entries",
				Query: fmt.Sprintf("SELECT COUNT(*) FROM %s", p.table),
				Icon:  "clock",
			},
			{
				Type:  "stat",
				Label: "Deletes Logged",
				Query: fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE action = 'delete'", p.table),
				Icon:  "trash",
			},
		},
	}); err != nil {
		return err
	}

	pb.AddNavigationGroup(plugin.NavigationGroup{
		Group: "Audit",
		Icon:  "clock",
		Sort:  90,
		Items: []plugin.NavigationItem{
			{Resource: "AuditLog"},
			{Page: "AuditOverview"},
		},
	})

	pb.AddSQLFile("migrations/audit_schema.sql", p.schemaSQL())
	pb.AddSQLFile("queries/audit.sql", p.queriesSQL())

	return pb.AddHookToResource("Customer", "delete", "after", plugin.Hook{
		Name: "audit_customer_delete",
		SQL: fmt.Sprintf(
			"INSERT INTO %s (table_name, record_id, action, message, created_at) VALUES ('customers', $1, 'delete', 'Customer deleted', CURRENT_TIMESTAMP)",
			p.table),
	})
}

// Boot is a no-op for this plugin.
func (p *auditPlugin) Boot(pb *plugin.Panel) error { return nil }

// schemaSQL returns the audit_log DDL in the dialect matching the configured
// driver. The file is consumed by sqlc for type inference (never executed
// against the database); the e2e setup applies the equivalent DDL per driver.
func (p *auditPlugin) schemaSQL() string {
	idCol := "id SERIAL PRIMARY KEY"
	if p.driver == "sqlite" || p.driver == "sqlite3" {
		idCol = "id INTEGER PRIMARY KEY AUTOINCREMENT"
	}
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
    %s,
    table_name TEXT NOT NULL,
    record_id INTEGER NOT NULL,
    action TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`, p.table, idCol)
}

// queriesSQL returns the audit sqlc queries using the driver-appropriate bind
// placeholder ($N for postgres/mssql, ? for sqlite).
func (p *auditPlugin) queriesSQL() string {
	ph := "$1"
	if p.driver == "sqlite" || p.driver == "sqlite3" {
		ph = "?"
	}
	return fmt.Sprintf(`-- name: ListAuditLogs :many
SELECT id, table_name, record_id, action, message, created_at FROM %s ORDER BY created_at DESC;

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM %s;

-- name: GetAuditLog :one
SELECT id, table_name, record_id, action, message, created_at FROM %s WHERE id = %s;
`, p.table, p.table, p.table, ph)
}
