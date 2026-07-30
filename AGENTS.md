# go-fila — Agent Guide

## Build

```sh
go build ./cmd/go-fila          # the project binary
go build ./...                   # FAILS if admin/ dir exists (separate Go module)
```

Single binary at `cmd/go-fila/main.go` — stdlib flags, no cobra/viper.

## Init → Generate flow

```sh
go-fila init            # writes go-fila.yaml + sql/{migrations,queries}/
go-fila generate        # generates admin/ app, runs sqlc + tailwind (non-fatal)
cd admin
npm install && npm run build:css   # build tailwind manually
sqlc generate                       # retry if it failed during generate
go mod tidy && go build ./...
```

`sqlc generate` and `npx tailwindcss` failures are **non-fatal**. The user re-runs them manually.

## Generator pipeline (10 files in `internal/generator/`)

`generator.go` orchestrates calls to (in order):

1. `sqlc.go` — sqlc.yaml + SQLC query lookup (`findSQLCQuery`)
2. `main.go` — `main.go` with `database/sql` init
3. `router.go` — chi routes + RBAC wiring, page handlers with widgets
4. `auth.go` — login/logout handlers, session middleware, RBAC middleware
5. `handler.go` — per-resource: list, detail, create, update, **delete, action, CSV export**
6. `templ.go` — all `.templ` views (layout, components, resource views, page views)
7. `mod.go` — `go.mod`
8. `viewmodels.go` — Go structs for view data
9. `tailwind.go` — Tailwind config + static asset generation
10. `router.go:generatePage` — page handlers with widget DB queries

All generation uses `os.WriteFile` + `fmt.Sprintf`, never `text/template`.

## Resource handler SQL strategy

| Operation | SQL approach |
|---|---|
| List | Raw SQL with dynamic WHERE/ORDER BY/LIMIT |
| Detail | SQLC function (`data.GetUser(db, int64(id))`) |
| Create POST | Raw SQL INSERT via `db.ExecContext` |
| Update GET | SQLC populate query |
| Update POST | Raw SQL UPDATE via `db.ExecContext` |
| Delete | Raw SQL DELETE via `db.ExecContext` |
| Action | Raw SQL per action name (switch dispatch) |
| CSV Export | Raw SQL SELECT + `encoding/csv` |

Create/update avoid SQLC params because `r.FormValue` returns `string` but SQLC generates typed structs (`int32` for `INTEGER`). Raw SQL `ExecContext` accepts `interface{}`.

## Critical gotchas

### Format specifier counting in Sprintf
Every `%s`/`%q`/`%d` must have a matching arg. `%%` is escaped (produce `%` in output, no arg consumed). A mismatch silently produces garbled Go source. `buildOptionsLoader`, `preHashCode`, and `fileImport` insertions are common drift points.

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

### Generated app is a separate Go module
`admin/` has its own `go.mod`. Module name = basename of `--out` dir. Uses module-relative imports (`internal/data`, `internal/panel`).

### No comments in generated code
All generation uses clean string concatenation. No comments emitted.

## SQL queries (`options_query`)

Fields with `options_query` (e.g. `ListRoles`) call `findSQLCQuery` in `sqlc.go` at generation time to extract raw SQL body from `-- name: QueryName` annotated `.sql` files. The handler GET code executes `SELECT options_value, options_label FROM (rawSQL) AS _opt` at request time.

## bcrypt + file uploads

### Password hashing
Create handlers detect `type: password` fields, import `golang.org/x/crypto/bcrypt`, hash via `bcrypt.GenerateFromPassword` before appending to INSERT values. Update handler does **not** re-hash (the update form omits password in default YAML).

### File upload
When any form field has type `file` or `image`:
- Form template adds `enctype="multipart/form-data"`
- Handler switches from `r.ParseForm()` to `r.ParseMultipartForm(32 << 20)`
- `saveUploadedFile` helper (duplicated in create.go + update.go) reads via `r.FormFile`, saves to `static/uploads/{field_name}/{timestamp}{ext}`, and stores the relative URL path in DB

## Auth & RBAC

- Login: GET renders `LoginPage` templ, POST verifies bcrypt vs `auth.table`, sets `gorilla/sessions` cookie.
- Logout: clears session, redirects to `panel.path`.
- Middleware: `AuthMiddleware` reads `session.Values["user_id"]`, stores `user_id` + `role` in context.
- RBAC middleware generated **conditionally**: `r.With(auth.RBACMiddleware(resource, action)).Get/Post(...)` when resource has `policies:` in YAML. Action routes **never** use RBAC (plain `r.Post`).

## Pages & widgets

Supported widget types: `stat`, `stats_grid`, `chart` (line/bar/pie/area via Chart.js CDN), `table`, `list`, `html`. Each queries DB via raw SQL at request time. Chart data serialized to JSON in `data-chart-labels` / `data-chart-values` attributes.

## Repo layout

| Path | Purpose |
|---|---|
| `cmd/go-fila/main.go` | CLI entry (init/generate/validate/version), hand-rolled flags |
| `internal/types/` | YAML-tagged Go structs for config schema (4 files: config.go, panel.go, resource.go, field.go) |
| `internal/parser/` | yaml.v3 unmarshal + validation (schema.go, validator.go) |
| `internal/generator/` | Code generation pipeline (10 files) |
| `examples/` | Working YAML examples (full + minimal) |
| `SPEC.md` | Authoritative YAML schema and spec — check before adding features |
| `testdata/`, `pkg/auth/`, `internal/templates/` | Reserved / unused |

## Generated app dependencies

`github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `github.com/gorilla/sessions`, `golang.org/x/crypto`
