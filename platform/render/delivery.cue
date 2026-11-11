package render

import (
	"encoding/json"
	"list"
	"strings"
)

// Promotion model (rendered-manifests + Kargo), one Kargo Stage per target (ADR 0012):
//   CI renders every (app, stage, target) to <repo>@rendered (branch, ADR 0022): rendered/apps/<cap>/<app>/<stage>/<target>/
//   Kargo Warehouse per app watches that path and the app's image repo (SemVer tags); Freight creation
//   is Manual: promoters assemble Freight = (config commit, image digest) in the UI (ADR 0016).
//   Target Stage <target> (in the app's project, ADR 0029) takes Freight from its upstream node (graph.cue, ADR 0019: the Warehouse,
//   upstream targets, or a control-flow Stage where many targets feed many or a stage needs a one-click
//   release point) and copies its target folder from that commit onto branch env/<cap>/<app>/<target>,
//   pins the Freight's image in its deployment.yaml, then syncs that target's Argo CD app.
//   Argo CD Application <cap>-<app>-<target> tracks that branch, path ".".
//   deployWith: gha targets (ADR 0020) get no Application: after the push the Stage dispatches a GitHub
//   Actions workflow (http step, PAT from the shared `github-actions` secret) and waits for its run.

// one Kargo project per app (ADR 0029); its name is also the project's namespace
_kargoProject: {for an, _ in pipelines {(an): "kargo-\(_cap)-\(an)"}}
_maxProjectName: 63
// gcd lint locates projectNameErrors.<app> at the app's app.yml
projectNameErrors: {for an, n in _kargoProject if len(n) > _maxProjectName {
	(an): error("Kargo project name \(n) is \(len(n)) characters (max \(_maxProjectName)): shorten the app name")
}}

// per deploy kind: what the Freight's image subscription pins (dispatch input, commit message)
_artifactKey: {k8s: "image", terraform: "module"}
// yaml-update target per platform: the file in the target folder and the key holding the artifact
_pinFile: {K8S: "./out/deployment.yaml", TERRAFORM: "./out/terraform.yaml", CLOUD_RUN: "./out/service.yaml"}
_pinKey: {K8S: "spec.template.spec.containers.0.image", TERRAFORM: "module", CLOUD_RUN: "spec.template.spec.containers.0.image"}
_platformLabel: {K8S: "k8s", TERRAFORM: "terraform", CLOUD_RUN: "cloudrun"}

