# go-fila — Software Specification

**go-fila** is a YAML-driven admin dashboard generator for Go, inspired by FilamentPHP. It reads a declarative YAML specification and generates a fully functional admin panel with CRUD resources, pages, widgets, authentication, navigation, and theming — all without writing boilerplate Go code.

---

## Core Architecture

```
User writes:
  ├── go-fila.yaml         (panel config, resources, pages, navigation, auth)
  ├── sql/schema.sql       (DDL — CREATE TABLE)
  └── sql/queries/*.sql    (annotated SQLC queries)

  OR uses `init --db` to auto-generate all of the above from an existing database.

go-fila generate:
  ├── runs sqlc generate              → internal/data/ (Go structs + query fns)
  ├── generates internal/panel/       → handlers calling SQLC functions
  ├── generates internal/views/       → .templ components (type-safe UI)
  ├── generates internal/assets/      → Tailwind CSS (compiled), static files
  ├── generates main.go + router      → chi-based wiring
  ├── writes go.mod + Makefile        → templ tool directive; `make` builds the binary
  └── scaffolds working example       → User/Role resources + auth + dashboard
```

---

## Tech Stack

| Concern | Decision |
|---|---|
| Language | Go (stdlib `database/sql`) |
| Data layer | SQLC (type-safe Go from SQL) |
| Frontend | Templ (compiled Go templates) |
| CSS | Tailwind CSS (compiled at build time) |
| Charts | Chart.js (bundled static asset) |
| Icons | Heroicons (inline SVG) |
| Router | chi |
| Auth | `gorilla/sessions` + bcrypt |
| Runtime model | **Pure code-gen** — zero runtime dep on go-fila |

---

## YAML Configuration Schema

### Top-Level Structure

```yaml
# go-fila.yaml
version: "1.0"

panel:
  id: admin
  path: /admin
  name: "My Admin"
  brand:
    logo: /assets/logo.svg
    favicon: /assets/favicon.ico
    colors:
      primary: "#6366f1"
      secondary: "#64748b"
  layout:
    sidebar:
      collapsible: true
      width: 280
      collapsed_width: 72
    topbar:
      sticky: true
    max_content_width: 7xl
  theme:
    dark_mode: true
    font:
      family: "Inter, sans-serif"
      mono: "JetBrains Mono, monospace"

connections:
  default:
    driver: postgres
    dsn: "postgres://user:pass@localhost:5432/db?sslmode=disable"
    pool:
      max_open: 25
      max_idle: 10
      lifetime: 5m

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
  registration: false
  password_reset: true
  remember_me: true

navigation:
  - group: "User Management"
    icon: users
    sort: 1
    items:
      - resource: User
      - resource: Role
  - group: "Analytics"
    icon: chart
    sort: 2
    items:
      - page: Reports
      - type: link
        label: "Google Analytics"
        url: https://analytics.google.com
        opens_in_new_tab: true
```

---

## Core Concepts

### Panel

A panel is the top-level admin interface container. Multiple panels can exist (e.g., `/admin`, `/app`), each with its own resources, pages, auth, and theme.

### Resource — Query-Driven, Not Table-Driven

Each resource has three independent views, each sourcing data from a **SQLC query function** (not a 1:1 table mapping):

