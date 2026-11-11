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
	k := docs(t, filepath.Join(sharedOut(t), "kargo/payments.yaml"))

	// Kargo visibility (email claim)
	rb := find(k, "RoleBinding", "viewers")
	if str(get(rb, "roleRef.kind"))+"/"+str(get(rb, "roleRef.name"))+"/"+str(get(rb, "subjects.0.name")) != "Role/kargo-viewer/viewers" {
		t.Error("missing viewers RoleBinding")
	}
	if claims(k, "viewers") != `{"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}` {
		t.Error("viewers SA must map all promoters + viewers by email")
	}
	users := find(k, "ServiceAccount", "users-payments")
	if str(get(users, "metadata.namespace"))+" "+claims(k, "users-payments") != `kargo-shared-resources {"email":["alice@example.com","bob@example.com","carol@example.com","doug@example.com"]}` {
		t.Error("missing users-payments global SA")
	}
	crb := find(k, "ClusterRoleBinding", "kargo-users-payments")
	if str(get(crb, "roleRef.name"))+"/"+str(get(crb, "subjects.0.namespace"))+"/"+str(get(crb, "subjects.0.name")) != "kargo-user/kargo-shared-resources/users-payments" {
		t.Error("missing kargo-users-payments ClusterRoleBinding")
	}

	// Kargo's promote API needs create on promotions as well as promote on the (named) stages
	for _, role := range names(k, "Role", func(d map[string]any) bool { return strings.HasPrefix(str(get(d, "metadata.name")), "promote-") }) {
		ok := false
		for _, rule := range list(find(k, "Role", role), "rules") {
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
	if claims(k, "promote-worker-dev") != `{"email":["alice@example.com","carol@example.com","doug@example.com"]}` {
		t.Error("promote-worker-dev claims")
	}
	if claims(k, "promote-worker-staging") != `{"email":["carol@example.com","doug@example.com"]}` {
		t.Error("promote-worker-staging must inherit")
	}
	if claims(k, "promote-worker-prod") != `{"email":["alice@example.com","carol@example.com"]}` {
		t.Error("promote-worker-prod claims")
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
	if strings.Join(bgs, "\n") != `["carol@example.com"]` {
		t.Error("every my-app Stage needs the break-glass annotation")
	}

	if resourceNames(k, "promote-worker-dev") != "worker-us-east4-dev-k8s" {
		t.Error("promote-worker-dev resourceNames")
	}
	if resourceNames(k, "promote-worker-staging") != "worker-us-east4-npr-k8s" {
		t.Error("promote-worker-staging resourceNames")
	}
	if resourceNames(k, "promote-worker-prod") != "worker-us-east4-prd-k8s" {
		t.Error("promote-worker-prod resourceNames")
	}
	for _, n := range []string{"promote-worker-perf", "promote-worker", "promote-my-app"} {
		if find(k, "Role", n) != nil {
			t.Error("unexpected promote role")
		}
	}
	for _, st := range []string{"staging", "perf"} {
		found := false
		for _, n := range strs(find(k, "Role", "promote-my-app-"+st), "rules.0.resourceNames") {
			if n == "my-app-dev-done" {
				found = true
			}
		}
		if !found {
			t.Errorf("promote-my-app-%s must include shared my-app-dev-done", st)
		}
	}
	union := map[string]bool{}
	for _, r := range findAll(k, "Role") {
		if strings.HasPrefix(str(get(r, "metadata.name")), "promote-my-app-") {
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
	if strings.Join(u, ",") != "my-app-dc1-dev-k8s,my-app-dc1-npr-k8s,my-app-dc1-prd-k8s,my-app-dev,my-app-dev-done,my-app-prod,my-app-staging,my-app-us-east4-dev-k8s,my-app-us-east4-npr-k8s,my-app-us-east4-npr-k8s-perf,my-app-us-east4-prd-k8s" {
		t.Error("promote-my-app-* union must cover targets + control flow")
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
	if claims(docs(t, kf), "promote-api-dev") != `{"email":["dave@example.com"]}` {
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
// App viewers (apps.<a>.viewers) are included in the viewers SA claims
func TestAppViewers(t *testing.T) {
	t.Parallel()
	r := fixture(t)
	must(t, yamledit.Set(filepath.Join(r, "capabilities/payments/owners.yml"), []string{"apps", "worker", "viewers"}, []string{"erin@example.com"}))
	o := renderCap(t, r, "payments")
	if !strings.Contains(claims(docs(t, filepath.Join(o, "kargo/payments.yaml")), "viewers"), "erin@example.com") {
		t.Error("app viewer missing from viewers SA")
	}
}
