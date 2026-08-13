# yaga generated dashboard (Agent Guide)

This project is a **generated** admin dashboard produced by [yaga](https://github.com/MichalHerstus/yaga)
from two sources of truth:

1. `yaga.yaml` (repo root) — the declarative spec: panel, resources, pages, navigation, auth, actions, hooks, policies.
2. `admin/sql/` — `migrations/schema.sql` (tables) and `queries/*.sql` (named SQLC queries).

Everything under `admin/` is a build artifact. This guide exists so an AI agent
modifies this project correctly: edit the YAML + SQL, regenerate, rebuild — and
only hand-write the two Go files that yaga deliberately leaves to the user.

## THE GOLDEN RULE

**Never hand-edit generated Go code.** The generated files are rewritten on every
`yaga generate` run; manual edits are silently lost and cause confusing drift.

There are exactly **two exceptions** — the files yaga emits as *stubs or seams*
for the user to fill in:

- `admin/internal/hooks/hooks.go` — the **fn hook** stubs. yaga writes
  `func <Name>(ctx context.Context, db *sql.DB, s Scope) error { return nil }`
  for every `fn:` hook declared in the YAML. Implementing those bodies is your
  job (see below). Signature: `Scope{ID int64, Table, Action string, Values map[string]interface{}}`.
- `admin/internal/panel/resources/<res>/actions.go` — the **custom action**
  handler switch. Only reach in when an action's inline `query:`/`proc:` in the
  YAML cannot express the logic (multi-step, loops, extra args, external calls).
  Prefer the declarative path first (see "Custom actions"). Any hand-edit here
  is **lost on regeneration** — re-apply it after every `generate` or document
  it in this file.

All other generated files are **off-limits**, including:
`admin/main.go`, `admin/internal/panel/**` (handlers/router/auth),
`admin/internal/views/**` (templ), `admin/internal/data/**` (sqlc output),
`admin/internal/viewmodels/**`, `admin/internal/assets/**`, `admin/sqlc.yaml`,
`admin/tailwind.config.js`, `admin/Makefile`, `admin/static/**`.
(There is no `package.json` — the dashboard builds with no npm/node.)

## Build & regeneration

```sh
# from the repo root (~/dev/pokus-fila)
./yaga generate --config yaga.yaml --out admin --force   # regenerate
cd admin
make                                                           # tailwind + sqlc + templ + go build
./admin --port 8080                                            # run
```

- `generate` runs `sqlc generate` + tailwind itself but their failure is
  **non-fatal** — re-run `make` afterwards; it redoes them in the right order.
  Tailwind needs the standalone binary: `make get-tailwind` downloads it to
  `.tools/`, and `make css` picks it up automatically.
- `make` targets: `build` (default), `css`, `sqlc`, `templ`, `tidy`,
  `get-tailwind`, `run`, `package`, `clean`.
- Sanity-check a config without building: `./yaga validate --verbose`
  (parses YAML; does NOT verify SQL references — that check is manual, see below).

## How the YAML drives the code (the dependency graph)

Read every edit against this chain — a broken link fails the build or renders wrong data:

```
yaga.yaml ──► resource name ──► Go package / URL segment
      │
      ├── list.query / count_query ──► -- name: <QueryName> in admin/sql/queries/*.sql ──► sqlc ──► admin/internal/data/<res>.sql.go
      ├── detail.query ───────────────────────────────┘
      ├── form.create.query / update.query ───────────────┘
      ├── options_query (relation/select fields) ──► SELECT id,label FROM (<query body>) AS _opt  (resolved at GENERATION time)
      ├── FK *_label list columns ──► LEFT JOIN reconstructed from a matching relation form field
      ├── actions ──► switch in actions.go (inline SQL / proc)
      ├── hooks ──► stubs in internal/hooks/hooks.go + inlined db.ExecContext
      └── pages/widgets ──► raw SQL executed at REQUEST time (tables/columns must exist in schema.sql + db)
```

### Naming rules (must match exactly)

- Resource `OrderLine` → Go package/dir `orderline`, URL `/admin/orderline`.
  PascalCase in YAML is lowercased verbatim (`User`→`user`, `OrderLine`→`orderline`).
- `snakeToPascal` = sqlc's field naming: lowercase all, split only on `_`, `id`→`ID`.
  So YAML column `role_id` → Go field `RoleID`, `customer_name` → `CustomerName`.
- Query names in YAML (`query:`, `count_query:`, `populate_query:`, `options_query:`)
  must match a `-- name: X` block **verbatim** in an `admin/sql/queries/*.sql` file.
  Example files here: `products.sql`, `orders.sql`, `customers.sql`, `users.sql`,
  `roles.sql`, `orderlines.sql`.
- `table:`/`id_column:`/`id_type:` override conventions only when the DB differs
  (not the case here — sqlite, table name = lowercased resource, PK = `id`, `int64`).

### Drivers: postgres / sqlite (current) / mssql

The driver comes from the first `connections:.*.driver` value in the YAML.
Acceptable values: `postgres` (default when the key is absent), `sqlite`/`sqlite3`,
`mssql`/`sqlserver`. **This project currently uses `sqlite`**, but the YAML,
the SQL files and the generated code must stay driver-correct in case it changes.
When you flip the driver, re-run `generate` (it rewrites `sql.Open`, `sqlc.yaml`,
handlers, pagination, sanity check) AND rewrite every query/schema file for the
new dialect — the generator does not translate SQL for you.

| Concern | postgres | sqlite (current) | mssql |
|---|---|---|---|
| YAML `driver:` | `postgres` | `sqlite`/`sqlite3` | `mssql`/`sqlserver` |
| sqlc.yaml `engine` | `postgresql` | `sqlite` | `postgresql` |
| `sql.Open` driver (main.go) | `pgx` | `sqlite3` | `mssql` |
| bind placeholders | `$1..$N` | `?` | `$1..$N` (loose `$N`→`@pN`) |
| LIKE operator | `ILIKE` | `LIKE` | `LIKE` (case-insensitive collation) |
| pagination | `LIMIT $1 OFFSET $2` | `LIMIT ? OFFSET ?` | `OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY` (REQUIRES an ORDER BY) |
| sqlc id type | `int32` | `int64` | `int32` (bigint PK → `id_type: int64`) |
| create-hook id capture | `RETURNING <id>` | `RETURNING <id>` | `OUTPUT INSERTED.<id>` |
| stored procedures | `CALL name($1)` | not supported | `EXEC name $1` |
| startup sanity check | `SELECT 1 FROM {table} LIMIT 1` | same | `SELECT TOP 1 1 FROM {table}` |
| go.mod extra | `github.com/jackc/pgx/v5` | `github.com/mattn/go-sqlite3` | `github.com/microsoft/go-mssqldb` |

#### Postgres rules
- Write `admin/sql/queries/*.sql` and `schema.sql` in **postgres dialect**: `$N`
  placeholders, `ILIKE`, `LIMIT/OFFSET`, `SERIAL`/`TIMESTAMPTZ` DDL.
- `proc:` hooks/actions → `CALL name($1)`.
- Bind args are ordered with the LIMIT/OFFSET params first, then the search
  clauses numbered `$3..`.

#### SQLite rules (current project)
- Bind placeholders are `?` (positional, SQL-text order — search args before `LIMIT ? OFFSET ?`).
- LIKE operator is `LIKE` (not ILIKE). Pagination `LIMIT ? OFFSET ?`.
- sqlc ids are `int64`. sqlc engine in `admin/sqlc.yaml` is `sqlite`.
- No stored procedures: `proc:` hooks/actions are **ignored** on sqlite — use `query:`/`sql:`.

#### MSSQL rules
- **PascalCase columns** (`CeleJmeno`, `ZamestnanecID`). sqlc lowercases + splits
  only on `_`, so a column like `ZamestnanecID` maps to Go field `Zamestnanecid`;
  `role_id` still maps to `RoleID`. When introspection detects a non-`id` key it
  emits `id_column: ID`, and bigint PKs emit `id_type: int64`.
- sqlc runs against a **postgres-dialect `schema.sql`** (engine stays
  `postgresql`); that file is never executed against the DB.
- `$N` placeholders; go-mssqldb validates arg count against the **highest** `$N`,
  so the list/card COUNT query numbers its clauses separately from the data query.
- Pagination `OFFSET/FETCH` **requires an ORDER BY**; when no sort is configured
  the generator emits `ORDER BY (SELECT NULL)`. ORDER BY is omitted from
  derived-table `options_query`/list queries entirely.
- No `RETURNING` — create-hook id capture uses `OUTPUT INSERTED.<id>`.
- `proc:` hooks/actions → `EXEC name $1` (bound `$1` passes positionally to the proc).
- Startup sanity check is `SELECT TOP 1 1 FROM {table}`.

### Custom actions

Declared in YAML, per resource:

```yaml
actions:
  - name: complete            # URL /admin/order/{id}/action/complete
    label: Complete
    icon: check
    color: success
    requires_confirmation: true
    bulk: false
    query: UPDATE orders SET status = 'completed' WHERE id = $1   # $1 = record id (works on sqlite)
    hooks: null
```

- `query:` is inline SQL bound to `$1` = the record id. This is the **preferred** way
  to add an action — no Go code touched. Keep the SQL in the **current driver's dialect**
  (`$N` for postgres/mssql, `?` for sqlite).
- `bulk: true` runs the same SQL once per selected id from `bulk.go` (no hooks run).
- `proc:` is postgres/mssql only — on this sqlite project use `query:`.
  Postgres emits `CALL name($1)`, mssql emits `EXEC name $1`.
- Editing the generated `actions.go` case is the last resort (see Golden Rule).

### Hooks

Attach to `form.create/update/delete.hooks.before/after` or an action's `hooks`.

```yaml
form:
  create:
    hooks:
      before:
        - name: validate_domain
          fn: ValidateUserDomain      # stub → admin/internal/hooks/hooks.go
      after:
        - name: notify
          sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"
```

- `fn:` → a stub named `func <Fn>(ctx context.Context, db *sql.DB, s Scope) error`
  in `admin/internal/hooks/hooks.go`. **Implement the body there** — it is the
  one generated file you own. `Scope.ID` = 0 before-create, new row id after-create,
  path id otherwise.
- `sql:` → inlined `db.ExecContext(..., "<sql>", scope.ID)` at generate time;
  keep the SQL well-formed for the **current driver's dialect**.
- Returning a non-nil error aborts the request with HTTP 500.
- Adding a hook requires re-running `generate` (stub emission) and `make`.
- On postgres/mssql the create handler switches to a `RETURNING`/`OUTPUT INSERTED`
  id capture so after-create hooks see the new row id (`Scope.ID`).

### Relations / FK labels (`options_query`)

- A `select`/`relation` form field with `options_query: ListRoles` renders a modal
  record picker; generation reads `admin/sql/queries/*.sql`, wraps the query body
  as `SELECT options_value, options_label FROM (<body>) AS _opt`. The query must
  select an id-ish first column + a label column (e.g. `-- name: ListRoles :many\nSELECT id, name FROM roles ORDER BY name;`).
- List views show FK targets via `<fk>_label` columns (e.g. `customer_name`).
  The generated handler reconstructs the LEFT JOIN from the matching relation
  form field — if you rename a column or drop the relation field, list queries
  break with `column "<fk>_label" does not exist`.

### Pages & widgets

`pages:` → Dashboard (`/Dashboard`, default) and Reports (`/Reports`).
Widgets (`stat`, `stats_grid`, `chart`, `table`, `list`, `html`) run **raw SQL at
request time** — no sqlc, no generation-time checks. Verify every table/column
you reference exists in `admin/sql/migrations/schema.sql` and in the live DB
(sqlite: `admin/data/admin.db`; postgres/mssql: the configured server). The widget
SQL must also be in the current driver's dialect (e.g. `strftime` is sqlite-only;
postgres uses `to_char`/`date_trunc`). `generate` does not re-seed the DB.

