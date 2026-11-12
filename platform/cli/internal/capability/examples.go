package capability

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// FixtureRoot is the repo-relative fixture that documents every field by example.
const FixtureRoot = "platform/render/testdata/root"

// Examples finds, for every field path of every kind (see FieldPaths), the first place the fixture under
// root uses it: key "kind/path", value "<FixtureRoot>/<file>:<line>". Files are walked in routes order,
// then by path. A missing root yields no examples.
func Examples(root string) (map[string]string, error) {
	out := map[string]string{}
	for _, d := range Schemas(Default()) {
		for _, r := range routes {
			if r.file+".yml" != d.File {
				continue
			}
			pat := strings.ReplaceAll(r.file, "{cap}", "*")
			m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pat)+".yml"))
			for _, p := range m {
				b, err := os.ReadFile(p)
				if err != nil {
					return nil, err
				}
				doc, diag := parse(p, b)
				if diag != nil {
					continue // lint reports syntax errors
				}
				rel, _ := filepath.Rel(root, p)
				loc := FixtureRoot + "/" + filepath.ToSlash(rel) + ":"
				record := func(path string, line int) {
					k := d.Kind + "/" + path
					if _, ok := out[k]; !ok {
						out[k] = loc + strconv.Itoa(line)
					}
				}
				walkExamples(doc, "", d.Fields, nil, "map", record)
			}
		}
	}
	return out, nil
}

// walkExamples follows n along the schema: fields (map), elem (dict, list values).
func walkExamples(n *yaml.Node, prefix string, fields []FieldDoc, elem *FieldDoc, typ string, record func(string, int)) {
	join := func(name string) string {
		if prefix == "" {
			return name
		}
		return prefix + "." + name
	}
	switch {
	case n.Kind == yaml.MappingNode && (typ == "map" || typ == "dict"):
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			var f *FieldDoc
			if typ == "dict" {
				f = elem
			} else {
				for j := range fields {
					if fields[j].Name == k.Value || fields[j].Name == "*" {
						f = &fields[j]
						break
					}
				}
			}
			if f == nil {
				continue
			}
			name := f.Name
			if typ == "dict" {
				name = "*"
			}
			p := join(name)
			record(p, k.Line)
			walkExamples(v, p, f.Fields, f.Elem, f.Type, record)
		}
	case n.Kind == yaml.SequenceNode && typ == "list" && elem != nil:
		for _, it := range n.Content {
			p := join("[]")
			record(p, it.Line)
			walkExamples(it, p, elem.Fields, elem.Elem, elem.Type, record)
		}
	}
}

// WithExamples returns docs with each field's Example set from ex (keys as Examples makes them).
func WithExamples(docs []SchemaDoc, ex map[string]string) []SchemaDoc {
	out := make([]SchemaDoc, len(docs))
	for i, d := range docs {
		d.Fields = withEx(d.Kind, "", d.Fields, ex)
		out[i] = d
	}
	return out
}

func withEx(kind, prefix string, fs []FieldDoc, ex map[string]string) []FieldDoc {
	out := make([]FieldDoc, len(fs))
	for i, f := range fs {
		out[i] = withExField(kind, prefix, f, ex)
	}
	return out
}

func withExField(kind, prefix string, f FieldDoc, ex map[string]string) FieldDoc {
	p := f.Name
	if prefix != "" {
		p = prefix + "." + f.Name
	}
	f.Example = ex[kind+"/"+p]
	f.Fields = withEx(kind, p, f.Fields, ex)
	if f.Elem != nil {
		e := *f.Elem
		e.Name = "*"
		if f.Type == "list" {
			e.Name = "[]"
		}
		e = withExField(kind, p, e, ex)
		e.Name = ""
		f.Elem = &e
	}
	return f
}
