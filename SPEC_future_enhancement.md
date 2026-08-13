# Future Enhancements — Security, Optimization & Roadmap

Review date: 2026-08-07. Status: in progress. Phase A (security hardening) is
implemented; the remaining items are proposed. File references point at the
yaga generator sources that emit the affected generated-app code.

## 1. Security findings

### Critical

- **Hardcoded session secret → auth bypass** ✅ implemented (Phase A)
  Generated `internal/panel/auth/session.go` uses
  `sessions.NewCookieStore([]byte("yaga-secret-key-change-in-production"))`.
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
  Source: `cmd/yaga/introspect.go:725-767`, `cmd/yaga/demo.go:1278-1296`

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

- `html` widget renders DB output via `templ.Raw` (untrusted data = stored XSS) —
  documented as trusted-input-required (`SPEC.md`); `stat`/`stats_grid` values are
  numeric-only (safe by construction). ✅ documented (Phase C, 2026-08-13)
  Source: `internal/generator/templ.go:1071`, `router.go:383`
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

**Phase C — Performance & correctness** (status 2026-08-13: C.0 all done, C.2 done,
C.3 done, **C.1 deferred**)
Windowed COUNT, configurable `per_page`, pool settings wiring, transactional bulk,
batched options loader, `idColumn(r)` in update/delete, widget error logging, request
logging with request-id + timing.

**C.0 — Original items (all implemented; verified 2026-08-13):**
1. **Windowed COUNT** ✅ — the list/card data query emits
   `SELECT {cols}, COUNT(*) OVER() AS _total FROM …` and scans `_total` per row — a
   single round trip; when the page is empty `totalSet` stays false and the handler
   falls back to `total = page*perPage`. The old two-query + `countClauses`/$N-renumber
   hack is gone. Source: `internal/generator/handler.go` (list/card).
2. **Configurable `per_page`** ✅ — `ListConfig.PerPage` (default 20 applied in
   `internal/parser/validator.go:95`), an editor "Per page" field
   (`cmd/yaga/editor/resource.go:166`), and the handler reads `r.List.PerPage` for
   `LIMIT/OFFSET` (`handler.go:326`). Sources: `internal/types/resource.go:33`.
3. **Pool settings wiring** ✅ — `connections.*.pool` (`max_open_conns`/`max_idle_conns`/
   `conn_max_lifetime`) is emitted as `db.SetMaxOpenConns`/`SetMaxIdleConns`/
   `SetConnMaxLifetime` right after `Ping()` and before the sanity query; no setters
   when the block is absent. Source: `internal/generator/main.go`
   (`TestGeneratePoolSettings`).
4. **Transactional bulk** ✅ — the bulk id-loop runs inside a single `db.BeginTx`;
   `Commit()` only when every Exec succeeded, `defer Rollback()` otherwise. Source:
   `internal/generator/bulk.go`.
5. **Batched options loader** ✅ — distinct `options_query` values load once per resource
   into a shared `{name}Opts := map[string]string{}`; no N queries for N fields.
   Source: `internal/generator/handler.go` (`TestGenerateOptionsLoaderDedupe`).
6. **Widget error logging** ✅ — every widget `Query*`/`Scan` error is
   `log.Printf`'d and the widget renders with whatever rows it got. Source:
   `internal/generator/router.go`.
7. **`idColumn(r)` in update/delete** ✅ — DELETE/UPDATE use `idColumn(r)` (honoring
   `id_column:` overrides) instead of a hardcoded `WHERE id`. Source:
   `internal/generator/handler.go:1069,1822`; regression tests at
   `generator_test.go:1360-1389`.

**C.2 — Stored-XSS documentation (done 2026-08-13):** only the `html` widget is a real
raw-HTML vector — it casts a query *string* result to `template.HTML` and renders via
`templ.Raw` (`internal/generator/router.go:383`, `templ.go:1071`); the query is
config-authored, so its *result* must be trusted input. `stat`/`stats_grid` values are
safe by construction: they scan into `int64` and wrap `fmt.Sprintf("%d", …)`, so only
config-authored numbers ever reach `statWidget`'s `templ.Raw` (`templ.go:1090`). No code
change; documented in `SPEC.md` and the §1 Low finding is marked ✅ documented.

