// generate.go
//
// SQL/YAML emitters used by the editor's sync tool to generate missing SQLC
// query stubs and resource YAML blocks from parsed schema tables. These are
// file-level ports of the string builders in cmd/go-fila/introspect.go
// (no database connection involved).
package schema

import (
	"fmt"
	"strings"
)

// MapDBTypeToFieldType converts a column type string to a go-fila field type.
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

func placeholder(n int, driver string) string {
	if driver == "sqlite" {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

// GenerateQueries produces SQLC-annotated queries for the given tables, one
// file per table plus _options.sql files for FK targets.
func GenerateQueries(tables []Table, driver string) map[string]string {
	queries := make(map[string]string)
	generatedFKTargets := map[string]bool{}

	for _, ti := range tables {
		var b strings.Builder
		pluralName := ToPascalCase(ti.Name)
		singularName := ToSingularPascal(ti.Name)
		pk := FindPKColumn(ti)

		// List
		b.WriteString(fmt.Sprintf("-- name: List%s :many\n", pluralName))
		b.WriteString("SELECT ")
		for i, c := range ti.Columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("t.%s", c.Name))
		}
		for _, fk := range ti.FKs() {
			ft := FindTableByName(tables, fk.ForeignTable)
			if ft == nil {
				continue
			}
			labelCol := FindLabelColumn(*ft)
			b.WriteString(fmt.Sprintf(", f_%s.%s AS %s_label", fk.ForeignTable, labelCol, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nFROM %s t", ti.Name))
		for _, fk := range ti.FKs() {
			ft := FindTableByName(tables, fk.ForeignTable)
			if ft == nil {
				continue
			}
			foreignPK := FindPKColumn(*ft)
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, foreignPK, fk.Column))
		}
		if driver != "mssql" {
			b.WriteString(fmt.Sprintf("\nORDER BY t.%s DESC", pk))
		}
		b.WriteString(";\n\n")

		// Count
		b.WriteString(fmt.Sprintf("-- name: Count%s :one\n", pluralName))
		b.WriteString(fmt.Sprintf("SELECT COUNT(*) FROM %s;\n\n", ti.Name))

		// Get
		b.WriteString(fmt.Sprintf("-- name: Get%s :one\n", singularName))
		b.WriteString("SELECT ")
		for i, c := range ti.Columns {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("t.%s", c.Name))
		}
		for _, fk := range ti.FKs() {
			ft := FindTableByName(tables, fk.ForeignTable)
			if ft == nil {
				continue
			}
			labelCol := FindLabelColumn(*ft)
			b.WriteString(fmt.Sprintf(", f_%s.%s AS %s_label", fk.ForeignTable, labelCol, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nFROM %s t", ti.Name))
		for _, fk := range ti.FKs() {
			ft := FindTableByName(tables, fk.ForeignTable)
			if ft == nil {
				continue
			}
			foreignPK := FindPKColumn(*ft)
			b.WriteString(fmt.Sprintf("\nLEFT JOIN %s f_%s ON f_%s.%s = t.%s",
				fk.ForeignTable, fk.ForeignTable, fk.ForeignTable, foreignPK, fk.Column))
		}
		b.WriteString(fmt.Sprintf("\nWHERE t.%s = %s;\n\n", pk, placeholder(1, driver)))

		// Create
		var insertCols []string
		for _, c := range ti.Columns {
			if c.IsPrimaryKey && driver != "sqlite" {
				continue
			}
			insertCols = append(insertCols, c.Name)
		}
		b.WriteString(fmt.Sprintf("-- name: Create%s :one\n", singularName))
		b.WriteString(fmt.Sprintf("INSERT INTO %s (", ti.Name))
		for i, col := range insertCols {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(col)
		}
		b.WriteString(")\nVALUES (")
		for i := range insertCols {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(placeholder(i+1, driver))
		}
		if driver != "sqlite" {
			b.WriteString(")\nRETURNING *;\n\n")
		} else {
			b.WriteString(");\n\n")
		}

		// Update
		var updateCols []string
		for _, c := range ti.Columns {
			if !c.IsPrimaryKey {
				updateCols = append(updateCols, c.Name)
			}
		}
		b.WriteString(fmt.Sprintf("-- name: Update%s :one\n", singularName))
		b.WriteString(fmt.Sprintf("UPDATE %s SET\n", ti.Name))
		for i, col := range updateCols {
			comma := ","
			if i == len(updateCols)-1 {
				comma = ""
			}
			b.WriteString(fmt.Sprintf("    %s = %s%s\n", col, placeholder(i+2, driver), comma))
		}
		b.WriteString(fmt.Sprintf("WHERE %s = %s", pk, placeholder(1, driver)))
		if driver != "sqlite" {
			b.WriteString("\nRETURNING *;\n\n")
		} else {
			b.WriteString(";\n\n")
		}

		// Delete
		b.WriteString(fmt.Sprintf("-- name: Delete%s :exec\n", singularName))
		b.WriteString(fmt.Sprintf("DELETE FROM %s WHERE %s = %s;\n\n", ti.Name, pk, placeholder(1, driver)))

		queries[ti.Name+".sql"] = b.String()
	}

	// List queries for FK target tables (used by relation options_query)
	for _, ti := range tables {
		for _, fk := range ti.FKs() {
			if generatedFKTargets[fk.ForeignTable] {
				continue
			}
			generatedFKTargets[fk.ForeignTable] = true
			ft := FindTableByName(tables, fk.ForeignTable)
			if ft == nil {
				continue
			}
			foreignPluralName := ToPascalCase(ft.Name)
			labelCol := FindLabelColumn(*ft)
			var b strings.Builder
			b.WriteString(fmt.Sprintf("-- name: List%s :many\n", foreignPluralName))
			if driver == "mssql" {
				b.WriteString(fmt.Sprintf("SELECT %s, %s FROM %s;\n\n",
					FindPKColumn(*ft), labelCol, ft.Name))
			} else {
				b.WriteString(fmt.Sprintf("SELECT %s, %s FROM %s ORDER BY %s;\n\n",
					FindPKColumn(*ft), labelCol, ft.Name, labelCol))
			}
			fname := ft.Name + "_options.sql"
			if _, exists := queries[fname]; !exists {
				queries[fname] = b.String()
			}
		}
	}

	return queries
}

// GenerateResourceYAML produces a go-fila resource block for one table.
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
