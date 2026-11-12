package capability

import (
	"strings"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/color"
)

// exampleDir prefixes every fixture example; shown once in the header instead of per field.
const exampleDir = FixtureRoot + "/capabilities/"

// SchemaYAML renders docs as annotated YAML skeletons, one document per kind, each opened by a header
// comment. examples adds each field's fixture example to its comment.
func SchemaYAML(docs []SchemaDoc, examples bool, p color.P) string {
	var b strings.Builder
	if examples {
		b.WriteString(p.Dim("# e.g. paths are under "+exampleDir) + "\n")
	}
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.WriteString(p.Dim("# "+d.Kind+": "+d.File+" — "+d.Desc) + "\n")
		y := yamlLines{p: p}
		for _, f := range d.Fields {
			schemaField(&y, f, 0, examples)
		}
		b.WriteString(y.String())
	}
	return b.String()
}

func placeholder(f FieldDoc) string {
	if len(f.Enum) > 0 {
		return strings.Join(f.Enum, "|")
	}
	switch f.Type {
	case "int", "bool":
		return "<" + f.Type + ">"
	}
	return "<str>"
}

func dictKey(name string) string {
	switch name {
	case "stages", "promoters":
		return "<stage>"
	case "apps":
		return "<app>"
	case "targets":
		return "<target>"
	case "env":
		return "<KEY>"
	}
	return "<key>"
}

func fieldComment(f FieldDoc, examples bool) string {
	var parts []string
	if f.Required {
		parts = append(parts, "required;")
	}
	if f.Pattern != "" {
		p := "/" + f.Pattern + "/"
		if f.Desc != "" {
			p += " —"
		}
		parts = append(parts, p)
	}
	if f.Desc != "" {
		parts = append(parts, f.Desc)
	}
	if len(f.Platforms) > 0 {
		parts = append(parts, "(platforms: "+strings.Join(f.Platforms, ", ")+")")
	}
	if examples && f.Example != "" {
		parts = append(parts, "(e.g. "+strings.TrimPrefix(f.Example, exampleDir)+")")
	}
	return strings.Join(parts, " ")
}

// schemaField writes f (a map field, dict, list or scalar) and what is under it.
func schemaField(y *yamlLines, f FieldDoc, indent int, ex bool) {
	name := f.Name
	if name == "*" {
		name = "<KEY>"
	}
	c := fieldComment(f, ex)
	switch f.Type {
	case "map":
		y.addC(indent, name+":", y.p.Blue(name)+":", c)
		for _, ch := range f.Fields {
			schemaField(y, ch, indent+2, ex)
		}
	case "dict":
		y.addC(indent, name+":", y.p.Blue(name)+":", c)
		if f.Elem == nil {
			return
		}
		e := *f.Elem
		e.Name = dictKey(f.Name)
		schemaField(y, e, indent+2, ex)
	case "list":
		y.addC(indent, name+":", y.p.Blue(name)+":", c)
		if f.Elem == nil {
			return
		}
		e := *f.Elem
		if e.Type == "map" {
			sub := yamlLines{p: y.p}
			for _, ch := range e.Fields {
				schemaField(&sub, ch, indent+4, ex)
			}
			if len(sub.ls) > 0 {
				sub.ls[0].text = strings.Repeat(" ", indent+2) + "- " + sub.ls[0].text[indent+4:]
				sub.ls[0].ctext = strings.Repeat(" ", indent+2) + "- " + sub.ls[0].ctext[indent+4:]
			}
			y.ls = append(y.ls, sub.ls...)
			return
		}
		y.addC(indent+2, "- "+placeholder(e), "- "+y.p.Yellow(placeholder(e)), fieldComment(e, ex))
	default:
		y.addC(indent, name+": "+placeholder(f), y.p.Blue(name)+": "+y.p.Yellow(placeholder(f)), c)
	}
}
