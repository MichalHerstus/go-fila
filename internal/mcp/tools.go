// tools.go — the MCP tool registry (full E5 set) and their implementations.
//
// Mutating tools derive a yaml.Node tree from the current config, edit it, and
// hand the result back to commitOrError which re-parses and validates it — the
// same round-trip the `--fix` and AI edit paths use, so invalid edits are
// rejected (isError) without touching the in-memory config.
package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// toolDef describes one MCP tool.
type toolDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func strProp(desc string, enums ...string) map[string]interface{} {
	m := map[string]interface{}{"type": "string", "description": desc}
	if len(enums) > 0 {
		m["enum"] = enums
	}
	return m
}

func objProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "object", "description": desc}
}

func intProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": desc}
}

func schemaOf(props map[string]interface{}, required ...string) map[string]interface{} {
	m := map[string]interface{}{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

// toolDefs is the full v1 toolset.
func (s *Server) toolDefs() []toolDef {
	return []toolDef{
		// Lifecycle
		{Name: "validate", Description: "Run the full config health check (structural validation + schema-block references); returns errors and warnings.", InputSchema: schemaOf(nil)},
		{Name: "save", Description: "Persist the in-memory config to yaga.yaml (validates first; the previous file is backed up to <config>.bak).", InputSchema: schemaOf(nil)},
		{Name: "open", Description: "Replace the in-memory config by reading another yaga.yaml file from disk.", InputSchema: schemaOf(map[string]interface{}{
			"path": strProp("absolute or relative path to a yaga.yaml file"),
		}, "path")},
		{Name: "analyze", Description: "Return the schema/query sync analysis (tables, queries, missing references).", InputSchema: schemaOf(nil)},

		// Read
		{Name: "get_config", Description: "Return the whole config as JSON whose keys match the YAML (usable with get_value/set_value paths).", InputSchema: schemaOf(nil)},
		{Name: "get_value", Description: "Read a value by case-insensitive path: panel/name, resources/Customer/list/columns, navigation/0/items/1 (#idx, name or identity keys).", InputSchema: schemaOf(map[string]interface{}{
			"path": strProp("path to read"),
		}, "path")},
		{Name: "list_resources", Description: "List resources (name, label, icon, table).", InputSchema: schemaOf(nil)},
		{Name: "list_navigation", Description: "List the navigation groups and their items.", InputSchema: schemaOf(nil)},

		// Edit (scalar)
		{Name: "set_value", Description: "Set a single config value by case-insensitive path; the path must already exist. Validated before applying.", InputSchema: schemaOf(map[string]interface{}{
			"path":  strProp("path to the value to overwrite, e.g. panel/brand/logo"),
			"value": map[string]interface{}{"description": "new value (string, number, bool, object or array)"},
		}, "path", "value")},

		// Edit (structural)
		{Name: "add_resource", Description: "Add a resource to the config.", InputSchema: schemaOf(map[string]interface{}{
			"resource": objProp("resource object; at least name is required"),
		}, "resource")},
		{Name: "remove_resource", Description: "Remove a resource by name.", InputSchema: schemaOf(map[string]interface{}{
			"name": strProp("resource name"),
		}, "name")},
		{Name: "add_column", Description: "Append a list column to a resource's list view.", InputSchema: schemaOf(map[string]interface{}{
			"resource": strProp("resource name"),
			"column":   objProp("column object; name is required, type defaults to string"),
		}, "resource", "column")},
		{Name: "add_field", Description: "Append a field to one of a resource's create/update/detail/card sections.", InputSchema: schemaOf(map[string]interface{}{
			"resource": strProp("resource name"),
			"section":  strProp("section to add the field to", "create", "update", "detail", "card"),
			"field":    objProp("field object; at least name is required"),
		}, "resource", "section", "field")},
		{Name: "add_nav_item", Description: "Append a navigation item to a group (item.type resource|page|url plus the matching target key).", InputSchema: schemaOf(map[string]interface{}{
			"group": strProp("navigation group name"),
			"item":  objProp("item object: type + resource|page|url (+ optional label, new_tab)"),
		}, "group", "item")},
		{Name: "remove_nav_item", Description: "Remove a navigation item from a group by index or by its label/target.", InputSchema: schemaOf(map[string]interface{}{
			"group": strProp("navigation group name"),
			"index": intProp("0-based item index (alternative to item)"),
			"item":  strProp("item label or target (resource/page/url value) to remove"),
		}, "group")},

		// Edit (bulk)
		{Name: "merge_yaml_fragment", Description: "Apply a YAML fragment (top-level keys) onto the config: mappings recurse, keyed lists merge by identity key, scalars replace.", InputSchema: schemaOf(map[string]interface{}{
			"yaml": strProp("YAML fragment, e.g. `panel: {name: CRM}`"),
		}, "yaml")},
	}
}

// callTool dispatches a tool call, returning the text result and isErr.
func (s *Server) callTool(name string, args map[string]interface{}) (text string, isErr bool) {
	switch name {
	case "validate":
		return s.tValidate()
	case "save":
		text, isErr := s.tValidate()
		if isErr {
			return "save aborted — invalid config:\n" + text, true
		}
		if err := s.state.Save(); err != nil {
			return "save failed: " + err.Error(), true
		}
		return fmt.Sprintf("Written to %s (backup: %s.bak)", s.state.ConfigPath(), s.state.ConfigPath()), false
	case "open":
		return s.open(args)
	case "analyze":
		data, err := json.Marshal(s.state.Analyze(s.state.Config()))
		if err != nil {
			return "analyze failed: " + err.Error(), true
		}
		return string(data), false
	case "get_config":
		data, err := configJSON(s.state.Config())
		if err != nil {
			return "get_config failed: " + err.Error(), true
		}
		return string(data), false
	case "get_value":
		return s.getValue(args)
	case "list_resources":
		data, _ := json.Marshal(resourceList(s.state.Config()))
		return string(data), false
	case "list_navigation":
		return s.listNavigation(args)
	case "set_value":
		return s.setValue(args)
	case "merge_yaml_fragment":
		return s.mergeFragment(args)
	case "add_resource":
		return s.addResource(args)
	case "remove_resource":
		return s.removeResource(args)
	case "add_column":
		return s.addColumn(args)
	case "add_field":
		return s.addField(args)
	case "add_nav_item":
		return s.addNavItem(args)
	case "remove_nav_item":
		return s.removeNavItem(args)
	default:
		return fmt.Sprintf("unknown tool: %s", name), true
	}
}

// ---- argument helpers ----

func argString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func argMap(args map[string]interface{}, key string) map[string]interface{} {
	if v, ok := args[key]; ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

// argIndex reads an optional numeric "index" argument.
func argIndex(args map[string]interface{}) (int, bool) {
	switch v := args["index"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	}
	return 0, false
}

// ---- shared mutation plumbing ----

// configRoot renders the current config into an editable node tree.
func (s *Server) configRoot() (*yaml.Node, error) {
	data, err := yaml.Marshal(s.state.Config())
	if err != nil {
		return nil, err
	}
	return rootOf(data)
}

// rootOf parses YAML bytes into a mapping root node.
func rootOf(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return mappingOf(&doc)
}

// splitPath splits a "/"-joined path into segments, dropping empties.
func splitPath(p string) []string {
	var out []string
	for _, seg := range strings.Split(p, "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

// nodeText renders a node as its scalar value or YAML subtree text.
func nodeText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	data, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(data), "\n")
}

// commitOrError re-parses and validates mutated YAML, committing when valid.
func (s *Server) commitOrError(out []byte, okMsg string) (string, bool) {
	cfg, errs, warns := s.state.Parse(out)
	if errs == nil {
		errs = []string{}
	}
	if cfg == nil || len(errs) > 0 {
		msg := "not applied — validation errors:"
		if len(errs) == 0 {
			msg = "not applied — config could not be parsed"
		}
		if len(errs) > 0 {
			msg += "\n" + strings.Join(errs, "\n")
		}
		return msg, true
	}
	s.state.Commit(cfg)
	if len(warns) > 0 {
		return okMsg + "\nwarnings:\n" + strings.Join(warns, "\n"), false
	}
	return okMsg, false
}

// resourceList is defined in mcp.go.

// ---- lifecycle tools ----

func (s *Server) tValidate() (string, bool) {
	errs, warns := s.state.Report(s.state.Config())
	if len(errs) == 0 {
		if len(warns) == 0 {
			return "OK", false
		}
		return fmt.Sprintf("OK (%d warning(s)):\n%s", len(warns), strings.Join(warns, "\n")), false
	}
	msg := fmt.Sprintf("%d error(s):\n%s", len(errs), strings.Join(errs, "\n"))
	if len(warns) > 0 {
		msg += fmt.Sprintf("\n%d warning(s):\n%s", len(warns), strings.Join(warns, "\n"))
	}
	return msg, true
}

func (s *Server) open(args map[string]interface{}) (string, bool) {
	path := argString(args, "path")
	if path == "" {
		return "path is required", true
	}
	data, err := s.state.ReadConfigFile(path)
	if err != nil {
		return "cannot read " + path + ": " + err.Error(), true
	}
	cfg, errs, warns := s.state.Parse(data)
	if errs == nil {
		errs = []string{}
	}
	if cfg == nil || len(errs) > 0 {
		return "invalid config at " + path + ":\n" + strings.Join(errs, "\n"), true
	}
	s.state.Commit(cfg)
	msg := "Loaded " + path
	if len(warns) > 0 {
		msg += "\nwarnings:\n" + strings.Join(warns, "\n")
	}
	return msg, false
}

// ---- read tools ----

func (s *Server) getValue(args map[string]interface{}) (string, bool) {
	path := argString(args, "path")
	if path == "" {
		return "path is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	target := nodeAt(root, splitPath(path))
	if target == nil {
		return "path not found: " + path, true
	}
	return nodeText(target), false
}

func (s *Server) listNavigation(args map[string]interface{}) (string, bool) {
	cfg := s.state.Config()
	type itemView struct {
		Type          string `json:"type"`
		Resource      string `json:"resource,omitempty"`
		Page          string `json:"page,omitempty"`
		URL           string `json:"url,omitempty"`
		Label         string `json:"label,omitempty"`
		OpensInNewTab bool   `json:"opens_in_new_tab,omitempty"`
	}
	type groupView struct {
		Group string     `json:"group"`
		Icon  string     `json:"icon,omitempty"`
		Items []itemView `json:"items"`
	}
	out := make([]groupView, 0, len(cfg.Navigation))
	for _, g := range cfg.Navigation {
		gv := groupView{Group: g.Group, Icon: g.Icon, Items: make([]itemView, 0, len(g.Items))}
		for _, it := range g.Items {
			gv.Items = append(gv.Items, itemView{Type: it.Type, Resource: it.Resource, Page: it.Page, URL: it.URL, Label: it.Label, OpensInNewTab: it.OpensInNewTab})
		}
		out = append(out, gv)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return err.Error(), true
	}
	return string(data), false
}

// ---- edit tools ----

func (s *Server) setValue(args map[string]interface{}) (string, bool) {
	path := argString(args, "path")
	if path == "" {
		return "path is required", true
	}
	if _, ok := args["value"]; !ok {
		return "value is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	target := nodeAt(root, splitPath(path))
	if target == nil {
		return "path not found: " + path, true
	}
	val, err := nodeFromValue(args["value"])
	if err != nil {
		return err.Error(), true
	}
	replaceNode(target, val)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "set "+path)
}

func (s *Server) mergeFragment(args map[string]interface{}) (string, bool) {
	frag := argString(args, "yaml")
	if frag == "" {
		return "yaml is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	fragRoot, err := rootOf([]byte(frag))
	if err != nil {
		return "invalid YAML fragment: " + err.Error(), true
	}
	if err := mergeMaps(root, fragRoot); err != nil {
		return "merge failed: " + err.Error(), true
	}
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "merged fragment")
}

// resourcesSeq returns the resources sequence, creating the key when create is
// true.
func resourcesSeq(root *yaml.Node, create bool) (*yaml.Node, error) {
	if ri := mappingIndex(root, "resources"); ri >= 0 {
		seq := root.Content[ri+1]
		if seq.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("resources is not a list")
		}
		return seq, nil
	}
	if !create {
		return nil, fmt.Errorf("config has no resources")
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, scalarNode("resources", "!!str"), seq)
	return seq, nil
}

// resourceLookup resolves a resource name against the resources sequence.
func resourceLookup(root *yaml.Node, name string) (*yaml.Node, bool) {
	seq, err := resourcesSeq(root, false)
	if err != nil {
		return nil, false
	}
	for _, it := range seq.Content {
		if it.Kind == yaml.MappingNode && strings.EqualFold(mappingValue(it, "name"), name) {
			return it, true
		}
	}
	return nil, false
}

func (s *Server) addResource(args map[string]interface{}) (string, bool) {
	obj := argMap(args, "resource")
	if obj == nil {
		return "resource object is required", true
	}
	val, err := nodeFromValue(obj)
	if err != nil {
		return err.Error(), true
	}
	name := mappingValue(val, "name")
	if name == "" {
		return "resource name is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	seq, err := resourcesSeq(root, true)
	if err != nil {
		return err.Error(), true
	}
	if _, ok := resourceLookup(root, name); ok {
		return "resource already exists: " + name, true
	}
	appendSeqItem(seq, val)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "added resource "+name)
}

func (s *Server) removeResource(args map[string]interface{}) (string, bool) {
	name := argString(args, "name")
	if name == "" {
		return "name is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	seq, err := resourcesSeq(root, false)
	if err != nil {
		return err.Error(), true
	}
	idx := matchSequenceItem(seq, name)
	if idx < 0 {
		return "resource not found: " + name, true
	}
	removeSeqItem(seq, idx)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "removed resource "+name)
}

func (s *Server) addColumn(args map[string]interface{}) (string, bool) {
	resource, column := argString(args, "resource"), argMap(args, "column")
	if resource == "" || column == nil {
		return "resource and column are required", true
	}
	val, err := nodeFromValue(column)
	if err != nil {
		return err.Error(), true
	}
	if mappingValue(val, "name") == "" {
		return "column.name is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	if _, ok := resourceLookup(root, resource); !ok {
		return "resource not found: " + resource, true
	}
	seq, err := ensureSeq(root, []string{"resources", resource, "list", "columns"})
	if err != nil {
		return err.Error(), true
	}
	appendSeqItem(seq, val)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "added column to "+resource)
}

func (s *Server) addField(args map[string]interface{}) (string, bool) {
	resource, section := argString(args, "resource"), argString(args, "section")
	field := argMap(args, "field")
	if resource == "" || section == "" || field == nil {
		return "resource, section and field are required", true
	}
	var segs []string
	switch section {
	case "create":
		segs = []string{"resources", resource, "form", "create", "fields"}
	case "update":
		segs = []string{"resources", resource, "form", "update", "fields"}
	case "detail":
		segs = []string{"resources", resource, "detail", "fields"}
	case "card":
		segs = []string{"resources", resource, "card", "fields"}
	default:
		return fmt.Sprintf("unknown section %q (want create, update, detail, card)", section), true
	}
	val, err := nodeFromValue(field)
	if err != nil {
		return err.Error(), true
	}
	name := mappingValue(val, "name")
	if name == "" {
		return "field.name is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	if _, ok := resourceLookup(root, resource); !ok {
		return "resource not found: " + resource, true
	}
	seq, err := ensureSeq(root, segs)
	if err != nil {
		return err.Error(), true
	}
	for _, it := range seq.Content {
		if it.Kind == yaml.MappingNode && strings.EqualFold(mappingValue(it, "name"), name) {
			return fmt.Sprintf("field already exists in %s: %s", section, name), true
		}
	}
	fieldNode, err := nodeFromValue(field)
	if err != nil {
		return err.Error(), true
	}
	appendSeqItem(seq, fieldNode)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, fmt.Sprintf("added field %s to %s/%s", name, resource, section))
}

func (s *Server) addNavItem(args map[string]interface{}) (string, bool) {
	group, item := argString(args, "group"), argMap(args, "item")
	if group == "" || item == nil {
		return "group and item are required", true
	}
	val, err := nodeFromValue(item)
	if err != nil {
		return err.Error(), true
	}
	itType := mappingValue(val, "type")
	if itType != "resource" && itType != "page" && itType != "url" {
		return "item.type must be resource|page|url", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	seq, err := ensureSeq(root, []string{"navigation", group, "items"})
	if err != nil {
		return err.Error(), true
	}
	appendSeqItem(seq, val)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "added nav item to group "+group)
}

func (s *Server) removeNavItem(args map[string]interface{}) (string, bool) {
	group := argString(args, "group")
	if group == "" {
		return "group is required", true
	}
	root, err := s.configRoot()
	if err != nil {
		return err.Error(), true
	}
	seq, err := ensureSeq(root, []string{"navigation", group, "items"})
	if err != nil {
		return err.Error(), true
	}
	var idx int
	if i, ok := argIndex(args); ok {
		idx = i
	} else {
		item := argString(args, "item")
		if item == "" {
			return "either index or item is required", true
		}
		idx = matchSequenceItem(seq, item)
	}
	if idx < 0 || idx >= len(seq.Content) {
		return "nav item not found", true
	}
	removeSeqItem(seq, idx)
	out, err := yaml.Marshal(root)
	if err != nil {
		return err.Error(), true
	}
	return s.commitOrError(out, "removed nav item from group "+group)
}
