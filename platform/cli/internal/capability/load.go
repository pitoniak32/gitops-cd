package capability

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// route maps one kind of input file to its place in inputs.
type route struct {
	file string // path under the repo root, no extension; {cap} = the capability, * = a name
	kind string
}

// routes is the only place that knows the input layout. Order is the load order.
var routes = []route{
	{"teams/*", "team"},
	{"capabilities/{cap}/owners", "owners"},
	{"capabilities/{cap}/stages/*", "stage"},
	{"capabilities/{cap}/targets/*", "legacyTarget"}, // pre-dir layout: read so it fails loudly
	{"capabilities/{cap}/targets/*/target", "target"},
	{"capabilities/{cap}/targets/*/environment-variables/*", "targetEnv"},
	{"capabilities/{cap}/apps/*/app", "app"},
	{"capabilities/{cap}/apps/*/owners", "legacyAppOwners"}, // old per-app owners file (ADR 0007): read so it fails loudly
}

// file is one input YAML file.
type file struct {
	kind  string
	path  string   // relative to root, slash-separated
	names []string // the * segments: [team], [stage], [target], [target, bundle], [app]
	doc   *yaml.Node
}

func (f *file) name() string { return f.names[0] }

// inputs are a capability's files, each kind in load order.
type inputs struct {
	cap   string
	files map[string][]*file
}

func (in *inputs) of(kind string) []*file { return in.files[kind] }

func (in *inputs) find(kind, name string) *file {
	for _, f := range in.files[kind] {
		if f.names[0] == name {
			return f
		}
	}
	return nil
}

func (in *inputs) names(kind string) []string {
	var out []string
	seen := map[string]bool{}
	for _, f := range in.files[kind] {
		if !seen[f.names[0]] {
			seen[f.names[0]] = true
			out = append(out, f.names[0])
		}
	}
	return out
}

var yamlErrLine = regexp.MustCompile(`line (\d+)`)

// load reads the capability's files; teams come from teams (read once for every capability). A YAML
// syntax error is a diagnostic at the error's line.
func load(root, capName string, teams *Teams) (*inputs, []Diag, error) {
	in := &inputs{cap: capName, files: map[string][]*file{"team": teams.files}}
	var diags []Diag
	for _, r := range routes {
		if r.kind == "team" {
			continue
		}
		fs, d, err := readRoute(root, r, capName)
		if err != nil {
			return nil, nil, err
		}
		in.files[r.kind] = append(in.files[r.kind], fs...)
		diags = append(diags, d...)
	}
	return in, diags, nil
}

// readRoute reads every file matching one route.
func readRoute(root string, r route, capName string) ([]*file, []Diag, error) {
	var out []*file
	var diags []Diag
	pattern := strings.Split(strings.ReplaceAll(r.file, "{cap}", capName), "/")
	m, _ := filepath.Glob(filepath.Join(root, filepath.Join(pattern...)+".y*ml"))
	for _, p := range m {
		rel, err := filepath.Rel(root, p)
		ext := filepath.Ext(p)
		if err != nil || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		rel = filepath.ToSlash(rel)
		f := &file{kind: r.kind, path: rel}
		for i, seg := range strings.Split(strings.TrimSuffix(rel, ext), "/") {
			if pattern[i] == "*" {
				f.names = append(f.names, seg)
			}
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, nil, err
		}
		doc, d := parse(rel, b)
		if d != nil {
			diags = append(diags, *d)
			continue
		}
		f.doc = doc
		out = append(out, f)
	}
	return out, diags, nil
}

// parse is a file's top node (an empty mapping for an empty file), or its syntax error as a diagnostic.
func parse(rel string, b []byte) (*yaml.Node, *Diag) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		line := 1
		if mm := yamlErrLine.FindStringSubmatch(err.Error()); mm != nil {
			line, _ = strconv.Atoi(mm[1])
		}
		return nil, &Diag{File: rel, Line: line, Col: 1, Msg: strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		return doc.Content[0], nil
	}
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Line: 1, Column: 1}, nil
}

// Teams are the teams/ files, read once and shared (read-only) by every capability.
type Teams struct {
	files  []*file
	syntax []Diag
}

// LoadTeams reads teams/ under root.
func LoadTeams(root string) (*Teams, error) {
	for _, r := range routes {
		if r.kind == "team" {
			fs, d, err := readRoute(root, r, "")
			if err != nil {
				return nil, err
			}
			return &Teams{files: fs, syntax: d}, nil
		}
	}
	return &Teams{}, nil
}

// Lint validates the team files themselves; capabilities only check the names they reference.
func (t *Teams) Lint() []Diag {
	diags := append([]Diag{}, t.syntax...)
	for _, f := range t.files {
		r := reporter{f, &diags}
		if !nameRx.MatchString(f.name()) {
			r.at(nil, nil, badName)
		}
		r.check(teamS, nil, f.doc, nil)
	}
	return SortDedupe(diags)
}

// ErrNoCapability is returned for a capability without a directory.
var ErrNoCapability = errors.New("no capability dir")

func checkDir(root, capName string) error {
	d := filepath.Join(root, "capabilities", capName)
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return fmt.Errorf("%w %s", ErrNoCapability, d)
	}
	return nil
}

// Names lists every capability under root (capabilities/* directories), sorted.
func Names(root string) []string {
	ents, _ := os.ReadDir(filepath.Join(root, "capabilities"))
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// ---- yaml.Node access, nil-safe

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// get walks map keys from n; nil when any step is missing or not a mapping.
func get(n *yaml.Node, path ...string) *yaml.Node {
	_, v := getKV(n, path...)
	return v
}

// getKV is get, also returning the last key's node.
func getKV(n *yaml.Node, path ...string) (key, val *yaml.Node) {
	val = deref(n)
	for _, s := range path {
		if val == nil || val.Kind != yaml.MappingNode {
			return nil, nil
		}
		var next *yaml.Node
		key = nil
		for i := 0; i+1 < len(val.Content); i += 2 {
			if val.Content[i].Value == s {
				key, next = val.Content[i], val.Content[i+1]
			}
		}
		val = deref(next)
	}
	return key, val
}

type pair struct {
	k   string
	key *yaml.Node
	v   *yaml.Node
}

// pairs are a mapping's fields in file order (a repeated key keeps its first place, last value).
func pairs(n *yaml.Node) []pair {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	var out []pair
	at := map[string]int{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		p := pair{n.Content[i].Value, n.Content[i], deref(n.Content[i+1])}
		if j, ok := at[p.k]; ok {
			out[j] = p
			continue
		}
		at[p.k] = len(out)
		out = append(out, p)
	}
	return out
}

func items(n *yaml.Node) []*yaml.Node {
	n = deref(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*yaml.Node, len(n.Content))
	for i, c := range n.Content {
		out[i] = deref(c)
	}
	return out
}

func isStr(n *yaml.Node) bool  { return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!str" }
func isInt(n *yaml.Node) bool  { return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!int" }
func isBool(n *yaml.Node) bool { return n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!bool" }
func isMap(n *yaml.Node) bool  { return n != nil && n.Kind == yaml.MappingNode }
func isSeq(n *yaml.Node) bool  { return n != nil && n.Kind == yaml.SequenceNode }

// str is a string scalar's value, "" otherwise.
func str(n *yaml.Node) string {
	if isStr(n) {
		return n.Value
	}
	return ""
}

func strList(n *yaml.Node) []string {
	var out []string
	for _, i := range items(n) {
		if isStr(i) {
			out = append(out, i.Value)
		}
	}
	return out
}
