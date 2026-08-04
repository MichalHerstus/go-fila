// introspect.go
//
// Implements `go-fila init --db {dsn}`: connects to an existing database,
// introspects its schema (tables, columns, primary keys, foreign keys),
// generates go-fila.yaml and SQL migration/query files from the discovered
// tables. Creates users/roles auth tables + admin user when missing.
package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// ColumnInfo describes a single column as discovered by schema introspection.
type ColumnInfo struct {
	Name         string
	DBType       string
	Nullable     bool
	Default      string
	IsPrimaryKey bool
}

// ForeignKeyInfo describes a foreign key constraint discovered on a table.
type ForeignKeyInfo struct {
	Column        string
	ForeignTable  string
	ForeignColumn string
}

// TableInfo describes a database table as discovered by schema introspection.
type TableInfo struct {
	Name        string
	Columns     []ColumnInfo
	ForeignKeys []ForeignKeyInfo
}

// detectDriver determines the database driver from a DSN string. Postgres DSNs
// start with "postgres://" or "postgresql://"; everything else is treated as
// sqlite (file path, :memory:, etc.).
func detectDriver(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return "postgres"
	}
	return "sqlite"
}

// openDB opens a database connection using the appropriate driver for the DSN.
func openDB(dsn, driver string) (*sql.DB, error) {
	if driver == "postgres" {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening postgres connection: %w", err)
		}
		return db, nil
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite connection: %w", err)
	}
	return db, nil
}

// introspectSchema discovers all user tables in the database along with their
// columns, primary keys and foreign keys.
func introspectSchema(db *sql.DB, driver string) ([]TableInfo, error) {
	if driver == "postgres" {
		return introspectPostgres(db)
	}
	return introspectSQLite(db)
}

// introspectPostgres queries information_schema to discover tables, columns,
// primary keys and foreign keys in a PostgreSQL database.
func introspectPostgres(db *sql.DB) ([]TableInfo, error) {
	rows, err := db.Query(`
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		ORDER BY table_name`)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var tables []TableInfo
	for _, name := range tableNames {
		ti := TableInfo{Name: name}

		colRows, err := db.Query(`
			SELECT column_name, data_type, is_nullable, column_default
			FROM information_schema.columns
			WHERE table_name = $1 AND table_schema = 'public'
			ORDER BY ordinal_position`, name)
		if err != nil {
			return nil, fmt.Errorf("listing columns for %s: %w", name, err)
		}
		for colRows.Next() {
			var c ColumnInfo
			var nullable string
			var defaultVal sql.NullString
			if err := colRows.Scan(&c.Name, &c.DBType, &nullable, &defaultVal); err != nil {
				colRows.Close()
				return nil, err
			}
			c.Nullable = nullable == "YES"
			if defaultVal.Valid {
				c.Default = defaultVal.String
			}
			ti.Columns = append(ti.Columns, c)
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			return nil, err
		}

		pkRows, err := db.Query(`
			SELECT kcu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			WHERE tc.table_name = $1
				AND tc.constraint_type = 'PRIMARY KEY'
				AND tc.table_schema = 'public'
			ORDER BY kcu.ordinal_position`, name)
		if err != nil {
			return nil, fmt.Errorf("listing PKs for %s: %w", name, err)
		}
		for pkRows.Next() {
			var colName string
			if err := pkRows.Scan(&colName); err != nil {
				pkRows.Close()
				return nil, err
			}
			for i := range ti.Columns {
				if ti.Columns[i].Name == colName {
					ti.Columns[i].IsPrimaryKey = true
					break
				}
			}
		}
		pkRows.Close()
		if err := pkRows.Err(); err != nil {
			return nil, err
		}

		fkRows, err := db.Query(`
			SELECT kcu.column_name, ccu.table_name, ccu.column_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			JOIN information_schema.constraint_column_usage ccu
				ON tc.constraint_name = ccu.constraint_name
				AND tc.table_schema = ccu.table_schema
			WHERE tc.table_name = $1
				AND tc.constraint_type = 'FOREIGN KEY'
				AND tc.table_schema = 'public'`, name)
		if err != nil {
			return nil, fmt.Errorf("listing FKs for %s: %w", name, err)
		}
		for fkRows.Next() {
			var fk ForeignKeyInfo
			if err := fkRows.Scan(&fk.Column, &fk.ForeignTable, &fk.ForeignColumn); err != nil {
				fkRows.Close()
				return nil, err
			}
			ti.ForeignKeys = append(ti.ForeignKeys, fk)
		}
		fkRows.Close()
		if err := fkRows.Err(); err != nil {
			return nil, err
		}

		tables = append(tables, ti)
	}
	return tables, nil
}

