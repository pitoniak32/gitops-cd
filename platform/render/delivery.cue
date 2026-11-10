package render

import (
	"encoding/json"
	"list"
	"strings"
)

// Promotion model (rendered-manifests + Kargo), one Kargo Stage per target (ADR 0012):
//   CI renders every (app, stage, target) to <repo>@main: rendered/apps/<cap>/<app>/<stage>/<target>/
//   Kargo Warehouse per app watches that path and the app's image repo (SemVer tags); Freight creation
//   is Manual: promoters assemble Freight = (config commit, image digest) in the UI (ADR 0016).
//   Target Stage <app>-<target> takes Freight from its upstream node (graph.cue, ADR 0019: the Warehouse,
//   upstream targets, or a control-flow Stage where many targets feed many or a stage needs a one-click
//   release point) and copies its target folder from that commit onto branch env/<cap>/<app>/<target>,
//   pins the Freight's image in its deployment.yaml, then syncs that target's Argo CD app.
//   Argo CD Application <cap>-<app>-<target> tracks that branch, path ".".
//   deployWith: gha targets (ADR 0020) get no Application: after the push the Stage dispatches a GitHub
//   Actions workflow (http step, PAT from the shared `github-actions` secret) and waits for its run.

_kargoProject: "kargo-\(_cap)"

// GitHub REST for gha targets; token: kargo-shared-resources/github-actions (`gcd creds`)
_ghAPI: "https://api.github.com/repos/${{ vars.workflowRepo }}"
_ghHeaders: [
	{name: "Authorization", value: "Bearer ${{ sharedSecret('github-actions').token }}"},
	{name: "Accept", value: "application/vnd.github+json"},
	{name: "X-GitHub-Api-Version", value: "2022-11-28"},
]
_kargoAPI: "kargo.akuity.io/v1alpha1"
// per deploy kind: what the Freight's image subscription pins (dispatch input, commit message), and
// how long a gha run may take (terraform applies are slower)
_artifactKey: {k8s: "image", terraform: "module"}
_waitTimeout: {k8s: "30m", terraform: "60m"}

