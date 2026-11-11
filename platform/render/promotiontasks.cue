package render

import (
	"encoding/yaml"
	"list"
)

// Shared promotion steps (ADR 0027): one ClusterPromotionTask per deployWith. Every target Stage's
// promotionTemplate is a single `task` step plus the vars below; Kargo inflates the task's steps into each
// Promotion. Platform-level (no capability input; loaded with clusters.cue regions.cue): written once to
// rendered/kargo/_promotion-tasks.yaml (synced by rendered-kargo; `_` can't start a capability's project name).
// A change here reaches every capability's next Promotion as soon as Argo CD syncs it.
//
// Vars come from the Stage's promotionTemplate.spec.vars (Kargo lets Promotion vars satisfy a task's vars);
// kargo-restore reads repo/branch/image there by name. Inside a task, step outputs are task.outputs.<alias>.

_kargoAPI: "kargo.akuity.io/v1alpha1"

// GitHub REST for gha targets; token: kargo-shared-resources/github-actions (`gcd creds`)
_ghAPI: "https://api.github.com/repos/${{ vars.workflowRepo }}"
_ghHeaders: [
	{name: "Authorization", value: "Bearer ${{ sharedSecret('github-actions').token }}"},
	{name: "Accept", value: "application/vnd.github+json"},
	{name: "X-GitHub-Api-Version", value: "2022-11-28"},
]
// one timeout for every gha run: retry is not an expression, and terraform applies need the longest
_ghaWaitTimeout: "60m"

// vars every task needs, all set per Stage (none has a default: Kargo fails the Promotion if one is missing)
_promoteVars: ["repo", "src", "branch", "image", "capability", "app", "stage", "target", "pinFile", "pinKey", "artifactKey"]
_ghaVars: ["workflowRepo", "workflowFile", "workflowRef"]

// copy the target folder from the Freight's rendered commit onto the env branch, pin the artifact, push
_promoteSteps: [
	{uses: "git-clone", config: {
		repoURL: "${{ vars.repo }}"
		checkout: [
			{commit: "${{ commitFrom(vars.repo).ID }}", path: "./src"},
			{branch: "${{ vars.branch }}", create: true, path: "./out"},
		]
	}},
	{uses: "git-clear", config: path: "./out"},
	{uses: "copy", config: {inPath: "${{ vars.src }}", outPath: "./out/"}},
	// plain YAML on the branch (no kustomize); the tag is informational, the digest pins
	{uses: "yaml-update", config: {
		path: "${{ vars.pinFile }}"
		updates: [{
			key:   "${{ vars.pinKey }}"
			value: "${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}"
		}]
	}},
	// kargo-restore parses this message
	{uses: "git-commit", as: "commit", config: {
		path:    "./out"
		message: "promote ${{ vars.capability }}/${{ vars.app }} to ${{ vars.stage }}/${{ vars.target }} from ${{ commitFrom(vars.repo).ID }}, ${{ vars.artifactKey }} ${{ imageFrom(vars.image).Tag }}"
	}},
	{uses: "git-push", config: path: "./out"},
]

_promotionTask: {
	#name: string
	#vars: [...string]
	#steps: [...]
	apiVersion: _kargoAPI
	kind:       "ClusterPromotionTask"
	metadata: {
		name: #name
		labels: "app.kubernetes.io/managed-by": "gitops-platform"
	}
	spec: {
		vars: [for n in #vars {name: n}]
		steps: #steps
	}
}

promotionTasks: [
	_promotionTask & {
		#name: "promote-argocd"
		#vars: _promoteVars
		#steps: list.Concat([_promoteSteps, [
			{uses: "argocd-update", config: apps: [{
				name:      "${{ vars.capability }}-${{ vars.app }}-${{ vars.target }}"
				namespace: "argocd"
				sources: [{repoURL: "${{ vars.repo }}", desiredRevision: "${{ task.outputs.commit.commit }}"}]
			}]},
		]])
	},
	// gha (ADR 0020): dispatch the workflow with the pushed commit, then poll its run until it completes;
	// Running (neither expression true) is retried until retry.timeout
	_promotionTask & {
		#name: "promote-gha"
		#vars: list.Concat([_promoteVars, _ghaVars])
		#steps: list.Concat([_promoteSteps, [
			{uses: "http", as: "dispatch", config: {
				method: "POST"
				url:    "\(_ghAPI)/actions/workflows/${{ vars.workflowFile }}/dispatches"
				headers: list.Concat([_ghHeaders, [{name: "Content-Type", value: "application/json"}]])
				// "} }": Kargo ends an expression at the first "}}"; (vars.artifactKey): computed key (image|module)
				body:              "${{ quote({'ref': vars.workflowRef, 'return_run_details': true, 'inputs': {'capability': vars.capability, 'app': vars.app, 'stage': vars.stage, 'target': vars.target, 'commit': task.outputs.commit.commit, (vars.artifactKey): vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest} }) }}"
				successExpression: "response.status == 200"
				failureExpression: "response.status != 200"
				errorExpression:   "response.body.message"
				outputs: [
					{name: "runID", fromExpression: "response.body.workflow_run_id"},
					{name: "runURL", fromExpression: "response.body.html_url"},
				]
			}},
			{uses: "http", as: "wait", retry: timeout: _ghaWaitTimeout, config: {
				method:            "GET"
				url:               "\(_ghAPI)/actions/runs/${{ task.outputs.dispatch.runID }}"
				headers:           _ghHeaders
				successExpression: "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'"
				// 404 = run not readable yet right after dispatch (GitHub returns its id first): neither expression
				// is true, so the step stays Running and is retried
				failureExpression: "(response.status != 200 && response.status != 404) || (response.status == 200 && response.body.status == 'completed' && response.body.conclusion != 'success')"
				errorExpression:   "response.status != 200 ? response.body.message : 'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url"
			}},
		]])
	},
]

// rendered/ path (gcd render, with regionFiles)
promotionTaskFiles: "kargo/_promotion-tasks.yaml": yaml.MarshalStream(promotionTasks)
