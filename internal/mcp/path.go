// path.go — yaml.Node path navigation + edit helpers for the MCP tools.
//
// Paths use the same case-insensitive grammar as the TUI editor: mapping keys by
// name, sequence items by identity key ("name" for resources/pages/columns/
// fields, "group" for navigation groups, resource/page/url for navigation
// items) or by index ("0", "#1"). Mutations operate on the raw yaml.Node tree
// (the same surgical round-trip the --fix and AI path use); callers re-parse and
// ValidateAll the result before committing, so config defaults are never
// injected.
package mcp

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// identityKeyPreference is the order sequence-item matching tries scalar keys.
var identityKeyPreference = []string{
	"name", "group", "label", "resource", "page", "url",
}

// mappingOf unwraps document nodes and requires a mapping root.
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

// mappingIndex returns the index of key within a mapping's Content, or -1.
func mappingIndex(n *yaml.Node, key string) int {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return i
		}
	}
	return -1
}

// findMappingKey returns the mapping value node for key (exact, then
// case-insensitive), or nil.
func findMappingKey(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if strings.EqualFold(m.Content[i].Value, key) {
			return m.Content[i+1]
		}
	}
	return nil
}

// mappingValue returns the scalar value for key, or "".
func mappingValue(n *yaml.Node, key string) string {
	if v := findMappingKey(n, key); v != nil {
		return v.Value
	}
	return ""
}

// isIndexSegment reports whether seg is a bare integer ("3" or "#3").
func isIndexSegment(seg string) bool {
	if strings.HasPrefix(seg, "#") {
		seg = seg[1:]
	}
	_, err := strconv.Atoi(seg)
	return err == nil
}

// numIndex returns the integer of an index segment, or -1.
func numIndex(seg string) int {
	if strings.HasPrefix(seg, "#") {
		seg = seg[1:]
	}
	n, err := strconv.Atoi(seg)
	if err != nil {
		return -1
	}
	return n
}

// matchSequenceItem returns the index of the sequence item matching seg by
// identity key or any equal scalar leaf, or -1.
func matchSequenceItem(seq *yaml.Node, seg string) int {
	for i, it := range seq.Content {
		if it.Kind != yaml.MappingNode {
			continue
		}
		for _, pref := range identityKeyPreference {
			if v := mappingValue(it, pref); v != "" && strings.EqualFold(v, seg) {
				return i
			}
		}
		for j := 0; j+1 < len(it.Content); j += 2 {
			val := it.Content[j+1]
			if val.Kind == yaml.ScalarNode && strings.EqualFold(val.Value, seg) {
				return i
			}
		}
	}
	return -1
}

// seqIndex resolves a sequence-item segment ("idx", "#idx" or identity match).
func seqIndex(seq *yaml.Node, seg string) int {
	if isIndexSegment(seg) {
		n := numIndex(seg)
		if n >= 0 && n < len(seq.Content) {
			return n
		}
		return -1
	}
	return matchSequenceItem(seq, seg)
}

// nodeAt resolves segs under root and returns the node, or nil.
func nodeAt(root *yaml.Node, segs []string) *yaml.Node {
	cur := root
	for _, seg := range segs {
		if cur == nil {
			return nil
		}
		switch cur.Kind {
		case yaml.MappingNode:
			cur = findMappingKey(cur, seg)
		case yaml.SequenceNode:
			idx := seqIndex(cur, seg)
			if idx < 0 || idx >= len(cur.Content) {
				return nil
			}
			cur = cur.Content[idx]
		default:
			return nil
		}
	}
	return cur
}

// isNullNode reports whether n is the placeholder null scalar yaml.v3 emits for
// nil pointer fields when marshaling the typed config.
func isNullNode(n *yaml.Node) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!null"
}

// ensureCell walks (and creates) a path, returning the terminal node. Creates
// mapping keys and (when the next segment is index-like) sequences along the
// way; a pre-existing `null` placeholder (yaml.v3 emits nil fields as null) is
// replaced by the needed container. Numeric/identity segments must already
// exist.
func ensureCell(root *yaml.Node, segs []string) (*yaml.Node, error) {
	if len(segs) == 0 {
		return root, nil
	}
	seg := segs[0]
	switch root.Kind {
	case yaml.MappingNode:
		v := findMappingKey(root, seg)
		if v == nil {
			v = newContainer(containerKind(segs))
			root.Content = append(root.Content, scalarNode(seg, "!!str"), v)
		} else if isNullNode(v) {
			*v = *newContainer(containerKind(segs))
		}
		return ensureCell(v, segs[1:])
	case yaml.SequenceNode:
		idx := seqIndex(root, seg)
		if idx < 0 {
			return nil, fmt.Errorf("sequence item %q not found at %s", seg, strings.Join(segs, "/"))
		}
		return ensureCell(root.Content[idx], segs[1:])
	}
	return nil, fmt.Errorf("unexpected scalar node at %s", strings.Join(segs, "/"))
}

// containerKind decides mapping vs sequence for a created node: a sequence is
// used when the NEXT path segment is index-like.
func containerKind(segs []string) (kind yaml.Kind) {
	if len(segs) > 1 && isIndexSegment(segs[1]) {
		return yaml.SequenceNode
	}
	return yaml.MappingNode
}