**C.1 — Request logging with request-id + timing (deferred, not implemented):** remove
the dead `SessionMiddleware` (emitted `auth.go:574-582`, registered at `router.go:52` —
its `/static/` branch no-ops then falls through to `next`) and replace the
`if logLevel == "err" { errorOnlyLogger } else { middleware.Logger }` split
(`router.go:46-50`) and the `errorOnlyLogger` literal (`router.go:139-148`, no timing /
request id) with a single generated `requestLogger`: `r.Use(middleware.RequestID)` then
`r.Use(requestLogger(logLevel == "err"))`, wrapping `middleware.NewWrapResponseWriter`,
`time.Since(start)`, logging `[<reqid>] <method> <uri> <status> <duration>` (err mode
skips status < 400). `--log full|err` flag values unchanged; generated router imports
gain `"time"`. This is a global emitter change (all configs), so the byte-identical
regression guard does not apply — assert via `assertGeneratedGoParses` + snippet tests.

**C.3 — Cross-cutting (done 2026-08-13):** version 0.9.0 → 0.10.0
(`cmd/yaga/main.go`); AGENTS.md hardening notes; gates `go build ./...`,
`go vet ./...`, `go test ./...`, `gofmt -l .`.

**Phase D — Feature roadmap**

Status: partially implemented (2026-08-13). Implementation order D2 → D3 →
D5 → D6 (D1 — auth features — and D4 — API mode — are excluded from the plan).
D2 (audit log), D3 (CSV import + export column selection), D5 (plugin fn hooks)
and D6 (SQLite stored procedures) are done.
Decisions already taken: sqlite procedures are **YAML-seeded only** (no runtime
editor UI). Assumptions flagged ⚠️ below are open to veto before implementation.

| Item | Status |
|---|---|
| Plugin system (`SPECv05plus.md` M4) | **Done (D5)** — loader, `pkg/plugin`, `--skip-plugins`, plus `AddHookSource` + plugin fn hooks |
| Audit log resource | **Done (D2)** — config `audit` block, generator-implicit INSERTs on create/update/delete/action in one tx, augmented list-only AuditLog resource + nav, driver-aware DDL/queries, demo-enabled |
| CSV import + export column selection | **Done (D3)** — `list.export` subset (Label headers) + `import_csv` (import.go, shared `buildCreateParams`, transactional, ?flash topbar, modal) |
| SQLite stored procedures (batch-in-table) | **Done (D6)** — YAML `procedures:` block, `sql_procedures` DDL + `INSERT OR IGNORE` seeds, `internal/panel/procs` package (`Exec(db,name,id)` + tokenizer statement split), sqlite proc emission flips (actions/hooks/bulk/create RETURNING), validator rejects undeclared sqlite proc refs |
| AI-assisted `yaga edit` (OpenRouter / LM Studio) | **Done (D7)** — `edit --prompt/--apikey/--model/--dry-run` (`cmd/yaga/ai.go`, embedded `ai_spec.md`, spinner progress, fragment-then-merge (keyed-item), single retry, `.ENV` credential persistence, path+value diff output (`changedPaths`), local LM Studio provider via `--model "lmstudio"`, httptest stub (OpenRouter + LM Studio) + `mergeYAML`/`changedPaths` suites) |
| Drop Node.js/npm from the dashboard build | **Done (D8)** |
| Editor Validate (main menu → results list → jump-to-fix) | **Done (D9)** |
| Rename project to YAGA (binary, module path, repo, docs) | Done |
| List/Card filter section (`list.filter` / `card.filter`, collapsible, `$N` params) | Planned (D11) |

---

### D2 — Audit log resource

