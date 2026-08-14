// resource.go
//
// YAML-tagged structs describing CRUD resources: list/detail/form sections,
// custom actions, columns, fields and optional RBAC policies.
package types

// Resource is a CRUD-managed entity (e.g. "User") with optional list, detail,
// form, actions and policies sections.
type Resource struct {
	Name     string        `yaml:"name"`
	Label    string        `yaml:"label"`
	Icon     string        `yaml:"icon"`
	Group    string        `yaml:"group"`
	Table    string        `yaml:"table"`
	IDType   string        `yaml:"id_type"`
	IDColumn string        `yaml:"id_column"`
	List     *ListConfig   `yaml:"list"`
	Card     *CardConfig   `yaml:"card"`
	Detail   *DetailConfig `yaml:"detail"`
	Form     *FormConfig   `yaml:"form"`
	Actions  []Action      `yaml:"actions"`
	Policies *Policy       `yaml:"policies"`
	// ImportCSV enables the POST /{res}/import/csv route and the "Import CSV"
	// button on the list view. Imports reuse the create form's field set.
	ImportCSV bool `yaml:"import_csv"`
}

// ListConfig defines the resource list view: the displayed columns, the rows
// per page and the default sort (a leading "-" means descending). The list
// handler builds its own raw paginated query with a windowed COUNT(*) OVER()
// for the total, so the SQLC Query/CountQuery names are informational only.
type ListConfig struct {
	Query       string   `yaml:"query"`
	CountQuery  string   `yaml:"count_query"`
	Columns     []Column `yaml:"columns"`
	PerPage     int      `yaml:"per_page"`
	DefaultSort string   `yaml:"default_sort"`
	// Export is an optional subset of column names for CSV export. When set,
	// the export emits only those columns (with Label headers); when empty the
	// export falls back to all list columns with raw column-name headers.
	Export []string `yaml:"export"`
	// Filter is an optional collapsible filter section above the list table. The
	// where expression is a mini-DSL compiled at generation time into
	// dialect-correct SQL; runtime-valued $N params are collected from labeled
	// inputs on the filter form and travel in URL query params.
	Filter *FilterConfig `yaml:"filter"`
}

// CardConfig defines a card-grid view of the resource: display fields (cards
// share the same field definition as forms), how many cards to fit per row
// (Columns) and rows per page (Rows), and an optional select field name used
// to render a kanban board instead of a grid. Pagination and search behave
// like the list view.
type CardConfig struct {
	Fields      []Field  `yaml:"fields"`
	Columns     int      `yaml:"columns"`
	Rows        int      `yaml:"rows"`
	KanbanField string   `yaml:"kanban_field"`
	Searchable  []string `yaml:"searchable"`
	DefaultSort string   `yaml:"default_sort"`
	// Filter is an optional collapsible filter section above the card grid,
	// with the same shape and runtime behavior as list.filter.
	Filter *FilterConfig `yaml:"filter"`
}

// FilterConfig defines a collapsible filter section on a list or card view: a
// label for the collapsible header and a `where` expression (the filterexpr
// mini-DSL) plus the ordered runtime params ($N tokens) it references. The
// expression and params are trusted YAML compiled to SQL at generation time.
type FilterConfig struct {
	Label  string        `yaml:"label"`
	Where  string        `yaml:"where"`
	Params []FilterParam `yaml:"params"`
}

// FilterParam names a runtime-valued $N token in a filter `where` expression.
// Each param renders as a labeled input on the filter form; a missing or empty
// value on Apply skips the filter entirely.
type FilterParam struct {
	Name  string `yaml:"name"`
	Label string `yaml:"label"`
}

// Column is a single list column: its name, label, type, sortable/searchable
// flags and static display options.
type Column struct {
	Name       string            `yaml:"name"`
	Label      string            `yaml:"label"`
	Type       string            `yaml:"type"`
	Sortable   bool              `yaml:"sortable"`
	Searchable bool              `yaml:"searchable"`
	Options    map[string]string `yaml:"options"`
}

// DetailConfig defines the resource detail view: the SQLC query, its
// parameters and the fields to display.
type DetailConfig struct {
	Query  string            `yaml:"query"`
	Params map[string]string `yaml:"params"`
	Fields []Field           `yaml:"fields"`
}

// FormConfig groups the create, update and delete form actions of a resource.
type FormConfig struct {
	Create *FormAction `yaml:"create"`
	Update *FormAction `yaml:"update"`
	Delete *FormAction `yaml:"delete"`
}

// FormAction defines one form action (create/update/delete): its SQLC query,
// the query used to populate the form on GET, and the form fields.
type FormAction struct {
	Query          string            `yaml:"query"`
	PopulateQuery  string            `yaml:"populate_query"`
	PopulateParams map[string]string `yaml:"populate_params"`
	Fields         []Field           `yaml:"fields"`
	Hooks          *Hooks            `yaml:"hooks"`
}

// Field is a single form/detail field: its name, label, type, required flag,
// visibility contexts, validation and its options (static map or a SQLC-backed
// query).
type Field struct {
	Name         string            `yaml:"name"`
	Label        string            `yaml:"label"`
	Type         string            `yaml:"type"`
	Required     bool              `yaml:"required"`
	Visible      []string          `yaml:"visible"`
	Validation   *Validation       `yaml:"validation"`
	OptionsQuery string            `yaml:"options_query"`
	OptionsValue string            `yaml:"options_value"`
	OptionsLabel string            `yaml:"options_label"`
	OptionsSQL   string            `yaml:"options_sql"`
	Options      map[string]string `yaml:"options"`
}

// Validation declares min/max constraints for a form field.
type Validation struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// Action is a custom row action: name/label/icon/color, optional confirmation
// and bulk support, and either the SQL to execute or a stored procedure to
// call (proc; ignored on sqlite). Query and Proc are mutually exclusive.
// Policy, when set, restricts the action (and its bulk variant) to the listed
// roles (a "|" separates roles), enforced by the generated
// auth.ActionRBACMiddleware on the action/bulk routes.
type Action struct {
	Name                 string `yaml:"name"`
	Label                string `yaml:"label"`
	Icon                 string `yaml:"icon"`
	Color                string `yaml:"color"`
	RequiresConfirmation bool   `yaml:"requires_confirmation"`
	Bulk                 bool   `yaml:"bulk"`
	Query                string `yaml:"query"`
	Proc                 string `yaml:"proc"`
	Policy               string `yaml:"policy"`
	Hooks                *Hooks `yaml:"hooks"`
}

// Policy lists the roles allowed for each resource action (view_any, view,
// create, update, delete). A "|" in a value separates allowed roles.
type Policy struct {
	ViewAny string `yaml:"view_any"`
	View    string `yaml:"view"`
	Create  string `yaml:"create"`
	Update  string `yaml:"update"`
	Delete  string `yaml:"delete"`
}