// introspectSQLite queries sqlite_master and PRAGMA statements to discover
// tables, columns, primary keys and foreign keys in a SQLite database.
func introspectSQLite(db *sql.DB) ([]TableInfo, error) {
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var tables []TableInfo
	for _, name := range tableNames {
		ti := TableInfo{Name: name}

		colRows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, name))
		if err != nil {
			return nil, fmt.Errorf("listing columns for %s: %w", name, err)
		}
		for colRows.Next() {
			var c ColumnInfo
			var cid int
			var notnull int
			var pk int
			var defaultVal sql.NullString
			if err := colRows.Scan(&cid, &c.Name, &c.DBType, &notnull, &defaultVal, &pk); err != nil {
				colRows.Close()
				return nil, err
			}
			c.Nullable = notnull == 0
			c.IsPrimaryKey = pk > 0
			if defaultVal.Valid {
				c.Default = defaultVal.String
			}
			ti.Columns = append(ti.Columns, c)
		}
		colRows.Close()
		if err := colRows.Err(); err != nil {
			return nil, err
		}

		fkRows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, name))
		if err != nil {
			return nil, fmt.Errorf("listing FKs for %s: %w", name, err)
		}
		for fkRows.Next() {
			var fk ForeignKeyInfo
			var id, seq int
			var onUpdate, onDelete, match string
			if err := fkRows.Scan(&id, &seq, &fk.ForeignTable, &fk.Column, &fk.ForeignColumn, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				return nil, err
			}
			ti.ForeignKeys = append(ti.ForeignKeys, fk)
		}
		fkRows.Close()
		if err := fkRows.Err(); err != nil {
			return nil, err
		}

		tables = append(tables, ti)
	}
	return tables, nil
}

// mapDBTypeToFieldType converts a database column type string to a go-fila
// field type. It strips parenthesised size modifiers and matches against
// known type prefixes.
func mapDBTypeToFieldType(dbType string) string {
	t := strings.ToLower(dbType)
	if idx := strings.Index(t, "("); idx != -1 {
		t = t[:idx]
	}
	t = strings.TrimSpace(t)

	switch {
	case strings.Contains(t, "int") || strings.Contains(t, "serial") || strings.Contains(t, "bigserial") || strings.Contains(t, "smallserial"):
		return "integer"
	case strings.Contains(t, "varchar") || strings.Contains(t, "text") || strings.Contains(t, "char") || strings.Contains(t, "character"):
		return "string"
	case strings.Contains(t, "bool"):
		return "boolean"
	case strings.Contains(t, "timestamp") || strings.Contains(t, "datetime") || strings.Contains(t, "date"):
		return "datetime"
	case strings.Contains(t, "real") || strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "numeric") || strings.Contains(t, "decimal"):
		return "float"
	case strings.Contains(t, "json"):
		return "json"
	case strings.Contains(t, "bytea") || strings.Contains(t, "blob"):
		return "file"
	default:
		return "string"
	}
}

