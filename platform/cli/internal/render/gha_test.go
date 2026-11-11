package render

import (
	"path/filepath"
	"strings"
	"testing"
)

// kargoPayments are the Kargo docs of the shared fixture render.
func kargoPayments(t *testing.T) []map[string]any {
	t.Helper()
	return docs(t, filepath.Join(sharedOut(t), "kargo/payments.yaml"))
}

// st mirrors test.sh's st(): a field of one Stage; "" when absent.
func st(t *testing.T, ds []map[string]any, name, path string) string {
	t.Helper()
	s := stage(ds, name)
	if s == nil {
		t.Fatalf("no Stage %s", name)
	}
	return str(get(s, path))
}

func stepUses(t *testing.T, ds []map[string]any, name string) []string {
	t.Helper()
	var out []string
	for _, s := range list(stage(ds, name), "spec.promotionTemplate.spec.steps") {
		out = append(out, str(get(s, "uses")))
	}
	return out
}

func varNames(ds []map[string]any, name string) string {
	var out []string
	for _, v := range list(stage(ds, name), "spec.promotionTemplate.spec.vars") {
		out = append(out, str(get(v, "name")))
	}
	return strings.Join(out, ",")
}

// varValuesFrom4 is `.vars[4:] | map(.value) | join(" ")`.
func varValuesFrom4(ds []map[string]any, name string) string {
	var out []string
	vs := list(stage(ds, name), "spec.promotionTemplate.spec.vars")
	if len(vs) > 4 {
		vs = vs[4:]
	} else {
		vs = nil
	}
	for _, v := range vs {
		out = append(out, str(get(v, "value")))
	}
	return strings.Join(out, " ")
}

// stepAs finds the step with `as: <as>` (dispatch, wait).
func stepAs(ds []map[string]any, name, as string) map[string]any {
	for _, s := range list(stage(ds, name), "spec.promotionTemplate.spec.steps") {
		if str(get(s, "as")) == as {
			return s.(map[string]any)
		}
	}
	return nil
}

func stepUsing(ds []map[string]any, name, uses string) map[string]any {
	for _, s := range list(stage(ds, name), "spec.promotionTemplate.spec.steps") {
		if str(get(s, "uses")) == uses {
			return s.(map[string]any)
		}
	}
	return nil
}

// test.sh: "gha targets ok"
func TestGhaTargets(t *testing.T) {
	t.Parallel()
	k := kargoPayments(t)
	const w = "worker-us-east4-dev-k8s"
	if got := strings.Join(stepUses(t, k, w), ","); got != "git-clone,git-clear,copy,yaml-update,git-commit,git-push,http,http" {
		t.Error("worker-us-east4-dev-k8s steps")
	}
	if u := stepUses(t, k, "worker-us-east4-npr-k8s"); u[len(u)-1] != "argocd-update" {
		t.Error("worker-us-east4-npr-k8s must stay argocd")
	}
	if st(t, k, w, `metadata.labels."example.com/deploy-with"`)+" "+st(t, k, "worker-us-east4-npr-k8s", `metadata.labels."example.com/deploy-with"`) != "gha argocd" {
		t.Error("deploy-with label")
	}
	if varNames(k, w) != "repo,src,branch,image,workflowRepo,workflowFile,workflowRef" {
		t.Error("vars order (kargo-restore reads by name)")
	}
	if varNames(k, "worker-us-east4-npr-k8s") != "repo,src,branch,image" {
		t.Error("argocd Stage keeps its four vars")
	}
	if varValuesFrom4(k, w) != "pitoniak32/gitops-cd deploy.yaml main" {
		t.Error("default workflow")
	}
	if varValuesFrom4(k, "my-app-dc1-npr-k8s") != "acme/deploys onprem.yml v1" {
		t.Error("target workflow override")
	}
	d := stepAs(k, w, "dispatch")
	if str(get(d, "config.method"))+" "+str(get(d, "config.url")) != `POST https://api.github.com/repos/${{ vars.workflowRepo }}/actions/workflows/${{ vars.workflowFile }}/dispatches` {
		t.Error("dispatch url")
	}
	auth := ""
	for _, h := range list(d, "config.headers") {
		if str(get(h, "name")) == "Authorization" {
			auth = str(get(h, "value"))
		}
	}
	if auth != "Bearer ${{ sharedSecret('github-actions').token }}" {
		t.Error("auth header")
	}
	if str(get(d, "config.successExpression"))+" | "+str(get(d, "config.failureExpression"))+" | "+str(get(d, "config.errorExpression")) != "response.status == 200 | response.status != 200 | response.body.message" {
		t.Error("dispatch expressions")
	}
	var outs []string
	for _, o := range list(d, "config.outputs") {
		outs = append(outs, str(get(o, "name"))+"="+str(get(o, "fromExpression")))
	}
	if strings.Join(outs, ",") != "runID=response.body.workflow_run_id,runURL=response.body.html_url" {
		t.Error("dispatch outputs")
	}
	for _, f := range []string{"'ref': vars.workflowRef", "'return_run_details': true", "'commit': outputs.commit.commit", "'target': 'us-east4-dev-k8s'", "imageFrom(vars.image).Digest"} {
		if !strings.Contains(str(get(d, "config.body")), f) {
			t.Errorf("dispatch body missing %s", f)
		}
	}
	wt := stepAs(k, w, "wait")
	if str(get(wt, "retry.timeout"))+" "+str(get(wt, "config.url")) != `30m https://api.github.com/repos/${{ vars.workflowRepo }}/actions/runs/${{ outputs.dispatch.runID }}` {
		t.Error("wait step")
	}
	if str(get(wt, "config.successExpression")) != "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'" {
		t.Error("wait success")
	}
	// 404: the dispatched run isn't readable yet (GitHub returns its id before the API serves it): keep polling
	if str(get(wt, "config.failureExpression")) != "(response.status != 200 && response.status != 404) || (response.status == 200 && response.body.status == 'completed' && response.body.conclusion != 'success')" {
		t.Error("wait failure (404 must keep polling)")
	}
	if str(get(wt, "config.errorExpression")) != "response.status != 200 ? response.body.message : 'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url" {
		t.Error("wait error message (non-200 has no run fields)")
	}
}
