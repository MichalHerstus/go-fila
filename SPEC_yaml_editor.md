# Plan: `go-fila edit --config {yaml}` — Interactive TUI YAML Editor

## Overview

Add an `edit` subcommand that opens the YAML config in a terminal UI. Uses `charmbracelet/huh` (built on Bubble Tea) for form editing and a custom Bubble Tea model for list management. Supports all YAML blocks: Panel, Connections, SQLC, Auth, Navigation, Resources (all sub-sections), and Pages (with widgets).

## Dependencies to Add

```
github.com/charmbracelet/huh           # form toolkit (Input, Select, Confirm, MultiSelect)
github.com/charmbracelet/lipgloss      # styling (indirect dep of huh, import for custom models)
```

`huh` pulls in `bubbletea` and `lipgloss` as transitive deps.

## Architecture: Stack-Based Navigation

A screen stack where each screen is a `tea.Model`. Push on enter, pop on Esc.

```go
EditorModel {
    cfg         *types.Config
    configPath  string
    stack       []tea.Model       // screen stack
    modified    bool
    quit        bool
}
```

### Screens

1. **MainMenu** — List of YAML blocks with item counts. Uses `huh.NewSelect` or a custom list model.
2. **BlockForm** — A `huh.Form` for simple blocks (Panel, Connections, SQLC, Auth). Built dynamically from current config values.
3. **ListManager** — Custom Bubble Tea model for arrays (resources, navigation groups, pages, columns, fields, actions, widgets). Shows list + add/edit/delete buttons.
4. **ItemForm** — `huh.Form` for a single item in a list (a Resource, a Column, a Field, etc.).

### Navigation Flow

```
MainMenu → [Panel/Connections/SQLC/Auth] → BlockForm → pop on Esc
MainMenu → [Resources] → ListManager(resources) → [Edit "User"] → ResourceMenu
ResourceMenu → [List View] → BlockForm(listConfig) → pop
ResourceMenu → [Columns] → ListManager(columns) → [Edit "name"] → ColumnForm → pop
ResourceMenu → [Detail View] → BlockForm(detailConfig) → pop
ResourceMenu → [Form] → FormSectionMenu → [Create] → BlockForm(create) → pop
ResourceMenu → [Actions] → ListManager(actions) → [Edit "archive"] → ActionForm → pop
ResourceMenu → [Policies] → BlockForm(policies) → pop
MainMenu → [Pages] → ListManager(pages) → [Edit "Dashboard"] → PageForm → pop
PageForm → [Widgets] → ListManager(widgets) → [Edit "stat"] → WidgetForm → pop
MainMenu → [Navigation] → ListManager(groups) → [Edit "Management"] → GroupForm → pop
GroupForm → [Items] → ListManager(items) → [Edit item] → NavItemForm → pop
```

## File Structure

```
cmd/go-fila/
├── main.go           # add "edit" case in switch
├── edit.go           # cmdEdit() entry point + marshal + save
└── editor/
    ├── model.go      # EditorModel (stack), New(), Run()
    ├── menu.go       # MainMenu — list of YAML blocks
    ├── list.go       # ListManager — generic array editor (add/edit/delete)
    ├── panel.go      # Panel form (5 groups: basic, brand, layout, theme, sidebar)
    ├── connection.go # Connection form (driver select, DSN, pool)
    ├── sqlc.go       # SQLC form (config, queries_dir, schema_dir, output_pkg)
    ├── auth.go       # Auth form (guard, provider, table, login fields, flags)
    ├── navigation.go # Navigation group + item forms
    ├── resource.go   # Resource menu + list/detail/card/form/actions/policies forms
    ├── page.go       # Page form + widget forms
    ├── field.go      # Column + Field forms, field type select
    ├── options.go    # All enum option slices
    └── helpers.go    # Generic form field builders, int↔string conversion
```

## Enum Option Maps (`options.go`)

```go
var fieldTypeOptions = []huh.Option[string]{
    huh.NewOption("Integer", "integer"), huh.NewOption("String", "string"),
    huh.NewOption("Text", "text"),       huh.NewOption("Email", "email"),
    huh.NewOption("Password", "password"), huh.NewOption("Boolean", "boolean"),
    huh.NewOption("Badge", "badge"),     huh.NewOption("DateTime", "datetime"),
    huh.NewOption("Date", "date"),       huh.NewOption("Image", "image"),
    huh.NewOption("File", "file"),       huh.NewOption("Select", "select"),
    huh.NewOption("Relation", "relation"), huh.NewOption("JSON", "json"),
    huh.NewOption("Float", "float"),     huh.NewOption("GPS", "gps"),
}

var widgetTypeOptions, chartTypeOptions, driverOptions,
    actionColorOptions, iconOptions, visibleOptions,
    authGuardOptions, authProviderOptions
```

