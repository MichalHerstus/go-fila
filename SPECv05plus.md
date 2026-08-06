# go-fila — Phase v0.5+ Specification & Implementation Plan

**Status:** Milestones 1, 2 & 3 implemented (see `session-ses_04c7.md`); M4+ planned.
**Audience:** contributors implementing phase v0.5+
**Source:** `SPEC.md` Phased Development table row `v0.5+` — *"Plugins, custom actions, hooks, file uploads, CSV export, dark mode, multi-panel"*

---

## 1. Scope and current state

The SPEC v0.5+ row lists seven deliverables. Three are already implemented and verified end-to-end against a sqlite sample app (see `session-ses_0419.md`, `session-ses_04c7.md`):

| Item | Status | Plan phase |
|---|---|---|
| Custom actions | **Done** — raw-SQL switch dispatch, `POST /{res}/{id}/action/{action}`; M1 wired `requires_confirmation`, `bulk`, `icon` | **M1 — done** |
| File uploads | **Done** — `file`/`image` fields, `saveUploadedFile`, multipart | — |
| CSV export | **Done** — M1 fixed the list button to link to `/{res}/export/csv` | **M1 — done** |
| Dark mode | **Done** — M2 wired `panel.theme.dark_mode` (class-based, toggled + persisted), brand colors, fonts, layout widths | **M2 — done** |
| Hooks | **Done** — `before`/`after` hooks on create/update/delete + custom actions; fn stubs in `internal/hooks`, inline SQL hooks, `RETURNING id` id capture | **M3 — done** |
| Plugins | **Missing** — nothing parses `plugins:` | **M4** |
| Multi-panel | **Missing** — `Config.Panel` is a single struct | **M5** |

Latent bugs folded into this phase (both fixed in M1):

- The `list` and `html` page widgets existed only in the generated templ (`templ.go:555`, `templ.go:567`) — `generatePage` (`router.go:132`) now populates them from `w.Query`.
- The CSV export button in the list view linked to `?export=csv` (`templ.go:181`) while the router registers `/{res}/export/csv` — the button now links to the route.

---

## 2. Design decisions (agreed)

1. **Plugins are generator-time.** Plugins extend go-fila at *generation* time: go-fila resolves the plugin's source, runs its `Register`/`Boot` against a `Panel` builder, and merges the contributed resources/pages/widgets/navigation/hooks into the config before code generation. Generated apps keep the core design decision of **zero runtime dependency on go-fila**. The SPEC's `Plugin` interface shape is honored via a `pkg/plugin` authoring API.
2. **Multi-panel schema is additive.** `panel:` plus top-level `resources`/`pages`/`navigation`/`auth` remain the primary panel (non-breaking). A new optional top-level `panels:` list defines additional panels, each owning its own identity, theme, auth, resources, pages, and navigation. Existing configs keep working unchanged.
3. **Scope includes v0.5 polish.** The plan also wires the remaining parsed-but-unused v0.5+ fields (action `requires_confirmation`/`bulk`/`icon`), fixes the broken `list`/`html` page widgets (required for plugins to contribute widgets), and dark mode's sibling theming (brand colors, fonts, layout widths).

Out of scope (deferred to a later phase): `auth.registration`, `auth.password_reset`, `auth.remember_me`, `auth.guard`/`provider`.

---

## 3. Milestone 1 — v0.5 polish (no schema changes) — DONE

### 3.1 Fix CSV Export button
- `templ.go` `generateListTempl`: change `exportBtn` from `?export=csv` to `fmt.Sprintf("%s/%s/export/csv", panelPath, resLower)`.
- Wrap in `templ.SafeURL(...)` — required by templ v0.3.819 on every URL-bearing attr.

### 3.2 Action `requires_confirmation`
- In list.templ and detail.templ, when `a.RequiresConfirmation` is true, add `onsubmit="return confirm('<label>?')"` to the action `<form>` (mirrors the existing delete confirmation).
- The label must be HTML-attribute-safe (single-quoted).

### 3.3 Action `icon`
- Render an inline icon in the action button when `a.Icon != ""`, reusing the `iconSVG`/`iconNav` switches in the generated templ. Add missing Heroicons to the switches as needed.

