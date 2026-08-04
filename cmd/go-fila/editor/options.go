package editor

import "github.com/charmbracelet/huh"

var fieldTypeOptions = []huh.Option[string]{
	huh.NewOption("Integer", "integer"),
	huh.NewOption("String", "string"),
	huh.NewOption("Text", "text"),
	huh.NewOption("Email", "email"),
	huh.NewOption("Password", "password"),
	huh.NewOption("Boolean", "boolean"),
	huh.NewOption("Badge", "badge"),
	huh.NewOption("DateTime", "datetime"),
	huh.NewOption("Date", "date"),
	huh.NewOption("Image", "image"),
	huh.NewOption("File", "file"),
	huh.NewOption("Select", "select"),
	huh.NewOption("Relation", "relation"),
	huh.NewOption("JSON", "json"),
	huh.NewOption("Float", "float"),
	huh.NewOption("GPS", "gps"),
}

var widgetTypeOptions = []huh.Option[string]{
	huh.NewOption("Stat", "stat"),
	huh.NewOption("Stats Grid", "stats_grid"),
	huh.NewOption("Chart", "chart"),
	huh.NewOption("Table", "table"),
	huh.NewOption("List", "list"),
	huh.NewOption("HTML", "html"),
}

var chartTypeOptions = []huh.Option[string]{
	huh.NewOption("Line", "line"),
	huh.NewOption("Bar", "bar"),
	huh.NewOption("Pie", "pie"),
	huh.NewOption("Area", "area"),
}

var driverOptions = []huh.Option[string]{
	huh.NewOption("PostgreSQL", "postgres"),
	huh.NewOption("SQLite", "sqlite"),
}

var actionColorOptions = []huh.Option[string]{
	huh.NewOption("Success", "success"),
	huh.NewOption("Danger", "danger"),
	huh.NewOption("Warning", "warning"),
	huh.NewOption("Primary", "primary"),
	huh.NewOption("Info", "info"),
	huh.NewOption("Gray", "gray"),
}

var iconOptions = []huh.Option[string]{
	huh.NewOption("Users", "users"),
	huh.NewOption("Chart", "chart"),
	huh.NewOption("Dollar", "dollar"),
	huh.NewOption("Check", "check"),
	huh.NewOption("Cog", "cog"),
	huh.NewOption("Bell", "bell"),
	huh.NewOption("Home", "home"),
	huh.NewOption("Mail", "mail"),
	huh.NewOption("Lock", "lock"),
	huh.NewOption("Info", "info"),
	huh.NewOption("Box", "box"),
	huh.NewOption("Calendar", "calendar"),
	huh.NewOption("Camera", "camera"),
	huh.NewOption("Clock", "clock"),
	huh.NewOption("Cloud", "cloud"),
	huh.NewOption("Code", "code"),
	huh.NewOption("File", "file"),
	huh.NewOption("Folder", "folder"),
	huh.NewOption("Globe", "globe"),
	huh.NewOption("Heart", "heart"),
	huh.NewOption("Image", "image"),
	huh.NewOption("List", "list"),
	huh.NewOption("Map", "map"),
	huh.NewOption("Settings", "settings"),
	huh.NewOption("Star", "star"),
	huh.NewOption("Tag", "tag"),
	huh.NewOption("Trash", "trash"),
	huh.NewOption("Zap", "zap"),
	huh.NewOption("ShoppingCart", "shopping_cart"),
	huh.NewOption("BarChart", "bar_chart"),
}

var visibleOptions = []huh.Option[string]{
	huh.NewOption("Create Form", "create"),
	huh.NewOption("Update Form", "update"),
}

var authGuardOptions = []huh.Option[string]{
	huh.NewOption("Web (Session)", "web"),
	huh.NewOption("API (Token)", "api"),
}

var authProviderOptions = []huh.Option[string]{
	huh.NewOption("Session", "session"),
	huh.NewOption("JWT", "jwt"),
}

var loginFieldOptions = []huh.Option[string]{
	huh.NewOption("Email", "email"),
	huh.NewOption("Password", "password"),
	huh.NewOption("Name", "name"),
	huh.NewOption("Username", "username"),
}

var navItemTypeOptions = []huh.Option[string]{
	huh.NewOption("Resource", "resource"),
	huh.NewOption("Page", "page"),
	huh.NewOption("Link", "link"),
}
