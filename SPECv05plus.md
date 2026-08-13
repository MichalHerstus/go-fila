# yaga — Phase v0.5+ Specification & Implementation Plan

**Status:** Milestones 1, 2 & 3 implemented (see `session-ses_04c7.md`); M4 (plugins) planned with finalized design in §6; M5+ planned.
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

1. **Plugins are generator-time.** Plugins extend yaga at *generation* time: yaga resolves the plugin's source, runs its `Register`/`Boot` against a `Panel` builder, and merges the contributed resources/pages/widgets/navigation/hooks into the config before code generation. Generated apps keep the core design decision of **zero runtime dependency on yaga**. The SPEC's `Plugin` interface shape is honored via a `pkg/plugin` authoring API.
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
- Topbar toggle button + inline JS: read `localStorage['yaga-theme']`, toggle `.dark` on `<html>`, persist. When `dark_mode: false` the toggle still works (dark classes stay compiled; dark mode is opt-in per panel).
- Chart.js auto-render picks its line color from the `--brand-primary` CSS variable (or a `.dark` class check) instead of hardcoded `#6366f1`.

### 4.4 Layout wiring
- Sidebar: `style="width: <width>px"` (via `data-width` + a JS init); `toggleSidebar()` JS flips `aside.style.display` between shown and hidden (the sidebar no longer collapses to a narrow strip). `collapsible: false` hides the toggle button.
- Topbar: `sticky` only when `panel.layout.topbar.sticky` (currently always sticky). Left group holds the nav-toggle + dark-mode toggle buttons; right group shows the logged-in user name (from the session, set at login) before the Logout link.
- `max_content_width`: apply `max-w-{value} mx-auto` to `<main>`.
- Because `Base`/`Sidebar`/`Topbar` signatures change and are called from resource, page, and auth handlers, add a generated `viewmodels.ThemeConfig` struct (dark flag, brand hexes, font stacks, sidebar widths, sticky, max width) passed through every `views.Base(...)` call site. All call sites are generated, so this is mechanical.

**Exit criteria:** build + visual smoke — light/dark toggle persists across reload, brand color renders on buttons/links, sidebar shows/hides.

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

**Model:** generator-time subprocess. yaga runs each plugin in a throwaway module, collects a JSON *manifest* of contributions, and merges it into the config before code generation. The generated app keeps its zero runtime dependency on yaga. Plugin load failure is **fatal** (an explicitly declared plugin that fails to load is a config error), unlike the non-fatal sqlc/tailwind steps.

### 6.1 Prerequisite fix — hooks.go for SQL-only hooks
`generateHooks()` currently writes `internal/hooks/hooks.go` **only when fn hooks exist**, but handlers emit `hooks.Scope{...}` + `import hooks` for *any* hook block (sql-only included). A config with only SQL hooks — and plugin-added SQL hooks — would generate an app that does not compile. **Fix:** `generateHooks` writes `hooks.go` whenever any hook is declared (fn or sql); stubs are emitted only for fn hook names.

### 6.2 Authoring API (new `pkg/plugin/`)
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
func (p *Panel) AddResource(r Resource) error            // errors on name collision
func (p *Panel) AddPage(pg Page) error                   // Path defaults to "/"+Name
func (p *Panel) AddNavigationGroup(g NavigationGroup)
func (p *Panel) AddSQLFile(name, content string)         // "queries/…" | "migrations/…"
func (p *Panel) AddHookToResource(resource, action, when string, h Hook) error
func (p *Panel) Manifest() Manifest                      // JSON-serializable snapshot

type Manifest struct {
    Resources       []Resource
    Pages           []Page
    Navigation      []NavigationGroup
    HookAttachments []HookAttachment
    SQLFiles        map[string]string
}

