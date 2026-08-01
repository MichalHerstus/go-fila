// field.go
//
// Documents the supported field/column types of the schema. FieldTypes maps
// each type name to a short description of how it renders in lists and forms.
package types

// FieldTypes is the registry of supported field/column types and their
// human-readable descriptions, used for documentation and validation.
var FieldTypes = map[string]string{
	"integer":  "Number column / number input",
	"string":   "Text column / text input",
	"text":     "Text column / textarea",
	"email":    "Mailto link / email input",
	"password": "Hidden / password input",
	"boolean":  "Check icon / toggle",
	"badge":    "Colored badge",
	"datetime": "Formatted date/time",
	"date":     "Date only",
	"image":    "Thumbnail",
	"file":     "Download link",
	"select":   "Dropdown (static or SQLC-backed)",
	"relation": "Link to related record",
	"json":     "Pretty-printed",
	"float":    "Decimal / number input",
}
