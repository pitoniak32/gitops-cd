package system

// FieldDoc documents one field of an input file, from its schema.
type FieldDoc struct {
	Name      string     `json:"name,omitempty"`
	Type      string     `json:"type"` // map | dict | list | str | int | bool
	Required  bool       `json:"required,omitempty"`
	Enum      []string   `json:"enum,omitempty"`
	Pattern   string     `json:"pattern,omitempty"`
	Desc      string     `json:"desc,omitempty"`
	Platforms []string   `json:"platforms,omitempty"` // target fields: the platforms that take it (none = all)
	Example   string     `json:"example,omitempty"`
	Fields    []FieldDoc `json:"fields,omitempty"` // map
	Elem      *FieldDoc  `json:"elem,omitempty"`   // dict, list: the values
}

// SchemaDoc documents one kind of input file.
type SchemaDoc struct {
	Kind   string     `json:"kind"` // owners | stage | app | target | env-vars
	File   string     `json:"file"` // path pattern under the repo root
	Desc   string     `json:"desc"`
	Fields []FieldDoc `json:"fields"`
}

func (s *schema) doc(name, desc string, required bool, platforms []string) FieldDoc {
	d := FieldDoc{Name: name, Type: s.kind, Required: required, Enum: s.enum, Desc: desc, Platforms: platforms}
	if s.re != nil {
		d.Pattern = s.re.String()
	}
	for _, f := range s.fields {
		d.Fields = append(d.Fields, f.s.doc(f.name, f.desc, f.required, f.platforms))
	}
	if s.elem != nil {
		e := s.elem.doc("", "", false, nil)
		d.Elem = &e
	}
	return d
}

// Schemas documents every input file kind. Enums that depend on the system (stages, teams) are
// left empty; the field's Desc says what it refers to.
func Schemas(cfg Config) []SchemaDoc {
	rootFields := func(s *schema) []FieldDoc {
		if s.kind == "map" {
			return s.doc("", "", false, nil).Fields
		}
		return []FieldDoc{s.elem.doc("*", "value of the variable named by the key", false, nil)} // a flat file: every key
	}
	kinds := []struct {
		kind, route, desc string
		s                 *schema
	}{
		{"owners", "owners", "who reviews, promotes and views a system (one file per system)", ownersSchema(nil, nil)},
		{"stage", "stage", "a stage of the system's promotion pipeline", stageSchema(nil)},
		{"app", "app", "an app: what it ships and its settings at app, stage and target level", appS},
		{"target", "target", "a target: where a stage deploys, reusable across apps", targetSchema(cfg)},
		{"env-vars", "targetEnv", "environment variables shared by apps that opt in with envFrom", envVarsS},
	}
	var out []SchemaDoc
	for _, k := range kinds {
		d := SchemaDoc{Kind: k.kind, Desc: k.desc, Fields: rootFields(k.s)}
		for _, r := range routes {
			if r.kind == k.route {
				d.File = r.file + ".yml"
			}
		}
		out = append(out, d)
	}
	return out
}

// FieldPaths lists every field path of d, depth-first in field order: map field = name, dict key = *,
// list item = [], joined with ".".
func FieldPaths(d SchemaDoc) []string {
	var out []string
	var walk func(prefix string, f FieldDoc)
	walk = func(prefix string, f FieldDoc) {
		p := f.Name
		if prefix != "" {
			p = prefix + "." + f.Name
		}
		out = append(out, p)
		for _, c := range f.Fields {
			walk(p, c)
		}
		if f.Elem != nil {
			e := *f.Elem
			e.Name = "*"
			if f.Type == "list" {
				e.Name = "[]"
			}
			walk(p, e)
		}
	}
	for _, f := range d.Fields {
		walk("", f)
	}
	return out
}
