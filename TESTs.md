# Automated E2E Tests Plan

## Goal

Add automated end-to-end tests that run the full yaga pipeline against real databases (SQLite, PostgreSQL, MSSQL) using Testcontainers, verifying the generated admin panel works correctly.

---

## 1. Test Infrastructure

### Dependencies (add to `go.mod`)

```go
// test dependencies
github.com/testcontainers/testcontainers-go v0.44.0
github.com/testcontainers/testcontainers-go/modules/postgres v0.44.0
github.com/testcontainers/testcontainers-go/modules/mssql v0.44.0
github.com/stretchr/testify v1.9.0
```

### New Package: `internal/testutil/`

```
internal/testutil/
├── database.go          # Testcontainers setup per driver
├── generated_app.go     # yaga generate + sqlc + go build helpers
├── http_client.go       # Authenticated HTTP client for admin panel
├── fixtures.go          # Minimal YAML configs for testing
└── e2e_test.go          # Main e2e test suite
```

---

## 2. Test Matrix

| Driver | Module | Port | DSN Format | Speed |
|--------|--------|------|------------|-------|
| SQLite | N/A (file) | N/A | `file:./test.db` | ~1s |
| PostgreSQL | `postgres` module | 5432 | `postgres://user:pass@localhost:5432/testdb?sslmode=disable` | ~5s |
| MSSQL | `mssql` module | 1433 | `sqlserver://sa:Password123!@localhost:1433?database=testdb` | ~60s |

### Run Modes

- **Short mode** (`go test -short`): SQLite only (fast, no containers)
- **Full mode** (`go test`): All three drivers in parallel

---

## 3. E2E Test Flow (per driver)

```go
func TestE2E_Driver(t *testing.T) {
    // 1. Start database container (or create sqlite file)
    db, dsn, cleanup := setupTestDB(t, driver)
    defer cleanup()

    // 2. Create temp dir for project
    tmpDir := t.TempDir()

    // 3. Write minimal yaga.yaml + schema + queries to tmpDir
    //    (or use demoYAML for full feature coverage)

    // 4. Update connections.default.dsn = dsn in config

    // 5. Run: yaga generate --config tmpDir/yaga.yaml --out tmpDir/admin

    // 6. Run: cd tmpDir/admin && sqlc generate

    // 7. Run: cd tmpDir/admin && go build -o admin .

    // 8. Start admin binary on random port
    // 9. HTTP client: login -> test CRUD + actions + hooks + bulk + export
    // 10. Assert responses
}
```

---

## 4. Test Coverage Per Driver

| Feature | Test Case |
|---------|-----------|
| **Auth** | Login with admin user, session cookie, logout |
| **List** | Pagination, search, sort, default_sort |
| **Detail** | View record, field rendering (badge, datetime, email) |
| **Create** | Form GET, POST with all field types, password hashing |
| **Update** | Form GET (populate), POST |
| **Delete** | Confirm dialog, POST |
| **Actions** | Custom action with confirmation, icon, color |
| **Bulk** | Select rows, bulk action toolbar |
| **CSV Export** | `/export/csv` returns CSV |
| **Hooks** | Before/after create/update/delete/action fire |
| **Card view** | Grid + kanban mode |
| **Pages** | Dashboard widgets (stat, chart, table, list, html) |
| **RBAC** | Role-based access (if auth table has roles) |

---

## 5. Key Implementation Details

### Parallel Execution
- Use `t.Parallel()` for each driver sub-test
- Each test gets its own temp dir and random port

### MSSQL Considerations
- Container startup is slow (~30-60s) - start once per package with `TestMain`
- Use `mcr.microsoft.com/mssql/server:2022-latest` (accept EULA)
- SA password must meet complexity: `Password123!`
- Consider Azure SQL Edge for lighter image

### SQLite
- No container needed - use `modernc.org/sqlite` in-memory/file
- Fastest - run in `-short` mode

### PostgreSQL
- Official `postgres:16-alpine` image
- Ready in ~3-5s

### Generated App Binary
- Build once per driver per test run (cache in temp dir)
- Or build once in `TestMain` and reuse across tests

---

## 6. Directory Structure

```
yaga/
├── internal/testutil/
│   ├── database.go          # Testcontainers setup per driver
│   ├── generated_app.go     # Generate + build helpers
│   ├── http_client.go       # Authenticated HTTP client
│   ├── fixtures.go          # Minimal YAML configs
│   └── e2e_test.go          # Main e2e test suite
├── cmd/yaga/
│   └── e2e_test.go          # Integration test entry
└── TESTs.md                 # This file
```

---

