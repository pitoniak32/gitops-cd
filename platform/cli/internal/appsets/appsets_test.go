package appsets

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/repo"
)

const goldenParams = "platform/render/testdata/golden/argocd/deploys/dc1/payments/my-app-dc1-prd-k8s.json"

const wantRegion = `{{index .metadata.labels "example.com/region"}}`

// ---- helpers ----

func load(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return v, nil
}

// get walks map keys; "[]" is not supported, use list().
func get(v any, path ...string) any {
	for _, k := range path {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func list(v any) []any { l, _ := v.([]any); return l }

func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// walk visits every map under v (yq `..` for maps).
func walk(v any, fn func(map[string]any)) {
	switch t := v.(type) {
	case map[string]any:
		fn(t)
		for _, c := range t {
			walk(c, fn)
		}
	case []any:
		for _, c := range t {
			walk(c, fn)
		}
	}
}

func glob(root, pattern string) []string {
	m, _ := filepath.Glob(filepath.Join(root, pattern))
	sort.Strings(m)
	return m
}

func yamlFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".yaml") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func rel(root, p string) string { r, _ := filepath.Rel(root, p); return r }

// sources returns spec.template.spec.sources.
func sources(doc any) []any { return list(get(doc, "spec", "template", "spec", "sources")) }

func pin(root, addon string) (string, error) {
	doc, err := load(filepath.Join(root, "platform/appsets/addons", addon+".yaml"))
	if err != nil {
		return "", err
	}
	for _, s := range sources(doc) {
		if m, ok := s.(map[string]any); ok {
			if _, has := m["chart"]; has {
				return str(m["targetRevision"]), nil
			}
		}
	}
	return "", nil
}

// ---- checks: each returns failure messages for its rule ----

type rule struct {
	name  string
	check func(root string) []string
}

var rules = []rule{
	{"addon-has-appset", checkAddonAppSets},
	{"no-inlined-app", checkNoAppYAML},
	{"no-shard-placeholder", checkNoShard},
	{"appset-basics", checkAppSetBasics},
	{"retry-unlimited", checkRetry},
	{"region-selector", checkRegion},
	{"projects-allow-empty", checkAllowEmpty},
	{"chart-first", checkChartFirst},
	{"pins", checkPins},
	{"root-app", checkRootApp},
	{"workloads-keys", checkWorkloadsKeys},
	{"rendered-revision", checkRenderedRevision},
}

func checkAddonAppSets(root string) []string {
	var f []string
	for _, d := range glob(root, "platform/infrastructure/*") {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			continue
		}
		a := filepath.Base(d)
		if _, err := os.Stat(filepath.Join(root, "platform/appsets/addons", a+".yaml")); err != nil {
			f = append(f, fmt.Sprintf("no platform/appsets/addons/%s.yaml for %s/", a, rel(root, d)))
		}
	}
	return f
}

func checkNoAppYAML(root string) []string {
	if len(glob(root, "platform/infrastructure/*/app.yaml")) > 0 {
		return []string{"platform/infrastructure/*/app.yaml must be gone (AppSets replace them)"}
	}
	return nil
}

func checkNoShard(root string) []string {
	var f []string
	self, _ := filepath.Abs("appsets_test.go")
	_ = filepath.WalkDir(filepath.Join(root, "platform"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); n == ".bin" || n == "vendor" || n == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if abs, _ := filepath.Abs(p); abs == self {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), "__SHARD__") {
			f = append(f, rel(root, p)+": __SHARD__ templating left (use {{.values.region}} / {{.region}})")
		}
		return nil
	})
	return f
}

