# go-fila — Software Specification

**go-fila** is a YAML-driven admin dashboard generator for Go, inspired by FilamentPHP. It reads a declarative YAML specification and generates a fully functional admin panel with CRUD resources, pages, widgets, authentication, navigation, and theming — all without writing boilerplate Go code.

---

## Core Architecture

```
User writes:
  ├── go-fila.yaml         (panel config, resources, pages, navigation, auth)
  ├── sql/schema.sql       (DDL — CREATE TABLE)
  └── sql/queries/*.sql    (annotated SQLC queries)

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
      query: ListUsers           # SQLC :many function name
      count_query: CountUsers    # SQLC :one for pagination
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

    # ── POLICIES ────────────────────────────────────
    policies:
      view_any: "admin|manager"
      view: "admin|manager"
      create: "admin"
      update: "admin|manager"
      delete: "admin"
```

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
| `select` | Dropdown (static or SQLC-backed) |
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
  go-fila generate       Run SQLC + generate admin panel Go application
  go-fila validate       Validate YAML + verify SQLC function references resolve
  go-fila version        Print version information

Flags:
  --config, -c   Path to YAML config file (default: go-fila.yaml)
  --out, -o      Output directory (default: ./admin)
  --force        Overwrite existing files
  --demo         With init: seed sqlite demo DB; login admin@demo.test / admin
  --verbose      Enable verbose logging
```

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

Plugins extend go-fila by registering resources, pages, widgets, and navigation:

```yaml
plugins:
  - name: audit
    source: github.com/go-fila/plugin-audit
    config:
      retention_days: 90
```

Plugin Go interface:

```go
type Plugin interface {
    ID() string
    Register(p *Panel) error
    Boot(p *Panel) error
}
```

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
│       └── main.go            # CLI entry point
├── internal/
│   ├── parser/                # YAML parsing & validation
│   │   ├── schema.go
│   │   └── validator.go
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
