package render

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pitoniak32/gitops-cd/platform/cli/internal/yamledit"
)

// test.sh: "cloud run render ok" (ADR 0024)
func TestCloudRunRender(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	cr := filepath.Join(out, "apps/payments/web/staging/us-east4-npr-cr")
	ents, err := os.ReadDir(cr)
	if err != nil {
		t.Fatal(err)
	}
	var ns []string
	for _, e := range ents {
		ns = append(ns, e.Name())
	}
	if strings.Join(ns, "\n") != "service.yaml" {
		t.Errorf("CLOUD_RUN target renders only service.yaml: %s", strings.Join(ns, " "))
	}
	sf := filepath.Join(cr, "service.yaml")
	svc := docs(t, sf)[0]
	if str(svc["kind"])+" "+str(svc["apiVersion"])+" "+str(get(svc, "metadata.namespace"))+" "+str(get(svc, `metadata.labels."cloud.googleapis.com/location"`)) != "Service serving.knative.dev/v1 acme-payments-npr us-east4" {
		t.Error("service.yaml header + location label")
	}
	if get(svc, "metadata.annotations") != nil {
		t.Error("service.yaml: no run.googleapis.com/location annotation")
	}
	if orderedJSON(t, sf, "spec.template.metadata.labels") != `{"app":"web","managed-by":"cue-platform","capability":"payments","stage":"staging","target":"us-east4-npr-cr","environment":"staging","environment-type":"npr","billing":"payments"}` {
		t.Error("service.yaml template labels (GCP-safe keys)")
	}
	safe := regexp.MustCompile(`^[a-z0-9_.-]+$`)
	for _, p := range []string{"metadata.labels", "spec.template.metadata.labels"} {
		m, _ := get(svc, p).(map[string]any)
		for k := range m {
			if k != "cloud.googleapis.com/location" && !safe.MatchString(k) {
				t.Error("service.yaml label keys must be GCP-safe")
			}
		}
	}
	if str(get(svc, `spec.template.metadata.annotations."autoscaling.knative.dev/minScale"`))+" "+str(get(svc, "spec.template.spec.containers.0.ports.0.containerPort"))+" "+str(get(svc, "spec.template.spec.containers.0.image")) != "0 8080 ghcr.io/example/web" {
		t.Error("service.yaml minScale 0 / port 8080 / image repo")
	}
	if orderedJSON(t, sf, "spec.template.spec.containers.0.resources") != `{"limits":{"cpu":"1","memory":"512Mi"}}` {
		t.Error("CLOUD_RUN default cpu 1 / memory 512Mi")
	}
	if get(svc, "spec.template.spec.containerConcurrency") != nil {
		t.Error("no containerConcurrency at 1 vCPU")
	}
	if str(get(svc, `spec.template.metadata.annotations."run.googleapis.com/secrets"`)) != "acme-secrets-web-api-key:projects/acme-secrets/secrets/web-api-key" {
		t.Error("service.yaml secrets alias annotation (cross-project only)")
	}
	if orderedJSON(t, sf, "spec.template.spec.containers.0.env") != `[{"name":"API_KEY","valueFrom":{"secretKeyRef":{"name":"acme-secrets-web-api-key","key":"latest"}}},{"name":"DB_PASS","valueFrom":{"secretKeyRef":{"name":"db_pass","key":"3"}}},{"name":"GREETING","value":"hello"}]` {
		t.Error("service.yaml env (cross-project alias, same-project direct name + version, sorted)")
	}

	// below 1 vCPU Cloud Run needs containerConcurrency 1
	c := fixture(t)
	must(t, yamledit.Set(filepath.Join(c, "capabilities/payments/apps/web/app.yml"), []string{"stages", "staging", "targets", "us-east4-npr-cr", "resources", "cpu"}, "500m"))
	o := renderCap(t, c, "payments")
	s2 := docs(t, filepath.Join(o, "apps/payments/web/staging/us-east4-npr-cr/service.yaml"))[0]
	if str(get(s2, "spec.template.spec.containerConcurrency"))+" "+str(get(s2, "spec.template.spec.containers.0.resources.limits.cpu")) != "1 500m" {
		t.Error("cpu 500m must set containerConcurrency 1")
	}

	k := kargoPayments(t)
	const w = "web-us-east4-npr-cr"
	shard := st(t, k, w, "spec.shard")
	if shard == "" {
		shard = "-"
	}
	if shard+" "+st(t, k, w, `metadata.labels."example.com/platform"`)+" "+st(t, k, w, `metadata.labels."example.com/deploy-with"`) != "- cloudrun gha" {
		t.Error("web-us-east4-npr-cr: no shard, cloudrun, gha")
	}
	yu := stepUsing(k, w, "yaml-update")
	if str(get(yu, "config.path"))+" "+str(get(yu, "config.updates.0.key")) != "./out/service.yaml spec.template.spec.containers.0.image" {
		t.Error("cloud run yaml-update")
	}
	wf := ""
	for _, v := range list(stage(k, w), "spec.promotionTemplate.spec.vars") {
		if str(get(v, "name")) == "workflowFile" {
			wf = str(get(v, "value"))
		}
	}
	if wf != "cloud-run.yaml" {
		t.Error("cloud run workflow default")
	}
	if str(get(stepAs(k, w, "wait"), "retry.timeout")) != "30m" {
		t.Error("cloud run wait timeout 30m")
	}
	us := stepUses(t, k, "web-us-east4-dev-k8s")
	if st(t, k, "web-us-east4-dev-k8s", `metadata.labels."example.com/platform"`)+" "+st(t, k, "web-us-east4-dev-k8s", `metadata.labels."example.com/deploy-with"`)+" "+us[len(us)-1] != "k8s argocd argocd-update" {
		t.Error("web K8S stage stays argocd")
	}
	if _, err := os.Stat(filepath.Join(out, "argocd/deploys/us-east4/payments/web-us-east4-dev-k8s.json")); err != nil {
		t.Error("web K8S target needs Argo CD params")
	}
	if m := globOne(t, filepath.Join(out, "argocd/deploys/*/payments/web-us-east4-npr-cr.json")); len(m) > 0 {
		t.Error("CLOUD_RUN targets must not get Argo CD params")
	}
	n := 0
	for _, wh := range findAll(k, "Warehouse") {
		if str(get(wh, "metadata.name")) == "web" {
			n++
		}
	}
	if n != 1 {
		t.Error("mixed app needs exactly one Warehouse")
	}
}
