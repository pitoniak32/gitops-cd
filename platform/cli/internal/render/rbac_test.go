package render

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

func claims(ds []map[string]any, sa string) string {
	return str(get(find(ds, "ServiceAccount", sa), `metadata.annotations."rbac.kargo.akuity.io/claims"`))
}

func resourceNames(ds []map[string]any, role string) string {
	return strings.Join(strs(find(ds, "Role", role), "rules.0.resourceNames"), ",")
}

// test.sh: "kind cluster ok" (Kargo RBAC half)
func TestKindClusterRBAC(t *testing.T) {
	t.Parallel()
	all := docs(t, filepath.Join(sharedOut(t), "kargo/payments.yaml"))
	k := inProject(all, "kargo-payments-my-app")
	w := inProject(all, "kargo-payments-worker")

	// Kargo visibility (email claim): per app project (ADR 0029)
	rb := find(k, "RoleBinding", "viewers")
	if str(get(rb, "roleRef.kind"))+"/"+str(get(rb, "roleRef.name"))+"/"+str(get(rb, "subjects.0.name")) != "Role/kargo-viewer/viewers" {
		t.Error("missing viewers RoleBinding")
	}
	if claims(k, "viewers") != `{"email":["bob@example.com","doug@example.com","oncall@example.com"]}` {
		t.Error("my-app viewers SA must map its promoters + viewers + break-glass by email")
	}
	if claims(w, "viewers") != `{"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com","oncall@example.com"]}` {
		t.Error("worker viewers SA must map its promoters + viewers + break-glass by email")
	}
	users := find(all, "ServiceAccount", "users-payments")
	if str(get(users, "metadata.namespace"))+" "+claims(all, "users-payments") != `kargo-shared-resources {"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com","oncall@example.com"]}` {
		t.Error("missing users-payments global SA")
	}
	crb := find(all, "ClusterRoleBinding", "kargo-users-payments")
	if str(get(crb, "roleRef.name"))+"/"+str(get(crb, "subjects.0.namespace"))+"/"+str(get(crb, "subjects.0.name")) != "kargo-user/kargo-shared-resources/users-payments" {
		t.Error("missing kargo-users-payments ClusterRoleBinding")
	}

	// Kargo's promote API needs create on promotions as well as promote on the (named) stages
	for _, rl := range findAll(all, "Role") {
		role := str(get(rl, "metadata.namespace")) + "/" + str(get(rl, "metadata.name"))
		ok := false
		for _, rule := range list(rl, "rules") {
			if str(get(rule, "resources.0")) != "promotions" {
				continue
			}
			for _, v := range list(rule, "verbs") {
				if str(v) == "create" {
					ok = true
				}
			}
		}
		if !ok {
			t.Errorf("%s can't create promotions", role)
		}
	}

	// promoters are per app: worker's app owners replace promoters; my-app inherits the capability's
	if claims(w, "promote-dev") != `{"email":["alice@example.com","doug@example.com","oncall@example.com"]}` {
		t.Error("worker promote-dev claims")
	}
	if claims(w, "promote-staging") != `{"email":["doug@example.com","oncall@example.com"]}` {
		t.Error("worker promote-staging must inherit")
	}
	if claims(w, "promote-prod") != `{"email":["alice@example.com","oncall@example.com"]}` {
		t.Error("worker promote-prod claims")
	}

	// break-glass (ADR 0013): every control-flow and target Stage carries the app's list for the approve guard
	bg := map[string]bool{}
	for _, s := range findAll(k, "Stage") {
		if str(get(s, `metadata.labels."example.com/app"`)) == "my-app" {
			bg[str(get(s, `metadata.annotations."example.com/break-glass"`))] = true
		}
	}
	var bgs []string
	for v := range bg {
		bgs = append(bgs, v)
	}
	sort.Strings(bgs)
	if strings.Join(bgs, "\n") != `["oncall@example.com"]` {
		t.Error("every my-app Stage needs the break-glass annotation")
	}

	if resourceNames(w, "promote-dev") != "us-east4-dev-k8s" {
		t.Error("worker promote-dev resourceNames")
	}
	if resourceNames(w, "promote-staging") != "us-east4-npr-k8s" {
		t.Error("worker promote-staging resourceNames")
	}
	if resourceNames(w, "promote-prod") != "us-east4-prd-k8s" {
		t.Error("worker promote-prod resourceNames")
	}
	if got := strings.Join(names(w, "Role", nil), " "); got != "promote-dev promote-prod promote-staging" {
		t.Errorf("worker promote roles: %s", got)
	}
	for _, st := range []string{"staging", "perf"} {
		found := false
		for _, n := range strs(find(k, "Role", "promote-"+st), "rules.0.resourceNames") {
			if n == "dev-done" {
				found = true
			}
		}
		if !found {
			t.Errorf("my-app promote-%s must include shared dev-done", st)
		}
	}
	union := map[string]bool{}
	for _, r := range findAll(k, "Role") {
		if strings.HasPrefix(str(get(r, "metadata.name")), "promote-") {
			for _, n := range strs(r, "rules.0.resourceNames") {
				union[n] = true
			}
		}
	}
	var u []string
	for n := range union {
		u = append(u, n)
	}
	sort.Strings(u)
	if strings.Join(u, ",") != "dc1-dev-k8s,dc1-npr-k8s,dc1-prd-k8s,dev,dev-done,prod,staging,us-east4-dev-k8s,us-east4-npr-k8s,us-east4-npr-k8s-perf,us-east4-prd-k8s" {
		t.Error("my-app promote-* union must cover targets + control flow")
	}
}

