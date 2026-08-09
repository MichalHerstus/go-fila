// plugin.go
//
// Authoring API for go-fila plugins. A plugin is a separate Go module that
// imports this package, implements the Plugin interface (and optionally
// Configurer), and is loaded by go-fila at generation time: go-fila runs the
// plugin's Register/Boot against a Panel builder, then merges the JSON
// manifest of contributed resources/pages/navigation/SQL files/hook
// attachments into the config before code generation. The generated app keeps
// its zero runtime dependency on go-fila.
//
// The public types are aliases of the internal config types (which carry only
// yaml tags), so the manifest JSON round-trips via Go field names and the
// loader decodes it back into the same types.
package plugin

import (
	"fmt"
	"strings"

	"github.com/go-fila/go-fila/internal/types"
)

// Plugin is the interface a plugin module exposes via `func New() Plugin`.
// Register adds contributions to the panel builder; Boot runs after Register
// and may perform initialization or additional registration.
type Plugin interface {
	ID() string
	Register(p *Panel) error
	Boot(p *Panel) error
}

// Configurer is an optional interface implemented by plugins that want the
// YAML `config:` map from go-fila.yaml. It is detected via type assertion by
// the loader, which passes the config as a map[string]any (JSON-decoded).
// The loader injects the database driver under the reserved "driver" key
// (e.g. "postgres", "sqlite", "mssql") so plugins can emit driver-appropriate
// SQL.
type Configurer interface {
	Configure(cfg map[string]any) error
}

// Public config-type aliases so plugins (a separate module) can use the same
// struct shapes as go-fila.yaml without importing internal packages.
type (
	Resource        = types.Resource
	Page            = types.Page
	Widget          = types.Widget
	NavigationGroup = types.NavigationGroup
	NavigationItem  = types.NavigationItem
	Column          = types.Column
	Field           = types.Field
	Hook            = types.Hook
	Hooks           = types.Hooks
	ListConfig      = types.ListConfig
	DetailConfig    = types.DetailConfig
	FormConfig      = types.FormConfig
	FormAction      = types.FormAction
	CardConfig      = types.CardConfig
	Action          = types.Action
	Policy          = types.Policy
	ChartConfig     = types.ChartConfig
	Validation      = types.Validation
)

// HookAttachment declares that a hook should be appended to the before/after
// list of an existing resource's form action (create/update/delete) or custom
// action. The merge resolves it against the config's merged resource set.
type HookAttachment struct {
	Resource string // existing resource name (e.g. "Customer")
	Action   string // "create" | "update" | "delete" | <custom action name>
	When     string // "before" | "after"
	Hook     Hook
}

// Manifest is the JSON-serializable snapshot of everything a plugin
// contributes to the panel. go-fila's loader decodes it and merges the
// contributions into the config.
type Manifest struct {
	Resources       []Resource
	Pages           []Page
	Navigation      []NavigationGroup
	HookAttachments []HookAttachment
	SQLFiles        map[string]string
}

// Panel is the builder a plugin registers contributions into. It accumulates a
// Manifest and performs lightweight validation (name collisions, page paths,
// SQL file locations).
type Panel struct {
	manifest  Manifest
	resNames  map[string]bool
	pageNames map[string]bool
}

// NewPanel creates an empty panel builder.
func NewPanel() *Panel {
	return &Panel{
		manifest: Manifest{
			SQLFiles: map[string]string{},
		},
		resNames:  map[string]bool{},
		pageNames: map[string]bool{},
	}
}

// AddResource registers a resource. It returns an error when a resource with
// the same name was already added by this plugin.
func (p *Panel) AddResource(r Resource) error {
	if r.Name == "" {
		return fmt.Errorf("plugin: resource name is required")
	}
	if p.resNames[r.Name] {
		return fmt.Errorf("plugin: duplicate resource %q", r.Name)
	}
	p.resNames[r.Name] = true
	p.manifest.Resources = append(p.manifest.Resources, r)
	return nil
}

// AddPage registers a dashboard page. Path defaults to "/"+Name when empty.
// It returns an error for a missing name or a duplicate page name.
func (p *Panel) AddPage(pg Page) error {
	if pg.Name == "" {
		return fmt.Errorf("plugin: page name is required")
	}
	if p.pageNames[pg.Name] {
		return fmt.Errorf("plugin: duplicate page %q", pg.Name)
	}
	if pg.Path == "" {
		pg.Path = "/" + pg.Name
	}
	p.pageNames[pg.Name] = true
	p.manifest.Pages = append(p.manifest.Pages, pg)
	return nil
}

// AddNavigationGroup appends a sidebar group to the plugin's navigation
// contribution.
func (p *Panel) AddNavigationGroup(g NavigationGroup) {
	p.manifest.Navigation = append(p.manifest.Navigation, g)
}

// AddSQLFile contributes a SQL file into the generated project's sql/ tree.
// name must be either "queries/<file>.sql" or "migrations/<file>.sql"; the
// file is only written by go-fila when it does not already exist.
func (p *Panel) AddSQLFile(name, content string) {
	p.manifest.SQLFiles[name] = content
}

// AddHookToResource records that a hook should be attached to a resource's
// form action (create/update/delete) or custom action, before or after it.
// The target resource must exist in the merged config; the loader resolves it
// (and rejects fn hooks, which require M5). when must be "before" or "after".
func (p *Panel) AddHookToResource(resource, action, when string, h Hook) error {
	if resource == "" || action == "" {
		return fmt.Errorf("plugin: hook target resource and action are required")
	}
	if when != "before" && when != "after" {
		return fmt.Errorf("plugin: hook when must be \"before\" or \"after\"")
	}
	p.manifest.HookAttachments = append(p.manifest.HookAttachments, HookAttachment{
		Resource: resource,
		Action:   action,
		When:     when,
		Hook:     h,
	})
	return nil
}

// Manifest returns a copy of the plugin's accumulated contributions.
func (p *Panel) Manifest() Manifest {
	return p.manifest
}

// ValidateSQLFileName reports whether name is a valid SQL file contribution
// location ("queries/<file>.sql" or "migrations/<file>.sql"). It is used by
// the loader to give a helpful error before writing files.
func ValidateSQLFileName(name string) error {
	if (strings.HasPrefix(name, "queries/") || strings.HasPrefix(name, "migrations/")) &&
		strings.HasSuffix(name, ".sql") && !strings.Contains(name[strings.LastIndex(name, "/")+1:], "/") {
		return nil
	}
	return fmt.Errorf("plugin: sql file %q must be under queries/ or migrations/ with a .sql extension", name)
}
