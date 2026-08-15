// Package fixer implements the auto-repair engine behind `yaga validate --fix`
// and the Fix buttons in the TUI and web editors. Known-fixable validation
// problems are corrected by editing the yaml.v3 node tree directly — the same
// round-trip the AI edit path uses — so only the offending leaves change and
// config defaults are never injected back into the file. Callers that persist
// the result are responsible for any backup of the original.
package fixer

import (
	"fmt"

	"github.com/MichalHerstus/yaga/internal/parser"
	"github.com/MichalHerstus/yaga/internal/types"
	"gopkg.in/yaml.v3"
)

// fixers is the ordered list of registered fixers. The repair loop
// applies every fixer each pass and stops when a pass changes nothing or the
// config validates cleanly.
var fixers = []func(root *yaml.Node, fixed *[]string){
	fixEmptyFilters,
}

// Apply runs the registered fixers against YAML config bytes until it
// validates cleanly (ignoring non-fatal parser warnings) or no fixer makes
// progress. It never touches the filesystem.
//
// Returns the (possibly repaired) config bytes, the dotted paths that were
// fixed (e.g. "resources/Category/list.filter"), the remaining unfixable
// validation errors (nil when the result is valid), and an error only when
// the YAML cannot be parsed at all. When at least one fix applied but
// unparsable/remaining errors still exist, the returned bytes carry the
// partial repair so callers can persist what they can.
func Apply(data []byte) (out []byte, fixed []string, remaining []error, err error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("parsing yaml: %w", err)
	}
	root, err := mappingOf(&doc)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parsing yaml: %w", err)
	}
	cur := data
	for {
		var cfg types.Config
		if err := yaml.Unmarshal(cur, &cfg); err != nil {
			return nil, fixed, nil, fmt.Errorf("parsing yaml: %w", err)
		}
		var problems []error
		for _, e := range parser.ValidateAll(&cfg) {
			if _, ok := e.(parser.Warning); !ok {
				problems = append(problems, e)
			}
		}
		if len(problems) == 0 {
			return cur, fixed, nil, nil
		}
		before := len(fixed)
		for _, fix := range fixers {
			fix(root, &fixed)
		}
		if len(fixed) == before {
			return cur, fixed, problems, nil
		}
		out, err := yaml.Marshal(root)
		if err != nil {
			return nil, fixed, nil, fmt.Errorf("encoding yaml: %w", err)
		}
		cur = out
	}
}

// mappingOf unwraps document nodes and requires the result to be a mapping.
func mappingOf(n *yaml.Node) (*yaml.Node, error) {
	for n.Kind == yaml.DocumentNode {
		if len(n.Content) != 1 {
			return nil, fmt.Errorf("expected a single YAML document")
		}
		n = n.Content[0]
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected a YAML mapping")
	}
	return n, nil
}

// mappingIndex returns the index of key within a mapping node's Content
// (key/value pairs), or -1 when absent.
func mappingIndex(n *yaml.Node, key string) int {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// mappingValue returns the scalar value for key in a mapping node, or "".
func mappingValue(n *yaml.Node, key string) string {
	if i := mappingIndex(n, key); i >= 0 {
		return n.Content[i+1].Value
	}
	return ""
}

// fixEmptyFilters removes inert filter blocks from list/card views: a filter
// present in the YAML but with no label, where or params carries no user
// intent and only trips the "where is required" validation error. It is
// replaced with "no filter" (the key is dropped from the node tree). A filter
// with a label or a where expression is left untouched — it is not
// auto-fixable.
func fixEmptyFilters(root *yaml.Node, fixed *[]string) {
	ri := mappingIndex(root, "resources")
	if ri < 0 {
		return
	}
	ress := root.Content[ri+1]
	if ress.Kind != yaml.SequenceNode {
		return
	}
	for _, res := range ress.Content {
		if res.Kind != yaml.MappingNode {
			continue
		}
		name := mappingValue(res, "name")
		for _, view := range []string{"list", "card"} {
			vi := mappingIndex(res, view)
			if vi < 0 {
				continue
			}
			viewNode := res.Content[vi+1]
			if viewNode.Kind != yaml.MappingNode {
				continue
			}
			fi := mappingIndex(viewNode, "filter")
			if fi < 0 {
				continue
			}
			filterNode := viewNode.Content[fi+1]
			if filterNode.Kind != yaml.MappingNode {
				continue
			}
			var fc types.FilterConfig
			if err := filterNode.Decode(&fc); err != nil {
				continue
			}
			if fc.Label != "" || fc.Where != "" || len(fc.Params) > 0 {
				continue
			}
			viewNode.Content = append(viewNode.Content[:fi], viewNode.Content[fi+2:]...)
			*fixed = append(*fixed, fmt.Sprintf("resources/%s/%s.filter", name, view))
		}
	}
}
