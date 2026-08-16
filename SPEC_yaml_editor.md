# Spec & Implementation Plan: `yaga edit` — tview-based TUI Editor

## 1. Goal

Replace the `charmbracelet/huh` + Bubble Tea editor with a `rivo/tview` TUI that provides:

1. **Comfortable persistent UI** — left navigation menu + right content pane + status bar (no full-screen form swaps).
2. **Full YAML spec coverage** — every key in the `internal/types` schema (see §5).
3. **TUI dashboard preview** — sidebar + pages/widgets, plus per-resource list/form/detail preview.
4. **Schema-block reference check (Validate)** — cross-checks every YAML table/column reference against the captured `schema:` block, with jump-to-fix.

Decisions:
- **Validate**: the captured `schema:` block (D11) is the sole schema source — no DB connection, no sqlc, no `sql/` query-file reads.
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
┌ Title bar: yaga editor — yaga.yaml ───────── [modified] ─┐
├─────────────┬──────────────────────────────────────────────────┤
│  Panel      │  <right pane = tview.Pages stack>               │
│  Connections│   section forms / lists / validate / preview     │
│  Auth       │                                                  │
│  Navigation │                                                  │
│  Resources  │                                                  │
│  Pages      │                                                  │
│ ─────────── │                                                  │
│  Validate   │                                                  │
│  Preview    │                                                  │
│  Save       │                                                  │
│  Quit       │                                                  │
├─────────────┴──────────────────────────────────────────────────┤
│  ↑↓ Enter    Ctrl+S save  Ctrl+V validate  Ctrl+P go-to  Ctrl+O home  Esc back  F10 quit │
└──────────────────────────────────────────────────────────────────────────────────────────────┘
```

- `Editor` struct owns `*tview.Application`, `*tview.Pages` (right pane), left `*tview.List`, status `*tview.TextView`, `cfg`, `configPath`, `modified`, and a `history []string` of page names for Esc-back.
- **Live editing**: form fields bind to structs via `SetChangedFunc` closures; any change sets `modified` (status bar shows it).
- **Save** (Ctrl+S / Save menu item): `yaml.Marshal(cfg)` → write → toast "Saved". Runs `parser.Validate` first; errors shown in a modal, file not written.
- **Quit** (F10 / Quit item / Esc at root): if `modified`, modal confirm *Save & quit / Discard / Cancel*.
- **Validate** (Ctrl+V / Validate nav item): full health check listing every structural + schema finding; Enter on a row jumps to the exact editor page and highlights the offending column/field.
- **Go to** (Ctrl+P / Ctrl+> alias / Ctrl+P): cd-style navigation dialog. Every screen has a canonical path (`Panel/Brand`, `Resources/<res>/List/Columns/<col>/Options`, `Pages/<page>/Widgets/<widget>`, `Navigation/<group>/Items/<item>`, `Preview/Page/<p>`, …). Enter navigates, Tab completes to the longest common prefix, Esc clears-then-closes, unknown paths keep the dialog open. Absolute (`~/…`, `/…`) and relative (`../…`, `<child>`) paths both resolve against the current screen; matching is case/space-insensitive and unnamed/duplicate items use `#<idx>`.
- **Home** (Ctrl+O / Ctrl+/ alias): jump back to the overview screen (closes the dialog if open).
- tview Forms have no page-groups: sections separated with `AddTextView`. Type-dependent widget/field forms rebuild via `form.Clear(true)` + re-add on type change.

**Files** (`cmd/yaga/editor/`, old bubbletea/huh files deleted):

```
editor.go   (Editor struct, shell, save/quit, home page, key capture)
style.go    (colors, boxed(), statusColor)
widgets.go  (str/num/yesno/password/long/pick, showPage/back, toast, errorModal)
options.go  (enumerated option sets for drop-downs)
lists.go    (recordList + listSpec, confirm modal, tagsPage, stringListPage, promptInput)
menu.go     (Panel/Brand/Layout/Theme, Connections, Auth, Navigation, Pages+widgets)
resource.go (Resources, List/Card/Detail/Form, Policies)
columns.go  (list columns editor)
fields.go   (form/card/detail field editor, validation, visible)
actions.go  (custom actions editor)
hooks.go    (before/after hooks: name + fn|sql)
maps.go     (map[string]string editor)
validate.go (validation screen: structural + schema findings, jump-to-fix)
preview.go  (dashboard + per-resource ASCII-frame previews)
nav.go      (cd navigation: canonical paths, resolvePath/completePath, go-to dialog)
+ editor_test.go, run_test.go (tview sim-screen integration tests), nav_test.go
```