### SQL file editing rules

- `admin/sql/queries/*.sql` and `admin/sql/migrations/schema.sql` are YOUR
  source (they are not copied/overwritten by `generate` in this layout — the
  config dir `~` has no `sql/`).
- Format: `-- name: <QueryName> :many|:one|:exec` followed by the body; sqlc
  compiles them into `internal/data`. Keep bodies valid for the **current driver's
  dialect** — sqlite (`?` binds, `LIKE`), postgres (`$N`, `ILIKE`), mssql (`$N`,
  `LIKE`, OFFSET/FETCH + ORDER BY).
- Adding/changing a query referenced from YAML → re-run `generate` (rewrites
  handlers + options loaders) and `make sqlc`.
- schema.sql is never executed by the app — the DB schema already exists in
  the live DB (`admin/data/admin.db` for sqlite; a real server for postgres/mssql).
  Change schema.sql AND apply the same DDL to the live DB (sqlite:
  `sqlite3 admin/data/admin.db`; postgres: psql; mssql: sqlcmd).

## Workflow checklist (any change)

1. Edit `yaga.yaml` (and/or `admin/sql/**`).
2. Cross-check every referenced name: query names ↔ `-- name:` blocks, FK label
   columns ↔ relation fields, page URLs ↔ navigation, policy roles ↔ `auth` login.
3. `./yaga validate` for YAML sanity.
4. `./yaga generate --config yaga.yaml --out admin --force`.
5. `cd admin && make` (fix sqlc/templ errors if any).
6. Run and click through the affected screens.

## Do / Don't

- DO edit `yaga.yaml` and `admin/sql/**` freely — they are the source of truth.
- DO implement `fn:` hook bodies in `admin/internal/hooks/hooks.go`.
- DON'T touch any other generated Go/templ/JS/config file.
- DON'T invent query names, columns, or URLs that don't exist — verify against the YAML + SQL first.
- DON'T expect `generate` to update the live DB — apply DDL manually if schema changes.
- DON'T mix dialects: keep every SQL file consistent with the `connections.default.driver`
  in the YAML (placeholders, LIKE/ILIKE, pagination, date functions).
