// viewmodels.go
//
// Generates internal/viewmodels/models.go for the admin panel application:
// plain Go structs that carry data from the handlers into the templ views
// (list, detail, form, page and widget data).
package generator

import (
	"fmt"
	"os"
	"path/filepath"
)

// generateViewModels writes internal/viewmodels/models.go containing the view
// data structs (ColumnDef, ListData, DetailData, FormData, PageData,
// WidgetData, AuthData, NavGroupData, NavItemData, ThemeConfig) plus the
// DefaultTheme constructor that packs the panel's theming/layout config for the
// templ Base layout. Returns an error on write failure.
func (g *Generator) generateViewModels() error {
	panel := g.Config.Panel
	primary := panel.Brand.Colors.Primary
	if primary == "" {
		primary = "#6366f1"
	}
	secondary := panel.Brand.Colors.Secondary
	if secondary == "" {
		secondary = "#8b5cf6"
	}
	width := panel.Layout.Sidebar.Width
	if width == 0 {
		width = 256
	}
	collapsed := panel.Layout.Sidebar.CollapsedWidth
	if collapsed == 0 {
		collapsed = 64
	}
	maxWidth := panel.Layout.MaxContentWidth
	if maxWidth == "" {
		maxWidth = "none"
	}

	code := fmt.Sprintf(`package viewmodels

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "html/template"
)

type ThemeConfig struct {
	DarkMode            bool
	BrandPrimary        string
	BrandSecondary      string
	FontFamily          string
	FontMono            string
	SidebarWidth        int
	SidebarCollapsedWidth int
	SidebarCollapsible  bool
	TopbarSticky        bool
	MaxContentWidth     string
}

func DefaultTheme() ThemeConfig {
    return ThemeConfig{
        DarkMode: %t,
        BrandPrimary: %q,
        BrandSecondary: %q,
        FontFamily: %q,
        FontMono: %q,
        SidebarWidth: %d,
        SidebarCollapsedWidth: %d,
        SidebarCollapsible: %t,
        TopbarSticky: %t,
        MaxContentWidth: %q,
    }
}

type ColumnDef struct {
	Name       string
	Label      string
	FieldType  string
	Sortable   bool
	Searchable bool
	Picker     bool
	Options    map[string]string
}
`,
		panel.Theme.DarkMode, primary, secondary, panel.Theme.Font.Family, panel.Theme.Font.Mono,
		width, collapsed, panel.Layout.Sidebar.Collapsible, panel.Layout.Topbar.Sticky, maxWidth)

	code += `

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

type CardColumnData struct {
	Key      string
	Label    string
	Items    []map[string]interface{}
}

type CardData struct {
	Items      []map[string]interface{}
	Page       int
	PerPage    int
	Total      int
	TotalPages int
	Search     string
	Sort       string
	Order      string
	Fields     []ColumnDef
	Columns    int
	Rows       int
	Kanban     bool
	KanbanField string
	KanbanColumns []CardColumnData
	Resource   string
	PanelPath  string
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

// OptionLabel returns the label for a field option key, falling back to the
// raw key when the field has no matching option (or the field is unknown).
func OptionLabel(fields []ColumnDef, name string, raw string) string {
	for _, fd := range fields {
		if fd.Name == name {
			if l, ok := fd.Options[raw]; ok {
				return l
			}
			return raw
		}
	}
	return raw
}

// ItemValue formats a form item value for display/input, returning "" for
// missing (nil) values instead of Go's "<nil>".
func ItemValue(item map[string]interface{}, name string) string {
	if v, ok := item[name]; ok && v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// OptionsJS renders a field's option map as a JSON object literal (keys
// sorted, proper escaping) for use by the modal picker. Returns "{}" when the
// field is unknown or has no options.
func OptionsJS(fields []ColumnDef, name string) string {
	for _, fd := range fields {
		if fd.Name == name {
			b, err := json.Marshal(fd.Options)
			if err != nil {
				return "{}"
			}
			return string(b)
		}
	}
	return "{}"
}
`

	return os.WriteFile(filepath.Join(g.OutDir, "internal/viewmodels/models.go"), []byte(code), 0644)
}