### 3.4 Action `bulk` (list view only)
- **Router:** for resources with any `bulk: true` action, register `POST /{name}/bulk/{action}` (plain `r.Post`, no RBAC — matches the custom-action route convention).
- **New generated handler** `bulk.go`: `BulkAction(db)` parses `:action` and a repeated `ids` form field, `switch`es on the action name, and loops `db.ExecContext(r.Context(), "<query>", int64(id))` per id (the query binds a single id placeholder). Redirects to the list after success.
- **list.templ:** render a checkbox column (`name="ids"`) per row plus bulk submit buttons. One outer `<form method="POST">` wraps the table so bulk selection submits to `/{path}/{res}/bulk/{action}`. Per-row non-bulk actions (view/edit/actions/delete) must not be nested inside that form — use explicit `formaction` + `formmethod` on their buttons, or emit them outside the bulk form. **Decision:** one bulk form around the table; non-bulk row buttons use `formaction`/`formmethod`.

### 3.5 `list` and `html` page widgets
- `router.go` `generatePage` gains two handler cases:
  - `case "list"`: run `w.Query`, expect `label` + `value` columns → `WidgetData{Type: "list", TableRows: [...], Label: w.Label}` (the templ at `templ.go:555` already renders `row["label"]` / `row["value"]`).
  - `case "html"`: run `w.Query`, take the first scalar → `WidgetData{Type: "html", Value: template.HTML(scalar), Label: w.Label}`.
  - Honor `w.Limit` for table/list (`LIMIT n` appended when set).

**Exit criteria:** `go build ./...`, `go vet ./...`, `gofmt -l .` clean; sqlite e2e smoke — export link returns 200, bulk action flips multiple rows, list/html widgets render real data, confirmation attribute present.

---

## 4. Milestone 2 — Dark mode + theming — DONE

### 4.1 Tailwind config (`tailwind.go`)
- Add `darkMode: 'class'` to `tailwind.config.js`.
- Add `theme.extend.colors.brand = { primary: "<hex>", secondary: "<hex>" }` from YAML `brand.colors` and `theme.extend.fontFamily.sans/mono` from `theme.font` (empty-safe).
- Replace hardcoded `indigo` accents in generated views with `brand-primary`/`brand-secondary` classes so configured brand colors apply. Mechanical but touches every templ emitter.

### 4.2 Dark: variants (`templ.go`, `auth.go`)
Apply `dark:` variants to all shared surfaces: `bg-white` → `bg-white dark:bg-gray-800`, `text-gray-900` → `dark:text-gray-100`, borders, table headers/rows, sidebar, topbar, pagination, form inputs, badges. Concentrate in `renderersSource()`, `generateLayoutViews()` (base/sidebar/topbar), list/detail/form/page emitters, and the auth `login.templ`. The light theme must remain byte-identical where possible.

### 4.3 Toggle + persistence (`base.templ`)
- `<html class="dark">` served by default when `panel.theme.dark_mode: true`.
- Topbar toggle button + inline JS: read `localStorage['gf-theme']`, toggle `.dark` on `<html>`, persist. When `dark_mode: false` the toggle still works (dark classes stay compiled; dark mode is opt-in per panel).
- Chart.js auto-render picks its line color from the `--brand-primary` CSS variable (or a `.dark` class check) instead of hardcoded `#6366f1`.

### 4.4 Layout wiring
- Sidebar: `style="width: <width>px"` plus `collapsed_width`; `toggleSidebar()` JS flips the width and the Topbar `ml-*` offset matches. `collapsible: false` hides the toggle button.
- Topbar: `sticky` only when `panel.layout.topbar.sticky` (currently always sticky).
- `max_content_width`: apply `max-w-{value} mx-auto` to `<main>`.
- Because `Base`/`Sidebar`/`Topbar` signatures change and are called from resource, page, and auth handlers, add a generated `viewmodels.ThemeConfig` struct (dark flag, brand hexes, font stacks, sidebar widths, sticky, max width) passed through every `views.Base(...)` call site. All call sites are generated, so this is mechanical.

