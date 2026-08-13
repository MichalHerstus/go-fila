// plugin.go
//
// YAML-tagged struct describing a plugin declaration in yaga.yaml. A plugin
// extends the panel at generation time: yaga runs the plugin's source in a
// throwaway module, collects a JSON manifest of contributed resources, pages,
// navigation groups, SQL files and hook attachments, and merges it into the
// config before code generation.
package types

// PluginConfig declares one plugin to load at generation time. Source is a
// local directory ("./plugins/audit", an absolute path) or a Go module import
// path ("github.com/..."); Config is passed verbatim to the plugin's optional
// Configure method as a JSON object.
type PluginConfig struct {
	Name   string         `yaml:"name"`
	Source string         `yaml:"source"`
	Config map[string]any `yaml:"config"`
}
