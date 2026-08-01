// viewmodels.go
//
// Generates internal/viewmodels/models.go for the admin panel application:
// plain Go structs that carry data from the handlers into the templ views
// (list, detail, form, page and widget data).
package generator

import (
	"os"
	"path/filepath"
)

// generateViewModels writes internal/viewmodels/models.go containing the view
// data structs (ColumnDef, ListData, DetailData, FormData, PageData,
// WidgetData, AuthData, NavGroupData, NavItemData). Returns an error on write
// failure.
func (g *Generator) generateViewModels() error {
	code := `package viewmodels

import (
    "database/sql"
    "fmt"
    "html/template"
)

type ColumnDef struct {
	Name       string
	Label      string
	FieldType  string
	Sortable   bool
	Searchable bool
	Options    map[string]string
}

type ListData struct {
	Items      []map[string]interface{}
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	Search     string
	Sort       string
	Order      string
	Columns    []ColumnDef
	Resource   string
	PanelPath  string
}

type DetailData struct {
	Item      map[string]interface{}
	Fields    []ColumnDef
	Resource  string
	PanelPath string
}

type FormData struct {
	Item      map[string]interface{}
	Fields    []ColumnDef
	Action    string
	Method    string
	Resource  string
	PanelPath string
	IsCreate  bool
}

type PageData struct {
	Name     string
	PanelID  string
	PanelPath string
	Widgets  []WidgetData
}

type WidgetData struct {
	Type        string
	Label       string
	Value       template.HTML
	Color       string
	Icon        string
	Prefix      string
	Suffix      string
	ChartType   string
	ChartLabels []string
	ChartValues []float64
	ChartLabelsJSON string
	ChartValuesJSON string
	TableColumns    []string
	TableRows       []map[string]interface{}
	SubWidgets      []WidgetData
}

type AuthData struct {
	Email     string
	Error     string
	PanelPath string
	PanelName string
}

type NavGroupData struct {
	Group string
	Icon  string
	Items []NavItemData
}

type NavItemData struct {
	Label       string
	URL         string
	OpensInNewTab bool
}

func OptionValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case int64:
		return fmt.Sprintf("%d", t)
	case int32:
		return fmt.Sprintf("%d", t)
	case int:
		return fmt.Sprintf("%d", t)
	case float64:
		return fmt.Sprintf("%v", t)
	case bool:
		return fmt.Sprintf("%v", t)
	case sql.NullInt64:
		if t.Valid {
			return fmt.Sprintf("%d", t.Int64)
		}
		return ""
	case sql.NullInt32:
		if t.Valid {
			return fmt.Sprintf("%d", t.Int32)
		}
		return ""
	case sql.NullString:
		if t.Valid {
			return t.String
		}
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
`

	return os.WriteFile(filepath.Join(g.OutDir, "internal/viewmodels/models.go"), []byte(code), 0644)
}
