package types

type Page struct {
	Name    string   `yaml:"name"`
	Path    string   `yaml:"path"`
	Default bool     `yaml:"default"`
	Widgets []Widget `yaml:"widgets"`
}

type Widget struct {
	Type        string        `yaml:"type"`
	Label       string        `yaml:"label"`
	Query       string        `yaml:"query"`
	Icon        string        `yaml:"icon"`
	Color       string        `yaml:"color"`
	Prefix      string        `yaml:"prefix"`
	Limit       int           `yaml:"limit"`
	Columns     int           `yaml:"columns"`
	DataColumns []string      `yaml:"data_columns"`
	Widgets     []Widget      `yaml:"widgets"`
	Chart       *ChartConfig  `yaml:"chart"`
}

type ChartConfig struct {
	Type  string `yaml:"type"`
	Query string `yaml:"query"`
	X     string `yaml:"x"`
	Y     string `yaml:"y"`
}
