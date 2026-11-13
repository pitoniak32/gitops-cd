package system

import (
	. "github.com/pitoniak32/gitops-cd/platform/cli/internal/emit"
)

// PromotionTasksFile is the platform-level ClusterPromotionTasks file under rendered/ (ADR 0027).
const PromotionTasksFile = "kargo/_promotion-tasks.yaml"

// PlatformFiles are the rendered files that need no system: the promotion tasks, and per region the
// region list entry for hub-side AppSets (git files generator) plus an AppProjects dir, so each region's
// argocd-projects app has a path even without targets.
func PlatformFiles(cfg Config) map[string]string {
	out := map[string]string{PromotionTasksFile: YAMLStream(promotionTasks())}
	for _, r := range cfg.Regions() {
		out["infrastructure/regions/"+r+".json"] = JSON(M("region", r)) + "\n"
		out["argocd/projects/"+r+"/.keep"] = ""
	}
	return out
}

// Shared promotion steps (ADR 0027): one ClusterPromotionTask per deployWith. Every target Stage's
// promotionTemplate is a single `task` step plus vars; Kargo inflates the task's steps into each
// Promotion. Written once to rendered/kargo/_promotion-tasks.yaml (synced by rendered-kargo; `_` can't
// start a system's project name). A change here reaches every system's next Promotion as soon as
// Argo CD syncs it.
//
// Vars come from the Stage's promotionTemplate.spec.vars (Kargo lets Promotion vars satisfy a task's
// vars); kargo-restore reads repo/branch/image there by name. Inside a task, step outputs are
// task.outputs.<alias>.

// GitHub REST for gha targets; token: kargo-shared-resources/github-actions (`gcd creds`)
const ghAPI = "https://api.github.com/repos/${{ vars.workflowRepo }}"

func ghHeaders(more ...Map) List {
	l := List{
		M("name", "Authorization", "value", "Bearer ${{ sharedSecret('github-actions').token }}"),
		M("name", "Accept", "value", "application/vnd.github+json"),
		M("name", "X-GitHub-Api-Version", "value", "2022-11-28"),
	}
	for _, m := range more {
		l = append(l, m)
	}
	return l
}

const (
	// one timeout for every gha run: retry is not an expression, and terraform applies need the longest
	ghaWaitTimeout = "60m"
	// gha-check hard sys; each check's own timeout (vars.checkTimeout, at most this) fails it sooner
	ghaCheckSystem = "2h"
)

// vars every task needs, all set per Stage (none has a default: Kargo fails the Promotion if one is missing)
var (
	promoteVars = []string{"repo", "src", "branch", "image", "system", "app", "stage", "target", "pinFile", "pinKey", "artifactKey"}
	ghaVars     = []string{"workflowRepo", "workflowFile", "workflowRef"}
)

// copy the target folder from the Freight's rendered commit onto the env branch, pin the artifact, push
func promoteSteps(more ...Map) List {
	l := List{
		M("uses", "git-clone", "config", M(
			"repoURL", "${{ vars.repo }}",
			"checkout", List{
				M("commit", "${{ commitFrom(vars.repo).ID }}", "path", "./src"),
				M("branch", "${{ vars.branch }}", "create", true, "path", "./out"),
			})),
		M("uses", "git-clear", "config", M("path", "./out")),
		M("uses", "copy", "config", M("inPath", "${{ vars.src }}", "outPath", "./out/")),
		// plain YAML on the branch (no kustomize); the tag is informational, the digest pins
		M("uses", "yaml-update", "config", M(
			"path", "${{ vars.pinFile }}",
			"updates", List{M(
				"key", "${{ vars.pinKey }}",
				"value", "${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}")})),
		// kargo-restore parses this message
		M("uses", "git-commit", "as", "commit", "config", M(
			"path", "./out",
			"message", "promote ${{ vars.system }}/${{ vars.app }} to ${{ vars.stage }}/${{ vars.target }} from ${{ commitFrom(vars.repo).ID }}, ${{ vars.artifactKey }} ${{ imageFrom(vars.image).Tag }}")),
		M("uses", "git-push", "config", M("path", "./out")),
	}
	for _, m := range more {
		l = append(l, m)
	}
	return l
}