```yaml
resources:
  - name: User
    label: Users
    icon: users
    group: "User Management"

    # ── LIST VIEW ──────────────────────────────────
    list:
      query: ListUsers           # SQLC :many function name (informational; the handler builds its own raw paginated query)
      count_query: CountUsers    # SQLC :one for pagination (informational; total comes from a windowed COUNT(*) OVER())
      per_page: 20               # rows per page (default 20)
      columns:                   # UI rendering — matches SQLC struct field names
        - name: id
          type: integer
          sortable: true
        - name: name
          type: string
          searchable: true
        - name: email
          type: email
          sortable: true
        - name: role_name
          label: Role
          type: text
        - name: status
          type: badge
          options:
            active: success
            inactive: warning
            suspended: danger
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at

    # ── DETAIL VIEW ────────────────────────────────
    detail:
      query: GetUser             # SQLC :one function
      params:
        id: "{record.id}"
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
        - name: role_name
          label: Role

    # ── CARD VIEW (optional) ───────────────────────
    # View-only card grid. Fields match the form field definition. `columns`
    # = cards per row (X), `rows` = rows per page (Y); a page shows X*Y cards.
    # Pagination and search behave like the list view. Set `kanban_field` to
    # the name of a `select` field to render a kanban board instead: cards are
    # grouped into columns by that field's option values.
    card:
      fields:
        - name: title
          type: string
        - name: status
          type: select
          options:
            todo: "To Do"
            doing: "In Progress"
            done: "Done"
      columns: 3                 # cards per row (X)
      rows: 4                    # rows per page (Y)
      kanban_field: status       # optional select field -> kanban board
      searchable:
        - title
      default_sort: -created_at
        - name: status
          type: badge
          options:
            active: success
            inactive: warning

    # ── FORM VIEW (create + edit) ─────────────────
    form:
      create:
        query: CreateUser        # SQLC :one or :exec function
        fields:
          - name: name
            type: text
            required: true
            validation:
              min: 2
              max: 255
          - name: email
            type: email
            required: true
          - name: password
            type: password
            required: true
            visible: [create]
          - name: role_id
            type: select
            options_query: ListRoles     # SQLC query for dynamic options
            options_value: id
            options_label: name
          - name: status
            type: select
            options:
              - active
              - inactive
              - suspended
      update:
        query: UpdateUser
        populate_query: GetUser
        populate_params:
          id: "{record.id}"
        fields:
          - name: name
            type: text
          - name: email
            type: email
          - name: role_id
            type: select
            options_query: ListRoles

    # ── ACTIONS ────────────────────────────────────
    actions:
      - name: activate
        label: "Activate"
        icon: check
        color: success
        requires_confirmation: true
        bulk: true
        query: ActivateUser     # SQLC :exec function
      - name: archive
        label: "Archive"
        proc: sp_archive_user   # stored procedure (CALL/EXEC); ignored on sqlite
        bulk: true

    # ── POLICIES ────────────────────────────────────
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin"
      update: "admin|manager"
      delete: "admin"
```

### Hooks (v0.5+)

`before`/`after` lifecycle hooks attach to any form action (`form.create`, `form.update`, `form.delete`) and to custom `actions`. Each hook is either a user-implemented Go function (`fn`), an inline SQL statement (`sql`) or a stored procedure call (`proc`; postgres/mssql only — ignored on sqlite):

```yaml
    form:
      create:
        hooks:
          before:
            - name: validate_domain
              fn: ValidateUserDomain      # stub generated in internal/hooks/hooks.go
          after:
            - name: notify
              sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"
            - name: archive_after_create
              proc: sp_archive_user       # CALL sp_archive_user($1) / EXEC sp_archive_user $1
    actions:
      - name: deactivate
        query: ActivateUser
        hooks:
          before: []
          after: []
```

`internal/hooks/hooks.go` defines `Scope{ID int64, Table, Action string, Values map[string]interface{}}` and one compile-ready stub per declared `fn` hook; the user implements the stubs. `sql` hooks are inlined as `db.ExecContext(..., <sql>, scope.ID)`. Create handlers capture the new row id via a driver-aware `QueryRowContext(...).Scan(&newID)` using `RETURNING <id>` (postgres/sqlite) or `OUTPUT INSERTED.<id>` (mssql) so after-create hooks see it. A hook error aborts the request with HTTP 500.

