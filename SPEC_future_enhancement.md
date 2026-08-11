# Future Enhancements — Security, Optimization & Roadmap

Review date: 2026-08-07. Status: in progress. Phase A (security hardening) is
implemented; the remaining items are proposed. File references point at the
go-fila generator sources that emit the affected generated-app code.

## 1. Security findings

### Critical

- **Hardcoded session secret → auth bypass** ✅ implemented (Phase A)
  Generated `internal/panel/auth/session.go` uses
  `sessions.NewCookieStore([]byte("go-fila-secret-key-change-in-production"))`.
  The secret is public in every generated app, so an attacker can forge a signed
  session cookie claiming any `user_id` and log in as any user.
  Fix: read `SESSION_SECRET` env var; generate a random one and fail fast when
  missing in production.
  Source: `internal/generator/auth.go:261`

- **SQL injection via unvalidated `order` param** ✅ implemented (Phase A)
  (list + card handlers)
  `sort` is whitelisted against `validSorts`, but `order` is interpolated raw into
  `ORDER BY %s %s`. On postgres, stacked statements execute, e.g.
  `?sort=created_at&order=desc;DROP TABLE x--`.
  Fix: whitelist `order` to `asc`/`desc`.
  Source: `internal/generator/handler.go:235,299,351,397`

- **Arbitrary file upload → stored XSS** ✅ implemented (Phase A)
  `saveUploadedFile` accepts any extension (`.html`, `.svg`, `.php`) and serves it
  from `/uploads/*` inline via `http.FileServer`. Uploaded HTML runs scripts in
  the admin origin.
  Fix: extension + content-type whitelist; serve uploads as `Content-Disposition:
  attachment` or store outside the web root.
  Source: `internal/generator/handler.go:1264-1282`

### High

- **No CSRF protection** on any state-changing POST (create/update/delete/action/
  bulk/logout). Admin panels are prime CSRF targets.
  Fix: `SameSite=Lax` session cookie + CSRF token middleware on state-changing routes.
  ✅ implemented (Phase B)

- **Error responses leak DB internals** ✅ implemented (Phase A) —
  `http.Error(w, err.Error(), ...)` on all
  handlers exposes SQL, table names and host details to clients.
  Fix: log server-side, return a generic 500.
  Source: throughout `internal/generator/handler.go`

- **Known default admin credentials** ✅ implemented (Phase A) —
  `init --db` ships `admin@admin.test / admin`;
  `init --demo` ships `admin@demo.test / admin`.
  Fix: `--admin-password` flag or generate + print a random one-time password.
  Source: `cmd/go-fila/introspect.go:725-767`, `cmd/go-fila/demo.go:1278-1296`

### Medium

- **Session cookie hardening** — cookie `Options` never set (no `Secure`, no
  `SameSite`; gorilla default 30-day `MaxAge`), no session ID rotation after login,
  logout exposed as GET (CSRF-able).
  Fix: explicit cookie options, rotate session on login, POST-only logout.
  ✅ implemented (Phase B)

- **No login rate limiting** — brute-forceable.
  Fix: per-IP throttling (configurable).
  ✅ implemented (Phase B)

- **CSV formula injection** — exported values starting `=`, `+`, `-`, `@` execute
  as formulas in Excel.
  Fix: escape with a leading `'` or tab.
  Source: `internal/generator/handler.go:1000-1050` (export.go)
  ✅ implemented (Phase B)

- **No security headers** ✅ implemented (Phase A) — CSP, `X-Frame-Options`,
  `X-Content-Type-Options`,
  `Referrer-Policy` unset.
  Fix: configurable security-headers middleware.
  Source: `internal/generator/router.go` (`securityHeaders`, registered on every
  generated router; a configurable variant is Phase C roadmap).

### Low / correctness

- `html` widget + `stat` render DB output via `templ.Raw` (untrusted data = stored
  XSS) — document or opt-in.
  Source: `internal/generator/templ.go:998-1027`
- Action + bulk routes skip RBAC entirely (documented design) — consider optional
  enforcement.