// dispatch the workflow (return_run_details: GitHub answers 200 with the run id). inputs: the inputs that
// differ per task (commit), spliced in before the identity and artifact inputs.
func ghaDispatch(inputs string) Map {
	return M("uses", "http", "as", "dispatch", "config", M(
		"method", "POST",
		"url", ghAPI+"/actions/workflows/${{ vars.workflowFile }}/dispatches",
		"headers", ghHeaders(M("name", "Content-Type", "value", "application/json")),
		// "} }": Kargo ends an expression at the first "}}"; (vars.artifactKey): computed key (image|module)
		"body", "${{ quote({'ref': vars.workflowRef, 'return_run_details': true, 'inputs': {'system': vars.system, 'app': vars.app, 'stage': vars.stage, 'target': vars.target, "+inputs+", (vars.artifactKey): vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest} }) }}",
		"successExpression", "response.status == 200",
		"failureExpression", "response.status != 200",
		"errorExpression", "response.body.message",
		"outputs", List{
			M("name", "runID", "fromExpression", "response.body.workflow_run_id"),
			M("name", "runURL", "fromExpression", "response.body.html_url"),
		}))
}

// poll the dispatched run until it completes; Running (neither expression true) is retried until timeout.
// failure/errorPrefix: extra conditions (gha-check's own timeout), ORed in / checked first. These
// expressions see only `response` (Kargo http step); anything else must be templated in with ${{ }}.
func ghaWait(timeout, failure, errorPrefix string) Map {
	return M("uses", "http", "as", "wait", "retry", M("timeout", timeout), "config", M(
		"method", "GET",
		"url", ghAPI+"/actions/runs/${{ task.outputs.dispatch.runID }}",
		"headers", ghHeaders(),
		"successExpression", "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'",
		// 404 = run not readable yet right after dispatch (GitHub returns its id first): neither
		// expression is true, so the step stays Running and is retried
		"failureExpression", "(response.status != 200 && response.status != 404) || (response.status == 200 && response.body.status == 'completed' && response.body.conclusion != 'success')"+failure,
		"errorExpression", "response.status != 200 ? response.body.message : "+errorPrefix+"'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url"))
}

func promotionTask(name string, vars []string, steps List) Map {
	v := List{}
	for _, n := range vars {
		v = append(v, M("name", n))
	}
	return M("apiVersion", kargoAPI, "kind", "ClusterPromotionTask",
		"metadata", M("name", name, "labels", M("app.kubernetes.io/managed-by", "gitops-platform")),
		"spec", M("vars", v, "steps", steps))
}

func promotionTasks() []any {
	return []any{
		promotionTask("promote-argocd", promoteVars, promoteSteps(
			M("uses", "argocd-update", "config", M("apps", List{M(
				"name", "${{ vars.system }}-${{ vars.app }}-${{ vars.target }}",
				"namespace", "argocd",
				"sources", List{M("repoURL", "${{ vars.repo }}", "desiredRevision", "${{ task.outputs.commit.commit }}")})})))),
		// gha (ADR 0020): dispatch the workflow with the pushed commit, then poll its run until it completes
		promotionTask("promote-gha", append(append([]string{}, promoteVars...), ghaVars...), promoteSteps(
			ghaDispatch("'commit': task.outputs.commit.commit"),
			ghaWait(ghaWaitTimeout, "", ""))),
		// pre-deploy check (ADR 0028): runs before the push, so the commit input is the Freight's rendered
		// commit; the run may take vars.checkTimeout (per check), 2h at most
		promotionTask("gha-check",
			[]string{"repo", "image", "system", "app", "stage", "target", "artifactKey", "workflowRepo", "workflowFile", "workflowRef", "checkTimeout"},
			List{
				ghaDispatch("'commit': commitFrom(vars.repo).ID"),
				ghaWait(ghaCheckSystem,
					" || (response.status == 200 && response.body.status != 'completed' && now() - date(response.body.created_at) > duration('${{ vars.checkTimeout }}'))",
					"response.body.status != 'completed' ? 'run ' + string(response.body.id) + ' exceeded ${{ vars.checkTimeout }}: ' + response.body.html_url : "),
			}),
	}
}

// RegionConfig is the committed config derived from the cluster registry (`gcd regions`).
type RegionConfig struct {
	Regions []string
	// Keycloak public client per regional Argo CD, as JSON objects
	KeycloakClients []string
	KargoArgocdURLs map[string]string
}

// Regions derives the region config from cfg.
func Regions(cfg Config) RegionConfig {
	c := RegionConfig{Regions: cfg.Regions(), KargoArgocdURLs: map[string]string{}}
	for _, r := range c.Regions {
		host := "https://argocd-" + r + ".localhost"
		c.KeycloakClients = append(c.KeycloakClients, JSON(M(
			"clientId", "argocd-"+r,
			"name", "Argo CD ("+r+")",
			"publicClient", true,
			"standardFlowEnabled", true,
			"directAccessGrantsEnabled", false,
			"redirectUris", List{host + "/pkce/verify", host + "/auth/callback", "http://localhost:8085/auth/callback"},
			"webOrigins", List{host},
			"attributes", M("pkce.code.challenge.method", "S256", "post.logout.redirect.uris", host+"/*"))))
		c.KargoArgocdURLs[r] = host
	}
	return c
}
