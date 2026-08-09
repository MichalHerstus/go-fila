// introspect.go
//
// Implements `go-fila init --db {dsn}`: connects to an existing database,
// introspects its schema (tables, columns, primary keys, foreign keys),
// generates go-fila.yaml and SQL migration/query files from the discovered
// tables. Creates users/roles auth tables + admin user when missing.
package main

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "github.com/microsoft/go-mssqldb"
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
// start with "postgres://" or "postgresql://", MSSQL DSNs with "sqlserver://"
// or "mssql://"; everything else is treated as sqlite (file path, :memory:,
// etc.).
func detectDriver(dsn string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		return "postgres"
	}
	if strings.HasPrefix(dsn, "sqlserver://") || strings.HasPrefix(dsn, "mssql://") {
		return "mssql"
	}
	return "sqlite"
}

// openDB opens a database connection using the appropriate driver for the DSN.
// MSSQL uses the "mssql" driver name so the driver's loose placeholder parsing
// accepts the $N placeholders that the postgres-flavored SQL uses.
func openDB(dsn, driver string) (*sql.DB, error) {
	if driver == "postgres" {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening postgres connection: %w", err)
		}
		return db, nil
	}
	if driver == "mssql" {
		db, err := sql.Open("mssql", dsn)
		if err != nil {
			return nil, fmt.Errorf("opening mssql connection: %w", err)
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
	if driver == "mssql" {
		return introspectMSSQL(db)
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

// introspectMSSQL queries INFORMATION_SCHEMA and sys views to discover tables,
// columns, primary keys and foreign keys in a SQL Server database. Table
// discovery is restricted to the current schema (SCHEMA_NAME()). The $N
// placeholders work because the connection uses the mssql driver name with
// loose placeholder parsing.
func introspectMSSQL(db *sql.DB) ([]TableInfo, error) {
	rows, err := db.Query(`
		SELECT table_name FROM INFORMATION_SCHEMA.TABLES
		WHERE table_type = 'BASE TABLE' AND table_schema = SCHEMA_NAME()
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
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE table_name = $1 AND table_schema = SCHEMA_NAME()
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
			FROM INFORMATION_SCHEMA.table_constraints tc
			JOIN INFORMATION_SCHEMA.key_column_usage kcu
				ON tc.constraint_name = kcu.constraint_name
				AND tc.table_schema = kcu.table_schema
			WHERE tc.table_name = $1
				AND tc.constraint_type = 'PRIMARY KEY'
				AND tc.table_schema = SCHEMA_NAME()
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

		// Tables without a declared PRIMARY KEY still often have an IDENTITY
		// column that acts as the row key (e.g. legacy MSSQL schemas). Treat
		// identity columns as the primary key so go-fila keys routes on them
		// and omits them from INSERT/UPDATE. A declared PK takes precedence.
		hasPK := false
		for i := range ti.Columns {
			if ti.Columns[i].IsPrimaryKey {
				hasPK = true
				break
			}
		}
		if !hasPK {
			idRows, err := db.Query(`
				SELECT c.name
				FROM sys.columns c
				JOIN sys.tables t ON t.object_id = c.object_id
				WHERE t.name = $1 AND c.is_identity = 1`, name)
			if err != nil {
				return nil, fmt.Errorf("listing identity columns for %s: %w", name, err)
			}
			for idRows.Next() {
				var colName string
				if err := idRows.Scan(&colName); err != nil {
					idRows.Close()
					return nil, err
				}
				for i := range ti.Columns {
					if ti.Columns[i].Name == colName {
						ti.Columns[i].IsPrimaryKey = true
						break
					}
				}
			}
			idRows.Close()
			if err := idRows.Err(); err != nil {
				return nil, err
			}
		}

		fkRows, err := db.Query(`
			SELECT fk_col.name AS column_name, rt.name AS foreign_table, rc.name AS foreign_column
			FROM sys.foreign_keys fk
			JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
			JOIN sys.tables t ON t.object_id = fk.parent_object_id
			JOIN sys.columns fk_col ON fk_col.object_id = fkc.parent_object_id AND fk_col.column_id = fkc.parent_column_id
			JOIN sys.tables rt ON rt.object_id = fk.referenced_object_id
			JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
			WHERE t.name = $1`, name)
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
	case strings.Contains(t, "varchar") || strings.Contains(t, "text") || strings.Contains(t, "char") || strings.Contains(t, "character") || strings.Contains(t, "uniqueidentifier") || strings.Contains(t, "xml"):
		return "string"
	case strings.Contains(t, "bool") || t == "bit":
		return "boolean"
	case strings.Contains(t, "timestamp") || strings.Contains(t, "datetime") || strings.Contains(t, "date") || t == "time":
		return "datetime"
	case strings.Contains(t, "real") || strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "numeric") || strings.Contains(t, "decimal") || strings.Contains(t, "money"):
		return "float"
	case strings.Contains(t, "json"):
		return "json"
	case strings.Contains(t, "bytea") || strings.Contains(t, "blob") || strings.Contains(t, "varbinary") || strings.Contains(t, "binary") || strings.Contains(t, "image"):
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

// idColumnName returns the actual column name go-fila should treat as the row
// key for a table: the primary key column when declared, otherwise the column
// conventionally named "id" (any case). Returns "" when neither exists. Unlike
// findPKColumn, this preserves the real column case so the key matches the
// names in list row maps (e.g. "ID" on MSSQL).
func idColumnName(ti TableInfo) string {
	pk := findPKColumn(ti)
	for _, c := range ti.Columns {
		if strings.EqualFold(c.Name, pk) {
			return c.Name
		}
	}
	return ""
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
	} else if driver == "mssql" {
		if !hasRoles {
			if _, err := db.Exec(`
				CREATE TABLE roles (
					id INT IDENTITY(1,1) PRIMARY KEY,
					name NVARCHAR(100) NOT NULL
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
					id INT IDENTITY(1,1) PRIMARY KEY,
					name NVARCHAR(255) NOT NULL,
					email NVARCHAR(255) UNIQUE NOT NULL,
					password NVARCHAR(255) NOT NULL,
					role_id INT REFERENCES roles(id),
					role_name NVARCHAR(100) DEFAULT 'user',
					status NVARCHAR(20) DEFAULT 'active',
					created_at DATETIME2 DEFAULT SYSUTCDATETIME()
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
// empty. Credentials: admin@admin.test / password (an empty password makes the
// caller generate and print a random one). Returns whether a user was inserted.
func insertAdminUser(db *sql.DB, driver, password string) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return false, fmt.Errorf("counting users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	hash, err := bcryptHash(password)
	if err != nil {
		return false, fmt.Errorf("hashing admin password: %w", err)
	}

	var adminRoleID int
	if driver == "postgres" {
		err = db.QueryRow(`SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID)
	} else {
		err = db.QueryRow(`SELECT id FROM roles WHERE name = 'admin'`).Scan(&adminRoleID)
	}
	if err != nil {
		return false, fmt.Errorf("finding admin role: %w", err)
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
		return false, fmt.Errorf("inserting admin user: %w", err)
	}
	return true, nil
}

// bcryptHash produces a bcrypt hash of the given plaintext password.
func bcryptHash(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// randomPassword returns a cryptographically random 14-character password built
// from an unambiguous alphabet (no 0/O, 1/l/I). It is used as the one-time
// admin password for --demo and --db scaffolding when --admin-password is not
// given, and is printed to the user instead of being embedded anywhere.
func randomPassword() string {
	const chars = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 14)
	if _, err := rand.Read(buf); err != nil {
		return "changeme"
	}
	for i, c := range buf {
		buf[i] = chars[int(c)%len(chars)]
	}
	return string(buf)
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

	if strings.ToLower(resourceName)+"s" != ti.Name {
		b.WriteString(fmt.Sprintf("    table: %s\n", ti.Name))
	}

	if idCol := idColumnName(ti); idCol != "" && idCol != "id" {
		b.WriteString(fmt.Sprintf("    id_column: %s\n", idCol))
	}

	pkGo := pkGoType(ti, driver)
	defaultPKGo := "int32"
	if driver == "sqlite" {
		defaultPKGo = "int64"
	}
	if pkGo != "" && pkGo != defaultPKGo {
		b.WriteString(fmt.Sprintf("    id_type: %s\n", pkGo))
	}

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
				b.WriteString(fmt.Sprintf("%s  options_query: List%s\n", indent, toPascalCase(fk.ForeignTable)))
				b.WriteString(fmt.Sprintf("%s  options_value: %s\n", indent, fk.ForeignColumn))
				labelCol := findLabelColumnByTable(allTables, fk.ForeignTable)
				b.WriteString(fmt.Sprintf("%s  options_label: %s\n", indent, labelCol))
			} else {
				b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
				b.WriteString(fmt.Sprintf("%s  type: %s\n", indent, mapDBTypeToFieldType(c.DBType)))
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

// generateSchemaSQL produces the DDL for every introspected user table plus
// the auth tables (roles + users) if they were created by ensureAuthTables.
// The output is the sqlc schema input, so it is written in the dialect the
// sqlc engine expects: postgres dialect for postgres and mssql projects (the
// mssql engine is postgres-flavored by design), sqlite dialect for sqlite.
func generateSchemaSQL(tables []TableInfo, hasRoles, hasUsers bool, driver string) string {
	var b strings.Builder

	for _, ti := range tables {
		if ti.Name == "users" || ti.Name == "roles" {
			continue
		}
		b.WriteString(tableDDL(ti, driver))
		b.WriteString("\n")
	}

	if driver == "sqlite" {
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
	} else {
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
	}
	return b.String()
}

// tableDDL renders a CREATE TABLE statement for one introspected table in the
// dialect used as the sqlc schema input: postgres types for postgres/mssql
// (MSSQL types are mapped via mssqlTypeToPostgres), native types for sqlite.
func tableDDL(ti TableInfo, driver string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", ti.Name))
	for i, c := range ti.Columns {
		colType := c.DBType
		if driver == "mssql" {
			colType = mssqlTypeToPostgres(colType)
		}
		parts := []string{fmt.Sprintf("    %s %s", c.Name, colType)}
		if !c.Nullable {
			parts = append(parts, "NOT NULL")
		}
		if c.IsPrimaryKey {
			parts = append(parts, "PRIMARY KEY")
		}
		comma := ","
		if i == len(ti.Columns)-1 {
			comma = ""
		}
		b.WriteString(strings.Join(parts, " ") + comma + "\n")
	}
	b.WriteString(");\n")
	return b.String()
}

// mssqlTypeToPostgres maps a SQL Server column data type to the equivalent
// postgres type used in the sqlc schema input for mssql projects. The mapping
// only needs to be parseable by sqlc and type-inferable to the desired Go
// types; it is not executed against a database.
func mssqlTypeToPostgres(dbType string) string {
	switch strings.ToLower(dbType) {
	case "int", "smallint", "tinyint":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "bit":
		return "BOOLEAN"
	case "nvarchar", "varchar", "nchar", "char", "text", "ntext", "xml", "uniqueidentifier":
		return "TEXT"
	case "datetime", "datetime2", "smalldatetime", "date", "time", "datetimeoffset":
		return "TIMESTAMP"
	case "decimal", "numeric", "money", "smallmoney", "real", "float":
		return "DOUBLE PRECISION"
	case "varbinary", "binary", "image":
		return "BYTEA"
	default:
		return "TEXT"
	}
}

// pkGoType returns the Go type sqlc generates for the primary key column of a
// table, matching sqlc's postgres engine mapping for the schema type emitted
// by tableDDL (int32 for INTEGER, int16 for SMALLINT, int64 for BIGINT;
// sqlite INTEGER always maps to int64). When the table declares no primary
// key, the conventional "id" column (findPKColumn's fallback) is used so the
// type of the column the generated app keys routes on is still inferred
// correctly. Returns "" when no matching column is found.
func pkGoType(ti TableInfo, driver string) string {
	pk := findPKColumn(ti)
	for _, c := range ti.Columns {
		if !strings.EqualFold(c.Name, pk) {
			continue
		}
		if driver == "sqlite" {
			return "int64"
		}
		switch strings.ToLower(c.DBType) {
		case "bigint":
			return "int64"
		case "smallint":
			return "int16"
		default:
			return "int32"
		}
	}
	return ""
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
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, fk.ForeignColumn, fk.Column))
		}
		if driver != "mssql" {
			b.WriteString(fmt.Sprintf("\nORDER BY t.%s DESC", pk))
		}
		b.WriteString(";\n\n")

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
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, fk.ForeignColumn, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nWHERE t.%s = %s;\n\n", pk, placeholder(1, driver)))

		// Create
		var insertCols []string
		for _, c := range ti.Columns {
			if c.IsPrimaryKey && driver != "sqlite" {
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
		if driver != "sqlite" {
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
		if driver != "sqlite" {
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
			// The table already has its own List query when it is a user-visible
			// resource; generating a second query with the same name would make
			// sqlc fail with a duplicate query name. Its List query is usable
			// directly as the options source.
			if _, exists := queries[foreignTI.Name+".sql"]; exists {
				continue
			}
			foreignPluralName := toPascalCase(foreignTI.Name)
			labelCol := findLabelColumn(*foreignTI)

			var b strings.Builder
			b.WriteString(fmt.Sprintf("-- name: List%s :many\n", foreignPluralName))
			if driver == "mssql" {
				b.WriteString(fmt.Sprintf("SELECT %s, %s FROM %s;\n\n",
					foreignPKColumn(*foreignTI), labelCol, foreignTI.Name))
			} else {
				b.WriteString(fmt.Sprintf("SELECT %s, %s FROM %s ORDER BY %s;\n\n",
					foreignPKColumn(*foreignTI), labelCol, foreignTI.Name, labelCol))
			}

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
func cmdInitFromDB(configPath, outDir, dsn, adminPassword string, force bool) error {
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

	adminPass := adminPassword
	if adminPass == "" {
		adminPass = randomPassword()
	}
	inserted, err := insertAdminUser(db, driver, adminPass)
	if err != nil {
		return fmt.Errorf("inserting admin user: %w", err)
	}

	if !hasRoles || !hasUsers {
		fmt.Println("Created auth tables (users, roles) and seeded admin user.")
		if inserted {
			fmt.Printf("Admin login: admin@admin.test / %s\n", adminPass)
		}
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

	schemaSQL := generateSchemaSQL(tables, hasRoles, hasUsers, driver)
	if err := os.WriteFile(filepath.Join(sqlDir, "migrations", "schema.sql"), []byte(schemaSQL), 0644); err != nil {
		return fmt.Errorf("writing schema.sql: %w", err)
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
