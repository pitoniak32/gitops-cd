package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// test.sh: "regions ok"
func TestRegions(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	if got := strings.TrimRight(mustRead(t, filepath.Join(out, "infrastructure/regions/us-east4.json")), "\n"); got != `{"region":"us-east4"}` {
		t.Error("infrastructure/regions/us-east4.json wrong")
	}
	ents, err := os.ReadDir(filepath.Join(out, "infrastructure/regions"))
	if err != nil {
		t.Fatal(err)
	}
	var ns []string
	for _, e := range ents {
		ns = append(ns, e.Name())
	}
	if strings.Join(ns, " ") != "dc1.json us-east4.json" {
		t.Error("one regions JSON per cluster-registry region")
	}
	if _, err := os.Stat(filepath.Join(out, "argocd/projects/dc1/.keep")); err != nil {
		t.Error("argocd/projects/<r>/.keep missing")
	}
}

// test.sh: "kind cluster ok" (Argo CD half: AppProjects, deploy params, per-region output)
func TestKindClusterArgoCD(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	ap := docs(t, filepath.Join(out, "argocd/projects/us-east4/payments.yaml"))
	if find(ap, "AppProject", "payments") == nil {
		t.Error("missing AppProject payments")
	}
	// no secret store -> no ExternalSecrets / secret env refs
	secretRef := regexp.MustCompile(`ExternalSecret|secretKeyRef`)
	err := filepath.WalkDir(filepath.Join(out, "apps"), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() == "service.yaml" {
			return err
		}
		if secretRef.MatchString(mustRead(t, p)) {
			t.Error("secret refs rendered for a cluster without a secret store")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mustRead(t, abs(t, "testdata/root/systems/payments/apps/my-app/app.yml")), "APP_SECRET") {
		t.Error("fixture lost its secret: env value")
	}

	// Argo CD visibility (ADR 0029): one viewer-<app> role per app with argocd deploys in the region, naming
	// exactly its Applications, for its promoters + viewers + break-glass; infra (terraform, gha only) gets none
	pol := func(app string, tgts ...string) string {
		var ps []string
		for _, tg := range tgts {
			ps = append(ps, `"p, proj:payments:viewer-`+app+`, applications, get, payments/payments-`+app+`-`+tg+`, allow"`)
		}
		return `"policies":[` + strings.Join(ps, ",") + `]`
	}
	const std = `"groups":["bob@example.com","doug@example.com","grace@example.com"]`
	want := strings.Join([]string{
		`{"name":"viewer-my-app",` + pol("my-app", "us-east4-dev-k8s", "us-east4-npr-k8s", "us-east4-npr-k8s-perf", "us-east4-prd-k8s") + `,` + std + `}`,
		`{"name":"viewer-payments",` + pol("payments", "us-east4-dev-k8s", "us-east4-npr-k8s", "us-east4-prd-k8s") + `,` + std + `}`,
		`{"name":"viewer-web",` + pol("web", "us-east4-dev-k8s") + `,` + std + `}`,
		`{"name":"viewer-worker",` + pol("worker", "us-east4-npr-k8s", "us-east4-prd-k8s") + `,"groups":["alice@example.com","bob@example.com","carol@example.com","doug@example.com","grace@example.com"]}`,
	}, "\n")
	type role struct {
		Name     string   `json:"name"`
		Policies []string `json:"policies"`
		Groups   []string `json:"groups"`
	}
	var lines []string
	for _, d := range findAll(ap, "AppProject") {
		for _, r := range list(d, "spec.roles") {
			b, _ := json.Marshal(r)
			var x role
			if err := json.Unmarshal(b, &x); err != nil {
				t.Fatal(err)
			}
			b, _ = json.Marshal(x)
			lines = append(lines, string(b))
		}
	}
	if got := strings.Join(lines, "\n"); got != want {
		t.Errorf("AppProject viewer role\nwant: %s\ngot:  %s", want, got)
	}

	d := filepath.Join(out, "argocd/deploys/dc1/payments/my-app-dc1-prd-k8s.json")
	dd := docs(t, d)[0]
	if got := str(dd["appName"]) + " " + str(dd["branch"]) + " " + str(dd["authorizedStage"]) + " " + str(dd["destination"]) + "/" + str(dd["namespace"]); got != "payments-my-app-dc1-prd-k8s env/payments/my-app/dc1-prd-k8s kargo-payments-my-app:dc1-prd-k8s in-cluster/payments-onprem-prod" {
		t.Error("deploy params must track the target branch, authorized for its target stage")
	}
	// workloads AppSet (platform/appsets/workloads.yaml) reads exactly these keys; git files gen reserves path/values
	var keys []string
	for k := range dd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if got := strings.Join(keys, ","); got != "app,appName,authorizedStage,branch,destination,namespace,stage,system,target" {
		t.Errorf("deploy params keys changed: %s", got)
	}

	// Argo CD output is per region; destination is the regional Argo CD's own cluster
	var files []string
	argocd := filepath.Join(out, "argocd")
	err = filepath.WalkDir(argocd, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || e.Name() == ".keep" {
			return err
		}
		rel, _ := filepath.Rel(argocd, p)
		files = append(files, "./"+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if strings.Join(files, " ") != "./deploys/dc1/payments/my-app-dc1-dev-k8s.json ./deploys/dc1/payments/my-app-dc1-prd-k8s.json ./deploys/us-east4/payments/my-app-us-east4-dev-k8s.json ./deploys/us-east4/payments/my-app-us-east4-npr-k8s-perf.json ./deploys/us-east4/payments/my-app-us-east4-npr-k8s.json ./deploys/us-east4/payments/my-app-us-east4-prd-k8s.json ./deploys/us-east4/payments/payments-us-east4-dev-k8s.json ./deploys/us-east4/payments/payments-us-east4-npr-k8s.json ./deploys/us-east4/payments/payments-us-east4-prd-k8s.json ./deploys/us-east4/payments/web-us-east4-dev-k8s.json ./deploys/us-east4/payments/worker-us-east4-npr-k8s.json ./deploys/us-east4/payments/worker-us-east4-prd-k8s.json ./deploys/us-east4/search/api-us-east4-dev-k8s.json ./projects/dc1/payments.yaml ./projects/us-east4/payments.yaml ./projects/us-east4/search.yaml" {
		t.Error("argocd output must be per region: deploys/<r>/<sys>/ + projects/<r>/<sys>.yaml")
	}
	appRe := regexp.MustCompile(`(?m)kind: Application$`)
	for _, f := range files {
		if appRe.MatchString(mustRead(t, filepath.Join(argocd, f))) {
			t.Error("renderer must not emit Applications (workloads AppSet does)")
		}
	}
	var dests []string
	for _, x := range list(find(docs(t, filepath.Join(out, "argocd/projects/dc1/payments.yaml")), "AppProject", ""), "spec.destinations") {
		dests = append(dests, str(get(x, "name"))+"/"+str(get(x, "namespace")))
	}
	if strings.Join(dests, " ") != "in-cluster/payments-onprem-dev in-cluster/payments-onprem-prod" {
		t.Error("onprem AppProject must list only onprem destinations")
	}
	for _, f := range globOne(t, filepath.Join(out, "argocd/deploys/us-east4/*/*.json")) {
		if strings.Contains(mustRead(t, f), "dc1") {
			t.Error("us-east4 deploys contain dc1 targets")
		}
	}
	for _, f := range globOne(t, filepath.Join(out, "kargo/*.yaml")) {
		if strings.Contains(mustRead(t, f), `"groups"`) {
			t.Error("no Kargo claim may use groups")
		}
	}
}

// test.sh: "empty resourceNames in golden"
func TestNoEmptyResourceNames(t *testing.T) {
	t.Parallel()
	fs := globOne(t, abs(t, "testdata/golden/kargo/*.yaml"))
	if len(fs) == 0 {
		t.Fatal("no golden kargo yaml")
	}
	for _, f := range fs {
		for _, r := range findAll(docs(t, f), "Role") {
			for _, rule := range list(r, "rules") {
				if rn, ok := get(rule, "resourceNames").([]any); ok && len(rn) == 0 {
					t.Error("empty resourceNames in golden")
				}
			}
		}
	}
}

// test.sh: "envFrom precedence ok"
// target env > stage env > app env > environment-variables bundle
func TestEnvFromPrecedence(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ target, want string }{{"us-east4-dev-k8s", "true"}, {"dc1-dev-k8s", "false"}} {
		var vals []string
		for _, f := range globOne(t, abs(t, "testdata/golden/apps/payments/my-app/dev/"+c.target+"/*.yaml")) {
			for _, d := range findAll(docs(t, f), "Deployment") {
				for _, e := range list(d, "spec.template.spec.containers.0.env") {
					if str(get(e, "name")) == "APP_ENV" {
						vals = append(vals, str(get(e, "value")))
					}
				}
			}
		}
		if v := strings.Join(vals, "\n"); v != c.want {
			t.Errorf("APP_ENV on %s: got '%s' want '%s' (bundle must not beat app/target env)", c.target, v, c.want)
		}
	}
}

// test.sh: "prefix systems ok"
// system names that prefix each other (pay vs payments): re-rendering one must not wipe the other's deploys
func TestPrefixSystems(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	copyTree(t, filepath.Join(r, "systems/payments"), filepath.Join(r, "systems/pay"))
	o := t.TempDir()
	renderTestRoot(t, r, o, "payments", "pay", "pay")
	for _, c := range []string{"payments", "pay"} {
		if _, err := os.Stat(filepath.Join(o, "argocd/deploys/us-east4", c, "my-app-us-east4-dev-k8s.json")); err != nil {
			t.Error("re-render of pay wiped payments deploys")
		}
	}
}
