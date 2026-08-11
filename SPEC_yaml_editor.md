# Spec & Implementation Plan: `go-fila edit` — tview-based TUI Editor

## 1. Goal

Replace the `charmbracelet/huh` + Bubble Tea editor with a `rivo/tview` TUI that provides:

1. **Comfortable persistent UI** — left navigation menu + right content pane + status bar (no full-screen form swaps).
2. **Full YAML spec coverage** — every key in the `internal/types` schema (see §5).
3. **TUI dashboard preview** — sidebar + pages/widgets, plus per-resource list/form/detail preview.
4. **SQL ↔ YAML sync tool** — file-level consistency checks between `go-fila.yaml`, `sql/queries/*.sql`, and `sql/migrations/schema.sql`, with apply actions.

Decisions:
- **Sync tool**: file-level only — no DB connection, no introspection refactor.
- **Preview**: dashboard pages + per-resource list/form/detail.
- **Editor switch**: full replacement of the huh editor; charmbracelet deps removed.
- **Docs**: this file (`SPEC_yaml_editor.md`) is the single source for the editor.

## 2. Dependencies

- Add `github.com/rivo/tview` (v0.42.x; pulls `gdamore/tcell/v2` + `rivo/uniseg` — uniseg already indirect).
- Remove: `charmbracelet/huh`, `bubbletea`, `lipgloss`, `bubbles`, `catppuccin`, `xo/terminfo`, etc. via `go mod tidy`.
- Keep `gopkg.in/yaml.v3` (save path), `internal/parser`, `internal/types`.

## 3. Architecture

**Persistent 3-pane layout** (lazygit-style):

```
┌ Title bar: go-fila editor — go-fila.yaml ───────── [modified] ─┐
├─────────────┬──────────────────────────────────────────────────┤
│  Panel      │  <right pane = tview.Pages stack>               │
│  Connections│   section forms / lists / sync table / preview   │
│  SQLC       │                                                  │
│  Auth       │                                                  │
│  Navigation │                                                  │
│  Resources  │                                                  │
│  Pages      │                                                  │
│ ─────────── │                                                  │
│  Validate   │                                                  │
│  Sync       │                                                  │
│  Preview    │                                                  │
│  Save       │                                                  │
│  Quit       │                                                  │
├─────────────┴──────────────────────────────────────────────────┤
│  ↑↓ Enter    Ctrl+S save   Ctrl+V validate   Esc back   F10 quit   toast area │
└────────────────────────────────────────────────────────────────┘
```

- `Editor` struct owns `*tview.Application`, `*tview.Pages` (right pane), left `*tview.List`, status `*tview.TextView`, `cfg`, `configPath`, `modified`, and a `history []string` of page names for Esc-back.
- **Live editing**: form fields bind to structs via `SetChangedFunc` closures; any change sets `modified` (status bar shows it).
- **Save** (Ctrl+S / Save menu item): `yaml.Marshal(cfg)` → write → toast "Saved". Runs `parser.Validate` first; errors shown in a modal, file not written.
- **Quit** (F10 / Quit item / Esc at root): if `modified`, modal confirm *Save & quit / Discard / Cancel*.
- **Validate** (Ctrl+V / Validate nav item): full health check listing every structural + schema finding; Enter on a row jumps to the exact editor page and highlights the offending column/field.
- tview Forms have no page-groups: sections separated with `AddTextView`. Type-dependent widget/field forms rebuild via `form.Clear(true)` + re-add on type change.

**Files** (`cmd/go-fila/editor/`, old bubbletea/huh files deleted):

```
editor.go   (Editor struct, shell, save/quit, home page, key capture)
style.go    (colors, boxed(), statusColor)
widgets.go  (str/num/yesno/password/long/pick, showPage/back, toast, errorModal)
options.go  (enumerated option sets for drop-downs)
lists.go    (recordList + listSpec, confirm modal, tagsPage, stringListPage, promptInput)
menu.go     (Panel/Brand/Layout/Theme, Connections, SQLC, Auth, Navigation, Pages+widgets)
resource.go (Resources, List/Card/Detail/Form, Policies)
columns.go  (list columns editor)
fields.go   (form/card/detail field editor, validation, visible)
actions.go  (custom actions editor)
hooks.go    (before/after hooks: name + fn|sql)
maps.go     (map[string]string editor)
sqledit.go  (per-resource SQLC query SQL editor: staged, flushed on save)
sync.go     (SQL<->YAML analysis: simple list of schema tables, queries, missing refs)
validate.go (validation screen: structural + schema findings, jump-to-fix)
preview.go  (dashboard + per-resource ASCII-frame previews)
+ editor_test.go, run_test.go (tview sim-screen integration tests)
```

