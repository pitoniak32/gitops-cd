package emit

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func fromNode(t *testing.T, n *yaml.Node) any {
	switch n.Kind {
	case yaml.DocumentNode:
		return fromNode(t, n.Content[0])
	case yaml.MappingNode:
		m := Map{}
		for i := 0; i < len(n.Content); i += 2 {
			m = append(m, KV{n.Content[i].Value, fromNode(t, n.Content[i+1])})
		}
		return m
	case yaml.SequenceNode:
		l := List{}
		for _, c := range n.Content {
			l = append(l, fromNode(t, c))
		}
		return l
	case yaml.ScalarNode:
		switch n.Tag {
		case "!!str":
			return n.Value
		case "!!int":
			i, err := strconv.Atoi(n.Value)
			if err != nil {
				t.Fatal(err)
			}
			return i
		case "!!bool":
			return n.Value == "true"
		}
	}
	t.Fatalf("unsupported node %v %s", n.Kind, n.Tag)
	return nil
}

func fromJSON(t *testing.T, dec *json.Decoder) any {
	tok, err := dec.Token()
	if err != nil {
		t.Fatal(err)
	}
	switch x := tok.(type) {
	case json.Delim:
		if x == '{' {
			m := Map{}
			for dec.More() {
				k, _ := dec.Token()
				m = append(m, KV{k.(string), fromJSON(t, dec)})
			}
			dec.Token()
			return m
		}
		l := List{}
		for dec.More() {
			l = append(l, fromJSON(t, dec))
		}
		dec.Token()
		return l
	case json.Number:
		i, _ := strconv.Atoi(x.String())
		return i
	}
	return tok
}

// Every golden file re-encodes to itself: the emitter writes the committed formatting.
func TestGoldenRoundTrip(t *testing.T) {
	t.Parallel()
	golden, _ := filepath.Abs("../../../render/testdata/golden")
	n := 0
	err := filepath.Walk(golden, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		var got string
		switch filepath.Ext(p) {
		case ".yaml":
			var docs []any
			dec := yaml.NewDecoder(bytes.NewReader(b))
			for {
				var d yaml.Node
				if err := dec.Decode(&d); err == io.EOF {
					break
				} else if err != nil {
					return err
				}
				docs = append(docs, fromNode(t, &d))
			}
			got = YAMLStream(docs)
		case ".json":
			dec := json.NewDecoder(bytes.NewReader(b))
			dec.UseNumber()
			v := fromJSON(t, dec)
			if strings.Contains(string(b), "\n  ") {
				got = JSONIndent(v)
			} else {
				got = JSON(v) + "\n"
			}
		default:
			return nil
		}
		n++
		if got != string(b) {
			rel, _ := filepath.Rel(golden, p)
			t.Errorf("%s does not round-trip", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n < 50 {
		t.Fatalf("only %d golden files checked", n)
	}
}
