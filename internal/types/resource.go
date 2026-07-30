package types

type Resource struct {
	Name     string       `yaml:"name"`
	Label    string       `yaml:"label"`
	Icon     string       `yaml:"icon"`
	Group    string       `yaml:"group"`
	List     *ListConfig  `yaml:"list"`
	Detail   *DetailConfig `yaml:"detail"`
	Form     *FormConfig  `yaml:"form"`
	Actions  []Action     `yaml:"actions"`
	Policies *Policy      `yaml:"policies"`
}

type ListConfig struct {
	Query       string       `yaml:"query"`
	CountQuery  string       `yaml:"count_query"`
	Columns     []Column     `yaml:"columns"`
	DefaultSort string       `yaml:"default_sort"`
}

type Column struct {
	Name       string         `yaml:"name"`
	Label      string         `yaml:"label"`
	Type       string         `yaml:"type"`
	Sortable   bool           `yaml:"sortable"`
	Searchable bool           `yaml:"searchable"`
	Options    map[string]string `yaml:"options"`
}

type DetailConfig struct {
	Query  string            `yaml:"query"`
	Params map[string]string `yaml:"params"`
	Fields []Field           `yaml:"fields"`
}

type FormConfig struct {
	Create *FormAction `yaml:"create"`
	Update *FormAction `yaml:"update"`
	Delete *FormAction `yaml:"delete"`
}

type FormAction struct {
	Query          string            `yaml:"query"`
	PopulateQuery  string            `yaml:"populate_query"`
	PopulateParams map[string]string `yaml:"populate_params"`
	Fields         []Field           `yaml:"fields"`
}

type Field struct {
	Name           string            `yaml:"name"`
	Label          string            `yaml:"label"`
	Type           string            `yaml:"type"`
	Required       bool              `yaml:"required"`
	Visible        []string          `yaml:"visible"`
	Validation     *Validation       `yaml:"validation"`
	OptionsQuery   string            `yaml:"options_query"`
	OptionsValue   string            `yaml:"options_value"`
	OptionsLabel   string            `yaml:"options_label"`
	Options        map[string]string `yaml:"options"`
}

type Validation struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

type Action struct {
	Name                string `yaml:"name"`
	Label               string `yaml:"label"`
	Icon                string `yaml:"icon"`
	Color               string `yaml:"color"`
	RequiresConfirmation bool   `yaml:"requires_confirmation"`
	Bulk                bool   `yaml:"bulk"`
	Query               string `yaml:"query"`
}

type Policy struct {
	ViewAny string `yaml:"view_any"`
	View    string `yaml:"view"`
	Create  string `yaml:"create"`
	Update  string `yaml:"update"`
	Delete  string `yaml:"delete"`
}