- `update.go` / `delete.go` hardcode `WHERE id` instead of `idColumn(r)` — breaks
  introspected MSSQL tables with an `ID` key column.
  Source: `internal/generator/handler.go:959,1568`

## 2. Optimization findings

1. **Two queries per list** (COUNT + SELECT) → use `COUNT(*) OVER()` window function
   for a single round trip.
2. **`per_page` hardcoded to 20** → make configurable per resource.
3. **Connection pool config unused** — `connections.*.pool` (max_open/max_idle/
   lifetime) is parsed but never applied; wire into generated `main.go`.
   Source: `internal/generator/main.go` (no `SetMaxOpenConns` etc.), `internal/types/config.go:85`
4. **Bulk actions run N queries with no transaction** → wrap in one transaction
   (rollback on error).
5. **Options loader runs one query per `options_query` field** per form GET (N+1) →
   batch lookups.
   Source: `internal/generator/handler.go:1400-1425`
6. **Widget DB errors silently swallowed** (`_ = db.QueryRowContext`) → log errors.
   Source: `internal/generator/router.go:157-315`
7. **`SessionMiddleware` is a no-op** → remove or implement (e.g. security headers).
   Source: `internal/generator/auth.go:286-294`
8. Error-only logger exists (`--log err`); add request-id + timing for ops.

## 3. Enhancement roadmap

**Phase A — Security hardening (small surface, high priority)** ✅ done
Session secret via env, `order` whitelist, upload validation, safe error responses,
admin password handling, security headers.
- Session secret: generated `session.go` reads `SESSION_SECRET` (min 32 chars),
  fails fast on `APP_ENV=production` when unset, otherwise uses an ephemeral
  random secret with a warning.
- `order` whitelist: list/card handlers clamp to `asc`/`desc` after the
  default-sort block.
- Upload validation: `saveUploadedFile` whitelists extensions and rejects
  `text/html` / `image/svg+xml` by magic bytes; `/uploads/*` is served with
  `Content-Disposition: attachment`.
- Safe errors: generated `internal/panel/httperr` (Internal/NotFound) logs
  server-side and returns generic status text; all handlers use it.
- Admin password: `--admin-password` flag on `init --demo` / `init --db`;
  random 14-char one-time password generated + printed when omitted.
- Security headers: `securityHeaders` middleware on every generated router
  (CSP, X-Frame-Options DENY, nosniff, Referrer-Policy, Permissions-Policy).

**Phase B — CSRF & auth robustness** ✅ done
CSRF tokens + SameSite cookies, session rotation, login rate limiting, CSV escaping,
optional row-level RBAC enforcement.
- CSRF: generated `session.go` adds `CSRFToken(r, w)` (session-bound 32-byte
  random token) + `CSRFMiddleware` (skips GET/HEAD/OPTIONS and `/static/`,
  `/uploads/`; accepts `_csrf` form field or `X-CSRF-Token` header;
  `subtle.ConstantTimeCompare`; 403 on mismatch). Registered first in the panel
  `Route` block. Every state-changing form (create/update/delete/action/bulk/
  logout/login) embeds a hidden `_csrf`; `ListData`/`DetailData`/`FormData`/
  `LoginPageData` carry the token.
- SameSite cookies: `newStore` helper sets `Path: "/"`, `MaxAge: 0` (session
  cookie), `HttpOnly: true`, `Secure: os.Getenv("APP_ENV") == "production"`,
  `SameSite: http.SameSiteLaxMode`.
- Session rotation: successful login expires the old session (`MaxAge=-1` +
  `Save`) then mints a fresh one via `Store.New`; `resetLoginLimit(r)` clears
  the IP counter.
- Logout is POST-only (GET is 405); login POSTs are CSRF-protected too
  (prevents login CSRF).
- Login rate limiting: optional `auth.login.rate_limit` (`max_attempts`,
  `window_seconds`) emits `ratelimit.go` (per-IP map, mutex, windowed counter,
  `net.SplitHostPort`) and a re-render with "Too many login attempts" when
  exceeded. Absent/zero config → legacy behavior.