// Kargo Stage names per app (graph.cue): target Stages <target> + control-flow Stages
_kargoStages: {for an, _ in pipelines {(an): list.SortStrings([for n, _ in _graph[an] {n}])}}
// Stage names must fit a label value: Kargo 1.12 labels each PromotionRequest with the unshortened
// Stage name (pkg/api/promotion_request.go), so a longer name creates the Stage but fails every promotion.
_maxStageName: 63
// Reported via stageNameErrors (not apps: the graph is computed from apps); gcd lint locates
// stageNameErrors.<app>.<stage>[.<target>] in the app's app.yml.
stageNameErrors: {
	for an, g in _graph for n, x in g if len(n) > _maxStageName {
		let _msg = "Kargo Stage name \(n) is \(len(n)) characters (max \(_maxStageName)): shorten the"
		if x.kind == "target" {(an): (x.stage): (x.target): error("\(_msg) target name")}
		if x.kind == "control-flow" {(an): (x.stage): error("\(_msg) stage names")}
	}

	// a control-flow Stage named like one of the app's targets (graph.cue skips emitting it)
	for an, a in _gp for n, sn in a.cf if a.tgts[n] != _|_ {
		(an): (sn): error("Kargo Stage name \(n) is used by both stage \(sn) and target \(n): rename the stage")
	}
}
// auto-promoted, per app: every control-flow Stage (it only means "upstream verified") + targets of autoPromote stages
_autoStages: {for an, p in pipelines {(an): list.SortStrings([for n, x in _graph[an]
	if x.kind == "control-flow" || p.stages[x.stage].autoPromote {n}])
}}
_stageAnnotations: {
	#app:   string
	#stage: string

	// read by the break-glass guard (platform/infrastructure/break-glass): who may approve unverified
	// Freight for this Stage (ADR 0013)
	"example.com/break-glass": json.Marshal(_breakGlass)
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

// per app: its project's Namespace, Project, ProjectConfig, Warehouse and Stages
_kargoCore: {for an, _ in pipelines {(an): [
	// Rendered (not left to Kargo) so Argo CD's dry-run of the namespaced resources below passes on
	// first sync; Kargo adopts a pre-existing namespace carrying this label. kargo-restore finds a
	// capability's projects by example.com/capability.
	{apiVersion: "v1", kind: "Namespace", metadata: {name: _kargoProject[an], labels: {
		"kargo.akuity.io/project": "true"
		"example.com/capability":  _cap
		"example.com/app":         an
	}}},
	{apiVersion: _kargoAPI, kind: "Project", metadata: name: _kargoProject[an]},
	{
		apiVersion: _kargoAPI
		kind:       "ProjectConfig"
		metadata: {name: _kargoProject[an], namespace: _kargoProject[an]}
		// a stage's autoPromote covers its targets; control-flow Stages always auto-promote
		spec: promotionPolicies: [for n in _autoStages[an] {stageSelector: name: n, autoPromotionEnabled: true}]
	},
	{
		apiVersion: _kargoAPI
		kind:       "Warehouse"
		metadata: {name: an, namespace: _kargoProject[an]}
		spec: {
			freightCreationPolicy: "Manual"
			subscriptions: [{git: {
				repoURL: _repoURL
				branch:  renderedBranch
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
	for n, x in _graph[an] if x.kind == "control-flow" {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name:      n
			namespace: _kargoProject[an]
			labels: _stageLabels & {#app: an, #stage: x.stage, "example.com/kind": "control-flow"}
			annotations: _stageAnnotations & {#app: an, #stage: x.stage}
		}
		spec: requestedFreight: [_freightRequest & {#app: an, #node: x}]
	},
	for sn, s in pipelines[an].stages for tn, _ in s.targets
	let d = deploys["\(an)/\(tn)"]
	// platform pre-deploy checks for this target's environment type (prechecks.cue, ADR 0028)
	let _checks = [if preDeploy[d.tgt.environmentType] != _|_ {preDeploy[d.tgt.environmentType]}, []][0] {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name:      tn
			namespace: _kargoProject[an]
			labels: _stageLabels & {#app: an, #stage: sn, "example.com/kind": "target", "example.com/target": tn, "example.com/deploy-with": d.deployWith, "example.com/platform": _platformLabel[d.tgt.platform]}
			annotations: _stageAnnotations & {#app: an, #stage: sn}
			if len(_checks) > 0 {
				annotations: "example.com/pre-deploy": strings.Join([for c in _checks {"\(apps[an].sourceRepo)/\(c.file)@\(c.ref)"}], ",")
			}
		}
		spec: {
			// the region's Kargo controller promotes it (control-flow Stages and Warehouses stay on the hub controller);
			// terraform and Cloud Run targets touch no cluster: hub controller too
			if targets[tn].config.platform == "K8S" {shard: clusters[targets[tn].config.cluster].region}
			requestedFreight: [_freightRequest & {#app: an, #node: _graph[an][tn]}]
			// steps: the shared ClusterPromotionTask for this deployWith (promotiontasks.cue, ADR 0027); its vars
			// come from here. kargo-restore reads repo/branch/image by name.
			promotionTemplate: spec: {
				vars: [
					{name: "repo", value: _repoURL},
					{name: "src", value: "./src/\(renderedDir)/apps/\(_cap)/\(an)/\(sn)/\(tn)/"},
					{name: "branch", value: "env/\(_cap)/\(an)/\(tn)"},
					{name: "image", value: [if apps[an].module != _|_ {apps[an].module}, apps[an].image][0]},
					{name: "capability", value: _cap},
					{name: "app", value: an},
					{name: "stage", value: sn},
					{name: "target", value: tn},
					{name: "pinFile", value: _pinFile[d.tgt.platform]},
					{name: "pinKey", value: _pinKey[d.tgt.platform]},
					{name: "artifactKey", value: _artifactKey[d.kind]},
					if d.deployWith == "gha" {{name: "workflowRepo", value: d.workflow.repo}},
					if d.deployWith == "gha" {{name: "workflowFile", value: d.workflow.file}},
					if d.deployWith == "gha" {{name: "workflowRef", value: d.workflow.ref}},
				]
				// pre-<n>: gha-check per platform check, dispatched to the app's sourceRepo; workflow vars on the
				// step itself so a gha deploy's Promotion-level workflow vars stay for `promote`
				steps: list.Concat([[for i, c in _checks {
					as: "pre-\(i+1)"
					task: {kind: "ClusterPromotionTask", name: "gha-check"}
					vars: [
						{name: "workflowRepo", value: apps[an].sourceRepo},
						{name: "workflowFile", value: c.file},
						{name: "workflowRef", value: c.ref},
						{name: "checkTimeout", value: c.timeout},
					]
				}], [{as: "promote", task: {kind: "ClusterPromotionTask", name: "promote-\(d.deployWith)"}}]])
			}
		}
	},
]}}

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
		authorizedStage: "\(_kargoProject[d.app]):\(d.target)"
	}
}}

// Promote RBAC (ADR 0023, 0029): in each app's project, one Kargo ServiceAccount promote-<stage> per stage, mapped from the OIDC email claim to
// that stage's promoters + break-glass, allowed to promote (and manually approve Freight for) the Kargo Stages
// of that capability stage. Only stages with Kargo Stages: an empty resourceNames would mean *every* stage.
_rbacMeta: {
	#app:      string
	#name:     string
	name:      #name
	namespace: _kargoProject[#app]
	labels: "app.kubernetes.io/managed-by":      "gitops-platform"
	annotations: "rbac.kargo.akuity.io/managed": "true"
}
// Capability stages each Kargo Stage (graph node) belongs to: targets and entry control-flow Stages to their
// own stage; a shared control-flow Stage to every stage consuming it.
_nodeStages: {for an, nodes in _graph {(an): {for n, x in nodes {
	(n): [
		if x.kind == "target" {[x.stage]},
		if n == x.stage {[x.stage]},
		[for _, g in _gp[an].grp if g.shared if g.name == n for m in g.mem {m}],
	][0]
}}}}
_kargoRBAC: {for an, _ in pipelines {(an): list.Concat([for sn in list.SortStrings([for sn, _ in _promoters[an] {sn}]) let _names = list.SortStrings([for n, _ in _graph[an] if list.Contains(_nodeStages[an][n], sn) {n}]) if len(_names) > 0 {
	let meta = _rbacMeta & {#app: an, #name: "promote-\(sn)"}
	let _emails = (_uniqSorted & {#in: list.Concat([_promoters[an][sn], _breakGlass])}).out
	[{
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: meta & {annotations: "rbac.kargo.akuity.io/claims": json.Marshal({email: _emails})}
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "Role"
		metadata:   meta
		rules: [{
			apiGroups: ["kargo.akuity.io"]
			resources: ["stages"]
			resourceNames: _names
			verbs: ["promote"]
		}, {
			// the promote API creates the Promotion as the user; which stages is limited above
			apiGroups: ["kargo.akuity.io"]
			resources: ["promotions"]
			verbs: ["create", "get", "list", "watch"]
		}, {
			// Freight assembly (Warehouses are Manual, ADR 0016). Names are content hashes, so this can't
			// name a Warehouse; the project holds only this app's (ADR 0029).
			apiGroups: ["kargo.akuity.io"]
			resources: ["freights"]
			verbs: ["create"]
		}]
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "RoleBinding"
		metadata:   meta
		roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: meta.name}
		subjects: [{kind: "ServiceAccount", name: meta.name, namespace: _kargoProject[an]}]
	}]
}])
}}

// read access to the app's project for its promoters, viewers and break-glass (_appEmails): Kargo's own
// per-project kargo-viewer Role
_kargoViewers: {for an, _ in pipelines let meta = _rbacMeta & {#app: an, #name: "viewers"} {(an): [
	{
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: meta & {annotations: "rbac.kargo.akuity.io/claims": json.Marshal({email: _appEmails[an]})}
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "RoleBinding"
		metadata:   meta
		roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: "kargo-viewer"}
		subjects: [{kind: "ServiceAccount", name: meta.name, namespace: _kargoProject[an]}]
	}]}}

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

kargo: list.Concat([for an in list.SortStrings([for an, _ in pipelines {an}]) for l in [_kargoCore[an], _kargoRBAC[an], _kargoViewers[an]] {l}, _kargoUsers])