func checkAppSetBasics(root string) []string {
	var f []string
	for _, p := range yamlFiles(filepath.Join(root, "platform/appsets")) {
		n := rel(root, p)
		doc, err := load(p)
		if err != nil {
			f = append(f, err.Error())
			continue
		}
		if str(get(doc, "kind")) != "ApplicationSet" {
			f = append(f, n+": not an ApplicationSet")
			continue
		}
		if get(doc, "spec", "goTemplate") != true {
			f = append(f, n+": goTemplate must be true")
		}
		if o := list(get(doc, "spec", "goTemplateOptions")); len(o) != 1 || str(o[0]) != "missingkey=error" {
			f = append(f, n+": goTemplateOptions must be [missingkey=error]")
		}
		if get(doc, "spec", "syncPolicy", "preserveResourcesOnDeletion") != true {
			f = append(f, n+": syncPolicy.preserveResourcesOnDeletion must be true")
		}
		if md, _ := get(doc, "spec", "template", "metadata").(map[string]any); md != nil {
			if _, has := md["finalizers"]; has {
				f = append(f, n+": template must not set finalizers")
			}
		}
		for _, s := range sources(doc) {
			for _, v := range list(get(s, "helm", "valueFiles")) {
				vf := strings.TrimPrefix(str(v), "$values/")
				if _, err := os.Stat(filepath.Join(root, vf)); err != nil {
					f = append(f, fmt.Sprintf("%s: valueFile %s missing", n, str(v)))
				}
			}
		}
	}
	return f
}

func checkRetry(root string) []string {
	var f []string
	for _, p := range glob(root, "platform/appsets/addons/*.yaml") {
		doc, err := load(p)
		if err != nil {
			f = append(f, err.Error())
			continue
		}
		if str(get(doc, "spec", "template", "spec", "syncPolicy", "retry", "limit")) != "-1" {
			f = append(f, rel(root, p)+": syncPolicy.retry.limit must be -1 (unlimited)")
		}
	}
	return f
}

func checkRegion(root string) []string {
	var f []string
	for _, p := range yamlFiles(filepath.Join(root, "platform/appsets")) {
		b, _ := os.ReadFile(p)
		if !strings.Contains(string(b), "example.com/role: region") {
			continue
		}
		n := rel(root, p)
		doc, err := load(p)
		if err != nil {
			f = append(f, err.Error())
			continue
		}
		found := 0
		walk(doc, func(m map[string]any) {
			for _, e := range list(m["matchExpressions"]) {
				if str(get(e, "key")) == "example.com/region" && str(get(e, "operator")) == "Exists" {
					found++
				}
			}
		})
		if found < 1 {
			f = append(f, n+": region cluster selector needs matchExpressions [{key: example.com/region, operator: Exists}]")
		}
		walk(doc, func(m map[string]any) {
			if v := get(m, "values", "region"); v != nil && str(v) != wantRegion {
				f = append(f, fmt.Sprintf("%s: values.region must be '%s', got '%s'", n, wantRegion, str(v)))
			}
		})
	}
	return f
}

func checkAllowEmpty(root string) []string {
	doc, err := load(filepath.Join(root, "platform/appsets/projects.yaml"))
	if err != nil {
		return []string{err.Error()}
	}
	if get(doc, "spec", "template", "spec", "syncPolicy", "automated", "allowEmpty") != true {
		return []string{"projects.yaml: automated.allowEmpty must be true"}
	}
	return nil
}

func checkChartFirst(root string) []string {
	var f []string
	for _, p := range glob(root, "platform/appsets/addons/*.yaml") {
		doc, err := load(p)
		if err != nil {
			f = append(f, err.Error())
			continue
		}
		src := sources(doc)
		hasChart := false
		for _, s := range src {
			if m, ok := s.(map[string]any); ok {
				if _, has := m["chart"]; has {
					hasChart = true
				}
			}
		}
		if !hasChart {
			continue
		}
		m, _ := src[0].(map[string]any)
		if _, has := m["chart"]; !has {
			f = append(f, rel(root, p)+": chart source must be sources[0]")
		}
	}
	return f
}

func checkPins(root string) []string {
	var f []string
	p := map[string]string{}
	for _, a := range []string{"kargo", "kargo-shard", "kargo-shard-hub", "argocd", "argocd-region"} {
		v, err := pin(root, a)
		if err != nil {
			return []string{err.Error()}
		}
		p[a] = v
	}
	if p["kargo"] != p["kargo-shard"] || p["kargo"] != p["kargo-shard-hub"] {
		f = append(f, fmt.Sprintf("kargo chart pin differs: kargo=%s kargo-shard=%s kargo-shard-hub=%s", p["kargo"], p["kargo-shard"], p["kargo-shard-hub"]))
	}
	if p["argocd"] != p["argocd-region"] {
		f = append(f, fmt.Sprintf("argo-cd chart pin differs: argocd=%s argocd-region=%s", p["argocd"], p["argocd-region"]))
	}
	return f
}

