package kargo

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/kube"
)

const (
	repoURL = "https://github.com/pitoniak32/gitops-cd"
	imgRepo = "ghcr.io/pitoniak32/nginx"
	goodMsg = "promote payments/my-app to dev/us-dev from a35be032c36807442071c8cb1f069718a14b1aa2, image 1.1.0"
	goodImg = "ghcr.io/pitoniak32/nginx:1.1.0@sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6"
)

func TestFreightFromBranch(t *testing.T) {
	f, err := FreightFromBranch("my-app", repoURL, imgRepo, goodMsg, goodImg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1", "kind": "Freight",
		"metadata": map[string]any{},
		"origin":   map[string]any{"kind": "Warehouse", "name": "my-app"},
		"commits":  []any{map[string]any{"repoURL": repoURL, "id": "a35be032c36807442071c8cb1f069718a14b1aa2"}},
		"images": []any{map[string]any{"repoURL": imgRepo, "tag": "1.1.0",
			"digest": "sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6"}},
	}
	got, _ := f.MarshalJSON()
	w := &unstructured.Unstructured{Object: want}
	wj, _ := w.MarshalJSON()
	if string(got) != string(wj) {
		t.Fatalf("got %s\nwant %s", got, wj)
	}

	for _, c := range []struct{ name, msg, img, reason string }{
		{"pre-ADR", "promote payments/worker to dev/us-dev from a4b1dea7d42e7ba001b9292983998890cbaed8b6", "", "no pinned artifact"},
		{"foreign commit", "manual edit", goodImg, "not a Kargo promotion commit"},
		{"other repo", goodMsg, "ghcr.io/other/img:1.0@sha256:abc", "not ghcr.io/pitoniak32/nginx"},
		{"no digest", goodMsg, "ghcr.io/pitoniak32/nginx:1.1.0", "no pinned artifact"},
	} {
		f, err := FreightFromBranch("my-app", repoURL, imgRepo, c.msg, c.img)
		if err == nil || f != nil || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: got %v, %v; want reason %q", c.name, f, err, c.reason)
		}
	}
}

type fakeBranches map[string][2]string // branch -> msg, image

func (f fakeBranches) Head(_, branch string) (string, string, bool, error) {
	v, ok := f[branch]
	return v[0], v[1], ok, nil
}

// projectNS is an app's Kargo project namespace as rendered (ADR 0029): kargo-restore finds it by label.
func projectNS(cap, app string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kargo-" + cap + "-" + app, Labels: map[string]string{
		"kargo.akuity.io/project": "true", "example.com/capability": cap, "example.com/app": app,
	}}}
}

func stage(name, branch string, hasFreight bool) *unstructured.Unstructured {
	return stageIn("my-app", name, branch, hasFreight)
}

func stageIn(app, name, branch string, hasFreight bool) *unstructured.Unstructured {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "kargo.akuity.io/v1alpha1", "kind": "Stage",
		"metadata": map[string]any{"name": name, "namespace": "kargo-payments-" + app},
		"spec": map[string]any{
			"requestedFreight": []any{map[string]any{"origin": map[string]any{"kind": "Warehouse", "name": app}}},
			"promotionTemplate": map[string]any{"spec": map[string]any{"vars": []any{
				map[string]any{"name": "repo", "value": repoURL},
				map[string]any{"name": "branch", "value": branch},
				map[string]any{"name": "image", "value": imgRepo},
			}}},
		},
	}}
	if hasFreight {
		u.Object["status"] = map[string]any{"freightHistory": []any{map[string]any{"items": map[string]any{"x": map[string]any{}}}}}
	}
	return u
}

