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

### `init --db` — Database introspection

`go-fila init --db {connection_string}` connects to an existing database, introspects its schema (tables, columns, primary keys, foreign keys), and generates `go-fila.yaml` + SQL migration/query files from the discovered tables. Works for both SQLite and Postgres.

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

Flags: `init --db <dsn> --config <yaml> --out <dir> --force`.

### `edit` — Interactive YAML config editor

`go-fila edit` opens the YAML config in a terminal UI built with `charmbracelet/huh` (Bubble Tea). Navigation uses a stack-based model: push screens on enter, pop on Esc.

**Supported blocks:** Panel (5 groups), Connections, SQLC, Auth, Navigation (groups + items), Resources (basic/list/detail/card/form/actions/policies), Pages (widgets).

**Architecture:** `cmd/go-fila/editor/` package with stack-based navigation. `EditorModel` wraps `[]tea.Model` (the screen stack). Each screen implements `isDone()` + `popScreen()` (the `screen` interface). huh forms bind directly to config fields for in-place editing. The `s` key saves (`yaml.Marshal` → write file), `q` discards.

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

Flags: `generate --config <yaml> --out <dir> --force --verbose`. `--out` basename becomes the module name.

## Driver support (postgres default, sqlite opt-in)

Driver comes from the first `connections:.*.driver` value (default `"postgres"`); `isSQLite()` accepts `sqlite`/`sqlite3`. Per-driver differences the generator must handle:

| Concern | postgres | sqlite |
|---|---|---|
| sqlc.yaml `engine` | `postgresql` | `sqlite` |
| `sql.Open` driver | `postgres` + blank-import sqlc pkg | `sqlite3` + `github.com/mattn/go-sqlite3` |
| go.mod | — | adds `github.com/mattn/go-sqlite3 v1.14.24` |
| LIKE operator | `ILIKE` | `LIKE` |
| bind placeholders | `$N` | `?` (positional, SQL-text order) |
| sqlc id type | `int32` | `int64` |

Helpers in `generator.go`: `driver()`, `isSQLite()`, `placeholder(n)`, `likeOp()`, `idGoType()`. **`placeholder()` is still unused** — create/update/delete handlers hardcode `$N` (works on sqlite since mattn binds positionally). Only the list handler is driver-aware.

### Generated main.go: DB sanity check runs BEFORE binding the port
`generateMain()` (`main.go`) emits `sql.Open` → `db.Ping()` → **`SELECT 1 FROM {auth.table} LIMIT 1`** (`sql.ErrNoRows` treated as OK) → only then `net.Listen` + `srv.Serve`. Rationale: mattn/go-sqlite3 silently **creates an empty DB file** when the file is missing, so `db.Ping()` succeeds against a "not found" database and the dashboard would otherwise bind the port and run broken (`no such table`) while holding it — a restart then hits `address already in use`. The sanity query makes a missing/uninitialized DB a fatal startup error **before** the port is bound. The listen port is resolved as `--port` flag → `ADDR` env → `:8080` (`flag.Int("port", 0, ...)`; stdlib `flag` accepts both `--port 9090` and `-port 9090`); the emitted `Makefile` `run` target passes `--port $(PORT)` (`PORT ?= 8080`). Generated server also does graceful shutdown on SIGINT/SIGTERM (`signal.NotifyContext` → `srv.Shutdown`) and logs a `is another dashboard instance already running?` hint on bind failure. Keep the bind AFTER the DB checks — ordering is what prevents a broken DB from occupying the port.

### sqlite list handler arg order (critical)
mattn binds `?` args positionally in SQL-text order, so sqlite branch appends **search args first, then `LIMIT ? OFFSET ?`**, and uses `LIKE`. The postgres branch appends `perPage, offset` first with `ILIKE $N` + `LIMIT $1 OFFSET $2`. Mixing these up silently returns wrong rows on sqlite.

### SQL files are copied into the output
`Generate()` calls `copySQLFiles()` which copies `sql/{queries,migrations}/*` from the **config dir** into `g.OutDir/sql/`. Without this, sqlc fails (`no queries contained in paths`) and `options_query` lookups return empty. `ConfigDir` is set in `cmd/go-fila/main.go` (`filepath.Dir(configPath)`); the generator has no other knowledge of where the YAML lives.

## Generator pipeline (files in `internal/generator/`)

`generator.go` orchestrates calls to (in order):