**Status: implemented (2026-08-13).** Config block `audit` (`enabled`, `table` default
`audit_log`, `include_values`, `policy`, `exclude_resources`) implemented in
`internal/generator/audit.go`. `applyAudit()` runs after plugins in `Generate()` and
appends a list-only `AuditLog` resource (`default_sort: -created_at`, `values_json`
column only when `include_values`, RBAC from `policy`) + an "Audit Log" nav group.
`generateAuditSchema()` emits driver-aware `audit_log` DDL into `sql/migrations/` and a
sqlc List/Count file into `sql/queries/`, both skipped when a migration already declares
the audit table (`auditTableInMigrations`/`containsCreateTable` — otherwise sqlc fails
with "relation already exists"). `auditFor(r)` honors `exclude_resources` and never
audits the generated AuditLog resource. `auditInsertStr`/`auditTxBeginStr`/
`auditTxCommitStr`/`auditValuesStr` weave
`INSERT INTO audit_log (user_id, user_name, table_name, action, row_id, values_json)
VALUES ($1,$2,$3,$4,$5,$6)` into create/update/delete/action handlers, with the op + audit
insert wrapped in one transaction (`tx, err := db.BeginTx` / `defer tx.Rollback()` /
`tx.Commit()`; the hookless `_, err := db.ExecContext` path and byte-identical output are
relaxed for audited resources). Actor id comes from a generated `auth.UserID(r)` helper
(gated on `auditAnyResource()`); create needs the `RETURNING <id>` capture path even
without hooks (`fmt.Sprintf("%d", newID)`); delete/action store `row_id` only.
`values_json` contains bcrypt output (documented). Demo enables audit
(`include_values: true, policy: "admin"`) with `audit_log` in `demoSchema()`. Tests:
generator snippets (create/update/delete/action/middleware, no-values+excluded keeps
hookless path, schema skipped-when-declared, `containsCreateTable` table), parser
(defaults/validation), plus a full HTTP e2e against the generated demo (login → create/
update/action/delete → rows appear in `audit_log` with correct actor/action/row_id/values
JSON; curl's naive cookie jar 403s on the two-cookie session-rotation response — use an
RFC 6265 jar).

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

**Status: implemented (2026-08-13).** `ListConfig.Export []string` (optional subset;
when set the CSV export emits only those columns with `Label` headers, else the
historical all-list-columns + raw-header behavior) and `resource.import_csv: true`
(generates `import.go` + `POST /{res}/import/csv`, CSRF-protected and RBAC-wrapped
with the create permission). The create INSERT value construction was factored out of
`create.go` into a package-level `buildCreateParams(m map[string]string)
([]interface{}, error)` shared by the Create POST and the import handler (bcrypt-hashes
password fields, coerces booleans, returns a clear error for `file`/`image` fields —
the create POST keeps the legacy inline path only when the resource has such fields,
where `buildCreateParams` becomes a stub for import). Import parses a multipart CSV,
maps header cells (trimmed) to the create field names, runs every row's
`buildCreateParams` + INSERT inside ONE transaction, and redirects to the list with a
`?flash=...` message ("Imported N, Skipped M: row R: error..."). The flash is
middleware-stashed into the request context (`flashHandler` + `viewmodels.SetFlash`/
`FlashMessage`) and rendered as a topbar bar in `Base`. The list view gains an "Import
CSV" button + modal (outside the bulk `<form>`) when `import_csv` is set. Parser
rejects unknown `list.export` columns and `import_csv` without a create form. Demo
enables both on Customer (`export: [id, name, email, status]`, `import_csv: true`).
Editor: "Import CSV" toggle on the resource page + "Export" string-list editor on the
list page (`Resources/<res>/List/Export`). Known limitation: imports are NOT audited
(the audit weaving only covers the create/update/delete/action handlers). Tests:
generator snippets (export subset/all-columns, import.go reuse + transaction + flash,
create POST reuses buildCreateParams, file-field resource keeps the upload path +
stub), parser validation, and a live e2e (3 valid + 1 duplicate-email row →
"Imported 3, Skipped 1" flash; export returns only the selected columns).

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

**Done (D5, 2026-08-13):** implemented as designed. `AddHookSource` (name validated:
bare `<file>.go`, not the reserved `hooks.go`); the loader writes hook sources into
`internal/hooks/`, tracks every package-level function name via `hookFuncNames`, and
`collectFnHooks` skips stubs for those names. `attachHook` merges fn hooks when the fn
name is backed by a source (fatal otherwise). `generateHooks` now also emits `hooks.go`
(Scope only, no stubs/imports) when plugin hook sources exist even with zero YAML hook
blocks, so plugin files compile. The audit example gained a `LogCustomerCreated` fn hook
+ `audit_hooks.go` source; e2e verified the hook fires on customer create (audit_log row)
and delete (SQL hook) on sqlite, and the generated app builds. Regression: existing
no-plugin hook tests byte-identical (all green). Also fixed a latent flag bug: `cmdGenerate`
and `cmdValidate` mis-indexed `parseGlobalFlags`'s tuple so `--force`/`--verbose`/`--skip-plugins`
were swapped (`--verbose` silently disabled the plugin loader).

