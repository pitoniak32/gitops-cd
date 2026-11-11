// Package yamledit edits YAML files in place at a key path (yq-style set/delete/rename), keeping
// comments and writing 2-space indent. Used by tests to mutate fixture inputs.
package yamledit

import (
	"bytes"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

func load(file string) (*yaml.Node, *yaml.Node, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	return &doc, doc.Content[0], nil
}

func save(file string, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(file, buf.Bytes(), 0o644)
}

func isIndex(s string) bool {
	_, err := strconv.ParseUint(s, 10, 31)
	return err == nil
}

func resolve(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// child returns the node under seg in n (map key or, for sequences, index), nil if absent.
func child(n *yaml.Node, seg string) *yaml.Node {
	n = resolve(n)
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == seg {
				return n.Content[i+1]
			}
		}
	case yaml.SequenceNode:
		if isIndex(seg) {
			if i, _ := strconv.Atoi(seg); i < len(n.Content) {
				return n.Content[i]
			}
		}
	}
	return nil
}

// Set sets path to value (encoded as YAML), creating missing keys; a scalar or null on the way
// is replaced by a map (yq: `.a.b = x`).
func Set(file string, path []string, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("yamledit: empty path")
	}
	doc, root, err := load(file)
	if err != nil {
		return err
	}
	var nv yaml.Node
	if err := nv.Encode(value); err != nil {
		return err
	}
	cur := resolve(root)
	for i, seg := range path {
		last := i == len(path)-1
		next := child(cur, seg)
		if next == nil {
			if cur.Kind != yaml.MappingNode {
				return fmt.Errorf("yamledit: %v: parent is not a map", path[:i])
			}
			next = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			cur.Content = append(cur.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg}, next)
		}
		if last {
			head, line, foot := next.HeadComment, next.LineComment, next.FootComment
			*next = nv
			next.HeadComment, next.LineComment, next.FootComment = head, line, foot
			break
		}
		next = resolve(next)
		if next.Kind != yaml.MappingNode && next.Kind != yaml.SequenceNode {
			*next = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		}
		cur = next
	}
	return save(file, doc)
}

func parent(root *yaml.Node, path []string) (*yaml.Node, error) {
	cur := resolve(root)
	for _, seg := range path[:len(path)-1] {
		if cur = child(cur, seg); cur == nil {
			return nil, fmt.Errorf("yamledit: %v: not found", path)
		}
		cur = resolve(cur)
	}
	return cur, nil
}

// Del removes path; a missing path is a no-op (yq semantics).
func Del(file string, path []string) error {
	if len(path) == 0 {
		return fmt.Errorf("yamledit: empty path")
	}
	doc, root, err := load(file)
	if err != nil {
		return err
	}
	p, err := parent(root, path)
	if err != nil {
		return nil
	}
	last := path[len(path)-1]
	switch p.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(p.Content); i += 2 {
			if p.Content[i].Value == last {
				p.Content = append(p.Content[:i], p.Content[i+2:]...)
				break
			}
		}
	case yaml.SequenceNode:
		if i, err := strconv.Atoi(last); err == nil && i < len(p.Content) {
			p.Content = append(p.Content[:i], p.Content[i+1:]...)
		}
	}
	return save(file, doc)
}

// Rename changes the last key of path to newKey, keeping its value and position.
func Rename(file string, path []string, newKey string) error {
	if len(path) == 0 {
		return fmt.Errorf("yamledit: empty path")
	}
	doc, root, err := load(file)
	if err != nil {
		return err
	}
	p, err := parent(root, path)
	if err != nil || p.Kind != yaml.MappingNode {
		return fmt.Errorf("yamledit: %v: not a map key", path)
	}
	last := path[len(path)-1]
	for i := 0; i+1 < len(p.Content); i += 2 {
		if p.Content[i].Value == last {
			p.Content[i].Value = newKey
			return save(file, doc)
		}
	}
	return fmt.Errorf("yamledit: %v: not found", path)
}