No separate `model.go`/`panel.go`/`auth.go`/`pages.go` — those sections live in `menu.go`/`resource.go`. `Editor` embeds the app; the only test seam is `SetScreen(tcell.Screen)` (simulation screen for integration tests).

## 4. New package: `internal/schema/` (file-level sync engine)

Small, testable, UI-independent. Used by the Sync screen:

- `schema.go` — `Table{Name, Columns[]Column{Name,Type,Nullable,Default,IsPrimaryKey,FKs}}`; `ParseSchema(files...)` parses `CREATE TABLE` from `sql/migrations/*.sql` (regex-based, sqlite + postgres dialect). Takes explicit file paths (glob with `filepath.Glob` first).
- `queries.go` — `Query{Name,Variant,Body,RawBody,File,SelectCols[]string}`; `ParseQueries(dir)` scans `-- name: X :variant` and best-effort extracts the SELECT projection; `ParseQueriesForFile(text, file)` parses one file's text (used by the editor to overlay staged edits); `RewriteQueryBody(text, name, newBody)` replaces one query block, leaving every other block byte-identical.
- `references.go` — `CollectReferences(cfg)` walks every YAML query reference (list/detail/form/delete/action/options_query/page widgets incl. nested) returning `{name, origin}`; plus table/column references per resource.
- `generate.go` — ports the `generateQueries`/`writeResourceYAML` SQL/YAML emitters from `introspect.go` (string-builders only; no DB) so the sync tool can generate stubs.

## 5. Full YAML coverage

Everything the old editor covered stays editable; new coverage:

| Area | Newly added |
|---|---|
| Top | `version` |
| Connections | multi-connection list + add/delete/rename (old editor handled first conn only) |
| Navigation | group **name/icon/sort** (old `buildGroupForm` was unwired) |
| Resource basic | `table`, `id_type` (dropdown), `id_column` |
| Resource SQL | per-resource **SQL queries** list (list/count/detail/form/populate/options_query, deduped) + full-text SQL editor; edits stage into sql/queries files and flush with the global save |
| Resource list | `query`, `count_query`, `default_sort` (old editor left these uneditable), column `options` map |
| Resource detail | `params` map, fields editor |
| Resource card | fields editor (old editor left it unwired), `searchable` tag editor |
| Resource form | **delete** action, `populate_params` map, create/update/delete `hooks`, field `options` map, field `validation` min/max, `options_value`/`options_label`, `visible` |
| Actions | `hooks` |
| Pages/widgets | `prefix`, `limit`, `data_columns`, nested `stats_grid` widgets (recursive), chart config |
| New | `Hooks` editor (before/after, name/fn/sql) |
| New | map editor wired (options, params, populate_params) |

Reusable widgets in `widgets.go`: textField, passwordField, intField (numeric accept + parse-on-change), boolField, selectField (Dropdown), textArea, sectionHeader, and a `tagEditor` (tview.List of toggles, space to toggle — replaces `huh.MultiSelect`), plus `maps.go` map editor (list + two-field add/edit form).

## 6. TUI Preview

**Dashboard preview** — emulates the generated app:
- Topbar strip: panel name, current page title, "PREVIEW", dark/light badge.
- Sidebar: `navigation` groups rendered as `tview.List` (group headers + items). Enter on a page item → switches content; on a resource item → that resource's list preview.
- Content grid of widgets:
  - `stat` — bordered box: icon+label, big `—` value, color hint.
  - `stats_grid` — nested Grid (columns = `widget.columns`) of stat boxes.
  - `chart` — box with type + x/y labels + ASCII sparkline/bars.
  - `table` — `tview.Table` with `data_columns` headers + placeholder rows.
  - `list` — bullet list box. `html` — placeholder box.

**Per-resource preview** (from resource menu → Preview): list view as a `tview.Table` (columns as headers, type-appropriate placeholder rows), form view as a read-only `tview.Form` built from `form.create.fields` (field-type→control mapping: InputField/PasswordField/DropDown/Checkbox/TextArea), detail view as key/value table.

