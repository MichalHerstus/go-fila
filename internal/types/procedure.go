// procedure.go
//
// YAML-tagged struct describing a named SQLite "stored procedure": a batch of
// SQL statements stored in a table and executed by the sqlite engine inside
// one transaction at call time. SQLite has no real stored procedures, so the
// body lives in the sql_procedures table (seeded from this YAML block) and the
// generated app splits it into statements before executing. The block is
// sqlite-only semantics: on postgres/mssql the `procedures:` block is ignored
// (real procedures come from user DDL).
package types

// Procedure declares one SQLite SQL-batch procedure. Name is the identifier
// referenced by existing `proc:` fields on actions and hooks — the same config
// works on all three drivers with three execution strategies (`CALL` /
// `EXEC` / procs.Exec).
type Procedure struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	SQL         string `yaml:"sql"`
}