## Key Form Builders

### Panel (`panel.go`)

5 groups in one `huh.Form`:
- **Basic**: ID (input), Path (input), Name (input)
- **Brand**: Logo (input), Favicon (input), Primary Color (input), Secondary Color (input)
- **Layout**: Sidebar Collapsible (confirm), Sidebar Width (input→int), Collapsed Width (input→int), Topbar Sticky (confirm), Max Content Width (input)
- **Theme**: Dark Mode (confirm), Font Family (input), Font Mono (input)

### Connection (`connection.go`)

- Driver (select: postgres/sqlite), DSN (input), Pool Max Open (input→int), Pool Max Idle (input→int), Pool Lifetime (input)

### Auth (`auth.go`)

- Guard (select: web/api), Provider (select: session/jwt), Table (input)
- Login Fields (multi-select: email/password/name), Login Redirect (input)
- Registration (confirm), Password Reset (confirm), Remember Me (confirm)

### Resource (`resource.go`)

- **ResourceMenu**: A list showing [List View, Detail View, Card View, Form (Create/Update/Delete), Actions (N items), Policies]
- **List Form**: Query (input), Count Query (input), Default Sort (input)
- **Detail Form**: Query (input)
- **Card Form**: Kanban Field (input), Columns (input→int), Rows (input→int), Default Sort (input), Searchable (multi-select of column names)
- **Form Section**: A menu with Create, Update, Delete sub-items. Create/Update each get a form (Query, Populate Query, and a fields sub-list). Delete gets a single confirm toggle.
- **Actions**: List manager → Action Form: Name, Label, Icon (select), Color (select), Requires Confirmation (confirm), Bulk (confirm), Query (input)
- **Policies**: View Any (input), View (input), Create (input), Update (input), Delete (input)

### Column (`field.go`)

- Name (input), Label (input), Type (select), Sortable (confirm), Searchable (confirm), Options (map editor: key-value pairs)

### Field (`field.go`)

- Name (input), Label (input), Type (select), Required (confirm), Visible (multi-select: create/update), Validation Min (input→int), Validation Max (input→int), Options Query (input), Options Value (input), Options Label (input), Options (map editor)

### Widget (`page.go`)

- Type (select), Label (input), Query (input), Icon (select), Color (select), Prefix (input), Limit (input→int), Columns (input→int), Data Columns (comma-separated input)
- If type=chart: Chart Type (select), Chart Query (input), X (input), Y (input)
- If type=stats_grid: nested Widgets (sub-list)

### Navigation Item (`navigation.go`)

- Resource (input), Page (input), Type (select: resource/page/link), Label (input), URL (input), Opens In New Tab (confirm)

## List Manager (`list.go`)

A custom Bubble Tea model for managing arrays:

```
  Resources
  ┌───────────────────┐
  │ > User            │
  │   Product         │
  │   Order           │
  │                   │
  │  [a]dd [e]dit [d]el  [↑↓] move
  └───────────────────┘
  Enter: edit selected  Esc: back
```

- Renders a list with cursor highlight
- `a` → pushes a new-item form (empty form, appends to slice on save)
- `e` → pushes an edit-item form (pre-filled, updates slice on save)
- `d` → removes selected item (with confirm)
- `↑↓` → move cursor
- `Esc` → pop back to parent

The ListManager is parameterized: it takes a label, a slice pointer, a form builder function, and a label extractor function.

## Integer Field Handling

`huh.Input` works with strings. For integer fields (`SidebarWidth`, `Card.Columns`, `Pool.MaxOpen`, etc.):

1. Create a temp `string` variable bound to `huh.Input`
2. After form submission, parse with `strconv.Atoi` and set the int field
3. On form init, convert current int to string with `strconv.Itoa`

`helpers.go` provides:

```go
func intField(label string, value *int) huh.Field {
    s := strconv.Itoa(*value)
    return huh.NewInput().Title(label).Value(&s)
    // caller applies: *value, _ = strconv.Atoi(s) after form submit
}
```

## Map Field Handling (Options)

`Column.Options` and `Field.Options` are `map[string]string`. The editor shows a simple key-value list with add/remove:

