# yaga — Feature Summary

Status of all implemented and planned features across `SPEC.md`, `SPECv05plus.md`,
`SPEC_future_enhancement.md`, `SPEC_mssql_support.md` and `SPEC_yaml_editor.md`.
Generated from the SPECs + git history (2026-08-16).

## Core generator (SPEC.md)

| Feature | Description | Status |
|---|---|---|
| YAML-driven admin dashboard generator | Reads a declarative `yaga.yaml` (panel, resources, pages, widgets, auth, navigation, theming) and generates a fully functional admin panel in Go — no boilerplate hand-writing. | **Implemented** (2026-08-01) |
| Pure code-gen, zero runtime framework dependency | Generated app is a standard Go app (`database/sql` + chi + Templ); `yaga` itself is never linked into the output. | **Implemented** (2026-08-01) |
| CRUD resources — list/detail/card/form views | Each resource has independent list, detail, optional card (grid + kanban) and create/update/delete form views with sorting, pagination, search and per-view columns/fields. | **Implemented** (2026-08-01) |
| Field types | 17 UI field types (integer, string, text, email, password, boolean, badge, datetime, date, image, file, select, relation, json, float, gps) with type-specific list renderers and form inputs. | **Implemented** (2026-08-01; gps 2026-08-03) |
| Pages & widgets | Custom dashboard pages with widget grid: `stat`, `stats_grid`, `chart` (line/bar/pie/area), `table`, `list`, `html`; widget DB queries run at request time with logged, non-fatal errors. | **Implemented** (2026-08-01) |
| Panel config — brand / layout / theme / navigation | Sidebar (width, collapsible), sticky topbar, `max_content_width`, brand colors/fonts, grouped navigation with icons, resource/page/link items, `opens_in_new_tab`. | **Implemented** (2026-08-01) |
| Auth & RBAC | Session-based login (`gorilla/sessions` + bcrypt), logout, `AuthMiddleware`, per-resource `policies:` (view_any/view/create/update/delete) via `RBACMiddleware`. | **Implemented** (2026-08-01) |
| CLI — `init`/`generate`/`validate`/`edit`/`version` | Stdlib-flag CLI; `init` (scaffold or `--db`), offline `generate`, `validate` (+ `--fix` auto-repair), TUI `edit`, `wedit`. | **Implemented** (2026-08-01) |
| `init --db` — database introspection | Connects to an existing SQLite/Postgres/MSSQL DB, introspects schema (tables, columns, PKs, FKs), creates `users`/`roles` + admin user, writes `yaga.yaml` with a captured `schema:` block and one resource per table. | **Implemented** (2026-08-04) |
| Card/kanban views | Optional per-resource card view (`columns`×`rows` grid, `lg:grid-cols-N`) with search + pagination; `kanban_field` on a select renders a kanban board grouped by option values. | **Implemented** (2026-08-03) |
| Generated app build tooling | Generated `Makefile` (`build`/`templ`/`run`/`package`/`clean`), standalone `go.mod` declaring templ as a Go tool, offline runtime with vendored Chart.js 4.4.1. | **Implemented** (2026-08-02; hardened D8/D12) |
| Driver support — postgres / sqlite / mssql | Driver from `connections.*.driver`; driver-aware SQL (`ILIKE`/`LIKE`, `$N`/`?`/loose `$N`, identifier quoting, pagination syntax, id Go types, `RETURNING` vs `OUTPUT INSERTED`). | **Implemented** (postgres/sqlite 2026-08-01; mssql 2026-08-05) |

## v0.5+ milestones (SPECv05plus.md)