- CSV escaping: exported headers and values pass through `csvSafe`, which
  prefixes a `'` when a value starts with `=`, `+`, `-`, `@`, tab or CR.
- Row-level action/bulk RBAC: `Action.Policy` (pipe-separated roles) generates
  `ActionRBACMiddleware`; action and bulk routes are wrapped with
  `r.With(auth.ActionRBACMiddleware(res))` only when a policy exists
  (`hasActionPolicies`).

**Phase C — Performance & correctness**
Windowed COUNT, configurable `per_page`, pool settings wiring, transactional bulk,
batched options loader, `idColumn(r)` in update/delete, widget error logging.

**Phase D — Feature roadmap**

Status: planned (2026-08-09). Four milestones below; implementation order D2 → D3 →
D5 → D6 (D1 — auth features — and D4 — API mode — are excluded from the plan).
Decisions already taken: sqlite procedures are **YAML-seeded only** (no runtime
editor UI). Assumptions flagged ⚠️ below are open to veto before implementation.

| Item | Status |
|---|---|
| Plugin system (`SPECv05plus.md` M4) | **Done** (loader, `pkg/plugin`, `--skip-plugins`); remaining work is M5 (plugin **fn** hooks) → D5 |
| Audit log resource | Greenfield (strong existing infra: `hooks.Scope`, `RETURNING id`, `auth.UserName`) |
| CSV import + export column selection | Export exists (all list cols); import + selection new |
| SQLite stored procedures (batch-in-table) | Greenfield |
| AI-assisted `go-fila edit` (OpenRouter) | **Done (D7)** — `edit --prompt/--apikey/--model/--dry-run` (`cmd/go-fila/ai.go`, embedded `ai_spec.md`, single retry, httptest stub) |
| Drop Node.js/npm from the dashboard build | Planned (D8) |
| Editor Validate (main menu → results list → jump-to-fix) | **Done (D9)** |

---

### D2 — Audit log resource

**Design (recommended: generator-implicit audit on every mutating op)**
```yaml
audit:
  enabled: true
  table: audit_log            # default
  include_values: true        # store JSON of submitted form values
  policy: "admin"             # optional RBAC on the audit resource
  exclude_resources: []
```
The generator:
1. **Augments config at generation time** (same technique as plugin merging): appends an
   `AuditLog` resource (list-only over `audit_log`, `default_sort: -created_at`) + an
   "Audit Log" navigation group, unless excluded — reuses all existing resource generation.
2. **Emits `audit_log` schema** into `sql/migrations/` (driver-aware DDL) +
   `sql/queries/audit.sql` (sqlc List/Count).
3. **Weaves audit INSERTs** into create/update/delete/action handlers, after the DB op,
   before the redirect:
   `INSERT INTO audit_log (user_id, user_name, table_name, action, row_id, values_json)
   VALUES ($1,$2,$3,$4,$5,$6)` — actor from `auth.UserName(r)`.

**Key design consequence:** audit requires the `RETURNING <id>` capture path on create
even when no user hooks exist — the "hookless path stays byte-identical" invariant is
relaxed for create/update/delete/action handlers when `audit.enabled` (snippet builder
`auditInsertStr` in a new `internal/generator/audit.go`, reusing `g.returningClause(r)`,
`scopeValuesStr`). Delete/action store `row_id` only (no pre-delete snapshot in v1).
Wrap op + audit insert in one transaction (folds in optimization finding #4 for single-row
ops; transactional bulk stays in D3). Respects `exclude_resources`; RBAC via the existing
`policies:` when `audit.policy` set. `values_json` contains bcrypt output (already what
`scope.Values` holds) — documented, no plaintext passwords.

**Tests / exit criteria:** snippet assertions — create.go has `RETURNING <id>` + audit
INSERT when audit on with no user hooks; delete/update/action carry the INSERT; e2e:
create/update/delete/action rows appear in the audit_log resource with correct
actor/action/row_id/values JSON.

---

### D3 — CSV import + export column selection