// newContainer returns an empty typed node for a kind.
func newContainer(kind yaml.Kind) *yaml.Node {
	n := &yaml.Node{Kind: kind}
	if kind == yaml.MappingNode {
		n.Tag = "!!map"
	} else {
		n.Tag = "!!seq"
	}
	return n
}

// scalarNode builds a scalar node.
func scalarNode(value, tag string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
}

// nodeFromValue decodes a JSON-ish value into a yaml node (mappings/sequences
// for objects/arrays, scalars otherwise). Returns nil for a nil value.
func nodeFromValue(v interface{}) (*yaml.Node, error) {
	if v == nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, err
	}
	for n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		n = *n.Content[0]
	}
	return &n, nil
}

// replaceNode copies src into dst in place, keeping dst's tree position.
func replaceNode(dst, src *yaml.Node) {
	*dst = *cloneNode(src)
}

// cloneNode deep-copies a yaml node.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = make([]*yaml.Node, len(n.Content))
	for i, cn := range n.Content {
		c.Content[i] = cloneNode(cn)
	}
	return &c
}

// appendSeqItem appends item to a sequence node.
func appendSeqItem(seq *yaml.Node, item *yaml.Node) {
	seq.Content = append(seq.Content, item)
}

// removeSeqItem splices out the item at idx.
func removeSeqItem(seq *yaml.Node, idx int) {
	seq.Content = append(seq.Content[:idx], seq.Content[idx+1:]...)
}

// ensureSeq returns the sequence node at segs, creating the key and any
// missing mapping containers along the way. The terminal key always becomes a
// sequence (used by add_column/add_field/add_nav_item).
func ensureSeq(root *yaml.Node, segs []string) (*yaml.Node, error) {
	if len(segs) == 0 {
		return nil, fmt.Errorf("ensureSeq: empty path")
	}
	parent, err := ensureCell(root, segs[:len(segs)-1])
	if err != nil {
		return nil, err
	}
	if parent.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("cannot create %q: parent is not a mapping", segs[len(segs)-1])
	}
	key := segs[len(segs)-1]
	if v := findMappingKey(parent, key); v != nil {
		if v.Kind == yaml.SequenceNode {
			return v, nil
		}
		if isNullNode(v) {
			*v = *newContainer(yaml.SequenceNode)
			return v, nil
		}
		return nil, fmt.Errorf("%s is not a list", strings.Join(segs, "/"))
	}
	v := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	parent.Content = append(parent.Content, scalarNode(key, "!!str"), v)
	return v, nil
}

// merge helpers for merge_yaml_fragment (top-level mapping overlay). Fragments
// use the same merge semantics as the AI edit path: mappings recurse, keyed
// sequences merge item-by-item by identity key, other values replace; a null
// fragment value leaves the target untouched.
func mergeMaps(dst, src *yaml.Node) error {
	for i := 0; i < len(src.Content); i += 2 {
		key := src.Content[i].Value
		if j := mappingIndex(dst, key); j >= 0 {
			if err := mergeValue(dst.Content[j+1], src.Content[i+1], key); err != nil {
				return err
			}
		} else {
			dst.Content = append(dst.Content, cloneNode(src.Content[i]), cloneNode(src.Content[i+1]))
		}
	}
	return nil
}

func mergeValue(dst, src *yaml.Node, key string) error {
	if src.Tag == "!!null" {
		return nil // null leaves the target untouched
	}
	if dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode {
		return mergeMaps(dst, src)
	}
	if dst.Kind == yaml.SequenceNode && src.Kind == yaml.SequenceNode {
		return mergeSeqs(dst, src, key)
	}
	replaceNode(dst, src)
	return nil
}

// identityKeysFor returns the identity keys for a keyed sequence.
func identityKeysFor(key string) []string {
	switch key {
	case "resources", "pages", "fields", "actions", "columns":
		return []string{"name"}
	case "navigation":
		return []string{"group"}
	case "items":
		return []string{"resource", "page", "url"}
	}
	return nil
}

func allMappings(items []*yaml.Node) bool {
	for _, it := range items {
		if it.Kind != yaml.MappingNode {
			return false
		}
	}
	return true
}

func itemKey(item *yaml.Node, keys []string) string {
	if item.Kind != yaml.MappingNode {
		return ""
	}
	for _, k := range keys {
		if v := mappingValue(item, k); v != "" {
			return strings.ToLower(v)
		}
	}
	return ""
}

func mergeSeqs(dst, src *yaml.Node, key string) error {
	keys := identityKeysFor(key)
	if len(keys) == 0 || !allMappings(dst.Content) || !allMappings(src.Content) {
		replaceNode(dst, src)
		return nil
	}
	for _, s := range src.Content {
		sk := itemKey(s, keys)
		j := -1
		for i, d := range dst.Content {
			if itemKey(d, keys) == sk {
				j = i
				break
			}
		}
		if j >= 0 {
			if err := mergeMaps(dst.Content[j], s); err != nil {
				return err
			}
		} else {
			dst.Content = append(dst.Content, cloneNode(s))
		}
	}
	return nil
}