func checkRootApp(root string) []string {
	var f []string
	doc, _ := load(filepath.Join(root, "platform/bootstrap/root.yaml"))
	if str(get(doc, "spec", "source", "path")) != "platform/appsets" {
		f = append(f, "platform/bootstrap/root.yaml must sync platform/appsets")
	}
	for _, n := range []string{"kargo", "projects", "workloads"} {
		if _, err := os.Stat(filepath.Join(root, "platform/appsets", n+".yaml")); err != nil {
			f = append(f, fmt.Sprintf("missing platform/appsets/%s.yaml", n))
		}
	}
	return f
}

var tmplKey = regexp.MustCompile(`\{\{\.([A-Za-z.]*)\}\}`)

func checkWorkloadsKeys(root string) []string {
	b, err := os.ReadFile(filepath.Join(root, goldenParams))
	if err != nil {
		return []string{err.Error()}
	}
	var params map[string]any
	if err := yaml.Unmarshal(b, &params); err != nil {
		return []string{err.Error()}
	}
	w, err := os.ReadFile(filepath.Join(root, "platform/appsets/workloads.yaml"))
	if err != nil {
		return []string{err.Error()}
	}
	seen := map[string]bool{}
	var f []string
	for _, m := range tmplKey.FindAllStringSubmatch(string(w), -1) {
		k := m[1]
		if seen[k] || strings.HasPrefix(k, "values.") {
			continue
		}
		seen[k] = true
		if _, ok := params[k]; !ok {
			f = append(f, fmt.Sprintf("workloads.yaml uses {{.%s}}, not a deploy params key", k))
		}
	}
	return f
}

func checkRenderedRevision(root string) []string {
	var f []string
	for _, p := range yamlFiles(filepath.Join(root, "platform/appsets")) {
		n := rel(root, p)
		doc, err := load(p)
		if err != nil {
			f = append(f, err.Error())
			continue
		}
		walk(doc, func(m map[string]any) {
			rev := m["revision"]
			if rev == nil {
				rev = m["targetRevision"]
			}
			if rev == nil || str(rev) == "" {
				return
			}
			var paths []string
			if s := str(m["path"]); m["path"] != nil {
				paths = append(paths, s)
			}
			for _, fl := range list(m["files"]) {
				if v := get(fl, "path"); v != nil {
					paths = append(paths, str(v))
				}
			}
			joined := strings.Join(paths, ",")
			if strings.HasPrefix(joined, "rendered/") {
				if str(rev) != "rendered" {
					f = append(f, fmt.Sprintf("%s: %s must be read from revision rendered, not %s", n, joined, str(rev)))
				}
			} else if str(rev) == "rendered" {
				f = append(f, fmt.Sprintf("%s: revision rendered only holds rendered/, not '%s'", n, joined))
			}
		})
	}
	return f
}

// ---- tests ----

func repoRoot(t *testing.T) string {
	t.Helper()
	r, err := repo.Root()
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAppSets(t *testing.T) {
	root := repoRoot(t)
	for _, r := range rules {
		t.Run(r.name, func(t *testing.T) {
			for _, m := range r.check(root) {
				t.Errorf("appsets: %s", m)
			}
		})
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		r, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, r), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, r), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, src string) string {
	t.Helper()
	d := t.TempDir()
	for _, p := range []string{"platform/appsets", "platform/infrastructure", "platform/bootstrap", filepath.Dir(goldenParams)} {
		copyTree(t, filepath.Join(src, p), filepath.Join(d, p))
	}
	return d
}

