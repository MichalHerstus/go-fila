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
}

// ListConfig defines the resource list view: SQLC queries for rows and the
// count, the displayed columns and the default sort (a leading "-" means
// descending).
// ListConfig defines the resource list view: SQLC queries for rows and the
// count, the displayed columns and the default sort (a leading "-" means
// descending).
type ListConfig struct {
	Query       string   `yaml:"query"`
	CountQuery  string   `yaml:"count_query"`
	Columns     []Column `yaml:"columns"`
	DefaultSort string   `yaml:"default_sort"`
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
	Options      map[string]string `yaml:"options"`
}

// Validation declares min/max constraints for a form field.
type Validation struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// Action is a custom row action: name/label/icon/color, optional confirmation
// and bulk support, and the SQL to execute.
type Action struct {
	Name                 string `yaml:"name"`
	Label                string `yaml:"label"`
	Icon                 string `yaml:"icon"`
	Color                string `yaml:"color"`
	RequiresConfirmation bool   `yaml:"requires_confirmation"`
	Bulk                 bool   `yaml:"bulk"`
	Query                string `yaml:"query"`
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