// singularize converts a plural table name to a singular resource name.
// It handles common English plurals: "s", "ies", "es", "ses".
func singularize(tableName string) string {
	lower := strings.ToLower(tableName)
	switch {
	case strings.HasSuffix(lower, "ies"):
		return tableName[:len(tableName)-3] + "y"
	case strings.HasSuffix(lower, "ses") || strings.HasSuffix(lower, "xes") || strings.HasSuffix(lower, "zes") || strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes"):
		return tableName[:len(tableName)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss"):
		return tableName[:len(tableName)-1]
	default:
		return tableName
	}
}

// toPascalCase converts a snake_case or lowercase string to PascalCase.
func toPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// toSingularPascal converts a table name to singular PascalCase.
// e.g. "user_roles" -> "UserRole", "order_items" -> "OrderItem"
func toSingularPascal(tableName string) string {
	return toPascalCase(singularize(tableName))
}

// pluralize converts a singular resource name to a plural table-like name.
func pluralize(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "y") && !containsAny(lower, "a", "e", "i", "o", "u"):
		return name[:len(name)-1] + "ies"
	case strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") || strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "sh"):
		return name + "es"
	default:
		return name + "s"
	}
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// findLabelColumn picks the "best" column to display as a human-readable label
// for a table. It prefers columns named "name", "title", or "label", then
// falls back to the first non-PK text column, then the PK.
func findLabelColumn(ti TableInfo) string {
	for _, c := range ti.Columns {
		if strings.ToLower(c.Name) == "name" {
			return c.Name
		}
	}
	for _, c := range ti.Columns {
		if strings.ToLower(c.Name) == "title" {
			return c.Name
		}
	}
	for _, c := range ti.Columns {
		if strings.ToLower(c.Name) == "label" {
			return c.Name
		}
	}
	for _, c := range ti.Columns {
		if !c.IsPrimaryKey && mapDBTypeToFieldType(c.DBType) == "string" {
			return c.Name
		}
	}
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			return c.Name
		}
	}
	if len(ti.Columns) > 0 {
		return ti.Columns[0].Name
	}
	return ""
}

// findLabelColumnByTable finds the label column for a table name in a list of tables.
func findLabelColumnByTable(tables []TableInfo, tableName string) string {
	for _, ti := range tables {
		if ti.Name == tableName {
			return findLabelColumn(ti)
		}
	}
	return "name"
}

// findTableByName finds a table by name in a list of tables.
func findTableByName(tables []TableInfo, name string) *TableInfo {
	for i, ti := range tables {
		if ti.Name == name {
			return &tables[i]
		}
	}
	return nil
}

// findPKColumn returns the primary key column name for a table. Returns "id"
// as a fallback.
func findPKColumn(ti TableInfo) string {
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			return c.Name
		}
	}
	return "id"
}

