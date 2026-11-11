// Package yamlpos finds the source position of a key path in a YAML file (real file line numbers).
package yamlpos

import (
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Load parses file and returns its document's content node.
func Load(file string) (*yaml.Node, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	return &doc, nil
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Lookup walks path from n: a string segment selects a map key, an all-digits segment a
// sequence item. It returns the last key node (nil for an item or empty path) and the value node.
func Lookup(n *yaml.Node, path []string) (key, val *yaml.Node) {
	val = n
	for _, s := range path {
		for val != nil && val.Kind == yaml.AliasNode {
			val = val.Alias
		}
		if val == nil {
			return nil, nil
		}
		key = nil
		if isIndex(s) {
			i, _ := strconv.Atoi(s)
			if val.Kind != yaml.SequenceNode || i >= len(val.Content) {
				return nil, nil
			}
			val = val.Content[i]
			continue
		}
		if val.Kind != yaml.MappingNode {
			return nil, nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(val.Content); i += 2 {
			if val.Content[i].Value == s {
				key, next = val.Content[i], val.Content[i+1]
			}
		}
		if next == nil {
			return nil, nil
		}
		val = next
	}
	return key, val
}

// Find returns the position of path in file: a map key's own position, a list item's position,
// or the document's for an empty path.
func Find(file string, path []string) (line, col int, ok bool) {
	n, err := Load(file)
	if err != nil {
		return 0, 0, false
	}
	key, val := Lookup(n, path)
	if key != nil {
		val = key
	}
	if val == nil || val.Line == 0 {
		return 0, 0, false
	}
	return val.Line, val.Column, true
}
