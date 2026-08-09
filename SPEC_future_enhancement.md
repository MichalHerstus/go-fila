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
- Auth: wire the stubbed `remember_me` / `password_reset` / `registration` flags
  (`internal/types/config.go:107`); optional 2FA.
- API mode: read/write REST endpoints + scoped tokens for headless integrations.
- Audit log resource (actor + action + row on create/update/delete/action).
- CSV import; per-resource export column selection.
- Plugin system (see `SPECv05plus.md` M4).