// test.sh: "override-all owners ok"
// Every app overrides promoters: the capability-level promoter gets no promote role but still
// views; stages/targets approvers (CODEOWNERS) are the apps' owners.
func TestOverrideAllOwners(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	writeFile(t, filepath.Join(r, "capabilities/search/owners.yml"), "codeOwners: [team1]\npromoters: {dev: [carol@example.com]}\napps: {api: {codeOwners: [team2], promoters: {dev: [dave@example.com]}}}\n")
	o := renderCap(t, r, "search")
	kf := filepath.Join(o, "kargo/search.yaml")
	if _, err := os.Stat(kf); err != nil {
		t.Fatalf("missing %s", kf)
	}
	if claims(inProject(docs(t, kf), "kargo-search-api"), "promote-dev") != `{"email":["dave@example.com"]}` {
		t.Error("capability-only promoter got promote rights")
	}
	co := mustRead(t, filepath.Join(o, "codeowners/search"))
	has := false
	for _, l := range strings.Split(co, "\n") {
		if l == "/capabilities/search/ @acme/team2" {
			has = true
		}
	}
	if !has {
		t.Errorf("stages/targets approvers must be the apps' owners:\n%s", co)
	}
}

// test.sh: "app viewers ok"
// App viewers (apps.<a>.viewers) replace the capability's viewers in that app's project only (ADR 0029)
func TestAppViewers(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	must(t, yamledit.Set(filepath.Join(r, "capabilities/payments/owners.yml"), []string{"apps", "worker", "viewers"}, []string{"erin@example.com"}))
	o := renderCap(t, r, "payments")
	all := docs(t, filepath.Join(o, "kargo/payments.yaml"))
	if got := claims(inProject(all, "kargo-payments-worker"), "viewers"); got != `{"email":["alice@example.com","doug@example.com","erin@example.com","oncall@example.com"]}` {
		t.Errorf("worker viewers: app viewer replaces capability viewer bob: %s", got)
	}
	if got := claims(inProject(all, "kargo-payments-my-app"), "viewers"); got != `{"email":["bob@example.com","doug@example.com","oncall@example.com"]}` {
		t.Errorf("my-app viewers must not include worker's viewer: %s", got)
	}
	if !strings.Contains(claims(all, "users-payments"), "erin@example.com") {
		t.Error("app viewer must still list the capability's projects")
	}
}