| Feature | Description | Status |
|---|---|---|
| Custom actions — `requires_confirmation` / `icon` / `bulk` (M1) | Raw-SQL switch-dispatch `POST /{res}/{id}/action/{action}`; per-row buttons with confirm prompt + icons; `bulk: true` adds a select-checkbox list toolbar posting to `/{res}/bulk/{action}`. | **Implemented** (2026-08-05) |
| Dark mode + theming (M2) | `panel.theme.dark_mode`, class-based Tailwind dark variants, `--brand-*` colors, fonts, sidebar width/collapsible, sticky topbar, `max_content_width`, `localStorage['yaga-theme']` toggle. | **Implemented** (2026-08-05) |
| Lifecycle hooks on form actions + custom actions (M3) | `before`/`after` hooks (fn stubs in `internal/hooks`, inline SQL, proc) on create/update/delete + actions; `RETURNING id`/`OUTPUT INSERTED` id capture for after-create hooks; hook error → HTTP 500. | **Implemented** (2026-08-06) |
| Stored procedures (`proc:`) on postgres/mssql | `proc:` on hooks/actions emits `CALL <name>($1)` (pg) / `EXEC <name> $1` (mssql, loose `$N`), binding the record id; schema-qualified names pass through verbatim. | **Implemented** (2026-08-06) |
| Plugins (M4) | Generator-time plugin system: `pkg/plugin` authoring API (Resources/Pages/Navigation/SQL files/HookAttachments), shim-module loader, manifest merge, fatal on load failure, `--skip-plugins` escape hatch. | **Implemented** (2026-08-09) |
| Plugin fn hooks — `AddHookSource` (M5/D5) | Plugins contribute `package hooks` Go sources backing fn hooks; loader writes them, `generateHooks` skips stubs for plugin-backed names, unbacked fn hook is fatal. | **Implemented** (2026-08-13) |
| Multi-panel (M5) | Additive `panels:` list, per-panel router/auth/resources/views, namespaced `yaga-session-{id}` cookies, `r.Mount` in main.go. | **Planned, not started** |

## TUI editor (SPEC_yaml_editor.md)

| Feature | Description | Status |
|---|---|---|
| tview TUI editor (`yaga edit`) | Persistent 3-pane shell (nav list + `tview.Pages` + title/status bars), live edits via `SetChangedFunc`, Ctrl+S/Ctrl+V/Ctrl+P/Ctrl+O/Ctrl+Q/F10/Esc shortcuts, full YAML spec coverage, `modified` tracking. | **Implemented** (2026-08-04) |
| cd-style navigation dialog | Ctrl+P go-to with canonical per-screen paths, absolute/relative resolution, Tab autocomplete to longest common prefix, case/space-insensitive matching, `#<idx>` for unnamed items. | **Implemented** (2026-08-13) |
| TUI dashboard + per-resource preview | ASCII-frame mock of the dashboard (topbar, sidebar from `cfg.Navigation`, widget boxes) and per-resource list/form/detail previews; no DB, no generated app. | **Implemented** (2026-08-04) |
| Editor Validate screen with jump-to-fix (D9) | Single health check: `ValidateAll` structural pass + schema-block reference pass (missing tables = errors, missing columns = warnings); Enter jumps to the exact page and highlights the offending row. | **Implemented** (2026-08-11) |

## Security & robustness (SPEC_future_enhancement.md, Phases A–C)

| Feature | Description | Status |
|---|---|---|
| Phase A — security hardening | `SESSION_SECRET` env (ephemeral random fallback, fail-fast in production), `order` asc/desc whitelist, upload extension+content-type allow-list + attachment serving, safe `httperr` responses, `--admin-password`/random one-time password, `securityHeaders` middleware (CSP/X-Frame-Options/nosniff/Referrer-Policy/Permissions-Policy). | **Implemented** (2026-08-09) |
| Phase B — CSRF & auth robustness | CSRF token middleware (hidden `_csrf`/`X-CSRF-Token`, constant-time compare) registered first in the panel, SameSite=Lax session cookies, session rotation on login, POST-only logout, optional login rate limiting (`auth.login.rate_limit`), CSV formula-injection escaping, action/bulk RBAC (`Action.Policy`). | **Implemented** (2026-08-11) |
| Phase C.0 — performance & correctness | Windowed `COUNT(*) OVER() AS _total` (single round trip), configurable `per_page`, `connections.*.pool` → `db.Set*` setters, transactional bulk, deduped/batched options loader, widget error logging, `idColumn(r)` in update/delete. | **Implemented** (2026-08-11/13, verified 2026-08-13) |
| Phase C.1 — request-id + timing request logger | Single generated `requestLogger` (`[<reqid>] method uri status duration`) replacing `middleware.Logger`/`errorOnlyLogger` split and the dead `SessionMiddleware`. | **Planned (deferred)** |
| Phase C.2 — stored-XSS documentation | `html` widget documented as raw-HTML/trusted-input-required; `stat`/`stats_grid` numeric-only and safe by construction. | **Implemented** (2026-08-13, doc-only) |

