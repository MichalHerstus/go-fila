// generate.go
//
// YAML emitter used to build resource blocks from parsed schema tables — a
// file-level port of the string builder in cmd/yaga/introspect.go (no database
// connection involved).
package schema

import (
	"fmt"
	"strings"
)

// MapDBTypeToFieldType converts a column type string to a yaga field type.
func MapDBTypeToFieldType(dbType string) string {
	t := strings.ToLower(dbType)
	if idx := strings.Index(t, "("); idx != -1 {
		t = t[:idx]
	}
	t = strings.TrimSpace(t)

	switch {
	case strings.Contains(t, "int") || strings.Contains(t, "serial"):
		return "integer"
	case strings.Contains(t, "varchar") || strings.Contains(t, "text") || strings.Contains(t, "char") || strings.Contains(t, "character") || strings.Contains(t, "uniqueidentifier") || strings.Contains(t, "xml"):
		return "string"
	case strings.Contains(t, "bool") || t == "bit":
		return "boolean"
	case strings.Contains(t, "timestamp") || strings.Contains(t, "datetime") || strings.Contains(t, "date") || t == "time":
		return "datetime"
	case strings.Contains(t, "real") || strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "numeric") || strings.Contains(t, "decimal") || strings.Contains(t, "money"):
		return "float"
	case strings.Contains(t, "json"):
		return "json"
	case strings.Contains(t, "bytea") || strings.Contains(t, "blob") || strings.Contains(t, "varbinary") || strings.Contains(t, "binary") || strings.Contains(t, "image"):
		return "file"
	default:
		return "string"
	}
}

// Singularize converts a plural table name to a singular form.
func Singularize(tableName string) string {
	lower := strings.ToLower(tableName)
	switch {
	case strings.HasSuffix(lower, "ies"):
		return tableName[:len(tableName)-3] + "y"
	case strings.HasSuffix(lower, "ses") || strings.HasSuffix(lower, "xes") || strings.HasSuffix(lower, "zes") || strings.HasSuffix(lower, "ches") || strings.HasSuffix(lower, "shes"):
		return tableName[:len(tableName)-2]
	case strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss"):
		return tableName[:len(tableName)-1]
	default:
		return tableName
	}
}

// Pluralize converts a singular resource name to a plural table-like name.
func Pluralize(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, "y") && !containsAny(lower, "a", "e", "i", "o", "u"):
		return name[:len(name)-1] + "ies"
	case strings.HasSuffix(lower, "s") || strings.HasSuffix(lower, "x") || strings.HasSuffix(lower, "z") || strings.HasSuffix(lower, "ch") || strings.HasSuffix(lower, "sh"):
		return name + "es"
	default:
		return name + "s"
	}
}

// ToPascalCase converts a snake_case or lowercase string to PascalCase.
func ToPascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "")
}

