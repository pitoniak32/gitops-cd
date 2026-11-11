package render

import (
	"list"
	"strings"
)

// stages/<name>.yml
#Stage: {
	dependsOn: [...[if generic {#Name}, or(_stageNames) | error("unknown stage; allowed: \(_stageList)")][0]]
	// promote automatically once upstream stages are verified
	autoPromote: *false | bool
	// Kargo UI color for all of this stage's Kargo Stages (control flow + targets); default picked per stage
	color?: or(_kargoColors) | error("unknown color; allowed: \(strings.Join(_kargoColors, ", "))")
}

// Kargo UI named stage colors (kargo.akuity.io/color)
_kargoColors: ["red", "salmon", "orange", "amber", "yellow", "lime", "avocado", "green", "teal", "cyan", "sky", "blue", "indigo", "periwinkle", "violet", "purple", "fuchsia", "pink", "rose", "dragonfruit", "gray"]
// default color: spread sorted stage names across the palette
_defaultColors: ["blue", "amber", "violet", "green", "red", "teal", "pink", "orange", "indigo", "lime"]
for i, n in list.SortStrings(_stageNames) {
	stages: (n): color: *_defaultColors[mod(i, len(_defaultColors))] | _
}

// platform suffix in target names (ADR 0025)
_platformSuffix: {K8S: "k8s", TERRAFORM: "tf", CLOUD_RUN: "cr"}

// Field constraints, applied only where the platform allows the field (forbidden fields carry just their
// "not allowed" error, so it's the only line printed); the generic editor schema applies all of them.

_clusterC:    or(_clusterNames) | error("unknown cluster; allowed: \(strings.Join(_clusterNames, ", "))")
_namespaceC:  =~"^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$"
_regionC:     or(_gcpRegions) | error("unknown region; allowed: \(strings.Join(_gcpRegions, ", "))")
_gcpProjectC: =~"^[a-z][a-z0-9-]{4,28}[a-z0-9]$" | error("expected a GCP project id")
_backendC: {
	bucket!: =~"^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$" | error("expected a GCS bucket name")
	prefix?: =~"^[A-Za-z0-9._/-]+$" | error("expected a GCS object prefix")
}

// targets/<name>/target.yml — where a stage deploys. Reusable across apps.
// Sibling environment-variables/<n>.yml files: see #EnvironmentVariables, envFrom.
// platform K8S (default): a namespace in a platform cluster, deployed by Argo CD or a gha workflow.
// platform TERRAFORM: a GCP project + GCS state backend, applied by a gha workflow (terraform apps only).
// platform CLOUD_RUN: a GCP project + region, a Cloud Run service deployed by a gha workflow (image apps only).
#Target: {
	config: {
		platform: *"K8S" | "TERRAFORM" | "CLOUD_RUN" | error("expected K8S, TERRAFORM or CLOUD_RUN")
		// free name (namespaces, labels); the type drives policy
		environment:      #Name
		environmentType!: #EnvironmentType | error("expected dev, npr or prd")
		auth: wifEmail: =~"^[a-z0-9-]+@[a-z0-9-]+\\.iam\\.gserviceaccount\\.com$"
		billing: =~"^[a-z0-9-]+$"
		// Which fields are required or forbidden is per platform. cluster, namespace: K8S only. region
		// (the GCP region; a K8S target's is its cluster's), gcpProject: TERRAFORM and CLOUD_RUN.
		// backend (GCS state; prefix default <capability>/<app>/<target>): TERRAFORM only.
		cluster?:    _
		namespace?:  _
		region?:     _
		gcpProject?: _
		backend?:    _
		if generic {
			cluster?:    _clusterC
			namespace?:  _namespaceC
			region?:     _regionC
			gcpProject?: _gcpProjectC
			backend?:    _backendC
		}
		if !generic {
			if platform == "K8S" {
				cluster:   _clusterC
				namespace: _namespaceC
				if clusters[cluster] != _|_ {
					// the cluster must accept this environment type
					environmentType: or(clusters[cluster].environmentTypes) | error("cluster \(cluster) accepts environment types: \(strings.Join(clusters[cluster].environmentTypes, ", "))")
				}
				region?:     error("region not allowed on a K8S target (it's the cluster's)")
				gcpProject?: error("gcpProject not allowed on a K8S target")
				backend?:    error("backend not allowed on a K8S target")
			}
			if platform == "TERRAFORM" {
				gcpProject: _gcpProjectC
				backend:    _backendC
				region:     _regionC
				cluster?:   error("cluster not allowed on a TERRAFORM target")
				namespace?: error("namespace not allowed on a TERRAFORM target")
			}
			if platform == "CLOUD_RUN" {
				gcpProject: _gcpProjectC
				region:     _regionC
				cluster?:   error("cluster not allowed on a CLOUD_RUN target")
				namespace?: error("namespace not allowed on a CLOUD_RUN target")
				backend?:   error("backend not allowed on a CLOUD_RUN target")
			}
		}
	}
}

// Longest path from a root. A dependency cycle shows up as "structural cycle".
stageDepth: {for s, v in stages {
	(s): list.Max([0, for d in v.dependsOn {stageDepth[d] + 1}])
}}

// Prod is only ever promoted by a person: no autoPromote on a stage that deploys to a prod target.
_prodStages: {for _, d in deploys if d.tgt.environmentType == "prd" {(d.stage): _}}
stages: [S=_]: {
	if _prodStages[S] != _|_ {
		autoPromote: false | error("stage \(S) deploys to prod targets; autoPromote not allowed")
	}
}
