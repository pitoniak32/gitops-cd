package system

import (
	"reflect"
	"strings"
	"testing"
)

func docKinds(docs []SchemaDoc) []string {
	var out []string
	for _, d := range docs {
		out = append(out, d.Kind)
	}
	return out
}

func findDoc(t *testing.T, kind, path string) FieldDoc {
	t.Helper()
	for _, d := range Schemas(Default()) {
		if d.Kind != kind {
			continue
		}
		fs := d.Fields
		var cur *FieldDoc
		for _, seg := range strings.Split(path, ".") {
			cur = nil
			for i := range fs {
				if fs[i].Name == seg {
					cur = &fs[i]
				}
			}
			if cur == nil {
				t.Fatalf("%s: no field %s", kind, path)
			}
			fs = cur.Fields
		}
		return *cur
	}
	t.Fatalf("no kind %s", kind)
	return FieldDoc{}
}

func TestSchemasKinds(t *testing.T) {
	want := []string{"owners", "stage", "app", "target", "env-vars"}
	if got := docKinds(Schemas(Default())); !reflect.DeepEqual(got, want) {
		t.Fatalf("kinds = %v, want %v", got, want)
	}
	for _, d := range Schemas(Default()) {
		if d.File == "" || d.Desc == "" {
			t.Errorf("%s: File %q Desc %q", d.Kind, d.File, d.Desc)
		}
	}
}

func TestEveryFieldHasDesc(t *testing.T) {
	var missing []string
	var walk func(kind, path string, f FieldDoc)
	walk = func(kind, path string, f FieldDoc) {
		if f.Name != "" && f.Desc == "" {
			missing = append(missing, kind+":"+path+f.Name)
		}
		for _, c := range f.Fields {
			walk(kind, path+f.Name+".", c)
		}
		if f.Elem != nil {
			walk(kind, path+f.Name+".", *f.Elem)
		}
	}
	for _, d := range Schemas(Default()) {
		for _, f := range d.Fields {
			walk(d.Kind, "", f)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("fields without desc: %v", missing)
	}
}

func TestTargetPlatforms(t *testing.T) {
	for path, want := range map[string][]string{
		"config.cluster":     {"K8S"},
		"config.gcpProject":  {"CLOUD_RUN", "TERRAFORM"},
		"config.region":      {"CLOUD_RUN", "TERRAFORM"},
		"config.backend":     {"TERRAFORM"},
		"config.environment": nil,
	} {
		if got := findDoc(t, "target", path).Platforms; !reflect.DeepEqual(got, want) {
			t.Errorf("%s platforms = %v, want %v", path, got, want)
		}
	}
}

func TestFieldPaths(t *testing.T) {
	paths := map[string][]string{}
	for _, d := range Schemas(Default()) {
		paths[d.Kind] = FieldPaths(d)
	}
	for kind, want := range map[string][]string{
		"app":    {"stages.*.targets.*.env.*", "replicas"},
		"owners": {"promoters.*.[]"},
	} {
		for _, w := range want {
			if !has(paths[kind], w) {
				t.Errorf("%s paths missing %s: %v", kind, w, paths[kind])
			}
		}
	}
}

func TestTargetEnums(t *testing.T) {
	cfg := Default()
	if got := findDoc(t, "target", "config.cluster").Enum; !reflect.DeepEqual(got, cfg.clusterNames()) {
		t.Errorf("cluster enum = %v, want %v", got, cfg.clusterNames())
	}
	if got := findDoc(t, "target", "config.region").Enum; !reflect.DeepEqual(got, cfg.GcpRegions) {
		t.Errorf("region enum = %v, want %v", got, cfg.GcpRegions)
	}
}
