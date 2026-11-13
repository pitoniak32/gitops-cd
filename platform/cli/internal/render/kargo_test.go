package render

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

func stage(ds []map[string]any, name string) map[string]any { return find(ds, "Stage", name) }

func label(d map[string]any, key string) string {
	return str(get(d, `metadata.labels."`+key+`"`))
}

// stagesOf lists an app's Stage names, sorted.
func stagesOf(ds []map[string]any, app string) string {
	return strings.Join(names(ds, "Stage", func(d map[string]any) bool { return label(d, "example.com/app") == app }), " ")
}

// src renders a Stage's first freight source as "<direct>:<availabilityStrategy>:<stages,...>".
func src(ds []map[string]any, name string) string {
	s := stage(ds, name)
	if s == nil {
		return ""
	}
	direct := "false"
	if get(s, "spec.requestedFreight.0.sources.direct") == true {
		direct = "true"
	}
	return direct + ":" + str(get(s, "spec.requestedFreight.0.sources.availabilityStrategy")) + ":" + strings.Join(strs(s, "spec.requestedFreight.0.sources.stages"), ",")
}

func wantSrc(t *testing.T, ds []map[string]any, name, want string) {
	t.Helper()
	if got := src(ds, name); got != want {
		t.Errorf("Stage %s sources: want '%s' got '%s'", name, want, got)
	}
}

// oneclick: every stage with >1 target is exactly the downstream set of one Stage (any app in ds).
func oneclick(t *testing.T, ds []map[string]any) {
	t.Helper()
	uniq := func(xs []string) []string {
		sort.Strings(xs)
		var out []string
		for i, x := range xs {
			if i == 0 || xs[i-1] != x {
				out = append(out, x)
			}
		}
		return out
	}
	var apps []string
	for _, s := range findAll(ds, "Stage") {
		apps = append(apps, label(s, "example.com/app"))
	}
	all := ds
	for _, app := range uniq(apps) {
		// Stage names repeat across apps (one project each, ADR 0029): look only at this app's
		var ds []map[string]any
		for _, d := range findAll(all, "Stage") {
			if label(d, "example.com/app") == app {
				ds = append(ds, d)
			}
		}
		count := map[string]int{}
		for _, s := range findAll(ds, "Stage") {
			if label(s, "example.com/app") == app && label(s, "example.com/kind") == "target" {
				count[label(s, "example.com/stage")]++
			}
		}
		var multi []string
		for st, n := range count {
			if n > 1 { // uniq -d
				multi = append(multi, st)
			}
		}
		sort.Strings(multi)
		for _, st := range multi {
			tg := strings.Join(names(ds, "Stage", func(d map[string]any) bool {
				return label(d, "example.com/app") == app && label(d, "example.com/kind") == "target" && label(d, "example.com/stage") == st
			}), ",")
			ok := false
			for _, n := range strings.Fields(stagesOf(ds, app)) {
				ch := strings.Join(names(ds, "Stage", func(d map[string]any) bool {
					for _, x := range strs(d, "spec.requestedFreight.0.sources.stages") {
						if x == n {
							return true
						}
					}
					return false
				}), ",")
				if ch == tg {
					ok = true
				}
			}
			if !ok {
				t.Errorf("e) %s stage %s (%s) has no one-click node", app, st, tg)
			}
		}
	}
}