func edit(t *testing.T, root, file, old, new string) {
	t.Helper()
	p := filepath.Join(root, file)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s: %q not found", file, old)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), old, new, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, root, file, content string) {
	t.Helper()
	p := filepath.Join(root, file)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAppSetsNegative(t *testing.T) {
	src := repoRoot(t)
	byName := map[string]func(string) []string{}
	for _, r := range rules {
		byName[r.name] = r.check
	}
	cases := []struct {
		rule   string
		mutate func(t *testing.T, root string)
		want   string
	}{
		{"addon-has-appset", func(t *testing.T, r string) {
			write(t, r, "platform/infrastructure/newaddon/values.yaml", "a: b\n")
		}, "no platform/appsets/addons/newaddon.yaml for platform/infrastructure/newaddon/"},
		{"no-inlined-app", func(t *testing.T, r string) {
			write(t, r, "platform/infrastructure/traefik/app.yaml", "x: y\n")
		}, "platform/infrastructure/*/app.yaml must be gone"},
		{"no-shard-placeholder", func(t *testing.T, r string) {
			write(t, r, "platform/infrastructure/traefik/x.txt", "a __SHARD__ b\n")
		}, "__SHARD__ templating left"},
		{"appset-basics", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "goTemplate: true", "goTemplate: false")
		}, "workloads.yaml: goTemplate must be true"},
		{"appset-basics", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "missingkey=error", "missingkey=zero")
		}, "goTemplateOptions must be [missingkey=error]"},
		{"appset-basics", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "preserveResourcesOnDeletion: true", "preserveResourcesOnDeletion: false")
		}, "syncPolicy.preserveResourcesOnDeletion must be true"},
		{"appset-basics", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "  template:\n    metadata:\n", "  template:\n    metadata:\n      finalizers: [x]\n")
		}, "template must not set finalizers"},
		{"appset-basics", func(t *testing.T, r string) {
			write(t, r, "platform/appsets/notaset.yaml", "kind: ConfigMap\n")
		}, "platform/appsets/notaset.yaml: not an ApplicationSet"},
		{"appset-basics", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/traefik.yaml", "$values/platform/infrastructure/traefik/values.yaml", "$values/platform/infrastructure/traefik/nope.yaml")
		}, "valueFile $values/platform/infrastructure/traefik/nope.yaml missing"},
		{"retry-unlimited", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/traefik.yaml", "limit: -1", "limit: 5")
		}, "traefik.yaml: syncPolicy.retry.limit must be -1 (unlimited)"},
		{"region-selector", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/argocd-region.yaml", "operator: Exists", "operator: In")
		}, "region cluster selector needs matchExpressions"},
		{"region-selector", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/argocd-region.yaml", `{{index .metadata.labels "example.com/region"}}`, `{{.name}}`)
		}, "values.region must be"},
		{"projects-allow-empty", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/projects.yaml", "allowEmpty: true", "allowEmpty: false")
		}, "projects.yaml: automated.allowEmpty must be true"},
		{"chart-first", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/traefik.yaml", "      sources:\n", "      sources:\n        - repoURL: x\n          ref: values\n")
		}, "traefik.yaml: chart source must be sources[0]"},
		{"pins", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/kargo-shard.yaml", "targetRevision: ", "targetRevision: 0.0.0-")
		}, "kargo chart pin differs"},
		{"pins", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/argocd-region.yaml", "targetRevision: ", "targetRevision: 0.0.0-")
		}, "argo-cd chart pin differs"},
		{"root-app", func(t *testing.T, r string) {
			edit(t, r, "platform/bootstrap/root.yaml", "path: platform/appsets", "path: platform/other")
		}, "platform/bootstrap/root.yaml must sync platform/appsets"},
		{"root-app", func(t *testing.T, r string) {
			if err := os.Remove(filepath.Join(r, "platform/appsets/kargo.yaml")); err != nil {
				t.Fatal(err)
			}
		}, "missing platform/appsets/kargo.yaml"},
		{"workloads-keys", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "{{.appName}}", "{{.bogusKey}}")
		}, "workloads.yaml uses {{.bogusKey}}, not a deploy params key"},
		{"rendered-revision", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/workloads.yaml", "revision: rendered", "revision: main")
		}, "must be read from revision rendered, not main"},
		{"rendered-revision", func(t *testing.T, r string) {
			edit(t, r, "platform/appsets/addons/traefik.yaml", "targetRevision: 42.3.1", "targetRevision: rendered")
		}, "revision rendered only holds rendered/"},
	}
	for _, c := range cases {
		t.Run(c.rule+"/"+c.want, func(t *testing.T) {
			root := fixture(t, src)
			if got := byName[c.rule](root); len(got) != 0 {
				t.Fatalf("baseline copy fails: %v", got)
			}
			c.mutate(t, root)
			got := strings.Join(byName[c.rule](root), "\n")
			if !strings.Contains(got, c.want) {
				t.Fatalf("want failure containing %q, got %q", c.want, got)
			}
		})
	}
}