**Exit criteria:** build + visual smoke — light/dark toggle persists across reload, brand color renders on buttons/links, sidebar collapses.

---

## 5. Milestone 3 — Hooks — DONE

### 5.1 YAML schema (`types/hook.go`, `types/resource.go`)
```go
type Hook struct {
    Name string `yaml:"name"`          // identifier (used for generated stub names)
    Fn   string `yaml:"fn"`            // Go func in internal/hooks (user-implemented)
    SQL  string `yaml:"sql"`           // raw SQL executed inline (alternative to fn)
}
type Hooks struct {
    Before []Hook `yaml:"before"`
    After  []Hook `yaml:"after"`
}
```
Attach `Hooks *Hooks` to `FormAction` (create/update/delete) and to `Action` (custom actions).

YAML shape:
```yaml
resources:
  - name: User
    form:
      create:
        hooks:
          before:
            - name: validate_domain
              fn: ValidateUserDomain
            - name: create_audit
              sql: "INSERT INTO audit_log (table_name, action) VALUES ('users', 'create')"
          after:
            - name: notify
              sql: "INSERT INTO notifications (target, msg) VALUES ($1, 'user created')"
    actions:
      - name: deactivate
        query: "UPDATE users SET status = 'inactive' WHERE id = $1"
        hooks:
          before: []
          after: []
```

### 5.2 Generated hooks package (new `internal/generator/hooks.go`)
- `ensureDirs` adds `internal/hooks/`.
- Generate `internal/hooks/hooks.go`:
```go
package hooks

type Scope struct {
    ID     int64
    Table  string
    Action string // create|update|delete|<actionName>
    Values map[string]interface{}
}

// <Fn> — one stub per declared fn hook; user implements.
func <Fn>(ctx context.Context, db *sql.DB, s Scope) error { return nil }
```
Stubs compile out of the box; the user fills them in. The generated handlers import this package.

### 5.3 Handler wiring (`handler.go`)
- **create.go POST:** build `Scope{Table, Action: "create", Values}` → run `before` hooks → INSERT → **switch INSERT from `db.ExecContext` to a driver-aware `QueryRowContext(...).Scan(&newID)` using `RETURNING id`** (both postgres and mattn sqlite ≥3.35 support `RETURNING`) → set `Scope.ID = newID` → run `after` hooks → redirect.
- **update.go POST:** before → UPDATE → after (id known).
- **delete.go:** before → DELETE → after.
- **actions.go:** per case `{ before → ExecContext(query, id) → after }` inside the block scope (block scope is required — AGENTS.md gotcha).
- `fn` hooks call `hooks.<Fn>(r.Context(), db, scope)`; `sql` hooks are inlined as `db.ExecContext(r.Context(), "<sql>", scope.ID)` (the scope id is 0 for before-create, the new row id after-create, and the path id for update/delete/actions).
- **The generated handler imports the hooks package whenever a hook block is declared** (not only when an fn hook exists) — the `hooks.Scope{...}` literal itself lives in that package, so a sql-only hook still needs the import. Verified against a sqlite demo: `before_create` fn + `after_create`/`after_delete` sql hooks + an action fn hook all fire and are observable in an audit table.
- **Sprintf format-specifier drift is the #1 risk here** (AGENTS.md) — this milestone adds many new `%s` emissions. Every hook SQL string is passed as a Sprintf *arg* (`%q`), never concatenated into a template.

**Exit criteria (met):** e2e — a `before_create` fn hook and an `after_delete` sql hook both fire and are observable (e.g., audit-table rows). sqlite create id capture via `RETURNING id` verified (`QueryRowContext(...).Scan(&newID)`); mssql uses `OUTPUT INSERTED.<id>` (best-effort, untested on live MSSQL).

---

## 6. Milestone 4 — Plugins (generator-time)

