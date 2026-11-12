package capability

import "strings"

// SchemaYAML renders docs as annotated YAML skeletons, one document per kind, each opened by a header
// comment. examples adds each field's fixture example to its comment.
func SchemaYAML(docs []SchemaDoc, examples bool) string {
	var b strings.Builder
	for i, d := range docs {
		if i > 0 {
			b.WriteString("---\n")
		}
		b.WriteString("# " + d.Kind + ": " + d.File + " — " + d.Desc + "\n")
		var y yamlLines
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
		parts = append(parts, "(e.g. "+f.Example+")")
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
		y.add(indent, name+":", c)
		for _, ch := range f.Fields {
			schemaField(y, ch, indent+2, ex)
		}
	case "dict":
		y.add(indent, name+":", c)
		if f.Elem == nil {
			return
		}
		e := *f.Elem
		e.Name = dictKey(f.Name)
		schemaField(y, e, indent+2, ex)
	case "list":
		y.add(indent, name+":", c)
		if f.Elem == nil {
			return
		}
		e := *f.Elem
		if e.Type == "map" {
			var sub yamlLines
			for _, ch := range e.Fields {
				schemaField(&sub, ch, indent+4, ex)
			}
			if len(sub.ls) > 0 {
				sub.ls[0].text = strings.Repeat(" ", indent+2) + "- " + sub.ls[0].text[indent+4:]
			}
			y.ls = append(y.ls, sub.ls...)
			return
		}
		y.add(indent+2, "- "+placeholder(e), fieldComment(e, ex))
	default:
		y.add(indent, name+": "+placeholder(f), c)
	}
}
