# MSSQL Support Plan

Status: implemented & verified against a live MSSQL instance (connection details in local-only `mssql.txt`, gitignored)
Goal: add `mssql` as a supported database driver for `go-fila init --db`, config `connections.default.driver`, and the generated admin app.

## Context / findings

- `sqlc` does not support MSSQL (engines: `postgresql`, `mysql`, `sqlite`; SQL Server is on the roadmap, Go T-SQL parser `teesql` not ready).
- Approach: keep `sqlc` on the `postgresql` engine parsing postgres-flavored SQL, producing DB-agnostic Go models. The same postgres-flavored query text is executed at runtime against MSSQL.
- Key constraint: sqlc's postgres engine can only parse `$N` placeholders, and sqlc `GetX` queries are executed at runtime (detail + update-populate). `github.com/microsoft/go-mssqldb` (latest v1.10.0) supports this via the **`mssql` driver name**, whose loose placeholder parser accepts `?`, `:n`, `$nnn`. The `sqlserver` driver name does strict `@name` only and would break the sqlc queries.
- Decisions (confirmed):
  1. Keep sqlc in the loop; use the `mssql` driver name + `$N` placeholders everywhere.
  2. Emit the full postgres-dialect `schema.sql` (all introspected tables) for **all drivers** — sqlc cannot type-infer user tables today (only auth tables are emitted), which breaks the generated app.
  3. End-to-end verification against a live MSSQL instance.

## Runtime SQL that must become MSSQL-aware

| Today (postgres branch) | MSSQL equivalent |
|---|---|
| `LIMIT $1 OFFSET $2` | `OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY` (requires ORDER BY; add `ORDER BY (SELECT NULL)` fallback) |
| `ILIKE $N` | `LIKE $N` |
| `SELECT 1 FROM users LIMIT 1` (main.go sanity check) | `SELECT TOP 1 1 FROM users` |
| `FROM (ListX) AS _opt` with `ORDER BY` | MSSQL forbids ORDER BY in a derived table → options ListX queries must drop ORDER BY |
| `INSERT/UPDATE ... $N`, `DELETE $1`, auth login `$1` | already fine via `mssql` loose parsing (no change) |
| sqlc `GetX` (`$1`, LEFT JOIN) | fine via `mssql` loose parsing |
| `CreateX`/`UpdateX` with `RETURNING *` | parse-only (never executed at runtime) — fine |

## Changes

### 1. `cmd/go-fila/introspect.go`

- `detectDriver`: `sqlserver://` / `mssql://` prefix → `"mssql"`.
- `openDB`: `sql.Open("mssql", dsn)` + blank-import `github.com/microsoft/go-mssqldb`.
- `introspectMSSQL` (new): tables/columns/PK via `INFORMATION_SCHEMA` filtered by `table_schema = SCHEMA_NAME()`; FKs via `sys.foreign_keys` / `sys.foreign_key_columns`. Placeholders stay `$1` (loose parsing).
- **Identity-column fallback**: when a table has no declared PRIMARY KEY (common on MSSQL line-of-business schemas), `sys.columns.is_identity = 1` marks that column `IsPrimaryKey=true` so routes key on it and INSERT/UPDATE omit it. Declared PK wins when present.
- `mapDBTypeToFieldType`: add `bit`→boolean, `datetime2`/`smalldatetime`/`datetimeoffset`/`time`→datetime, `money`/`smallmoney`→float, `varbinary`/`binary`/`image`→file, `uniqueidentifier`/`xml`→string (nvarchar/nchar/ntext already match).
- `ensureAuthTables`: MSSQL branch — `INT IDENTITY(1,1) PRIMARY KEY`, `NVARCHAR`, `DATETIME2 DEFAULT SYSUTCDATETIME()`; multi-row `INSERT` unchanged.
- `generateSchemaSQL` → full schema (all drivers): emit postgres-dialect DDL for every introspected table (MSSQL types via new `mssqlTypeToPostgres`: int→`INTEGER`, bigint→`BIGINT`, bit→`BOOLEAN`, nvarchar/varchar→`TEXT`, datetime→`TIMESTAMP`, decimal/money→`DOUBLE PRECISION`, varbinary→`BYTEA`) plus auth tables when created. sqlite stays sqlite-dialect, postgres uses native `data_type`. `cmdInitFromDB` always writes `schema.sql`.
- `generateQueries`: branch on `driver != "sqlite"` (mssql gets postgres dialect: `$N`, `RETURNING *`); omit `ORDER BY` when `driver == "mssql"` (derived-table rule).
- `writeResourceYAML`: emits `table:` when pluralized name differs from real table, `id_column:` when the row-key column isn't literally `id`, `id_type:` when PK Go type differs from driver default.
- `insertAdminUser`: unchanged (`$N` works via loose parsing).

