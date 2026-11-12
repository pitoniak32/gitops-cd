// Package emit writes the renderer's output documents: ordered maps to YAML and JSON, in the exact
// formatting the rendered/ tree is committed in.
package emit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// KV is one field of a Map.
type KV struct {
	K string
	V any
}

// Map is a mapping whose fields are written in order. Values are Map, List, string, int or bool.
type Map []KV

// List is a sequence.
type List []any

// M builds a Map from alternating keys and values. A nil value (or nil Map/List) drops the field.
func M(kv ...any) Map {
	m := Map{}
	for i := 0; i+1 < len(kv); i += 2 {
		switch v := kv[i+1].(type) {
		case nil:
			continue
		case Map:
			if v == nil {
				continue
			}
		case List:
			if v == nil {
				continue
			}
		}
		m = append(m, KV{kv[i].(string), kv[i+1]})
	}
	return m
}

// Strings is a List of strings.
func Strings(s []string) List {
	l := make(List, len(s))
	for i, x := range s {
		l[i] = x
	}
	return l
}

// Get returns the value of key k, or nil.
func (m Map) Get(k string) any {
	for _, kv := range m {
		if kv.K == k {
			return kv.V
		}
	}
	return nil
}

// Strings that YAML 1.1 parsers read as something else: always double-quoted.
var legacy = map[string]bool{}

func init() {
	for _, s := range strings.Fields("y Y yes Yes YES n N t T f F no No NO true True TRUE false False FALSE on On ON off Off OFF .Nan") {
		legacy[s] = true
	}
}

var timeLike = regexp.MustCompile(`^[\-+0-9:\. \t]+([-:]|[tT])[\-+0-9:\. \t]+[zZ]?$|^0x[a-fA-F0-9]+$`)

func strNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	switch {
	case strings.Contains(s, "\n"):
		n.Style = yaml.LiteralStyle
	case legacy[s] || timeLike.MatchString(s):
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

func node(v any) *yaml.Node {
	switch x := v.(type) {
	case Map:
		n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, kv := range x {
			n.Content = append(n.Content, strNode(kv.K), node(kv.V))
		}
		return n
	case List:
		n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range x {
			n.Content = append(n.Content, node(e))
		}
		return n
	case string:
		return strNode(x)
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}
	}
	panic(fmt.Sprintf("emit: unsupported %T", v))
}

// YAML is one document.
func YAML(v any) string {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(node(v)); err != nil {
		panic(err)
	}
	enc.Close()
	return b.String()
}

// YAMLStream is the documents separated by "---".
func YAMLStream(docs []any) string {
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.WriteString(YAML(d))
	}
	return b.String()
}

func writeJSON(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case Map:
		b.WriteByte('{')
		for i, kv := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, kv.K)
			b.WriteByte(':')
			writeJSON(b, kv.V)
		}
		b.WriteByte('}')
	case List:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
		}
		b.WriteByte(']')
	case string, int, bool:
		s, err := json.Marshal(x)
		if err != nil {
			panic(err)
		}
		b.Write(s)
	default:
		panic(fmt.Sprintf("emit: unsupported %T", v))
	}
}

// JSON is the compact encoding.
func JSON(v any) string {
	var b bytes.Buffer
	writeJSON(&b, v)
	return b.String()
}

// JSONIndent is the two-space indented encoding with a trailing newline.
func JSONIndent(v any) string {
	var b bytes.Buffer
	if err := json.Indent(&b, []byte(JSON(v)), "", "  "); err != nil {
		panic(err)
	}
	b.WriteByte('\n')
	return b.String()
}