---

### D6 — SQLite stored procedures (SQL-batch-in-table, YAML-seeded)

**Status: implemented (2026-08-13).** Config block `procedures:` (top-level,
sqlite-only semantics) with `name`/`description`/`sql`. `internal/generator/procs.go`
emits `sql/migrations/procedures.sql` (`CREATE TABLE IF NOT EXISTS sql_procedures(name PK,
body, description, updated_at)` + one `INSERT OR IGNORE` seed per procedure, `''`-escaped
bodies) and `internal/panel/procs/procs.go` — `Exec(db, name, id) error` looks the body up
at call time, splits it with a tokenizer (`'…'` strings incl. `''` escapes, `"…"`/`[…]`
identifiers, `--`/`/* */` comments) and runs each statement inside one transaction,
draining result rows and rolling back on error; the id is bound only when the statement
contains a `$N` placeholder (mattn errors when args exceed placeholders). Driver-aware
flips: `hookBlockEmits` is true for a declared proc on sqlite, `hookCallsStr` emits
`procs.Exec(db, "<name>", scope.ID)`, `actionExecSQL`/bulk emit `procs.Exec(db, "<name>",
id)`, and create gains `RETURNING <id>` capture for proc-only after-hooks. `procs` import
is added per-handler only when that handler actually emits a `procs.Exec` call. Undeclared
sqlite proc refs are skipped (feature-off output byte-identical — `TestGenerateProcSQLiteIgnored`
guards this). Validator (`validateProcedures`) requires unique non-empty names and — when
the driver is sqlite — every `proc:` reference on an action/hook to match a declared body
(fatal config error). Postgres/mssql ignore the block. e2e verified on sqlite: a proc
after-hook on create and a bulk proc action both ran their multi-statement batches
atomically with `$1` bound (customer_log rows + audit intact).

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

### D7 — AI-assisted config editing (`yaga edit` via OpenRouter / LM Studio)

**Status: done (2026-08-10).** Non-interactive and opt-in: AI flags live on `edit`
only; without `--prompt` the current TUI runs unchanged. Provider is OpenRouter by
default (`openRouterBaseURL`), with an opt-in local LM Studio provider selected by
the `--model "lmstudio"` sentinel (`lmStudioBaseURL` = `http://127.0.0.1:1234/v1`,
no API key; the loaded model id is discovered via GET `/models`). Decisions taken
(2026-08-10):
one-shot write + `--dry-run` preview; `--model` flag defaulting to `openrouter/auto`;
API key via `--apikey` with `OPENROUTER_API_KEY` env fallback; after a successful run
the effective key/model are persisted to `.ENV` in the current folder so later runs can
omit the flags (`--apikey` > `OPENROUTER_API_KEY` env > `.ENV`; `--model` > `.ENV` > default);
terminal output prints only the changed keys and their new values as `path -> 'value'` lines
(`changedPaths`), never the whole file.

**Command shape:**
```
yaga edit --apikey KEY [--model MODEL] --prompt "Change dashboard title to: Order management"
yaga edit [--apikey KEY] --prompt "…" --dry-run      # preview changed sections, no write
yaga edit --prompt "…"                               # uses key + model persisted in .ENV
```

**Design:**
- `cmdEdit` branches to an AI path when `--prompt` is set; a second flag pass
  (`parseEditFlags` in a new `cmd/yaga/ai.go`) picks out `--apikey/--prompt/--model/
  --dry-run`, leaving `parseGlobalFlags`'s tuple untouched (only `edit` understands them).
- Load via `parser.ParseFile(configPath)`, marshal current YAML. Build messages: a system
  role with the output contract ("return ONLY the changed sections of the config as a YAML
  fragment in a ```yaml fence; keyed-item lists by `name` / navigation groups by `group` /
  items by `resource`/`page`/`url`; keep `version`; don't invent keys") + a user message of
  the embedded compact schema cheat-sheet (`//go:embed ai_spec.md`, ~7 KB — the 33 KB
  `SPEC.md` stays out of the prompt to keep tokens low) + the current YAML + the user's
  instruction.