1. `ensureDirs()` — directory layout (also creates `sql/queries` + `sql/migrations`)
2. `generateSQLCConfig()` (`sqlc.go`) — sqlc.yaml, driver-aware engine
3. `copySQLFiles()` (`generator.go`) — copies user SQL into the out dir
4. `generateMain()` (`main.go`) — `main.go` with driver-aware `sql.Open`
5. `generateRouter()` (`router.go`) — chi routes + RBAC wiring, page handlers
6. `generateAuth()` (`auth.go`) — login/logout, session, RBAC middleware
7. `generateResource()` → per-resource handlers (`handler.go`): list, **card**, detail, create, update, **delete, action, CSV export**
8. `generatePage()` — page handlers with widget DB queries
9. `generateViews()` (`templ.go`) — all `.templ` views
10. `generateGoMod()` (`mod.go`, declares the templ `tool` directive), `generateMakefile()` (`makefile.go`), `generateViewModels()` (`viewmodels.go`), `generateAssets()` (`tailwind.go`)

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
| Action | Raw SQL per action name (switch dispatch) |
| CSV Export | Raw SQL SELECT + `encoding/csv` |

Create/update avoid SQLC params because `r.FormValue` returns `string` but SQLC generates typed structs (`int32` for `INTEGER`). Raw SQL `ExecContext` accepts `interface{}`.

Detail/update SQLC calls must cast the id to `idGoType()` — sqlite ids are `int64`, postgres `int32`. A literal `int32(id)` breaks the sqlite build.

## Card view (`card` section)

Optional per-resource view at `GET /{panel}/{resource}/cards` (reachable via a "Cards" button on the list view). Fields reuse the form `Field` type. `columns` (X = cards/row) and `rows` (Y = rows/page) define `per_page = X*Y`; pagination + search mirror the list handler (driver-aware, `LIKE`/`ILIKE`). When `card.kanban_field` names a **select** field, the handler groups rows into `KanbanColumns` via `viewmodels.OptionValue(item[field])` instead of a grid — `g.Card.Kanban` flips the shortcut in `cards.templ` (grid vs. board). The grid templ hardcodes `lg:grid-cols-{Columns}`; kanban buckets are keyed by the select's option keys plus any extra row values discovered at request time.

### Field renderer for `gps`
`renderCell` maps `gps` → `@renderGPS` and the form emits a text input with `lat, lng` placeholder; `renderGPS` renders a link out to Google Maps. Registering a new field type means updating BOTH `renderCell`'s switch and the form-input switch in `templ.go`, plus `FieldTypes` in `types/field.go`.

## Critical gotchas

### Format specifier counting in Sprintf
Every `%s`/`%q`/`%d` must have a matching arg. `%%` is escaped (produce `%` in output, no arg consumed). A mismatch silently produces garbled Go source (e.g. `%!s(MISSING)` literal in emitted templ). This is especially dangerous when a templ substring is built with its own `fmt.Sprintf` and then inserted into a parent one — any `%v`/`%d`/`%s` **inside** emitted `fmt.Sprintf(...)` calls must be doubled (`%%v`) in the generator source. `buildOptionsLoader`, `preHashCode`, `fileImport` insertions, and the `cardBody`/`actions`/`gridView`/`kanbanView` strings in `templ.go` are common drift points.

### `snakeToPascal` special-cases `id`
- `id` → `ID`
- `role_id` → `RoleID`
- `user_role_id` → `UserRoleID`
Any other pattern would produce `Id`/`RoleId` (wrong). Must match SQLC's output convention.

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

### Select options render from `data.Fields`, not static HTML
Form select options are rendered at runtime by looping `data.Fields` for the matching field and ranging its `Options`. The generated handler wires `options_query` into `ColumnDef.Options` (`formFieldDefsWithOpts`); the templ compares with `viewmodels.OptionValue(data.Item[f.Name])` because sqlc populates `sql.NullInt64`/`sql.NullString` (a bare `fmt.Sprintf("%v")` on `{1 true}` won't match key `"1"`).

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
| `cmd/go-fila/editor/` | TUI editor: stack-based navigation, huh forms, list managers (10 files) |
| `internal/types/` | YAML-tagged Go structs for config schema (4 files: config.go, panel.go, resource.go, field.go) |
| `internal/parser/` | yaml.v3 unmarshal + validation (schema.go, validator.go) |
| `internal/generator/` | Code generation pipeline (11 files, see above) |
| `examples/` | Empty placeholder dirs (`full`, `minimal`) — working examples live in `cmd/go-fila/main.go`'s `cmdInit` |
| `SPEC.md` | Authoritative YAML schema and spec — check before adding features |
| `testdata/`, `pkg/auth/` | Empty placeholders (.gitkeep only), unused |

## Generated app dependencies

`github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `github.com/gorilla/sessions`, `golang.org/x/crypto`. Plus `github.com/mattn/go-sqlite3 v1.14.24` when the driver is sqlite (blank-imported in main.go).

The generated `go.mod` also declares `tool github.com/a-h/templ/cmd/templ` so `go tool templ generate` works without a manual templ install, and `generateMakefile()` emits a `Makefile` whose `build` target runs all steps (npm deps, Tailwind, sqlc, tidy, templ, `go build -o <binary> .`).
