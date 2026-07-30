package types

type Config struct {
	Version     string              `yaml:"version"`
	Panel       Panel               `yaml:"panel"`
	Connections map[string]Connection `yaml:"connections"`
	SQLC        SQLCConfig          `yaml:"sqlc"`
	Auth        AuthConfig          `yaml:"auth"`
	Navigation  []NavigationGroup   `yaml:"navigation"`
	Resources   []Resource          `yaml:"resources"`
	Pages       []Page              `yaml:"pages"`
}

type Panel struct {
	ID     string      `yaml:"id"`
	Path   string      `yaml:"path"`
	Name   string      `yaml:"name"`
	Brand  Brand       `yaml:"brand"`
	Layout Layout      `yaml:"layout"`
	Theme  Theme       `yaml:"theme"`
}

type Brand struct {
	Logo     string        `yaml:"logo"`
	Favicon  string        `yaml:"favicon"`
	Colors   BrandColors   `yaml:"colors"`
}

type BrandColors struct {
	Primary   string `yaml:"primary"`
	Secondary string `yaml:"secondary"`
}

type Layout struct {
	Sidebar         SidebarLayout `yaml:"sidebar"`
	Topbar          TopbarLayout  `yaml:"topbar"`
	MaxContentWidth string        `yaml:"max_content_width"`
}

type SidebarLayout struct {
	Collapsible    bool `yaml:"collapsible"`
	Width          int  `yaml:"width"`
	CollapsedWidth int  `yaml:"collapsed_width"`
}

type TopbarLayout struct {
	Sticky bool `yaml:"sticky"`
}

type Theme struct {
	DarkMode bool   `yaml:"dark_mode"`
	Font     Font   `yaml:"font"`
}

type Font struct {
	Family string `yaml:"family"`
	Mono   string `yaml:"mono"`
}

type Connection struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
	Pool   PoolConfig `yaml:"pool"`
}

type PoolConfig struct {
	MaxOpen  int    `yaml:"max_open"`
	MaxIdle  int    `yaml:"max_idle"`
	Lifetime string `yaml:"lifetime"`
}

type SQLCConfig struct {
	Config     string `yaml:"config"`
	QueriesDir string `yaml:"queries_dir"`
	SchemaDir  string `yaml:"schema_dir"`
	OutputPkg  string `yaml:"output_pkg"`
}

type AuthConfig struct {
	Guard         string      `yaml:"guard"`
	Provider      string      `yaml:"provider"`
	Table         string      `yaml:"table"`
	Login         LoginConfig `yaml:"login"`
	Registration  bool        `yaml:"registration"`
	PasswordReset bool        `yaml:"password_reset"`
	RememberMe    bool        `yaml:"remember_me"`
}

type LoginConfig struct {
	Fields   []string `yaml:"fields"`
	Redirect string   `yaml:"redirect"`
}

type NavigationGroup struct {
	Group string             `yaml:"group"`
	Icon  string             `yaml:"icon"`
	Sort  int                `yaml:"sort"`
	Items []NavigationItem   `yaml:"items"`
}

type NavigationItem struct {
	Resource      string `yaml:"resource"`
	Page          string `yaml:"page"`
	Type          string `yaml:"type"`
	Label         string `yaml:"label"`
	URL           string `yaml:"url"`
	OpensInNewTab bool   `yaml:"opens_in_new_tab"`
}