### 6.1 Authoring API (new `pkg/plugin/`)
Faithful to the SPEC interface, resolved at generate time:
```go
package plugin

type Plugin interface {
    ID() string
    Register(p *Panel) error
    Boot(p *Panel) error
}

// Optional; detected via type assertion by the loader.
type Configurer interface {
    Configure(cfg map[string]any) error
}

type Panel struct{ /* internal builders */ }

func NewPanel() *Panel
func (p *Panel) AddResource(r types.Resource) error   // errors on name collision
func (p *Panel) AddPage(pg types.Page) error
func (p *Panel) AddNavigationGroup(g types.NavigationGroup)
func (p *Panel) AddHook(h types.Hook)
func (p *Panel) AddSQLFile(name, content string)      // written into the out sql/ tree before sqlc
func (p *Panel) Manifest() Manifest                   // JSON-serializable snapshot
```
`Manifest` reuses `types.Resource`/`Page`/`NavigationGroup`/`Hook`. The structs only carry `yaml:` tags, so JSON round-trips via Go field names — the loader decodes back into the same types. Plugin authors import `github.com/go-fila/go-fila/pkg/plugin`.

### 6.2 YAML (`types/plugin.go`, `types/config.go`, `parser/validator.go`)
```go
type PluginConfig struct {
    Name   string         `yaml:"name"`
    Source string         `yaml:"source"` // local dir ("./plugins/audit") or module path ("github.com/...")
    Config map[string]any `yaml:"config"`
}
// Config gains:
Plugins []PluginConfig `yaml:"plugins"`
```
Validate `name`/`source` non-empty; reject duplicate plugin names.

### 6.3 Loader (new `internal/generator/plugin.go`)
Insert `g.loadPlugins()` in `Generate()` **after** `copySQLFiles()` (plugin SQL must land in the out dir before sqlc runs) and **before** resource/page generation. Per plugin:
1. Resolve `source`: local directory (starts with `.`/`/`) or module import path.
2. Write a shim into `os.TempDir()`:
   - temp `go.mod` requiring `github.com/go-fila/go-fila` and the plugin module; local dirs get a `replace <mod> => <abs>`.
   - `shim.go`: `p := pluginapi.NewPanel()`, type-assert `Configurer` and call `Configure` with the YAML `config`, then `Register(p)`, `Boot(p)`, and `json.NewEncoder(os.Stdout).Encode(p.Manifest())`.
3. `go mod tidy` + `go run shim.go` (network needed for module-path sources). **Plugin load failure is a fatal error** — an explicitly declared plugin that fails to load is a config error, unlike the non-fatal sqlc/tailwind steps.
4. Decode the manifest → append to `cfg.Resources`, `cfg.Pages`, `cfg.Navigation`, `cfg.Hooks`; write `SQLFiles` into `OutDir/sql/{queries,migrations}`; print a summary under `--verbose`.
5. Add a `--skip-plugins` CLI flag as an escape hatch.

### 6.4 Deliverable
- Example plugin under `examples/plugins/audit/` that contributes a resource, a dashboard widget, a nav group, and an SQL file.
- README + SPEC documentation.

**Exit criteria:** a sample `audit` plugin loads, contributes an `audit_log` resource + stat widget + nav group; a `go-fila generate` with no plugins produces output byte-identical to the end of M3 (regression).

---

## 7. Milestone 5 — Multi-panel (additive)

### 7.1 YAML (`types/panelconfig.go`, `types/config.go`, `parser/validator.go`)
```go
// Config gains:
Panels []PanelConfig `yaml:"panels"` // additional panels; primary = top-level panel:

type PanelConfig struct {
    ID         string
    Path       string
    Name       string
    Brand      Brand
    Layout     Layout
    Theme      Theme
    Auth       AuthConfig
    Navigation []NavigationGroup
    Resources  []Resource
    Pages      []Page
}
```
Validate: `id`/`path`/`name` required, `path` starts with `/`, panel paths unique across primary + extras, each additional panel has at least one resource or page.

### 7.2 Generator refactor (largest change)
- `Generator` gains a normalized `Panels []*PanelConfig` (the primary panel is normalized into the same shape as index 0).
- Split `Generate()`:
  - **Shared once:** `ensureDirs`, `generateSQLCConfig`, `copySQLFiles`, `loadPlugins`, `generateGoMod`, `generateViewModels`, `generateAssets`, `generateMain`.
  - **Per panel** (new `generatePanel(p *PanelConfig)`): router, auth, resources, pages, views.
