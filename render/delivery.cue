package render

import (
	"encoding/json"
	"list"
)

// Promotion model (rendered-manifests + Kargo):
//   CI renders every (app, stage, target) to <repo>@main: rendered/apps/<cap>/<app>/<stage>/<target>/
//   Kargo Warehouse per app watches that path; Freight = a main commit that changed it.
//   Promoting Freight to a stage copies rendered/apps/<cap>/<app>/<stage>/ from that commit onto
//   branch env/<cap>/<app>/<stage>, then syncs the Argo CD apps for its targets.
//   Argo CD Application per (app, target) tracks that branch, path <target>/.

_kargoProject: "kargo-\(_cap)"
_kargoAPI:     "kargo.akuity.io/v1alpha1"

// naming: stage branch env/<cap>/<app>/<stage>, Argo app <cap>-<app>-<target>

_kargoCore: [
	{apiVersion: _kargoAPI, kind: "Project", metadata: name: _kargoProject},
	{
		apiVersion: _kargoAPI
		kind:       "ProjectConfig"
		metadata: {name: _kargoProject, namespace: _kargoProject}
		spec: promotionPolicies: [for an, p in pipelines for sn, s in p.stages if s.autoPromote {
			stageSelector: name: "\(an)-\(sn)"
			autoPromotionEnabled: true
		}]
	},
	for an, _ in pipelines {
		apiVersion: _kargoAPI
		kind:       "Warehouse"
		metadata: {name: an, namespace: _kargoProject}
		spec: subscriptions: [{git: {
			repoURL: _repoURL
			branch:  "main"
			includePaths: ["regexp:^\(renderedDir)/apps/\(_cap)/\(an)/"]
		}}]
	},
	for an, p in pipelines for sn, s in p.stages {
		apiVersion: _kargoAPI
		kind:       "Stage"
		metadata: {name: "\(an)-\(sn)", namespace: _kargoProject}
		spec: {
			requestedFreight: [{
				origin: {kind: "Warehouse", name: an}
				sources: [
					if len(s.dependsOn) == 0 {direct: true},
					{
						stages: [for d in s.dependsOn {"\(an)-\(d)"}]
						availabilityStrategy: "All" // verified in every upstream stage
					},
				][0]
			}]
			promotionTemplate: spec: {
				vars: [
					{name: "repo", value: _repoURL},
					{name: "src", value: "./src/\(renderedDir)/apps/\(_cap)/\(an)/\(sn)/"},
					{name: "branch", value: "env/\(_cap)/\(an)/\(sn)"},
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
					{uses: "git-commit", as: "commit", config: {
						path:    "./out"
						message: "promote \(_cap)/\(an) to \(sn) from ${{ commitFrom(vars.repo).ID }}"
					}},
					{uses: "git-push", config: path: "./out"},
					{uses: "argocd-update", config: apps: [for tn, _ in s.targets {
						name:      "\(_cap)-\(an)-\(tn)"
						namespace: "argocd"
						sources: [{repoURL: "${{ vars.repo }}", desiredRevision: "${{ outputs.commit.commit }}"}]
					}]},
				]
			}
		}
	},
]

argocd: [for _, d in deploys {
	apiVersion: "argoproj.io/v1alpha1"
	kind:       "Application"
	metadata: {
		name:      "\(_cap)-\(d.app)-\(d.target)"
		namespace: "argocd"
		annotations: "kargo.akuity.io/authorized-stage": "\(_kargoProject):\(d.app)-\(d.stage)"
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
			targetRevision: "env/\(_cap)/\(d.app)/\(d.stage)"
			path:           d.target
		}
		destination: {
			name:      clusters[d.tgt.cluster].argocdName
			namespace: d.tgt.namespace
		}
		syncPolicy: syncOptions: ["CreateNamespace=true"]
	}
}]

// Promote RBAC: one Kargo role per owning team (OIDC group claim), allowed to promote
// (and manually approve Freight for) every stage of the apps it owns.
// Only teams that own at least one stage: an empty resourceNames would mean *every* stage.
_promoteStages: {for t in _allOwners {
	(t): list.SortStrings([for an, o in owners if list.Contains(o, t) for sn, _ in apps[an].stages {"\(an)-\(sn)"}])
}}
_kargoRBAC: [for t, names in _promoteStages if len(names) > 0 {
	let meta = {
		name:      "owner-\(t)"
		namespace: _kargoProject
		labels: "app.kubernetes.io/managed-by":      "gitops-platform"
		annotations: "rbac.kargo.akuity.io/managed": "true"
	}
	[{
		apiVersion: "v1"
		kind:       "ServiceAccount"
		metadata: meta & {annotations: "rbac.kargo.akuity.io/claims": json.Marshal({groups: [teams[t].idpGroup]})}
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "Role"
		metadata:   meta
		rules: [{
			apiGroups: ["kargo.akuity.io"]
			resources: ["stages"]
			resourceNames: names
			verbs: ["promote"]
		}, {
			apiGroups: ["kargo.akuity.io"]
			resources: ["freights", "stages", "warehouses", "promotions"]
			verbs: ["get", "list", "watch"]
		}]
	}, {
		apiVersion: "rbac.authorization.k8s.io/v1"
		kind:       "RoleBinding"
		metadata:   meta
		roleRef: {apiGroup: "rbac.authorization.k8s.io", kind: "Role", name: meta.name}
		subjects: [{kind: "ServiceAccount", name: meta.name, namespace: _kargoProject}]
	}]
}]
kargo: list.Concat([_kargoCore, for r in _kargoRBAC {r}])
