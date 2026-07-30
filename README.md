# go-fila

**go-fila** is a YAML-driven admin dashboard generator for Go. Write a declarative YAML config + SQL queries, and it generates a fully functional admin panel with CRUD resources, custom pages, widgets, authentication, RBAC — no boilerplate.

## Prerequisites

| Tool | Required for | Notes |
|---|---|---|
| [Go](https://go.dev/dl/) 1.26+ | Running go-fila + building generated app | |
| [SQLC](https://docs.sqlc.dev/en/latest/overview/install.html) | Generating the data layer (`internal/data/`) | Generator runs it, failure is non-fatal |
| [Node.js](https://nodejs.org/) + npm | Building Tailwind CSS in the generated app | Run `npm install && npm run build:css` in the output dir |
| [Templ](https://templ.dev/) | Compiling `.templ` files in the generated app | `go tool templ generate` in the output dir |

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
npm install
npm run build:css             # build Tailwind
go mod tidy
go build ./...
```

The `init` command fails if files already exist unless `--force` is passed.

## CLI

```
go-fila init           Scaffold go-fila.yaml + sqlc.yaml + sql/ + working example
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
│   └── generator/              # Code generation pipeline (10 files)
│       ├── generator.go        # Orchestrator
│       ├── handler.go          # Per-resource handlers (list, detail, create,
│       │                       #   update, delete, action, CSV export)
│       ├── templ.go            # All .templ views
│       ├── router.go           # Chi router + RBAC + page handlers
│       ├── auth.go             # Login/logout/session/RBAC middleware
│       ├── main.go             # Generated server entrypoint
│       ├── mod.go              # Generated go.mod
│       ├── viewmodels.go       # View data structs
│       ├── tailwind.go         # Tailwind config + static assets
│       └── sqlc.go             # SQLC config + query lookup
├── examples/
│   ├── minimal/go-fila.yaml    # Minimal working config
│   └── full/                   # Full-featured example
├── SPEC.md                     # Authoritative spec
└── AGENTS.md                   # Agent instructions
```

## Generated output structure

```
output/
├── main.go                          # Chi router, DB pool, server start
├── go.mod / go.sum
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

## YAML config

The config file (`go-fila.yaml`) defines your admin panel. See `SPEC.md` for the full schema. Key sections:

### Panel

```yaml
panel:
  id: admin
  path: /admin
  name: "My Admin"
  brand:
    logo: /assets/logo.svg
    colors:
      primary: "#6366f1"
  layout:
    sidebar:
      collapsible: true
      width: 280
    max_content_width: 7xl
  theme:
    dark_mode: true
```

### Resources

Each resource has three independent views, each backed by its own SQLC query:

```yaml
resources:
  - name: User
    label: Users
    icon: users
    group: "User Management"

    list:
      query: ListUsers               # SQLC :many
      count_query: CountUsers         # SQLC :one — pagination total
      columns:                        # UI rendering — name matches SQLC struct field
        - name: name
          type: string
          searchable: true
        - name: email
          type: email
          sortable: true
        - name: role_name
          label: Role
        - name: status
          type: badge
          options:
            active: success
            inactive: warning
      default_sort: -created_at       # prefix "-" = descending

    detail:
      query: GetUser                 # SQLC :one
      params:
        id: "{record.id}"
      fields:                         # Display fields in detail view
        - name: name
        - name: email
        - name: role_name
          label: Role
        - name: status
          type: badge
          options: { active: success, inactive: warning }

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
            visible: [create]         # Only in create form, not update
          - name: role_id
            type: select
            options_query: ListRoles  # Dynamic options from SQLC query
            options_value: id
            options_label: name
          - name: status
            type: select
            options: { active: Active, inactive: Inactive }
      update:
        populate_query: GetUser       # Query to pre-fill the form
        fields:
          - name: name
          - name: email
          - name: role_id
            type: select
            options_query: ListRoles
      delete: {}                      # Enable delete button on detail page

    actions:                          # Custom action buttons
      - name: activate
        label: "Activate"
        icon: check
        color: success
        requires_confirmation: true
        query: ActivateUser

    policies:                         # RBAC roles
      view_any: "admin|manager"
      create: "admin"
      update: "admin|manager"
      delete: "admin"
```

#### Field types

| Type | Display / Input |
|---|---|
| `string` | Text / text input |
| `text` | Text / textarea |
| `integer` | Number / number input |
| `float` | Decimal / number input |
| `email` | Mailto link / email input |
| `password` | Hidden / password input |
| `boolean` | Check icon / checkbox toggle |
| `select` | Dropdown (static options or SQLC-backed) |
| `datetime` | Formatted date-time / datetime-local input |
| `date` | Date / date input |
| `badge` | Colored badge |
| `image` | Thumbnail / file input (accepts image/*) |
| `file` | Download link / file input |
| `relation` | Link to related record |
| `json` | Pretty-printed / textarea (mono font) |

### Pages & widgets

```yaml
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stat
        label: "Total Users"
        query: CountUsers
        icon: users
        color: primary
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
        columns: [id, customer_name, total, status]
        limit: 5
```

Widget types: `stat`, `stats_grid`, `chart` (line/bar/pie/area), `table`, `list`, `html`.

### Auth

```yaml
auth:
  guard: web
  provider: session
  table: users                     # DB table for auth
  login:
    fields: [email, password]
    redirect: /admin/dashboard
  registration: false
  password_reset: true
  remember_me: true
```

### Navigation

```yaml
navigation:
  - group: "User Management"
    icon: users
    sort: 1
    items:
      - resource: User
      - resource: Role
  - group: "Analytics"
    icon: chart
    items:
      - page: Dashboard
      - type: link
        label: "Google Analytics"
        url: https://analytics.google.com
        opens_in_new_tab: true
```

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