**Export column selection:** `ListConfig` gains `Export []string` (optional subset).
`generateCSVHandler` uses those column names + `Label` headers when set (falls back to
today's all-list-columns behavior). No UI change (config-driven); a request-time column
picker deferred.

**CSV import:** config `resource.import_csv: true` → generates `import.go` + route
`POST /{res}/import/csv` (CSRF-protected; RBAC-wrapped when policies exist).
- **Refactor to avoid SQL drift:** factor the create INSERT construction out of `create.go`
  POST into a package-level `func buildCreateParams(m map[string]string) ([]interface{}, error)`
  (bcrypt-hashes password fields; skips/errors on `file`/`image` fields in v1). Both
  `Create` POST and `Import` call it.
- Import handler: `r.ParseMultipartForm`, `encoding/csv`, map header → create-field,
  per-row `buildCreateParams`, **one transaction** around all inserts, report
  `Imported N, Skipped M` (row numbers + errors).
- List templ: "Import CSV" button (only when `import_csv`) opening a modal
  (`enctype="multipart/form-data"`); POST → redirect with `?flash=...` shown in the topbar.
- Sprintf risk: `import.go` is a new emitter — every `%s`/`%d` counted (AGENTS.md #1).

**Tests / exit criteria:** unit — export uses subset columns; import.go emits
`buildCreateParams` reuse + transaction; e2e — `curl -F file=...` with 3 valid + 1 bad row
→ 3 inserted, 1 reported; export returns only selected columns.

---

### D5 — Plugin M5 (fn hooks)

Completes the already-built plugin system (`SPECv05plus.md` §6.7):
- `pkg/plugin`: `Panel.AddHookSource(name, content)` writes a `package hooks` Go file into
  the manifest (`HookSources map[string]string`); loader writes them to
  `OutDir/internal/hooks/`.
- `attachHook` in `internal/generator/plugin.go`: stop rejecting `fn` hooks — track
  plugin-provided fn names; `generateHooks.collectFnHooks` skips stub generation for names
  backed by a plugin hook source (or a user stub).
- Merge validation: a plugin fn hook must have a matching hook source, else fatal.
- Deliverable: extend the plugin example with an fn hook; regression — no plugins →
  unchanged output.

---

### D6 — SQLite stored procedures (SQL-batch-in-table, YAML-seeded)

SQLite has no stored procedures; a "procedure stored in a table and run by the sqlite
engine" is a **named SQL-batch executor** — the body is read from a table at call time,
split into statements, and executed inside one transaction. This gives `proc:` real
semantics on sqlite (today it is a silent no-op: `procSQL` returns `""`, proc hooks/actions
emit nothing). mattn/go-sqlite3 v1.14.24 facts that shape the design: `Exec`/`Query` only
run the **first** statement of a multi-statement string (no tail loop) → must split;
`$1` binds positionally → the existing `$1` convention keeps working.

**Config** (top-level, sqlite-only semantics):
```yaml
procedures:                       # top-level
  - name: archive_old_orders
    description: "Archive orders older than 1 year"
    sql: |
      UPDATE orders SET status='archived' WHERE created_at < datetime('now','-1 year');
      INSERT INTO audit_events (msg) VALUES ('bulk archive ran');
```
Existing `proc:` fields (actions + hooks) reference these by name — same config on all
three drivers, three execution strategies (`CALL` / `EXEC` / helper).

**Generator changes** (new `internal/generator/procs.go`):
- Emits `sql_procedures(name PK, body, description, updated_at)` DDL into
  `sql/migrations/` with `INSERT OR IGNORE` seeds.
- Emits the shared `internal/panel/procs/procs.go` package:
  `Exec(db, name, id) error` — look up body → **tokenizer-based statement split**
  (handles `'…'`, `"…"`/`[…]`, `--`/`/* */` comments) → one transaction,
  `ExecContext(stmt, id)` per statement, drain stray result rows, rollback on error.
- **Driver-aware `proc` emission flips for sqlite:** `hookBlockEmits` returns true for proc
  on sqlite; `actionExecSQL`/`hookCallsStr`/bulk emit `procs.Exec(db, "<name>", <id>)`
  instead of an empty block. Create gains `RETURNING <id>` capture for proc-only
  after-hooks.
- ⚠️ **Inverts a documented AGENTS.md invariant** ("proc-only hooks on sqlite emit
  nothing") — rewrite that gotcha; regression guard asserts feature-off output stays
  byte-identical.
- **Validator:** when the driver is sqlite, every `proc:` reference must match a
  `procedures:` body — fatal config error at generation time (no runtime editor exists),
  mirroring plugin-load-failure semantics.
- Postgres/mssql: the `procedures:` block is ignored (real procs come from user DDL);
  emitting `CREATE PROCEDURE` from the YAML batch is a documented future extension.

**Tests / exit criteria:** unit — statement splitter cases (quoted `;`, comments, trailing
`;`, empty body), sqlite proc emission in actions/hooks/create, validator error on a
missing body, idempotent seeds; e2e (sqlite) — YAML-declared proc on an action + an
after-hook runs the multi-statement batch atomically; one failing statement rolls back all;
missing proc → clean `httperr` page.

---

### D7 — AI-assisted config editing (`go-fila edit` via OpenRouter)

**Status: done (2026-08-10).** Non-interactive and opt-in: AI flags live on `edit`
only; without `--prompt` the current TUI runs unchanged. Provider locked to OpenRouter
(base URL hardcoded — the only supported provider). Decisions taken (2026-08-10):
one-shot write + `--dry-run` preview; `--model` flag defaulting to `openrouter/auto`;
API key via `--apikey` with `OPENROUTER_API_KEY` env fallback.

**Command shape:**
```
go-fila edit --apikey KEY [--model MODEL] --prompt "Change dashboard title to: Order management"
go-fila edit [--apikey KEY] --prompt "…" --dry-run      # preview proposed YAML + diff, no write
```

**Design:**
- `cmdEdit` branches to an AI path when `--prompt` is set; a second flag pass
  (`parseEditFlags` in a new `cmd/go-fila/ai.go`) picks out `--apikey/--prompt/--model/
  --dry-run`, leaving `parseGlobalFlags`'s tuple untouched (only `edit` understands them).
- Load via `parser.ParseFile(configPath)`, marshal current YAML. Build messages: a system
  role with the output contract ("return ONLY the complete new go-fila.yaml in a ```yaml```
  fence; keep `version`; don't invent keys") + a user message of the embedded compact
  schema cheat-sheet (`//go:embed ai_spec.md`, ~3–4 KB — the 33 KB `SPEC.md` stays out of
  the prompt to keep tokens low) + the current YAML + the user's instruction.
- POST `https://openrouter.ai/api/v1/chat/completions` (stdlib `net/http`,
  `Authorization: Bearer`; the key is never logged/echoed), `temperature: 0`, ~90 s HTTP
  client timeout. On parse/validate failure, retry **once** feeding the validator error
  back; on failure exit 1 with the original file untouched.
- `extractYAMLBlock` (```yaml``` fence with fallback heuristics) → `yaml.Unmarshal` into
  `types.Config` → `parser.Validate`. `--dry-run` prints the proposed YAML + a compact `±`
  line diff and exits 0 without writing; otherwise writes `configPath` (0644) and prints
  the same diff summary.
- Config-only scope: SQL/`sql/queries` files are not edited by the AI path. Full
  `go-fila.yaml` is transmitted to OpenRouter (documented in usage text — consent is the
  user supplying the key + prompt).

**Files:** new `cmd/go-fila/ai.go` (`parseEditFlags`, `openrouterChat`, `buildEditPrompt`,
`extractYAMLBlock`, `applyAIDiff` with single retry, `diffLines`) + new `ai_spec.md`
(embedded schema reference); `edit.go` branch; `main.go` usage text.

**Tests / exit criteria:** httptest OpenRouter stub — happy path writes the file + prints a
diff; retry-on-invalid yields a valid second attempt; `--dry-run` never writes; fence
extraction; flag/env key resolution (missing key → clear error). Docs: AGENTS.md CLI
section + `SPEC.md` usage line (config is sent to OpenRouter).

---

### D9 — Editor validation with jump-to-fix (`go-fila edit` → Validate)

**Status: done (2026-08-11).** Adds a "Validate" entry to the editor's left
nav that runs a full health check (structural + field-name + missing table/query)
and lists every problem; pressing Enter on a finding jumps to the exact editor
page and highlights the offending column/field row. Decisions taken (2026-08-11):
results-list screen (not jump-to-first), full health-check scope, missing columns
are warnings (computed-column tolerance) while structural / missing-table /
missing-query findings are errors.

**Design:**
1. **`internal/parser/validator.go`** — split `Validate` into
   `ValidateAll(cfg) []error` (collects every structural problem instead of
   early-returning) plus a thin `Validate` wrapper returning the first error, so
   `parser.Parse` and the editor's save path keep their current behaviour.
2. **`internal/schema/references.go`** — location-aware column refs: new
   `ColumnRef{Column, Section string, Index int}` +
   `References.ColumnRefs map[string][]ColumnRef` (kept beside the existing
   `Columns` map). `CollectReferences` records section+index for `list.columns`,
   `card.fields`, `detail.fields`, `form.create/update/delete.fields`, plus
   `list/card.default_sort`, `card.kanban_field`, `card.searchable` (leading `-`
   stripped via a `sortColumn` helper).
3. **`cmd/go-fila/editor/sync.go`** — `syncReport.missingCols` becomes
   `[]colMissing{resource string; ref schema.ColumnRef}`; the Sync screen renders
   `resource.section.column` (more precise than today's `resource.column`).
4. **`cmd/go-fila/editor/validate.go` (new)** — `finding{kind, label, detail;
   goTo}` + `runValidation()`:
   - structural: validate a YAML copy via `parser.ValidateAll` (same copy
     technique as `validateCopy`); a `goTo` is attached when the message parses
     `resources[i]`/`pages[i]` (→ resource/page page) or mentions `panel.name`/
     `panel.path` (→ panel page).
   - schema: reuse `analyze()` — missing tables → resource page; missing columns
     → `sectionJump`; missing queries → the resource's SQL-queries page (query
     row focused when it appears there); missing FK `List{}` queries →
     informational warning linking to the Sync screen.
   - `sectionJump(idx, section, focusIdx)` maps sections to the existing builders
     (`columnsPage`, `cardFieldsPage`, `detailFieldsPage`, `formFieldsPage(idx,
     which)`, `listPage`, `cardPage`), `showPage`s the result, then
     `SetCurrentItem(focusIdx)` to highlight the bad row.
   - `validatePage()`: tview.List of findings (red errors / yellow warnings),
     "No problems found" empty state, Refresh (Ctrl+R) + Back (Ctrl+B) buttons —
     mirrors the Sync screen layout.
5. **`cmd/go-fila/editor/editor.go`** — `buildNav` gains
   `e.navItem("Validate (Ctrl+V)", "validate", e.validatePage)` between "Sync SQL
   & YAML" and "Preview", plus a global `tcell.KeyCtrlV` case in `capture`.

**Tests / exit criteria (met):** parser — `ValidateAll` returns multiple errors while
`Validate` returns the first (`TestValidateAllReportsEveryProblem`); schema — `ColumnRefs` sections/indexes asserted in
`TestCollectReferences`; editor — `validatePage` builds (added to
`TestPageBuilders`), `runValidation` flags a bad column in list/card/form sections
with a working `goTo` (`TestRunValidationFindsBadColumns`), `sectionJump` focuses
the offending row (`TestSectionJumpFocusesOffendingRow`), sim-screen smoke
navigates to Validate (`TestValidateGlobalShortcut`). Also added a save-then-quit
regression test for the reported "Ctrl+S then Ctrl+Q asks to save" bug
(`TestSaveThenQuitSkipsConfirm`, `TestQuitConfirmClearsModified`). Docs: AGENTS.md
editor section.

---

### D8 — Drop Node.js/npm from the generated dashboard build

**Status: planned (2026-08-10).** Goal: `make` (and `go-fila generate`) must not require
node/npm; generated-app output stays byte-identical and runtime stays offline. The only
npm consumers are Tailwind CSS compilation (`npx tailwindcss`) and Chart.js vendoring
(`cp node_modules/...`). Decisions taken (2026-08-10): Tailwind via the **standalone
binary** (PATH + optional `make get-tailwind` download, sqlc-style toolchain model);
Chart.js **embedded** into go-fila via `//go:embed`.

**Design:**
- **Chart.js**: commit `chart.umd.min.js` @ **4.4.1** (MIT, license banner intact) at
  `internal/generator/assets/chart.umd.min.js`; `//go:embed` it and copy to
  `OutDir/static/js/chart.js` during `generateAssets` (mkdir `static/js`). Charts then work
  after `go-fila generate` alone — a bare `go build` in `admin/` serves them, zero network.
  go-fila binary grows ~180 KB. `/static/js/chart.js` reference in `templ.go` unchanged.
- **Tailwind**: `RunTailwind` in `tailwind.go` swaps `npx tailwindcss` for the
  `tailwindcss` binary (PATH, honoring a `TAILWIND` env override; still non-fatal from
  `cmdGenerate`). `generateStaticAssets` **stops emitting `package.json`** (keeps
  `tailwind.config.js` + `styles.css`). Rewritten generated `Makefile`
  (`makefile.go`): remove `deps`/`npm install`; `build: css sqlc templ`; `css:`
  runs `$(TAILWIND) -i ./internal/assets/css/styles.css -o ./static/css/styles.css
  --minify` with `TAILWIND ?= tailwindcss`; new optional `get-tailwind` target maps
  `uname -s/-m` (linux/macos × x64/arm64; Windows excluded as today) and curls the pinned
  **v3.4.x** standalone binary to `.tools/tailwindcss`, printing the
  `make TAILWIND=$(CURDIR)/.tools/tailwindcss css` usage.
- **Docs + hints**: `cmdGenerate` warning/Next-steps drop npm (`make css` /
  install the standalone binary); update `AGENTS.md`, `README.md` (prereq table),
  `SPEC.md`, `AGENTS_for_generated_dashboard.md`.

**Tests / exit criteria:** generator unit — `generateAssets` emits `static/js/chart.js`
equal to the embedded bytes and **no** `package.json`; the generated `Makefile` contains no
`npm` and its `css` target invokes `$(TAILWIND)`. E2E — `go-fila init --demo` + `make css`
with the standalone binary produces `static/css/styles.css` and `static/js/chart.js`; a bare
`go build` serves chart.js.

---

### Order, dependencies & cross-cutting

**D2 → D3 → D5 → D6.** Rationale: D2's audit INSERT weaving and transactional single-row
ops establish the op-wrapping pattern that D3 (transactional import) and D6 (transactional
proc batches) mirror; D3's `buildCreateParams` refactor is a hard dependency for CSV import
reuse; D5 and D6 are independent and smallest — last. **D7 is independent of D2–D6**
(CLI-only, no generator changes; smallest surface) and can slot in alongside any milestone.
**D8 (no-npm build) is independent of D2–D7** — it touches only the build/asset tooling, not
generated-app handlers or the editor.

Cross-cutting for every milestone: version bump `0.8.0 → 0.9.0`; docs
(`SPEC.md`, `README.md`, `AGENTS.md`) updated per milestone; every milestone ends with
`go build ./...`, `go vet ./...`, `gofmt -l .`, and a templ + tailwind compile of a
generated project. Respect the documented generator gotchas: Sprintf spec counting
(biggest risk — most milestones add emitters), `templ.SafeURL` on every new URL-bearing
attr, no `style={}`/conditional `class={}`, IIFE-wrapped inline scripts, driver-aware
placeholders + sqlite arg order, `idColumn(r)` everywhere, no comments in generated code.
Each milestone carries a **regression guard**: feature-off output stays byte-identical
(snippet-asserted in `generator_test.go`).
