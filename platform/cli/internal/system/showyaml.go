package system

import (
	"strconv"
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
	"gopkg.in/yaml.v3"
)

// ynode is a tree of YAML keys built from dotted value paths.
type ynode struct {
	key   string
	kids  []*ynode
	val   *Value   // leaf
	items []*Value // block list of scalars (envFrom): Value.Path is the item
}

func (n *ynode) kid(key string) *ynode {
	for _, k := range n.kids {
		if k.key == key {
			return k
		}
	}
	k := &ynode{key: key}
	n.kids = append(n.kids, k)
	return k
}

func ysplit(p string) []string {
	p = strings.TrimPrefix(p, "config.")
	if rest, ok := strings.CutPrefix(p, "env."); ok {
		return []string{"env", rest}
	}
	return strings.Split(p, ".")
}

func (n *ynode) put(artifact string, v Value) {
	if v.Path == "artifact" {
		v.Path = artifact
	}
	if name, ok := strings.CutPrefix(v.Path, "envFrom."); ok {
		k := n.kid("envFrom")
		v.Path = name
		k.items = append(k.items, &v)
		return
	}
	cur := n
	for _, s := range ysplit(v.Path) {
		cur = cur.kid(s)
	}
	cur.val = &v
}

func yscalar(v any) string {
	b, err := yaml.Marshal(v)
	if err != nil {
		return "null"
	}
	return strings.TrimRight(string(b), "\n")
}

func ykey(k string) string { return yscalar(k) }

// srcComment is the source of a value as shown in a comment: the file relative to its system, or the note.
func srcComment(s Source) string {
	if s.none() {
		return ""
	}
	if s.File == "" {
		return s.Note
	}
	f := s.File
	if rest, ok := strings.CutPrefix(f, "systems/"); ok {
		if _, r, ok := strings.Cut(rest, "/"); ok {
			f = r
		}
	}
	return f + ":" + strconv.Itoa(s.Line)
}

func (n *ynode) write(y *yamlLines, indent int, sources bool) {
	cm := func(s Source) string {
		if sources {
			return srcComment(s)
		}
		return ""
	}
	switch {
	case n.val != nil:
		var s string
		if l, ok := n.val.Value.([]string); ok {
			el := make([]string, len(l))
			for i, e := range l {
				el[i] = yscalar(e)
			}
			s = "[" + strings.Join(el, ", ") + "]"
		} else {
			s = yscalar(n.val.Value)
		}
		y.addC(indent, ykey(n.key)+": "+s, y.p.Blue(ykey(n.key))+": "+s, cm(n.val.Source))
	default:
		y.addC(indent, ykey(n.key)+":", y.p.Blue(ykey(n.key))+":", "")
		for _, it := range n.items {
			y.add(indent+2, "- "+yscalar(it.Path), cm(it.Source))
		}
		for _, k := range n.kids {
			k.write(y, indent+2, sources)
		}
	}
}

// YAML renders r for people as YAML: per app its owners, then <stage>.<target> with the
// effective values (keys and source comments painted by p). sources appends each value's origin (file:line or note) as a comment.
func (r ShowResult) YAML(sources bool, p color.P) string {
	var order []string
	byApp := map[string][]ShowDeploy{}
	for _, d := range r.Deploys {
		if _, ok := byApp[d.App]; !ok {
			order = append(order, d.App)
		}
		byApp[d.App] = append(byApp[d.App], d)
	}
	var out strings.Builder
	for _, app := range order {
		root := &ynode{key: app}
		if ov := r.Owners[app]; len(ov) > 0 {
			o := root.kid("owners")
			for _, v := range ov {
				o.put("", v)
			}
		}
		for _, d := range byApp[app] {
			t := root.kid(d.Stage).kid(d.Target)
			art := d.artifact
			if art == "" {
				art = "image"
			}
			for _, v := range d.Values {
				t.put(art, v)
			}
		}
		y := yamlLines{p: p}
		root.write(&y, 0, sources)
		out.WriteString(y.String())
	}
	return out.String()
}