- POST `{provider}/chat/completions` (stdlib `net/http`, `Authorization: Bearer`
  only when a key is set — the local LM Studio provider sends none; the key is
  never logged/echoed), `temperature: 0`, 300 s HTTP client timeout; a `spinner` on
  stderr gives live progress while waiting. On merge/validate failure, retry **once**
  feeding the validator error back; on failure exit 1 with the original file untouched.
- `extractYAMLBlock` (```yaml``` fence with fallback heuristics) → `mergeYAML` (yaml.v3
  Node merge: mappings recurse, sequences merge item-by-item by identity key, keyless lists
  replace wholesale, null fragment values leave targets untouched, no deletion support) →
  `yaml.Unmarshal` into `types.Config` → `parser.Validate`. After the run `persistEnv`
  writes the effective `OPENROUTER_API_KEY`/`MODEL` into `.ENV` (0600, unrelated lines
  preserved); both write and `--dry-run` then print the changed keys as `path -> 'value'`
  lines (`changedPaths` walks both docs and emits one line per differing leaf, keyed-list
  identity values inline, strings single-quoted) and exit 0 without echoing the whole file.
  Fragment-only output keeps responses small, so slow free-tier models finish instead of
  timing out.
- Config-only scope: SQL/`sql/queries` files are not edited by the AI path. Full
  `yaga.yaml` is transmitted to OpenRouter (documented in usage text — consent is the
  user supplying the key + prompt).

**Files:** `cmd/yaga/ai.go` (`parseEditFlags`+`.ENV` fallback, `chatCompletions`,
`lmStudioModelID`, `buildEditPrompt`, `extractYAMLBlock`, `mergeYAML` + identity-key merge
helpers, `proposeEdit` with single retry, `spinner`, `changedPaths` leaf diff, `readEnvFile`/
`writeEnvFile`/`persistEnv`, `envPathFunc`) + `ai_spec.md` (embedded schema reference incl. § AI edit
output); `edit.go` branch; `main.go` usage text.

**Tests / exit criteria:** httptest provider stub (serves GET `/models` so the same stub covers
OpenRouter and LM Studio) — happy path writes the file + prints path/value diff lines and preserves
unrelated sections; retry-on-invalid yields a valid second attempt; `--dry-run` never writes (but
still persists `.ENV`); fence extraction; a `mergeYAML` unit suite (mapping deep-merge, keyed-item
resource/fields/navigation merge, item append, wholesale widgets replace, null leaves untouched,
unknown-key/malformed/empty/non-mapping fragment errors); a `changedPaths` suite (scalar/resource/
column/navigation/index paths, added-resource leaves, value quoting, no-changes); LM Studio happy
path (discovered model id sent, no auth header, stale key ignored) and no-model-loaded error;
flag/env/`.ENV` key resolution (missing key → clear error, `.ENV` persisted + reused, precedence
flag > env > `.ENV`, model flag > `.ENV` > default, unrelated `.ENV` lines preserved). Docs:
AGENTS.md CLI section + `SPEC.md` usage lines (config is sent to the provider; `lmstudio` model).

---