No separate `model.go`/`panel.go`/`auth.go`/`pages.go` — those sections live in `menu.go`/`resource.go`. `Editor` embeds the app; the only test seam is `SetScreen(tcell.Screen)` (simulation screen for integration tests).

## 4. New package: `internal/schema/` (file-level schema analysis)

Small, testable, UI-independent. Used by the Validate screen:

- `schema.go` — `Table{Name, Columns[]Column{Name,Type,Nullable,Default,IsPrimaryKey,FKs}}`; `ParseSchema(files...)` parses `CREATE TABLE` from `sql/migrations/*.sql` (regex-based, sqlite + postgres dialect). Takes explicit file paths (glob with `filepath.Glob` first). Remains only as the schema-source fallback; the captured `schema:` block is the primary source.
- `references.go` — `CollectReferences(cfg)` walks every YAML query reference (list/detail/form/delete/action/options_query/page widgets incl. nested) returning `{name, origin}`; plus table/column references per resource and per-section column locations (`ColumnRef`) for jump-to-fix.
- `generate.go` — ports the `writeResourceYAML` YAML emitter from `introspect.go` (string-builders only; no DB).

## 5. Full YAML coverage

Everything the old editor covered stays editable; new coverage:

| Area | Newly added |
|---|---|
| Top | `version` |
| Connections | multi-connection list + add/delete/rename (old editor handled first conn only) |
| Navigation | group **name/icon/sort** (old `buildGroupForm` was unwired) |
| Resource basic | `table`, `id_type` (dropdown), `id_column` |
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

## 7. Validate screen (the single health check)

The old SQL-file **Sync** screen was removed (D11 — sql/migrations and sql/queries are obsolete as the source of truth; the captured `schema:` block is). The Validate screen runs the structural validator (`parser.ValidateAll` against a YAML copy so defaults are not injected) **plus** a schema-block reference pass: each resource's table must exist in `cfg.Schema.Tables` (error → jump to the resource), and every referenced column must be a column of that table (warning → jump to the exact `columnsPage`/`cardFieldsPage`/`detailFieldsPage`/`formFieldsPage` row). No schema block → a single "no schema block captured" warning. Renders one `tview.List` row per problem (red errors / yellow warnings, "No problems found" empty state) with Refresh (Ctrl+R) + Back (Ctrl+B) buttons.

**Buttons** (each with a Ctrl+letter shortcut from its first free label letter; Ctrl+B is reserved so every Back button is pinned to Ctrl+B and other buttons skip it):
1. Refresh (Ctrl+R) — re-run the analysis.
2. Back (Ctrl+B) — return to the previous screen.

## 8. Implementation order

1. `go get github.com/rivo/tview`; confirm build.
2. `internal/schema/` package + unit tests.
3. Editor shell: `editor.go`, `model.go`, `style.go`, `menu.go`; rewire `edit.go`; save/quit + modified tracking; delete old huh/bubbletea files.
4. `options.go` (tview format) + `widgets.go` helpers + `maps.go` + `tagEditor`.
5. Simple sections: panel, auth, connections.
6. List sections: navigation, pages+widgets, resources + all sub-editors (columns, fields, actions, hooks, policies, forms).
7. Validate screen.
8. Preview screen.
9. `go mod tidy` (drop charmbracelet).
10. Update AGENTS.md + SPEC.md CLI section.
11. Verify: `go build ./...`, `go vet ./cmd/yaga/...`, `go test ./...`, manual E2E on `init --demo`.

## 9. Risks

- tview v0.42 specifics: `Modal` has no `SetTitle` (title goes in `SetText` first line); `Form` has no `SetScrollable`; `DropDown.SetCurrentOption` fires the changed callback at construction (guard with a `first` flag or the page build marks `modified`).
- **`Pages.SwitchToPage`/`AddPage` never move focus** (`AddPage`'s 4th arg is `visible`, not `focus`) — every page switch must be followed by an explicit `app.SetFocus(e.pages)` (or `e.nav` on home) or focus stays on the nav list.
- **Never call `app.Draw()` synchronously inside the event loop** — `Application.Draw`/`QueueUpdate` block on the update queue and deadlock the loop when called from a capture/handler. Set text directly; redraws happen after each event. (This was a real deadlock found by the sim-screen tests.)
- tview Form dynamic re-render → sub-pages are rebuilt via `refreshPage`/`showPage` rather than `Clear(true)` in place.
- Esc semantics → centralized app-level `SetInputCapture` with history stack.
- Int/bool fields mutate live → validate + revert-if-parse-fails.