// ToSingularPascal converts a table name to singular PascalCase.
func ToSingularPascal(tableName string) string {
	return ToPascalCase(Singularize(tableName))
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// FindLabelColumn picks the best human-readable column for a table.
func FindLabelColumn(t Table) string {
	for _, c := range t.Columns {
		if strings.ToLower(c.Name) == "name" {
			return c.Name
		}
	}
	for _, c := range t.Columns {
		if strings.ToLower(c.Name) == "title" {
			return c.Name
		}
	}
	for _, c := range t.Columns {
		if strings.ToLower(c.Name) == "label" {
			return c.Name
		}
	}
	for _, c := range t.Columns {
		if !c.IsPrimaryKey && MapDBTypeToFieldType(c.Type) == "string" {
			return c.Name
		}
	}
	for _, c := range t.Columns {
		if c.IsPrimaryKey {
			return c.Name
		}
	}
	if len(t.Columns) > 0 {
		return t.Columns[0].Name
	}
	return ""
}

// FindLabelColumnByTable finds the label column of a named table.
func FindLabelColumnByTable(tables []Table, name string) string {
	if t := FindTableByName(tables, name); t != nil {
		return FindLabelColumn(*t)
	}
	return "name"
}

// FindPKColumn returns the primary key column of a table (default "id").
func FindPKColumn(t Table) string {
	for _, c := range t.Columns {
		if c.IsPrimaryKey {
			return c.Name
		}
	}
	return "id"
}

// IDColumnName returns the actual key column name preserving case ("" if none).
func IDColumnName(t Table) string {
	pk := FindPKColumn(t)
	for _, c := range t.Columns {
		if strings.EqualFold(c.Name, pk) {
			return c.Name
		}
	}
	return ""
}

// FindDefaultSort picks a sensible default sort column.
func FindDefaultSort(t Table) string {
	for _, c := range t.Columns {
		if MapDBTypeToFieldType(c.Type) == "datetime" {
			return c.Name
		}
	}
	return FindPKColumn(t)
}

// pkGoType mirrors sqlc's type mapping for the key column.
func pkGoType(t Table, driver string) string {
	pk := FindPKColumn(t)
	for _, c := range t.Columns {
		if !strings.EqualFold(c.Name, pk) {
			continue
		}
		if driver == "sqlite" {
			return "int64"
		}
		switch strings.ToLower(c.Type) {
		case "bigint":
			return "int64"
		case "smallint":
			return "int16"
		default:
			return "int32"
		}
	}
	return ""
}

// GenerateResourceYAML produces a yaga resource block for one table.
func GenerateResourceYAML(ti Table, allTables []Table, driver string) string {
	var b strings.Builder
	resourceName := ToSingularPascal(ti.Name)
	pluralPascal := ToPascalCase(ti.Name)
	pk := FindPKColumn(ti)

	b.WriteString(fmt.Sprintf("  - name: %s\n", resourceName))
	b.WriteString(fmt.Sprintf("    label: %s\n", pluralPascal))
	if strings.ToLower(resourceName)+"s" != ti.Name {
		b.WriteString(fmt.Sprintf("    table: %s\n", ti.Name))
	}
	if idCol := IDColumnName(ti); idCol != "" && idCol != "id" {
		b.WriteString(fmt.Sprintf("    id_column: %s\n", idCol))
	}
	pkGo := pkGoType(ti, driver)
	defaultPKGo := "int32"
	if driver == "sqlite" {
		defaultPKGo = "int64"
	}
	if pkGo != "" && pkGo != defaultPKGo {
		b.WriteString(fmt.Sprintf("    id_type: %s\n", pkGo))
	}
	defaultSort := FindDefaultSort(ti)

	// list
	b.WriteString("    list:\n")
	b.WriteString(fmt.Sprintf("      query: List%s\n", pluralPascal))
	b.WriteString(fmt.Sprintf("      count_query: Count%s\n", pluralPascal))
	b.WriteString("      columns:\n")
	for _, c := range ti.Columns {
		if isFKColumn(ti, c.Name) {
			continue
		}
		writeColumnYAML(&b, c)
	}
	for _, fk := range ti.FKs() {
		ft := FindTableByName(allTables, fk.ForeignTable)
		if ft == nil {
			continue
		}
		b.WriteString(fmt.Sprintf("        - name: %s\n", fk.Column+"_label"))
		b.WriteString(fmt.Sprintf("          label: %s\n", ToPascalCase(Singularize(fk.ForeignTable))))
		b.WriteString("          type: string\n")
	}
	if defaultSort != "" {
		b.WriteString(fmt.Sprintf("      default_sort: -%s\n", defaultSort))
	}

	// detail
	b.WriteString("    detail:\n")
	b.WriteString(fmt.Sprintf("      query: Get%s\n", ToSingularPascal(ti.Name)))
	b.WriteString("      params:\n")
	b.WriteString(fmt.Sprintf("        id: \"{record.%s}\"\n", pk))
	b.WriteString("      fields:\n")
	for _, c := range ti.Columns {
		writeFieldYAML(&b, c, ti, allTables, driver, false, "        ")
	}

	// form
	b.WriteString("    form:\n")
	b.WriteString("      create:\n")
	b.WriteString(fmt.Sprintf("        query: Create%s\n", ToSingularPascal(ti.Name)))
	b.WriteString("        fields:\n")
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			continue
		}
		if c.Default != "" && c.Nullable {
			continue
		}
		writeFieldYAML(&b, c, ti, allTables, driver, true, "          ")
	}
	b.WriteString("      update:\n")
	b.WriteString(fmt.Sprintf("        query: Update%s\n", ToSingularPascal(ti.Name)))
	b.WriteString(fmt.Sprintf("        populate_query: Get%s\n", ToSingularPascal(ti.Name)))
	b.WriteString("        fields:\n")
	for _, c := range ti.Columns {
		if c.IsPrimaryKey {
			continue
		}
		writeFieldYAML(&b, c, ti, allTables, driver, true, "          ")
	}
	return b.String()
}

func isFKColumn(ti Table, col string) bool {
	for _, fk := range ti.FKs() {
		if fk.Column == col {
			return true
		}
	}
	return false
}

func writeColumnYAML(b *strings.Builder, c Column) {
	ft := MapDBTypeToFieldType(c.Type)
	b.WriteString(fmt.Sprintf("        - name: %s\n", c.Name))
	b.WriteString(fmt.Sprintf("          type: %s\n", ft))
	if c.IsPrimaryKey || ft == "integer" {
		b.WriteString("          sortable: true\n")
	}
	if ft == "string" || ft == "email" {
		b.WriteString("          searchable: true\n")
	}
}

func writeFieldYAML(b *strings.Builder, c Column, ti Table, allTables []Table, driver string, isForm bool, indent string) {
	for _, fk := range ti.FKs() {
		if fk.Column == c.Name {
			if isForm {
				b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
				b.WriteString(indent + "  type: relation\n")
				foreignResource := ToSingularPascal(fk.ForeignTable)
				b.WriteString(fmt.Sprintf("%s  options_query: List%s\n", indent, Pluralize(foreignResource)))
				b.WriteString(fmt.Sprintf("%s  options_value: %s\n", indent, fk.ForeignColumn))
				labelCol := FindLabelColumnByTable(allTables, fk.ForeignTable)
				b.WriteString(fmt.Sprintf("%s  options_label: %s\n", indent, labelCol))
			} else {
				b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
				b.WriteString(indent + "  type: integer\n")
			}
			return
		}
	}
	ft := MapDBTypeToFieldType(c.Type)
	b.WriteString(fmt.Sprintf("%s- name: %s\n", indent, c.Name))
	b.WriteString(fmt.Sprintf("%s  type: %s\n", indent, ft))
	if c.Name == "password" {
		b.WriteString(indent + "  type: password\n")
	}
}
