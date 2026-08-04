# go-fila

**go-fila** is a YAML-driven admin dashboard generator for Go. Write a declarative YAML config + SQL queries, and it generates a fully functional admin panel with CRUD resources, custom pages, widgets, authentication, RBAC — no boilerplate.

## Prerequisites

| Tool | Required for | Notes |
|---|---|---|
| [Go](https://go.dev/dl/) 1.26+ | Running go-fila + building generated app | |
| [SQLC](https://docs.sqlc.dev/en/latest/overview/install.html) | Generating the data layer (`internal/data/`) | Generator runs it, failure is non-fatal |
| [Node.js](https://nodejs.org/) + npm | Building Tailwind CSS + vendoring Chart.js in the generated app | `make css` / `npm install && npm run build:css && npm run copy:chartjs` in the output dir |
| [Templ](https://templ.dev/) | Compiling `.templ` files in the generated app | Optional — the generated `go.mod` declares `tool github.com/a-h/templ/cmd/templ`, so `go tool templ generate` is handled by the Go toolchain |

## Quick start

```sh
# Install go-fila
go install github.com/go-fila/go-fila/cmd/go-fila@latest

# Scaffold a project
mkdir my-admin && cd my-admin
go-fila init                  # writes go-fila.yaml + sql/{migrations,queries}/

# Write your SQL schema and queries in sql/
# Edit go-fila.yaml to configure panel, resources, pages, auth

# Generate the admin panel
go-fila generate              # produces ./admin/

# Build and run the generated app
cd admin
make build                    # builds the dashboard binary + assets

# Run the dashboard server
make run

# Individual steps: make deps / css / sqlc / templ / tidy / clean
# Deploy: make package (tar.gz of binary + static + sql + data), extract on
# the target machine and run the binary from the extracted dir.
```

The generated `Makefile` runs every step needed to build the dashboard binary (the `--out` basename becomes both the module name and the binary name): `npm install` → `npm run build:css` → `npm run copy:chartjs` → `sqlc generate` → `go mod tidy` → `go tool templ generate` → `go build -o <binary> .`. The generated `go.mod` declares `tool github.com/a-h/templ/cmd/templ`, so `go tool templ generate` works via the Go toolchain (no templ install needed). Equivalent manual steps: `npm install`, `npm run build:css`, `npm run copy:chartjs`, `sqlc generate`, `go tool templ generate`, `go mod tidy`, `go build -o admin .`.

Chart.js is vendored into `static/js/chart.js` at build time (pinned to `^4.4.1`; `copy:chartjs` copies `node_modules/chart.js/dist/chart.umd.js`), so the running dashboard serves charts locally and needs **no internet at runtime**. A plain `go build` skips the npm steps, so run `make` (or `make css`) at least once or `/static/js/chart.js` will 404.

## Deployment

`make package` bundles everything the dashboard needs to run into a single `tar.gz` release archive: the binary, the built `static/` assets (CSS, Chart.js, and any uploaded files), plus `sql/` migrations and the sqlite `data/` directory when present. The archive is named `<binary>-<date>.tar.gz` (override with `make package PACKAGE_NAME=my-release`).

```sh
make package
scp admin-20260804.tar.gz user@target:/
# on the target machine
tar xzf admin-20260804.tar.gz && cd admin-20260804
./admin --port 8080
```

Run the binary from the extracted directory — the sqlite DSN (`file:./data/admin.db`) is relative to the working directory. For postgres deployments, configure the database on the server and pass the DSN via the `DATABASE_URL` env var (or keep the one baked in at generation time).

The `init` command fails if files already exist unless `--force` is passed.

## CLI

```
go-fila init           Scaffold go-fila.yaml + sqlc.yaml + sql/ + working example
                       with --demo flag it generates fully functional demo dashboard including data 
go-fila generate       Generate admin panel Go application
go-fila validate       Validate YAML + verify SQLC references
go-fila version        Print version

Flags:
  --config, -c   Config file path (default: go-fila.yaml)
  --out, -o      Output directory (default: ./admin)
  --force        Overwrite existing files
  --verbose      Verbose logging
```

`sqlc generate` and `npx tailwindcss` failures during `go-fila generate` are non-fatal. Re-run them manually in the output directory.

## Project structure

```
go-fila/
├── cmd/go-fila/main.go         # CLI entry point
├── internal/
│   ├── types/                  # YAML-tagged config structs
│   │   ├── config.go           # Top-level config, panel, connection, auth
│   │   ├── panel.go            # Navigation types
│   │   ├── resource.go         # Resource, action, policy, validation types
│   │   └── field.go            # Field type enum (string, email, password, etc.)
│   ├── parser/                 # YAML parsing + validation
│   │   ├── schema.go
│   │   └── validator.go
│   └── generator/              # Code generation pipeline (11 files)
│       ├── generator.go        # Orchestrator
│       ├── handler.go          # Per-resource handlers (list, detail, create,
│       │                       #   update, delete, action, CSV export)
│       ├── templ.go            # All .templ views
│       ├── router.go           # Chi router + RBAC + page handlers
│       ├── auth.go             # Login/logout/session/RBAC middleware
│       ├── main.go             # Generated server entrypoint
│       ├── mod.go              # Generated go.mod (incl. templ tool directive)
│       ├── makefile.go         # Generated Makefile (build/run/clean targets)
│       ├── viewmodels.go       # View data structs
│       ├── tailwind.go         # Tailwind config + static assets
│       └── sqlc.go             # SQLC config + query lookup
├── examples/                   # Empty placeholder dirs (full, minimal)
├── SPEC.md                     # Authoritative spec
└── AGENTS.md                   # Agent instructions
```

## Generated output structure

```
output/
├── main.go                          # Chi router, DB pool, server start
├── go.mod / go.sum                  # go.mod declares templ as a Go tool
├── Makefile                         # make / make build — builds the binary
├── sqlc.yaml
├── package.json / tailwind.config.js
├── sql/
│   ├── migrations/schema.sql        # Copied from your project
│   └── queries/*.sql                # Copied from your project
├── internal/
│   ├── data/                        # SQLC generated (Go types + query fns)
│   │   ├── db.go
│   │   ├── models.go
│   │   └── *.sql.go
│   ├── panel/
│   │   ├── router.go                # All chi routes
│   │   ├── auth/                    # Login handler, session, middleware, RBAC
│   │   ├── resources/
│   │   │   └── {resource}/
│   │   │       ├── list.go          # List handler (raw SQL + dynamic filters)
│   │   │       ├── detail.go        # Detail handler (SQLC call)
│   │   │       ├── create.go        # Create handler (raw SQL INSERT)
│   │   │       ├── update.go        # Update handler (SQLC populate + raw SQL UPDATE)
│   │   │       ├── delete.go        # Delete handler (raw SQL DELETE) — if configured
│   │   │       ├── export.go        # CSV export handler — if list configured
│   │   │       └── actions.go       # Custom action handler — if actions defined
│   │   └── pages/
│   │       └── *.go                 # Page handlers (dashboard, reports, etc.)
│   ├── views/
│   │   ├── layout/                  # Base, sidebar, topbar .templ files
│   │   ├── resources/{resource}     # List, detail, form .templ files per resource
│   │   ├── pages/                   # Page .templ files
│   │   ├── widgets/                 # Stat, chart, table, list, html widgets
│   │   └── components/              # Badge, pagination, search, renderers, icons
│   ├── viewmodels/models.go         # View data structs
│   └── assets/css/styles.css        # Compiled Tailwind
└── static/js/chart.js               # Chart.js (bundled)
```

## YAML config reference

The config file (`go-fila.yaml`) is the single source of truth for your admin panel. This reference documents every attribute, its possible values, and its meaning. See `SPEC.md` for the authoritative schema.

### Top-level keys

| Key | Required | Default | Meaning |
|---|---|---|---|
| `version` | yes | — | Schema version string, e.g. `"1.0"`. Any non-empty value is accepted. |
| `panel` | yes | — | Panel identity and branding (see below). |
| `connections` | no | — | Database connections; the **first** entry's `driver` and `dsn` are used by the generated app. |
| `sqlc` | no | — | SQLC paths and output package (see below). |
| `auth` | no | — | Authentication config (see below). |
| `navigation` | no | — | Sidebar navigation groups. |
| `resources` | no | — | CRUD resources. At least one resource **or** page is required. |
| `pages` | no | — | Custom dashboard pages. At least one resource **or** page is required. |

### Panel

```yaml
panel:
  id: admin            # lowercase identifier; becomes part of generated handler/view
                       #   names (e.g. AdminDashboard). Default: "admin".
  path: /admin         # URL prefix for the whole panel, e.g. "/admin". Must start
                       #   with "/". Used as the base for all generated routes.
  name: "My Admin"     # Display name shown in the sidebar and login page.
```

The `brand`, `layout`, and `theme` sub-sections are parsed by the config schema but are **not yet wired into the generated output** (they are accepted for forward-compatibility):

```yaml
panel:
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
```

### Connections

```yaml
connections:
  default:             # map key — arbitrary name; only the FIRST entry is used
    driver: postgres   # "postgres" (default) | "sqlite" | "sqlite3"
    dsn: "postgres://user:pass@localhost:5432/db?sslmode=disable"
    pool:              # parsed but NOT applied to the generated app yet
      max_open: 25
      max_idle: 10
      lifetime: 5m
```

- `driver` determines sqlc's engine, the `sql.Open` driver, LIKE operator, bind placeholders, and SQLC id type throughout generation. If every entry omits `driver`, it defaults to `postgres`. `sqlite`/`sqlite3` enable the SQLite branch.
- `dsn` is embedded in the generated `main.go`. At runtime, the `DATABASE_URL` environment variable overrides it. A SQLite example: `file:./data/admin.db`.
- SQLite requires `github.com/mattn/go-sqlite3`, which is added to the generated `go.mod` automatically.

### SQLC

```yaml
sqlc:
  config: sqlc.yaml        # Default: "sqlc.yaml". Written to the output dir.
  queries_dir: ./sql/queries   # Default: "./sql/queries"
  schema_dir: ./sql/migrations # Default: "./sql/migrations"
  output_pkg: internal/data    # Default: "internal/data". Go package for SQLC output.
```

The generated `sqlc.yaml` is fixed relative to the output directory and always uses `./sql/migrations` / `./sql/queries`, `database/sql`, and a `data` package name. `queries_dir` also locates the `.sql` files used to resolve `options_query` references at generation time.

### Auth

```yaml
auth:
  guard: web          # parsed but not yet used
  provider: session   # parsed but not yet used
  table: users        # DB table holding auth users (login lookup). Default: "users".
  login:
    fields: [email, password]  # [usernameField, passwordField]; first = identity,
                               #   second = password (bcrypt-hashed in DB).
    redirect: /admin/dashboard  # URL after successful login. Must be a registered
                               #   route (e.g. the default page). Default: "<panel.path>/dashboard".
  registration: false     # parsed but not yet used
  password_reset: true    # parsed but not yet used
  remember_me: true       # parsed but not yet used
```

The login handler reads the identity field and the password field from the POST form, verifies the password against a bcrypt hash in `auth.table`, sets a `gorilla/sessions` cookie, and redirects to `login.redirect`. Unauthenticated users hitting a protected route are redirected to `<panel.path>/login`.

### Navigation

```yaml
navigation:
  - group: "User Management"  # sidebar section heading
    icon: users               # icon name rendered next to the heading
    sort: 1                   # integer; groups are ordered ascending by this value
    items:
      - resource: User        # link to a resource list -> "<panel.path>/user"
      - resource: Role
  - group: "Analytics"
    icon: chart
    items:
      - page: Dashboard       # link to a page -> "<panel.path>/dashboard"
      - type: link            # external / custom link
        label: "Google Analytics"
        url: https://analytics.google.com
        opens_in_new_tab: true   # adds target="_blank"
```

Each `item` is one of three kinds, selected by which key is present:
- `resource: <Name>` — link to that resource's list view (name is lowercased for the URL).
- `page: <Name>` — link to that page's route (path = `"/" + pageName` unless the page declares a custom `path`).
- `type: link` with `label`, `url`, and optional `opens_in_new_tab`.

### Resources

A resource is a CRUD-managed entity. It has up to three independent views (list, detail, form) plus optional custom actions and RBAC policies:

```yaml
resources:
  - name: User            # REQUIRED. PascalCase; lowercased for the Go package,
                          #   output dir and URL segment ("user"). The table name
                          #   is derived as <lowercase> + "s" ("users").
    label: Users          # Human-readable name used in the UI. Default: same as name.
    icon: users           # parsed but not yet used
    group: "User Management"  # parsed but not yet used (use navigation groups)
```

#### list

```yaml
    list:
      query: ListUsers            # ACCEPTED BUT IGNORED — the list handler builds a
                                  #   raw "SELECT <columns> FROM <table>" query instead.
      count_query: CountUsers     # ACCEPTED BUT IGNORED — the count is computed from
                                  #   "SELECT COUNT(*) FROM <table>" + the search WHERE.
      columns:
        - name: id                # column name; must match the SQL column used in the
                                  #   raw list query
          type: integer
          sortable: true          # clickable sort header for this column
        - name: name
          type: string
          searchable: true        # included in the global search (OR-ed LIKE/ILIKE)
        - name: email
          type: email
          sortable: true
        - name: role_name
          label: Role             # display label; default: the column name
          type: text
        - name: status
          type: badge
          options:                # map: value -> badge color (success|warning|danger)
            active: success
            inactive: warning
        - name: created_at
          type: datetime
          sortable: true
      default_sort: -created_at   # sort column + direction; leading "-" = descending
```

The list handler reads `page`, `search`, `sort`, and `order` from the query string, applies the configured `searchable`/`sortable` columns dynamically, and paginates at **20 rows per page**. The `default_sort` prefix `-` is stripped to produce the sort column and `desc` order; a missing prefix means ascending. Sort input is validated against the `sortable` columns (invalid values fall back to the default).

#### detail

```yaml
    detail:
      query: GetUser              # REQUIRED to render detail. SQLC :one function name.
      params:                     # ACCEPTED BUT IGNORED — the detail handler always
        id: "{record.id}"         #   calls data.New(db).<Query>(ctx, id) with the URL id.
      fields:
        - name: id
          type: integer
        - name: name
          type: string
        - name: email
          type: email
        - name: role_name
          label: Role
        - name: status
          type: badge
          options: { active: success, inactive: warning }
```

The detail handler parses `:id` from the URL, calls `data.New(db).<Query>(ctx, id)`, maps each declared field to the matching SQLC struct field (snake_case → PascalCase), and renders the detail view. Omit `detail` entirely to have no detail page.

#### card

```yaml
    card:
      fields:                      # view-only, same field definition as forms
        - name: title
          type: string
        - name: status
          type: select
          options:
            todo: "To Do"
            doing: "In Progress"
            done: "Done"
      columns: 3                   # X: cards per row
      rows: 4                      # Y: rows per page (a page shows X*Y cards)
      kanban_field: status         # optional select field -> kanban board
      searchable:
        - title
      default_sort: -created_at
```

The card view is view-only (no CRUD wiring of its own). It is served at `GET /{panel}/{resource}/cards` and reachable via a "Cards" button on the list view. Pagination and search behave exactly like the list view. When `kanban_field` names a `select` field, cards are grouped into columns keyed by that field's option values instead of a grid. Omit `card` entirely to have no card view.

#### form

```yaml
    form:
      create:
        query: CreateUser          # ACCEPTED BUT IGNORED — creation uses a raw
                                   #   INSERT built from the declared fields.
        fields:
          - name: name
            type: text
            required: true         # renders a "Required" hint (no server-side check)
          - name: email
            type: email
            required: true
          - name: password
            type: password         # bcrypt-hashed before insert
            required: true
            visible: [create]      # on create forms, only render if "create" is in
                                   #   this list; other contexts are not special-cased
          - name: role_id
            type: select
            options_query: ListRoles   # dynamic options from a SQLC query name
            options_value: id          # column for the option value. Default: "id".
            options_label: name        # column for the option label. Default: "name".
          - name: status
            type: select
            options: { active: Active, inactive: Inactive }  # static value->label map
          - name: score
            type: integer
            validation: { min: 0, max: 100 }   # renders "Min: 0, Max: 100" hint only
      update:
        populate_query: GetUser     # SQLC :one used to pre-fill the edit form on GET.
                                    #   Default: "GetByID".
        populate_params:            # ACCEPTED BUT IGNORED — the update GET handler
          id: "{record.id}"         #   calls data.New(db).<PopulateQuery>(ctx, id).
        fields:                     # SET columns on POST (raw UPDATE ... WHERE id = $N).
          - name: name
          - name: email
          - name: role_id
            type: select
            options_query: ListRoles
      delete: {}                    # presence enables the delete button + route
                                    #   (raw "DELETE FROM <table> WHERE id = $1")
```

- **create** renders a GET form and performs a raw `INSERT INTO <table> (<fields>) VALUES ($1...)` on POST, then redirects to the list. `password` fields are bcrypt-hashed first. `file`/`image` fields are saved to `static/uploads/<field>/<timestamp><ext>` and the relative URL is stored.
- **update** renders a GET form pre-filled by the `populate_query`, and on POST performs a raw `UPDATE <table> SET <col> = $N ... WHERE id = $N`. Password fields are **not** re-hashed.
- **delete** is a POST-only route (`/<panel>/<resource>/{id}/delete`) confirmed via a JS `confirm()` dialog.

#### actions

```yaml
    actions:
      - name: activate          # REQUIRED action id, used in the POST route + switch case
        label: "Activate"       # button text
        icon: check             # parsed but not yet used
        color: success          # button color: success | danger | warning | (default: gray)
        requires_confirmation: true  # parsed but not yet used
        bulk: true              # parsed but not yet used
        query: ActivateUser     # REQUIRED raw SQL executed via db.ExecContext on POST
```

Each action produces a POST route `/<panel>/<resource>/{id}/action/<name>`. On submit the handler `switch`es on the action name and runs its `query` with `int64(id)`, then redirects to the list. Unknown action names return 404.

#### policies (RBAC)

```yaml
    policies:
      view_any: "admin|manager"  # pipe-separated roles allowed for the list + CSV export
      view: "admin|manager"      # detail page
      create: "admin"            # create form GET + POST
      update: "admin|manager"    # update form GET + POST
      delete: "admin"            # delete route
```

Policies are optional. When **any** resource declares them, the generator appends an RBAC middleware to the generated app; resource routes are then wrapped with `auth.RBACMiddleware("<resource>", "<action>")` and the authenticated user's role (from the auth session) is checked against the pipe-separated role list. A `|` in a value separates allowed roles. Custom action routes never use RBAC.

#### Field types

`type` is a **UI rendering hint only** — it never defines DB column types (SQLC does). The same set applies to list `columns`, detail `fields`, and form `fields`:

| Type | List/detail display | Form input |
|---|---|---|
| `string` | Text | text input |
| `text` | Text | textarea |
| `integer` | Number | number input |
| `float` | Decimal | number input |
| `email` | mailto link | email input |
| `password` | — | password input |
| `boolean` | Check icon | checkbox |
| `select` | — | dropdown (static `options` map or `options_query`) |
| `datetime` | Formatted date-time | datetime-local input |
| `date` | Date | date input |
| `badge` | Colored badge | text input |
| `image` | Thumbnail | file input (`accept="image/*"`) |
| `file` | Download link | file input |
| `relation` | Link to related record | text input (related ID) |
| `json` | Pretty-printed | textarea (mono font) |
| `gps` | GPS coordinates (maps link) | text input (`"lat, lng"`) |

### Pages & widgets

```yaml
pages:
  - name: Dashboard     # REQUIRED. Becomes the handler name (<PanelID>Dashboard) and,
                        #   unless overridden, the route ("/dashboard").
    path: /dashboard    # custom route path. Default: "/" + name.
    default: true       # mounts the page at BOTH "/" and its path; this is the
                        #   landing page after login.
    widgets:
      - type: stat
        label: "Total Users"
        query: "SELECT COUNT(*) FROM users"   # raw SQL returning ONE scalar
        icon: users
        color: primary     # parsed but not yet used
        prefix: "$"        # text prepended to the value
      - type: stats_grid
        columns: 2         # grid columns (detected from the first stats_grid)
        widgets:           # nested stat widgets
          - type: stat
            label: "Active Users"
            query: "SELECT COUNT(*) FROM users WHERE status = 'active'"
            icon: check
      - type: chart
        label: "Monthly Revenue"
        query: "SELECT month, total FROM revenue ORDER BY month"  # raw SQL,
                                          #   must return exactly TWO columns:
                                          #   a label (string) and a value (number)
        chart:
          type: line        # line | bar | pie | area (rendered via Chart.js)
          # note: chart.query / chart.x / chart.y are parsed but NOT used —
          #   the SQL above is the single source of labels + values
      - type: table
        label: "Recent Orders"
        query: "SELECT id, customer_name, total FROM orders ORDER BY created_at DESC LIMIT 5"
        data_columns: [id, customer_name, total]   # column names to display
      - type: list
        label: "Top Products"
        # renders rows as "label : value" pairs from the raw SQL result
      - type: html
        label: "Note"
        # renders raw HTML (wired in the templ widget; not generated by the page
        #   handler yet)
```

Widget types: `stat`, `stats_grid`, `chart`, `table`, `list`, `html`.

- `stat` runs its `query` and shows the first scalar as a large number; `prefix` (and the templ's `Suffix` field) decorate it.
- `stats_grid` renders nested `stat` widgets in a grid; `columns` sizes the grid (default 4).
- `chart` runs its `query` expecting exactly two result columns (string label, numeric value), serializes them into `data-labels`/`data-values`, and renders a Chart.js canvas of `chart.type`. The nested `chart.query`/`chart.x`/`chart.y` keys are parsed but not used.
- `table` runs its `query` and renders every returned column, filtered to `data_columns` (note: the YAML key is `data_columns`, not `columns`). Rows are formatted with `%v`.
- `list` renders `label`/`value` columns of the result as a vertical list.
- `html` renders raw HTML in the templ view.

**Important:** widget `query` values are **raw SQL**, not SQLC query names. Unlike resource list/detail/actions, page widgets execute the string directly against the DB.

## Resource handler SQL strategy

Handlers use a consistent SQL approach:

- **List**: Raw SQL with dynamic WHERE/ORDER BY/LIMIT — enables search, sort, pagination without SQLC parameter limitations
- **Detail**: Calls a SQLC function (`data.GetUser(db, int64(id))`) — type-safe by ID
- **Create**: Raw SQL INSERT via `db.ExecContext` — avoids SQLC typed structs since form values are strings
- **Update GET**: SQLC populate query to pre-fill the edit form
- **Update POST**: Raw SQL UPDATE via `db.ExecContext` — same reason as create
- **Delete**: Raw SQL DELETE via `db.ExecContext`
- **Actions**: Raw SQL per action name (switch dispatch)
- **CSV Export**: Raw SQL SELECT + `encoding/csv`

## Tech stack

| Concern | Choice |
|---|---|
| Language | Go (stdlib `database/sql`) |
| Data layer | SQLC |
| Frontend | Templ (compiled Go templates) |
| CSS | Tailwind CSS |
| Router | chi |
| Auth | gorilla/sessions + bcrypt |
| Charts | Chart.js (bundled) |
| Icons | Heroicons (inline SVG) |
| Runtime | **Zero runtime dependency on go-fila** — pure code-gen |