// test.sh: "kind cluster ok" (Kargo graph half)
func TestKindClusterKargo(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	all := docs(t, filepath.Join(out, "kargo/payments.yaml"))
	// one project per app (ADR 0029); Argo CD dry-runs all resources first: each project namespace must be
	// rendered (labeled for Kargo to adopt) ahead of the project's resources
	if got := strings.Join(names(all, "Project", nil), " "); got != "kargo-payments-flagged kargo-payments-infra kargo-payments-my-app kargo-payments-payments kargo-payments-web kargo-payments-worker" {
		t.Errorf("one Kargo project per app: %s", got)
	}
	for _, p := range names(all, "Project", nil) {
		ds := inProject(all, p)
		ns := ds[0]
		if str(ns["kind"])+"/"+str(get(ns, "metadata.name"))+"/"+label(ns, "kargo.akuity.io/project")+"/"+label(ns, "example.com/capability")+"/"+label(ns, "example.com/app") != "Namespace/"+p+"/true/payments/"+strings.TrimPrefix(p, "kargo-payments-") {
			t.Errorf("%s must start with its labeled Namespace", p)
		}
		if str(ds[1]["kind"]) != "Project" || find(ds, "ProjectConfig", p) == nil {
			t.Errorf("%s: Project + ProjectConfig", p)
		}
		if w := names(ds, "Warehouse", nil); len(w) != 1 || w[0] != strings.TrimPrefix(p, "kargo-payments-") {
			t.Errorf("%s: one Warehouse named after the app, got %v", p, w)
		}
	}
	k := inProject(all, "kargo-payments-my-app")

	// Kargo graph (ADR 0012); Stage names carry no app prefix (ADR 0029)
	if got := stagesOf(k, "my-app"); got != "dc1-dev-k8s dc1-npr-k8s dc1-prd-k8s dev dev-done prod staging us-east4-dev-k8s us-east4-npr-k8s us-east4-npr-k8s-perf us-east4-prd-k8s" {
		t.Errorf("my-app Stage set: %s", got)
	}
	wantSrc(t, k, "dev", "true::")
	wantSrc(t, k, "us-east4-dev-k8s", "false::dev")
	wantSrc(t, k, "dc1-dev-k8s", "false::dev")
	wantSrc(t, k, "dev-done", "false:All:dc1-dev-k8s,us-east4-dev-k8s")
	wantSrc(t, k, "staging", "false::dev-done")
	wantSrc(t, k, "us-east4-npr-k8s", "false::staging")
	wantSrc(t, k, "dc1-npr-k8s", "false::staging")
	wantSrc(t, k, "us-east4-npr-k8s-perf", "false::dev-done")
	wantSrc(t, k, "prod", "false:All:dc1-npr-k8s,us-east4-npr-k8s,us-east4-npr-k8s-perf")
	wantSrc(t, k, "us-east4-prd-k8s", "false::prod")
	wantSrc(t, k, "dc1-prd-k8s", "false::prod")
	w := inProject(all, "kargo-payments-worker")
	if got := stagesOf(w, "worker"); got != "us-east4-dev-k8s us-east4-npr-k8s us-east4-prd-k8s" {
		t.Errorf("worker must have no control flow: %s", got)
	}
	wantSrc(t, w, "us-east4-dev-k8s", "true::")
	wantSrc(t, w, "us-east4-npr-k8s", "false::us-east4-dev-k8s")
	wantSrc(t, w, "us-east4-prd-k8s", "false::us-east4-npr-k8s")
	s := inProject(docs(t, filepath.Join(out, "kargo/search.yaml")), "kargo-search-api")
	if got := stagesOf(s, "api"); got != "us-east4-dev-k8s" {
		t.Error("search api must be one target Stage")
	}
	wantSrc(t, s, "us-east4-dev-k8s", "true::")
	for _, n := range []string{"dev", "dev-done", "prod", "staging"} {
		st := stage(k, n)
		has := "false"
		if _, ok := get(st, "spec").(map[string]any)["promotionTemplate"]; ok {
			has = "true"
		}
		shard := str(get(st, "spec.shard"))
		if shard == "" {
			shard = "-"
		}
		if label(st, "example.com/kind")+" "+has+" "+shard != "control-flow false -" {
			t.Errorf("%s must be control-flow: no steps, no shard", n)
		}
	}
	if label(stage(k, "dev-done"), "example.com/stage")+" "+label(stage(k, "staging"), "example.com/stage") != "dev staging" {
		t.Error("control-flow stage labels")
	}
	// ProjectConfig: only this app's auto-promoted Stages
	autoOf := func(p string) string {
		var sel []string
		for _, x := range list(find(all, "ProjectConfig", p), "spec.promotionPolicies") {
			sel = append(sel, str(get(x, "stageSelector.name")))
		}
		sort.Strings(sel)
		return strings.Join(sel, " ")
	}
	if got := autoOf("kargo-payments-my-app"); got != "dc1-dev-k8s dev dev-done prod staging us-east4-dev-k8s" {
		t.Errorf("auto-promotion: every control-flow Stage + targets of autoPromote stages only: %s", got)
	}
	// worker overrides autoPromote: false on dev in its app.yml: nothing auto-promotes
	if got := autoOf("kargo-payments-worker"); got != "" {
		t.Errorf("worker auto-promotion (app override): %s", got)
	}
	if str(get(stage(k, "dc1-prd-k8s"), "spec.promotionTemplate.spec.vars.2.value")) != "env/payments/my-app/dc1-prd-k8s" {
		t.Error("target stage must push its own branch")
	}

	// regions: target Stages run on their cluster's region shard; control-flow Stages and Warehouses stay on the hub controller
	var sh []string
	for _, st := range findAll(k, "Stage") {
		v := str(get(st, "spec.shard"))
		if v == "" {
			v = "-"
		}
		sh = append(sh, str(get(st, "metadata.name"))+"="+v)
	}
	sort.Strings(sh)
	if strings.Join(sh, " ") != "dc1-dev-k8s=dc1 dc1-npr-k8s=dc1 dc1-prd-k8s=dc1 dev-done=- dev=- prod=- staging=- us-east4-dev-k8s=us-east4 us-east4-npr-k8s-perf=us-east4 us-east4-npr-k8s=us-east4 us-east4-prd-k8s=us-east4" {
		t.Errorf("shard must be set on target Stages only, from the target's cluster region: %v", sh)
	}
	for _, w := range findAll(all, "Warehouse") {
		if get(w, "spec.shard") != nil {
			t.Error("Warehouses must not be sharded")
		}
	}
	// e) one-click: every stage with >1 target is exactly the downstream set of one Stage
	oneclick(t, all)
}