// placeholder returns the SQL bind placeholder for the given 1-based argument
// position. Postgres uses $N; sqlite uses "?".
func placeholder(n int, driver string) string {
	if driver == "sqlite" {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// ensureAuthTables checks whether "users" and "roles" tables exist in the
// database. If either is missing, both are created with a driver-appropriate
// DDL, default roles are seeded, and an admin user is inserted.
func ensureAuthTables(db *sql.DB, driver string, tables []TableInfo) error {
	hasUsers := findTableByName(tables, "users") != nil
	hasRoles := findTableByName(tables, "roles") != nil

	if hasUsers && hasRoles {
		return nil
	}

	if driver == "postgres" {
		if !hasRoles {
			if _, err := db.Exec(`
				CREATE TABLE roles (
					id SERIAL PRIMARY KEY,
					name VARCHAR(100) NOT NULL
				)`); err != nil {
				return fmt.Errorf("creating roles table: %w", err)
			}
			if _, err := db.Exec(`INSERT INTO roles (name) VALUES ('admin'), ('manager'), ('user')`); err != nil {
				return fmt.Errorf("seeding roles: %w", err)
			}
		}
		if !hasUsers {
			if _, err := db.Exec(`
				CREATE TABLE users (
					id SERIAL PRIMARY KEY,
					name VARCHAR(255) NOT NULL,
					email VARCHAR(255) UNIQUE NOT NULL,
					password VARCHAR(255) NOT NULL,
					role_id INT REFERENCES roles(id),
					role_name VARCHAR(100) DEFAULT 'user',
					status VARCHAR(20) DEFAULT 'active',
					created_at TIMESTAMPTZ DEFAULT NOW()
				)`); err != nil {
				return fmt.Errorf("creating users table: %w", err)
			}
		}
	} else {
		if !hasRoles {
			if _, err := db.Exec(`
				CREATE TABLE roles (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL
				)`); err != nil {
				return fmt.Errorf("creating roles table: %w", err)
			}
			if _, err := db.Exec(`INSERT INTO roles (name) VALUES ('admin'), ('manager'), ('user')`); err != nil {
				return fmt.Errorf("seeding roles: %w", err)
			}
		}
		if !hasUsers {
			if _, err := db.Exec(`
				CREATE TABLE users (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					name TEXT NOT NULL,
					email TEXT UNIQUE NOT NULL,
					password TEXT NOT NULL,
					role_id INTEGER REFERENCES roles(id),
					role_name TEXT DEFAULT 'user',
					status TEXT DEFAULT 'active',
					created_at TEXT DEFAULT (datetime('now'))
				)`); err != nil {
				return fmt.Errorf("creating users table: %w", err)
			}
		}
	}

	return nil
}

// insertAdminUser inserts a default admin user into the users table if it is
// empty. Credentials are fixed: admin@admin.test / admin.
func insertAdminUser(db *sql.DB, driver string) error {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return nil
	}

	hash, err := bcryptHash("admin")
	if err != nil {
		return fmt.Errorf("hashing admin password: %w", err)
	}

	var adminRoleID int
	if driver == "postgres" {
		err = db.QueryRow(`SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID)
	} else {
		err = db.QueryRow(`SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID)
	}
	if err != nil {
		return fmt.Errorf("finding admin role: %w", err)
	}

	if driver == "postgres" {
		_, err = db.Exec(`
			INSERT INTO users (name, email, password, role_id, role_name, status)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			"Admin User", "admin@admin.test", hash, adminRoleID, "admin", "active")
	} else {
		_, err = db.Exec(`
			INSERT INTO users (name, email, password, role_id, role_name, status)
			VALUES (?, ?, ?, ?, ?, ?)`,
			"Admin User", "admin@admin.test", hash, adminRoleID, "admin", "active")
	}
	if err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}
	return nil
}

// bcryptHash produces a bcrypt hash of the given plaintext password.
func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// generateYAML builds a go-fila.yaml config string from the introspected
// schema. It creates a resource for each table (excluding users/roles) with
// list, detail and form sections. Foreign keys become relation fields with
// options_query.
func generateYAML(tables []TableInfo, driver, dsn string) string {
	var b strings.Builder

	b.WriteString(`version: "1.0"

panel:
  id: admin
  path: /admin
  name: "My Admin"

connections:
  default:
    driver: `)
	b.WriteString(driver)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf(`    dsn: %q
`, dsn))

	b.WriteString(`
sqlc:
  config: sqlc.yaml
  queries_dir: ./sql/queries
  schema_dir: ./sql/migrations
  output_pkg: internal/data

auth:
  guard: web
  provider: session
  table: users
  login:
    fields: [email, password]
    redirect: /admin/dashboard

resources:
`)

	for _, ti := range tables {
		if ti.Name == "users" || ti.Name == "roles" {
			continue
		}
		writeResourceYAML(&b, ti, tables, driver)
	}

	b.WriteString(`
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stats_grid
        columns: 2
        widgets:
          - type: stat
            label: "Total Users"
            query: SELECT COUNT(*) FROM users
            icon: users
          - type: stat
            label: "Active Users"
            query: SELECT COUNT(*) FROM users WHERE status = 'active'
            icon: check

navigation:
  - group: "Management"
    icon: all
    sort: 1
    items:
`)

	for _, ti := range tables {
		if ti.Name == "users" || ti.Name == "roles" {
			continue
		}
		resourceName := toSingularPascal(ti.Name)
		b.WriteString(fmt.Sprintf("      - resource: %s\n", resourceName))
	}

	return b.String()
}

// writeResourceYAML writes a single resource definition in YAML format to the
// builder. It configures list columns, detail fields and form fields based on
// the introspected columns and foreign keys.
func writeResourceYAML(b *strings.Builder, ti TableInfo, allTables []TableInfo, driver string) {
	resourceName := toSingularPascal(ti.Name)
	pluralPascal := toPascalCase(ti.Name)
	pk := findPKColumn(ti)

	b.WriteString(fmt.Sprintf("  - name: %s\n", resourceName))
	b.WriteString(fmt.Sprintf("    label: %s\n", pluralPascal))

	defaultSort := findDefaultSort(ti)

	// list section
	b.WriteString("    list:\n")
	b.WriteString(fmt.Sprintf("      query: List%s\n", pluralPascal))
	b.WriteString(fmt.Sprintf("      count_query: Count%s\n", pluralPascal))
	b.WriteString("      columns:\n")

	for _, c := range ti.Columns {
		isFK := false
		for _, fk := range ti.ForeignKeys {
			if fk.Column == c.Name {
				isFK = true
				break
			}
		}
		if isFK {
			continue
		}
		writeColumnYAML(b, c)
	}

	// FK label columns in the list
	for _, fk := range ti.ForeignKeys {
		foreignTable := findTableByName(allTables, fk.ForeignTable)
		if foreignTable == nil {
			continue
		}
		labelCol := findLabelColumn(*foreignTable)
		colName := fk.Column + "_label"
		b.WriteString(fmt.Sprintf("        - name: %s\n", colName))
		b.WriteString(fmt.Sprintf("          label: %s\n", toPascalCase(singularize(fk.ForeignTable))))
		b.WriteString("          type: string\n")
		_ = labelCol
	}

	if defaultSort != "" {
		b.WriteString(fmt.Sprintf("      default_sort: -%s\n", defaultSort))
	}

	// detail section
	b.WriteString("    detail:\n")
	b.WriteString(fmt.Sprintf("      query: Get%s\n", toSingularPascal(ti.Name)))
	b.WriteString("      params:\n")
	b.WriteString(fmt.Sprintf("        id: \"{record.%s}\"\n", pk))
	b.WriteString("      fields:\n")
	for _, c := range ti.Columns {
		writeFieldYAML(b, c, ti, allTables, driver, false, "        ")
	}

	// form section
	b.WriteString("    form:\n")

	// create
	b.WriteString("      create:\n")
	b.WriteString(fmt.Sprintf("        query: Create%s\n", toSingularPascal(ti.Name)))
	b.WriteString("        fields:\n")
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			continue
		}
		if c.Default != "" && c.Nullable {
			continue
		}
		writeFieldYAML(b, c, ti, allTables, driver, true, "          ")
	}

	// update
	b.WriteString("      update:\n")
	b.WriteString(fmt.Sprintf("        query: Update%s\n", toSingularPascal(ti.Name)))
	b.WriteString(fmt.Sprintf("        populate_query: Get%s\n", toSingularPascal(ti.Name)))
	b.WriteString("        fields:\n")
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			continue
		}
		writeFieldYAML(b, c, ti, allTables, driver, true, "          ")
	}
}

// writeColumnYAML writes a list column definition.
func writeColumnYAML(b *strings.Builder, c ColumnInfo) {
	ft := mapDBTypeToFieldType(c.DBType)
	b.WriteString(fmt.Sprintf("        - name: %s\n", c.Name))
	b.WriteString(fmt.Sprintf("          type: %s\n", ft))
	if c.IsPrimaryKey || ft == "integer" {
		b.WriteString("          sortable: true\n")
	}
	if ft == "string" || ft == "email" {
		b.WriteString("          searchable: true\n")
	}
}

// writeFieldYAML writes a detail/form field definition with the given
// indentation prefix. For foreign key columns in forms, it writes a relation
// field with options_query.
func writeFieldYAML(b *strings.Builder, c ColumnInfo, ti TableInfo, allTables []TableInfo, driver string, isForm bool, indent string) {
	for _, fk := range ti.ForeignKeys {
		if fk.Column == c.Name {
			if isForm {
				b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
				b.WriteString(indent + "  type: relation\n")
				foreignResource := toSingularPascal(fk.ForeignTable)
				b.WriteString(fmt.Sprintf("%s  options_query: List%s\n", indent, pluralize(foreignResource)))
				b.WriteString(fmt.Sprintf("%s  options_value: %s\n", indent, fk.ForeignColumn))
				labelCol := findLabelColumnByTable(allTables, fk.ForeignTable)
				b.WriteString(fmt.Sprintf("%s  options_label: %s\n", indent, labelCol))
			} else {
				b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
				b.WriteString(indent + "  type: integer\n")
			}
			return
		}
	}

	ft := mapDBTypeToFieldType(c.DBType)
	b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
	b.WriteString(fmt.Sprintf("%s  type: %s\n", indent, ft))
	if c.Name == "password" {
		b.WriteString(indent + "  type: password\n")
	}
}

// findDefaultSort picks a sensible default sort column: the first datetime
// column, or the primary key.
func findDefaultSort(ti TableInfo) string {
	for _, c := range ti.Columns {
		ft := mapDBTypeToFieldType(c.DBType)
		if ft == "datetime" {
			return c.Name
		}
	}
	return findPKColumn(ti)
}

// generateSchemaSQL produces the DDL for auth tables (roles + users) if they
// were created by ensureAuthTables. Returns empty string if auth tables already
// existed.
func generateSchemaSQL(hasRoles, hasUsers bool, driver string) string {
	if hasRoles && hasUsers {
		return ""
	}

	var b strings.Builder
	if driver == "postgres" {
		if !hasRoles {
			b.WriteString(`CREATE TABLE roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

INSERT INTO roles (name) VALUES ('admin'), ('manager'), ('user');

`)
		}
		if !hasUsers {
			b.WriteString(`CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    role_id INT REFERENCES roles(id),
    role_name VARCHAR(100) DEFAULT 'user',
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

`)
		}
	} else {
		if !hasRoles {
			b.WriteString(`CREATE TABLE roles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL
);

INSERT INTO roles (name) VALUES ('admin'), ('manager'), ('user');

`)
		}
		if !hasUsers {
			b.WriteString(`CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password TEXT NOT NULL,
    role_id INTEGER REFERENCES roles(id),
    role_name TEXT DEFAULT 'user',
    status TEXT DEFAULT 'active',
    created_at TEXT DEFAULT (datetime('now'))
);

`)
		}
	}
	return b.String()
}

// generateQueries produces SQLC-annotated query files for each table
// (excluding users/roles). Returns a map of filename -> SQL content.
func generateQueries(tables []TableInfo, driver string) map[string]string {
	queries := make(map[string]string)
	generatedFKTargets := map[string]bool{}

	// generate queries for user-visible tables
	for _, ti := range tables {
		if ti.Name == "users" || ti.Name == "roles" {
			continue
		}
		var b strings.Builder
		pluralName := toPascalCase(ti.Name)
		singularName := toSingularPascal(ti.Name)
		pk := findPKColumn(ti)

		// ListUsers-style: with LEFT JOINs for FK label columns
		b.WriteString(fmt.Sprintf("-- name: List%s :many\n", pluralName))
		b.WriteString("SELECT ")
		for i, c := range ti.Columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("t.%s", c.Name))
		}
		for _, fk := range ti.ForeignKeys {
			foreignTable := findTableByName(tables, fk.ForeignTable)
			if foreignTable == nil {
				continue
			}
			labelCol := findLabelColumn(*foreignTable)
			b.WriteString(fmt.Sprintf(", f_%s.%s AS %s_label", fk.ForeignTable, labelCol, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nFROM %s t", ti.Name))
		for _, fk := range ti.ForeignKeys {
			foreignTable := findTableByName(tables, fk.ForeignTable)
			if foreignTable == nil {
				continue
			}
			foreignPK := findPKColumn(*foreignTable)
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, foreignPK, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nORDER BY t.%s DESC;\n\n", pk))

		// Count
		b.WriteString(fmt.Sprintf("-- name: Count%s :one\n", pluralName))
		b.WriteString(fmt.Sprintf("SELECT COUNT(*) FROM %s;\n\n", ti.Name))

		// Get
		b.WriteString(fmt.Sprintf("-- name: Get%s :one\n", singularName))
		b.WriteString("SELECT ")
		for i, c := range ti.Columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("t.%s", c.Name))
		}
		for _, fk := range ti.ForeignKeys {
			foreignTable := findTableByName(tables, fk.ForeignTable)
			if foreignTable == nil {
				continue
			}
			labelCol := findLabelColumn(*foreignTable)
			b.WriteString(fmt.Sprintf(", f_%s.%s AS %s_label", fk.ForeignTable, labelCol, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nFROM %s t", ti.Name))
		for _, fk := range ti.ForeignKeys {
			foreignTable := findTableByName(tables, fk.ForeignTable)
			if foreignTable == nil {
				continue
			}
			foreignPK := findPKColumn(*foreignTable)
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, foreignPK, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nWHERE t.%s = %s;\n\n", pk, placeholder(1, driver)))

		// Create
		var insertCols []string
		for _, c := range ti.Columns {
			if c.IsPrimaryKey && (driver == "postgres" || strings.ToLower(c.DBType) == "integer") {
				continue
			}
			insertCols = append(insertCols, c.Name)
		}
		b.WriteString(fmt.Sprintf("-- name: Create%s :one\n", singularName))
		b.WriteString(fmt.Sprintf("INSERT INTO %s (", ti.Name))
		for i, col := range insertCols {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(col)
		}
		b.WriteString(")\nVALUES (")
		for i := range insertCols {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(placeholder(i+1, driver))
		}
		if driver == "postgres" {
			b.WriteString(")\nRETURNING *;\n\n")
		} else {
			b.WriteString(");\n\n")
		}

		// Update
		b.WriteString(fmt.Sprintf("-- name: Update%s :one\n", singularName))
		b.WriteString(fmt.Sprintf("UPDATE %s SET\n", ti.Name))
		argN := 2
		for i, c := range ti.Columns {
			if c.IsPrimaryKey {
				continue
			}
			comma := ","
			if i == len(ti.Columns)-1 {
				comma = ""
			}
			b.WriteString(fmt.Sprintf("    %s = %s%s\n", c.Name, placeholder(argN, driver), comma))
			argN++
		}
		b.WriteString(fmt.Sprintf("WHERE %s = %s", pk, placeholder(1, driver)))
		if driver == "postgres" {
			b.WriteString("\nRETURNING *;\n\n")
		} else {
			b.WriteString(";\n\n")
		}

		// Delete
		b.WriteString(fmt.Sprintf("-- name: Delete%s :exec\n", singularName))
		b.WriteString(fmt.Sprintf("DELETE FROM %s WHERE %s = %s;\n\n", ti.Name, pk, placeholder(1, driver)))

		queries[ti.Name+".sql"] = b.String()
	}

	// generate List queries for FK target tables (used by relation options_query)
	for _, ti := range tables {
		for _, fk := range ti.ForeignKeys {
			if fk.ForeignTable == "users" || fk.ForeignTable == "roles" {
				continue
			}
			if generatedFKTargets[fk.ForeignTable] {
				continue
			}
			generatedFKTargets[fk.ForeignTable] = true

			foreignTI := findTableByName(tables, fk.ForeignTable)
			if foreignTI == nil {
				continue
			}
			foreignPluralName := toPascalCase(foreignTI.Name)
			labelCol := findLabelColumn(*foreignTI)

			var b strings.Builder
			b.WriteString(fmt.Sprintf("-- name: List%s :many\n", foreignPluralName))
			b.WriteString(fmt.Sprintf("SELECT %s, %s FROM %s ORDER BY %s;\n\n",
				foreignPKColumn(*foreignTI), labelCol, foreignTI.Name, labelCol))

			fname := foreignTI.Name + "_options.sql"
			if _, exists := queries[fname]; !exists {
				queries[fname] = b.String()
			}
		}
	}

	return queries
}

// foreignPKColumn returns the primary key column name of a table.
func foreignPKColumn(ti TableInfo) string {
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			return c.Name
		}
	}
	return "*"
}

// cmdInitFromDB is the main entry point for `go-fila init --db {dsn}`. It
// connects to the database, introspects the schema, creates auth tables if
// missing, inserts an admin user when the users table is empty, then generates
// go-fila.yaml and SQL files.
func cmdInitFromDB(configPath, outDir, dsn string, force bool) error {
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("%s already exists. Use --force to overwrite.", configPath)
		}
		if _, err := os.Stat(outDir); err == nil {
			return fmt.Errorf("%s already exists. Use --force to overwrite.", outDir)
		}
	}

	driver := detectDriver(dsn)

	db, err := openDB(dsn, driver)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}

	tables, err := introspectSchema(db, driver)
	if err != nil {
		return fmt.Errorf("introspecting schema: %w", err)
	}

	if len(tables) == 0 {
		fmt.Println("No tables found in database.")
	}

	hasRoles := findTableByName(tables, "roles") != nil
	hasUsers := findTableByName(tables, "users") != nil

	if err := ensureAuthTables(db, driver, tables); err != nil {
		return fmt.Errorf("ensuring auth tables: %w", err)
	}

	if err := insertAdminUser(db, driver); err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	if !hasRoles || !hasUsers {
		fmt.Println("Created auth tables (users, roles) and seeded admin user.")
	}

	// Re-introspect after creating auth tables so the full schema is available
	// for YAML/SQL generation (needed for FK references to roles, etc.)
	tables, err = introspectSchema(db, driver)
	if err != nil {
		return fmt.Errorf("re-introspecting schema: %w", err)
	}

	// Write go-fila.yaml
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(configPath, []byte(generateYAML(tables, driver, dsn)), 0644); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}

	// Write SQL files
	sqlDir := filepath.Join(outDir, "sql")
	if err := os.MkdirAll(filepath.Join(sqlDir, "migrations"), 0755); err != nil {
		return fmt.Errorf("creating migrations directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(sqlDir, "queries"), 0755); err != nil {
		return fmt.Errorf("creating queries directory: %w", err)
	}

	schemaSQL := generateSchemaSQL(hasRoles, hasUsers, driver)
	if schemaSQL != "" {
		if err := os.WriteFile(filepath.Join(sqlDir, "migrations", "schema.sql"), []byte(schemaSQL), 0644); err != nil {
			return fmt.Errorf("writing schema.sql: %w", err)
		}
	}

	for name, content := range generateQueries(tables, driver) {
		if err := os.WriteFile(filepath.Join(sqlDir, "queries", name), []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}

	fmt.Println("Introspected database and generated config in", outDir)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Review", configPath)
	fmt.Println("  2. Run 'go-fila generate --config", configPath, "--out", outDir, "'")
	return nil
}