**Stored procedures (`proc`):** `proc` on a hook or custom action names a stored procedure to call, binding the current record id as its single argument. Postgres emits `CALL <name>($1)`; mssql emits `EXEC <name> $1` (go-mssqldb loose `$N`→`@p1` mapping passes the bound parameter positionally to the proc's first parameter). Schema-qualified names (`myschema.sp_foo`) are passed through verbatim. **SQLite has no stored procedures: proc references are ignored at generation time** — a proc hook is skipped, and a proc-only custom action becomes a no-op that still redirects back to the list (hooks in the same block still run). No output parameters or return values are captured (plain `db.ExecContext`).

**Field types (UI rendering only, no DB mapping):**

| Type | Renders As |
|---|---|
| `integer` | Number column / number input |
| `string` | Text column / text input |
| `text` | Text column / textarea |
| `email` | Mailto link / email input |
| `password` | Hidden / password input |
| `boolean` | Check icon / toggle |
| `badge` | Colored badge |
| `datetime` | Formatted date/time |
| `date` | Date only |
| `image` | Thumbnail |
| `file` | Download link |
| `select` | Dropdown (static or SQLC-backed); with `options_query` renders as a modal record picker (Browse button + searchable list, only row selection active) |
| `relation` | Link to related record |
| `json` | Pretty-printed |
| `float` | Decimal / number input |
| `gps` | GPS coordinates (maps link) |

### Pages & Widgets

Pages are custom dashboard-style views that place widgets in a grid:

```yaml
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stats_grid
        columns: 4
        widgets:
          - type: stat
            label: "Total Users"
            query: CountUsers
            icon: users
            color: primary
          - type: stat
            label: "Revenue"
            query: TotalRevenue
            icon: dollar
            prefix: "$"
      - type: chart
        label: "Monthly Revenue"
        chart:
          type: line
          query: MonthlyRevenue
          x: month
          y: total
      - type: table
        label: "Recent Orders"
        query: ListRecentOrders
        columns: [id, customer_name, total, status, created_at]
        limit: 5
```

**Widget types:** `stat`, `stats_grid`, `chart` (line/bar/pie/area), `table`, `list`, `html`

---

## SQLC Integration

The user writes standard SQLC-annotated SQL:

```sql
-- sql/schema.sql
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password VARCHAR(255) NOT NULL,
    role_id INT REFERENCES roles(id),
    status VARCHAR(20) DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE roles (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL
);

-- sql/queries/user.sql
-- name: ListUsers :many
SELECT u.*, r.name as role_name
FROM users u
LEFT JOIN roles r ON r.id = u.role_id
ORDER BY u.created_at DESC;

-- name: GetUser :one
SELECT u.*, r.name as role_name
FROM users u
LEFT JOIN roles r ON r.id = u.role_id
WHERE u.id = $1;

-- name: CreateUser :one
INSERT INTO users (name, email, password, role_id, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET name = $2, email = $3, role_id = $4, status = $5
WHERE id = $1
RETURNING *;

-- name: CountUsers :one
SELECT COUNT(*) FROM users;

-- name: ListRoles :many
SELECT * FROM roles ORDER BY name;
```

YAML `query` values reference the **SQLC function name** (e.g., `ListUsers`, `GetUser`, `CreateUser`). During generation, go-fila parses SQLC output to resolve parameter structs, return types, and function signatures for type-safe code generation.

---

## Code Generation Pipeline

```
go-fila generate --config go-fila.yaml --out ./output
```

1. **Parse YAML** — validate structure, cross-reference references
2. **Run `sqlc generate`** — produce `internal/data/` with Go types + query functions
3. **Parse SQLC output** — extract struct types, field names, function signatures
4. **Generate handlers** — for each resource view:
   - List: `ListUsers(ctx)` → paginate → render `UserList.templ`
   - Detail: `GetUser(ctx, id)` → render `UserDetail.templ`
   - Form create: parse input → build `CreateUserParams` → `CreateUser(ctx, params)` → redirect
   - Form update: parse input → build `UpdateUserParams` → `UpdateUser(ctx, params)` → redirect
5. **Generate Templ views** — type-safe UI components for each resource/page
6. **Generate router** — chi routes with middleware
7. **Generate auth** — login handler, session middleware, RBAC middleware
8. **Generate main.go** — DB pool init, chi setup, server start
9. **Compile Tailwind** — scan `.templ` files for classes, output `styles.css`
10. **Generate go.mod + Makefile** — `tool github.com/a-h/templ/cmd/templ` directive; a `Makefile` whose `build` target runs all steps (npm deps, Tailwind, sqlc, tidy, templ, `go build -o <binary> .`) to produce the dashboard binary

---

## Generated Output Structure

```
output/
├── main.go                     # chi router, DB pool, server start
├── go.mod / go.sum             # go.mod declares templ as a Go tool
├── Makefile                    # make / make build — builds the dashboard binary
├── sqlc.yaml
├── sql/
│   ├── schema.sql
│   └── queries/
│       ├── user.sql
│       └── role.sql
├── internal/
│   ├── data/                   # SQLC generated (untouched by user)
│   │   ├── db.go
│   │   ├── models.go
│   │   ├── user.sql.go
│   │   └── role.sql.go
│   ├── panel/
│   │   ├── router.go           # all chi routes
│   │   ├── auth/
│   │   │   ├── handler.go
│   │   │   ├── session.go
│   │   │   └── middleware.go
│   │   ├── resources/
│   │   │   ├── user/
│   │   │   │   ├── list.go
│   │   │   │   ├── detail.go
│   │   │   │   └── form.go
│   │   │   └── role/
│   │   └── pages/
│   │       └── dashboard.go
│   ├── views/                  # Templ components
│   │   ├── layout/
│   │   │   ├── base.templ
│   │   │   ├── sidebar.templ
│   │   │   └── topbar.templ
│   │   ├── resources/
│   │   │   ├── user/
│   │   │   │   ├── list.templ
│   │   │   │   ├── detail.templ
│   │   │   │   └── form.templ
│   │   │   └── role/
│   │   ├── pages/
│   │   │   └── dashboard.templ
│   │   ├── widgets/
│   │   │   ├── stat.templ
│   │   │   ├── chart.templ
│   │   │   └── table.templ
│   │   └── components/
│   │       ├── badge.templ
│   │       ├── pagination.templ
│   │       ├── search.templ
│   │       └── modal.templ
│   └── assets/
│       └── css/
│           └── styles.css      # compiled Tailwind (produced by Tailwind CLI)
└── static/
    └── js/
        └── chart.js            # bundled Chart.js
```

---

## CLI Commands

```
go-fila — YAML-driven admin panel generator

Usage:
  go-fila init           Scaffold go-fila.yaml + sqlc.yaml + sql/ + working example
  go-fila init --demo    Scaffold + seed sqlite demo DB (roles/users/customers/products/orders/orderlines)
  go-fila init --db DSN  Introspect existing DB, generate config + SQL from discovered tables
  go-fila edit           Interactive YAML config editor (TUI)
  go-fila generate       Run SQLC + generate admin panel Go application
  go-fila validate       Validate YAML + verify SQLC function references resolve
  go-fila version        Print version information

Flags:
  --config, -c   Path to YAML config file (default: go-fila.yaml)
  --out, -o      Output directory (default: ./admin)
  --db, -d DSN   Introspect database (postgres://... or sqlite file path)
  --force, -f    Overwrite existing files
  --demo, -D     With init: seed sqlite demo DB; login admin@demo.test (password generated)
  --admin-password, -p PASSWORD
                 Set the initial admin password for --demo / --db scaffolding
                 (a random one-time password is generated and printed when omitted)
  --verbose, -v  Enable verbose logging
  --skip-plugins, -s
                 Skip loading declared plugins (generate cannot use them)

AI-assisted edit (edit only):
  --prompt TEXT  Edit go-fila.yaml via OpenRouter instead of the TUI
                 (the full config is sent to OpenRouter)
                 file://PATH reads the prompt from a file (~ expands to home)
  --apikey KEY   OpenRouter API key (fallback: OPENROUTER_API_KEY env)
  --model MODEL  Model id (default: openrouter/auto)
  --dry-run      Print proposed YAML + diff without writing
```

### Generated Dashboard Runtime Flags

The generated dashboard binary accepts the following flags (each long form has a
short alias; stdlib `flag` also accepts a single dash: `-port`):

| Flag | Short | Default | Values | Description |
|---|---|---|---|---|
| `--port` | `-p` | `:8080` (or `ADDR` env var) | any int | Listen port; overrides the `ADDR` env var. 0 means "use `ADDR` / `:8080`" |
| `--log` | `-l` | `full` | `full`, `err` | Request log level. `full` logs every request via chi's `middleware.Logger`; `err` logs only requests that produced an error response (status >= 400) via a generated `errorOnlyLogger` middleware |
| `--help` | `-h` | — | — | Print command line syntax + flag meanings and exit 0 (before any DB or session setup) |

`logLevel` is passed to `panel.NewRouter(db, logLevel)`, which selects `middleware.Logger` or `errorOnlyLogger`. The `errorOnlyLogger` wraps the response with `middleware.NewWrapResponseWriter` and emits `log.Printf("%s %s -> %d", r.Method, r.RequestURI, ww.Status())` only when `Status() >= 400`. The generated `Makefile` `run` target passes `--port $(PORT)` and `--log $(LOG)` (`PORT ?= 8080`, `LOG ?= full`).

Example: `./admin --port 9090 --log err` or `./admin -p 9090 -l err`

### Database Introspection (`--db`)

`go-fila init --db {dsn}` connects to an existing database, introspects its schema, and generates everything needed to build an admin panel:

1. **Driver detection:** `postgres://`/`postgresql://` DSN → postgres driver (via `pgx/v5`); everything else → sqlite (via `modernc.org/sqlite`)
2. **Schema introspection:** discovers tables, columns (types, nullability, defaults), primary keys, and foreign keys
3. **Auth table management:** if `users`/`roles` tables are missing, creates them with driver-appropriate DDL and seeds default roles + admin user. The admin password is set from `--admin-password` or a random one-time password (generated + printed to the console, not stored) when omitted. If the tables already exist with data, they are left untouched.
4. **YAML generation:** one `Resource` per discovered table (excluding `users`/`roles`) with list/detail/form sections
5. **SQL generation:** SQLC-annotated queries per table — List (with LEFT JOINs for FK labels), Count, Get, Create, Update, Delete — plus options queries for FK relation fields
6. **Type mapping:** database column types are mapped to go-fila field types (e.g. `varchar` → `string`, `int` → `integer`, `timestamp` → `datetime`)

**Foreign key handling:** FK columns become `relation` fields in forms with `options_query`. In list views, FK columns are replaced with LEFT JOINs showing the foreign table's label column (auto-detected: prefers `name`, then `title`, then `label`, then first non-PK text column).

---

## Authentication & Authorization

- Session-based auth with `gorilla/sessions` + bcrypt
- Configurable user table and login fields
- Password reset flows
- "Remember me" support
- Resource-level RBAC policies: `view_any`, `view`, `create`, `update`, `delete`
- Permission strings reference roles from the roles table
- Generated middleware checks authenticated user role against policy

---

## Plugin System (v0.5+)

Plugins extend go-fila at **generation time** by contributing resources, pages, navigation groups, SQL files, and hook attachments. A plugin is a separate Go module that implements the `github.com/go-fila/go-fila/pkg/plugin.Plugin` interface. When plugins are declared in `go-fila.yaml`, go-fila runs each plugin in a throwaway module (a "shim"), collects a JSON manifest of its contributions, and merges it into the config before code generation. The generated app keeps the core design decision of **zero runtime dependency on go-fila**.

### YAML Configuration

```yaml
plugins:
  - name: audit                    # REQUIRED — unique plugin identifier
    source: ./plugins/audit        # REQUIRED — local dir (./, /, ~) or Go module path
    config:                        # OPTIONAL — arbitrary map passed to Configure()
      table: audit_log
      retention_days: 90
  - name: other
    source: github.com/user/plugin
    config:
      key: value
```

- `source` as a **local directory** (starts with `.`, `/`, or `~`) is resolved to an absolute path; its `go.mod` `module` directive is read to get the module path. The shim adds a `replace <mod> => <abs>` directive so the plugin compiles against the exact local sources.
- `source` as a **module import path** (e.g., `github.com/user/plugin`) is fetched from the Go module proxy during `go mod tidy` in the shim.
- The `config` map is JSON-encoded and passed to the plugin's `Configure(cfg map[string]any)` method if it implements `Configurer` (detected via type assertion). go-fila **injects the database driver** under the reserved `"driver"` key (`"postgres"`, `"sqlite"`, or `"mssql"`) so plugins can emit driver-appropriate SQL (placeholders, DDL).
- **Plugin load failure is fatal** — an explicitly declared plugin that fails to load is a config error. Use `--skip-plugins` to disable all plugins (escape hatch for CI or broken plugins).

### Plugin Go Interface (Authoring API)

A plugin module must export a `func New() Plugin` at its root package. The package name must equal the last element of the module path (e.g., module `github.com/user/myplugin` → package `myplugin`).

```go
type Plugin interface {
    ID() string           // stable plugin identifier
    Register(p *Panel) error  // add contributions to the panel builder
    Boot(p *Panel) error      // post-registration initialization
}

type Configurer interface {     // optional; detected via type assertion
    Configure(cfg map[string]any) error  // receives YAML config + injected "driver"
}
```

### Panel Builder (`pkg/plugin.Panel`)

```go
func NewPanel() *Panel

func (p *Panel) AddResource(r Resource) error
    // Returns error on duplicate resource name within this plugin.

func (p *Panel) AddPage(pg Page) error
    // Path defaults to "/" + Name when empty. Returns error on missing/duplicate name.

func (p *Panel) AddNavigationGroup(g NavigationGroup)

func (p *Panel) AddSQLFile(name, content string)
    // name must be "queries/<file>.sql" or "migrations/<file>.sql".
    // go-fila writes it only if the destination file does not already exist.

func (p *Panel) AddHookToResource(resource, action, when string, h Hook) error
    // resource: existing resource name in merged config (e.g., "Customer")
    // action: "create" | "update" | "delete" | <custom action name>
    // when: "before" | "after"
    // h: Hook (Name, Fn, SQL, Proc). Only SQL/proc hooks are supported in M4; fn hooks rejected.

func (p *Panel) Manifest() Manifest  // JSON-serializable snapshot of all contributions
```

### Manifest & Merge

The `Manifest` struct contains:
```go
Resources       []Resource       // appended to config.Resources
Pages           []Page           // appended to config.Pages
Navigation      []NavigationGroup // appended to config.Navigation
HookAttachments []HookAttachment // resolved against merged resources
SQLFiles        map[string]string // written to outDir/sql/ (no overwrite)
```

**HookAttachment** semantics:
- The loader resolves the target resource by name in the **merged** config (YAML resources + previously loaded plugins' resources).
- The hook is appended to the target action's `Before` or `After` list (creating the `Hooks` block if missing).
- Only `SQL`/`proc` hooks are accepted from plugins in M4; `Fn` hooks cause a fatal merge error ("requires M5 — use sql").
- The hook's SQL/proc binds the current record id as `$1` (parity with existing hook emission): `0` for before-create, new row id after-create, parsed path id otherwise. `proc` hooks are ignored on sqlite.

**SQL file handling**: Files are written into `outDir/sql/<name>` only when they do not already exist (preserves user edits).

### Example: Audit Plugin

The `examples/plugins/audit/` directory contains a complete, driver-aware plugin:

```go
package audit

import plugin "github.com/go-fila/go-fila/pkg/plugin"

func New() plugin.Plugin { return &auditPlugin{} }

func (p *auditPlugin) Register(pb *plugin.Panel) error {
    // Add AuditLog resource with list/detail
    // Add AuditOverview page with stat widgets
    // Add "Audit" navigation group
    // Add migrations/audit_schema.sql + queries/audit.sql (driver-aware)
    // Attach after-delete hook to Customer resource
}
```

At generation time it contributes:
- `AuditLog` resource (list/detail with badge column for action)
- `AuditOverview` page (2 stat widgets: total entries, deletes logged)
- "Audit" navigation group (sidebar, links to AuditLog + AuditOverview)
- `migrations/audit_schema.sql` (DDL with driver-appropriate id column)
- `queries/audit.sql` (ListAuditLogs, CountAuditLogs, GetAuditLog with driver placeholders)
- Hook attachment: `Customer.delete.after` → `INSERT INTO audit_log ...`

To use it in your project:
```yaml
plugins:
  - name: audit
    source: ./plugins/audit      # or github.com/go-fila/plugin-audit when published
    config:
      table: audit_log
      retention_days: 90
```

### Implementation Notes (for go-fila maintainers)

- **Loader** (`internal/generator/plugin.go`): `loadPlugins()` runs after `copySQLFiles()` (so plugin SQL lands before sqlc) and before resource/page generation. It writes a shim module, runs `go mod tidy && go run .`, reads `manifest.json`, and merges it. Local go-fila checkout is found by walking up from `os.Executable()` / cwd for a `go.mod` declaring `module github.com/go-fila/go-fila` and `replace`d into the shim.
- **Shim** (`writeShim`): Generates `go.mod` + `main.go` that imports the plugin, builds a `Panel`, calls `Configure/Register/Boot`, and writes `manifest.json`.
- **Merge** (`mergeManifest`): Appends resources/pages/navigation; resolves hook attachments; writes SQL files (no overwrite); rejects fn hooks; validates duplicates.
- **Generated hooks.go fix** (M4 prerequisite): `generateHooks()` now writes `internal/hooks/hooks.go` whenever **any** hook block exists (fn or sql), not just when fn hooks exist. This ensures the `hooks.Scope` type is available for sql-only hooks (including plugin-contributed ones). Only fn hooks emit stubs; sql-only configs get a minimal file with just `Scope` and no `context`/`database/sql` imports.

---

## Phased Development

| Phase | Deliverable |
|---|---|
| **v0.1** | YAML parser + SQLC integration + model/schema generation + chi router + `init` with working example |
| **v0.2** | Templ list/detail/form views + Tailwind compilation + all field types + sorting/pagination/search |
| **v0.3** | Dashboard + stat/chart/table widgets + navigation sidebar |
| **v0.4** | Auth (login, session, middleware) + RBAC policies |
| **v0.5+** | Plugins, custom actions, hooks, file uploads, CSV export, dark mode, multi-panel |

---

## Example Minimal Config

```yaml
version: "1.0"
panel:
  id: admin
  path: /admin
  name: "My Admin"
connections:
  default:
    driver: sqlite
    dsn: "file:./data/admin.db"
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
  - name: User
    label: Users
    list:
      query: ListUsers
      columns:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
    detail:
      query: GetUser
      params: { id: "{record.id}" }
      fields:
        - name: id
        - name: name
        - name: email
    form:
      create:
        query: CreateUser
        fields:
          - name: name
            type: text
            required: true
          - name: email
            type: email
            required: true
          - name: password
            type: password
            required: true
      update:
        query: UpdateUser
        populate_query: GetUser
        fields:
          - name: name
            type: text
          - name: email
            type: email
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stat
        label: "Total Users"
        query: CountUsers
navigation:
  - group: "Management"
    icon: users
    items:
      - resource: User
```

---

## Project Repository Structure

```
go-fila/
├── SPEC.md                    # This specification
├── README.md
├── go.mod
├── cmd/
│   └── go-fila/
│       ├── main.go            # CLI entry point (init/generate/validate/version)
│       ├── demo.go            # init --demo — full sqlite demo scaffolding + seeding
│       ├── introspect.go      # init --db — DB introspection, auth table creation, YAML/SQL generation
│       ├── edit.go            # edit — entry point for interactive YAML config editor
│       └── editor/            # tview TUI editor: 3-pane shell, sections, sync + preview (see SPEC_yaml_editor.md)
├── internal/
│   ├── parser/                # YAML parsing & validation
│   │   ├── schema.go
│   │   └── validator.go
│   ├── schema/                # File-level SQL↔YAML analysis (editor sync tool)
│   │   ├── schema.go          # CREATE TABLE parser (sqlite/postgres dialects)
│   │   ├── queries.go         # SQLC query parser + SELECT projection extraction
│   │   ├── references.go      # YAML query/table/column reference collection
│   │   └── generate.go        # SQL/YAML stub emitters (query files, resource blocks)
│   ├── generator/             # Code generation pipeline
│   │   ├── sqlc.go            # SQLC invocation
│   │   ├── handler.go         # Handler generation
│   │   ├── templ.go           # Templ view generation
│   │   ├── router.go          # Router generation
│   │   ├── auth.go            # Auth code generation
│   │   ├── tailwind.go        # Tailwind compilation
│   │   └── main.go            # main.go generation
│   ├── templates/             # Go source and Templ templates (embed)
│   │   ├── handler.tmpl
│   │   ├── list.templ.tmpl
│   │   ├── form.templ.tmpl
│   │   └── ...
│   └── types/                 # Shared config types
│       ├── config.go
│       ├── resource.go
│       ├── field.go
│       └── panel.go
├── pkg/
│   └── auth/                  # Reusable auth helpers
├── examples/
│   ├── minimal/
│   │   └── go-fila.yaml
│   └── full/
│       └── go-fila.yaml
└── testdata/
```

---

## Key Design Decisions Summary

1. **SQLC is the single source of truth for Go types** — YAML never defines DB column types, only UI rendering hints
2. **Each view is an independent SQLC query** — list, detail, create, and update can each use completely different queries with different JOINs
3. **Field `name` matches SQLC struct field names** — cross-referenced at generate-time, not runtime
4. **Pure code-gen, zero runtime framework dependency** — generated app is a standard Go app using `database/sql` + SQLC + chi + Templ
5. **User owns `schema.sql` and `query.sql`** — full SQL expressiveness (CTEs, window functions, PostGIS, etc.)