// test.sh: "rendered branch ok" (ADR 0002: Warehouses watch the rendered branch, not main)
func TestRenderedBranch(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	seen := map[string]bool{}
	for _, f := range []string{"payments", "search"} {
		for _, w := range findAll(docs(t, filepath.Join(out, "kargo", f+".yaml")), "Warehouse") {
			seen[str(get(w, "spec.subscriptions.0.git.branch"))] = true
		}
	}
	if len(seen) != 1 || !seen["rendered"] {
		t.Error("Warehouse git branch must be rendered")
	}
}

// test.sh: "}}" check, run on the fresh render and the golden Kargo files
func TestKargoExpressionsNotCutShort(t *testing.T) {
	t.Parallel()
	re := regexp.MustCompile(`[^ ]\}\}`)
	files := append(globOne(t, filepath.Join(sharedOut(t), "kargo/payments.yaml")), globOne(t, abs(t, "testdata/golden/kargo/*.yaml"))...)
	files = append(files, filepath.Join(sharedOut(t), PromotionTasksFile))
	for _, f := range files {
		if l := grepLines(mustRead(t, f), re); len(l) > 0 {
			t.Errorf("Kargo expression cut short: \"}}\" not preceded by a space (%s): %v", f, l)
		}
	}
}

// synth copies the fixture, applies setup, renders capName and returns its Kargo docs.
func synth(t *testing.T, capName string, setup func(t *testing.T, caps string)) []map[string]any {
	t.Helper()
	r := fixture(t)
	setup(t, filepath.Join(r, "capabilities"))
	return docs(t, filepath.Join(renderCap(t, r, capName), "kargo", capName+".yaml"))
}

func del(t *testing.T, file, path string) {
	t.Helper()
	must(t, yamledit.Del(file, strings.Split(path, ".")))
}

// addQA copies the npr targets of both regions as -qa targets and wires them into stage qa of my-app.
func addQA(t *testing.T, caps string) {
	for _, r := range []string{"us-east4", "dc1"} {
		n := r + "-npr-k8s-qa"
		copyTree(t, filepath.Join(caps, "payments/targets", r+"-npr-k8s"), filepath.Join(caps, "payments/targets", n))
		must(t, yamledit.Set(filepath.Join(caps, "payments/targets", n, "target.yml"), []string{"config", "namespace"}, "payments-"+r+"-qa"))
		must(t, yamledit.Set(filepath.Join(caps, "payments/apps/my-app/app.yml"), []string{"stages", "qa", "targets", n}, map[string]any{}))
	}
}