```
  Options
  ┌───────────────────┐
  │ active  = Active  │
  │ pending = Pending │
  │                     │
  │  [a]dd [d]el       │
  └───────────────────┘
```

Each add/edit opens a two-field form (Key input, Value input).

## Save/Quit Flow

- **`s` key** from MainMenu: marshal `cfg` → `yaml.Marshal` → write to `configPath` → print "Saved to {path}" → exit
- **`q` key**: if modified, show confirm "Unsaved changes. Save? [y/N]". If yes, save. If no, discard and exit.
- `edit.go` flow:

```go
func cmdEdit() {
    configPath, _, _, _, _, _ := parseGlobalFlags()
    cfg, err := parser.ParseFile(configPath)
    // ... error handling
    ed := editor.New(cfg, configPath)
    saved, err := ed.Run()
    if err != nil { ... }
    if saved {
        data, _ := yaml.Marshal(cfg)
        os.WriteFile(configPath, data, 0644)
        fmt.Printf("Saved %s\n", configPath)
    }
}
```

## Updated `main.go`

```go
case "edit":
    cmdEdit()
```

```go
func printUsage() {
    fmt.Println(`...
  go-fila edit          Interactive YAML config editor
  ...`)
}
```

## Implementation Order

1. **Scaffold**: Create `cmd/go-fila/edit.go` + `cmd/go-fila/editor/` directory
2. **Dependencies**: `go get github.com/charmbracelet/huh`
3. **Options**: `editor/options.go` — all enum option slices
4. **Helpers**: `editor/helpers.go` — int↔string, map editor, generic field builders
5. **Stack model**: `editor/model.go` — EditorModel with push/pop/Run
6. **Main menu**: `editor/menu.go` — top-level block selection
7. **Simple blocks**: `editor/panel.go`, `editor/connection.go`, `editor/sqlc.go`, `editor/auth.go`
8. **List manager**: `editor/list.go` — generic add/edit/delete for arrays
9. **Complex blocks**: `editor/resource.go`, `editor/page.go`, `editor/navigation.go`
10. **Field editing**: `editor/field.go` — Column + Field forms
11. **Wire into main.go**: Add `edit` case in switch
12. **Save/quit**: Integrate in `editor/model.go` + `cmd/go-fila/edit.go`
13. **Update docs**: AGENTS.md, README.md, SPEC.md

## Edge Cases

- **Empty config**: Show empty defaults in all form fields
- **Empty arrays**: ListManager shows "No items. Press [a] to add."
- **Nested arrays** (stats_grid widgets, resource form fields): Each level uses its own ListManager instance
- **Type-dependent widget fields**: When widget type changes, show/hide chart-specific fields. Implemented by having separate form groups per widget type and filtering at display time (or building a new form after type selection)
- **`map[string]string` fields**: Key-value editor with add/remove
- **`[]string` fields** (login.fields, card.searchable, visible): `huh.MultiSelect` with available options
- **Validation on save**: Check required fields (resource name, panel name/path) and show errors before writing

## Example Session

```
$ go-fila edit --config go-fila.yaml

  ┌─ go-fila config editor ──────────────────────────────┐
  │                                                       │
  │  Panel                                    [3 fields]  │
  │  Connections                            [1 conn]      │
  │  SQLC                                  [4 fields]     │
  │  Auth                                  [8 fields]     │
  │  Navigation                            [2 groups]     │
  │  Resources                             [3 items]      │
  │  Pages                                 [1 item]       │
  │                                                       │
  │  ↑↓ navigate  Enter edit  s save  q quit             │
  └───────────────────────────────────────────────────────┘

[Enter on Resources]

  Resources
  ┌───────────────────┐
  │ > User            │
  │   Product         │
  │   Order           │
  │                   │
  │  [a]dd [e]dit [d]el  [↑↓] move
  └───────────────────┘

[e on User]

  Resource: User
  1. List View
  2. Detail View
  3. Card View
  4. Form (Create/Update)
  5. Actions (2)
  6. Policies

[Enter on List View]

  ── List View ──────────────────────────────────────────
  Query:        ListUsers
  Count Query:  CountUsers
  Default Sort: -created_at

  Columns (4 items):

  ┌────────────────────┐
  │ > id (integer)     │
  │   name (string)    │
  │   email (email)    │
  │   status (badge)   │
  │                    │
  │  [a]dd [e]dit [d]el│
  └────────────────────┘
```
