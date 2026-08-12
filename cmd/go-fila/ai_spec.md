# go-fila.yaml schema cheat-sheet

Compact reference for the go-fila admin panel generator. Top-level keys:
`version`, `panel`, `connections`, `sqlc`, `auth`, `navigation`, `resources`,
`pages`, `plugins`.

## panel

- `panel.id` — string, default `"admin"`
- `panel.path` — string, required, must start with `/`
- `panel.name` — string, required
- `panel.brand.logo`, `panel.brand.favicon` — strings
- `panel.brand.colors.primary`, `panel.brand.colors.secondary` — hex colors
- `panel.layout.sidebar.collapsible` (bool), `sidebar.width` (int),
  `sidebar.collapsed_width` (int)
- `panel.layout.topbar.sticky` (bool)
- `panel.layout.max_content_width` — string (e.g. `"1200px"`)
- `panel.theme.dark_mode` (bool)
- `panel.theme.font.family`, `panel.theme.font.mono` — strings

## connections

Map of connection name → settings:
```yaml
connections:
  default:
    driver: postgres   # postgres (default), sqlite, mssql
    dsn: "postgres://..."
    pool: { max_open: 10, max_idle: 5, lifetime: 30m }
```

## sqlc

- `sqlc.config` — default `sqlc.yaml`
- `sqlc.queries_dir` — default `./sql/queries`
- `sqlc.schema_dir` — default `./sql/migrations`
- `sqlc.output_pkg` — default `internal/data`

## auth

- `auth.guard` — `web`; `auth.provider` — `session`
- `auth.table` — auth table name, e.g. `users`
- `auth.login.fields` — list, e.g. `[email, password]`
- `auth.login.redirect` — path after login, e.g. `/admin/dashboard`
- `auth.login.rate_limit.max_attempts` / `rate_limit.window_seconds` — ints
- `auth.registration`, `auth.password_reset`, `auth.remember_me` — bools

## navigation

```yaml
navigation:
  - group: "Management"
    icon: users
    sort: 1
    items:
      - resource: User       # or page: <name>, or url: <href>
        label: Optional       # overrides default label
        type: resource        # resource | page | external
        opens_in_new_tab: false
```

## resources

Each resource (PascalCase `name`; becomes lowercased URL segment):

```yaml
- name: User
  label: Users
  icon: users
  group: Management
  table: users             # optional table-name override
  id_type: int64           # optional (e.g. int64 for bigint PKs)
  id_column: id            # optional (e.g. ID when PK isn't lowercase "id")
  list:
    query: ListUsers       # SQLC query name (identifier, no spaces)
    count_query: CountUsers
    per_page: 20
    default_sort: -created_at   # leading "-" = descending
    columns:
      - name: email
        label: Email
        type: email        # integer|string|text|email|password|boolean|badge|datetime|date|image|file|select|relation|json|float|gps
        sortable: true
        searchable: true
        options: {active: Active}   # badge/select display options
  card:
    fields: []             # same field shape as form fields
    columns: 4             # cards per row
    rows: 4                # rows per page
    kanban_field: status   # optional select field => kanban board
    searchable: [name]
    default_sort: -created_at
  detail:
    query: GetUser
    params: {id: "{record.id}"}
    fields: []             # same field shape as form fields
  form:
    create:
      query: CreateUser
      populate_query: ""   # not used on create
      fields:
        - name: email
          label: Email
          type: email
          required: true
          visible: [create, update]   # contexts; omit = everywhere
          validation: {min: 2, max: 100}
          options_query: ListRoles    # SQLC name for select/relation
          options_value: id
          options_label: name
          options: {active: Active}   # static select options
    update:
      query: UpdateUser
      populate_query: GetUser
      fields: []
    delete:
      query: DeleteUser
      hooks: null          # see hooks below
  actions:
    - name: archive
      label: Archive
      icon: check
      color: warning
      requires_confirmation: true
      bulk: true
      query: "UPDATE users SET status = 'archived' WHERE id = $1"   # or proc:
      proc: ArchiveUser    # stored proc (mutually exclusive with query)
      policy: admin|manager    # role restriction, "|" separated
      hooks: null
  policies:
    view_any: admin|manager
    view: admin|manager
    create: admin
    update: admin
    delete: admin
```

Form/detail field shape: `name`, `label`, `type`, `required` (bool),
`visible` (list of create/update), `validation` (`{min, max}`),
`options_query` + `options_value` + `options_label` (SQLC-backed select),
`options` (static map key→label).

## pages

```yaml
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stat        # stat | chart | table | stats_grid | list | html
        label: Total Users
        query: "SELECT COUNT(*) FROM users"
        icon: users
        color: blue
        prefix: ""
      - type: chart
        label: Revenue
        query: "SELECT month, total FROM revenue"
        chart: {type: line, x: month, y: total}   # line|bar|pie|area
      - type: table
        label: Recent orders
        query: "SELECT * FROM orders LIMIT 5"
        data_columns: [id, total]
      - type: stats_grid
        columns: 2
        widgets: [ ... ]   # nested stat widgets
```

Widget `query` may be inline SQL (spaces allowed) — it is executed raw.

## plugins

```yaml
plugins:
  - name: audit
    source: ./plugins/audit   # or github.com/...
    config: {enabled: true}
```

## rules

- Keep the top-level `version` field unchanged.
- Named query references (`list.query`, `count_query`, `options_query`,
  form `query`/`populate_query`, `detail.query`, action `query`) are single
  identifiers without spaces, matching a `-- name: X` block in
  `sql/queries/*.sql`. Widget and action `query` values may be inline SQL.
- `panel.path` and every page `path` must start with `/`.
- Resource names are PascalCase; the name becomes the lowercased URL segment.
- `options` maps are `key: label` (key is the stored value).
- Hooks: each hook has a `name` and exactly one of `fn` (Go func),
  `sql` (raw SQL), or `proc` (stored proc; sqlite ignores procs).
- Do not invent keys outside this cheat-sheet; preserve unrelated sections of
  the current config verbatim.

## AI edit output

`go-fila edit --prompt "…"` returns ONLY the changed sections of config as a
YAML fragment in a ```yaml fence:

- Include only the top-level keys you changed (`panel`, `connections`, `sqlc`,
  `auth`, `navigation`, `resources`, `pages`, `plugins`), nested only as deep
  as the change. Never return unchanged sections.
- Keep `version` unchanged — do not include it in the fragment.
- Lists identified by `name` (`resources`, `pages`, `fields`, `actions`):
  return only the changed items, each with its `name`; matched items merge,
  new items are appended.
- `navigation` groups are keyed by `group`; their `items` by
  `resource` / `page` / `url` (whichever the item uses).
- Lists without an identity key (e.g. page `widgets`) are replaced wholesale —
  return the complete replacement list.
- Omit unchanged keys; a null value leaves the existing value untouched
  (deletion is not supported). Do not invent YAML keys.