func TestRestore(t *testing.T) {
	old := PromotionPoll
	PromotionPoll = time.Millisecond
	defer func() { PromotionPoll = old }()

	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		stageGVR: "StageList", freightGVR: "FreightList", promotionGVR: "PromotionList",
	},
		stage("have", "env/payments/my-app/have", true),
		stage("never", "env/payments/my-app/never", false),
		stage("pre", "env/payments/my-app/pre", false),
		stage("dev", "env/payments/my-app/dev", false),
		stageIn("worker", "dev", "env/payments/worker/dev", true), // same Stage name, other app's project
	)
	dyn.PrependReactor("create", "freights", func(a ktesting.Action) (bool, runtime.Object, error) {
		o := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		o.SetName("freight-abc")
		return false, nil, nil // let the tracker store it
	})
	dyn.PrependReactor("create", "promotions", func(a ktesting.Action) (bool, runtime.Object, error) {
		o := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		o.SetName(o.GetGenerateName() + "xyz")
		_ = unstructured.SetNestedField(o.Object, "Succeeded", "status", "phase")
		return false, nil, nil
	})
	// an unlabeled namespace with the old per-capability name is not a project
	core := k8sfake.NewSimpleClientset(projectNS("payments", "my-app"), projectNS("payments", "worker"), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kargo-other"}})
	hub := &kube.Clients{Ctx: "hub", Core: core, Dyn: dyn}

	br := fakeBranches{
		"env/payments/my-app/pre": {"promote payments/my-app to dev/pre from a35be032c36807442071c8cb1f069718a14b1aa2", ""},
		"env/payments/my-app/dev": {goodMsg, goodImg},
	}
	var out bytes.Buffer
	// "other" cap has no namespace
	if err := Restore(context.Background(), t.TempDir(), hub, []string{"payments", "other"}, br, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	got := out.String()
	for _, w := range []string{
		"other: no Kargo project labeled example.com/capability=other (not rendered/synced yet?), skipped",
		"payments/my-app/have: already has Freight, skipped",
		"payments/my-app/never: env/payments/my-app/never doesn't exist (never promoted), skipped",
		"payments/worker/dev: already has Freight, skipped",
		"payments/my-app/pre: no pinned artifact (<repo>:<tag>@<digest>) in deployment.yaml, service.yaml or terraform.yaml (promoted before ADR 0016?), skipped",
		"payments/my-app/dev: promoting 1.1.0 from a35be03 (Freight freight-abc)",
		"dev-restore-xyz: Succeeded",
	} {
		if !strings.Contains(got, w+"\n") {
			t.Errorf("missing line %q in:\n%s", w, got)
		}
	}

	fr, err := dyn.Resource(freightGVR).Namespace("kargo-payments-my-app").Get(context.Background(), "freight-abc", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = fr
	var patched bool
	for _, a := range dyn.Actions() {
		if p, ok := a.(ktesting.PatchAction); ok && a.GetSubresource() == "status" && a.GetResource().Resource == "freights" {
			patched = strings.Contains(string(p.GetPatch()), `"approvedFor":{"dev":{"approvedAt":"`)
		}
	}
	if !patched {
		t.Error("freight not approved for stage dev via status patch")
	}
	ps, _ := dyn.Resource(promotionGVR).Namespace("kargo-payments-my-app").List(context.Background(), metav1.ListOptions{})
	if len(ps.Items) != 1 {
		t.Fatalf("promotions = %d", len(ps.Items))
	}
	spec := ps.Items[0].Object["spec"].(map[string]any)
	if spec["stage"] != "dev" || spec["freight"] != "freight-abc" {
		t.Errorf("spec = %v", spec)
	}
}

func TestRestoreFailedPromotionFails(t *testing.T) {
	old := PromotionPoll
	PromotionPoll = time.Millisecond
	defer func() { PromotionPoll = old }()
	scheme := runtime.NewScheme()
	dyn := fake.NewSimpleDynamicClientWithCustomListKinds(scheme, map[schema.GroupVersionResource]string{
		stageGVR: "StageList", freightGVR: "FreightList", promotionGVR: "PromotionList",
	}, stage("dev", "b", false))
	dyn.PrependReactor("create", "freights", func(a ktesting.Action) (bool, runtime.Object, error) {
		a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured).SetName("f1")
		return false, nil, nil
	})
	dyn.PrependReactor("create", "promotions", func(a ktesting.Action) (bool, runtime.Object, error) {
		o := a.(ktesting.CreateAction).GetObject().(*unstructured.Unstructured)
		o.SetName("p1")
		_ = unstructured.SetNestedField(o.Object, "Failed", "status", "phase")
		return false, nil, nil
	})
	hub := &kube.Clients{Ctx: "hub", Dyn: dyn, Core: k8sfake.NewSimpleClientset(projectNS("payments", "my-app"))}
	err := Restore(context.Background(), t.TempDir(), hub, []string{"payments"}, fakeBranches{"b": {goodMsg, goodImg}}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want error")
	}
}

func TestRestoreDefaultCaps(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "capabilities", "a"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, "capabilities", "b"), 0o755)
	got, err := allCaps(root)
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestClassifyGHStderr(t *testing.T) {
	for _, c := range []struct {
		stderr string
		want   bool
	}{
		{"gh: Not Found (HTTP 404)", true},
		{"gh: No commit found for the ref x (HTTP 404)\n", true},
		{"gh: Bad credentials (HTTP 401)", false},
		{"gh: API rate limit exceeded (HTTP 403)", false},
		{"dial tcp: lookup api.github.com: no such host", false},
		{"", false},
	} {
		if got := ghNotFound(c.stderr); got != c.want {
			t.Errorf("ghNotFound(%q) = %v, want %v", c.stderr, got, c.want)
		}
	}
}

