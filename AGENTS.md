# go-fila — Agent Guide

## Build

```sh
go build ./cmd/go-fila          # the project binary
go build ./...                   # skips nested admin/ modules (separate go.mod)
```

Single binary at `cmd/go-fila/main.go` — stdlib flags, no cobra/viper.

## Init → Generate flow

```sh
go-fila init            # writes go-fila.yaml + sql/{migrations,queries}/
go-fila init --demo     # same + seeds sqlite demo DB (roles/users/customers/products/orders/orderlines); login admin@demo.test / admin
go-fila init --db DSN   # introspects existing DB, generates go-fila.yaml + SQL files from discovered tables
go-fila edit            # interactive TUI editor for go-fila.yaml
go-fila generate        # generates admin/ app, runs sqlc + tailwind (non-fatal)
cd admin
make                    # builds the dashboard binary + assets
```

### AI-assisted editing (D7)

`go-fila edit --prompt "Change dashboard title to: Order management"` edits `go-fila.yaml` via OpenRouter instead of launching the TUI (non-interactive, opt-in — no `--prompt` → TUI unchanged). `--prompt` also accepts `file://PATH` (with `~/` expanded to home) to read a long/multi-line instruction from a file — resolved by `resolvePrompt` inside `editAI`. Flags live on `edit` only and are parsed by `parseEditFlags` (`cmd/go-fila/ai.go`), which scans `os.Args[2:]` independently of `parseGlobalFlags` (both skip the flags they don't know): `--apikey` (falls back to `OPENROUTER_API_KEY` env), `--model` (default `openrouter/auto`), `--dry-run` (print proposed YAML + compact `±` diff, never write). Provider is locked to OpenRouter (`openRouterBaseURL` = `https://openrouter.ai/api/v1`); the full config is sent to OpenRouter — documented in usage text, consent is supplying the key + prompt. SQL/`sql/queries` files are never touched by the AI path.

**Flow (`editAI` → `proposeEdit`):** `parser.ParseFile` → `yaml.Marshal` current → `buildEditPrompt` (system message = output contract "return ONLY the complete new YAML in a ```yaml fence, keep version"; user message = embedded `ai_spec.md` cheat-sheet (`//go:embed ai_spec.md`, kept compact so tokens stay low) + current YAML + instruction) → `openrouterChat` POST `/chat/completions` (stdlib `net/http`, `Authorization: Bearer`, `temperature: 0`, 90 s timeout, key never logged) → `extractYAMLBlock` (```yaml fence, then any fence, then whole trimmed text) → `parser.Parse` + `parser.Validate`. On parse/validate failure it retries **once**, feeding the validator error back; network/API errors are not retried and the original file stays untouched. Valid output is normalized via `yaml.Marshal` (so the written bytes always round-trip) and written with `os.WriteFile(configPath, 0644)`; the same compact `diffLines` output (LCS-based, `+`/`-` prefixed, unchanged runs collapsed to `...`) prints in both write and `--dry-run` mode. The API key and the model output never appear in the error paths (only the HTTP status/body snippet on non-200).

**Testing:** `ai_test.go` drives `editAI` against an `httptest` OpenRouter stub (`stubOpenRouter` records each decoded `chatRequest` + `Authorization` header) — happy path writes + diff, retry-on-invalid feeds back the validator error, second-invalid fails leaving the file untouched, `--dry-run` never writes, missing key/prompt → clear errors, HTTP error → original preserved.

### `init --db` — Database introspection

`go-fila init --db {connection_string}` connects to an existing database, introspects its schema (tables, columns, primary keys, foreign keys), and generates `go-fila.yaml` + SQL migration/query files from the discovered tables. Works for SQLite, Postgres and MSSQL (MSSQL DSN prefix `sqlserver://` or `mssql://`; see "MSSQL-specific gotchas" below).

**Driver detection:** DSN prefix `postgres://` or `postgresql://` → postgres; everything else (file path, `:memory:`) → sqlite. Uses `github.com/jackc/pgx/v5/stdlib` for postgres and `modernc.org/sqlite` for sqlite.

**What it does:**
1. Connects to the DB, introspects schema via `information_schema` (postgres) or `PRAGMA` (sqlite)
2. If `users`/`roles` tables are missing, creates them with default roles (admin/manager/user) and inserts an admin user (`admin@admin.test` / `admin`, bcrypt-hashed)
3. If `users`/`roles` already exist with data, respects them as-is (no admin user inserted)
4. Generates `go-fila.yaml` with a resource per table (excluding `users`/`roles`) — list/detail/form sections, FK relation fields with `options_query`
5. Generates SQLC query files with LEFT JOINs for FK label display
6. Generates `schema.sql` only when auth tables were created

**Type mapping:** `int`/`serial` → `integer`, `varchar`/`text` → `string`, `bool` → `boolean`, `timestamp`/`date` → `datetime`, `real`/`float`/`numeric` → `float`, `json`/`jsonb` → `json`, `bytea`/`blob` → `file`.

**FK handling:** Foreign keys become `relation` fields in forms with `options_query: List{ForeignTable}`. In list views, FK columns are replaced with LEFT JOINs showing the foreign table's label column (preferred: `name`, then `title`, then `label`, then first non-PK text column).

**Auth table DDL:** Postgres uses `SERIAL`/`TIMESTAMPTZ`; SQLite uses `INTEGER PRIMARY KEY AUTOINCREMENT`/`datetime('now')`.

Flags: `init --db <dsn> --config <yaml> --out <dir> --force` (short variants `-d`, `-c`, `-o`, `-f`).

### `edit` — Interactive YAML config editor

`go-fila edit` opens the YAML config in a terminal UI built with `rivo/tview` (tcell). Persistent 3-pane layout: left `tview.List` menu + right `tview.Pages` stack + title/status bars. Spec: `SPEC_yaml_editor.md` (single source for the editor).

**Keys:** Ctrl+S save (runs `parser.Validate` on a copy first; errors in a modal, file not written), Ctrl+V validate (opens the Validate screen — same as the "Validate (Ctrl+V)" nav item), Ctrl+Q/F10 quit (Save&Quit / Discard / Cancel modal when modified), Esc back (app-level `SetInputCapture` popping a `history []string` page-name stack), `a`/`d` add/delete in list editors, Enter edit. **Every button also gets a Ctrl+key shortcut from the first free letter of its label** (e.g. Ctrl+G for a "Generate missing queries" button), shown as a `(Ctrl+X)` hint in the label — `shortcuts.go` (`takeShortcut`/`addButton`/`addModalButtons`/`bindShortcuts`). Ctrl+S/Ctrl+V/Ctrl+Q are reserved (save/validate/quit). Shortcuts are scoped to the displayed screen: collected in `e.pending` while a page/modal is built, bound in `showPage`/`refreshPage`/`showModal` (`e.shortcuts[ctx]`), dispatched from `capture` via `shortcutFor` using the "modal" context while `modalOpen`. The app-level capture runs BEFORE every widget, so a registered Ctrl key fires **even while a text field is focused** (hotkeys beat text-editing combos). `Editor.Run()` returns whether a save happened (re-wired in `edit.go`).

**Architecture:** `cmd/go-fila/editor/` package. `Editor` owns `*tview.Application`, `*tview.Pages`, `*tview.List` (nav), title/status `*tview.TextView`, `cfg *types.Config`, `configPath`, `modified`, `history`. Config fields are edited **live** via `SetChangedFunc` closures (`widgets.go`: `str/num/yesno/password/long/pick`); anything touching a value calls `markModified`. Collection editors share `listSpec` (`lists.go`): `recordList` + `a/d/Enter`. `promptInput`/`namePrompt` modals add collection members; `tagsPage` toggles `[]string`; `stringMapPage` (`maps.go`) edits `map[string]string` (column/field options, query params). Sub-screens: `menu.go` (Panel/Brand/Layout/Theme, Connections, SQLC, Auth, Navigation, Pages+widgets), `resource.go` (Resources + List/Card/Detail/Form/Policies + **SQL queries**), `columns.go`, `fields.go`, `actions.go`, `hooks.go`, `sqledit.go`, `validate.go`.

**Resource "SQL queries" editor** (`sqledit.go`): the resource form has an "SQL queries" button (head "Queries") → `sqlQueriesPage(idx)` lists every SQLC query that resource references (`sqlQueriesForResource`: list/count/detail/create/update/delete/populate queries + field `options_query`, deduped/sorted, each showing its `sql/queries` file). Enter opens `sqlEditPage(name)`: a full-height `tview.TextArea` editing the query's raw SQL body. Changes are **staged** (`stageQueryEdit` → `schema.RewriteQueryBody` on the effective file text) into `Editor.pendingSQL` (abs path → new file content), marked `modified`, and **flushed by the global save** (`save()` writes staged SQL files before the YAML). `queryBase` resolves a query's file, layering `pendingSQL` over disk so editing a second query in the same file sees the first one's staged change; the sync view still reads disk (saved state). `schema.ParseQueriesForFile`/`schema.RewriteQueryBody`/`Query.RawBody` in `internal/schema/queries.go` support the round-trip (block-preserving: other query blocks in the file stay byte-identical).

**tview v0.42 gotchas** (all hit during the port):
- `Modal` has no `SetTitle`/`SetBorder` — title goes in `SetText` first line; `Form` has no `SetScrollable`.
- **`Pages.SwitchToPage`/`AddPage` never move focus** (`AddPage`'s 4th arg is `visible`, not `focus`). After any page switch you MUST call `e.app.SetFocus(e.pages)` (or `e.nav` when returning home) or focus stays trapped on the nav list and the content form is unreachable. Modals likewise need explicit focus (`showModal` helper).
- `DropDown.SetCurrentOption` fires the changed callback at construction — `pick` guards with a `first` flag or merely building a page marks the config modified.
- **Never call `app.Draw()`/`QueueUpdate` synchronously from an input capture/handler** — they block on the update queue and deadlock the event loop (found via the sim-screen tests). Set `TextView` text directly; tview redraws after each event.
- `SetScreen(tcell.Screen)` is the test seam: `run_test.go` drives the real event loop with `tcell.NewSimulationScreen` and injects keys (Ctrl+S save round-trip, Enter/Esc/Ctrl+Q nav). `e.Run()` also does `app.SetRoot` + `app.SetInputCapture`; don't `Fini()` a sim screen yourself (app.Stop does it).

**Sync screen** (`sync.go`): file-level SQL↔YAML analysis via `internal/schema` — `ParseSchema` (globs `sql/migrations/*.sql`; takes explicit paths), `ParseQueries(sql/queries)`, `CollectReferences(cfg)` (query/table/column refs from YAML incl. options_query + nested page widgets). Renders a **simple scrolling `tview.TextView` list**: schema tables with column counts, sorted SQLC query definitions, then a colored YAML-reference summary of missing queries/tables/columns and missing FK-target `List{Table}` queries (each with detail lines). Buttons (with Ctrl+letter hints via `e.addButton`): `Generate missing queries` (Ctrl+G — writes `{table}.sql` per schema table into `sql/queries`, **never overwrites** existing files), `Refresh` (Ctrl+R — re-run `analyze`), `Back` (Ctrl+B). `importResourcesFromSchema` stays out of the UI (method kept for tests). Driver comes from `schema.Driver(cfg)`. **SQL dir resolution:** `sqlc.queries_dir`/`schema_dir` are relative to the output dir where sqlc runs — `init`/`init --demo` write `sql/` into `./admin/sql`, not next to the YAML. The editor's `sqlBase()` (sqledit.go) therefore resolves them against the config dir when it has any sql tree, else `{configDir}/admin`, else the config dir; `analyze()`, `queriesDir()`, `sqlEditPage`/`sqlViewer` and `generateMissingQueries` all use it. The per-resource SQL display (list/count/detail/form/options_query bodies on the resource editor pages) and the Sync summary are complementary: the editor pages show the SQL *of one resource* live, Sync is the whole-project health check + generator.

**Validate screen** (`validate.go`): full health check over the in-memory config — `runValidation()` runs the structural validator (`parser.ValidateAll` against a YAML copy so defaults are not injected) **plus** the `analyze()` schema/queries pass, and renders one `tview.List` row per problem (red errors / yellow warnings, "No problems found" empty state) with Refresh (Ctrl+R) + Back (Ctrl+B) buttons, mirroring the Sync layout. Structural errors map to a `goTo` via `structuralGoTo` (regexes on `resources[i]`/`pages[i]`/`panel.*` in the message). Schema findings jump straight to the fix location: missing tables → the resource page (`resourceGoTo`), missing columns → `columnGoTo`/`sectionJump` (opens the exact `columnsPage`/`cardFieldsPage`/`detailFieldsPage`/`formFieldsPage` and `SetCurrentItem`s the offending row; single-value sections like `list.default_sort`/`card.kanban_field` open the parent list/card page without a row focus), missing queries → `queryGoTo` (opens the resource's SQL-queries page and highlights the query row), missing FK `List{}` queries → a warning linking to the Sync screen. Requires the schema refs to carry locations: `schema.ColumnRef{Column, Section, Index}` + `References.ColumnRefs` (kept beside the deduped `Columns` summary) recorded by `CollectReferences` for list/card/detail/form sections incl. `default_sort` (leading `-` stripped by `sortColumn`), `card.kanban_field` and `card.searchable`; `internal/parser/validator.go` was split into `ValidateAll(cfg) []error` (collects every problem, defaults still applied) + a thin `Validate` wrapper returning the first error so `parser.Parse`/save keep their behaviour.

**Preview screen** (`preview.go`): ASCII-frame mock of the dashboard (topbar + sidebar from `cfg.Navigation` + per-page widget boxes) and per-resource list mock. The grid chrome (`│ ├ ┬ ┤ ┌ ┐ └ ┘ ─`) is drawn in light blue (`[lightblue]`) while the cell text is white (`[white]`), and every row is padded to the exact same total width (`previewWidth`=78) via `padVisual` (tag-aware: `tview.TaggedStringWidth`) — content row widths are `previewSideWidth`=26 / `previewContentWidth`=49 so the chrome rows (top/bottom borders, column separator) all add up to `previewWidth`. `colorStable` rewrites full color resets (`[-:-:-]`/`[:]`) from content into attribute-only `[::-]` so neither grid nor text color survives emphasis tags intact. No DB, no generated app.

The generated `admin/` contains a `Makefile` (written by `generateMakefile()` in `makefile.go`). Its default `build` target runs every step needed to produce the dashboard binary, in order: `npm install` → `npm run build:css` → `sqlc generate` → `go mod tidy` → `go tool templ generate` → `go build -o <binary> .` (binary name = `--out` basename). Individual steps are also exposed as `deps`, `css`, `sqlc`, `templ`, `tidy` targets, plus `run` (build + serve), `package` (bundle into a release tar.gz) and `clean`.

Equivalent manual steps:

```sh
cd admin
npm install && npm run build:css   # build tailwind
sqlc generate                       # retry if it failed during generate
go tool templ generate              # compile .templ -> *_templ.go (required before go build)
go mod tidy && go build -o admin .
```

`sqlc generate` and `npx tailwindcss` failures are **non-fatal**. The user re-runs them manually. `templ generate` is also never run by the generator — only `.templ` sources are emitted, so the build fails until you run it. The generated `go.mod` declares `tool github.com/a-h/templ/cmd/templ`, so `go tool templ generate` resolves templ through the Go toolchain (Go 1.24+) without a manual templ install.

Flags: `generate --config <yaml> --out <dir> --force --verbose` (short variants `-c`, `-o`, `-f`, `-v`). `--out` basename becomes the module name.

## Driver support (postgres default, sqlite opt-in, mssql opt-in)

Driver comes from the first `connections:.*.driver` value (default `"postgres"`); `isSQLite()` accepts `sqlite`/`sqlite3`; `isMSSQL()` accepts `mssql`/`sqlserver`. Per-driver differences the generator must handle:

| Concern | postgres | sqlite | mssql |
|---|---|---|---|
| sqlc.yaml `engine` | `postgresql` | `sqlite` | `postgresql` (postgres-dialect schema.sql is the sqlc input) |
| `sql.Open` driver | `pgx` + `_ "github.com/jackc/pgx/v5/stdlib"` | `sqlite3` + `github.com/mattn/go-sqlite3` | `mssql` + `_ "github.com/microsoft/go-mssqldb"` |
| go.mod | adds `github.com/jackc/pgx/v5 v5.10.0` | adds `github.com/mattn/go-sqlite3 v1.14.24` | adds `github.com/microsoft/go-mssqldb v1.10.0` |
| LIKE operator | `ILIKE` | `LIKE` | `LIKE` (case-insensitive default collation) |
| bind placeholders | `$N` | `?` (positional, SQL-text order) | `$N` (go-mssqldb loose mode maps `$N`→`@pN`) |
| sqlc id type | `int32` | `int64` | `int32` (unless `id_type` overrides; bigint → `int64`) |
| pagination | `LIMIT $1 OFFSET $2` | `LIMIT ? OFFSET ?` | `OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY` (REQUIRES an ORDER BY; no ORDER BY → emit `ORDER BY (SELECT NULL)`) |

Helpers in `generator.go`: `driver()`, `isSQLite()`, `isMSSQL()`, `placeholder(n)`, `likeOp()`, `idGoType()`, `idGoTypeForResource(r)` (honors `id_type`), and `tableName(r)`/`idColumn(r)` in `handler.go` (honor `table`/`id_column` overrides). **`placeholder()` is still unused** — create/update/delete handlers hardcode `$N` (works on sqlite since mattn binds positionally, and on mssql via loose `$N` parsing). Only the list/card handlers are driver-aware.

### MSSQL-specific gotchas

- **`init --db` DSN**: prefix `sqlserver://` or `mssql://` → driver `mssql`. Introspection uses INFORMATION_SCHEMA + `sys.foreign_keys`, and `sys.columns.is_identity` for tables with no declared PRIMARY KEY (common on MSSQL line-of-business schemas — identity `ID` columns act as the key; they are marked `IsPrimaryKey` so routes key on them and INSERT/UPDATE omit them).
- **sqlc must see postgres-dialect schema.sql**: `generateSchemaSQL()` now emits full DDL for ALL introspected tables (postgres dialect for postgres/mssql, sqlite dialect for sqlite). This is what lets sqlc infer types on mssql projects (and fixed pre-existing type inference for user tables on postgres). Never executed against the DB.
- **`RETURNING` is emitted for mssql Create/Update** (driver != "sqlite") — the sqlc engine is `postgresql`, so this is required; the generated handlers use raw SQL at runtime anyway.
- **Postgres/MSSQL list/card pagination count comes from a windowed `COUNT(*) OVER()`**: the list/card data query emits `SELECT {cols}, COUNT(*) OVER() AS _total FROM {table} …` and scans `_total` into `total` per row — a single round trip, no separate COUNT query, so the old `countClauses`/$N-renumbering hack is gone. When the page is empty (rows beyond the last, or a search matching nothing) `totalSet` stays false and the handler falls back to `total = page*perPage` so `totalPages` renders as the current page instead of 0. mssql still needs the `ORDER BY (SELECT NULL)` fallback (see next bullet) and go-mssqldb still validates arg count against the HIGHEST `$N`, which is fine because there is only one query now.
- **ORDER BY is omitted from generated list/options queries for mssql**: a derived table (the `options_query` wrapper) cannot have ORDER BY without TOP/OFFSET/FOR XML, and `TOP` cannot combine with `OFFSET` — so mssql list queries get `ORDER BY (SELECT NULL)` as a fallback only when no sort is set.
- **MSSQL column names are PascalCase** (`CeleJmeno`, `ZamestnanecID`). sqlc lowercases the whole identifier and only splits on `_`, so go-fila's `snakeToPascal` lowercases input first: `CeleJmeno`→`Celejmeno`, `ZamestnanecID`→`Zamestnanecid`, `role_id`→`RoleID` (still). Row maps are keyed by the raw selected column name, so introspection emits `id_column: ID` when the key column isn't literally `id`.
- **`table:` / `id_type:` / `id_column:` overrides** are emitted by introspection when the convention doesn't match the real schema (e.g. resource `Zamestnanec` → `table: Zamestnanec`; bigint PK → `id_type: int64`; `ID` column → `id_column: ID`). The generator's `tableName()`/`idColumn()`/`idGoTypeForResource()` fall back to the old conventions when the fields are absent.
- Sanity check in generated main.go for mssql is `SELECT TOP 1 1 FROM {table}` (`TOP 1` replaces `LIMIT 1`).

### Generated main.go: DB sanity check runs BEFORE binding the port
`generateMain()` (`main.go`) emits `sql.Open` → `db.Ping()` → **sanity query against `{auth.table}`** (mssql: `SELECT TOP 1 1 FROM …`; others: `SELECT 1 FROM … LIMIT 1`; `sql.ErrNoRows` treated as OK) → only then `net.Listen` + `srv.Serve`. Rationale: mattn/go-sqlite3 silently **creates an empty DB file** when the file is missing, so `db.Ping()` succeeds against a "not found" database and the dashboard would otherwise bind the port and run broken (`no such table`) while holding it — a restart then hits `address already in use`. The sanity query makes a missing/uninitialized DB a fatal startup error **before** the port is bound. The listen port is resolved as `--port` flag (`-p` alias) → `ADDR` env → `:8080` (`flag.Int("port", 0, ...)` + `flag.IntVar(port, "p", 0, ...)`; stdlib `flag` accepts both `--port 9090` and `-port 9090`); the emitted `Makefile` `run` target passes `--port $(PORT)` (`PORT ?= 8080`). Request logging is controlled by `--log` (`-l` alias), values `full` (default, chi's `middleware.Logger`, logs every request) or `err` (only requests that produced an error response, status >= 400) — the flag value is threaded through `NewRouter(db, logLevel)` and selects `middleware.Logger` vs the generated `errorOnlyLogger` (a `middleware.NewWrapResponseWriter` wrapper that `log.Printf`s only when `Status() >= 400`); the `Makefile` `run` target passes `--log $(LOG)` (`LOG ?= full`). `--help`/`-h` prints the command line syntax + flag meanings via `flag.Usage` (custom `flag.PrintDefaults` wrapper) and exits 0 BEFORE any DB or session work. **The generated `auth/session.go` must NOT use a package `init()`** — its `Init()` is called explicitly by `main()` right after the help check (before `sql.Open`), so `-h/--help` runs clean without the `SESSION_SECRET` warning/fail-fast, while production fail-fast still happens before the port binds (`GetSession` also lazily calls `Init()` if `Store` is nil). Generated server also does graceful shutdown on SIGINT/SIGTERM (`signal.NotifyContext` → `srv.Shutdown`) and logs a `is another dashboard instance already running?` hint on bind failure. Keep the bind AFTER the DB checks — ordering is what prevents a broken DB from occupying the port.

### sqlite list handler arg order (critical)
mattn binds `?` args positionally in SQL-text order, so sqlite branch appends **search args first, then `LIMIT ? OFFSET ?`**, and uses `LIKE`. The postgres branch appends `perPage, offset` first with `ILIKE $N` + `LIMIT $1 OFFSET $2`. Mixing these up silently returns wrong rows on sqlite.

### SQL files are copied into the output
`Generate()` calls `copySQLFiles()` which copies `sql/{queries,migrations}/*` from the **config dir** into `g.OutDir/sql/`. Without this, sqlc fails (`no queries contained in paths`) and `options_query` lookups return empty. `ConfigDir` is set in `cmd/go-fila/main.go` (`filepath.Dir(configPath)`); the generator has no other knowledge of where the YAML lives.

## Generator pipeline (files in `internal/generator/`)

`generator.go` orchestrates calls to (in order):

1. `ensureDirs()` — directory layout (also creates `sql/queries` + `sql/migrations` + `internal/hooks`)
2. `generateSQLCConfig()` (`sqlc.go`) — sqlc.yaml, driver-aware engine
3. `copySQLFiles()` (`generator.go`) — copies user SQL into the out dir
4. `generateMain()` (`main.go`) — `main.go` with driver-aware `sql.Open`
5. `generateRouter()` (`router.go`) — chi routes + RBAC wiring, page handlers
6. `generateAuth()` (`auth.go`) — login/logout, session, RBAC middleware
7. `generateResource()` → per-resource handlers (`handler.go`): list, **card**, detail, create, update, **delete, action, bulk, CSV export** (hooks wired into create/update/delete/action)
8. `generatePage()` — page handlers with widget DB queries
9. `generateViews()` (`templ.go`) — all `.templ` views
10. `generateHooks()` (`hooks.go`) — shared `internal/hooks/hooks.go` (Scope + stubs)
11. `generateGoMod()` (`mod.go`, declares the templ `tool` directive), `generateMakefile()` (`makefile.go`), `generateViewModels()` (`viewmodels.go`), `generateAssets()` (`tailwind.go`)

All generation uses `os.WriteFile` + `fmt.Sprintf`, never `text/template`.

## Resource handler SQL strategy

| Operation | SQL approach |
|---|---|
| List | Raw SQL with dynamic WHERE/ORDER BY/LIMIT |
| Card | Raw SQL identical to list; `LIMIT = Rows*Columns`, grouped into kanban columns |
| Detail | SQLC function (`data.GetUser(db, int64(id))`) |
| Create POST | Raw SQL INSERT via `db.ExecContext` |
| Update GET | SQLC populate query |
| Update POST | Raw SQL UPDATE via `db.ExecContext` |
| Delete | Raw SQL DELETE via `db.ExecContext` |
| Action | Raw SQL per action name, or stored-proc call when `proc:` is set (switch dispatch) |
| Bulk | Raw SQL per bulk action name, looped once per selected id (switch dispatch) |
| CSV Export | Raw SQL SELECT + `encoding/csv` |

Create/update avoid SQLC params because `r.FormValue` returns `string` but SQLC generates typed structs (`int32` for `INTEGER`). Raw SQL `ExecContext` accepts `interface{}`. **Boolean fields are emitted as `r.FormValue(name) == "true"`** (a Go `bool`), not a raw `r.FormValue(name)` string — otherwise an unchecked checkbox posts `""` and Postgres fails with `invalid input syntax for type boolean: ""` (BUG-3). mssql/pgx/mattn accept a `bool` value directly.

Detail/update SQLC calls must cast the id to `idGoType()` — sqlite ids are `int64`, postgres `int32`. A literal `int32(id)` breaks the sqlite build.

### FK label columns need LEFT JOINs in the raw list/card/export SQL
Introspection adds a `{fk}_label` list column per foreign key (and a LEFT JOIN in the SQLC queries), but the generated **list/card/export handlers build their own raw SQL** — a bare `SELECT …, {fk}_label FROM {table}` fails with `column "{fk}_label}" does not exist`. `listSelectFrom()` in `handler.go` reconstructs the JOINs at generation time: for each view column ending in `_label` it looks up a matching relation form field (`options_query: List{Foreign}`, `options_value`, `options_label`) and emits `LEFT JOIN {ftable} f_{ftable} ON f_{ftable}.{value} = t.{fk}` with `f_{ftable}.{label} AS {fk}_label` in the select. When joins exist, real columns are qualified with the `t.` alias (search/ORDER BY too) and the single windowed `COUNT(*) OVER() AS _total` runs inside the joined data query (the count reflects the joined row set). A `_label` column with no matching relation field falls back to the unjoined behavior.

## Card view (`card` section)

Optional per-resource view at `GET /{panel}/{resource}/cards` (reachable via a "Cards" button on the list view). Fields reuse the form `Field` type. `columns` (X = cards/row) and `rows` (Y = rows/page) define `per_page = X*Y`; pagination + search mirror the list handler (driver-aware, `LIKE`/`ILIKE`). When `card.kanban_field` names a **select** field, the handler groups rows into `KanbanColumns` via `viewmodels.OptionValue(item[field])` instead of a grid — `g.Card.Kanban` flips the shortcut in `cards.templ` (grid vs. board). The grid templ hardcodes `lg:grid-cols-{Columns}`; kanban buckets are keyed by the select's option keys plus any extra row values discovered at request time.

### Field renderer for `gps`
`renderCell` maps `gps` → `@renderGPS` and the form emits a text input with `lat, lng` placeholder; `renderGPS` renders a link out to Google Maps. Registering a new field type means updating BOTH `renderCell`'s switch and the form-input switch in `templ.go`, plus `FieldTypes` in `types/field.go`.

### Modal record picker (`select`/`relation` fields with `options_query`)
A form field of type `select` or `relation` that has `options_query` set renders as a **modal record picker** instead of a plain select: a hidden input (name = field, value = option key) + a read-only display input (the option label) + a "Browse…" button. Clicking Browse opens a shared modal (emitted once per form by `pickerFooter()` when any field is a picker) listing all options as clickable rows; a search box filters rows client-side; selecting a row sets the hidden input and the display label and closes the modal. Generated pieces (all in `templ.go`): `isPickerField()` (guards rendering), `pickerMarkup()` (per-field markup + script), `pickerFooter()` (shared modal + search/close wiring), plus `viewmodels.OptionLabel()`/`ItemValue()`/`OptionsJS()` helpers in `viewmodels.go`. `ColumnDef.Picker` is set true on the form's field defs for picker fields (`formFieldDefsWithOpts` in handler.go). Gotchas:
- **templ never evaluates `{ expr }` inside `<script>` content** — it writes the text literally. The option map must reach the JS via a data attribute (`data-picker-options={ viewmodels.OptionsJS(data.Fields, "field") }`) whose value is HTML-escaped JSON the browser decodes on `dataset` read, then `JSON.parse`d in the click handler.
- **The modal element is rendered AFTER the per-field scripts** (`pickerFooter()` is appended at the end of the form). The `#record-picker-modal` lookup MUST happen inside the click handler, not at script parse time — otherwise `pickerModal` is `null` and the handler's `if (!pickerModal) return` silently no-ops (the button appears dead).
- **Each per-field script must be wrapped in an IIFE** `(function() { ... })();` — top-level `const` in classic scripts lives in the shared global lexical environment, so the second picker script's `const pickerBtn` throws `Identifier 'pickerBtn' has already been declared` and the whole script block (and the product_id/role_id picker wiring) is discarded.
- Multiple picker fields on one form (e.g. orderline: `order_id` + `product_id`) need per-field scoping — each options div and querySelector carries `data-field="<name>"`.
- `viewmodels.OptionsJS` marshals the option map with `encoding/json` (sorted keys, `{}` fallback). `viewmodels.ItemValue` returns `""` for nil so the create form (empty `Item` map) doesn't render/Submit `"<nil>"`.
- `formFieldDefsWithOpts` in handler.go sets `Picker` only for `select`/`relation` fields that resolved an `options_query` var; plain `select` fields with inline `options` keep the old `<select>` renderer.

## Critical gotchas

### Format specifier counting in Sprintf
Every `%s`/`%q`/`%d` must have a matching arg. `%%` is escaped (produce `%` in output, no arg consumed). A mismatch silently produces garbled Go source (e.g. `%!s(MISSING)` literal in emitted templ). This is especially dangerous when a templ substring is built with its own `fmt.Sprintf` and then inserted into a parent one — any `%v`/`%d`/`%s` **inside** emitted `fmt.Sprintf(...)` calls must be doubled (`%%v`) in the generator source. `buildOptionsLoader`, `preHashCode`, `fileImport` insertions, the `cardBody`/`actions`/`gridView`/`kanbanView` strings in `templ.go`, the `pickerMarkup` format-arg list in `templ.go`, and the `hookCallsStr`/`scopeValuesStr`/postCode builders in `hooks.go`/`handler.go` are common drift points.

### `snakeToPascal` matches sqlc's field naming
`snakeToPascal` lowercases the whole input, splits only on `_`, and maps the `id` segment to `ID` — this is sqlc's exact algorithm, so:
- `id` → `ID`
- `role_id` → `RoleID`
- `user_role_id` → `UserRoleID`
- `CeleJmeno` → `Celejmeno`
- `ZamestnanecID` → `Zamestnanecid`
Any other pattern would produce `Id`/`RoleId`/`CeleJmeno` (wrong). Must match sqlc's output convention (sqlc lowercases unquoted identifiers then camel-cases per underscore segment).

### `Options` field type
Both `Column.Options` and `Field.Options` are `map[string]string` (key=value, value=label), never `[]string`.

### `default_sort` prefix
YAML `"-created_at"` means sort=`"created_at"`, order=`"desc"`. Trim the `-` prefix at generation time.

### `panel.path` must start with `/`
All hrefs use `path + "/" + resourceName`. Router uses `r.Route(panelPath, ...)`.

### Resource naming
PascalCase in YAML (`User`) → lowercase for Go package, dir, URL segment (`user`).

### Page handler naming
Generated as `{CapitalPanelID}{PageName}(db)` (e.g. `AdminDashboard`). Must be exported (capitalized) since `pages` package is separate from `panel`.

### Shared `package views`
All `.templ` files in `internal/views/*` declare `package views` — layout, components, resources, pages. Each subdirectory is a separate Go package; duplicate package names across directories are fine.

### templ `templ.SafeURL` on every URL-bearing attr
templ v0.3.819 requires `templ.SafeURL(...)` for `<a href>` **and** `<form action>` — a bare `fmt.Sprintf(...)` in those attrs is a compile error in generated code. Verified fixes needed this on: list/detail View+Edit links, action/delete form actions, mailto links.

### `formaction` is NOT a SafeURL attr — and no `if`/expressions in `style`
templ treats `formaction`/`formmethod` as plain string attributes: wrapping the value in `templ.SafeURL(...)` is a **compile error** (`cannot use templ.SafeURL as string in templ.JoinStringErrs`). The bulk-action buttons and per-row submit buttons (which must escape the wrapping bulk `<form method="POST">`) use `formaction={ fmt.Sprintf(...) }` + `formmethod="POST"` without SafeURL. Related templ parser rules that fail `go tool templ generate` at parse time: **`style={ expr }` is rejected** ("style attributes cannot be a templ expression") — emit widths via `data-width` attrs and set `el.style.width` from JS instead; **`class={ if cond { "x" } }` is rejected** — use a plain Go helper function (`darkClass(theme)`) returning the class string.

### Dark mode + theming (`viewmodels.ThemeConfig`, `DefaultTheme()`)
M2 wiring: Tailwind runs `darkMode: 'class'`; `theme.extend.colors.brand.{primary,secondary}` + `fontFamily` come from the YAML. `internal/generator/viewmodels.go` defines `ThemeConfig` (DarkMode, BrandPrimary, BrandSecondary, FontFamily, FontMono, SidebarWidth, SidebarCollapsedWidth, SidebarCollapsible, TopbarSticky, MaxContentWidth) built by `DefaultTheme()` with hex defaults `#6366f1`/`#8b5cf6`. Every `layoutviews.Base(title, panelPath, theme, userName, views.Xxx(vd))` call site passes `viewmodels.DefaultTheme()` and `auth.UserName(r)`. `Base` sets `:root { --brand-primary/--brand-secondary }`, `<html class={ darkClass(theme) }>`, a `toggleTheme()` JS + `localStorage['gf-theme']` persistence (login.templ re-reads it too), and Chart.js reads `--brand-primary` at runtime. `Sidebar`/`Topbar` take the theme param; the sidebar width is set from a JS init using `data-width` and `toggleSidebar()` now toggles `aside.style.display` between shown and hidden (no more collapsed-width state). The topbar's left group holds the nav-toggle + theme buttons; the right group renders the logged-in user name (from `auth.UserName(r)`, populated at login via `SELECT id, COALESCE(name,''), password, COALESCE(role_name,'') FROM {auth.table}`) before the Logout link. **Tailwind `fontFamily` values are comma-separated stacks** ("Inter, sans-serif") — emit them through the `fontStack()` helper in `tailwind.go` which splits/quotes each name (`['Inter', 'sans-serif']`); a single quoted `'Inter, sans-serif'` array item becomes one bogus font-family.

### Bulk actions (`bulk: true`)
`hasBulkActions(r)` (handler.go) guards the router line `r.Post("/{name}/bulk/{action}", name.Bulk(db))` — plain `r.Post`, no RBAC (matches custom-action routes). `generateBulkHandler` writes `bulk.go` (package `{resource}`): `Bulk(db)` reads `r.PathValue("action")`, `ParseForm`, collects `r.Form["ids"]` via `strconv.ParseInt`, `switch`es over bulk-action SQL, loops `db.ExecContext(ctx, q, id)`, redirects to the list (302). The list templ wraps the table in one `<form method="POST">` when bulk actions exist: a select-all checkbox (`toggleSelectAll`), per-row `name="ids"` checkboxes, per-row action buttons + Edit/Delete as `formaction`/`formmethod` submit buttons (NOT SafeURL), and a "N Selected" toolbar posting to `/{res}/bulk/{action}`. Bulk actions must NOT also render as per-row action buttons.

### Hooks (`hooks.go`, `RETURNING id`)
Hooks attach to `FormAction` (create/update/delete) and `Action`. `internal/generator/hooks.go` emits the shared `internal/hooks/hooks.go` (Scope struct + one stub per unique fn hook, deduped across the whole config) and the `hookCallsStr`/`scopeValuesStr`/`returningClause` snippet builders. Gotchas:
- **Any hook block forces the `hooks` import in the generated handler** — the `hooks.Scope{...}` literal lives in the hooks package, so a sql-only hook still needs `import hooks "…/internal/hooks"`. Condition on `Hooks != nil`, NOT on `HasFn()`. **Exception: proc-only hooks on sqlite.** Since sqlite cannot call stored procedures, use `g.hookBlockEmits(h)` (true for any fn/sql hook, or any proc hook when the driver isn't sqlite) as the gate for the import/Scope/`RETURNING` — a proc-only block on sqlite must produce no `hooks` import and no `RETURNING` or the generated handler fails with an unused import.
- **create id capture**: only when `g.hookBlockEmits(create.Hooks)` does the create POST switch from `db.ExecContext(query, vals...)` to `db.QueryRowContext(r.Context(), query+" RETURNING <id>", vals...).Scan(&newID)` (postgres/sqlite) or `" OUTPUT INSERTED.<id>"` (mssql, no RETURNING in T-SQL) — `idColumn(r)` drives the column. `scope.ID = newID` runs before after-hooks. The hookless path stays byte-identical (`ExecContext`, no RETURNING).
- `sql` hooks are emitted as `db.ExecContext(r.Context(), "<sql>", scope.ID)` — always pass the SQL as a Sprintf **arg** (`%q`), never concatenate it into a template (a `%` inside hook SQL would corrupt the emitted source). `scope.ID` is 0 for before-create, the new row id after-create, the parsed path id otherwise; `$1` works on sqlite (named-param syntax + positional binding) and mssql (loose `$N`).
- **Stored procedures (`proc:`)** — a third hook kind and an alternative to `query:` on actions (mutually exclusive, enforced by the parser). `procSQL(name)` emits `CALL <name>($1)` on postgres and `EXEC <name> $1` on mssql (go-mssqldb rewrites `$1`→bound `@p1`, passed positionally to the proc's first param); the record id binds as `$1`, same as sql hooks/actions. `procSQL` returns `""` on sqlite and callers skip the emission: `hookCallsStr` drops proc hooks, `actionExecSQL` returns `""` so the action case becomes an empty `{}` block (still redirects) and the bulk loop gets `_ = id` as its body. A proc-only `Hooks` block on sqlite must NOT emit the import/Scope/RETURNING (see `hookBlockEmits` above).
- Hooks run inside the action case's mandatory `{ }` block scope (actions.go); the hook lines use `if err := …` so the later `_, err :=` in the block still compiles. A hook error aborts the request with HTTP 500.
- `bulk.go` does NOT run hooks — bulk reuses the action SQL/proc without the before/after lifecycle.

### Select options render from `data.Fields`, not static HTML
Form select options are rendered at runtime by looping `data.Fields` for the matching field and ranging its `Options`. The generated handler wires `options_query` into `ColumnDef.Options` (`formFieldDefsWithOpts`); the templ compares with `viewmodels.OptionValue(data.Item[f.Name])` because sqlc populates `sql.NullInt64`/`sql.NullString` (a bare `fmt.Sprintf("%v")` on `{1 true}` won't match key `"1"`).

### Value rendering is centralized in `viewmodels.Stringify`
Every value-to-text render in the generated app routes through `viewmodels.Stringify(v)` (in `viewmodels/models.go`), which unwraps `nil`, plain scalars, `time.Time` and every `sql.Null*` type (`NullString`, `NullInt32`, `NullInt64`, `NullFloat64`, `NullBool`, `NullTime`) — returning `""` for `nil`/invalid NULL instead of Go struct text. This fixes two failure classes seen on mssql/postgres (nullable columns) and on every create form:
- *BUG-1*: create forms no longer render empty values as `value="<nil>"` (was `fmt.Sprintf("%v", nil)`).
- *BUG-2*: nullable columns no longer leak `{1 true}` / `{Spojovací materiál true}` in list rows, detail views, or edit-form inputs.
`OptionValue` and `ItemValue` are thin wrappers over `Stringify`; the boolean checkbox checked-state uses `viewmodels.BoolValue` (true only for the true state, so unset/NULL renders unchecked rather than `<nil>`); the datetime/date renderers and form inputs use `viewmodels.TimeValue` + `TimeInputValue`/`DateInputValue`, which unwrap `sql.NullTime` and format in the browser's local `2006-01-02T15:04` / `2006-01-02` layout. The field renderers in `renderers.templ` (`renderBadge`, `renderBoolean`, `renderEmail`, …, `renderFloat`) take `interface{}` and call `Stringify`; a NULL boolean renders an empty cell. `renderers.templ` is emitted per resource view dir AND into `internal/views/components`, and must be run through `prefixImports` (it now references `viewmodels`).

### Shared create/update form renders the union of both field sets
`generateFormTempl` builds the form from the **union** of `r.Form.Create.Fields` + `r.Form.Update.Fields` (deduped by name, create order first then update-only fields appended). Each field is emitted with `if data.IsCreate { … }` (create-only) or `if !data.IsCreate { … }` (update-only) guards honoring the field's `visible:` list, so update-only fields (e.g. `status`, `created_at`) are no longer dropped from the edit form. This fixes BUG-4: before, the shared template was generated from the create fields only, so a field present only in `update` was silently omitted and the edit POST submitted `""` (failing e.g. Postgres `invalid input syntax for type timestamp`). When only one of create/update exists the guards are omitted (behavior unchanged). `hasFile`/`hasPicker`/enctype are computed over the merged set.

### options_query option rows
`buildOptionsLoader` scans into `interface{}` then keys the map with `fmt.Sprintf("%v", val)` — the `id`/value column is usually an `INTEGER` (`int64`), scanning into `string` fails silently. `findSQLCQuery` strips a trailing `;` from the query body (it is embedded as `SELECT a,b FROM (... ) AS _opt`, a trailing `;` is a syntax error).

### Generated app is a separate Go module
`admin/` has its own `go.mod`. Module name = basename of `--out` dir. Uses module-relative imports (`internal/data`, `internal/panel`).

### No comments in generated code
All generation uses clean string concatenation. No comments emitted.

### Router: one `r.Route` block with an inner `r.Group`
Login routes and the auth-protected routes live in a **single** `r.Route(panelPath, ...)`; the protected set is an inner `r.Group(func(r chi.Router) { r.Use(auth.AuthMiddleware); ... })`. Two `r.Route` calls on the same path panic (`chi: attempting to Mount() a handler on an existing path`); calling `r.Use` after a registered route also panics (`all middlewares must be defined before routes`).

### Default page mounts at both `/` and its `path`
A page with `default: true` gets `r.Get("/", handler)` **and** `r.Get(pagePath, handler)`. The generator skips only the literal `/` (the default branch already adds it). Without the `pagePath` mount, a `auth.login.redirect: /admin/dashboard` (the `init` default) 404s because only `/admin/` is registered.

### RBAC middleware is appended to middleware.go
`rbacMiddleware` (`checkRole` + `RBACMiddleware`) is built only when a resource has `policies:` and is appended to the generated `internal/panel/auth/middleware.go`. Do not inject `"strings"` into auth handler.go — only middleware.go uses `strings.Split`. The router wraps protected routes with `r.With(auth.RBACMiddleware(resource, action))`; action routes never use RBAC (plain `r.Post`).

### Action switch cases need a block scope
Each `case "name":` body is wrapped in `{ }` — a bare case body followed by `default:` is a syntax error in the generated `actions.go`.

### Every HTML view must be wrapped in `layoutviews.Base(...)`
Page handlers wrap `pageviews.X(pd)` in `layoutviews.Base(title, panelPath, ...).Render(r.Context(), w)`. Resource handlers (list, cards, detail, create form GET, update form GET) MUST do the same — a bare `views.XList(vd).Render(r.Context(), w)` renders a fragment with **no** `<html>`/`<head>`/CSS link/sidebar/topbar, so the page appears completely unstyled. All five render call sites in `handler.go` (list.go, card.go, detail.go, create.go, update.go) use `layoutviews.Base(resourceTitle(r), g.Config.Panel.Path, views.Xxx(vd)).Render(...)`, and each generated file imports `layoutviews "…/internal/views/layout"`. `resourceTitle(r)` returns `r.Label` (falls back to `r.Name`) and is defined in handler.go. Symptom: login + pages look fine, but every resource list/form/detail renders raw/unstyled — that means a render call is missing the `layoutviews.Base(...)` wrapper.

### Sidebar layout: no `fixed` sidebar + `ml-64` on content
The old base layout used `<aside class="w-64 … fixed left-0 top-0">` with `<main>` having no offset — since the sidebar is `position: fixed` it is out of the flex flow and the content (which had no `ml-64`, only the topbar did) slid underneath the nav, overlapping it. Correct layout (in `templ.go` `Base`): the sidebar is a normal flex child `<div class="flex h-screen"><aside class="w-64 … h-screen overflow-y-auto shrink-0">` next to a `<div class="flex-1 flex flex-col">` column holding the sticky topbar and `<main class="flex-1 overflow-y-auto p-6">`. No `ml-64` anywhere. Any new layout change must keep the sidebar in-flow (or add the margin offset to BOTH topbar and main) or the nav overlaps content again.

## SQL queries (`options_query`)

Fields with `options_query` (e.g. `ListRoles`) call `findSQLCQuery` in `sqlc.go` at generation time to extract raw SQL body from `-- name: QueryName` annotated `.sql` files. The handler GET code executes `SELECT options_value, options_label FROM (rawSQL) AS _opt` at request time. The queries are read from the **copied** files in the out dir (`g.OutDir` + `Config.SQLC.QueriesDir`), not the config dir — copySQLFiles must run first.

## bcrypt + file uploads

### Password hashing
Create handlers detect `type: password` fields, import `golang.org/x/crypto/bcrypt`, hash via `bcrypt.GenerateFromPassword` before appending to INSERT values. Update handler does **not** re-hash (the update form omits password in default YAML).

### File upload
When any form field has type `file` or `image`:
- Form template adds `enctype="multipart/form-data"`
- Handler switches from `r.ParseForm()` to `r.ParseMultipartForm(32 << 20)`
- `saveUploadedFile` helper (duplicated in create.go + update.go) reads via `r.FormFile`, saves to `static/uploads/{field_name}/{timestamp}{ext}`, and stores the relative URL path in DB
- Posting a create/update form with `curl` must use `-F`, not `-d`, once a file field exists (a plain `-d` POST returns 400 "invalid form")

## Auth & RBAC

- Login: GET renders `LoginPage` templ, POST verifies bcrypt vs `auth.table`, sets `gorilla/sessions` cookie.
- Logout: clears session, redirects to `panel.path`.
- Middleware: `AuthMiddleware` reads `session.Values["user_id"]`, stores `user_id` + `role` in context.
- RBAC middleware generated **conditionally**: `r.With(auth.RBACMiddleware(resource, action)).Get/Post(...)` when resource has `policies:` in YAML. Action routes **never** use RBAC (plain `r.Post`).

## Security hardening (Phase A of SPEC_future_enhancement.md)

The generated app ships security defaults. Keep them intact when editing the emitters:

- **Session secret**: generated `session.go` uses an `init()` that reads
  `SESSION_SECRET` (must be ≥ 32 chars, else `log.Fatal`), requires it when
  `APP_ENV=production`, otherwise falls back to an ephemeral `crypto/rand`
  secret with a warning (sessions invalidated on restart). Never re-emit the old
  hardcoded `go-fila-secret-key-change-in-production` default.
- **`order` whitelist**: list + card handlers clamp `order` to `asc`/`desc`
  (inserted AFTER the `default_sort` block so a `-`-prefixed default survives).
  The `sort` column is whitelisted separately against `validSorts`.
- **Upload validation**: the emitted `saveUploadedFile` (create.go/update.go,
  only when a `file`/`image` field exists) lowercases the extension, allows only
  the `safeUploadExts` allow-list (images/pdf/txt/csv/zip/office), rejects
  `text/html`/`image/svg+xml` via `http.DetectContentType` magic bytes (with
  `file.Seek(0, io.SeekStart)` before copying), and `static/uploads` requires
  `strings` already in the file's imports (create.go/update.go import it
  unconditionally). `/uploads/*` is served by a wrapper handler that forces
  `Content-Disposition: attachment` — don't revert to a bare `http.FileServer`.
- **Safe errors**: `internal/panel/httperr` (generated by `httperr.go`, wired in
  `generator.go` after `generateAuth`) provides `Internal(w, err)` / `NotFound(w, err)`
  that log server-side and return generic status text. Every resource handler and
  page handler imports `httperr` and MUST NOT emit `http.Error(w, err.Error(), ...)`.
- **Admin password**: `init --demo` / `init --db` accept `--admin-password`
  (threaded through `parseGlobalFlags`); when omitted, `randomPassword()`
  (introspect.go) generates a 14-char one-time password that is printed, not
  stored. `insertAdminUser` now returns `(bool, error)` (inserted or not).
- **Security headers**: `securityHeaders` middleware (router.go) is registered on
  every generated router and sets CSP (inline scripts/styles allowed for theme
  toggle + picker), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: same-origin`, `Permissions-Policy`. Don't remove it from
  `r.Use(...)`.

### Phase B (CSRF, session, rate limiting, CSV, action RBAC)

- **CSRF middleware is registered FIRST inside the panel `r.Route` block**
  (`r.Use(auth.CSRFMiddleware)`), BEFORE the login routes. It skips
  GET/HEAD/OPTIONS and `/static/`/`/uploads/` prefixes; every other POST (incl.
  login, logout, create/update/delete/action/bulk) requires a matching token via
  hidden `_csrf` input or `X-CSRF-Token` header. The token lives in the session
  (`session.Values["csrf_token"]`, minted lazily by `CSRFToken(r, w)`); the
  generated `views` pass it via `data.CSRFToken` / `auth.CSRFToken(r, w)`. Don't
  drop the `auth.CSRFToken(r, w)` 5th arg from any `layoutviews.Base(...)` call
  — the signature is
  `Base(title, panelPath, theme, userName, csrfToken, children)`.
- **Login rate limiting is conditional**: `ratelimit.go` is emitted ONLY when
  `auth.login.rate_limit` is set with `max_attempts > 0` (`rateLimited` flag in
  auth.go). The `loginLimited(r)`/`resetLoginLimit(r)` helpers and the
  re-render-on-limit branch in `handler.go` are all gated on it — a config
  without the block must not reference the helpers.
- **Session rotation**: on successful login the old session is expired
  (`MaxAge = -1` + `Save`) then a FRESH session is minted with
  `Store.New(r, "go-fila-session")` — do NOT re-`GetSession` after expiring,
  gorilla reloads the old request cookie. `resetLoginLimit(r)` runs on success.
- **Logout is POST-only** (`r.Post("/logout", ...)`); the topbar renders a
  `<form method="POST">` with hidden `_csrf`. The logout form action is
  `templ.SafeURL(fmt.Sprintf("%s/logout", panelPath))` — the `%%s` in the
  generator source must stay doubled or the `panelPath` arg count breaks.
- **CSV formula injection**: `export.go` headers AND values pass through
  `csvSafe`, which prefixes `'` on leading `=`, `+`, `-`, `@`, tab or CR.
- **Action/bulk RBAC**: `Action.Policy` ("role1|role2") emits
  `ActionRBACMiddleware` in middleware.go and wraps the action + bulk routes
  with `r.With(auth.ActionRBACMiddleware("<res>"))` — gated on
  `hasActionPolicies(res)` (a `policies:` block alone is NOT enough; the action
  itself must set `policy:`).

## Pages & widgets

Supported widget types: `stat`, `stats_grid`, `chart` (line/bar/pie/area via Chart.js), `table`, `list`, `html`. Each queries DB via raw SQL at request time. Chart data serialized to JSON in `data-chart-labels` / `data-chart-values` attributes.

Chart.js is **vendored at build time** — no CDN, runtime is offline. The generated `package.json` declares `chart.js` (pinned `^4.4.1`) in `devDependencies` and a `copy:chartjs` script (`mkdir -p static/js && cp node_modules/chart.js/dist/chart.umd.js static/js/chart.js`). The Makefile `css` target runs `npm run build:css` **and** `npm run copy:chartjs`; `templ.go` `Base` references `/static/js/chart.js`. Version is pinned to 4.4.x because 4.5+ renamed the UMD bundle to `chart.umd.min.js`. `go-fila generate` itself never runs npm, so chart.js is only copied by the `make`/`css` step — a plain `go build` (without `make`) will 404 on chart.js.

## Repo layout

| Path | Purpose |
|---|---|
| `cmd/go-fila/main.go` | CLI entry (init/generate/validate/version), hand-rolled flags |
| `cmd/go-fila/demo.go` | `init --demo` — full-featured sqlite demo scaffolding + seeding |
| `cmd/go-fila/introspect.go` | `init --db` — DB introspection, auth table creation, YAML/SQL generation |
| `cmd/go-fila/edit.go` | `edit` — entry point for interactive YAML config editor |
| `cmd/go-fila/editor/` | tview TUI editor: 3-pane shell, section editors, sync + validate + preview screens (18 files, see `edit` above) |
| `internal/types/` | YAML-tagged Go structs for config schema (5 files: config.go, panel.go, resource.go, field.go, hook.go) |
| `internal/parser/` | yaml.v3 unmarshal + validation (schema.go, validator.go) |
| `internal/generator/` | Code generation pipeline (13 files, see above) |
| `examples/` | Empty placeholder dirs (`full`, `minimal`) — working examples live in `cmd/go-fila/main.go`'s `cmdInit` |
| `SPEC.md` | Authoritative YAML schema and spec — check before adding features |
| `testdata/`, `pkg/auth/` | Empty placeholders (.gitkeep only), unused |

## Generated app dependencies

`github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `github.com/gorilla/sessions`, `golang.org/x/crypto`. Plus `github.com/jackc/pgx/v5` (postgres, blank-imported in main.go), `github.com/mattn/go-sqlite3 v1.14.24` (sqlite, blank-imported in main.go), and `github.com/microsoft/go-mssqldb v1.10.0` (mssql, blank-imported in main.go) — the `pgx` stdlib driver registers the `"pgx"` database/sql name, so generated main.go calls `sql.Open("pgx", dsn)` for postgres.

The generated `go.mod` also declares `tool github.com/a-h/templ/cmd/templ` so `go tool templ generate` works without a manual templ install, and `generateMakefile()` emits a `Makefile` whose `build` target runs all steps (npm deps, Tailwind, sqlc, tidy, templ, `go build -o <binary> .`).
