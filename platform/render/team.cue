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
_platformSuffix: {K8S: "k8s", TERRAFORM: "tf"}

// targets/<name>/target.yml — where a stage deploys. Reusable across apps.
// Sibling environment-variables/<n>.yml files: see #EnvironmentVariables, envFrom.
// platform K8S (default): a namespace in a platform cluster, deployed by Argo CD or a gha workflow.
// platform TERRAFORM: a GCP project + GCS state backend, applied by a gha workflow (terraform apps only).
#Target: {
	config: {
		platform: *"K8S" | "TERRAFORM" | error("expected K8S or TERRAFORM")
		// free name (namespaces, labels); the type drives policy
		environment:      #Name
		environmentType!: #EnvironmentType | error("expected dev, npr or prd")
		auth: wifEmail: =~"^[a-z0-9-]+@[a-z0-9-]+\\.iam\\.gserviceaccount\\.com$"
		billing: =~"^[a-z0-9-]+$"
		// Constraints for both platforms here (the editor schema is exported generic, so it allows either);
		// which fields are required or forbidden is per platform below.
		cluster?:   or(_clusterNames) | error("unknown cluster; allowed: \(strings.Join(_clusterNames, ", "))")
		namespace?: =~"^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$"
		// TERRAFORM only: the GCP region (a K8S target's is its cluster's)
		region?: or(_gcpRegions) | error("unknown region; allowed: \(strings.Join(_gcpRegions, ", "))")
		terraform?: {
			gcpProject!: =~"^[a-z][a-z0-9-]{4,28}[a-z0-9]$" | error("expected a GCP project id")
			// GCS state; prefix default <capability>/<app>/<target>
			backend: {
				bucket!: =~"^[a-z0-9][a-z0-9._-]{1,61}[a-z0-9]$" | error("expected a GCS bucket name")
				prefix?: =~"^[A-Za-z0-9._/-]+$" | error("expected a GCS object prefix")
			}
		}
		if !generic {
			if platform == "K8S" {
				cluster:   _
				namespace: _
				if clusters[cluster] != _|_ {
					// the cluster must accept this environment type
					environmentType: or(clusters[cluster].environmentTypes) | error("cluster \(cluster) accepts environment types: \(strings.Join(clusters[cluster].environmentTypes, ", "))")
				}
				region?:    error("region not allowed on a K8S target (it's the cluster's)")
				terraform?: error("terraform not allowed on a K8S target (set platform: TERRAFORM)")
			}
			if platform == "TERRAFORM" {
				terraform:  _
				region:     _
				cluster?:   error("cluster not allowed on a TERRAFORM target")
				namespace?: error("namespace not allowed on a TERRAFORM target")
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
