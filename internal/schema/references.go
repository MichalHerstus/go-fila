// references.go
//
// Walks a *types.Config and collects every query name referenced from YAML
// (list/detail/form/delete/action/options_query/widget queries) plus the table
// and column names each resource references. The sync tool compares this set
// against what is actually defined in sql/queries and sql/migrations.
package schema

import (
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// QueryRef is a single query name referenced from the YAML config.
type QueryRef struct {
	Name   string
	Origin string // human-readable location, e.g. "User > list.query"
	Inline bool   // true when the reference is inline SQL, not a SQLC query name
}

// References is the full set of YAML-side references extracted from a config.
type References struct {
	Queries []QueryRef
	Tables  map[string]string   // resource name -> table name (lowercased)
	Columns map[string][]string // resource name -> column/field names referenced
}

// CollectReferences walks cfg and returns all query/table/column references.
func CollectReferences(cfg *types.Config) *References {
	refs := &References{
		Tables:  map[string]string{},
		Columns: map[string][]string{},
	}
	for _, r := range cfg.Resources {
		name := r.Name
		refs.Tables[name] = TableNameFor(r)
		if r.List != nil {
			refs.addQuery(r.List.Query, name+".list.query")
			refs.addQuery(r.List.CountQuery, name+".list.count_query")
			for _, c := range r.List.Columns {
				refs.addColumn(name, c.Name)
			}
		}
		if r.Card != nil {
			for _, f := range r.Card.Fields {
				refs.addFieldRefs(name, f, name+".card.fields")
			}
		}
		if r.Detail != nil {
			refs.addQuery(r.Detail.Query, name+".detail.query")
			for _, f := range r.Detail.Fields {
				refs.addFieldRefs(name, f, name+".detail.fields")
			}
		}
		if r.Form != nil {
			for _, fa := range []*types.FormAction{r.Form.Create, r.Form.Update, r.Form.Delete} {
				if fa == nil {
					continue
				}
				section := "form"
				switch {
				case fa == r.Form.Create:
					section = "form.create"
				case fa == r.Form.Update:
					section = "form.update"
				case fa == r.Form.Delete:
					section = "form.delete"
				}
				refs.addQuery(fa.Query, name+"."+section+".query")
				refs.addQuery(fa.PopulateQuery, name+"."+section+".populate_query")
				for _, f := range fa.Fields {
					refs.addFieldRefs(name, f, name+"."+section+".fields")
				}
			}
		}
		for _, a := range r.Actions {
			refs.addQuery(a.Query, name+".actions."+a.Name)
		}
	}
	for _, p := range cfg.Pages {
		collectWidgetQueries(refs, p.Widgets, "page "+p.Name)
	}
	return refs
}

func (refs *References) addQuery(name, origin string) {
	if name == "" {
		return
	}
	for _, q := range refs.Queries {
		if q.Name == name && q.Origin == origin {
			return
		}
	}
	refs.Queries = append(refs.Queries, QueryRef{Name: name, Origin: origin, Inline: isInlineSQL(name)})
}

// isInlineSQL reports whether a YAML query value is literal SQL rather than a
// SQLC query name. Query names are single identifiers without whitespace;
// inline SQL always contains spaces (SELECT/UPDATE/INSERT/DELETE/WITH …).
func isInlineSQL(s string) bool {
	return strings.ContainsAny(s, " \t\n")
}

func (refs *References) addColumn(resource, col string) {
	if col == "" {
		return
	}
	for _, c := range refs.Columns[resource] {
		if c == col {
			return
		}
	}
	refs.Columns[resource] = append(refs.Columns[resource], col)
}

func (refs *References) addFieldRefs(resource string, f types.Field, origin string) {
	refs.addColumn(resource, f.Name)
	if f.OptionsQuery != "" {
		refs.addQuery(f.OptionsQuery, origin+"."+f.Name)
	}
}

// collectWidgetQueries recurses into widget trees collecting query references.
func collectWidgetQueries(refs *References, widgets []types.Widget, origin string) {
	for _, w := range widgets {
		refs.addQuery(w.Query, origin+".widgets."+w.Label)
		if w.Chart != nil {
			refs.addQuery(w.Chart.Query, origin+".widgets."+w.Label+".chart")
		}
		if len(w.Widgets) > 0 {
			collectWidgetQueries(refs, w.Widgets, origin+".widgets."+w.Label)
		}
	}
}

// TableNameFor returns the table a resource maps to: the explicit table: field
// when set, otherwise the lowercased pluralised resource name (e.g. "User" ->
// "users"). Matching in the sync tool is case-insensitive and also tries a
// snake_case variant for multi-word names.
func TableNameFor(r types.Resource) string {
	if r.Table != "" {
		return r.Table
	}
	return strings.ToLower(Pluralize(r.Name))
}

// FindTableByName looks up a table in a slice by name (case-insensitive).
func FindTableByName(tables []Table, name string) *Table {
	for i := range tables {
		if strings.EqualFold(tables[i].Name, name) {
			return &tables[i]
		}
	}
	return nil
}

// Driver returns the driver of the first connection, defaulting to postgres.
func Driver(cfg *types.Config) string {
	for _, c := range cfg.Connections {
		if c.Driver != "" {
			return c.Driver
		}
	}
	return "postgres"
}