## Phase D roadmap (SPEC_future_enhancement.md)

| Feature | Description | Status |
|---|---|---|
| D2 — Audit log | `audit:` config block; generator-implicit `INSERT INTO audit_log` (user/table/action/row_id/values_json) inside the same transaction on create/update/delete/action; augmented list-only AuditLog resource + nav group; driver-aware DDL skipped when a migration declares the table. | **Implemented** (2026-08-13) |
| D3 — CSV import + export column selection | `list.export` subset with Label headers; `import_csv: true` → `POST /{res}/import/csv` reusing a shared `buildCreateParams`, one transaction, `?flash=` topbar report, modal + button on the list view. | **Implemented** (2026-08-13) |
| D6 — SQLite stored procedures | YAML `procedures:` block → `sql_procedures` table + `INSERT OR IGNORE` seeds + `internal/panel/procs` (`Exec(db,name,id)` with tokenizer statement split, one tx, `$N`-gated binding); sqlite proc emission flips in hooks/actions/bulk/create; validator rejects undeclared refs. | **Implemented** (2026-08-13) |
| D7 — AI-assisted config editing | `yaga edit --prompt` via OpenRouter (or local LM Studio with `--model "lmstudio"`); fragment-then-merge (`mergeYAML`, keyed-item identity), one retry on invalid, `.ENV` credential persistence, spinner, `path -> 'value'` diff output, `--dry-run`. | **Implemented** (2026-08-10; LM Studio 2026-08-13) |
| D8 — Drop Node.js/npm from the dashboard build | Chart.js 4.4.1 embedded into the yaga binary and vendored to `static/js/chart.js`; Tailwind via the standalone binary (`make get-tailwind` optional); no `package.json`, no npm step anywhere. | **Implemented** (2026-08-11) |
| D9 — Editor validation with jump-to-fix | `ValidateAll`/`Validate` split in the parser; location-aware `ColumnRef`s; Validate screen listing all findings with jump-to-fix (see TUI editor section). | **Implemented** (2026-08-11) |
| D10 — Rename project to YAGA | Renamed brand/binary/module (`github.com/MichalHerstus/yaga`) + repo, `yaga.yaml` default config, `yaga-session` cookie / `yaga-theme` key, version 1.0.0. | **Implemented** (2026-08-13) |
| D11 — Drop sqlc; DB as sole schema source | `init --db` captures a `schema:` block as the only schema truth; generate/build run fully offline (no sqlc, no query files); map-based `data.Get*`; removed TUI/wedit/MCP sqlc+Sync surfaces; `init` without `--db` is an error, `--demo` removed. | **Implemented** (Phase 1 2026-08-14; Phase 2 2026-08-16) |
| D12 — Embed pre-built CSS (drop the Tailwind build step) | Pre-built `styles.css` embedded via `//go:embed` and vendored to `static/css/styles.css`; CSS-variable brand colors (`rgb(var(--brand-primary-rgb))`); bounded grid/max-w knobs + `TestGenerateStylesEmbedded` coverage guard; `make styles` regen tooling. | **Implemented** (2026-08-14) |
| D13 — List/Card filter section | Collapsible `list.filter`/`card.filter` with a mini-DSL (`internal/filterexpr`: AND/OR, `= != < > contains not_contains is_null is_not_null`, literals/`$N` params); labeled inputs travel as `fp_*` query params, empty params skip; driver-correct SQL + pagination echo. | **Implemented** (2026-08-14) |
| D14 — Master-detail (header + child lines) navigation | `children:` block (+ auto-emitted by `init --db`) embeds a read-only lines table on detail/edit views; per-line Edit/Delete + "Add Line" with pre-seeded + locked FK; `?return=` navigation; child resource stays independent. | **Implemented** (2026-08-15) |

## Phase E roadmap (SPEC_future_enhancement.md)

