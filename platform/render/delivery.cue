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
//   Gate Stage <app>-<stage> (control flow, no steps) per gitops stage: the first takes Freight from
//   the Warehouse, later ones once it's verified in every target Stage of the upstream stage(s).
//   Target Stage <app>-<target> takes Freight from its gate and copies its target folder from that
//   commit onto branch env/<cap>/<app>/<target>, pins the Freight's image in its kustomization.yaml,
//   then syncs that target's Argo CD app.
//   Argo CD Application <cap>-<app>-<target> tracks that branch, path ".".

_kargoProject: "kargo-\(_cap)"
_kargoAPI:     "kargo.akuity.io/v1alpha1"

// Kargo Stage names per app: gates first, then targets (target names never equal stage names)
_gateName: {#app: string, #stage: string, out: "\(#app)-\(#stage)"}
_tgtName: {#app: string, #target: string, out: "\(#app)-\(#target)"}
_kargoStages: {for an, p in pipelines {(an): list.Concat([
	[for sn, _ in p.stages {(_gateName & {#app: an, #stage: sn}).out}],
	[for sn, s in p.stages for tn, _ in s.targets {(_tgtName & {#app: an, #target: tn}).out}],
])
}}
_autoStages: list.Concat([for an, p in pipelines for sn, s in p.stages if s.autoPromote {list.Concat([
	[(_gateName & {#app: an, #stage: sn}).out],
	[for tn, _ in s.targets {(_tgtName & {#app: an, #target: tn}).out}],
])
}])
_stageAnnotations: {
	#app:   string
	#stage: string

	// read by the break-glass guard (platform/infrastructure/break-glass): who may approve unverified
	// Freight for this Stage (ADR 0013)
	"example.com/break-glass": json.Marshal(_breakGlass[#app])
	// Kargo UI: a stage's gate and targets share its color
	"kargo.akuity.io/color": stages[#stage].color
}
_stageLabels: {
	#app:                     string
	#stage:                   string
	"example.com/capability": _cap
	"example.com/app":        #app
	"example.com/stage":      #stage
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
		// a stage's autoPromote covers its gate and all its targets
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
				repoURL:                apps[an].image
				imageSelectionStrategy: "SemVer"
				if apps[an].imageConstraint != _|_ {constraint: apps[an].imageConstraint}
				discoveryLimit: 10
			}}]
		}
	},
	// gates: control flow Stages (no promotionTemplate); Freight promoted here is verified at once
	for an, p in pipelines for sn, s in p.stages {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name: (_gateName & {#app: an, #stage: sn}).out
			namespace: _kargoProject
			labels: _stageLabels & {#app: an, #stage: sn, "example.com/kind": "gate"}
			annotations: _stageAnnotations & {#app: an, #stage: sn}
		}
		spec: requestedFreight: [{
			origin: {kind: "Warehouse", name: an}
			sources: [
				if len(s.dependsOn) == 0 {direct: true},
				{
					stages: list.SortStrings([for d in s.dependsOn for tn, _ in p.stages[d].targets {(_tgtName & {#app: an, #target: tn}).out}])
					availabilityStrategy: "All" // verified in every target of every upstream stage
				},
			][0]
		}]
	},
	for an, p in pipelines for sn, s in p.stages for tn, _ in s.targets {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {
			name: (_tgtName & {#app: an, #target: tn}).out
			namespace: _kargoProject
			labels: _stageLabels & {#app: an, #stage: sn, "example.com/kind": "target", "example.com/target": tn}
			annotations: _stageAnnotations & {#app: an, #stage: sn}
		}
		spec: {
			// the region's Kargo controller promotes it (gates and Warehouses stay on the hub controller)
			shard: clusters[targets[tn].config.cluster].region
			requestedFreight: [{
				origin: {kind: "Warehouse", name: an}
				sources: stages: [(_gateName & {#app: an, #stage: sn}).out]
			}]
			promotionTemplate: spec: {
				vars: [
					{name: "repo", value: _repoURL},
					{name: "src", value: "./src/\(renderedDir)/apps/\(_cap)/\(an)/\(sn)/\(tn)/"},
					{name: "branch", value: "env/\(_cap)/\(an)/\(tn)"},
					{name: "image", value: apps[an].image},
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
					{uses: "kustomize-set-image", config: {
						path: "./out"
						images: [{image: "${{ vars.image }}", digest: "${{ imageFrom(vars.image).Digest }}"}]
					}},
					{uses: "git-commit", as: "commit", config: {
						path:    "./out"
						message: "promote \(_cap)/\(an) to \(sn)/\(tn) from ${{ commitFrom(vars.repo).ID }}, image ${{ imageFrom(vars.image).Tag }}"
					}},
					{uses: "git-push", config: path: "./out"},
					{uses: "argocd-update", config: apps: [{
						name:      "\(_cap)-\(an)-\(tn)"
						namespace: "argocd"
						sources: [{repoURL: "${{ vars.repo }}", desiredRevision: "${{ outputs.commit.commit }}"}]
					}]},
				]
			}
		}
	},
]

// Argo CD output per region (one Argo CD per region, rendered/argocd/<region>/<cap>.yaml): an AppProject
// per capability with only that region's destinations, plus the Applications deployed there.
// Inside a regional Argo CD the workload cluster is its own (clusters[].argocdName, in-cluster).
_deployRegion: {for _, d in deploys {(clusters[d.tgt.cluster].region): _}}
_appProject: {
	#region:    string
	apiVersion: "argoproj.io/v1alpha1"
	kind:       "AppProject"
	metadata: {name: _cap, namespace: "argocd"}
	spec: {
		sourceRepos: [_repoURL]
		destinations: [for k in list.SortStrings([for k, _ in {for _, d in deploys if clusters[d.tgt.cluster].region == #region {"\(clusters[d.tgt.cluster].argocdName)/\(d.tgt.namespace)": _}} {k}]) {
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

argocd: {for r, _ in _deployRegion {
	(r): list.Concat([[_appProject & {#region: r}], [for a in _argocdApps if a.#region == r {a.app}]])
}}
_argocdApps: [for _, d in deploys {
	#region: clusters[d.tgt.cluster].region
	app: {
		apiVersion: "argoproj.io/v1alpha1"
		kind:       "Application"
		metadata: {
			name:      "\(_cap)-\(d.app)-\(d.target)"
			namespace: "argocd"
			annotations: "kargo.akuity.io/authorized-stage": "\(_kargoProject):\((_tgtName & {#app: d.app, #target: d.target}).out)"
			labels: {
				"example.com/capability": _cap
				"example.com/app":        d.app
				"example.com/stage":      d.stage
				"example.com/target":     d.target
			}
		}
		spec: {
			project: _cap // Argo CD AppProject per capability (platform-owned)
			source: {
				repoURL:        _repoURL
				targetRevision: "env/\(_cap)/\(d.app)/\(d.target)"
				path:           "."
			}
			destination: {
				name:      clusters[d.tgt.cluster].argocdName
				namespace: d.tgt.namespace
			}
			syncPolicy: {
				automated: {prune: true, selfHeal: true}
				syncOptions: ["CreateNamespace=true"]
			}
		}
	}
}]

// Promote RBAC: one Kargo ServiceAccount per app, mapped from the OIDC email claim to the app's
// promoters, allowed to promote (and manually approve Freight for) every gate and target Stage of that app.
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
