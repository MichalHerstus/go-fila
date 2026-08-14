// schema.go
//
// Captures the database structure introspected by `init --db` as a `schema:`
// block inside yaga.yaml. Since D11 the schema block is the sole source of
// schema truth for the generator: tables, views, columns (with their yaga
// field type), primary keys and foreign keys (each FK target carrying the
// label column used for option SQL and list/detail label joins). Generate and
// build run entirely offline against this block — no sqlc, no DB connection
// at build time.
package types

// Schema is the root of the `schema:` YAML block, a list of tables.
type Schema struct {
	Tables []SchemaTable `yaml:"tables"`
}

// SchemaTable describes one table or view: its name, the resolved row-key
// column (pk), whether it is a read-only view, its columns and its foreign
// keys.
type SchemaTable struct {
	Name        string         `yaml:"name"`
	PK          string         `yaml:"pk"`
	View        bool           `yaml:"view"`
	Columns     []SchemaColumn `yaml:"columns"`
	ForeignKeys []SchemaFK     `yaml:"foreign_keys"`
}

// SchemaColumn is one column of a table, carrying its yaga field type
// (mapDBTypeToFieldType at introspection time, e.g. "integer", "string").
type SchemaColumn struct {
	Name       string `yaml:"name"`
	Type       string `yaml:"type"`
	PrimaryKey bool   `yaml:"primary_key"`
}

// SchemaFK describes a foreign key: the local column referencing
// ForeignTable.ForeignColumn, plus the foreign table's label column used to
// render the FK relation in lists/details and to build option SQL.
type SchemaFK struct {
	Column        string `yaml:"column"`
	ForeignTable  string `yaml:"foreign_table"`
	ForeignColumn string `yaml:"foreign_column"`
	Label         string `yaml:"label"`
}