| Feature | Description | Status |
|---|---|---|
| E1 — Mobile device support | Always-on REST/JSON CRUD API at `{panel}/api` (Bearer-token auth via `api_tokens`, reuses SQL cores, JSON 401/403) + generated React Native/Expo app (`admin/mobile/`, manifest-driven screens, `visible_on_mobile` nav filter). | **Planned (spec rewritten 2026-08-16), not started** |
| E2 — Lua scripting for actions & hooks | gopher-lua runtime in the generated app; `script:` bodies with `ctx` scope + `db.exec/query/query_one`/`abort`/`log` host API, `?`→`$N` driver-aware renumbering, 5 s timeout, audit-tx integration. | **Planned (2026-08-14), not started** |
| E3 — TUI editor polish | Catppuccin Mocha palette (E3a), SQL syntax highlighting (E3b — **moot**, viewer removed with D11), input enhancements/Tab-completion (E3c). | **Planned (2026-08-14), not started** |
| E4 — `yaga wedit` web-based config editor | Local HTTP server (`internal/serve`) + embedded vanilla-JS SPA editing `yaga.yaml` in the browser; REST API (`/api/config`, `/api/validate`, `/api/fix`, `/api/save`, `/api/raw`), YAML↔JSON bridge, dual light/dark Catppuccin theme, iframe preview tab, live SSE sync + stale-write guard. | **Implemented** (2026-08-15; live sync 2026-08-16) |
| E5 — MCP over wedit | MCP Streamable HTTP endpoint (`POST /mcp`) on the wedit server — JSON-RPC 2.0 tools (`validate`/`save`/`get_value`/`set_value`/`merge_yaml_fragment`/`add_resource`/`add_column`/`add_field`/`add_nav_item`/`remove_nav_item`, …), yaml.Node path resolution, shared in-memory config + rev/SSE. | **Implemented** (2026-08-16) |

## Cross-cutting & extras

| Feature | Description | Status |
|---|---|---|
| MSSQL driver support | `init --db` with `sqlserver://`/`mssql://` DSNs, INFORMATION_SCHEMA introspection with identity-column PK fallback, `mssql`-driver loose `$N`, T-SQL pagination (`OFFSET…FETCH NEXT`, `ORDER BY (SELECT NULL)`), `TOP 1` sanity check, PascalCase naming + `table:`/`id_column:`/`id_type:` overrides. | **Implemented, verified against live MSSQL** (2026-08-05) |
| `validate --fix` auto-repair | `fixer.Apply` repairs known-fixable problems by editing the yaml.v3 node tree (no default injection); `.bak` backup, `--dry-run` preview, partial-repair persists fixes; shared by CLI/TUI/wedit. | **Implemented** (2026-08-16) |
| Modal record picker + auto-fill (`copies:`) | `select`/`relation` fields with resolved option SQL render a modal record picker (Browse button, client-side search, `data-picker-options` JSON); `copies:` auto-fills sibling fields from the selected row. | **Implemented** (2026-08-15) |
| SQL identifier quoting | Every table/column identifier in raw SQL quoted via `quoteIdent` (`"…"` / `[…]`) + per-driver runtime `sqlutil.Ident` for the user-supplied sort column. | **Implemented** (2026-08-15) |
| Card/kanban field rendering & value serialization | `viewmodels.Stringify` unwraps `sql.Null*`/`time.Time`/nil for correct rendering (BUG-1/2 fixes); `renderGPS` maps link; shared create/update form renders the union of both field sets (BUG-4 fix). | **Implemented** (2026-08-10) |
| Read-only view resources | `init --db` introspects DB views as read-only resources (no create/update/delete). | **Implemented** (2026-08-13) |
| SQLC query-file integration | Original data layer: sqlc generate → `internal/data`, query files, editor/sync surfaces. | **Implemented, then removed** by D11 (2026-08-14/16) |
| `init` scaffold + `--demo` | Schema scaffold + seeded sqlite demo (`init --demo`) with `--admin-password`. | **Implemented, then removed** by D11 (2026-08-14) |
| Multi-panel (M5) | See v0.5+ milestones — additive `panels:` support. | **Planned, not started** |

---

*Sources: `SPEC.md`, `SPECv05plus.md`, `SPEC_future_enhancement.md` (§1–E5), `SPEC_mssql_support.md`, `SPEC_yaml_editor.md`, git history (2026-08-01 → 2026-08-16).*
