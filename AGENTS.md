# yaga — Agent Guide

## Build

```sh
go build ./cmd/yaga          # the project binary
go build ./...                   # skips nested admin/ modules (separate go.mod)
```

Single binary at `cmd/yaga/main.go` — stdlib flags, no cobra/viper.

## Init → Generate flow

```sh
yaga init --db DSN # introspects the DB, writes yaga.yaml with a captured `schema:` block + admin login
yaga edit          # interactive TUI editor for yaga.yaml
yaga generate      # generates admin/ app offline (no sqlc, no DB connection); tailwind non-fatal
cd admin
make               # builds the dashboard binary + assets (css + templ + go build)
```

D11: the DB is the sole schema source. `init` without `--db` is an error (no
scaffold/`--demo`); the introspected schema is captured into the YAML `schema:`
block (tables, pk, view flag, columns with yaga field types, foreign_keys with
label) and the generator derives list/detail/form/options SQL from it at
generation time. The generated app never runs sqlc; `admin/sql/` is not produced.

### AI-assisted editing (D7)

`yaga edit --prompt "Change dashboard title to: Order management"` edits `yaga.yaml` via AI instead of launching the TUI (non-interactive, opt-in — no `--prompt` → TUI unchanged). `--prompt` also accepts `file://PATH` (with `~/` expanded to home) to read a long/multi-line instruction from a file — resolved by `resolvePrompt` inside `editAI`. Flags live on `edit` only and are parsed by `parseEditFlags` (`cmd/yaga/ai.go`), which scans `os.Args[2:]` independently of `parseGlobalFlags` (both skip the flags they don't know): `--apikey` (falls back to `OPENROUTER_API_KEY` env, then the `.ENV` file), `--model` (falls back to `.ENV`, else `openrouter/auto`), `--dry-run` (preview, never write). **`.ENV` persistence:** after a successful model run `persistEnv` writes the effective key (`OPENROUTER_API_KEY=…`) + model (`MODEL=…`) as dotenv lines into `.ENV` in the current folder (0600, other lines preserved, path via `envPathFunc`), so a later run can omit `--apikey`/`--model`; precedence is `--apikey` > `OPENROUTER_API_KEY` env > `.ENV` for the key and `--model` > `.ENV` > default for the model. **Providers:** any `--model` value routes to OpenRouter (the default, `openRouterBaseURL` = `https://openrouter.ai/api/v1`, requires a key); the sentinel `--model "lmstudio"` routes to a local LM Studio server (`lmStudioBaseURL` = `http://127.0.0.1:1234/v1`, OpenAI-compatible, **no API key** — a stale key is dropped so it is neither sent nor persisted). LM Studio rejects a `model` field that doesn't match a loaded model, so `lmStudioModelID` GETs `{base}/models` up front and uses the first loaded id. The full config is sent to the provider — OpenRouter, or the local server — documented in usage text, consent is supplying the key + prompt. SQL/`sql/queries` files are never touched by the AI path. Output prints only the **changed keys and their new values** (`changedPaths`/`diffValue`) as `path -> 'value'` lines — e.g. `panel/name -> 'Order management'`, `resources/User/list/columns/email/label -> 'Primary email'` — one line per changed leaf, never the whole file.

**Flow (`editAI` → `proposeEdit`):** `parser.ParseFile` → `yaml.Marshal` current → `buildEditPrompt` (system message = output contract "return ONLY the changed sections of the config as a YAML fragment in a ```yaml fence, omit unchanged sections, include only the top-level keys you changed, keyed-item lists by name / navigation groups by group / items by resource/page/url, keep version"; user message = embedded `ai_spec.md` cheat-sheet (`//go:embed ai_spec.md`, kept compact so tokens stay low) + current YAML + instruction) → `chatCompletions` POST `/chat/completions` (stdlib `net/http`, `Authorization: Bearer` only when a key exists, `temperature: 0`, 300 s timeout, key never logged; a `spinner` on stderr shows a live progress line so the CLI never looks frozen). For the LM Studio provider `proposeEdit` first calls `lmStudioModelID` (GET `{base}/models`, 10 s timeout, descriptive errors for unreachable server / no loaded model) and uses the discovered id in place of the `lmstudio` sentinel for both chat attempts. → `extractYAMLBlock` (```yaml fence, then any fence, then whole trimmed text) → **`mergeYAML`** (yaml.v3 Node merge: mappings recurse key-by-key, sequences merge item-by-item by identity key — `name` for resources/pages/fields/actions/columns, `group` then `resource`/`page`/`url` for navigation, whole-list replace for keyless lists like widgets; unknown top-level key / empty / non-mapping fragment → error feeds the retry; null fragment values leave the target untouched, no deletion support) → `parser.Parse` + `parser.Validate`. On merge/validate failure it retries **once**, feeding the error back; network/API errors are not retried and the original file stays untouched. Valid output is normalized via `yaml.Marshal` (so the written bytes always round-trip) and written with `os.WriteFile(configPath, 0644)`; the same compact `changedPaths` leaf diff (path per changed key, keyed-list identity values inline, strings single-quoted, keyless lists by index) prints in both write and `--dry-run` mode. The API key and the model output never appear in the error paths (only the HTTP status/body snippet on non-200).

**Testing:** `ai_test.go` drives `editAI` against an `httptest` provider stub (`stubProvider` records each decoded `chatRequest` + `Authorization` header; GET `/models` answers the loaded model id, so the same stub stands in for OpenRouter and LM Studio) — happy path writes + path/value diff and preserves unrelated sections, retry-on-invalid feeds back the validator error, second-invalid fails leaving the file untouched, `--dry-run` never writes, missing key/prompt → clear errors, HTTP error → original preserved, LM Studio happy path (discovered model id sent, no auth header, stale key ignored), LM Studio no-model-loaded error, plus a `mergeYAML` unit suite (mapping deep-merge, keyed-item resource/fields/navigation merge, item append, wholesale widgets replace, null leaves untouched, unknown-key/malformed/empty/non-mapping fragment errors) and a `changedPaths` suite (scalar/resource/column/navigation/index paths, added-resource leaves, value quoting, no-changes).

### `init --db` — Database introspection

`yaga init --db {connection_string}` connects to an existing database, introspects its schema (tables, columns, primary keys, foreign keys), and generates `yaga.yaml` with a captured `schema:` block from the discovered tables. Works for SQLite, Postgres and MSSQL (MSSQL DSN prefix `sqlserver://` or `mssql://`; see "MSSQL-specific gotchas" below).

**Driver detection:** DSN prefix `postgres://` or `postgresql://` → postgres; everything else (file path, `:memory:`) → sqlite. Uses `github.com/jackc/pgx/v5/stdlib` for postgres and `modernc.org/sqlite` for sqlite.

**What it does:**
1. Connects to the DB, introspects schema via `information_schema` (postgres) or `PRAGMA` (sqlite)
2. If `users`/`roles` tables are missing, creates them with default roles (admin/manager/user) and inserts an admin user (`admin@admin.test` / `admin`, bcrypt-hashed)
3. If `users`/`roles` already exist with data, respects them as-is (no admin user inserted)
4. Generates `yaga.yaml` with a resource per table (excluding `users`/`roles`) — list/detail/form sections, FK relation fields with `options_value`/`options_label`
5. Emits the introspected schema as the `schema:` block — the sole schema source for the offline generator (no SQL query files, no `schema.sql`; only auth-table DDL when auth tables were created)

**Type mapping:** `int`/`serial` → `integer`, `varchar`/`text` → `string`, `bool` → `boolean`, `timestamp`/`date` → `datetime`, `real`/`float`/`numeric` → `float`, `json`/`jsonb` → `json`, `bytea`/`blob` → `file`.

**FK handling:** Foreign keys become `relation` fields in forms with `options_value`/`options_label`; the option query is derived at generate time from the `schema:` FK metadata. In list views, FK columns are replaced with LEFT JOINs showing the foreign table's label column (preferred: `name`, then `title`, then `label`, then first non-PK text column).

**Auth table DDL:** Postgres uses `SERIAL`/`TIMESTAMPTZ`; SQLite uses `INTEGER PRIMARY KEY AUTOINCREMENT`/`datetime('now')`.

Flags: `init --db <dsn> --config <yaml> --out <dir> --force` (short variants `-d`, `-c`, `-o`, `-f`).

### `edit` — Interactive YAML config editor

`yaga edit` opens the YAML config in a terminal UI built with `rivo/tview` (tcell). Persistent 3-pane layout: left `tview.List` menu + right `tview.Pages` stack + title/status bars. Spec: `SPEC_yaml_editor.md` (single source for the editor).

**Keys:** Ctrl+S save (runs `parser.Validate` on a copy first; errors in a modal, file not written), Ctrl+V validate (opens the Validate screen — same as the "Validate (Ctrl+V)" nav item), Ctrl+P go-to (opens the cd-navigation dialog; Ctrl+> is an alias), Ctrl+O home (jump to the overview; Ctrl+/ is an alias), Ctrl+Q/F10 quit (Save&Quit / Discard / Cancel modal when modified), Esc back (app-level `SetInputCapture` popping a `history []string` page-name stack), `a`/`d` add/delete in list editors, Enter edit. **Every button also gets a Ctrl+key shortcut from the first free letter of its label** (e.g. Ctrl+G for a "Generate missing queries" button), shown as a `(Ctrl+X)` hint in the label — `shortcuts.go` (`takeShortcut`/`addButton`/`addButtonPref`/`addModalButtons`/`bindShortcuts`). **Ctrl+B is reserved for "Back"**: `backButton` pins Back to Ctrl+B (via `addButtonPref` with pref `'B'`), and `takeShortcut` skips `B` for every other button, so a "Brand"/"Before hooks" button that would otherwise take `B` falls through to its next free letter (e.g. Ctrl+R) and Back is always Ctrl+B on every screen. Ctrl+S/Ctrl+V/Ctrl+P/Ctrl+O/Ctrl+Q are reserved (save/validate/go-to/home/quit). Shortcuts are scoped to the displayed screen: collected in `e.pending` while a page/modal is built, bound in `showPage`/`refreshPage`/`showModal` (`e.shortcuts[ctx]`), dispatched from `capture` via `shortcutFor` using the "modal" context while `modalOpen`. The app-level capture runs BEFORE every widget, so a registered Ctrl key fires **even while a text field is focused** (hotkeys beat text-editing combos). `Editor.Run()` returns whether a save happened (re-wired in `edit.go`).

**Cd navigation (`nav.go`):** every screen has a canonical, unique path that doubles as its `tview.Pages` key, its `history` entry and its shortcut context. Root sections: `Panel[/Brand|/Layout|/Theme]`, `Connections/<name>`, `SQLC`, `Auth[/Login Fields]`, `Navigation/<group>[/Items[/<item>]]`, `Resources/<res>[/List[/Columns[/<col>[/Options]]][/Card[/Fields[/<f>][/Validation|Options|Visible|Copies]|/Searchable]][/Detail[/Params|/Fields[/<f>]]][/Form/<Create|Update|Delete>[/Params|/Fields[/<f>]|/Hooks[/Before|After[/<hook>]]]][/Actions[/<act>[/Hooks]]][/Policies][/Children[/<child>[/Columns]]][/SQL[/<query>]]`, `Pages/<page>[/Widgets[/<widget>[/Sub-widgets|/Data Columns]]]`, `Validate`, `Sync`, `Preview[/Page/<p>|/Resource/<r>]`. The dialog (Ctrl+P, Ctrl+> alias) resolves absolute (`~/Panel`, `/Resources`) and relative (`../Columns`, `Options`) paths against the current screen; Enter on a valid path navigates (unknown paths keep the dialog open with an error hint), Tab autocompletes to the longest common prefix of the matching child screens (`completePath`/`finishComplete`), Esc is two-stage (first clears the input, then closes — special-cased in `capture` before the generic modal Esc). `openNav` is ignored while another modal is open; `goHome` (Ctrl+O, Ctrl+/ alias) closes the dialog first and is a no-op on `home`. Matching is case/space-insensitive (`foldSeg`/`matchesSeg`); unnamed/duplicate items use `#<idx>` (`segName`/`findSeg`). `resolvePath`/`resolveSegs` dispatch to per-section resolvers; click-nav buttons and cd-nav must produce identical canonical names (single source of truth via the path helpers in `nav.go`).

**Architecture:** `cmd/yaga/editor/` package. `Editor` owns `*tview.Application`, `*tview.Pages`, `*tview.List` (nav), title/status `*tview.TextView`, `cfg *types.Config`, `configPath`, `modified`, `history`. Config fields are edited **live** via `SetChangedFunc` closures (`widgets.go`: `str/num/yesno/password/long/pick`); anything touching a value calls `markModified`. Collection editors share `listSpec` (`lists.go`): `recordList` + `a/d/Enter`. `promptInput`/`namePrompt` modals add collection members; `tagsPage` toggles `[]string`; `stringMapPage` (`maps.go`) edits `map[string]string` (column/field options, query params). Sub-screens: `menu.go` (Panel/Brand/Layout/Theme, Connections, SQLC, Auth incl. **login rate limit**, Navigation, Pages+widgets), `settings.go` (**Audit**, **Procedures**, **Plugins** — the D2/D6/D5 top-level blocks), `resource.go` (Resources + List/Card/Detail/Form/Policies + **SQL queries**), `columns.go`, `fields.go`, `actions.go`, `hooks.go`, `sqledit.go`, `validate.go`.

**Resource "SQL queries" editor** (`sqledit.go`): the resource form has an "SQL queries" button (head "Queries") → `sqlQueriesPage(idx)` lists every SQLC query that resource references (`sqlQueriesForResource`: list/count/detail/create/update/delete/populate queries + field `options_query`, deduped/sorted, each showing its `sql/queries` file). Enter opens `sqlEditPage(name)`: a full-height `tview.TextArea` editing the query's raw SQL body. Changes are **staged** (`stageQueryEdit` → `schema.RewriteQueryBody` on the effective file text) into `Editor.pendingSQL` (abs path → new file content), marked `modified`, and **flushed by the global save** (`save()` writes staged SQL files before the YAML). `queryBase` resolves a query's file, layering `pendingSQL` over disk so editing a second query in the same file sees the first one's staged change; the sync view still reads disk (saved state). `schema.ParseQueriesForFile`/`schema.RewriteQueryBody`/`Query.RawBody` in `internal/schema/queries.go` support the round-trip (block-preserving: other query blocks in the file stay byte-identical).

**tview v0.42 gotchas** (all hit during the port):
- `Modal` has no `SetTitle`/`SetBorder` — title goes in `SetText` first line; `Form` has no `SetScrollable`.
- **`Pages.SwitchToPage`/`AddPage` never move focus** (`AddPage`'s 4th arg is `visible`, not `focus`). After any page switch you MUST call `e.app.SetFocus(e.pages)` (or `e.nav` when returning home) or focus stays trapped on the nav list and the content form is unreachable. Modals likewise need explicit focus (`showModal` helper).
- `DropDown.SetCurrentOption` fires the changed callback at construction — `pick` guards with a `first` flag or merely building a page marks the config modified.
- **Never call `app.Draw()`/`QueueUpdate` synchronously from an input capture/handler** — they block on the update queue and deadlock the event loop (found via the sim-screen tests). Set `TextView` text directly; tview redraws after each event.
- `SetScreen(tcell.Screen)` is the test seam: `run_test.go` drives the real event loop with `tcell.NewSimulationScreen` and injects keys (Ctrl+S save round-trip, Enter/Esc/Ctrl+Q nav). `e.Run()` also does `app.SetRoot` + `app.SetInputCapture`; don't `Fini()` a sim screen yourself (app.Stop does it).

**Sync screen** (`sync.go`): file-level SQL↔YAML analysis via `internal/schema` — `ParseSchema` (globs `sql/migrations/*.sql`; takes explicit paths), `ParseQueries(sql/queries)`, `CollectReferences(cfg)` (query/table/column refs from YAML incl. options_query + nested page widgets). Renders a **simple scrolling `tview.TextView` list**: schema tables with column counts, sorted SQLC query definitions, then a colored YAML-reference summary of missing queries/tables/columns and missing FK-target `List{Table}` queries (each with detail lines). Buttons (with Ctrl+letter hints via `e.addButton`): `Generate missing queries` (Ctrl+G — writes `{table}.sql` per schema table into `sql/queries`, **never overwrites** existing files), `Refresh` (Ctrl+R — re-run `analyze`), `Back` (Ctrl+B). `importResourcesFromSchema` stays out of the UI (method kept for tests). Driver comes from `schema.Driver(cfg)`. **SQL dir resolution:** `sqlc.queries_dir`/`schema_dir` are relative to the output dir where sqlc runs — `init`/`init --demo` write `sql/` into `./admin/sql`, not next to the YAML. The editor's `sqlBase()` (sqledit.go) therefore resolves them against the config dir when it has any sql tree, else `{configDir}/admin`, else the config dir; `analyze()`, `queriesDir()`, `sqlEditPage`/`sqlViewer` and `generateMissingQueries` all use it. The per-resource SQL display (list/count/detail/form/options_query bodies on the resource editor pages) and the Sync summary are complementary: the editor pages show the SQL *of one resource* live, Sync is the whole-project health check + generator.

**Validate screen** (`validate.go`): full health check over the in-memory config — `runValidation()` runs the structural validator (`parser.ValidateAll` against a YAML copy so defaults are not injected) **plus** the `analyze()` schema/queries pass, and renders one `tview.List` row per problem (red errors / yellow warnings, "No problems found" empty state) with Refresh (Ctrl+R) + Back (Ctrl+B) buttons, mirroring the Sync layout. Structural errors map to a `goTo` via `structuralGoTo` (regexes on `resources[i]`/`pages[i]`/`panel.*` in the message). Schema findings jump straight to the fix location: missing tables → the resource page (`resourceGoTo`), missing columns → `columnGoTo`/`sectionJump` (opens the exact `columnsPage`/`cardFieldsPage`/`detailFieldsPage`/`formFieldsPage` and `SetCurrentItem`s the offending row; single-value sections like `list.default_sort`/`card.kanban_field` open the parent list/card page without a row focus), missing queries → `queryGoTo` (opens the resource's SQL-queries page and highlights the query row), missing FK `List{}` queries → a warning linking to the Sync screen. Requires the schema refs to carry locations: `schema.ColumnRef{Column, Section, Index}` + `References.ColumnRefs` (kept beside the deduped `Columns` summary) recorded by `CollectReferences` for list/card/detail/form sections incl. `default_sort` (leading `-` stripped by `sortColumn`), `card.kanban_field` and `card.searchable`; `internal/parser/validator.go` was split into `ValidateAll(cfg) []error` (collects every problem, defaults still applied) + a thin `Validate` wrapper returning the first error so `parser.Parse`/save keep their behaviour.

**Preview screen** (`preview.go`): ASCII-frame mock of the dashboard (topbar + sidebar from `cfg.Navigation` + per-page widget boxes) and per-resource list mock. The grid chrome (`│ ├ ┬ ┤ ┌ ┐ └ ┘ ─`) is drawn in light blue (`[lightblue]`) while the cell text is white (`[white]`), and every row is padded to the exact same total width (`previewWidth`=78) via `padVisual` (tag-aware: `tview.TaggedStringWidth`) — content row widths are `previewSideWidth`=26 / `previewContentWidth`=49 so the chrome rows (top/bottom borders, column separator) all add up to `previewWidth`. `colorStable` rewrites full color resets (`[-:-:-]`/`[:]`) from content into attribute-only `[::-]` so neither grid nor text color survives emphasis tags intact. No DB, no generated app.

The generated `admin/` contains a `Makefile` (written by `generateMakefile()` in `makefile.go`). Its default `build` target runs every step needed to produce the dashboard binary, in order: `go mod tidy` → `go tool templ generate` → `go build -o <binary> .` (binary name = `--out` basename). Individual steps are also exposed as `templ`, `tidy` targets, plus `run` (build + serve), `package` (bundle into a release tar.gz) and `clean`. **No npm/node, no sqlc and no Tailwind are required** (D8 + D12): Chart.js is embedded into the yaga binary and vendored to `static/js/chart.js` at generation time, and the Tailwind stylesheet is **pre-built** and vendored to `static/css/styles.css` — the generated project never runs a Tailwind binary.

Equivalent manual steps:

```sh
cd admin
go tool templ generate              # compile .templ -> *_templ.go (required before go build)
go mod tidy && go build -o admin .
```

`templ generate` is never run by the generator — only `.templ` sources are emitted, so the build fails until you run it. The generated `go.mod` declares `tool github.com/a-h/templ/cmd/templ`, so `go tool templ generate` resolves templ through the Go toolchain (Go 1.24+) without a manual templ install.

### Pre-built stylesheet (D12)

The pre-built Tailwind stylesheet lives at `internal/generator/assets/styles.css`, embedded via `//go:embed` (`stylesCSS` in `tailwind.go`) and written to `static/css/styles.css` by `generateAssets()`. It is compiled from the kitchen-sink fixture (`testdata/kitchen.yaml`) by the repo target `make styles` → `scripts/build-styles.sh`, which pins the Tailwind **v3.4.19** standalone binary (same OS/arch mapping as the old `make get-tailwind`), generates a kitchen project offline, drops in `scripts/styles.tailwind.config.js` + a `@tailwind` input, and copies the minified output into the embedded asset. Runtime-dynamic classes are **safelisted** in the config because Tailwind's content scan never sees them as literals: `lg:grid-cols-1..12` (card view + stats_grid, `variants: ['sm','md','lg']`), the `max-w-*` allowlist (from `max_content_width`), and the gray badge set (`renderBadge` builds them via `fmt.Sprintf`). The **coverage guard** `TestGenerateStylesEmbedded` (`styles_test.go`) regenerates the kitchen fixture and fails loudly if any literal `class="…"` token in the scanned templ sources (or any safelist entry) is missing from `stylesCSS` — run `make styles` after adding a class.

**Brand colors are RGB channel variables**, not bare `var(--brand-primary)`: the config defines `brand.{primary,secondary}` as `rgb(var(--brand-primary-rgb) / <alpha-value>)`, and both `Base` (`viewmodels.BrandChannels(theme.BrandPrimary)`) and `LoginPage` (generator-side `hexChannels`) emit a `--brand-primary-rgb: r g b;` alongside the existing `--brand-primary: #hex;` in `:root`. A bare `var()` color makes Tailwind silently drop every `/alpha` utility (`bg-brand-primary/10`, `hover:text-brand-primary/80`), so the channel form is required to keep those working. `--brand-*` hex vars are still emitted (Chart.js + the theme JS read them).

**Bounded knobs** (`internal/parser/validator.go`): `card.columns` and stats_grid widget `columns` are clamped to [1,12], and `max_content_width` is validated against the `maxWidths` allowlist (unknown → fall back to `"none"`), all as a non-fatal `parser.Warning` (muted: rendered yellow on the editor's Validate screen, never blocks a save or `generate`). `Validate` skips `Warning`s; `ValidateAll` returns them alongside errors.

### List/Card filter section (D13)

A YAML-defined collapsible filter on list/card views. `list.filter`/`card.filter: {label, where, params}` — the `where` expression is compiled at generation time by `internal/filterexpr` (mini-DSL: `and`/`or`/parentheses, ops `= != <> < <= > >= contains not_contains is_null is_not_null`, literal values or `$N` params). `$N` values are collected via labeled inputs and travel as `fp_<name>` query params; empty params skip the filter. The handler builds filter WHERE fragments **first** in SQL-text order (so sqlite positional `?` binds correctly), then the search block, joined as `(<filter>) AND (<search>)". Pagination echoes `filter=1&fp_...` via `filterQS`. Touch points: types, filterexpr, handler (filterListCore/filterCardCore), viewmodels (FilterData/FilterParamData), templ (filterBar component), schema refs, parser validation, editor filter page. Tested by `TestGenerateFilter` + `TestGenerateNoFilterRegression`.

Flags: `generate --config <yaml> --out <dir> --force --verbose` (short variants `-c`, `-o`, `-f`, `-v`). `--out` basename becomes the module name.

## Driver support (postgres default, sqlite opt-in, mssql opt-in)

Driver comes from the first `connections:.*.driver` value (default `"postgres"`); `isSQLite()` accepts `sqlite`/`sqlite3`; `isMSSQL()` accepts `mssql`/`sqlserver`. Per-driver differences the generator must handle:

| Concern | postgres | sqlite | mssql |
|---|---|---|---|
| `sql.Open` driver | `pgx` + `_ "github.com/jackc/pgx/v5/stdlib"` | `sqlite3` + `github.com/mattn/go-sqlite3` | `mssql` + `_ "github.com/microsoft/go-mssqldb"` |
| go.mod | adds `github.com/jackc/pgx/v5 v5.10.0` | adds `github.com/mattn/go-sqlite3 v1.14.24` | adds `github.com/microsoft/go-mssqldb v1.10.0` |
| LIKE operator | `ILIKE` | `LIKE` | `LIKE` (case-insensitive default collation) |
| bind placeholders | `$N` | `?` (positional, SQL-text order) | `$N` (go-mssqldb loose mode maps `$N`→`@pN`) |
| data-query id type | `int32` | `int64` | `int32` (unless `id_type` overrides; bigint → `int64`) |
| pagination | `LIMIT $1 OFFSET $2` | `LIMIT ? OFFSET ?` | `OFFSET $2 ROWS FETCH NEXT $1 ROWS ONLY` (REQUIRES an ORDER BY; no ORDER BY → emit `ORDER BY (SELECT NULL)`) |

Helpers in `generator.go`: `driver()`, `isSQLite()`, `isMSSQL()`, `placeholder(n)`, `likeOp()`, `idGoType()`, `idGoTypeForResource(r)` (honors `id_type`), and `tableName(r)`/`idColumn(r)` in `handler.go` (honor `table`/`id_column` overrides). **`placeholder()` is still unused** — create/update/delete handlers hardcode `$N` (works on sqlite since mattn binds positionally, and on mssql via loose `$N` parsing). Only the list/card handlers are driver-aware.

### MSSQL-specific gotchas

- **`init --db` DSN**: prefix `sqlserver://` or `mssql://` → driver `mssql`. Introspection uses INFORMATION_SCHEMA + `sys.foreign_keys`, and `sys.columns.is_identity` for tables with no declared PRIMARY KEY (common on MSSQL line-of-business schemas — identity `ID` columns act as the key; they are marked `IsPrimaryKey` so routes key on them and INSERT/UPDATE omit them).
- **`RETURNING` is emitted for mssql Create/Update** (driver != "sqlite") — the generated create/update handlers use raw SQL at runtime, and T-SQL has no `RETURNING` (create-hook id capture uses `OUTPUT INSERTED.<id>`).
- **Postgres/MSSQL list/card pagination count comes from a windowed `COUNT(*) OVER()`**: the list/card data query emits `SELECT {cols}, COUNT(*) OVER() AS _total FROM {table} …` and scans `_total` into `total` per row — a single round trip, no separate COUNT query, so the old `countClauses`/$N-renumbering hack is gone. When the page is empty (rows beyond the last, or a search matching nothing) `totalSet` stays false and the handler falls back to `total = page*perPage` so `totalPages` renders as the current page instead of 0. mssql still needs the `ORDER BY (SELECT NULL)` fallback (see next bullet) and go-mssqldb still validates arg count against the HIGHEST `$N`, which is fine because there is only one query now.
- **ORDER BY is omitted from generated list/options queries for mssql**: a derived table (the `options_sql` wrapper) cannot have ORDER BY without TOP/OFFSET/FOR XML, and `TOP` cannot combine with `OFFSET` — so mssql list queries get `ORDER BY (SELECT NULL)` as a fallback only when no sort is set.
- **MSSQL column names are PascalCase** (`CeleJmeno`, `ZamestnanecID`). The naming convention lowercases the whole identifier and only splits on `_`, so yaga's `snakeToPascal` lowercases input first: `CeleJmeno`→`Celejmeno`, `ZamestnanecID`→`Zamestnanecid`, `role_id`→`RoleID` (still). Row maps are keyed by the raw selected column name, so introspection emits `id_column: ID` when the key column isn't literally `id`.
- **`table:` / `id_type:` / `id_column:` overrides** are emitted by introspection when the convention doesn't match the real schema (e.g. resource `Zamestnanec` → `table: Zamestnanec`; bigint PK → `id_type: int64`; `ID` column → `id_column: ID`). The generator's `tableName()`/`idColumn()`/`idGoTypeForResource()` fall back to the old conventions when the fields are absent.
- Sanity check in generated main.go for mssql is `SELECT TOP 1 1 FROM {table}` (`TOP 1` replaces `LIMIT 1`).

### Generated main.go: DB sanity check runs BEFORE binding the port
`generateMain()` (`main.go`) emits `sql.Open` → `db.Ping()` → **sanity query against `{auth.table}`** (mssql: `SELECT TOP 1 1 FROM …`; others: `SELECT 1 FROM … LIMIT 1`; `sql.ErrNoRows` treated as OK) → only then `net.Listen` + `srv.Serve`. **`connections.*.pool` settings** (`max_open_conns`/`max_idle_conns`/`conn_max_lifetime`) are emitted as `db.SetMaxOpenConns`/`SetMaxIdleConns`/`SetConnMaxLifetime` setters right after `Ping()` and before the sanity query (lifetime parsed via `time.ParseDuration`, silently skipped on error); no setters when the block is absent (tested by `TestGeneratePoolSettings`). Rationale: mattn/go-sqlite3 silently **creates an empty DB file** when the file is missing, so `db.Ping()` succeeds against a "not found" database and the dashboard would otherwise bind the port and run broken (`no such table`) while holding it — a restart then hits `address already in use`. The sanity query makes a missing/uninitialized DB a fatal startup error **before** the port is bound. The listen port is resolved as `--port` flag (`-p` alias) → `ADDR` env → `:8080` (`flag.Int("port", 0, ...)` + `flag.IntVar(port, "p", 0, ...)`; stdlib `flag` accepts both `--port 9090` and `-port 9090`); the emitted `Makefile` `run` target passes `--port $(PORT)` (`PORT ?= 8080`). Request logging is controlled by `--log` (`-l` alias), values `full` (default, chi's `middleware.Logger`, logs every request) or `err` (only requests that produced an error response, status >= 400) — the flag value is threaded through `NewRouter(db, logLevel)` and selects `middleware.Logger` vs the generated `errorOnlyLogger` (a `middleware.NewWrapResponseWriter` wrapper that `log.Printf`s only when `Status() >= 400`); the `Makefile` `run` target passes `--log $(LOG)` (`LOG ?= full`). `--help`/`-h` prints the command line syntax + flag meanings via `flag.Usage` (custom `flag.PrintDefaults` wrapper) and exits 0 BEFORE any DB or session work. **The generated `auth/session.go` must NOT use a package `init()`** — its `Init()` is called explicitly by `main()` right after the help check (before `sql.Open`), so `-h/--help` runs clean without the `SESSION_SECRET` warning/fail-fast, while production fail-fast still happens before the port binds (`GetSession` also lazily calls `Init()` if `Store` is nil). Generated server also does graceful shutdown on SIGINT/SIGTERM (`signal.NotifyContext` → `srv.Shutdown`) and logs a `is another dashboard instance already running?` hint on bind failure. Keep the bind AFTER the DB checks — ordering is what prevents a broken DB from occupying the port.

### sqlite list handler arg order (critical)
mattn binds `?` args positionally in SQL-text order, so sqlite branch appends **search args first, then `LIMIT ? OFFSET ?`**, and uses `LIKE`. The postgres branch appends `perPage, offset` first with `ILIKE $N` + `LIMIT $1 OFFSET $2`. Mixing these up silently returns wrong rows on sqlite.

## Generator pipeline (files in `internal/generator/`)

`generator.go` orchestrates calls to (in order):

1. `ensureDirs()` — directory layout (also creates `internal/hooks`)
2. `generateMain()` (`main.go`) — `main.go` with driver-aware `sql.Open`
3. `generateRouter()` (`router.go`) — chi routes + RBAC wiring, page handlers
4. `generateAuth()` (`auth.go`) — login/logout, session, RBAC middleware
5. `generateData()` (`data.go`) — `internal/data` Get queries derived from the `schema:` block
6. `generateResource()` → per-resource handlers (`handler.go`): list, **card**, detail, create, update, **delete, action, bulk, CSV export** (hooks wired into create/update/delete/action)
7. `generatePage()` — page handlers with widget DB queries
8. `generateViews()` (`templ.go`) — all `.templ` views
9. `generateHooks()` (`hooks.go`) — shared `internal/hooks/hooks.go` (Scope + stubs)
10. `generateProcedures()` (`procs.go`), `generateAuditSchema()` (`audit.go`)
11. `generateGoMod()` (`mod.go`, declares the templ `tool` directive), `generateMakefile()` (`makefile.go`), `generateViewModels()` (`viewmodels.go`), `generateAssets()` (`tailwind.go`)

All generation uses `os.WriteFile` + `fmt.Sprintf`, never `text/template`. Nothing touches the live DB or any sqlc tooling.

## Resource handler SQL strategy

| Operation | SQL approach |
|---|---|
| List | Raw SQL with dynamic WHERE/ORDER BY/LIMIT |
| Card | Raw SQL identical to list; `LIMIT = Rows*Columns`, grouped into kanban columns |
| Detail | Generated data query (`data.GetUser(db, int64(id))` → `map[string]interface{}`) |
| Create POST | Raw SQL INSERT via `db.ExecContext` |
| Update GET | Generated data query (`data.GetUser`) |
| Update POST | Raw SQL UPDATE via `db.ExecContext` |
| Delete | Raw SQL DELETE via `db.ExecContext` |
| Action | Raw SQL per action name, or stored-proc call when `proc:` is set (switch dispatch) |
| Bulk | Raw SQL per bulk action name, looped once per selected id, **inside one transaction** (see "Bulk actions") |
| CSV Export | Raw SQL SELECT + `encoding/csv` |

Create/update/delete SQL uses `idColumn(r)` (or the explicit column list) for the row key, honoring `id_column:` overrides on mssql. Create/update avoid typed params because `r.FormValue` returns `string`; raw SQL `ExecContext` accepts `interface{}`. **Boolean fields are emitted as `r.FormValue(name) == "true"`** (a Go `bool`), not a raw `r.FormValue(name)` string — otherwise an unchecked checkbox posts `""` and Postgres fails with `invalid input syntax for type boolean: ""` (BUG-3). mssql/pgx/mattn accept a `bool` value directly.

Detail/update data queries must cast the id to `idGoType()` — sqlite ids are `int64`, postgres `int32`. A literal `int32(id)` breaks the sqlite build.

### FK label columns need LEFT JOINs in the raw list/card/export SQL
Introspection adds a `{fk}_label` list column per foreign key, and the generated **list/card/export handlers build their own raw SQL** — a bare `SELECT …, {fk}_label FROM {table}` fails with `column "{fk}_label}" does not exist`. `listSelectFrom()` in `handler.go` reconstructs the JOINs at generation time: for each view column ending in `_label` it prefers the schema block's FK metadata (`ForeignTable`/`ForeignColumn`/`Label`), falling back to a matching relation form field (`options_value`, `options_label`), and emits `LEFT JOIN {ftable} f_{ftable} ON f_{ftable}.{value} = t.{fk}` with `f_{ftable}.{label} AS {fk}_label` in the select. When joins exist, real columns are qualified with the `t.` alias (search/ORDER BY too) and the single windowed `COUNT(*) OVER() AS _total` runs inside the joined data query (the count reflects the joined row set). A `_label` column with no matching relation field falls back to the unjoined behavior.

## Card view (`card` section)

Optional per-resource view at `GET /{panel}/{resource}/cards` (reachable via a "Cards" button on the list view). Fields reuse the form `Field` type. `columns` (X = cards/row) and `rows` (Y = rows/page) define `per_page = X*Y`; pagination + search mirror the list handler (driver-aware, `LIKE`/`ILIKE`). When `card.kanban_field` names a **select** field, the handler groups rows into `KanbanColumns` via `viewmodels.OptionValue(item[field])` instead of a grid — `g.Card.Kanban` flips the shortcut in `cards.templ` (grid vs. board). The grid templ hardcodes `lg:grid-cols-{Columns}`; kanban buckets are keyed by the select's option keys plus any extra row values discovered at request time.

### Field renderer for `gps`
`renderCell` maps `gps` → `@renderGPS` and the form emits a text input with `lat, lng` placeholder; `renderGPS` renders a link out to Google Maps. Registering a new field type means updating BOTH `renderCell`'s switch and the form-input switch in `templ.go`, plus `FieldTypes` in `types/field.go`.

### Modal record picker (`select`/`relation` fields with `options_sql` or schema FK)
A form field of type `select` or `relation` renders as a **modal record picker** instead of a plain select whenever the option SQL resolves (D11: `options_sql:`, else derived from the schema block's FK metadata via `g.optionSQL`; legacy `options_query:` still parses): a hidden input (name = field, value = option key) + a read-only display input (the option label) + a "Browse…" button. Clicking Browse opens a shared modal (emitted once per form by `pickerFooter()` when any field is a picker) listing all options as clickable rows; a search box filters rows client-side; selecting a row sets the hidden input and the display label and closes the modal. Generated pieces (all in `templ.go`): `isPickerField()` (guards rendering, uses the same `optionSQL` resolution as the loader so templ and handler always agree), `pickerMarkup()` (per-field markup + script), `pickerFooter()` (shared modal + search/close wiring), plus `viewmodels.OptionLabel()`/`ItemValue()`/`OptionsJS()` helpers in `viewmodels.go`. `ColumnDef.Picker` is set true on the form's field defs for picker fields (`formFieldDefsWithOpts` in handler.go). Gotchas:
- **templ never evaluates `{ expr }` inside `<script>` content** — it writes the text literally. The option map must reach the JS via a data attribute (`data-picker-options={ viewmodels.OptionsJS(data.Fields, "field") }`) whose value is HTML-escaped JSON the browser decodes on `dataset` read, then `JSON.parse`d in the click handler.
- **The modal element is rendered AFTER the per-field scripts** (`pickerFooter()` is appended at the end of the form). The `#record-picker-modal` lookup MUST happen inside the click handler, not at script parse time — otherwise `pickerModal` is `null` and the handler's `if (!pickerModal) return` silently no-ops (the button appears dead).
- **Each per-field script must be wrapped in an IIFE** `(function() { ... })();` — top-level `const` in classic scripts lives in the shared global lexical environment, so the second picker script's `const pickerBtn` throws `Identifier 'pickerBtn' has already been declared` and the whole script block (and the product_id/role_id picker wiring) is discarded.
- Multiple picker fields on one form (e.g. orderline: `order_id` + `product_id`) need per-field scoping — each options div and querySelector carries `data-field="<name>"`.
- `viewmodels.OptionsJS` marshals the option map with `encoding/json` (sorted keys, `{}` fallback). `viewmodels.ItemValue` returns `""` for nil so the create form (empty `Item` map) doesn't render/Submit `"<nil>"`.
- `formFieldDefsWithOpts` in handler.go sets `Picker` only for `select`/`relation` fields that resolved an option SQL var; plain `select` fields with inline `options` keep the old `<select>` renderer.

### Auto-fill from the picker (`copies:`)
A picker field may carry `copies: {targetFormField: relatedColumn}` (types `Field.Copies`); selecting a row writes those related columns into sibling form inputs by name. `buildOptionsLoader` extends the per-field loader: for **FK-derived** option SQL it appends the copy source columns to the inner SELECT (`SELECT value, label, col1, … FROM table`, sorted-target deterministic order); a **custom `options_sql`** is wrapped verbatim and must expose them itself — a missing column is a runtime SQL error. Each copy value is formatted per the target field type via `viewmodels.PickCopyValue` (`datetime`/`date` targets get `2006-01-02T15:04` / `2006-01-02`, NULL → `""`). The loader emits a parallel `{field}Copies := map[string]map[string]string{}` keyed **by target form-field name**; the `ColumnDef` literal gains `CopyData: <var>` (`formFieldDefsWithOpts`), and `pickerMarkup` renders a `data-picker-copies={ viewmodels.CopyDataJSON(data.Fields, "x") }` attribute + a row-click loop that sets `querySelector('[name="<target>"]').value`. Dedup key = resolved SQL + the sorted target:source set, so fields sharing a loader share the copy map too. Editor: field page — "Copies" button → `stringMapPage` at `<field>/Copies` (`cmd/yaga/editor/fields.go` + `nav.go`). Parser emits **warnings** for copies on a non-select/relation field, an unknown target, or a self-copy (`validateCopies`).

### Master-detail children (D14)
A header resource may declare `children:` (`types.ChildResource` on `Resource`). **Detail + edit views embed a read-only lines table**: `childRels()` resolves each child (FK column from the entry or the schema-block reverse FK targeting the parent table/pk via `childLinesParts`), the handler SELECTs the child key + display columns (label-join columns like `{fk}_label` are dropped — they can't be selected from the child table); the shared `loadChildLines` helper lives in `childlines.go` (package-scoped — both detail.go and update.go call it; it is NOT duplicated). The edit form shows per-line Edit (→ child edit, FK locked) / Delete (→ child delete) and an "Add Line" button (→ child create with `?<fk>=<parentId>`), the create form shows a "Save the header, then add lines." note. **Return navigation:** child create/update/delete POST handlers honor `?return=` (path-only, `strings.HasPrefix("/")`, no open redirect). create/update GETs echo `return` into `FormData.Return` rendered as a hidden `_return` input, so the POST redirect survives the form submission; delete reads the query directly. **FK seed + lock**: create/update GET read `?<fk>=` for every picker field (`seedPickersOf`), pre-seed `item[<fk>]` and set a runtime `locked` map; `formFieldDefsWithOpts` emits `Locked: locked[<fk>]` and `pickerMarkup` wraps the Browse button + modal script in a templ `if !viewmodels.FieldLocked(data.Fields, "x")` guard (hidden input + read-only display always render). `ColumnDef` carries `Locked bool`. Templ pieces: `childLinesSection(withActions)` in templ.go renders one `viewmodels.ChildLinesData` (View link on detail; Edit/Delete/Add Line on the edit form); form.templ emits the children block + `_return` hidden input only when the resource has `children:` or a picker field. **Introspection** (`init --db`) emits a `children:` block for every non-view table whose FK targets this table's PK (copied in `writeResourceYAML`); the editor's `Children` screen (`cmd/yaga/editor/children.go`) edits them at `Resources/<res>/Children`. Parser: `validateChildren` — unknown child `.resource`, explicit `column` missing from the child schema, or a missing reverse FK are config errors. Only stock Tailwind classes are used (`mt-4`, `gap-2`, `hover:opacity-90`, …) so `TestGenerateStylesEmbedded` stays green without `make styles`.

## Critical gotchas

### Format specifier counting in Sprintf
Every `%s`/`%q`/`%d` must have a matching arg. `%%` is escaped (produce `%` in output, no arg consumed). A mismatch silently produces garbled Go source (e.g. `%!s(MISSING)` literal in emitted templ). This is especially dangerous when a templ substring is built with its own `fmt.Sprintf` and then inserted into a parent one — any `%v`/`%d`/`%s` **inside** emitted `fmt.Sprintf(...)` calls must be doubled (`%%v`) in the generator source. `buildOptionsLoader`, `preHashCode`, `fileImport` insertions, the `cardBody`/`actions`/`gridView`/`kanbanView` strings in `templ.go`, the `pickerMarkup` format-arg list in `templ.go`, and the `hookCallsStr`/`scopeValuesStr`/postCode builders in `hooks.go`/`handler.go` are common drift points.

### `snakeToPascal` field naming convention
`snakeToPascal` lowercases the whole input, splits only on `_`, and maps the `id` segment to `ID`:
- `id` → `ID`
- `role_id` → `RoleID`
- `user_role_id` → `UserRoleID`
- `CeleJmeno` → `Celejmeno`
- `ZamestnanecID` → `Zamestnanecid`
Any other pattern would produce `Id`/`RoleId`/`CeleJmeno` (wrong). The generated Go field names must follow this convention.

### `Options` field type
Both `Column.Options` and `Field.Options` are `map[string]string` (key=value, value=label), never `[]string`.

### `default_sort` prefix
YAML `"-created_at"` means sort=`"created_at"`, order=`"desc"`. Trim the `-` prefix at generation time.

### `panel.path` must start with `/`
All hrefs use `path + "/" + resourceName`. Router uses `r.Route(panelPath, ...)`.

### Resource naming
PascalCase in YAML (`User`) → lowercase for Go package, dir, URL segment (`user`).

### Page handler naming
Generated as `{CapitalPanelID}{PageName}(db)` (e.g. `AdminDashboard`). Must be exported (capitalized) since `pages` package is separate from `panel`.

### Shared `package views`
All `.templ` files in `internal/views/*` declare `package views` — layout, components, resources, pages. Each subdirectory is a separate Go package; duplicate package names across directories are fine.

### templ `templ.SafeURL` on every URL-bearing attr
templ v0.3.819 requires `templ.SafeURL(...)` for `<a href>` **and** `<form action>` — a bare `fmt.Sprintf(...)` in those attrs is a compile error in generated code. Verified fixes needed this on: list/detail View+Edit links, action/delete form actions, mailto links.

### `formaction` is NOT a SafeURL attr — and no `if`/expressions in `style`
templ treats `formaction`/`formmethod` as plain string attributes: wrapping the value in `templ.SafeURL(...)` is a **compile error** (`cannot use templ.SafeURL as string in templ.JoinStringErrs`). The bulk-action buttons and per-row submit buttons (which must escape the wrapping bulk `<form method="POST">`) use `formaction={ fmt.Sprintf(...) }` + `formmethod="POST"` without SafeURL. Related templ parser rules that fail `go tool templ generate` at parse time: **`style={ expr }` is rejected** ("style attributes cannot be a templ expression") — emit widths via `data-width` attrs and set `el.style.width` from JS instead; **`class={ if cond { "x" } }` is rejected** — use a plain Go helper function (`darkClass(theme)`) returning the class string.

### Dark mode + theming (`viewmodels.ThemeConfig`, `DefaultTheme()`)
M2 wiring: Tailwind runs `darkMode: 'class'`; `theme.extend.colors.brand.{primary,secondary}` + `fontFamily` come from the YAML. `internal/generator/viewmodels.go` defines `ThemeConfig` (DarkMode, BrandPrimary, BrandSecondary, FontFamily, FontMono, SidebarWidth, SidebarCollapsedWidth, SidebarCollapsible, TopbarSticky, MaxContentWidth) built by `DefaultTheme()` with hex defaults `#6366f1`/`#8b5cf6`. Every `layoutviews.Base(title, panelPath, theme, userName, views.Xxx(vd))` call site passes `viewmodels.DefaultTheme()` and `auth.UserName(r)`. `Base` sets `:root { --brand-primary/--brand-secondary }`, `<html class={ darkClass(theme) }>`, a `toggleTheme()` JS + `localStorage['yaga-theme']` persistence (login.templ re-reads it too), and Chart.js reads `--brand-primary` at runtime. `Sidebar`/`Topbar` take the theme param; the sidebar width is set from a JS init using `data-width` and `toggleSidebar()` now toggles `aside.style.display` between shown and hidden (no more collapsed-width state). The topbar's left group holds the nav-toggle + theme buttons; the right group renders the logged-in user name (from `auth.UserName(r)`, populated at login via `SELECT id, COALESCE(name,''), password, COALESCE(role_name,'') FROM {auth.table}`) before the Logout link. **Tailwind `fontFamily` values are comma-separated stacks** ("Inter, sans-serif") — emit them through the `fontStack()` helper in `tailwind.go` which splits/quotes each name (`['Inter', 'sans-serif']`); a single quoted `'Inter, sans-serif'` array item becomes one bogus font-family.

### Bulk actions (`bulk: true`)
`hasBulkActions(r)` (handler.go) guards the router line `r.Post("/{name}/bulk/{action}", name.Bulk(db))` — plain `r.Post`, no RBAC (matches custom-action routes). `generateBulkHandler` writes `bulk.go` (package `{resource}`): `Bulk(db)` reads `r.PathValue("action")`, `ParseForm`, collects `r.Form["ids"]` via `strconv.ParseInt`, `switch`es over bulk-action SQL, loops `db.ExecContext(ctx, q, id)`, redirects to the list (302). **Bulk is transactional**: the whole id-loop runs inside a single `db.BeginTx` — `tx.Commit()` only when every Exec succeeded, `defer tx.Rollback()` otherwise, so a mid-batch failure (e.g. FK violation on one id) rolls back the entire operation instead of leaving a partial update. The emitted body uses `tx, err := db.BeginTx(...)` / `defer tx.Rollback()` / `if err := tx.Commit(); err != nil { httperr.Internal(...) }`. The list templ wraps the table in one `<form method="POST">` when bulk actions exist: a select-all checkbox (`toggleSelectAll`), per-row `name="ids"` checkboxes, per-row action buttons + Edit/Delete as `formaction`/`formmethod` submit buttons (NOT SafeURL), and a "N Selected" toolbar posting to `/{res}/bulk/{action}`. Bulk actions must NOT also render as per-row action buttons.

### Hooks (`hooks.go`, `RETURNING id`)
Hooks attach to `FormAction` (create/update/delete) and `Action`. `internal/generator/hooks.go` emits the shared `internal/hooks/hooks.go` (Scope struct + one stub per unique fn hook, deduped across the whole config) and the `hookCallsStr`/`scopeValuesStr`/`returningClause` snippet builders. Gotchas:
- **Any hook block forces the `hooks` import in the generated handler** — the `hooks.Scope{...}` literal lives in the hooks package, so a sql-only hook still needs `import hooks "…/internal/hooks"`. Condition on `Hooks != nil`, NOT on `HasFn()`. **Proc hooks (driver-aware):** use `g.hookBlockEmits(h)` as the gate for the import/Scope/`RETURNING` — true for any fn/sql hook, or any proc hook when the driver isn't sqlite, or on sqlite **when the proc is declared under `procedures:`**. A proc-only block on sqlite with no declared body must produce no `hooks` import and no `RETURNING` or the generated handler fails with an unused import (byte-identical regression guarded by `TestGenerateProcSQLiteIgnored`).
- **`procs` import is per-handler:** the `internal/panel/procs` import (`g.procImport()`, gated on `usesProcedures()` — sqlite + `len(Procedures) > 0`) is added to a handler only when that handler actually emits a `procs.Exec` call — `hookUsesProc(hooks)` for create/update/delete, and `hasProcs` (any action's `actionProcExec` or proc hook) for actions.go/bulk.go. An unused `procs` import fails the build.
- **create id capture**: only when `g.hookBlockEmits(create.Hooks)` does the create POST switch from `db.ExecContext(query, vals...)` to `db.QueryRowContext(r.Context(), query+" RETURNING <id>", vals...).Scan(&newID)` (postgres/sqlite) or `" OUTPUT INSERTED.<id>"` (mssql, no RETURNING in T-SQL) — `idColumn(r)` drives the column. `scope.ID = newID` runs before after-hooks. The hookless path stays byte-identical (`ExecContext`, no RETURNING).
- `sql` hooks are emitted as `db.ExecContext(r.Context(), "<sql>", scope.ID)` — always pass the SQL as a Sprintf **arg** (`%q`), never concatenate it into a template (a `%` inside hook SQL would corrupt the emitted source). `scope.ID` is 0 for before-create, the new row id after-create, the parsed path id otherwise; `$1` works on sqlite (named-param syntax + positional binding) and mssql (loose `$N`).
- **Stored procedures (`proc:`)** — a third hook kind and an alternative to `query:` on actions (mutually exclusive, enforced by the parser). `procSQL(name)` emits `CALL <name>($1)` on postgres and `EXEC <name> $1` on mssql (go-mssqldb rewrites `$1`→bound `@p1`, passed positionally to the proc's first param); the record id binds as `$1`, same as sql hooks/actions. `procSQL` returns `""` on sqlite and callers skip the emission: `hookCallsStr` drops proc hooks, `actionExecSQL` returns `""` so the action case becomes an empty `{}` block (still redirects) and the bulk loop gets `_ = id` as its body. A proc-only `Hooks` block on sqlite must NOT emit the import/Scope/RETURNING (see `hookBlockEmits` above).
- Hooks run inside the action case's mandatory `{ }` block scope (actions.go); the hook lines use `if err := …` so the later `_, err :=` in the block still compiles. A hook error aborts the request with HTTP 500.
- `bulk.go` does NOT run hooks — bulk reuses the action SQL/proc without the before/after lifecycle.

### Select options render from `data.Fields`, not static HTML
Form select options are rendered at runtime by looping `data.Fields` for the matching field and ranging its `Options`. The generated handler wires the option SQL into `ColumnDef.Options` (`formFieldDefsWithOpts`); the templ compares with `viewmodels.OptionValue(data.Item[f.Name])` because the row map may hold `sql.NullInt64`/`sql.NullString` (a bare `fmt.Sprintf("%v")` on `{1 true}` won't match key `"1"`).

### Value rendering is centralized in `viewmodels.Stringify`
Every value-to-text render in the generated app routes through `viewmodels.Stringify(v)` (in `viewmodels/models.go`), which unwraps `nil`, plain scalars, `time.Time` and every `sql.Null*` type (`NullString`, `NullInt32`, `NullInt64`, `NullFloat64`, `NullBool`, `NullTime`) — returning `""` for `nil`/invalid NULL instead of Go struct text. This fixes two failure classes seen on mssql/postgres (nullable columns) and on every create form:
- *BUG-1*: create forms no longer render empty values as `value="<nil>"` (was `fmt.Sprintf("%v", nil)`).
- *BUG-2*: nullable columns no longer leak `{1 true}` / `{Spojovací materiál true}` in list rows, detail views, or edit-form inputs.
`OptionValue` and `ItemValue` are thin wrappers over `Stringify`; the boolean checkbox checked-state uses `viewmodels.BoolValue` (true only for the true state, so unset/NULL renders unchecked rather than `<nil>`); the datetime/date renderers and form inputs use `viewmodels.TimeValue` + `TimeInputValue`/`DateInputValue`, which unwrap `sql.NullTime` and format in the browser's local `2006-01-02T15:04` / `2006-01-02` layout. The field renderers in `renderers.templ` (`renderBadge`, `renderBoolean`, `renderEmail`, …, `renderFloat`) take `interface{}` and call `Stringify`; a NULL boolean renders an empty cell. `renderers.templ` is emitted per resource view dir AND into `internal/views/components`, and must be run through `prefixImports` (it now references `viewmodels`).

### Shared create/update form renders the union of both field sets
`generateFormTempl` builds the form from the **union** of `r.Form.Create.Fields` + `r.Form.Update.Fields` (deduped by name, create order first then update-only fields appended). Each field is emitted with `if data.IsCreate { … }` (create-only) or `if !data.IsCreate { … }` (update-only) guards honoring the field's `visible:` list, so update-only fields (e.g. `status`, `created_at`) are no longer dropped from the edit form. This fixes BUG-4: before, the shared template was generated from the create fields only, so a field present only in `update` was silently omitted and the edit POST submitted `""` (failing e.g. Postgres `invalid input syntax for type timestamp`). When only one of create/update exists the guards are omitted (behavior unchanged). `hasFile`/`hasPicker`/enctype are computed over the merged set.

### Option loader rows
`buildOptionsLoader` scans into `interface{}` then keys the map with `fmt.Sprintf("%v", val)` — the `id`/value column is usually an `INTEGER` (`int64`), scanning into `string` fails silently. `optionSQL` strips a trailing `;` from the resolved SQL (it is embedded as `SELECT a,b FROM (... ) AS _opt`, a trailing `;` is a syntax error). **Options loading is batched per resource**: when multiple form fields resolve to the same option SQL, the generated handler loads it **once** into a shared `{name}Opts := map[string]string{}` var and every field's `ColumnDef.Options` references it — no N queries for N fields, no duplicate maps, and the load block only exists when at least one field actually resolved an option SQL (tested by `TestGenerateOptionsLoaderDedupe`).

### Generated app is a separate Go module
`admin/` has its own `go.mod`. Module name = basename of `--out` dir. Uses module-relative imports (`internal/data`, `internal/panel`).

### No comments in generated code
All generation uses clean string concatenation. No comments emitted.

### Router: one `r.Route` block with an inner `r.Group`
Login routes and the auth-protected routes live in a **single** `r.Route(panelPath, ...)`; the protected set is an inner `r.Group(func(r chi.Router) { r.Use(auth.AuthMiddleware); ... })`. Two `r.Route` calls on the same path panic (`chi: attempting to Mount() a handler on an existing path`); calling `r.Use` after a registered route also panics (`all middlewares must be defined before routes`).

### Default page mounts at both `/` and its `path`
A page with `default: true` gets `r.Get("/", handler)` **and** `r.Get(pagePath, handler)`. The generator skips only the literal `/` (the default branch already adds it). Without the `pagePath` mount, a `auth.login.redirect: /admin/dashboard` (the `init` default) 404s because only `/admin/` is registered.

### RBAC middleware is appended to middleware.go
`rbacMiddleware` (`checkRole` + `RBACMiddleware`) is built only when a resource has `policies:` and is appended to the generated `internal/panel/auth/middleware.go`. Do not inject `"strings"` into auth handler.go — only middleware.go uses `strings.Split`. The router wraps protected routes with `r.With(auth.RBACMiddleware(resource, action))`; action routes never use RBAC (plain `r.Post`).

### Action switch cases need a block scope
Each `case "name":` body is wrapped in `{ }` — a bare case body followed by `default:` is a syntax error in the generated `actions.go`.

### Every HTML view must be wrapped in `layoutviews.Base(...)`
Page handlers wrap `pageviews.X(pd)` in `layoutviews.Base(title, panelPath, ...).Render(r.Context(), w)`. Resource handlers (list, cards, detail, create form GET, update form GET) MUST do the same — a bare `views.XList(vd).Render(r.Context(), w)` renders a fragment with **no** `<html>`/`<head>`/CSS link/sidebar/topbar, so the page appears completely unstyled. All five render call sites in `handler.go` (list.go, card.go, detail.go, create.go, update.go) use `layoutviews.Base(resourceTitle(r), g.Config.Panel.Path, views.Xxx(vd)).Render(...)`, and each generated file imports `layoutviews "…/internal/views/layout"`. `resourceTitle(r)` returns `r.Label` (falls back to `r.Name`) and is defined in handler.go. Symptom: login + pages look fine, but every resource list/form/detail renders raw/unstyled — that means a render call is missing the `layoutviews.Base(...)` wrapper.

### Sidebar layout: no `fixed` sidebar + `ml-64` on content
The old base layout used `<aside class="w-64 … fixed left-0 top-0">` with `<main>` having no offset — since the sidebar is `position: fixed` it is out of the flex flow and the content (which had no `ml-64`, only the topbar did) slid underneath the nav, overlapping it. Correct layout (in `templ.go` `Base`): the sidebar is a normal flex child `<div class="flex h-screen"><aside class="w-64 … h-screen overflow-y-auto shrink-0">` next to a `<div class="flex-1 flex flex-col">` column holding the sticky topbar and `<main class="flex-1 overflow-y-auto p-6">`. No `ml-64` anywhere. Any new layout change must keep the sidebar in-flow (or add the margin offset to BOTH topbar and main) or the nav overlaps content again.

## SQL queries (`options_sql` / schema FKs)

Fields resolve their option SQL through `optionSQL` in `handler.go`: `options_sql:` (embedded as `SELECT options_value, options_label FROM (sql) AS _opt` at request time), else the schema block's FK metadata (`SELECT {options_value}, {options_label} FROM {foreign_table}` when the field's column matches an FK), else legacy `options_query:` (looked up via `findSQLCQuery`). No query files are read from the output dir — everything comes from the config.

## bcrypt + file uploads

### Password hashing
Create handlers detect `type: password` fields, import `golang.org/x/crypto/bcrypt`, hash via `bcrypt.GenerateFromPassword` before appending to INSERT values. Update handler does **not** re-hash (the update form omits password in default YAML).

### File upload
When any form field has type `file` or `image`:
- Form template adds `enctype="multipart/form-data"`
- Handler switches from `r.ParseForm()` to `r.ParseMultipartForm(32 << 20)`
- `saveUploadedFile` helper (duplicated in create.go + update.go) reads via `r.FormFile`, saves to `static/uploads/{field_name}/{timestamp}{ext}`, and stores the relative URL path in DB
- Posting a create/update form with `curl` must use `-F`, not `-d`, once a file field exists (a plain `-d` POST returns 400 "invalid form")

## Auth & RBAC

- Login: GET renders `LoginPage` templ, POST verifies bcrypt vs `auth.table`, sets `gorilla/sessions` cookie.
- Logout: clears session, redirects to `panel.path`.
- Middleware: `AuthMiddleware` reads `session.Values["user_id"]`, stores `user_id` + `role` in context.
- RBAC middleware generated **conditionally**: `r.With(auth.RBACMiddleware(resource, action)).Get/Post(...)` when resource has `policies:` in YAML. Action routes **never** use RBAC (plain `r.Post`).

## Audit log (D2)

Config block `audit: {enabled, table (default audit_log), include_values, policy, exclude_resources}` (parser validates the block; unknown excluded resources are an error; the editor's `Audit` screen edits it). Implementation lives in `internal/generator/audit.go`; orchestration in `Generate()`:

- **`applyAudit()`** runs after `loadPlugins()` (before the second `ensureDirs` so the resource dirs are created). It appends a **list-only** `AuditLog` resource (`default_sort: -created_at`, `values_json` json column only when `include_values`, `policies.view_any` from `audit.policy`, `PerPage: 20` — the validator default does NOT apply post-parse) plus an "Audit Log" navigation group. Skipped when a resource named `AuditLog` already exists. The appended resource flows through the normal resource/router/views pipeline unchanged.
- **`generateAuditSchema()`** writes driver-aware DDL (`sql/migrations/{table}.sql` — postgres/mssql `BIGSERIAL`+`JSONB`, sqlite `AUTOINCREMENT`+`TEXT`). The audit list/count queries are raw SQL in the generated list handler (no query files, D11). The DDL is **skipped when any `.sql` migration in the out dir already declares the table** (`auditTableInMigrations` / `containsCreateTable`, case-insensitive, `IF NOT EXISTS`-aware).
- **`auditFor(r)`** returns the config when audit is on, the resource is not in `exclude_resources`, and it is not the generated `AuditLog` resource. `auditAnyResource()` gates the **emitted `auth.UserID(r)` helper** (middleware.go) — `fmt` import + `UserID` are only added when ≥1 resource is audited, keeping the auth output byte-identical otherwise.
- **Handler weaving** (create/update/delete/action): the op + audit INSERT run inside ONE transaction — `tx, err := db.BeginTx(r.Context(), nil)` / `defer tx.Rollback()` / `tx.Commit()` (after-hooks still run on `db`, after commit; before-hooks before the tx). `auditTxBeginStr`/`auditTxCommitStr` emit the prologue/epilogue; `auditInsertStr(r, action, rowID, valuesArg, indent)` emits `if _, err := tx.ExecContext(r.Context(), "INSERT INTO {table} (user_id, user_name, table_name, action, row_id, values_json) VALUES ($1,$2,$3,$4,$5,$6)", auth.UserID(r), auth.UserName(r), {table}, {action}, {rowID}, {valuesArg}); err != nil { ... }`. **The hookless `_, err := db.ExecContext` path is NOT emitted for audited resources** — the `RETURNING <id>`/`OUTPUT INSERTED.<id>` capture path is used on create even without hooks (`var newID int64` + `if err := tx.QueryRowContext(...).Scan(&newID)`; row_id = `fmt.Sprintf("%d", newID)`), update/delete use `_, err = tx.ExecContext` (reusing the outer `err`), actions use `_, err = tx.ExecContext` inside the case `{ }` block (where `err` is freshly declared by BeginTx's `:=`). Delete/action pass `""` for values; create/update pass `string(valuesJSON)` from `auditValuesStr(colNames, indent)` (`var valuesJSON []byte` + `json.Marshal(map[string]interface{}{...})` referencing `vals[i]` — `encoding/json` import added when `include_values`). Action cases only audit when the action actually executes SQL (`exec != ""`); proc-only-on-sqlite actions skip audit.
- **Imports to add conditionally**: create.go `encoding/json` (include_values only); delete.go/actions.go `auth` (audited only); update.go `encoding/json`. create.go/update.go already import `fmt`/`auth`; delete/actions already import `strconv`.
- **Gotchas**: `values_json` for password fields holds the bcrypt output (documented, not plaintext). The tx `err` interplay differs per handler (see above) — `if _, err := tx.ExecContext(...)` is legal anywhere (if-init shadows). For curl e2e, the login session-rotation emits TWO `Set-Cookie` for the same name and curl's naive cookie jar ends up empty (POSTs 403); use an RFC 6265 jar (Python `http.cookiejar`). Demo enables audit (`include_values: true, policy: "admin"`) with `audit_log` in `demoSchema()`.

## CSV export subset + import (D3)

- **`list.export`** (`[]string`, optional) — when set, `generateCSVHandler` (export.go)
  SELECTs only those columns (still through `listSelectFrom` for FK joins) and writes a
  header row of `Label` headers (`csvSafe(label)` fallback to the column name). Empty →
  historical behavior (all list columns, raw `rows.Columns()` headers).
- **`import_csv: true`** — `generateImportHandler` (import.go) emits
  `ImportCSV(db) http.HandlerFunc`; the router registers
  `{rbacPrefix("create")}Post("/{res}/import/csv", …)`. Pipeline: `r.ParseMultipartForm`
  → `r.FormFile("file")` → `csv.NewReader` → header cells trimmed into a
  `map[string]int` → one `tx, err := db.BeginTx` around every row's
  `buildCreateParams(m)` + `tx.ExecContext(INSERT)` (row errors are counted as skipped,
  not fatal) → `tx.Commit` → redirect to the list with
  `"<list>?flash="+url.QueryEscape("Imported N, Skipped M: row R: error; …")`.
- **`buildCreateParams(m map[string]string) ([]interface{}, error)`** is emitted in
  create.go and shared by the Create POST and ImportCSV: it maps create-form field names
  onto the INSERT column order, bcrypt-hashes password fields (propagating the error) and
  coerces booleans. **File/image fields cannot go through it** (uploads are request-bound):
  when the resource has one, the Create POST keeps the legacy inline
  `vals := []interface{}{saveUploadedFile(r, …), …}` path and `buildCreateParams` becomes
  a stub returning `file/image uploads are not supported in CSV import` (emitted only when
  `import_csv` is also set). When the POST uses it, the hookless exec line becomes
  `_, err = db.ExecContext` (err already declared — `:=` would fail to compile).
- **Flash**: the import redirect carries `?flash=…`; the emitted router runs a
  `flashHandler` middleware that stashes it in the request context via
  `viewmodels.SetFlash`; `Base` renders it as a green topbar bar via
  `viewmodels.FlashMessage(ctx)` (`ctx` is accessible inside templ bodies). The flash
  survives the 302 redirect (urllib/curl -L follow it); GET navigations without
  `?flash=` render no bar.
- **List templ**: when `import_csv` is set the header gains an "Import CSV" button and a
  `#import-modal` (outside the bulk `<form>`, `enctype="multipart/form-data"`) is
  appended after pagination in BOTH the normal and hasBulk templ variants.
- **Gotchas**: imports are NOT audited (audit weaving only covers the
  create/update/delete/action handlers). The CSV header must name the create fields
  exactly (trimmed); missing columns become empty strings. `$N` placeholders work on all
  drivers. Sprintf args: the `?flash=` redirects build the path as
  `%q+"?flash="+url.QueryEscape(…)` — pass the bare list path as `%q`, never with
  `?flash=` appended, or you get a double `?flash=`.
- Editor: "Import CSV" yes/no on the resource page; "Export" string-list on the list
  page (`Resources/<res>/List/Export`, registered in `nav.go`).

## Plugin fn hooks (D5)

Completes M5 (`SPECv05plus.md` §6.7). `pkg/plugin.Panel.AddHookSource(name, content)`
contributes a `package hooks` Go source file (name validated: bare `<file>.go`, not the
reserved `hooks.go`) into `Manifest.HookSources`. The loader (`mergeManifest` in
plugin.go) writes each source into `OutDir/internal/hooks/` (never overwriting) and tracks
every package-level function name via `hookFuncNames` (regex-free line scan for `func <Ident>(`,
skipping methods and generics) into `g.pluginFnNames`. `attachHook` merges fn `HookAttachment`s
only when the fn name is backed by a source — an unbacked plugin fn hook is **fatal**
("no matching hook source"). `collectFnHooks`/`generateHooks` skip stubs for plugin-backed
names; `generateHooks` also runs when `len(g.pluginHookFiles) > 0` even with zero YAML hook
blocks (emitting Scope only, no imports/stubs) so the plugin files compile. A plugin fn hook
that uses `Scope.Values` (e.g. `s.Values`) or the plugin's own table emits SQL that is driver-
agnostic via `$1`/`s.ID`. The editor's `Plugins` screen declares the `plugins:` block
(name + source). Gotchas:
- The loader registers a plugin's hook sources BEFORE its attachments are merged (same
  mergeManifest), so a plugin can back its own fn hooks; cross-plugin backing requires the
  provider to be declared earlier in `plugins:`.
- `parseGlobalFlags` returns `(configPath, outDir, db, adminPassword, force, verbose,
  skipPlugins, demo)` — callers must destructure in that exact order. `cmdGenerate` and
  `cmdValidate` previously swapped positions 4/5 so `--force` looked verbose and `--verbose`
  silently set `SkipPlugins` (fixed in D5).

## SQLite stored procedures (D6)

`internal/generator/procs.go` implements sqlite `proc:` as **named SQL-batch bodies**.
Config block (top-level, sqlite-only semantics):
```yaml
procedures:
  - name: sp_archive_customer
    description: "Archive a customer and record the event"
    sql: |
      UPDATE customers SET status = 'inactive' WHERE id = $1;
      INSERT INTO customer_log (customer_id, msg) VALUES ($1, 'archived');
```
`generateProcedures()` (wired after `generateAuditSchema`) emits `sql/migrations/procedures.sql`
(`CREATE TABLE IF NOT EXISTS sql_procedures(name PK, body, description, updated_at)` + one
`INSERT OR IGNORE` seed per procedure, `''`-escaped bodies, multi-line bodies preserved) and
the shared `internal/panel/procs/procs.go` package. Only emitted when `usesProcedures()`
(sqlite AND `len(Procedures) > 0`); postgres/mssql ignore the block.
- **`procs.Exec(db, name, id) error`** (runtime, generated app): reads the body from
  `sql_procedures` at call time, splits it with a tokenizer (`splitStatements` — top-level
  `;` only; handles `'…'` strings incl. `''` escapes, `"…"`/`[…]` identifiers, `--` and
  `/* */` comments), runs each statement inside one transaction, drains result rows, rolls
  back on error. **Placeholder binding:** `containsPlaceholder(stmt)` decides whether to
  bind the id — mattn errors when args exceed placeholders, so statements without `$N` get
  no args. The generator package carries byte-identical `splitStatements`/`containsPlaceholder`
  copies (unit-tested in `procs_test.go`); the emitted copy is validated at runtime by e2e.
- **Driver-aware `proc` emission flips on sqlite:** `hookBlockEmits` is true for a proc hook
  when `procedureByName(hook.Proc) != nil`; `hookCallsStr` emits `procs.Exec(db, "<name>",
  scope.ID)`; `actionProcExec(a, idExpr)` (actions.go `int64(id)`, bulk.go `id`) emits the
  `procs.Exec` call and is treated like a real exec (audit is skipped for sqlite proc actions —
  they can't nest inside the audit tx). Create gains `RETURNING <id>` capture for proc-only
  after-hooks. Undeclared sqlite proc refs still emit nothing (`procSQL` unchanged for
  pg/mssql; `TestGenerateProcSQLiteIgnored` guards the byte-identical regression).
- **Validator** (`validateProcedures` in parser): names required + unique; when the driver
  is sqlite, every `proc:` reference on an action/hook must match a `procedures:` entry —
  fatal config error (editor: the `Procedures` screen declares the bodies; a
  `undeclared procedure` validation error jumps there via `structuralGoTo`), mirroring
  plugin-load-failure semantics.
- **Seeds are `INSERT OR IGNORE`** — an existing `sql_procedures` row (already-applied
  migration with a newer body) is never overwritten by a re-run.
- **Bulk** (`generateBulkHandler`): a bulk proc action loops `procs.Exec(db, name, id)` per
  selected id; `hasExec` (the tx wrapper) is gated on raw-SQL exec actions only, so a
  proc-only bulk resource gets no outer `BeginTx` (procs.Exec is itself transactional).

## Security hardening (Phase A of SPEC_future_enhancement.md)

The generated app ships security defaults. Keep them intact when editing the emitters:

- **Session secret**: generated `session.go` uses an `init()` that reads
  `SESSION_SECRET` (must be ≥ 32 chars, else `log.Fatal`), requires it when
  `APP_ENV=production`, otherwise falls back to an ephemeral `crypto/rand`
  secret with a warning (sessions invalidated on restart). Never re-emit the old
  hardcoded `yaga-secret-key-change-in-production` default.
- **`order` whitelist**: list + card handlers clamp `order` to `asc`/`desc`
  (inserted AFTER the `default_sort` block so a `-`-prefixed default survives).
  The `sort` column is whitelisted separately against `validSorts`.
- **Upload validation**: the emitted `saveUploadedFile` (create.go/update.go,
  only when a `file`/`image` field exists) lowercases the extension, allows only
  the `safeUploadExts` allow-list (images/pdf/txt/csv/zip/office), rejects
  `text/html`/`image/svg+xml` via `http.DetectContentType` magic bytes (with
  `file.Seek(0, io.SeekStart)` before copying), and `static/uploads` requires
  `strings` already in the file's imports (create.go/update.go import it
  unconditionally). `/uploads/*` is served by a wrapper handler that forces
  `Content-Disposition: attachment` — don't revert to a bare `http.FileServer`.
- **Safe errors**: `internal/panel/httperr` (generated by `httperr.go`, wired in
  `generator.go` after `generateAuth`) provides `Internal(w, err)` / `NotFound(w, err)`
  that log server-side and return generic status text. Every resource handler and
  page handler imports `httperr` and MUST NOT emit `http.Error(w, err.Error(), ...)`.
- **Admin password**: `init --db` accepts `--admin-password`
  (threaded through `parseGlobalFlags`); when omitted, `randomPassword()`
  (introspect.go) generates a 14-char one-time password that is printed, not
  stored. `insertAdminUser` now returns `(bool, error)` (inserted or not).
- **Security headers**: `securityHeaders` middleware (router.go) is registered on
  every generated router and sets CSP (inline scripts/styles allowed for theme
  toggle + picker), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`,
  `Referrer-Policy: same-origin`, `Permissions-Policy`. Don't remove it from
  `r.Use(...)`.

### Phase B (CSRF, session, rate limiting, CSV, action RBAC)

- **CSRF middleware is registered FIRST inside the panel `r.Route` block**
  (`r.Use(auth.CSRFMiddleware)`), BEFORE the login routes. It skips
  GET/HEAD/OPTIONS and `/static/`/`/uploads/` prefixes; every other POST (incl.
  login, logout, create/update/delete/action/bulk) requires a matching token via
  hidden `_csrf` input or `X-CSRF-Token` header. The token lives in the session
  (`session.Values["csrf_token"]`, minted lazily by `CSRFToken(r, w)`); the
  generated `views` pass it via `data.CSRFToken` / `auth.CSRFToken(r, w)`. Don't
  drop the `auth.CSRFToken(r, w)` 5th arg from any `layoutviews.Base(...)` call
  — the signature is
  `Base(title, panelPath, theme, userName, csrfToken, children)`.
- **Login rate limiting is conditional**: `ratelimit.go` is emitted ONLY when
  `auth.login.rate_limit` is set with `max_attempts > 0` (`rateLimited` flag in
  auth.go). The `loginLimited(r)`/`resetLoginLimit(r)` helpers and the
  re-render-on-limit branch in `handler.go` are all gated on it — a config
  without the block must not reference the helpers.
- **Session rotation**: on successful login the old session is expired
  (`MaxAge = -1` + `Save`) then a FRESH session is minted with
  `Store.New(r, "yaga-session")` — do NOT re-`GetSession` after expiring,
  gorilla reloads the old request cookie. `resetLoginLimit(r)` runs on success.
- **Logout is POST-only** (`r.Post("/logout", ...)`); the topbar renders a
  `<form method="POST">` with hidden `_csrf`. The logout form action is
  `templ.SafeURL(fmt.Sprintf("%s/logout", panelPath))` — the `%%s` in the
  generator source must stay doubled or the `panelPath` arg count breaks.
- **CSV formula injection**: `export.go` headers AND values pass through
  `csvSafe`, which prefixes `'` on leading `=`, `+`, `-`, `@`, tab or CR.
- **Action/bulk RBAC**: `Action.Policy` ("role1|role2") emits
  `ActionRBACMiddleware` in middleware.go and wraps the action + bulk routes
  with `r.With(auth.ActionRBACMiddleware("<res>"))` — gated on
  `hasActionPolicies(res)` (a `policies:` block alone is NOT enough; the action
  itself must set `policy:`).

### Phase C (performance & correctness)

Already-implemented items (verified 2026-08-13): windowed `COUNT(*) OVER() AS _total`
on list/card (single round trip, empty-page fallback), configurable `r.List.PerPage`
(default 20), `connections.*.pool` → `db.Set*` setters, transactional bulk, batched
(deduped) options loader, per-widget error logging, and `idColumn(r)` in update/delete.
The `html` widget renders its query result as **raw HTML** (`templ.Raw`) — document in
configs that it requires trusted input; `stat` values are numeric-only. C.1 (request-id +
timing request logger replacing the dead `SessionMiddleware` and the
`middleware.Logger`/`errorOnlyLogger` split) is deferred.

## Pages & widgets

Supported widget types: `stat`, `stats_grid`, `chart` (line/bar/pie/area via Chart.js), `table`, `list`, `html`. Each queries DB via raw SQL at request time. Chart data serialized to JSON in `data-chart-labels` / `data-chart-values` attributes. **Widget query errors are logged, not fatal**: every widget's `QueryRowContext`/`Scan` error is emitted as `log.Printf("page %s widget %d (%s) <widget>: %v", pageName, i, widgetName, err)` (scan errors use a `"… <widget> scan: %v"` variant) and the widget renders with whatever rows it got — a broken widget never blanks the whole page or 500s.

Chart.js is **vendored at generation time** (D8) — no npm, no CDN, runtime is offline. The yaga binary embeds the pinned Chart.js **4.4.1** UMD bundle (`internal/generator/assets/chart.umd.js`, MIT license banner intact, `//go:embed`'d as `chartUmdJS` in `tailwind.go`) and `generateAssets()` writes it to `static/js/chart.js` (the same name `templ.go` `Base` references via `<script src="/static/js/chart.js">`). 4.4.1 ships only the unminified `chart.umd.js` (4.5+ added a `.min.js` variant); the file is embedded as-is. A bare `go build` in `admin/` serves chart.js with zero network — there is no `package.json` emitted and no npm step anywhere.

## Repo layout

| Path | Purpose |
|---|---|
| `cmd/yaga/main.go` | CLI entry (init/generate/validate/version), hand-rolled flags |
| `cmd/yaga/introspect.go` | `init --db` — DB introspection, auth table creation, YAML with `schema:` block generation |
| `cmd/yaga/edit.go` | `edit` — entry point for interactive YAML config editor |
| `cmd/yaga/editor/` | tview TUI editor: 3-pane shell, section editors, sync + validate + preview screens (18 files, see `edit` above) |
| `internal/types/` | YAML-tagged Go structs for config schema (7 files: config.go, panel.go, resource.go, field.go, hook.go, procedure.go, schema.go) |
| `internal/parser/` | yaml.v3 unmarshal + validation (schema.go, validator.go) |
| `internal/generator/` | Code generation pipeline (see above; `assets/` holds the embedded Chart.js 4.4.1 bundle + pre-built `styles.css`) |
| `examples/` | Empty placeholder dirs (`full`, `minimal`), unused |
| `SPEC.md` | Authoritative YAML schema and spec — check before adding features |
| `testdata/` | `kitchen.yaml` — the kitchen-sink fixture that regenerates the pre-built stylesheet (`make styles`) and drives `TestGenerateStylesEmbedded`; no longer empty |
| `pkg/auth/` | Empty placeholder (.gitkeep only), unused |

## Generated app dependencies

`github.com/a-h/templ`, `github.com/go-chi/chi/v5`, `github.com/gorilla/sessions`, `golang.org/x/crypto`. Plus `github.com/jackc/pgx/v5` (postgres, blank-imported in main.go), `github.com/mattn/go-sqlite3 v1.14.24` (sqlite, blank-imported in main.go), and `github.com/microsoft/go-mssqldb v1.10.0` (mssql, blank-imported in main.go) — the `pgx` stdlib driver registers the `"pgx"` database/sql name, so generated main.go calls `sql.Open("pgx", dsn)` for postgres.

The generated `go.mod` also declares `tool github.com/a-h/templ/cmd/templ` so `go tool templ generate` works without a manual templ install, and `generateMakefile()` emits a `Makefile` whose `build` target runs all steps (Tailwind via the standalone binary, tidy, templ, `go build -o <binary> .`) with no npm dependency.