type HookAttachment struct {
    Resource string // existing resource name (e.g. "Customer")
    Action   string // "create" | "update" | "delete" | <custom action name>
    When     string // "before" | "after"
    Hook     Hook   // SQL/proc hooks and fn hooks (fn backed by AddHookSource, M5)
}
```
`pkg/plugin` re-exports `internal/types` under public aliases (`type Resource = types.Resource`, plus `Page`, `Widget`, `NavigationGroup`, `NavigationItem`, `Column`, `Field`, `Hook`, `Hooks`, `ListConfig`, `DetailConfig`, `FormConfig`, `FormAction`, `CardConfig`, `Action`, `Policy`, `ChartConfig`, `Validation`, `HookAttachment`) — plugins are a separate module and cannot import `internal/*`. Structs carry only `yaml:` tags, so JSON round-trips via Go field names; the loader decodes back into the same types. Plugin authors import `github.com/MichalHerstus/yaga/pkg/plugin`.

**`AddHookToResource` semantics:** appends the hook to the target resource's `Before`/`After` list at merge time (same data the existing generator already emits hooks from). The SQL/proc string binds the current record id as `$1` (parity with the existing `hookCallsStr`, which passes `scope.ID`): `0` for before-create, the new row id after-create, the parsed path id otherwise. Only `$1` (the id) is bound for SQL/proc hooks; `Scope.Values` is available to fn hooks. **fn hooks are merged when the fn name is backed by a plugin hook source** (M5); an fn hook without a matching source is a fatal merge error. `proc` hooks are emitted as `CALL/EXEC` and ignored on sqlite.

### 6.3 YAML (`types/plugin.go`, `types/config.go`, `parser/validator.go`)
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

### 6.4 Loader (new `internal/generator/plugin.go`)
`Generator` gains `SkipPlugins bool`; insert `g.loadPlugins()` in `Generate()` **after** `copySQLFiles()` (plugin SQL must land in the out dir before sqlc runs) and **before** resource/page generation. Per plugin:
1. Resolve `source`: local directory (starts with `.`/`/`, read its `go.mod` `module` line) or module import path.
2. Write a shim into a temp dir (`yaga-plugin-shim/`):
   - `go.mod` requiring `github.com/MichalHerstus/yaga` (resolved via local checkout when found by walking up from `os.Executable()` for a `go.mod` declaring `module github.com/MichalHerstus/yaga`, else from the proxy) and the plugin module; local dirs get a `replace <mod> => <abs>`.
   - `main.go`: `p := pluginapi.NewPanel()`; if `New()` returns a `Configurer`, call `Configure` with the YAML `config` embedded as JSON; then `Register(p)`, `Boot(p)`, `json.NewEncoder(os.Stdout).Encode(p.Manifest())`. Any error → non-zero exit.
3. `go mod tidy` + `go run .` (network needed for module-path sources).
4. Decode the manifest → append to `cfg.Resources`, `cfg.Pages`, `cfg.Navigation`; resolve each `HookAttachment` against the merged resource set (missing resource/action → fatal; append to the target `Before`/`After` list in plugin order); write `SQLFiles` into `OutDir/sql/<name>` (never overwrite existing files); print a summary under `--verbose`.
5. Add a `--skip-plugins` CLI flag as an escape hatch (`parseGlobalFlags` + `cmdGenerate` → `gen.SkipPlugins`).
6. Convention: a plugin module exposes `func New() plugin.Plugin` at its root package; the package name must equal the last element of the module path.

### 6.5 Deliverable
- Example plugin under `examples/plugins/audit/` (`go.mod` module `github.com/yaga/plugin-audit` + `audit.go`): `New()` → `Configure(table, retention_days)` → `Register` adds an `AuditLog` resource, an `AuditOverview` page with two stat widgets, an "Audit" nav group, `AddSQLFile("migrations/audit_schema.sql")` + `AddSQLFile("queries/audit.sql")`, and an `AddHookToResource("Customer", "delete", "after", …)` demonstrating SQL-hook attachment.
- README + SPEC documentation.

### 6.6 Tests
- Unit: manifest JSON decode → config merge (resource/page/nav append, hook-attachment resolution, missing-resource error, sql-file write + no-overwrite).
- Loader integration (skip if no `go`): build a tiny plugin module in `t.TempDir()`, run `loadPlugins()`, assert merged config; assert `--skip-plugins` no-ops.
- Regression: no plugins → output unchanged (existing generator tests keep passing).
- New: SQL-only hooks now emit a compiling `internal/hooks/hooks.go` (prerequisite fix).

**Exit criteria:** a sample `audit` plugin loads, contributes an `audit_log` resource + stat widget + nav group, and a Customer delete inserts an `audit_log` row via the plugin's SQL hook; `yaga generate` with no plugins produces output byte-identical to the end of M3 (regression).

### 6.7 M5 (implemented 2026-08-13)
Plugin **fn hooks**: `Panel.AddHookSource(name, content)` writes a `package hooks` Go file into `OutDir/internal/hooks/` (compiles inside the generated app, so it may reference `hooks.Scope`); the loader tracks plugin-provided fn names and `generateHooks` skips stub generation for them; fn `HookAttachment`s are merged instead of rejected, and an fn hook without a matching hook source is fatal at merge time. A bug in plugin hook source surfaces as a generated-app build error, not a yaga error (same as hand-written user hooks). `generateHooks` also emits `hooks.go` (Scope only) when plugin hook sources exist even if no YAML hook block is declared, so the plugin files compile. Verified e2e: the audit example's `LogCustomerCreated` fn hook fires on customer create and inserts an `audit_log` row; no-plugin output unchanged.

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
- Each panel's `NewRouter(db)` is self-contained with its own SessionMiddleware/AuthMiddleware and a **namespaced session cookie** `yaga-session-{panelID}` so auth is isolated per panel. Routers register routes at the root and are mounted by main.go.

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
- **E2E smoke** (sqlite, per milestone): `yaga init` → extend YAML → `yaga generate` → `go tool templ generate` → `go build ./...` → run → curl login/list/create/action/bulk/delete/hook/export.
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