### D9 — Editor validation with jump-to-fix (`yaga edit` → Validate)

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
3. **`cmd/yaga/editor/sync.go`** — `syncReport.missingCols` becomes
   `[]colMissing{resource string; ref schema.ColumnRef}`; the Sync screen renders
   `resource.section.column` (more precise than today's `resource.column`).
4. **`cmd/yaga/editor/validate.go` (new)** — `finding{kind, label, detail;
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
5. **`cmd/yaga/editor/editor.go`** — `buildNav` gains
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

**Status: implemented (2026-08-11).** Goal: `make` (and `yaga generate`) must not require
node/npm; generated-app output stays byte-identical and runtime stays offline. The only
npm consumers are Tailwind CSS compilation (`npx tailwindcss`) and Chart.js vendoring
(`cp node_modules/...`). Decisions taken (2026-08-10): Tailwind via the **standalone
binary** (PATH + optional `make get-tailwind` download, sqlc-style toolchain model);
Chart.js **embedded** into yaga via `//go:embed`.

**Design:**
- **Chart.js**: commit `chart.umd.min.js` @ **4.4.1** (MIT, license banner intact) at
  `internal/generator/assets/chart.umd.min.js`; `//go:embed` it and copy to
  `OutDir/static/js/chart.js` during `generateAssets` (mkdir `static/js`). Charts then work
  after `yaga generate` alone — a bare `go build` in `admin/` serves them, zero network.
  yaga binary grows ~180 KB. `/static/js/chart.js` reference in `templ.go` unchanged.
- **Tailwind**: `RunTailwind` in `tailwind.go` swaps `npx tailwindcss` for the
  `tailwindcss` binary (PATH, honoring a `TAILWIND` env override; still non-fatal from
  `cmdGenerate`). `generateStaticAssets` **stops emitting `package.json`** (keeps
  `tailwind.config.js` + `styles.css`). Rewritten generated `Makefile`
  (`makefile.go`): remove `deps`/`npm install`; `build: css sqlc templ`; `css:`
  runs `$(TAILWIND) -i ./internal/assets/css/styles.css -o ./static/css/styles.css
  --minify` with `TAILWIND ?= $(if $(wildcard .tools/tailwindcss),.tools/tailwindcss,tailwindcss)`; new optional `get-tailwind` target maps
  `uname -s/-m` (linux/macos × x64/arm64; Windows excluded as today) and curls the pinned
  **v3.4.x** standalone binary to `.tools/tailwindcss`, which the `css` target then uses
  automatically (falling back to a `tailwindcss` on PATH).
- **Docs + hints**: `cmdGenerate` warning/Next-steps drop npm (`make css` /
  install the standalone binary); update `AGENTS.md`, `README.md` (prereq table),
  `SPEC.md`, `AGENTS_for_generated_dashboard.md`.

**Tests / exit criteria:** generator unit — `generateAssets` emits `static/js/chart.js`
equal to the embedded bytes and **no** `package.json`; the generated `Makefile` contains no
`npm` and its `css` target invokes `$(TAILWIND)`. E2E — `yaga init --demo` + `make css`
with the standalone binary produces `static/css/styles.css` and `static/js/chart.js`; a bare
`go build` serves chart.js.

---

### D10 — Rename project to YAGA

**Status: done (2026-08-13).** Renamed the project/brand from "go-fila" to
**YAGA** (**YA**ml-based **G**enerator of **A**pplications) across code, generated output,
binary, module path, GitHub repository and documentation. Decisions taken (2026-08-11): new
module path `github.com/MichalHerstus/yaga` (matches the real remote owner — the current
`github.com/go-fila/go-fila` never matched the remote `github.com/MichalHerstus/go-fila`);
default config file `go-fila.yaml` → `yaga.yaml`; version bumped 0.14.0 → 1.0.0; generated-app
runtime identifiers renamed (`go-fila-session` cookie → `yaga-session`, `gf-theme` storage key
→ `yaga-theme` — sessions invalidate + theme resets on redeploy, acceptable at 1.0.0); GitHub
repo renamed to `MichalHerstus/yaga`; `session-ses_*.md` transcripts left untouched (historical).

**Design:**
1. **Repo restructure** — `git mv cmd/go-fila cmd/yaga` (embedded `ai_spec.md` +
   `AGENTS_for_generated_dashboard.md` moved with it); repo `Makefile` `BINARY := yaga` and
   `build` target `go build -o $(BINARY) ./cmd/yaga`; `.gitignore` `/go-fila` → `/yaga`.
2. **Module path** — `go.mod` → `module github.com/MichalHerstus/yaga`; replaced
   `github.com/go-fila/go-fila` → `github.com/MichalHerstus/yaga` across the imports,
   `internal/generator/plugin.go` `gofilaModule` const (→ `yagaModule`, plus
   `gofilaCheckout`/`findGoFilaCheckout` → `yaga*`), and `examples/plugins/audit/go.mod`
   (require + `replace ... => ../../..`).
3. **CLI** — binary/command name `go-fila` → `yaga` in usage text (`main.go`), version
   output (`yaga version 1.0.0`, `main.go:46`), next-step prints (`main.go:413`,
   `demo.go:80`, `introspect.go:1456`), and default config path `go-fila.yaml` →
   `yaga.yaml` (`main.go:118`). TUI nav + title bar (`editor.go`) → "YAGA".
4. **Generated output** (`internal/generator/`) — session cookie `go-fila-session` →
   `yaga-session` (`auth.go`, 5 sites), theme key `gf-theme` → `yaga-theme` (`templ.go`,
   `auth.go`), Makefile comment "generated by go-fila" → "generated by YAGA"
   (`makefile.go:28`), plugin shim tmpdir `go-fila-plugin-shim` → `yaga-plugin-shim`
   (`plugin.go:65`).
5. **Docs & embedded content** — `README.md` (incl. `go install
   github.com/MichalHerstus/yaga/cmd/yaga@latest`), `AGENTS.md`, `SPEC.md`,
   `SPECv05plus.md`, `SPEC_yaml_editor.md`, `cmd/yaga/ai_spec.md`,
   `cmd/yaga/AGENTS_for_generated_dashboard.md` (lands inside generated apps:
   `./yaga generate --config yaga.yaml ...`). Prose brand = "YAGA", CLI word = `yaga`.
6. **GitHub** — `gh repo rename yaga --repo MichalHerstus/go-fila`,
   `git remote set-url origin https://github.com/MichalHerstus/yaga.git`, push.

**Tests / exit criteria:** new generator unit test asserting generated output contains no
`go-fila`; `go build ./...` / `go vet ./...` / `go test ./...` / `gofmt -l .` clean;
`grep -r go-fila` matches only `session-ses_*.md`. E2E — fresh `./yaga init --demo` →
`yaga generate` → `make` in the generated dir → login smoke verifying `yaga.yaml`,
`yaga-session` cookie, `yaga-theme` key, no npm.

---

### D11 — List/Card filter section

**Status: planned (2026-08-11), not started.** A YAML-defined filter on list and card
views: a collapsible filter section above the table/cards that builds an arbitrary
AND/OR filtering combination over the resource's columns, with runtime-valued
`$N` parameters. Decisions taken (2026-08-11): **one filter per view** (`list.filter`,
`card.filter` — the "multiple filters per view" idea was stepped down); expression is a
**mini-DSL** compiled at generation time to dialect-correct SQL (no raw SQL in YAML);
`$N` values are collected via **inline labeled inputs** in the filter form and travel in
URL query params (`filter=1`, `fp_<name>=<value>`); an empty param on Apply **skips the
filter** (like an empty search box).

**YAML schema** (`internal/types/resource.go`: `Filter *FilterConfig` on `ListConfig`
and `CardConfig`):

```yaml
list:
  filter:
    label: "Advanced filter"               # shown in the collapsible header
    where: "(price > 1000 and prod_name contains 'abc') or prod_code = $1"
    params:                                # optional; defaults to p<N> / "Value N"
      - name: code
        label: "Product code"
```

**DSL** (new package `internal/filterexpr/`, shared by generator + schema refs; standard
SQL precedence — AND binds tighter than OR — plus parentheses):

```
expr      := or
or        := and ( "and" and )*
and       := primary ( "or" primary )*
primary   := "(" expr ")" | condition
condition := column OP [value]
column    := [A-Za-z_][A-Za-z0-9_]*          # emitted with the `t.` colPrefix when FK joins exist
OP        := =  !=  <>  <  <=  >  >=  | contains | not_contains | is_null | is_not_null
value     := number | 'quoted string' ('' escapes) | $N
```

Driver mapping: `contains` → `ILIKE` (pg) / `LIKE` (sqlite, mssql), `not_contains` →
`NOT ILIKE`/`NOT LIKE`; literal values baked into the emitted SQL string; `$N` becomes a
runtime placeholder token (`__GFP__` in the emitted source, replaced at request time with
`?` / `$<argIdx>` per occurrence in SQL-text order, so `$2` before `$1` binds correctly);
`contains` binds are runtime-wrapped `"%" + v + "%"`. Deliberately excluded from the DSL:
`in`, `between`, `not`, param `type:` (values pass as strings; DB coerces — same class as
the existing search args, documented caveat).

**Runtime behavior:** Apply sets `filter=1` + `fp_*` params; the handler builds filter
WHERE fragments *before* the search block (sqlite binds positionally — placement matches
WHERE text order) reusing the existing `argIdx` numbering on pg/mssql; final WHERE =
`(<search ORed>) AND (<filters ANDed>)` via a `parts` join that degrades to today's exact
behavior when no filter exists. Missing/empty param → that filter block is skipped.
Pagination echoes `&filter=1&fp_x=...` so filters survive page changes (extend the shared
`pagination(...)` templ with a `filterQS` string arg). CSV export deliberately untouched
(no filter support in v1, mirrors its existing lack of search). Security posture intact:
only columns/literals from the trusted YAML and bound param values reach the SQL.

**Touch points:**
1. `internal/types/resource.go` — `FilterConfig`/`FilterParam`; `Filter` on list/card.
2. `internal/filterexpr/` (new) — parser + compile: `SQL(driver, colPrefix)` →
   (frag, placeholders in text order, param usage), `Columns()` for validation.
3. `internal/generator/handler.go` — filter build block per driver in
   `generateListHandler`/`generateCardHandler` (token replace, contains-wrapped args,
   whole-filter-skip on empty param), `net/url` import (only when a filter exists),
   `filterClauses` + `parts` WHERE join, `FilterData` population.
4. `internal/generator/viewmodels.go` — `FilterData`/`FilterParamData` (`Key`,
   `Label`, `Value`) + `Applied` flag on `ListData`/`CardData`.
5. `internal/generator/templ.go` — collapsible filter section in
   `generateListTempl`/`generateCardTempl` (toggle + GET form echoing
   `search/sort/order`, prefilled param inputs, Apply/Clear); `pagination(...)`
   gains `filterQS`. No-filter resources emit nothing new.
6. `internal/parser/validator.go` — `where` required; params count ≥ max `$N` when
   `params` present; param names non-empty + unique.
7. `internal/schema/references.go` — record columns referenced by
   `list.filter`/`card.filter` (Section `"list.filter"`, `"card.filter"`) so the editor
   Validate screen flags missing columns; `cmd/yaga/editor/validate.go` goTo mapping.
8. `cmd/yaga/editor/` — list/card sub-editor gains a "Filter" page (label/where
   text inputs + name/label params list, reusing `listSpec`).
9. `cmd/yaga/ai_spec.md` (cheat-sheet example), `cmd/yaga/demo.go` (one demo
   resource, e.g. orders `status = $1`, to exercise the feature end-to-end).
10. Docs — `SPEC.md` (schema + DSL), `README.md`, `TESTs.md`, `AGENTS.md`.

**Tests / exit criteria:** `internal/filterexpr` unit tests (grammar, precedence,
`$2`-before-`$1` ordering, pg/sqlite/mssql SQL output, contains arg wrapping, column
extraction, qualification); generator tests via `assertGeneratedGoParses` (emitted
fragments per driver, token-replacement code, missing-param skip, literal-only filter,
pagination arg; existing no-filter tests stay green); parser validation tests;
editor goTo test. Gates: `go build ./...`, `go vet ./...`, `go test ./...`,
`gofmt -l .`; E2E — `init --demo` → demo YAML filter → `generate` → `make` → login,
exercise filter/collapse/pagination. Regression guard: filter-off output stays
byte-identical.

---

### Order, dependencies & cross-cutting

**D2 → D3 → D5 → D6.** Rationale: D2's audit INSERT weaving and transactional single-row
ops establish the op-wrapping pattern that D3 (transactional import) and D6 (transactional
proc batches) mirror; D3's `buildCreateParams` refactor is a hard dependency for CSV import
reuse; D5 and D6 are independent and smallest — last. **D7 is independent of D2–D6**
(CLI-only, no generator changes; smallest surface) and can slot in alongside any milestone.
**D8 (no-npm build) is independent of D2–D7** — it touches only the build/asset tooling, not
generated-app handlers or the editor.
**D10 (rename to YAGA) is independent of all other phases** and lands last: it renames the
project, module path, CLI, repo and the very docs/`cmd` paths this roadmap references.
**D11 (list/card filters) is independent of D2–D9; it should land after D10 since its
docs/code references and example YAML (demo/ai_spec) touch the renamed paths.**

Cross-cutting for every milestone: version bump `0.8.0 → 0.9.0`; docs
(`SPEC.md`, `README.md`, `AGENTS.md`) updated per milestone; every milestone ends with
`go build ./...`, `go vet ./...`, `gofmt -l .`, and a templ + tailwind compile of a
generated project. Respect the documented generator gotchas: Sprintf spec counting
(biggest risk — most milestones add emitters), `templ.SafeURL` on every new URL-bearing
attr, no `style={}`/conditional `class={}`, IIFE-wrapped inline scripts, driver-aware
placeholders + sqlite arg order, `idColumn(r)` everywhere, no comments in generated code.
Each milestone carries a **regression guard**: feature-off output stays byte-identical
(snippet-asserted in `generator_test.go`).