## 7. CI Integration (GitHub Actions)

```yaml
# .github/workflows/e2e.yml
name: E2E Tests

on: [push, pull_request]

jobs:
  e2e-short:
    name: E2E (SQLite only)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go test -short ./internal/testutil/... -v

  e2e-full:
    name: E2E (All drivers)
    runs-on: ubuntu-latest
    timeout-minutes: 30
    services:
      postgres:
        image: postgres:16-alpine
        env:
          POSTGRES_PASSWORD: test
          POSTGRES_DB: testdb
        ports: [5432:5432]
        options: >-
          --health-cmd "pg_isready -U postgres"
          --health-interval 5s
          --health-timeout 5s
          --health-retries 10
      mssql:
        image: mcr.microsoft.com/mssql/server:2022-latest
        env:
          ACCEPT_EULA: Y
          SA_PASSWORD: Password123!
          MSSQL_PID: Developer
        ports: [1433:1433]
        options: >-
          --health-cmd "/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P Password123! -Q 'SELECT 1'"
          --health-interval 10s
          --health-timeout 10s
          --health-retries 20
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.26' }
      - run: go test ./internal/testutil/... -run TestE2E -v -timeout 20m
```

---

## 8. Minimal Test Fixture (fixtures.go)

Instead of the full demo (6 resources, slow), use a minimal config:

```yaml
version: "1.0"
panel:
  id: admin
  path: /admin
  name: "Test Admin"
connections:
  default:
    driver: {{DRIVER}}
    dsn: {{DSN}}
sqlc:
  queries_dir: ./sql/queries
  schema_dir: ./sql/migrations
  output_pkg: internal/data
auth:
  table: users
  login:
    fields: [email, password]
    redirect: /admin/dashboard
resources:
  - name: User
    list:
      columns:
        - name: id
          type: integer
        - name: name
          type: string
          searchable: true
        - name: email
          type: email
        - name: created_at
          type: datetime
    detail:
      query: GetUser
      fields:
        - name: id
        - name: name
        - name: email
    form:
      create:
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
        fields:
          - name: name
          - name: email
    actions:
      - name: deactivate
        label: "Deactivate"
        query: "UPDATE users SET status = 'inactive' WHERE id = $1"
pages:
  - name: Dashboard
    path: /dashboard
    default: true
    widgets:
      - type: stat
        label: "Total Users"
        query: "SELECT COUNT(*) FROM users"
navigation:
  - group: "Test"
    items:
      - resource: User
```

SQL schema (driver-aware):
- SQLite: `INTEGER PRIMARY KEY AUTOINCREMENT`, `datetime('now')`
- PostgreSQL: `SERIAL PRIMARY KEY`, `TIMESTAMPTZ DEFAULT NOW()`
- MSSQL: `IDENTITY(1,1)`, `DATETIME2 DEFAULT GETDATE()`

---

## 9. Clarifying Decisions Needed

1. **Minimal vs Demo config**: Use minimal for speed (recommended) or full demo for coverage?
2. **MSSQL in CI**: GitHub Actions MSSQL image is ~1.5GB. Options:
   - Use Azure SQL Edge (lighter)
   - Skip MSSQL in CI, run locally only
   - Self-hosted runners
3. **Test `init --db`**: Also test introspection flow (connect to existing DB with data)?
4. **Parallelism**: Run all 3 drivers in parallel (needs more CI resources) or sequentially?
5. **Session files**: Automate scenarios from `session-ses_*.md` manual test notes?

---

## 10. Implementation Order

1. Add test dependencies to `go.mod`
2. Create `internal/testutil/database.go` with testcontainers setup
3. Create `internal/testutil/generated_app.go` with generate/build helpers
4. Create `internal/testutil/http_client.go` with login/request helpers
5. Create `internal/testutil/fixtures.go` with minimal YAML fixtures
6. Write `internal/testutil/e2e_test.go` with driver matrix tests
7. Add GitHub Actions workflow
8. Run locally, iterate, then push

---

## 11. Estimated Effort

| Task | Estimate |
|------|----------|
| Test infrastructure (database.go, helpers) | 2-3 days |
| E2E test suite (fixtures, assertions) | 2-3 days |
| CI integration + MSSQL tuning | 1-2 days |
| **Total** | **~1 week** |

---

## 12. Related Files

- `cmd/yaga/demo.go` - demoYAML(), demoSchema(), demoQueries(), seedDemoDB()
- `internal/generator/generator_test.go` - existing unit tests for generated code
- `internal/generator/plugin_test.go` - plugin integration tests (uses go toolchain)
- `SPECv05plus.md` - lines 315-316 (verification strategy)