// Kargo Stage names per app (graph.cue): target Stages <app>-<target> + control-flow Stages
_tgtName: {#app: string, #target: string, out: "\(#app)-\(#target)"}
_kargoStages: {for an, _ in pipelines {(an): list.SortStrings([for n, _ in _graph[an] {n}])}}
// auto-promoted: every control-flow Stage (it only means "upstream verified") + targets of autoPromote stages
_autoStages: list.SortStrings([for an, p in pipelines for n, x in _graph[an]
	if x.kind == "control-flow" || p.stages[x.stage].autoPromote {n}])
_stageAnnotations: {
	#app:   string
	#stage: string

	// read by the break-glass guard (platform/infrastructure/break-glass): who may approve unverified
	// Freight for this Stage (ADR 0013)
	"example.com/break-glass": json.Marshal(_breakGlass[#app])
	// Kargo UI: a stage's control-flow and target Stages share its color
	"kargo.akuity.io/color": stages[#stage].color
}
_stageLabels: {
	#app:                     string
	#stage:                   string
	"example.com/capability": _cap
	"example.com/app":        #app
	"example.com/stage":      #stage
}

// a Stage's Freight request from its graph node: the Warehouse directly, or upstream Stages (All when
// several must have verified it)
_freightRequest: {
	#app:  string
	#node: #GraphNode
	origin: {kind: "Warehouse", name: #app}
	sources: [
		if #node.direct {direct: true},
		{
			stages: #node.sources
			if #node.all {availabilityStrategy: "All"}
		},
	][0]
}

_kargoCore: [
	// Rendered (not left to Kargo) so Argo CD's dry-run of the namespaced resources below passes on
	// first sync; Kargo adopts a pre-existing namespace carrying this label.
	{apiVersion: "v1", kind: "Namespace", metadata: {name: _kargoProject, labels: "kargo.akuity.io/project": "true"}},
	{apiVersion: _kargoAPI, kind: "Project", metadata: name: _kargoProject},
	{
		apiVersion: _kargoAPI
		kind:       "ProjectConfig"
		metadata: {name: _kargoProject, namespace: _kargoProject}
		// a stage's autoPromote covers its targets; control-flow Stages always auto-promote
		spec: promotionPolicies: [for n in _autoStages {stageSelector: name: n, autoPromotionEnabled: true}]
	},
	for an, _ in pipelines {
		apiVersion: _kargoAPI
		kind:       "Warehouse"
		metadata: {name: an, namespace: _kargoProject}
		spec: {
			freightCreationPolicy: "Manual"
			subscriptions: [{git: {
				repoURL: _repoURL
				branch:  "main"
				includePaths: ["regexp:^\(renderedDir)/apps/\(_cap)/\(an)/"]
			}}, {image: {
				repoURL: [if apps[an].module != _|_ {apps[an].module}, apps[an].image][0]
				imageSelectionStrategy: "SemVer"
				// a terraform module (OCI artifact, ADR 0021) is discovered like an image; never set
				// `platform` here: artifacts have none, so every tag would be filtered out
				if apps[an].imageConstraint != _|_ {constraint: apps[an].imageConstraint}
				if apps[an].moduleConstraint != _|_ {constraint: apps[an].moduleConstraint}
				discoveryLimit: 10
			}}]
		}
	},
	// control-flow Stages (no promotionTemplate, hub controller): Freight promoted here is verified at once
	for an, _ in pipelines for n, x in _graph[an] if x.kind == "control-flow" {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name:      n
			namespace: _kargoProject
			labels: _stageLabels & {#app: an, #stage: x.stage, "example.com/kind": "control-flow"}
			annotations: _stageAnnotations & {#app: an, #stage: x.stage}
		}
		spec: requestedFreight: [_freightRequest & {#app: an, #node: x}]
	},
	for an, p in pipelines for sn, s in p.stages for tn, _ in s.targets
	let d = deploys["\(an)/\(tn)"] {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name: (_tgtName & {#app: an, #target: tn}).out
			namespace: _kargoProject
			labels: _stageLabels & {#app: an, #stage: sn, "example.com/kind": "target", "example.com/target": tn, "example.com/deploy-with": d.deployWith, "example.com/platform": d.kind}
			annotations: _stageAnnotations & {#app: an, #stage: sn}
		}
		spec: {
			// the region's Kargo controller promotes it (control-flow Stages and Warehouses stay on the hub controller);
			// terraform targets touch no cluster: hub controller too
			if targets[tn].config.platform == "K8S" {shard: clusters[targets[tn].config.cluster].region}
			requestedFreight: [_freightRequest & {#app: an, #node: _graph[an][(_tgtName & {#app: an, #target: tn}).out]}]
			promotionTemplate: spec: {
				vars: [
					{name: "repo", value: _repoURL},
					{name: "src", value: "./src/\(renderedDir)/apps/\(_cap)/\(an)/\(sn)/\(tn)/"},
					{name: "branch", value: "env/\(_cap)/\(an)/\(tn)"},
					{name: "image", value: [if apps[an].module != _|_ {apps[an].module}, apps[an].image][0]},
					if d.deployWith == "gha" {{name: "workflowRepo", value: d.workflow.repo}},
					if d.deployWith == "gha" {{name: "workflowFile", value: d.workflow.file}},
					if d.deployWith == "gha" {{name: "workflowRef", value: d.workflow.ref}},
				]
				steps: [
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
						path: [if d.kind == "terraform" {"./out/terraform.yaml"}, "./out/deployment.yaml"][0]
						updates: [{
							key: [if d.kind == "terraform" {"module"}, "spec.template.spec.containers.0.image"][0]
							value: "${{ vars.image }}:${{ imageFrom(vars.image).Tag }}@${{ imageFrom(vars.image).Digest }}"
						}]
					}},
					{uses: "git-commit", as: "commit", config: {
						path:    "./out"
						message: "promote \(_cap)/\(an) to \(sn)/\(tn) from ${{ commitFrom(vars.repo).ID }}, \(_artifactKey[d.kind]) ${{ imageFrom(vars.image).Tag }}"
					}},
					{uses: "git-push", config: path: "./out"},
					if d.deployWith == "argocd" {{uses: "argocd-update", config: apps: [{
						name:      "\(_cap)-\(an)-\(tn)"
						namespace: "argocd"
						sources: [{repoURL: "${{ vars.repo }}", desiredRevision: "${{ outputs.commit.commit }}"}]
					}]}},
					// gha (ADR 0020): dispatch the workflow with the pushed commit, then poll its run until it
					// completes; Running (neither expression true) is retried until retry.timeout
					if d.deployWith == "gha" {{uses: "http", as: "dispatch", config: {
						method: "POST"
						url:    "\(_ghAPI)/actions/workflows/${{ vars.workflowFile }}/dispatches"
						headers: list.Concat([_ghHeaders, [{name: "Content-Type", value: "application/json"}]])
						// "} }": Kargo ends an expression at the first "}}"
						body:              "${{ quote({'ref': vars.workflowRef, 'return_run_details': true, 'inputs': {'capability': '\(_cap)', 'app': '\(an)', 'stage': '\(sn)', 'target': '\(tn)', 'commit': outputs.commit.commit, '\(_artifactKey[d.kind])': vars.image + ':' + imageFrom(vars.image).Tag + '@' + imageFrom(vars.image).Digest} }) }}"
						successExpression: "response.status == 200"
						failureExpression: "response.status != 200"
						errorExpression:   "response.body.message"
						outputs: [
							{name: "runID", fromExpression: "response.body.workflow_run_id"},
							{name: "runURL", fromExpression: "response.body.html_url"},
						]
					}}},
					if d.deployWith == "gha" {{uses: "http", as: "wait", retry: timeout: _waitTimeout[d.kind], config: {
						method:            "GET"
						url:               "\(_ghAPI)/actions/runs/${{ outputs.dispatch.runID }}"
						headers:           _ghHeaders
						successExpression: "response.status == 200 && response.body.status == 'completed' && response.body.conclusion == 'success'"
						failureExpression: "response.status != 200 || (response.body.status == 'completed' && response.body.conclusion != 'success')"
						errorExpression:   "response.status != 200 ? response.body.message : 'run ' + string(response.body.id) + ' ' + response.body.conclusion + ': ' + response.body.html_url"
					}}},
				]
			}
		}
	},
]

// Argo CD output per region (one Argo CD per region): rendered/argocd/projects/<region>/<cap>.yaml, an
// AppProject per capability with only that region's destinations (synced by the `projects` AppSet).
// Inside a regional Argo CD the workload cluster is its own (clusters[].argocdName, in-cluster).
// only argocd deploys get an Application / AppProject destination; gha deploys run a workflow (ADR 0020)
_argoDeploys: {for k, d in deploys if d.deployWith == "argocd" {(k): d}}
_deployRegion: {for _, d in _argoDeploys {(clusters[d.tgt.cluster].region): _}}
_appProject: {
	#region:    string
	apiVersion: "argoproj.io/v1alpha1"
	kind:       "AppProject"
	metadata: {name: _cap, namespace: "argocd"}
	spec: {
		sourceRepos: [_repoURL]
		destinations: [for k in list.SortStrings([for k, _ in {for _, d in _argoDeploys if clusters[d.tgt.cluster].region == #region {"\(clusters[d.tgt.cluster].argocdName)/\(d.tgt.namespace)": _}} {k}]) {
			name:      strings.Split(k, "/")[0]
			namespace: strings.Split(k, "/")[1]
		}]
		clusterResourceWhitelist: [{group: "", kind: "Namespace"}] // CreateNamespace=true
		namespaceResourceWhitelist: [{group: "*", kind: "*"}]
		// Argo CD UI/CLI: owners see this capability's apps, read-only (Kargo does all syncing)
		roles: [{
			name:        "viewer"
			description: "read-only view of \(_cap) apps for its promoters and viewers"
			policies: ["p, proj:\(_cap):viewer, applications, get, \(_cap)/*, allow"]
			// matched against the OIDC email claim (argocd-rbac-cm scopes: [email])
			groups: _allEmails
		}]
	}
}

argocdProjects: {for r, _ in _deployRegion {(r): _appProject & {#region: r}}}

// Workload Applications are generated in each regional Argo CD by the `workloads` ApplicationSet
// (platform/appsets/workloads.yaml: cluster gen x git files gen) from one params file per app+target:
// rendered/argocd/deploys/<region>/<cap>/<app>-<target>.json. Keys must match that template;
// git files gen reserves `path` and `values`.
argocdDeploys: {for _, d in _argoDeploys {
	"\(clusters[d.tgt.cluster].region)/\(_cap)/\(d.app)-\(d.target)": {
		appName:         "\(_cap)-\(d.app)-\(d.target)"
		capability:      _cap
		app:             d.app
		stage:           d.stage
		target:          d.target
		namespace:       d.tgt.namespace
		branch:          "env/\(_cap)/\(d.app)/\(d.target)"
		destination:     clusters[d.tgt.cluster].argocdName
		authorizedStage: "\(_kargoProject):\((_tgtName & {#app: d.app, #target: d.target}).out)"
	}
}}

// Promote RBAC: one Kargo ServiceAccount per app, mapped from the OIDC email claim to the app's
// promoters, allowed to promote (and manually approve Freight for) every control-flow and target Stage of that app.
// Only apps with stages: an empty resourceNames would mean *every* stage.
_rbacMeta: {
	#name:     string
	name:      #name
	namespace: _kargoProject
	labels: "app.kubernetes.io/managed-by":      "gitops-platform"
	annotations: "rbac.kargo.akuity.io/managed": "true"
}
_kargoRBAC: [for an in list.SortStrings([for an, _ in apps {an}]) if len(apps[an].stages) > 0 {
	let meta = _rbacMeta & {#name: "promote-\(an)"}
	[{
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: meta & {annotations: "rbac.kargo.akuity.io/claims": json.Marshal({email: _canPromote[an]})}
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "Role"
		metadata:   meta
		rules: [{
			apiGroups: ["kargo.akuity.io"]
			resources: ["stages"]
			resourceNames: list.SortStrings(_kargoStages[an])
			verbs: ["promote"]
		}, {
			// the promote API creates the Promotion as the user; which stages is limited above
			apiGroups: ["kargo.akuity.io"]
			resources: ["promotions"]
			verbs: ["create", "get", "list", "watch"]
		}, {
			// Freight assembly (Warehouses are Manual, ADR 0016). Names are content hashes, so this can't
			// be limited to the app's Warehouse: any promoter in the project can assemble for any app.
			apiGroups: ["kargo.akuity.io"]
			resources: ["freights"]
			verbs: ["create"]
		}]
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "RoleBinding"
		metadata:   meta
		roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: meta.name}
		subjects: [{kind: "ServiceAccount", name: meta.name, namespace: _kargoProject}]
	}]
}]

// read access to the whole project for every promoter and viewer: Kargo's own per-project kargo-viewer Role
_viewersMeta: _rbacMeta & {#name: "viewers"}
_kargoViewers: [{
	apiVersion: "v1"
	kind:       "ServiceAccount"
	metadata: _viewersMeta & {annotations: "rbac.kargo.akuity.io/claims": json.Marshal({email: _allEmails})}
}, {
	apiVersion: "rbac.authorization.k8s.io/v1"
	kind:       "RoleBinding"
	metadata:   _viewersMeta
	roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: "kargo-viewer"}
	subjects: [{kind: "ServiceAccount", name: _viewersMeta.name, namespace: _kargoProject}]
}]

// Listing projects needs cluster-scoped read (Kargo's kargo-user ClusterRole). Promoters/viewers get it via a
// global ServiceAccount (api.oidc.globalServiceAccounts: kargo-shared-resources). They see other
// projects' names; contents only where they promote or view (UI "My projects" filters to those).
_kargoUsers: [{
	apiVersion: "v1"
	kind:       "ServiceAccount"
	metadata: {
		name:      "users-\(_cap)"
		namespace: "kargo-shared-resources"
		labels: "app.kubernetes.io/managed-by": "gitops-platform"
		annotations: {
			"rbac.kargo.akuity.io/managed": "true"
			"rbac.kargo.akuity.io/claims": json.Marshal({email: _allEmails})
		}
	}
}, {
	apiVersion: "rbac.authorization.k8s.io/v1"
	kind:       "ClusterRoleBinding"
	metadata: {name: "kargo-users-\(_cap)", labels: "app.kubernetes.io/managed-by": "gitops-platform"}
	roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "ClusterRole", name: "kargo-user"}
	subjects: [{kind: "ServiceAccount", name: "users-\(_cap)", namespace: "kargo-shared-resources"}]
}]

kargo: list.Concat([_kargoCore, for r in _kargoRBAC {r}, _kargoViewers, _kargoUsers])
