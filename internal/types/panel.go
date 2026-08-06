// panel.go
//
// YAML-tagged structs describing custom dashboard pages and their widgets
// (stat, stats_grid, chart, table, list, html).
package types

// Page is a custom dashboard page in the admin panel, optionally marked as
// the default route.
type Page struct {
	Name    string   `yaml:"name"`
	Path    string   `yaml:"path"`
	Default bool     `yaml:"default"`
	Widgets []Widget `yaml:"widgets"`
}

// Widget is a dashboard widget. Depending on Type it uses different fields:
// stat (label/query/icon/color), chart (label/query + Chart), table
// (label/query/data_columns), stats_grid (columns + nested Widgets), list/html.
type Widget struct {
	Type        string       `yaml:"type"`
	Label       string       `yaml:"label"`
	Query       string       `yaml:"query"`
	Icon        string       `yaml:"icon"`
	Color       string       `yaml:"color"`
	Prefix      string       `yaml:"prefix"`
	Limit       int          `yaml:"limit"`
	Columns     int          `yaml:"columns"`
	DataColumns []string     `yaml:"data_columns"`
	Widgets     []Widget     `yaml:"widgets"`
	Chart       *ChartConfig `yaml:"chart"`
}

// ChartConfig configures a chart widget: the chart type and optional query
// with x/y axes.
type ChartConfig struct {
	Type  string `yaml:"type"`
	Query string `yaml:"query"`
	X     string `yaml:"x"`
	Y     string `yaml:"y"`
}