func TestRestoreMalformedStages(t *testing.T) {
	bad := func(name string, spec map[string]any, status map[string]any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "kargo.akuity.io/v1alpha1", "kind": "Stage",
			"metadata": map[string]any{"name": name, "namespace": "kargo-payments-my-app"},
			"spec":     spec, "status": status,
		}}
	}
	tmpl := map[string]any{"spec": map[string]any{}}
	stages := []unstructured.Unstructured{
		*bad("rf-string", map[string]any{"promotionTemplate": tmpl, "requestedFreight": []any{"oops"}}, nil),
		*bad("hist-string", map[string]any{"promotionTemplate": tmpl}, map[string]any{"freightHistory": []any{"oops"}}),
	}
	ts := targets(stages) // must not panic
	if len(ts) != 2 || ts[0].warehouse != "" || ts[1].hasFreight {
		t.Fatalf("targets = %+v", ts)
	}
	// findFreight with malformed commits/images must not panic either
	f := &unstructured.Unstructured{Object: map[string]any{"commits": []any{"x"}, "images": []any{"y"}}}
	_ = findFreight([]unstructured.Unstructured{*f}, f)
}

const (
	modRepo = "ghcr.io/pitoniak32/tf-bucket"
	modPin  = modRepo + ":1.2.3@sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6"
	modMsg  = "promote payments/infra to dev/tf-dev from a35be032c36807442071c8cb1f069718a14b1aa2, module 1.2.3"
)

func TestTerraformModule(t *testing.T) {
	if got := terraformModule([]byte("capability: payments\nmodule: " + modPin + "\n")); got != modPin {
		t.Errorf("got %q want %q", got, modPin)
	}
	if got := terraformModule([]byte("capability: payments\n")); got != "" {
		t.Errorf("missing module: got %q", got)
	}
}

func TestFreightFromTerraformBranch(t *testing.T) {
	f, err := FreightFromBranch("infra", repoURL, modRepo, modMsg, modPin)
	if err != nil {
		t.Fatal(err)
	}
	im := f.Object["images"].([]any)[0].(map[string]any)
	if im["repoURL"] != modRepo || im["tag"] != "1.2.3" || im["digest"] != "sha256:c8fa106acc6d713fc0cf8c00906b1d54157ff9cb77f6aa3f65ba1c27348e4ce6" {
		t.Errorf("images: %v", im)
	}
	// rendered but never promoted: module is the bare repo
	if _, err := FreightFromBranch("infra", repoURL, modRepo, modMsg, modRepo); err == nil || !strings.Contains(err.Error(), "no pinned artifact") || !strings.Contains(err.Error(), "terraform.yaml") {
		t.Errorf("unpinned module: %v", err)
	}
}

func TestBranchArtifact(t *testing.T) {
	files := func(m map[string]string) func(string) ([]byte, bool) {
		return func(p string) ([]byte, bool) { v, ok := m[p]; return []byte(v), ok }
	}
	dep := "spec:\n  template:\n    spec:\n      containers:\n        - image: " + goodImg + "\n"
	for _, c := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"k8s", map[string]string{"deployment.yaml": dep}, goodImg},
		{"cloud run", map[string]string{"service.yaml": dep}, goodImg},
		{"terraform", map[string]string{"terraform.yaml": "module: " + modPin + "\n"}, modPin},
		{"neither", map[string]string{"manifests.yaml": "x: 1\n"}, ""},
	} {
		if got := branchArtifact(files(c.files)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