// test.sh: "control-flow graph ok" (ADR 0012, synthetic variants a-f)
func TestControlFlowGraph(t *testing.T) {
	t.Parallel()
	// a) app skips perf: dev fans into staging alone (fan-in node named for staging), staging fans into prod
	const myApp, worker = "kargo-payments-my-app", "kargo-payments-worker"
	k := inProject(synth(t, "payments", func(t *testing.T, c string) { del(t, filepath.Join(c, "payments/apps/my-app/app.yml"), "stages.perf") }), myApp)
	if stage(k, "dev-done") != nil {
		t.Error("a) no dev-done without fan-out")
	}
	wantSrc(t, k, "staging", "false:All:dc1-dev-k8s,us-east4-dev-k8s")
	wantSrc(t, k, "prod", "false:All:dc1-npr-k8s,us-east4-npr-k8s")
	oneclick(t, k)

	// b) 1->N: single dev target feeds both prod targets directly
	k = inProject(synth(t, "search", func(t *testing.T, c string) {
		writeFile(t, filepath.Join(c, "search/stages/prod.yml"), "dependsOn: [dev]\n")
		must(t, yamledit.Set(filepath.Join(c, "search/owners.yml"), []string{"promoters", "prod"}, []string{"alice@example.com"}))
		for _, tg := range []string{"us-east4-prd-k8s", "dc1-prd-k8s"} {
			copyTree(t, filepath.Join(c, "payments/targets", tg), filepath.Join(c, "search/targets", tg))
			must(t, yamledit.Set(filepath.Join(c, "search/targets", tg, "target.yml"), []string{"config", "namespace"}, "search-"+tg))
			must(t, yamledit.Set(filepath.Join(c, "search/apps/api/app.yml"), []string{"stages", "prod", "targets", tg}, map[string]any{}))
		}
	}), "kargo-search-api")
	if got := stagesOf(k, "api"); got != "dc1-prd-k8s us-east4-dev-k8s us-east4-prd-k8s" {
		t.Errorf("b) 1->N needs no control flow: %s", got)
	}
	wantSrc(t, k, "us-east4-prd-k8s", "false::us-east4-dev-k8s")
	wantSrc(t, k, "dc1-prd-k8s", "false::us-east4-dev-k8s")
	oneclick(t, k)

	// c) worker-us-east4-dev-k8s feeds staging (2 targets) and perf: staging needs its own entry; prod joins 3 sources directly (M->1)
	k = inProject(synth(t, "payments", func(t *testing.T, c string) {
		f := filepath.Join(c, "payments/apps/worker/app.yml")
		must(t, yamledit.Set(f, []string{"stages", "perf", "targets", "us-east4-npr-k8s-perf"}, map[string]any{}))
		must(t, yamledit.Set(f, []string{"stages", "staging", "targets", "dc1-npr-k8s"}, map[string]any{}))
	}), worker)
	wantSrc(t, k, "staging", "false::us-east4-dev-k8s")
	wantSrc(t, k, "us-east4-npr-k8s", "false::staging")
	wantSrc(t, k, "us-east4-npr-k8s-perf", "false::us-east4-dev-k8s")
	wantSrc(t, k, "us-east4-prd-k8s", "false:All:dc1-npr-k8s,us-east4-npr-k8s,us-east4-npr-k8s-perf")
	oneclick(t, k)

	// d) qa and prod both depend on staging+perf: one shared node named after both upstream stages
	k = inProject(synth(t, "payments", func(t *testing.T, c string) {
		writeFile(t, filepath.Join(c, "payments/stages/qa.yml"), "dependsOn: [staging, perf]\n")
		must(t, yamledit.Set(filepath.Join(c, "payments/owners.yml"), []string{"promoters", "qa"}, []string{"doug@example.com"}))
		addQA(t, c)
	}), myApp)
	wantSrc(t, k, "perf-staging-done", "false:All:dc1-npr-k8s,us-east4-npr-k8s,us-east4-npr-k8s-perf")
	wantSrc(t, k, "prod", "false::perf-staging-done")
	wantSrc(t, k, "qa", "false::perf-staging-done")
	oneclick(t, k)

	// f) a single upstream target that also feeds an M->1 join: the multi-target stage it feeds still needs an entry
	k = inProject(synth(t, "payments", func(t *testing.T, c string) {
		f := filepath.Join(c, "payments/apps/my-app/app.yml")
		del(t, f, "stages.staging.targets.dc1-npr-k8s")
		del(t, f, "stages.prod.targets.dc1-prd-k8s")
		writeFile(t, filepath.Join(c, "payments/stages/qa.yml"), "dependsOn: [staging]\n")
		must(t, yamledit.Set(filepath.Join(c, "payments/owners.yml"), []string{"promoters", "qa"}, []string{"doug@example.com"}))
		addQA(t, c)
	}), myApp)
	wantSrc(t, k, "qa", "false::us-east4-npr-k8s")
	wantSrc(t, k, "us-east4-npr-k8s-qa", "false::qa")
	wantSrc(t, k, "us-east4-prd-k8s", "false:All:us-east4-npr-k8s,us-east4-npr-k8s-perf")
	oneclick(t, k)
}
