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

// promotionTasks are the platform-level ClusterPromotionTasks of the shared render (ADR 0027).
func promotionTasks(t *testing.T) []map[string]any {
	t.Helper()
	return docs(t, filepath.Join(sharedOut(t), PromotionTasksFile))
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

// taskOf is the ClusterPromotionTask a target Stage's single template step references.
func taskOf(t *testing.T, ds []map[string]any, name string) map[string]any {
	t.Helper()
	steps := list(stage(ds, name), "spec.promotionTemplate.spec.steps")
	if len(steps) != 1 || str(get(steps[0], "task.kind")) != "ClusterPromotionTask" {
		t.Fatalf("Stage %s must have exactly one ClusterPromotionTask step", name)
	}
	n := str(get(steps[0], "task.name"))
	task := find(promotionTasks(t), "ClusterPromotionTask", n)
	if task == nil {
		t.Fatalf("Stage %s: no ClusterPromotionTask %s", name, n)
	}
	return task
}

// taskSteps are the steps Kargo inflates into a Stage's Promotions.
func taskSteps(t *testing.T, ds []map[string]any, name string) []any {
	t.Helper()
	return list(taskOf(t, ds, name), "spec.steps")
}

func stepUses(t *testing.T, ds []map[string]any, name string) []string {
	t.Helper()
	var out []string
	for _, s := range taskSteps(t, ds, name) {
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

// varValue is a Stage's promotionTemplate var by name ("" when absent).
func varValue(ds []map[string]any, name, v string) string {
	for _, x := range list(stage(ds, name), "spec.promotionTemplate.spec.vars") {
		if str(get(x, "name")) == v {
			return str(get(x, "value"))
		}
	}
	return ""
}

// stepAs finds the inflated step with `as: <as>` (dispatch, wait).
func stepAs(t *testing.T, ds []map[string]any, name, as string) map[string]any {
	t.Helper()
	for _, s := range taskSteps(t, ds, name) {
		if str(get(s, "as")) == as {
			return s.(map[string]any)
		}
	}
	return nil
}

func stepUsing(t *testing.T, ds []map[string]any, name, uses string) map[string]any {
	t.Helper()
	for _, s := range taskSteps(t, ds, name) {
		if str(get(s, "uses")) == uses {
			return s.(map[string]any)
		}
	}
	return nil
}

const (
	commonVars = "repo,src,branch,image,capability,app,stage,target,pinFile,pinKey,artifactKey"
	ghaVars    = commonVars + ",workflowRepo,workflowFile,workflowRef"
)

// Every target Stage references an existing ClusterPromotionTask and sets every var it declares
// (Kargo fails the Promotion otherwise: the task vars have no defaults).
func TestStagesSatisfyPromotionTasks(t *testing.T) {
	t.Parallel()
	out := sharedOut(t)
	for _, f := range []string{"payments", "search"} {
		for _, s := range findAll(docs(t, filepath.Join(out, "kargo", f+".yaml")), "Stage") {
			if label(s, "example.com/kind") != "target" {
				continue
			}
			n := str(get(s, "metadata.name"))
			task := taskOf(t, []map[string]any{s}, n)
			if want := "promote-" + label(s, "example.com/deploy-with"); str(get(task, "metadata.name")) != want {
				t.Errorf("%s: task %s, want %s", n, str(get(task, "metadata.name")), want)
			}
			for _, v := range list(task, "spec.vars") {
				if varValue([]map[string]any{s}, n, str(get(v, "name"))) == "" {
					t.Errorf("%s: no value for task var %s", n, str(get(v, "name")))
				}
			}
		}
	}
	var tn []string
	for _, x := range promotionTasks(t) {
		if get(x, "metadata.namespace") != nil {
			t.Error("ClusterPromotionTask must be cluster-scoped")
		}
		tn = append(tn, str(x["kind"])+"/"+str(get(x, "metadata.name")))
	}
	if strings.Join(tn, " ") != "ClusterPromotionTask/promote-argocd ClusterPromotionTask/promote-gha ClusterPromotionTask/gha-check" {
		t.Errorf("promotion tasks: %s", strings.Join(tn, " "))
	}
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
	if varNames(k, w) != ghaVars {
		t.Error("gha vars (kargo-restore reads repo/branch/image by name)")
	}
	if varNames(k, "worker-us-east4-npr-k8s") != commonVars {
		t.Error("argocd Stage has no workflow vars")
	}
	wf := func(n string) string {
		return varValue(k, n, "workflowRepo") + " " + varValue(k, n, "workflowFile") + " " + varValue(k, n, "workflowRef")
	}
	if wf(w) != "pitoniak32/gitops-cd deploy.yaml main" {
		t.Error("default workflow")
	}
	if wf("my-app-dc1-npr-k8s") != "acme/deploys onprem.yml v1" {
		t.Error("target workflow override")
	}
	if varValue(k, w, "capability")+" "+varValue(k, w, "app")+" "+varValue(k, w, "stage")+" "+varValue(k, w, "target")+" "+varValue(k, w, "artifactKey") != "payments worker dev us-east4-dev-k8s image" {
		t.Error("identity vars (dispatch inputs, commit message)")
	}
	d := stepAs(t, k, w, "dispatch")
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
	// inside a task, outputs of its other steps are task.outputs.<alias>
	for _, f := range []string{"'ref': vars.workflowRef", "'return_run_details': true", "'commit': task.outputs.commit.commit", "'target': vars.target", "(vars.artifactKey): vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest"} {
		if !strings.Contains(str(get(d, "config.body")), f) {
			t.Errorf("dispatch body missing %s", f)
		}
	}
	wt := stepAs(t, k, w, "wait")
	if str(get(wt, "retry.timeout"))+" "+str(get(wt, "config.url")) != `60m https://api.github.com/repos/${{ vars.workflowRepo }}/actions/runs/${{ task.outputs.dispatch.runID }}` {
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
	au := stepUsing(t, k, "worker-us-east4-npr-k8s", "argocd-update")
	if str(get(au, "config.apps.0.name"))+" "+str(get(au, "config.apps.0.sources.0.desiredRevision")) != "${{ vars.capability }}-${{ vars.app }}-${{ vars.target }} ${{ task.outputs.commit.commit }}" {
		t.Error("argocd-update app name / revision")
	}
}

// gha-check (ADR 0028): pre-deploy check dispatched before anything is pushed; same dispatch/wait as
// promote-gha, but inputs come from the Freight and the wait has a per-check timeout under a 2h cap.
func TestGhaCheckTask(t *testing.T) {
	t.Parallel()
	ts := promotionTasks(t)
	c := find(ts, "ClusterPromotionTask", "gha-check")
	g := find(ts, "ClusterPromotionTask", "promote-gha")
	if c == nil || g == nil {
		t.Fatal("need ClusterPromotionTasks gha-check and promote-gha")
	}
	var vs []string
	for _, v := range list(c, "spec.vars") {
		vs = append(vs, str(get(v, "name")))
	}
	if strings.Join(vs, ",") != "repo,image,capability,app,stage,target,artifactKey,workflowRepo,workflowFile,workflowRef,checkTimeout" {
		t.Errorf("gha-check vars: %s", strings.Join(vs, ","))
	}
	stepByAs := func(task map[string]any, as string) map[string]any {
		for _, s := range list(task, "spec.steps") {
			if str(get(s, "as")) == as {
				return s.(map[string]any)
			}
		}
		t.Fatalf("%s: no step %s", str(get(task, "metadata.name")), as)
		return nil
	}
	var uses []string
	for _, s := range list(c, "spec.steps") {
		uses = append(uses, str(get(s, "uses"))+":"+str(get(s, "as")))
	}
	if strings.Join(uses, ",") != "http:dispatch,http:wait" {
		t.Errorf("gha-check steps: %s", strings.Join(uses, ","))
	}
	cd, gd := stepByAs(c, "dispatch"), stepByAs(g, "dispatch")
	for _, f := range []string{"config.url", "config.method", "config.successExpression", "config.failureExpression", "config.errorExpression"} {
		if str(get(cd, f)) != str(get(gd, f)) {
			t.Errorf("dispatch %s differs from promote-gha", f)
		}
	}
	if len(list(cd, "config.headers")) != len(list(gd, "config.headers")) {
		t.Error("dispatch headers differ from promote-gha")
	}
	body := str(get(cd, "config.body"))
	for _, f := range []string{"'commit': commitFrom(vars.repo).ID", "'return_run_details': true", "'ref': vars.workflowRef", "(vars.artifactKey): vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest"} {
		if !strings.Contains(body, f) {
			t.Errorf("gha-check dispatch body missing %s", f)
		}
	}
	if strings.Contains(body, "task.outputs.commit") {
		t.Error("gha-check runs before the push: no commit output to read")
	}
	cw, gw := stepByAs(c, "wait"), stepByAs(g, "wait")
	if str(get(cw, "retry.timeout")) != "2h" {
		t.Errorf("gha-check wait cap: %s", str(get(cw, "retry.timeout")))
	}
	if str(get(cw, "config.url")) != str(get(gw, "config.url")) || str(get(cw, "config.successExpression")) != str(get(gw, "config.successExpression")) {
		t.Error("gha-check wait url/success differ from promote-gha")
	}
	if want := str(get(gw, "config.failureExpression")) + " || (response.status == 200 && response.body.status != 'completed' && now() - date(response.body.created_at) > duration(vars.checkTimeout))"; str(get(cw, "config.failureExpression")) != want {
		t.Errorf("gha-check wait failure:\n got %s\nwant %s", str(get(cw, "config.failureExpression")), want)
	}
	if !strings.Contains(str(get(cw, "config.errorExpression")), "' exceeded ' + vars.checkTimeout") {
		t.Error("gha-check wait error must name the exceeded timeout")
	}
}