Implementation note: the preview is a single `tview.TextView` rendering an ASCII frame (topbar strip + two-column sidebar/content split built from `cfg.Navigation` / resource names). The grid chrome (`│ ├ ┬ ┤ ┌ ┐ └ ┘ ─`) is light blue while the cell text stays white; every row is padded (`padVisual`, tag-aware via `tview.TaggedStringWidth`) to the identical total width (`previewWidth`, chrome rows share the same sidebar/content layout), with full color resets in content rewritten to attribute-only resets. The generated dashboard is **not** run; widget types are drawn as labelled boxes. This keeps preview free of a DB or the generated app.

## 7. Sync tool

Reads the project's `sql/` via `internal/schema`. Renders a **simple scrolling `tview.TextView`** (`sync.go`) listing:

1. **Schema** — every table from `sql/migrations/*.sql` with its column count.
2. **Queries** — every SQLC query definition found in `sql/queries`, sorted.
3. **YAML references** — a colored summary of missing queries (with their YAML origin), missing tables, missing columns and missing FK-target `List{Table}` queries, each with detail lines.

**SQL path resolution** (`sqlBase()` in `sqledit.go`): `sqlc.queries_dir` / `sqlc.schema_dir` are relative to the generated output dir where sqlc runs — `init`/`init --demo` write `sql/{migrations,queries}` into `./admin/sql`. The editor resolves them against the **config dir** when it has any sql tree (the generator copies `configDir/sql` into the output dir), otherwise against `{configDir}/admin`, falling back to the config dir. So both the classic root-level `sql/` layout and the default `admin/sql` layout resolve the same files sqlc will consume.

The per-resource editor pages already show the live SQL bodies of a single resource's queries (List/Detail/Form/Card/Field `options_query`, with a `Reload SQL query` button); Sync complements that as the whole-project health check.

**Buttons** (each with a Ctrl+letter shortcut from its first free label letter):
1. Generate missing queries (Ctrl+G) — writes SQLC query stubs into `sql/queries/{table}.sql` (one file per schema table; existing files are never overwritten).
2. Refresh (Ctrl+R) — re-run the analysis.
3. Back (Ctrl+B) — return to the previous screen.

`importResourcesFromSchema` (add missing resources from parsed schema tables) is kept as a method for tests but not exposed in the UI.

## 8. Implementation order

1. `go get github.com/rivo/tview`; confirm build.
2. `internal/schema/` package + unit tests.
3. Editor shell: `editor.go`, `model.go`, `style.go`, `menu.go`; rewire `edit.go`; save/quit + modified tracking; delete old huh/bubbletea files.
4. `options.go` (tview format) + `widgets.go` helpers + `maps.go` + `tagEditor`.
5. Simple sections: panel, sqlc, auth, connections.
6. List sections: navigation, pages+widgets, resources + all sub-editors (columns, fields, actions, hooks, policies, forms).
7. Sync screen.
8. Preview screen.
9. `go mod tidy` (drop charmbracelet).
10. Update AGENTS.md + SPEC.md CLI section.
11. Verify: `go build ./...`, `go vet ./cmd/go-fila/...`, `go test ./...`, manual E2E on `init --demo`.

## 9. Risks

- tview v0.42 specifics: `Modal` has no `SetTitle` (title goes in `SetText` first line); `Form` has no `SetScrollable`; `DropDown.SetCurrentOption` fires the changed callback at construction (guard with a `first` flag or the page build marks `modified`).
- **`Pages.SwitchToPage`/`AddPage` never move focus** (`AddPage`'s 4th arg is `visible`, not `focus`) — every page switch must be followed by an explicit `app.SetFocus(e.pages)` (or `e.nav` on home) or focus stays on the nav list.
- **Never call `app.Draw()` synchronously inside the event loop** — `Application.Draw`/`QueueUpdate` block on the update queue and deadlock the loop when called from a capture/handler. Set text directly; redraws happen after each event. (This was a real deadlock found by the sim-screen tests.)
- tview Form dynamic re-render → sub-pages are rebuilt via `refreshPage`/`showPage` rather than `Clear(true)` in place.
- Esc semantics → centralized app-level `SetInputCapture` with history stack.
- Int/bool fields mutate live → validate + revert-if-parse-fails.
- File-level sync is heuristic (regex SQL projection) → results marked as hints, never silently destructive.