### 2. `internal/generator/` + `internal/types/`

- `generator.go`: add `isMSSQL()` (`mssql`/`sqlserver`); `likeOp()` returns `LIKE` for sqlite and mssql. `idGoTypeForResource(r)` honors per-resource `id_type` (bigint → `int64`). `snakeToPascal` lowercases the whole input first to match sqlc (see AGENTS.md).
- `types/resource.go`: new `Table` (`table:`), `IDColumn` (`id_column:`), existing `IDType` (`id_type:`).
- `handler.go`: add MSSQL branch to list + card query cores — `LIKE $N` search, `OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY` pagination, `ORDER BY (SELECT NULL)` fallback when no sort. **Count query builds its own `$1..N` clauses** and passes `args[2:]` (go-mssqldb validates arg count against highest `$N`). `tableName(r)`/`idColumn(r)` honor the overrides; detail/update/actions/delete cast ids with `idGoTypeForResource(r)`.
- `templ.go`: View/Edit/action/delete links key off `idColumn(r)` instead of hardcoded `item["id"]`.
- `main.go`: mssql → driver name `"mssql"`, `_ "github.com/microsoft/go-mssqldb"`; sanity check `SELECT TOP 1 1 FROM users` for mssql (else `LIMIT 1`).
- `mod.go`: add `github.com/microsoft/go-mssqldb v1.10.0` when `isMSSQL()`.

### 3. Other

- Root `go.mod`: add go-mssqldb v1.10.0 (for introspect.go).
- `cmd/go-fila/main.go`: update `--db` help to mention `sqlserver://...`.
- AGENTS.md / README: driver matrix row for mssql; document the `mssql`-driver-name/loose-`$N` decision, the derived-table ORDER BY rule, and full-schema emission.

## Verification

1. `go build ./...`, `go vet ./...`, `go test ./...` — all pass.
2. `go-fila init --db "sqlserver://user:pass@host:port?database=..."` → all user tables + users/roles; YAML emits `table:`/`id_column: ID`/`id_type: int64` for identity-keyed tables; queries + full postgres-dialect `schema.sql` written.
3. `go-fila generate` → `sqlc generate`, `go tool templ generate`, `go build` — all succeed.
4. Ran the generated binary against the live DB (ports 9311–9315): login 302, list 200, **search 200** (after count-clause fix), detail/edit GET 200, create POST 302 (row created), update POST 302 (row updated). Delete returns 404 (no `form.delete` in introspection output — matches demo behavior).

## Known limitations

- MSSQL identifiers (reserved words, case, spaces) are unquoted in generated SQL — same limitation postgres already has.
- `mssql` driver loose `$N` parsing is discouraged by go-mssqldb docs; safe for generated SQL (no `$N` in string literals).
- `schema.sql` is postgres dialect for mssql projects (sqlc input); the live DB schema is never modified except auth tables (created via T-SQL).
- sqlite `init --db` Create/Update still broken on HEAD (pre-existing `:one` without RETURNING — unrelated to MSSQL).