- Every existing generator method is parameterized by `*PanelConfig` (they currently read `g.Config.Panel`, `g.Config.Resources`, etc.). Per-panel output directories:
  - `internal/panel/{id}/router.go`
  - `internal/panel/{id}/auth/`
  - `internal/panel/{id}/resources/`
  - `internal/panel/{id}/pages/`
  - `internal/views/{id}/...` (layout, resources, pages, widgets, components)
- Shared: `internal/data/` (sqlc), `internal/viewmodels/`, static assets.
- Each panel's `NewRouter(db)` is self-contained with its own SessionMiddleware/AuthMiddleware and a **namespaced session cookie** `go-fila-session-{panelID}` so auth is isolated per panel. Routers register routes at the root and are mounted by main.go.

### 7.3 main.go (`generateMain`)
```go
r := chi.NewRouter()
r.Use(middleware.Logger, middleware.Recoverer)
r.Handle("/static/*", ...)   // static serving moves to the top-level router (once)
r.Handle("/uploads/*", ...)
r.Mount("<panel1.path>", admin.NewRouter(db))
r.Mount("<panel2.path>", app.NewRouter(db))
```
This preserves the "no two `r.Route` on the same path" and "no `r.Use` after registered routes" constraints (AGENTS.md) by construction.

### 7.4 Regression guard
Capture a golden output of a single-panel config **before** the refactor; assert the post-refactor tree is identical except for the directory-shape change (same filenames/names).

**Exit criteria:** a config with `/admin` + `/app` panels (separate auth tables and resources) builds and runs; sessions do not leak across panels.

---

## 8. Verification strategy

- **Unit tests** (the repo currently has none — introduce a test suite):
  - parser/validator tests for new YAML (plugins, panels, hooks, theme, action flags).
  - generator golden tests: run `Generate()` into `t.TempDir()` for fixture configs; assert key snippets (`RETURNING id`, `hooks.` calls, `dark:` classes, `Mount(`) and that generated `.templ` + `.go` are syntactically sane.
  - plugin loader test: feed a canned manifest JSON and assert the merge into the config.
- **E2E smoke** (sqlite, per milestone): `go-fila init` → extend YAML → `go-fila generate` → `go tool templ generate` → `go build ./...` → run → curl login/list/create/action/bulk/delete/hook/export.
- Every milestone ends with `go build ./...`, `go vet ./...`, `gofmt -l .`, plus a templ + tailwind compile of a generated project.

---

## 9. Documentation updates (after each milestone)

- **SPEC.md:** expand the Plugin System section to the generator-time model, add a Hooks section, document dark mode/theming + layout wiring and the multi-panel schema, and update the Phased Development table to mark v0.5+ items.
- **README.md:** YAML reference for `plugins:`, `panels:`, hooks, and theme.
- **AGENTS.md:** new gotchas — hook emission + `RETURNING id` + driver-awareness of id capture, the plugin loader (fatal on failure, `--skip-plugins` flag), per-panel output dirs and cookie namespacing, `templ.SafeURL` on all new bulk/export links, and the export-link fix.

---

## 10. Key risks

1. **Sprintf format-specifier drift** in `handler.go` emissions — hooks and bulk actions add many `%s`/`%d`. This is the documented #1 failure mode.
2. **Multi-panel refactor** touches every generator file — mitigate with a golden test captured before M5.
3. **Plugin module resolution** requires the Go toolchain and network for module-path sources; local-directory sources via `replace` are the low-risk path to ship first.
4. **`RETURNING id`** on sqlite requires mattn/go-sqlite3 ≥3.35 (current `v1.14.24` bundles it) — verify in the M3 smoke.
5. **templ v0.3.819 strictness** — every new URL-bearing attr must use `templ.SafeURL` or the generated build fails.

---

## 11. Suggested implementation order

**M1 → M2 → M3 → M4 → M5**, each milestone self-contained and independently verifiable.
