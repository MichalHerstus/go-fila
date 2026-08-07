package editor

// Enumerated option sets for drop-downs and tag editors.

var fieldTypeOptions = []Option{
	{Label: "Integer", Value: "integer"},
	{Label: "String", Value: "string"},
	{Label: "Text", Value: "text"},
	{Label: "Email", Value: "email"},
	{Label: "Password", Value: "password"},
	{Label: "Boolean", Value: "boolean"},
	{Label: "Badge", Value: "badge"},
	{Label: "DateTime", Value: "datetime"},
	{Label: "Date", Value: "date"},
	{Label: "Image", Value: "image"},
	{Label: "File", Value: "file"},
	{Label: "Select", Value: "select"},
	{Label: "Relation", Value: "relation"},
	{Label: "JSON", Value: "json"},
	{Label: "Float", Value: "float"},
	{Label: "GPS", Value: "gps"},
}

var widgetTypeOptions = []Option{
	{Label: "Stat", Value: "stat"},
	{Label: "Stats Grid", Value: "stats_grid"},
	{Label: "Chart", Value: "chart"},
	{Label: "Table", Value: "table"},
	{Label: "List", Value: "list"},
	{Label: "HTML", Value: "html"},
}

var chartTypeOptions = []Option{
	{Label: "Line", Value: "line"},
	{Label: "Bar", Value: "bar"},
	{Label: "Pie", Value: "pie"},
	{Label: "Area", Value: "area"},
}

var driverOptions = []Option{
	{Label: "PostgreSQL", Value: "postgres"},
	{Label: "SQLite", Value: "sqlite"},
	{Label: "MSSQL", Value: "mssql"},
}

var actionColorOptions = []Option{
	{Label: "Success", Value: "success"},
	{Label: "Danger", Value: "danger"},
	{Label: "Warning", Value: "warning"},
	{Label: "Primary", Value: "primary"},
	{Label: "Info", Value: "info"},
	{Label: "Gray", Value: "gray"},
}

var iconOptions = []Option{
	{Label: "Users", Value: "users"},
	{Label: "Chart", Value: "chart"},
	{Label: "Dollar", Value: "dollar"},
	{Label: "Check", Value: "check"},
	{Label: "Cog", Value: "cog"},
	{Label: "Bell", Value: "bell"},
	{Label: "Home", Value: "home"},
	{Label: "Mail", Value: "mail"},
	{Label: "Lock", Value: "lock"},
	{Label: "Info", Value: "info"},
	{Label: "Box", Value: "box"},
	{Label: "Calendar", Value: "calendar"},
	{Label: "Camera", Value: "camera"},
	{Label: "Clock", Value: "clock"},
	{Label: "Cloud", Value: "cloud"},
	{Label: "Code", Value: "code"},
	{Label: "File", Value: "file"},
	{Label: "Folder", Value: "folder"},
	{Label: "Globe", Value: "globe"},
	{Label: "Heart", Value: "heart"},
	{Label: "Image", Value: "image"},
	{Label: "List", Value: "list"},
	{Label: "Map", Value: "map"},
	{Label: "Settings", Value: "settings"},
	{Label: "Star", Value: "star"},
	{Label: "Tag", Value: "tag"},
	{Label: "Trash", Value: "trash"},
	{Label: "Zap", Value: "zap"},
	{Label: "Shopping Cart", Value: "shopping_cart"},
	{Label: "Bar Chart", Value: "bar_chart"},
}

var visibleOptions = []Option{
	{Label: "Create Form", Value: "create"},
	{Label: "Update Form", Value: "update"},
}

var authGuardOptions = []Option{
	{Label: "Web (Session)", Value: "web"},
	{Label: "API (Token)", Value: "api"},
}

var authProviderOptions = []Option{
	{Label: "Session", Value: "session"},
	{Label: "JWT", Value: "jwt"},
}

var loginFieldOptions = []Option{
	{Label: "Email", Value: "email"},
	{Label: "Password", Value: "password"},
	{Label: "Name", Value: "name"},
	{Label: "Username", Value: "username"},
}

var navItemTypeOptions = []Option{
	{Label: "Resource", Value: "resource"},
	{Label: "Page", Value: "page"},
	{Label: "Link", Value: "link"},
}

var idTypeOptions = []Option{
	{Label: "int32", Value: "int32"},
	{Label: "int64", Value: "int64"},
	{Label: "int16", Value: "int16"},
}

// optionValues returns just the values of the given options.
func optionValues(opts []Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Value
	}
	return out
}
