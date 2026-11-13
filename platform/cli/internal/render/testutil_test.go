package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"
)

// fixture copies testdata/root into a fresh temp dir that a test may mutate.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyTree(t, abs(t, "testdata/root"), root)
	return root
}

// renderFixture renders payments + search from root and the platform-level regions into a fresh
// dir (what the golden test compares).
func renderFixture(t *testing.T, root string) string {
	t.Helper()
	out := t.TempDir()
	renderTestRoot(t, root, out, "payments", "search")
	if err := Platform(repoRoot(t), out); err != nil {
		t.Fatal(err)
	}
	return out
}

// renderSystem renders one system from root into a fresh dir.
func renderSystem(t *testing.T, root, sysName string) string {
	t.Helper()
	out := t.TempDir()
	renderTestRoot(t, root, out, sysName)
	return out
}

var shared struct {
	once sync.Once
	out  string // set only after a successful render
	err  error
}

// sharedOut is the unmodified fixture render, built once and read-only for the whole package
// (tests run in parallel: never write into it).
func sharedOut(t *testing.T) string {
	t.Helper()
	shared.once.Do(func() {
		dir, err := os.MkdirTemp("", "render-shared-")
		if err == nil {
			err = renderShared(t, dir)
		}
		if err != nil {
			shared.err = err
			os.RemoveAll(dir)
			return
		}
		shared.out = dir
	})
	if shared.err != nil {
		t.Fatalf("shared render failed: %v", shared.err)
	}
	return shared.out
}

func renderShared(t *testing.T, dir string) error {
	if err := Selected(abs(t, "testdata/root"), []string{"payments", "search"}, dir, io.Discard, nil); err != nil {
		return err
	}
	return Platform(repoRoot(t), dir)
}

func TestMain(m *testing.M) {
	code := m.Run()
	if shared.out != "" {
		os.RemoveAll(shared.out)
	}
	os.Exit(code)
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// docs decodes every document of a (multi-doc) YAML or JSON file.
func docs(t *testing.T, file string) []map[string]any {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	var out []map[string]any
	for {
		var m map[string]any
		if err := dec.Decode(&m); err != nil {
			if err.Error() == "EOF" {
				return out
			}
			t.Fatalf("%s: %v", file, err)
		}
		if m != nil {
			out = append(out, m)
		}
	}
}

// find returns the first document of kind (and metadata.name, unless name is empty), or nil.
func find(ds []map[string]any, kind, name string) map[string]any {
	for _, d := range ds {
		if str(d["kind"]) == kind && (name == "" || str(get(d, "metadata.name")) == name) {
			return d
		}
	}
	return nil
}

// inProject returns one Kargo project's docs (ADR 0029): its Namespace and Project and every doc in
// its namespace.
func inProject(ds []map[string]any, ns string) []map[string]any {
	var out []map[string]any
	for _, d := range ds {
		k := str(d["kind"])
		if str(get(d, "metadata.namespace")) == ns || (k == "Namespace" || k == "Project") && str(get(d, "metadata.name")) == ns {
			out = append(out, d)
		}
	}
	return out
}

func findAll(ds []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, d := range ds {
		if str(d["kind"]) == kind {
			out = append(out, d)
		}
	}
	return out
}

func pathSegs(p string) []string {
	var segs []string
	for len(p) > 0 {
		if p[0] == '"' {
			end := strings.Index(p[1:], `"`)
			segs = append(segs, p[1:1+end])
			p = p[end+2:]
		} else {
			end := strings.Index(p, ".")
			if end < 0 {
				end = len(p)
			}
			segs = append(segs, p[:end])
			p = p[end:]
		}
		p = strings.TrimPrefix(p, ".")
	}
	return segs
}

// get walks a dot path through decoded YAML: quoted segments hold dots (metadata.labels."a.b/c"),
// numeric segments index lists (negative counts from the end). Missing = nil.
func get(v any, path string) any {
	for _, s := range pathSegs(path) {
		switch x := v.(type) {
		case map[string]any:
			v = x[s]
		case []any:
			i, err := strconv.Atoi(s)
			if err != nil {
				return nil
			}
			if i < 0 {
				i += len(x)
			}
			if i < 0 || i >= len(x) {
				return nil
			}
			v = x[i]
		default:
			return nil
		}
	}
	return v
}

// str renders a scalar the way yq prints it ("" for null/missing).
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func list(v any, path string) []any {
	l, _ := get(v, path).([]any)
	return l
}

func strs(v any, path string) []string {
	var out []string
	for _, x := range list(v, path) {
		out = append(out, str(x))
	}
	return out
}

// names lists metadata.name of every doc of kind that satisfies keep (nil = all), sorted.
func names(ds []map[string]any, kind string, keep func(map[string]any) bool) []string {
	var out []string
	for _, d := range findAll(ds, kind) {
		if keep == nil || keep(d) {
			out = append(out, str(get(d, "metadata.name")))
		}
	}
	sort.Strings(out)
	return out
}

func nodeJSON(n *yaml.Node, b *strings.Builder) {
	switch n.Kind {
	case yaml.DocumentNode:
		nodeJSON(n.Content[0], b)
	case yaml.MappingNode:
		b.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			k, _ := json.Marshal(n.Content[i].Value)
			b.Write(k)
			b.WriteByte(':')
			nodeJSON(n.Content[i+1], b)
		}
		b.WriteByte('}')
	case yaml.SequenceNode:
		b.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			nodeJSON(c, b)
		}
		b.WriteByte(']')
	case yaml.AliasNode:
		nodeJSON(n.Alias, b)
	default:
		switch n.Tag {
		case "!!int", "!!float", "!!bool":
			b.WriteString(n.Value)
		case "!!null":
			b.WriteString("null")
		default:
			s, _ := json.Marshal(n.Value)
			b.Write(s)
		}
	}
}

// orderedJSON is `yq -o=json -I=0 <path>` of a file's first document: compact JSON that keeps
// the file's key order.
func orderedJSON(t *testing.T, file, path string) string {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(mustRead(t, file)), &n); err != nil {
		t.Fatal(err)
	}
	cur := &n
	if cur.Kind == yaml.DocumentNode {
		cur = cur.Content[0]
	}
	if path != "" {
		for _, s := range pathSegs(path) {
			var next *yaml.Node
			switch cur.Kind {
			case yaml.MappingNode:
				for i := 0; i+1 < len(cur.Content); i += 2 {
					if cur.Content[i].Value == s {
						next = cur.Content[i+1]
					}
				}
			case yaml.SequenceNode:
				if i, err := strconv.Atoi(s); err == nil && i < len(cur.Content) {
					next = cur.Content[i]
				}
			}
			if next == nil {
				t.Fatalf("%s: no %s", file, path)
			}
			cur = next
		}
	}
	var b strings.Builder
	nodeJSON(cur, &b)
	return b.String()
}

func globOne(t *testing.T, pattern string) []string {
	t.Helper()
	m, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func grepLines(s string, re *regexp.Regexp) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if re.MatchString(l) {
			out = append(out, l)
		}
	}
	return out
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
